package server

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/echo-contrib/echoprometheus"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog/log"
	"github.com/thomiceli/opengist/internal/auth"
	"github.com/thomiceli/opengist/internal/config"
	"github.com/thomiceli/opengist/internal/db"
	"github.com/thomiceli/opengist/internal/i18n"
	"github.com/thomiceli/opengist/internal/web/context"
	"github.com/thomiceli/opengist/internal/web/handlers"
	"github.com/thomiceli/opengist/internal/web/siteroute"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func (s *Server) useCustomContext() {
	s.echo.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cc := context.NewContext(c, filepath.Join(config.GetHomeDir(), "sessions"))
			return next(cc)
		}
	})
}

func (s *Server) registerMiddlewares() {
	s.echo.Use(Middleware(dataInit).toEcho())
	s.echo.Use(Middleware(locale).toEcho())
	if config.C.MetricsEnabled {
		s.echo.Use(echoprometheus.NewMiddleware("opengist"))
	}

	s.echo.Pre(middleware.MethodOverrideWithConfig(middleware.MethodOverrideConfig{
		Getter: middleware.MethodFromForm("_method"),
	}))
	s.echo.Pre(middleware.RemoveTrailingSlash())
	s.echo.Pre(hostSiteRewrite())
	s.echo.Pre(middleware.CORS())
	s.echo.Pre(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogURI: true, LogStatus: true, LogMethod: true,
		LogValuesFunc: func(ctx echo.Context, v middleware.RequestLoggerValues) error {
			log.Info().Str("uri", v.URI).Int("status", v.Status).Str("method", v.Method).
				Str("ip", ctx.RealIP()).TimeDiff("duration", time.Now(), v.StartTime).
				Msg("HTTP")
			return nil
		},
	}))
	s.echo.Use(middleware.Recover())
	s.echo.Use(middleware.Secure())
	s.echo.Use(Middleware(sessionInit).toEcho())
	s.echo.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup:    "form:_csrf,header:X-CSRF-Token",
		CookiePath:     "/",
		CookieHTTPOnly: true,
		CookieSameSite: http.SameSiteStrictMode,
		Skipper: func(ctx echo.Context) bool {
			/* skip CSRF for embeds */
			gistName := ctx.Param("gistname")

			/* skip CSRF for git clients */
			matchUploadPack, _ := regexp.MatchString("(.*?)/git-upload-pack$", ctx.Request().URL.Path)
			matchReceivePack, _ := regexp.MatchString("(.*?)/git-receive-pack$", ctx.Request().URL.Path)

			/* skip CSRF for the PAT-authenticated JSON API: these routes
			   carry no session cookie, so CSRF offers no protection and
			   only blocks legitimate token requests */
			matchApi := strings.HasPrefix(ctx.Request().URL.Path, "/api/") &&
				strings.HasPrefix(ctx.Request().Header.Get("Authorization"), "Token ")

			return (filepath.Ext(gistName) == ".js" && ctx.Request().Method == "GET") || matchUploadPack || matchReceivePack || matchApi
		},
		ErrorHandler: func(err error, c echo.Context) error {
			log.Info().Err(err).Msg("CSRF error")
			return err
		},
	}))
	s.echo.Use(Middleware(csrfInit).toEcho())

}

func (s *Server) errorHandler(err error, ctx echo.Context) {
	var httpErr *echo.HTTPError
	data := ctx.Request().Context().Value(context.DataKeyStr).(echo.Map)
	if errors.As(err, &httpErr) {
		acceptJson := strings.Contains(ctx.Request().Header.Get("Accept"), "application/json") ||
			strings.HasPrefix(ctx.Request().URL.Path, "/api/")
		data["error"] = err
		if acceptJson {
			if err := ctx.JSON(httpErr.Code, httpErr); err != nil {
				if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
					return
				}
				log.Fatal().Err(err).Send()
			}
			return
		}

		if err := ctx.Render(httpErr.Code, "error", data); err != nil {
			if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
				return
			}
			log.Fatal().Err(err).Send()
		}
		return
	}

	if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
		return
	}
	log.Error().Err(err).Send()
	httpErr = echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	data["error"] = httpErr
	if err := ctx.Render(500, "error", data); err != nil {
		if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
			return
		}
		log.Fatal().Err(err).Send()
	}
}

func dataInit(next Handler) Handler {
	return func(ctx *context.Context) error {
		ctx.SetData("loadStartTime", time.Now())

		if err := loadSettings(ctx); err != nil {
			return ctx.ErrorRes(500, "Cannot load settings", err)
		}

		ctx.SetData("c", config.C)

		ctx.SetData("githubOauth", config.C.GithubClientKey != "" && config.C.GithubSecret != "")
		ctx.SetData("gitlabOauth", config.C.GitlabClientKey != "" && config.C.GitlabSecret != "")
		ctx.SetData("giteaOauth", config.C.GiteaClientKey != "" && config.C.GiteaSecret != "")
		ctx.SetData("oidcOauth", config.C.OIDCClientKey != "" && config.C.OIDCSecret != "" && config.C.OIDCDiscoveryUrl != "")

		httpProtocol := "http"
		if ctx.Request().TLS != nil || ctx.Request().Header.Get("X-Forwarded-Proto") == "https" {
			httpProtocol = "https"
		}
		ctx.SetData("httpProtocol", strings.ToUpper(httpProtocol))

		var baseHttpUrl string
		// if a custom external url is set, use it
		if config.C.ExternalUrl != "" {
			baseHttpUrl = config.C.ExternalUrl
		} else {
			baseHttpUrl = httpProtocol + "://" + ctx.Request().Host
		}

		ctx.SetData("baseHttpUrl", baseHttpUrl)

		return next(ctx)
	}
}

func writePermission(next Handler) Handler {
	return func(ctx *context.Context) error {
		gist := ctx.GetData("gist").(*db.Gist)
		user := ctx.User
		if !gist.CanWrite(user) {
			return ctx.ErrorRes(403, "You don't have permission to edit this gist", nil)
		}
		return next(ctx)
	}
}

func adminPermission(next Handler) Handler {
	return func(ctx *context.Context) error {
		user := ctx.User
		if user == nil || !user.IsAdmin {
			return ctx.NotFound("User not found")
		}
		return next(ctx)
	}
}

// adminRequired gates instance-level API routes on admin privileges. Unlike
// adminPermission (which 404s to hide the admin panel from the browser), it
// returns a 403 since the caller is authenticated tooling. Use it after
// tokenAuthRequired, which populates ctx.User from the access token.
func adminRequired(next Handler) Handler {
	return func(ctx *context.Context) error {
		if ctx.User == nil || !ctx.User.IsAdmin {
			return ctx.ErrorRes(403, "admin privileges required", nil)
		}
		return next(ctx)
	}
}

func logged(next Handler) Handler {
	return func(ctx *context.Context) error {
		user := ctx.User
		if user != nil {
			return next(ctx)
		}
		return ctx.RedirectTo("/all")
	}
}

func inMFASession(next Handler) Handler {
	return func(ctx *context.Context) error {
		sess := ctx.GetSession()
		_, ok := sess.Values["mfaID"].(uint)
		if !ok {
			return ctx.ErrorRes(400, ctx.Tr("error.not-in-mfa-session"), nil)
		}
		return next(ctx)
	}
}

func inOAuthRegisterSession(next Handler) Handler {
	return func(ctx *context.Context) error {
		sess := ctx.GetSession()
		_, ok := sess.Values["oauthProvider"].(string)
		if !ok {
			return ctx.RedirectTo("/login")
		}
		return next(ctx)
	}
}

func makeCheckRequireLogin(isSingleGistAccess bool) Middleware {
	return func(next Handler) Handler {
		return func(ctx *context.Context) error {
			if user := ctx.User; user != nil {
				return next(ctx)
			}

			if getUserByToken(ctx) != nil {
				return next(ctx)
			}
			allow, err := auth.ShouldAllowUnauthenticatedGistAccess(handlers.ContextAuthInfo{Context: ctx}, isSingleGistAccess)
			if err != nil {
				log.Fatal().Err(err).Msg("Failed to check if unauthenticated access is allowed")
			}

			if !allow {
				ctx.AddFlash(ctx.Tr("flash.auth.must-be-logged-in"), "error")
				return ctx.RedirectTo("/login")
			}
			return next(ctx)
		}
	}
}

func checkRequireLogin(next Handler) Handler {
	return makeCheckRequireLogin(false)(next)
}

// hostSiteRewrite is a Pre middleware that maps an external host (+ optional
// path prefix) onto the existing /<user>/<gist>/site route, using the
// admin-managed site routing table. It rewrites the request path in place; the
// regular gistAnonymousGate → gistInit → GistSite chain then serves it, so the
// public_site visibility gate enforces auth for free and write routes (/edit,
// etc.) are unreachable by construction (the rewrite always injects /site/).
//
// It runs on a raw echo.Context (Pre runs before the *context.Context wrapper
// and before routing); the matched-route signals are stashed with c.Set, which
// the wrapped context exposes via ctx.Get in GistSite.
func hostSiteRewrite() echo.MiddlewareFunc {
	extHost := externalUrlHost()

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			host := stripPort(req.Host)

			// Never shadow the canonical instance host: the admin panel, login,
			// and the normal gist UI must keep working there.
			if extHost != "" && host == extHost {
				return next(c)
			}

			m, ok := siteroute.Resolve(host, req.URL.Path)
			if !ok {
				return next(c)
			}

			rest := strings.TrimPrefix(req.URL.Path, m.Prefix)
			rest = strings.TrimPrefix(rest, "/")

			target := "/" + m.User + "/" + m.Slug + "/site"
			if m.Revision != "" {
				target += "/@" + m.Revision
			}
			if rest != "" {
				target += "/" + rest
			}

			c.Set("siteRouteMatched", true)
			c.Set("siteRoutePrefix", m.Prefix)
			req.URL.Path = target
			req.RequestURI = target

			return next(c)
		}
	}
}

// externalUrlHost returns the lowercased hostname (no port) of the configured
// canonical ExternalUrl, or "" if unset/unparseable.
func externalUrlHost() string {
	if config.C.ExternalUrl == "" {
		return ""
	}
	u, err := url.Parse(config.C.ExternalUrl)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func stripPort(host string) string {
	host = strings.ToLower(host)
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return host
}

// gistAnonymousGate runs before gistInit and decides whether an unauthenticated
// visitor may reach a per-gist route. It honors the gist's own anonymous-exposure
// visibility:
//
//   - PublicVisibility (3): any route is reachable without authentication.
//   - PublicSiteVisibility (4): only the /site routes are reachable without
//     authentication; the source page, raw and downloads still require login.
//
// For every other visibility it falls back to the global require-login policy
// (ShouldAllowUnauthenticatedGistAccess), preserving upstream behavior — in
// particular private gists keep redirecting to /login rather than being loaded.
// Authenticated requests (session or token) pass straight through to gistInit,
// which still enforces the per-gist owner/private checks.
func gistAnonymousGate(next Handler) Handler {
	return func(ctx *context.Context) error {
		if ctx.User != nil || getUserByToken(ctx) != nil {
			return next(ctx)
		}

		if gist, err := db.GetGist(ctx.Param("user"), gistNameFromParam(ctx)); err == nil && gist != nil {
			if gist.Private == db.PublicVisibility {
				return next(ctx)
			}
			if gist.Private == db.PublicSiteVisibility && isSiteRoute(ctx) {
				return next(ctx)
			}
		}

		allow, err := auth.ShouldAllowUnauthenticatedGistAccess(handlers.ContextAuthInfo{Context: ctx}, true)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to check if unauthenticated access is allowed")
		}
		if !allow {
			ctx.AddFlash(ctx.Tr("flash.auth.must-be-logged-in"), "error")
			return ctx.RedirectTo("/login")
		}
		return next(ctx)
	}
}

// gistNameFromParam strips the extension dispatched by gistInit (.js/.json/.git)
// from the :gistname route param so the gate looks up the same gist gistInit will.
func gistNameFromParam(ctx *context.Context) string {
	gistName := ctx.Param("gistname")
	switch filepath.Ext(gistName) {
	case ".js", ".json", ".git":
		gistName = strings.TrimSuffix(gistName, filepath.Ext(gistName))
	}
	return gistName
}

// isSiteRoute reports whether the matched route is one of the /site routes,
// using the registered route template (not the raw URL) so a gist literally
// named "site" cannot be confused for the site endpoint.
func isSiteRoute(ctx *context.Context) bool {
	p := ctx.Path()
	return strings.HasSuffix(p, "/site") || strings.HasSuffix(p, "/site/*")
}

func noRouteFound(ctx *context.Context) error {
	return ctx.NotFound("Page not found")
}

func locale(next Handler) Handler {
	return func(ctx *context.Context) error {
		// Check URL arguments
		lang := ctx.Request().URL.Query().Get("lang")
		changeLang := lang != ""

		// Then check cookies
		if len(lang) == 0 {
			cookie, _ := ctx.Request().Cookie("lang")
			if cookie != nil {
				lang = cookie.Value
			}
		}

		// Check again in case someone changes the supported language list.
		if lang != "" && !i18n.Locales.HasLocale(lang) {
			lang = ""
			changeLang = false
		}

		// 3.Then check from 'Accept-Language' header.
		if len(lang) == 0 {
			tags, _, _ := language.ParseAcceptLanguage(ctx.Request().Header.Get("Accept-Language"))
			lang = i18n.Locales.MatchTag(tags)
		}

		if changeLang {
			ctx.SetCookie(&http.Cookie{Name: "lang", Value: lang, Path: "/", MaxAge: 1<<31 - 1})
		}

		localeUsed, err := i18n.Locales.GetLocale(lang)
		if err != nil {
			return ctx.ErrorRes(500, "Cannot get locale", err)
		}

		ctx.SetData("localeName", localeUsed.Name)
		ctx.SetData("locale", localeUsed)
		ctx.SetData("allLocales", i18n.Locales.Locales)

		return next(ctx)
	}
}

func sessionInit(next Handler) Handler {
	return func(ctx *context.Context) error {
		sess := ctx.GetSession()
		if sess.Values["user"] != nil {
			var err error
			var user *db.User

			if user, err = db.GetUserById(sess.Values["user"].(uint)); err != nil {
				sess.Values["user"] = nil
				ctx.SaveSession(sess)
				ctx.User = nil
				ctx.SetData("userLogged", nil)
				return ctx.RedirectTo("/all")
			}
			if user != nil {
				ctx.User = user
				ctx.SetData("userLogged", user)
				ctx.SetData("currentStyle", user.GetStyle())
			}
			return next(ctx)
		}

		ctx.User = nil
		ctx.SetData("userLogged", nil)
		return next(ctx)
	}
}

func csrfInit(next Handler) Handler {
	return func(ctx *context.Context) error {
		var csrf string
		if csrfToken, ok := ctx.Get("csrf").(string); ok {
			csrf = csrfToken
		}
		ctx.SetData("csrfHtml", template.HTML(`<input type="hidden" name="_csrf" value="`+csrf+`">`))

		return next(ctx)
	}
}

func loadSettings(ctx *context.Context) error {
	settings, err := db.GetSettings()
	if err != nil {
		return err
	}

	for key, value := range settings {
		s := strings.ReplaceAll(key, "-", " ")
		s = cases.Title(language.English).String(s)
		ctx.SetData(strings.ReplaceAll(s, " ", ""), value == "1")
	}
	return nil
}

// tokenAuthRequired authenticates the request via a PAT in the Authorization
// header (`Token <pat>`) and sets ctx.User accordingly. Used by the JSON API
// surface where session/cookie auth is impractical. write=true requires the
// token to have gist-write scope, otherwise read scope is sufficient.
func tokenAuthRequired(write bool) Middleware {
	return func(next Handler) Handler {
		return func(ctx *context.Context) error {
			authHeader := ctx.Request().Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, "Token ") {
				return ctx.ErrorRes(401, "missing Token authorization", nil)
			}
			plainToken := strings.TrimPrefix(authHeader, "Token ")
			tok, err := db.GetAccessTokenByToken(plainToken)
			if err != nil || tok.IsExpired() {
				return ctx.ErrorRes(401, "invalid or expired token", err)
			}
			if write {
				if !tok.HasGistWritePermission() {
					return ctx.ErrorRes(403, "token lacks gist write scope", nil)
				}
			} else if !tok.HasGistReadPermission() {
				return ctx.ErrorRes(403, "token lacks gist read scope", nil)
			}
			_ = tok.UpdateLastUsed()
			ctx.User = &tok.User
			ctx.SetData("userLogged", &tok.User)
			return next(ctx)
		}
	}
}

// getUserByToken checks the Authorization header for token-based auth.
// Expects format: Authorization: Token <token>
// Returns the user if the token is valid and has gist read permission, nil otherwise.
func getUserByToken(ctx *context.Context) *db.User {
	authHeader := ctx.Request().Header.Get("Authorization")
	if authHeader == "" {
		return nil
	}

	if !strings.HasPrefix(authHeader, "Token ") {
		return nil
	}

	plainToken := strings.TrimPrefix(authHeader, "Token ")

	accessToken, err := db.GetAccessTokenByToken(plainToken)
	if err != nil {
		return nil
	}

	if accessToken.IsExpired() {
		return nil
	}

	if !accessToken.HasGistReadPermission() {
		return nil
	}

	_ = accessToken.UpdateLastUsed()

	return &accessToken.User
}

func gistInit(next Handler) Handler {
	return func(ctx *context.Context) error {
		currUser := ctx.User

		userName := ctx.Param("user")
		gistName := ctx.Param("gistname")

		switch filepath.Ext(gistName) {
		case ".js":
			ctx.SetData("gistpage", "js")
			gistName = strings.TrimSuffix(gistName, ".js")
		case ".json":
			ctx.SetData("gistpage", "json")
			gistName = strings.TrimSuffix(gistName, ".json")
		case ".git":
			ctx.SetData("gistpage", "git")
			gistName = strings.TrimSuffix(gistName, ".git")
		}

		gist, err := db.GetGist(userName, gistName)
		if err != nil {
			return ctx.NotFound("Gist not found")
		}

		if gist.Private == db.PrivateVisibility {
			if currUser == nil || currUser.ID != gist.UserID {
				// Check for token-based auth via Authorization header
				if tokenUser := getUserByToken(ctx); tokenUser != nil && tokenUser.ID == gist.UserID {
					// Token is valid and belongs to gist owner, allow access
				} else {
					return ctx.NotFound("Gist not found")
				}
			}
		}

		ctx.SetData("gist", gist)

		if config.C.SshGit {
			var sshDomain string

			if config.C.SshExternalDomain != "" {
				sshDomain = config.C.SshExternalDomain
			} else {
				sshDomain = strings.Split(ctx.Request().Host, ":")[0]
			}

			if config.C.SshPort == "22" {
				ctx.SetData("sshCloneUrl", sshDomain+":"+userName+"/"+gistName+".git")
			} else {
				ctx.SetData("sshCloneUrl", "ssh://"+sshDomain+":"+config.C.SshPort+"/"+userName+"/"+gistName+".git")
			}
		}

		baseHttpUrl := ctx.GetData("baseHttpUrl").(string)

		if config.C.HttpGit {
			ctx.SetData("httpCloneUrl", baseHttpUrl+"/"+userName+"/"+gistName+".git")
		}

		ctx.SetData("httpCopyUrl", baseHttpUrl+"/"+userName+"/"+gistName)
		ctx.SetData("currentUrl", template.URL(ctx.Request().URL.Path))
		ctx.SetData("embedScript", fmt.Sprintf(`<script src="%s"></script>`, baseHttpUrl+"/"+userName+"/"+gistName+".js"))

		nbCommits, err := gist.NbCommits()
		if err != nil {
			return ctx.ErrorRes(500, "Error fetching number of commits", err)
		}
		ctx.SetData("nbCommits", nbCommits)

		if currUser != nil {
			hasLiked, err := currUser.HasLiked(gist)
			if err != nil {
				return ctx.ErrorRes(500, "Cannot get user like status", err)
			}
			ctx.SetData("hasLiked", hasLiked)
		}

		if gist.Private > 0 {
			ctx.SetData("NoIndex", true)
		}

		return next(ctx)
	}
}

// gistSoftInit try to load a gist (same as gistInit) but does not return a 404 if the gist is not found
// useful for git clients using HTTP to obfuscate the existence of a private gist
func gistSoftInit(next Handler) Handler {
	return func(ctx *context.Context) error {
		userName := ctx.Param("user")
		gistName := ctx.Param("gistname")

		gistName = strings.TrimSuffix(gistName, ".git")

		gist, _ := db.GetGist(userName, gistName)
		ctx.SetData("gist", gist)

		return next(ctx)
	}
}

// gistNewPushSoftInit has the same behavior as gistSoftInit but create a new gist empty instead
func gistNewPushSoftInit(next Handler) Handler {
	return func(ctx *context.Context) error {
		ctx.SetData("gist", new(db.Gist))
		return next(ctx)
	}
}
func setAllGistsMode(mode string) Middleware {
	return func(next Handler) Handler {
		return func(ctx *context.Context) error {
			ctx.SetData("mode", mode)
			return next(ctx)
		}
	}
}
