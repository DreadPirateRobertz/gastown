package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// setupShowTestTown creates a minimal Gas Town structure for testing
// resolveBeadDir routing. No Dolt required — just filesystem layout
// with routes.jsonl and .beads directories.
func setupShowTestTown(t *testing.T) string {
	t.Helper()

	// Resolve symlinks (macOS /var -> /private/var) so path comparisons match
	townRoot, _ := filepath.EvalSymlinks(t.TempDir())

	// Create mayor/town.json so FindTownRoot() detects this as a Gas Town root
	mayorDir := filepath.Join(townRoot, "mayor")
	if err := os.MkdirAll(mayorDir, 0755); err != nil {
		t.Fatalf("mkdir mayor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mayorDir, "town.json"), []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatalf("write town.json: %v", err)
	}

	// Create town-level .beads with routes
	townBeadsDir := filepath.Join(townRoot, ".beads")
	if err := os.MkdirAll(townBeadsDir, 0755); err != nil {
		t.Fatalf("mkdir town .beads: %v", err)
	}

	routes := []beads.Route{
		{Prefix: "hq-", Path: "."},
		{Prefix: "gt-", Path: "gastown/mayor/rig"},
		{Prefix: "mp-", Path: "myproject/mayor/rig"},
	}
	if err := beads.WriteRoutes(townBeadsDir, routes); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	// Create rig directories with .beads
	for _, rig := range []string{"gastown/mayor/rig", "myproject/mayor/rig"} {
		rigPath := filepath.Join(townRoot, rig)
		if err := os.MkdirAll(filepath.Join(rigPath, ".beads"), 0755); err != nil {
			t.Fatalf("mkdir %s/.beads: %v", rig, err)
		}
	}

	return townRoot
}

// TestResolveBeadDirForShow verifies that resolveBeadDir routes rig-prefixed
// bead IDs to the correct rig directory. This is the fix for GH#2126:
// gt bead show / gt sling couldn't resolve rig-prefixed beads because
// gt show didn't set the working directory before exec'ing bd.
func TestResolveBeadDirForShow(t *testing.T) {
	townRoot := setupShowTestTown(t)

	// Override cwd to the town root so workspace.FindFromCwd() finds it
	origDir, _ := os.Getwd()
	if err := os.Chdir(townRoot); err != nil {
		t.Fatalf("chdir to town root: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	tests := []struct {
		beadID   string
		wantPath string // relative to townRoot, or "." for town root itself
	}{
		// Town-level beads should resolve to town root
		{"hq-abc123", "."},

		// Rig-level beads should resolve to rig directory
		{"gt-abc123", "gastown/mayor/rig"},
		{"mp-xyz789", "myproject/mayor/rig"},
	}

	for _, tc := range tests {
		t.Run(tc.beadID, func(t *testing.T) {
			dir := resolveBeadDir(tc.beadID)

			var wantFull string
			if tc.wantPath == "." {
				wantFull = townRoot
			} else {
				wantFull = filepath.Join(townRoot, tc.wantPath)
			}

			if dir != wantFull {
				t.Errorf("resolveBeadDir(%q) = %q, want %q", tc.beadID, dir, wantFull)
			}
		})
	}
}

// TestResolveBeadDirUnknownPrefix verifies fallback for unknown prefixes.
func TestResolveBeadDirUnknownPrefix(t *testing.T) {
	townRoot := setupShowTestTown(t)

	origDir, _ := os.Getwd()
	if err := os.Chdir(townRoot); err != nil {
		t.Fatalf("chdir to town root: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	// Unknown prefix should fall back to town root
	dir := resolveBeadDir("xx-unknown-123")
	if dir != townRoot {
		t.Errorf("resolveBeadDir with unknown prefix = %q, want %q (town root fallback)", dir, townRoot)
	}
}

// TestExtractPrefixForRouting verifies prefix extraction from bead IDs.
// This directly relates to GH#2126: the resolver needs to extract the
// first segment before the hyphen for route matching.
func TestExtractPrefixForRouting(t *testing.T) {
	tests := []struct {
		beadID string
		want   string
	}{
		{"gt-abc123", "gt-"},
		{"hq-xyz", "hq-"},
		{"myproject-abc", "myproject-"},
		{"bd-def456", "bd-"},
		{"mol-root-xyz", "mol-"},
		{"", ""},
		{"nohyphen", ""},
		{"-leadinghyphen", ""},
	}

	for _, tc := range tests {
		t.Run(tc.beadID, func(t *testing.T) {
			got := beads.ExtractPrefix(tc.beadID)
			if got != tc.want {
				t.Errorf("ExtractPrefix(%q) = %q, want %q", tc.beadID, got, tc.want)
			}
		})
	}
}
