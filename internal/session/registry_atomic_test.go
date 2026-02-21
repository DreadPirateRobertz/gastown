package session

import "testing"

func TestDefaultRegistrySwapAndPrefixFor(t *testing.T) {
	old := DefaultRegistry()
	defer SetDefaultRegistry(old)

	r := NewPrefixRegistry()
	r.Register("xy", "xrig")
	SetDefaultRegistry(r)

	if got := DefaultRegistry(); got != r {
		t.Fatalf("DefaultRegistry() did not return swapped registry")
	}
	if got := PrefixFor("xrig"); got != "xy" {
		t.Fatalf("PrefixFor(xrig) = %q, want %q", got, "xy")
	}
	if got := PrefixFor("unknown-rig"); got != DefaultPrefix {
		t.Fatalf("PrefixFor(unknown-rig) = %q, want %q", got, DefaultPrefix)
	}
}

func TestIsKnownSession_UsesDefaultRegistryAndHQPrefix(t *testing.T) {
	old := DefaultRegistry()
	defer SetDefaultRegistry(old)

	r := NewPrefixRegistry()
	r.Register("xy", "xrig")
	SetDefaultRegistry(r)

	if !IsKnownSession("hq-mayor") {
		t.Fatal("expected hq-mayor to always be known")
	}
	if !IsKnownSession("xy-worker") {
		t.Fatal("expected xy-worker to be known via registry prefix")
	}
	if IsKnownSession("zz-worker") {
		t.Fatal("expected zz-worker to be unknown")
	}
}
