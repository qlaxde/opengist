package siteroute

import "testing"

func TestResolveNilSnapshot(t *testing.T) {
	resetSnapshotForTest()
	if _, ok := Resolve("qlax.de", "/"); ok {
		t.Fatal("expected no match with nil snapshot")
	}
}

func TestResolveLongestPrefixWins(t *testing.T) {
	setSnapshotForTest([]Match{
		{Host: "qlax.de", Prefix: "", User: "pk", Slug: "root"},
		{Host: "qlax.de", Prefix: "/faq", User: "pk", Slug: "faq"},
		{Host: "qlax.de", Prefix: "/faq/legal", User: "pk", Slug: "legal"},
	})
	defer resetSnapshotForTest()

	cases := []struct {
		path     string
		wantSlug string
	}{
		{"/", "root"},
		{"/about", "root"},
		{"/faq", "faq"},
		{"/faq/q1", "faq"},
		{"/faq/legal", "legal"},
		{"/faq/legal/gdpr", "legal"},
	}
	for _, c := range cases {
		m, ok := Resolve("qlax.de", c.path)
		if !ok {
			t.Errorf("%s: expected match", c.path)
			continue
		}
		if m.Slug != c.wantSlug {
			t.Errorf("%s: got slug %q want %q", c.path, m.Slug, c.wantSlug)
		}
	}
}

func TestResolveSegmentBoundary(t *testing.T) {
	setSnapshotForTest([]Match{
		{Host: "qlax.de", Prefix: "/faq", User: "pk", Slug: "faq"},
	})
	defer resetSnapshotForTest()

	// "/faqs" must NOT match "/faq" (no root fallback here).
	if _, ok := Resolve("qlax.de", "/faqs"); ok {
		t.Error("/faqs should not match prefix /faq")
	}
	if _, ok := Resolve("qlax.de", "/faq"); !ok {
		t.Error("/faq should match prefix /faq")
	}
	if _, ok := Resolve("qlax.de", "/faq/x"); !ok {
		t.Error("/faq/x should match prefix /faq")
	}
}

func TestResolveRootCatchAll(t *testing.T) {
	setSnapshotForTest([]Match{
		{Host: "qlax.de", Prefix: "", User: "pk", Slug: "root"},
	})
	defer resetSnapshotForTest()

	if m, ok := Resolve("qlax.de", "/anything/at/all"); !ok || m.Slug != "root" {
		t.Errorf("root catch-all failed: ok=%v slug=%q", ok, m.Slug)
	}
}

func TestResolveHostIsolationAndPortStrip(t *testing.T) {
	setSnapshotForTest([]Match{
		{Host: "qlax.de", Prefix: "", User: "pk", Slug: "a"},
		{Host: "other.de", Prefix: "", User: "x", Slug: "b"},
	})
	defer resetSnapshotForTest()

	if m, ok := Resolve("QLAX.DE:8080", "/"); !ok || m.Slug != "a" {
		t.Errorf("host normalize/port strip failed: ok=%v slug=%q", ok, m.Slug)
	}
	if m, ok := Resolve("other.de", "/"); !ok || m.Slug != "b" {
		t.Errorf("host isolation failed: ok=%v slug=%q", ok, m.Slug)
	}
	if _, ok := Resolve("unknown.de", "/"); ok {
		t.Error("unknown host should not match")
	}
}
