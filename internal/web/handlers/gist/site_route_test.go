package gist_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thomiceli/opengist/internal/db"
	"github.com/thomiceli/opengist/internal/web/siteroute"
	webtest "github.com/thomiceli/opengist/internal/web/test"
)

func readBody(t *testing.T, r *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	return string(b)
}

// apiReq issues a PAT-authenticated JSON request (no session cookie).
func apiReq(t *testing.T, s *webtest.Server, method, path, token, jsonBody string, expected int) *http.Response {
	t.Helper()
	var body io.Reader
	if jsonBody != "" {
		body = strings.NewReader(jsonBody)
	}
	req := httptest.NewRequest(method, path, body)
	if token != "" {
		req.Header.Set("Authorization", "Token "+token)
	}
	if jsonBody != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	return s.RawRequest(t, req, expected)
}

// mintToken creates a gist read+write access token for the named user.
func mintToken(t *testing.T, username string) string {
	t.Helper()
	user, err := db.GetUserByUsername(username)
	require.NoError(t, err)
	tok := &db.AccessToken{Name: "test", UserID: user.ID, ScopeGist: 2}
	plain, err := tok.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, tok.Create())
	return plain
}

func TestSiteRoutesAPI(t *testing.T) {
	s := webtest.Setup(t)
	defer webtest.Teardown(t)

	s.Register(t, "thomas") // first user is admin

	// public_site gist to target.
	s.Login(t, "thomas")
	resp := s.Request(t, "POST", "/", url.Values{
		"title":   {"Site"},
		"name":    {"index.html"},
		"content": {"<!DOCTYPE html><html><head></head><body><h1>api</h1></body></html>"},
		"private": {"4"},
	}, 302)
	parts := strings.Split(strings.TrimPrefix(resp.Header.Get("Location"), "/"), "/")
	user, siteId := parts[0], parts[1]

	// Internal gist (no anonymous access) to reject.
	resp = s.Request(t, "POST", "/", url.Values{
		"title": {"Internal"}, "name": {"index.html"},
		"content": {"<html></html>"}, "private": {"0"},
	}, 302)
	internalId := strings.Split(strings.TrimPrefix(resp.Header.Get("Location"), "/"), "/")[1]
	s.Logout()

	adminToken := mintToken(t, "thomas")

	t.Run("RequiresToken", func(t *testing.T) {
		apiReq(t, s, "GET", "/api/site-routes", "", "", 401)
	})

	t.Run("NonAdminForbidden", func(t *testing.T) {
		s.Request(t, "POST", "/register", db.UserDTO{Username: "bob", Password: "bobbob12"}, 302)
		s.Logout()
		bobToken := mintToken(t, "bob")
		apiReq(t, s, "GET", "/api/site-routes", bobToken, "", 403)
		apiReq(t, s, "POST", "/api/site-routes", bobToken,
			`{"host":"bob.test","user":"`+user+`","slug":"`+siteId+`"}`, 403)
	})

	t.Run("RejectsNonPublicGist", func(t *testing.T) {
		apiReq(t, s, "POST", "/api/site-routes", adminToken,
			`{"host":"reject.test","user":"`+user+`","slug":"`+internalId+`"}`, 422)
	})

	t.Run("RejectsBadHost", func(t *testing.T) {
		apiReq(t, s, "POST", "/api/site-routes", adminToken,
			`{"host":"http://nope/","user":"`+user+`","slug":"`+siteId+`"}`, 400)
	})

	var createdID float64
	t.Run("CreateThenResolve", func(t *testing.T) {
		r := apiReq(t, s, "POST", "/api/site-routes", adminToken,
			`{"host":"api.test","path_prefix":"/docs","user":"`+user+`","slug":"`+siteId+`","revision":""}`, 201)
		var out map[string]any
		require.NoError(t, json.Unmarshal([]byte(readBody(t, r)), &out))
		require.Equal(t, "api.test", out["host"])
		require.Equal(t, "/docs", out["path_prefix"])
		require.Equal(t, true, out["enabled"])
		createdID = out["id"].(float64)

		// The resolver picked up the new route (Reload ran).
		m, ok := siteroute.Resolve("api.test", "/docs/x")
		require.True(t, ok)
		require.Equal(t, siteId, m.Slug)
	})

	t.Run("DuplicateConflicts", func(t *testing.T) {
		apiReq(t, s, "POST", "/api/site-routes", adminToken,
			`{"host":"api.test","path_prefix":"/docs","user":"`+user+`","slug":"`+siteId+`"}`, 409)
	})

	t.Run("ListIncludesRoute", func(t *testing.T) {
		r := apiReq(t, s, "GET", "/api/site-routes", adminToken, "", 200)
		require.Contains(t, readBody(t, r), "api.test")
	})

	t.Run("DeleteRemovesRoute", func(t *testing.T) {
		id := strconv.Itoa(int(createdID))
		apiReq(t, s, "DELETE", "/api/site-routes/"+id, adminToken, "", 200)
		if _, ok := siteroute.Resolve("api.test", "/docs/x"); ok {
			t.Fatal("route should be gone after delete")
		}
	})
}

// forgedGet issues an anonymous GET with a forged Host header, simulating a
// request arriving at an external site-routed host. httptest only honors
// req.Host (not the Host header), so it is set directly.
func forgedGet(t *testing.T, s *webtest.Server, host, path string, expected int) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	return s.RawRequest(t, req, expected)
}

func TestSiteRoutes(t *testing.T) {
	s := webtest.Setup(t)
	defer webtest.Teardown(t)

	s.Register(t, "thomas") // first user is admin

	indexHTML := "<!DOCTYPE html><html><head></head><body><h1>home</h1></body></html>"
	aboutHTML := "<!DOCTYPE html><html><head></head><body><h1>about</h1></body></html>"

	// Create a public_site gist with a few sibling files for fallback coverage.
	s.Login(t, "thomas")
	resp := s.Request(t, "POST", "/", url.Values{
		"title":   {"Site"},
		"name":    {"index.html", "about.html", "app.js"},
		"content": {indexHTML, aboutHTML, "console.log('hi')"},
		"private": {"4"}, // PublicSiteVisibility
	}, 302)
	parts := strings.Split(strings.TrimPrefix(resp.Header.Get("Location"), "/"), "/")
	require.Len(t, parts, 2)
	user, siteId := parts[0], parts[1]

	// Create an Internal gist (no anonymous access) for the bypass regression.
	resp = s.Request(t, "POST", "/", url.Values{
		"title":   {"Internal"},
		"name":    {"index.html"},
		"content": {indexHTML},
		"private": {"0"}, // InternalVisibility
	}, 302)
	parts = strings.Split(strings.TrimPrefix(resp.Header.Get("Location"), "/"), "/")
	require.Len(t, parts, 2)
	internalId := parts[1]
	internalGist, err := db.GetGist(user, internalId)
	require.NoError(t, err)

	headHash := func(id string) string {
		g, err := db.GetGist(user, id)
		require.NoError(t, err)
		commits, err := g.Log(0)
		require.NoError(t, err)
		require.NotEmpty(t, commits)
		return commits[0].Hash
	}
	siteRev := headHash(siteId)

	// Routes for the public_site gist, created through the admin endpoint (which
	// validates visibility and reloads the snapshot).
	createRoute := func(host, prefix, revision string) {
		s.Login(t, "thomas")
		s.Request(t, "POST", "/admin-panel/site-routes", url.Values{
			"host":       {host},
			"pathPrefix": {prefix},
			"user":       {user},
			"slug":       {siteId},
			"revision":   {revision},
			"enabled":    {"true"},
		}, 302)
		s.Logout()
	}
	createRoute("site.test", "", "")
	createRoute("faq.test", "/faq", "")
	createRoute("pinned.test", "", siteRev)
	createRoute("missing.test", "", "deadbeef") // valid hex, nonexistent commit

	t.Run("AdminListPageRenders", func(t *testing.T) {
		s.Login(t, "thomas")
		r := s.Request(t, "GET", "/admin-panel/site-routes", nil, 200)
		require.Contains(t, readBody(t, r), "site.test")
		s.Logout()
	})

	// A route may only be created against a public/public-site gist.
	t.Run("AdminRejectsNonPublicGist", func(t *testing.T) {
		s.Login(t, "thomas")
		s.Request(t, "POST", "/admin-panel/site-routes", url.Values{
			"host": {"reject.test"}, "pathPrefix": {""},
			"user": {user}, "slug": {internalId}, "revision": {""}, "enabled": {"true"},
		}, 302)
		s.Logout()
		// No route was created for this host, so it does not resolve.
		if _, ok := siteroute.Resolve("reject.test", "/"); ok {
			t.Fatal("admin should not have created a route for an Internal gist")
		}
	})

	// Force require-login so only per-gist anonymous exposure can bypass it; this
	// makes the auth-bypass regression meaningful.
	s.Login(t, "thomas")
	s.Request(t, "PUT", "/admin-panel/set-config", url.Values{"key": {db.SettingRequireLogin}, "value": {"1"}}, 200)
	defer func() {
		s.Login(t, "thomas")
		s.Request(t, "PUT", "/admin-panel/set-config", url.Values{"key": {db.SettingRequireLogin}, "value": {"0"}}, 200)
		s.Logout()
	}()
	s.Logout()

	t.Run("RootServesIndexWithRootBaseHref", func(t *testing.T) {
		r := forgedGet(t, s, "site.test", "/", 200)
		body := readBody(t, r)
		require.Contains(t, body, "<h1>home</h1>")
		require.Contains(t, body, `<base href="/">`)
	})

	t.Run("PrettyUrlFallbackToHtml", func(t *testing.T) {
		r := forgedGet(t, s, "site.test", "/about", 200)
		require.Contains(t, readBody(t, r), "<h1>about</h1>")
	})

	t.Run("AssetKeepsContentType", func(t *testing.T) {
		r := forgedGet(t, s, "site.test", "/app.js", 200)
		require.Contains(t, r.Header.Get("Content-Type"), "application/javascript")
	})

	t.Run("MissingAssetIsCleanNotFound", func(t *testing.T) {
		// has an extension -> no .html probe -> plain 404
		forgedGet(t, s, "site.test", "/styles.css", 404)
	})

	t.Run("PrefixRouteRewriteAndBaseHref", func(t *testing.T) {
		r := forgedGet(t, s, "faq.test", "/faq", 200)
		body := readBody(t, r)
		require.Contains(t, body, "<h1>home</h1>")
		require.Contains(t, body, `<base href="/faq/">`)

		r = forgedGet(t, s, "faq.test", "/faq/about", 200)
		require.Contains(t, readBody(t, r), "<h1>about</h1>")

		// A sibling path not under the prefix does not resolve as a site route;
		// it falls through to normal routing (which, with require-login on,
		// redirects to /login rather than serving the site).
		resp := forgedGet(t, s, "faq.test", "/other", 302)
		require.Contains(t, resp.Header.Get("Location"), "/login")
	})

	t.Run("PinnedRevisionIsImmutable", func(t *testing.T) {
		r := forgedGet(t, s, "pinned.test", "/", 200)
		require.Contains(t, r.Header.Get("Cache-Control"), "immutable")
	})

	t.Run("PinnedRevisionMissingIs404", func(t *testing.T) {
		forgedGet(t, s, "missing.test", "/", 404)
	})

	// MANDATORY auth-bypass regression: a route pointing at a gist that does not
	// allow anonymous access must NOT serve anonymously, even though the route
	// exists (visibility is authoritative at serve time). Insert the route
	// directly to model a gist whose visibility dropped after route creation.
	t.Run("AuthBypassRegression", func(t *testing.T) {
		route := &db.SiteRoute{
			Host:       "internal.test",
			PathPrefix: "",
			GistID:     internalGist.ID,
			Enabled:    true,
		}
		require.NoError(t, route.Create())
		require.NoError(t, siteroute.Reload())

		// Sanity: the resolver does map the host...
		if _, ok := siteroute.Resolve("internal.test", "/"); !ok {
			t.Fatal("expected internal.test to resolve")
		}
		// ...but the serve-time gate must refuse anonymous access.
		r := forgedGet(t, s, "internal.test", "/", 302)
		require.Contains(t, r.Header.Get("Location"), "/login")
	})

	// The canonical instance host must never be shadowed by site routing, so the
	// admin panel and normal gist UI keep working there. ExternalUrl is unset in
	// tests, so simulate by confirming the normal /<user>/<gist> path still 404s
	// for a bogus gist rather than being rewritten.
	t.Run("NormalGistPathUnaffected", func(t *testing.T) {
		// Unknown host with no route -> no rewrite -> normal gist routing. With
		// require-login on the anonymous gate redirects to /login (302), proving
		// the request was NOT rewritten into a /site route.
		resp := forgedGet(t, s, "unmapped.test", "/thomas/nonexistent", 302)
		require.Contains(t, resp.Header.Get("Location"), "/login")
	})
}
