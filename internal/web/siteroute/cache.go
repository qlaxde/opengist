package siteroute

import (
	"sort"
	"sync/atomic"

	"github.com/thomiceli/opengist/internal/db"
)

// snapshot is an immutable view of the routing table, indexed by host. Within a
// host the slice is sorted descending by prefix length so Resolve can return the
// first (longest) match.
type snapshot struct {
	byHost map[string][]Match
}

// current holds the active snapshot. Reads are lock-free; Reload swaps a freshly
// built snapshot in atomically. A nil pointer (never reloaded / reload failed at
// boot) makes Resolve return false, i.e. site-routing is simply off.
var current atomic.Pointer[snapshot]

// Reload rebuilds the snapshot from the database and swaps it in. It is called
// once at boot and after every admin CRUD write. On error the previous snapshot
// is left in place and the error is returned to the caller for logging.
func Reload() error {
	routes, err := db.GetSiteRoutesForMatching()
	if err != nil {
		return err
	}

	byHost := make(map[string][]Match)
	for _, r := range routes {
		// Defensive: a route whose gist or owner vanished is skipped rather than
		// producing a broken rewrite target. CASCADE should prevent this.
		if r.Gist.ID == 0 || r.Gist.User.ID == 0 {
			continue
		}
		host := normalizeHost(r.Host)
		byHost[host] = append(byHost[host], Match{
			Host:     host,
			Prefix:   r.PathPrefix,
			User:     r.Gist.User.Username,
			Slug:     r.Gist.Identifier(),
			Revision: r.Revision,
		})
	}

	for host := range byHost {
		ms := byHost[host]
		sort.SliceStable(ms, func(i, j int) bool {
			return len(ms[i].Prefix) > len(ms[j].Prefix)
		})
		byHost[host] = ms
	}

	current.Store(&snapshot{byHost: byHost})
	return nil
}

// setSnapshotForTest replaces the active snapshot directly. Test-only helper so
// resolver tests need no database.
func setSnapshotForTest(matches []Match) {
	byHost := make(map[string][]Match)
	for _, m := range matches {
		h := normalizeHost(m.Host)
		m.Host = h
		byHost[h] = append(byHost[h], m)
	}
	for host := range byHost {
		ms := byHost[host]
		sort.SliceStable(ms, func(i, j int) bool {
			return len(ms[i].Prefix) > len(ms[j].Prefix)
		})
		byHost[host] = ms
	}
	current.Store(&snapshot{byHost: byHost})
}

// resetSnapshotForTest clears the snapshot back to the nil (off) state.
func resetSnapshotForTest() {
	current.Store(nil)
}
