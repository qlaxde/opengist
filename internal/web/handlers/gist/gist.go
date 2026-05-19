package gist

import (
	"bufio"
	"bytes"
	gojson "encoding/json"
	"fmt"
	"html/template"
	"mime"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/thomiceli/opengist/internal/db"
	"github.com/thomiceli/opengist/internal/git"
	"github.com/thomiceli/opengist/internal/render"
	"github.com/thomiceli/opengist/internal/web/context"
)

type renderedComment struct {
	*db.GistComment
	HTML template.HTML
}

func loadRenderedComments(gistID uint) ([]renderedComment, error) {
	comments, err := db.GetCommentsByGistID(gistID)
	if err != nil {
		return nil, err
	}
	out := make([]renderedComment, len(comments))
	for i, c := range comments {
		html, err := render.MarkdownString(c.Content)
		if err != nil {
			return nil, err
		}
		out[i] = renderedComment{GistComment: c, HTML: template.HTML(html)}
	}
	return out, nil
}

func GistIndex(ctx *context.Context) error {
	if ctx.GetData("gistpage") == "js" {
		return GistJs(ctx)
	} else if ctx.GetData("gistpage") == "json" {
		return GistJson(ctx)
	}

	gist := ctx.GetData("gist").(*db.Gist)
	revision := ctx.Param("revision")

	if revision == "" {
		revision = "HEAD"
	}

	files, hasMoreFiles, err := gist.Files(revision, true)
	if _, ok := err.(*git.RevisionNotFoundError); ok {
		return ctx.NotFound("Revision not found")
	} else if err != nil {
		return ctx.ErrorRes(500, "Error fetching files", err)
	}

	renderedFiles := render.RenderFiles(files)

	comments, err := loadRenderedComments(gist.ID)
	if err != nil {
		return ctx.ErrorRes(500, "Error loading comments", err)
	}
	ctx.SetData("comments", comments)
	if u := ctx.User; u != nil {
		ctx.SetData("canComment", true)
		ctx.SetData("isGistOwner", u.ID == gist.UserID)
		ctx.SetData("viewerID", u.ID)
		ctx.SetData("viewerIsAdmin", u.IsAdmin)
	}

	ctx.SetData("page", "code")
	ctx.SetData("commit", revision)
	ctx.SetData("files", renderedFiles)
	ctx.SetData("hasMoreFiles", hasMoreFiles)
	ctx.SetData("revision", revision)
	ctx.SetData("htmlTitle", gist.Title)
	return ctx.Html("gist.html")
}

func GistJson(ctx *context.Context) error {
	gist := ctx.GetData("gist").(*db.Gist)
	files, hasMoreFiles, err := gist.Files("HEAD", true)
	if err != nil {
		return ctx.ErrorRes(500, "Error fetching files", err)
	}

	renderedFiles := render.RenderFiles(files)
	ctx.SetData("files", renderedFiles)
	ctx.SetData("hasMoreFiles", hasMoreFiles)

	topics, err := gist.GetTopics()
	if err != nil {
		return ctx.ErrorRes(500, "Error fetching topics for gist", err)
	}

	htmlbuf := bytes.Buffer{}
	w := bufio.NewWriter(&htmlbuf)
	if err = ctx.Echo().Renderer.Render(w, "gist_embed.html", ctx.DataMap(), ctx); err != nil {
		return err
	}
	_ = w.Flush()

	jsUrl, err := url.JoinPath(ctx.GetData("baseHttpUrl").(string), gist.User.Username, gist.Identifier()+".js")
	if err != nil {
		return ctx.ErrorRes(500, "Error joining js url", err)
	}

	cssUrl, err := url.JoinPath(ctx.GetData("baseHttpUrl").(string), context.ManifestEntries["embed.css"].File)
	if err != nil {
		return ctx.ErrorRes(500, "Error joining css url", err)
	}

	return ctx.JSON(200, map[string]interface{}{
		"owner":       gist.User.Username,
		"id":          gist.Identifier(),
		"uuid":        gist.Uuid,
		"title":       gist.Title,
		"description": gist.Description,
		"created_at":  time.Unix(gist.CreatedAt, 0).Format(time.RFC3339),
		"visibility":  gist.VisibilityStr(),
		"files":       renderedFiles,
		"topics":      topics,
		"embed": map[string]string{
			"html":    htmlbuf.String(),
			"css":     cssUrl,
			"js":      jsUrl,
			"js_dark": jsUrl + "?dark",
		},
	})
}

func GistJs(ctx *context.Context) error {
	theme := "light"
	if _, exists := ctx.QueryParams()["dark"]; exists {
		ctx.SetData("dark", "dark")
		theme = "dark"
	}

	gist := ctx.GetData("gist").(*db.Gist)
	files, hasMoreFiles, err := gist.Files("HEAD", true)
	if err != nil {
		return ctx.ErrorRes(500, "Error fetching files", err)
	}

	renderedFiles := render.RenderFiles(files)
	ctx.SetData("files", renderedFiles)
	ctx.SetData("hasMoreFiles", hasMoreFiles)

	htmlbuf := bytes.Buffer{}
	w := bufio.NewWriter(&htmlbuf)
	if err = ctx.Echo().Renderer.Render(w, "gist_embed.html", ctx.DataMap(), ctx); err != nil {
		return err
	}
	_ = w.Flush()

	cssUrl, err := url.JoinPath(ctx.GetData("baseHttpUrl").(string), context.ManifestEntries["ts/embed.ts"].Css[0])
	if err != nil {
		return ctx.ErrorRes(500, "Error joining css url", err)
	}

	themeUrl, err := url.JoinPath(ctx.GetData("baseHttpUrl").(string), context.ManifestEntries["ts/"+theme+".ts"].Css[0])
	if err != nil {
		return ctx.ErrorRes(500, "Error joining theme url", err)
	}

	js, err := escapeJavaScriptContent(htmlbuf.String(), cssUrl, themeUrl)
	if err != nil {
		return ctx.ErrorRes(500, "Error escaping JavaScript content", err)
	}
	ctx.Response().Header().Set("Content-Type", "text/javascript")
	return ctx.PlainText(200, js)
}

// GistSite serves the gist as a mini-site so relative paths between sibling
// files just work. With no file name it returns the first .html file; with a
// file name it returns that file with its real Content-Type. The
// X-Content-Type-Options: nosniff header is dropped so HTML executes and ES
// modules / stylesheets load. Visibility rules follow gistInit.
func GistSite(ctx *context.Context) error {
	gist := ctx.GetData("gist").(*db.Gist)
	filename := ctx.Param("*")

	if filename == "" || strings.HasSuffix(filename, "/") {
		files, _, err := gist.Files("HEAD", false)
		if err != nil {
			return ctx.ErrorRes(500, "Error fetching files", err)
		}
		for _, f := range files {
			if strings.HasSuffix(strings.ToLower(f.Filename), ".html") {
				return writeSiteFile(ctx, f)
			}
		}
		return ctx.NotFound("No HTML file found in this gist")
	}

	file, err := gist.File("HEAD", filename, false)
	if err != nil {
		return ctx.ErrorRes(500, "Error getting file content", err)
	}
	if file == nil {
		return ctx.NotFound("File not found")
	}
	return writeSiteFile(ctx, file)
}

func writeSiteFile(ctx *context.Context, file *git.File) error {
	ctx.Response().Header().Del("X-Content-Type-Options")
	ctx.Response().Header().Set("Content-Type", siteContentType(file))
	return ctx.PlainText(200, file.Content)
}

// siteContentType picks a real Content-Type for a gist file served at /site/.
// It prefers the file extension (so .js gets application/javascript, not
// text/plain), and falls back to the detected MIME for files without a
// known extension.
func siteContentType(file *git.File) string {
	ext := strings.ToLower(filepath.Ext(file.Filename))
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".wasm":
		return "application/wasm"
	case ".map":
		return "application/json; charset=utf-8"
	}
	if mt := mime.TypeByExtension(ext); mt != "" {
		return mt
	}
	if file.MimeType.ContentType != "" {
		return file.MimeType.ContentType
	}
	return "application/octet-stream"
}

func Preview(ctx *context.Context) error {
	content := ctx.FormValue("content")

	previewStr, err := render.MarkdownString(content)
	if err != nil {
		return ctx.ErrorRes(500, "Error rendering markdown", err)
	}

	return ctx.PlainText(200, previewStr)
}

func escapeJavaScriptContent(htmlContent, cssUrl, themeUrl string) (string, error) {
	jsonContent, err := gojson.Marshal(htmlContent)
	if err != nil {
		return "", fmt.Errorf("failed to encode content: %w", err)
	}

	jsonCssUrl, err := gojson.Marshal(cssUrl)
	if err != nil {
		return "", fmt.Errorf("failed to encode CSS URL: %w", err)
	}

	jsonThemeUrl, err := gojson.Marshal(themeUrl)
	if err != nil {
		return "", fmt.Errorf("failed to encode Theme URL: %w", err)
	}

	js := fmt.Sprintf(`
(function() {
    if (!customElements.get('opengist-embed')) {
        customElements.define('opengist-embed', class extends HTMLElement {
            constructor() {
                super();
                this.attachShadow({ mode: 'open' });
            }
            
            init(css1, css2, content) {
                this.shadowRoot.innerHTML = %s
                    <style>
                        @import url(${css1});
                        @import url(${css2});
                        :host { display: block; all: initial; font-family: sans-serif; }
                    </style>
                    <div class="container">${content}</div>
                %s;
            }
        });
    }

    var currentScript = document.currentScript || (function() {
        var scripts = document.getElementsByTagName('script');
        return scripts[scripts.length - 1];
    })();

    const instance = document.createElement('opengist-embed');
    instance.init(%s, %s, %s);
 	currentScript.parentNode.insertBefore(instance, currentScript.nextSibling);
})();
`,
		"`",
		"`",
		string(jsonCssUrl),
		string(jsonThemeUrl),
		string(jsonContent),
	)

	return js, nil
}
