// Package siteroute resolves an external (host, path) pair onto the rendered
// /site of a gist revision, using the admin-managed db.SiteRoute table.
//
// It is a leaf package importable by both internal/web/server (the rewrite
// middleware) and internal/web/handlers/gist (base-href awareness) without
// creating an import cycle.
package siteroute

import "strings"

// Match is a resolved route. Slug + User build the rewrite target
// (/{User}/{Slug}/site...), which db.GetGist resolves by uuid/url_normalized;
// it is therefore the gist's current Identifier(), not its numeric ID.
type Match struct {
	Host     string
	Prefix   string // "" = root catch-all, else "/faq"
	User     string
	Slug     string // gist Identifier() (url slug or uuid)
	Revision string // "" = HEAD
}

// matchPrefix reports whether route prefix p matches request path r on a
// segment boundary: p=="" (root) OR r==p OR r starts with p+"/". The trailing
// slash makes "/faq" not match "/faqs".
func matchPrefix(p, r string) bool {
	if p == "" {
		return true
	}
	return r == p || strings.HasPrefix(r, p+"/")
}

// normalizeHost lowercases and strips any :port from a request host.
func normalizeHost(host string) string {
	host = strings.ToLower(host)
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return host
}

// Resolve returns the best (longest-prefix) route for the given host and path.
// It reads the lock-free snapshot; if no snapshot is loaded it returns
// (Match{}, false) so the app behaves exactly as if site-routing were off.
func Resolve(host, urlPath string) (Match, bool) {
	snap := current.Load()
	if snap == nil {
		return Match{}, false
	}

	candidates, ok := snap.byHost[normalizeHost(host)]
	if !ok {
		return Match{}, false
	}

	// candidates are sorted descending by prefix length, so the first match is
	// the longest one; root ("") sorts last and acts as catch-all.
	for _, m := range candidates {
		if matchPrefix(m.Prefix, urlPath) {
			return m, true
		}
	}
	return Match{}, false
}
