package gist_test

import (
	"archive/zip"
	"bytes"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thomiceli/opengist/internal/db"
	webtest "github.com/thomiceli/opengist/internal/web/test"
)

func TestDownloadZip(t *testing.T) {
	s := webtest.Setup(t)
	defer webtest.Teardown(t)

	t.Run("MultipleFiles", func(t *testing.T) {
		_, _, username, identifier := s.CreateGist(t, "0")

		resp := s.Request(t, "GET", "/"+username+"/"+identifier+"/archive/HEAD", nil, 200)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		zipReader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		require.NoError(t, err)
		require.Len(t, zipReader.File, 2)

		fileNames := make([]string, len(zipReader.File))
		contents := make([]string, len(zipReader.File))
		for i, file := range zipReader.File {
			fileNames[i] = file.Name
			f, err := file.Open()
			require.NoError(t, err)
			content, err := io.ReadAll(f)
			require.NoError(t, err)
			contents[i] = string(content)
			f.Close()
		}
		require.ElementsMatch(t, []string{"file.txt", "otherfile.txt"}, fileNames)
		require.ElementsMatch(t, []string{"hello world", "other content"}, contents)
	})

	t.Run("PrivateGist", func(t *testing.T) {
		_, _, username, identifier := s.CreateGist(t, "2")

		s.Request(t, "GET", "/"+username+"/"+identifier+"/archive/HEAD", nil, 404)
	})

	t.Run("NonExistentRevision", func(t *testing.T) {
		_, _, username, identifier := s.CreateGist(t, "0")

		// TODO: return 404
		s.Request(t, "GET", "/"+username+"/"+identifier+"/archive/zz", nil, 0)
	})
}

func TestRawFile(t *testing.T) {
	s := webtest.Setup(t)
	defer webtest.Teardown(t)

	t.Run("ExistingFile", func(t *testing.T) {
		_, _, username, identifier := s.CreateGist(t, "0")

		resp := s.Request(t, "GET", "/"+username+"/"+identifier+"/raw/HEAD/file.txt", nil, 200)

		require.Equal(t, `inline; filename="file.txt"`, resp.Header.Get("Content-Disposition"))
		require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
		require.Contains(t, resp.Header.Get("Content-Type"), "text/plain")

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "hello world", string(body))
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		_, _, username, identifier := s.CreateGist(t, "0")

		s.Request(t, "GET", "/"+username+"/"+identifier+"/raw/HEAD/nonexistent.txt", nil, 404)
	})

	t.Run("NonExistentRevision", func(t *testing.T) {
		_, _, username, identifier := s.CreateGist(t, "0")

		s.Request(t, "GET", "/"+username+"/"+identifier+"/raw/zz/file.txt", nil, 404)
	})

	t.Run("PrivateGist", func(t *testing.T) {
		_, _, username, identifier := s.CreateGist(t, "2")

		s.Request(t, "GET", "/"+username+"/"+identifier+"/raw/HEAD/file.txt", nil, 404)
	})
}

func TestGistSite(t *testing.T) {
	s := webtest.Setup(t)
	defer webtest.Teardown(t)

	s.Register(t, "thomas")
	s.Register(t, "alice")

	createSiteGist := func(t *testing.T, visibility string, files map[string]string) (string, string) {
		s.Login(t, "thomas")
		form := url.Values{
			"title":   {"Site"},
			"private": {visibility},
		}
		for name, content := range files {
			form.Add("name", name)
			form.Add("content", content)
		}
		resp := s.Request(t, "POST", "/", form, 302)
		parts := strings.Split(strings.TrimPrefix(resp.Header.Get("Location"), "/"), "/")
		require.Len(t, parts, 2)
		s.Logout()
		return parts[0], parts[1]
	}

	t.Run("IndexServesFirstHtml", func(t *testing.T) {
		user, id := createSiteGist(t, "0", map[string]string{
			"index.html": "<!DOCTYPE html><html><body><h1>hello</h1></body></html>",
		})
		resp := s.Request(t, "GET", "/"+user+"/"+id+"/site", nil, 200)
		require.Equal(t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"))
		require.Empty(t, resp.Header.Get("X-Content-Type-Options"))
		body, _ := io.ReadAll(resp.Body)
		require.Contains(t, string(body), "<h1>hello</h1>")
	})

	t.Run("SiblingJsServedAsJavascript", func(t *testing.T) {
		user, id := createSiteGist(t, "0", map[string]string{
			"index.html": "<!DOCTYPE html><html><body></body></html>",
			"app.js":     "export const hi = 1;",
		})
		resp := s.Request(t, "GET", "/"+user+"/"+id+"/site/app.js", nil, 200)
		require.Equal(t, "application/javascript; charset=utf-8", resp.Header.Get("Content-Type"))
		require.Empty(t, resp.Header.Get("X-Content-Type-Options"))
		body, _ := io.ReadAll(resp.Body)
		require.Equal(t, "export const hi = 1;", string(body))
	})

	t.Run("SiblingCssServedAsCss", func(t *testing.T) {
		user, id := createSiteGist(t, "0", map[string]string{
			"index.html": "<!DOCTYPE html><html><body></body></html>",
			"style.css":  "body { color: red; }",
		})
		resp := s.Request(t, "GET", "/"+user+"/"+id+"/site/style.css", nil, 200)
		require.Equal(t, "text/css; charset=utf-8", resp.Header.Get("Content-Type"))
	})

	t.Run("NoHtmlFile404", func(t *testing.T) {
		user, id := createSiteGist(t, "0", map[string]string{
			"readme.txt": "plain text",
		})
		s.Request(t, "GET", "/"+user+"/"+id+"/site", nil, 404)
	})

	t.Run("FirstHtmlFileWins", func(t *testing.T) {
		user, id := createSiteGist(t, "0", map[string]string{
			"a.html": "<!DOCTYPE html><html><body>A</body></html>",
			"b.html": "<!DOCTYPE html><html><body>B</body></html>",
		})
		resp := s.Request(t, "GET", "/"+user+"/"+id+"/site", nil, 200)
		body, _ := io.ReadAll(resp.Body)
		require.Contains(t, string(body), "A")
		require.NotContains(t, string(body), "B")
	})

	t.Run("MissingSiblingFile404", func(t *testing.T) {
		user, id := createSiteGist(t, "0", map[string]string{
			"index.html": "<!DOCTYPE html><html><body></body></html>",
		})
		s.Request(t, "GET", "/"+user+"/"+id+"/site/missing.js", nil, 404)
	})

	t.Run("PrivateGistOwner", func(t *testing.T) {
		user, id := createSiteGist(t, "2", map[string]string{
			"index.html": "<!DOCTYPE html><html><body>private</body></html>",
		})
		s.Login(t, "alice")
		s.Request(t, "GET", "/"+user+"/"+id+"/site", nil, 404)
		s.Login(t, "thomas")
		s.Request(t, "GET", "/"+user+"/"+id+"/site", nil, 200)
		s.Logout()
	})

	t.Run("PrivateGistAccessToken", func(t *testing.T) {
		user, id := createSiteGist(t, "2", map[string]string{
			"index.html": "<!DOCTYPE html><html><body>private</body></html>",
		})
		owner, err := db.GetUserByUsername(user)
		require.NoError(t, err)
		tok := &db.AccessToken{Name: "site", UserID: owner.ID, ScopeGist: db.ReadPermission}
		plain, err := tok.GenerateToken()
		require.NoError(t, err)
		require.NoError(t, tok.Create())
		resp := s.RequestWithHeaders(t, "GET", "/"+user+"/"+id+"/site", nil, 200,
			map[string]string{"Authorization": "Token " + plain})
		require.Equal(t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"))
	})
}

func TestDownloadFile(t *testing.T) {
	s := webtest.Setup(t)
	defer webtest.Teardown(t)

	t.Run("ExistingFile", func(t *testing.T) {
		_, _, username, identifier := s.CreateGist(t, "0")

		resp := s.Request(t, "GET", "/"+username+"/"+identifier+"/download/HEAD/file.txt", nil, 200)

		require.Equal(t, "text/plain; charset=utf-8", resp.Header.Get("Content-Type"))
		require.Equal(t, `attachment; filename="file.txt"`, resp.Header.Get("Content-Disposition"))
		require.Equal(t, "11", resp.Header.Get("Content-Length"))
		require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "hello world", string(body))
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		_, _, username, identifier := s.CreateGist(t, "0")

		resp := s.Request(t, "GET", "/"+username+"/"+identifier+"/download/HEAD/nonexistent.txt", nil, 404)

		_, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		// TODO: change the response to not found
		// require.Equal(t, "File not found", string(body))
	})

	t.Run("NonExistentRevision", func(t *testing.T) {
		_, _, username, identifier := s.CreateGist(t, "0")

		resp := s.Request(t, "GET", "/"+username+"/"+identifier+"/download/zz/file.txt", nil, 404)

		_, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		// TODO: change the response to not found
		// require.Equal(t, "File not found", string(body))
	})

	t.Run("PrivateGist", func(t *testing.T) {
		_, _, username, identifier := s.CreateGist(t, "2")

		s.Request(t, "GET", "/"+username+"/"+identifier+"/download/HEAD/file.txt", nil, 404)
	})
}
