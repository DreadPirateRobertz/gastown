package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// mockBdForConvoyTest creates a fake bd binary tailored for convoy empty-check
// tests. The script handles show, dep, close, and list subcommands.
// closeLogPath is the file where close commands are logged for verification.
func mockBdForConvoyTest(t *testing.T, convoyID, convoyTitle string) (binDir, townBeads, closeLogPath string) {
	t.Helper()

	binDir = t.TempDir()
	townRoot := t.TempDir()
	townBeads = filepath.Join(townRoot, ".beads")
	if err := os.MkdirAll(townBeads, 0755); err != nil {
		t.Fatalf("mkdir townBeads: %v", err)
	}

	closeLogPath = filepath.Join(binDir, "bd-close.log")

	bdPath := filepath.Join(binDir, "bd")
	if runtime.GOOS == "windows" {
		t.Skip("skipping convoy empty test on Windows")
	}

	// Shell script that handles the bd subcommands needed by
	// checkSingleConvoy and findStrandedConvoys.
	script := `#!/bin/sh
CLOSE_LOG="` + closeLogPath + `"
CONVOY_ID="` + convoyID + `"
CONVOY_TITLE="` + convoyTitle + `"

# Find the actual subcommand (skip global flags like --allow-stale)
cmd=""
for arg in "$@"; do
  case "$arg" in
    --*) ;; # skip flags
    *) cmd="$arg"; break ;;
  esac
done

case "$cmd" in
  show)
    # Return convoy JSON
    echo '[{"id":"'"$CONVOY_ID"'","title":"'"$CONVOY_TITLE"'","status":"open","issue_type":"convoy"}]'
    exit 0
    ;;
  dep)
    # Return empty tracked issues
    echo '[]'
    exit 0
    ;;
  close)
    # Log the close command for verification
    echo "$@" >> "$CLOSE_LOG"
    exit 0
    ;;
  list)
    # Return one open convoy
    echo '[{"id":"'"$CONVOY_ID"'","title":"'"$CONVOY_TITLE"'"}]'
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
`
	if err := os.WriteFile(bdPath, []byte(script), 0755); err != nil {
		t.Fatalf("write mock bd: %v", err)
	}

	// Prepend mock bd to PATH
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	return binDir, townBeads, closeLogPath
}

// TestCheckSingleConvoy_EmptyConvoySkipsClose verifies that a convoy with 0
// tracked issues is NOT auto-closed. An empty dep list may be transient (Dolt
// snapshot issue), so closing on empty risks premature closure.
func TestCheckSingleConvoy_EmptyConvoySkipsClose(t *testing.T) {
	_, townBeads, closeLogPath := mockBdForConvoyTest(t, "hq-empty1", "Empty test convoy")

	err := checkSingleConvoy(townBeads, "hq-empty1", false)
	if err != nil {
		t.Fatalf("checkSingleConvoy() error: %v", err)
	}

	// Verify bd close was NOT called — empty convoys should be skipped
	if _, err := os.ReadFile(closeLogPath); err == nil {
		t.Error("bd close should NOT be called for empty convoys, but close log exists")
	}
}

func TestCheckSingleConvoy_EmptyConvoyDryRun(t *testing.T) {
	_, townBeads, closeLogPath := mockBdForConvoyTest(t, "hq-empty2", "Dry run convoy")

	err := checkSingleConvoy(townBeads, "hq-empty2", true)
	if err != nil {
		t.Fatalf("checkSingleConvoy() dry-run error: %v", err)
	}

	// In dry-run mode, bd close should NOT be called
	_, err = os.ReadFile(closeLogPath)
	if err == nil {
		t.Error("dry-run should not call bd close, but close log exists")
	}
}

// TestFindStrandedConvoys_EmptyConvoyNotFlagged verifies that convoys with 0
// tracked issues are NOT flagged as stranded. An empty dep list may be transient.
func TestFindStrandedConvoys_EmptyConvoyNotFlagged(t *testing.T) {
	_, townBeads, _ := mockBdForConvoyTest(t, "hq-empty3", "Stranded empty convoy")

	stranded, err := findStrandedConvoys(townBeads)
	if err != nil {
		t.Fatalf("findStrandedConvoys() error: %v", err)
	}

	// Empty convoys should NOT appear in stranded results
	if len(stranded) != 0 {
		t.Fatalf("expected 0 stranded convoys (empty convoys skipped), got %d", len(stranded))
	}
}

// TestFindStrandedConvoys_MixedConvoys verifies that findStrandedConvoys
// correctly returns feedable convoys (has ready issues) but skips empty ones.
func TestFindStrandedConvoys_MixedConvoys(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping convoy test on Windows")
	}

	binDir := t.TempDir()
	townRoot := t.TempDir()
	townBeads := filepath.Join(townRoot, ".beads")
	if err := os.MkdirAll(townBeads, 0755); err != nil {
		t.Fatalf("mkdir townBeads: %v", err)
	}
	// Routes needed so isSlingableBead can resolve gt- prefix to a rig
	if err := os.WriteFile(filepath.Join(townBeads, "routes.jsonl"), []byte(`{"prefix":"gt-","path":"gastown/mayor/rig"}`+"\n"), 0644); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	bdPath := filepath.Join(binDir, "bd")

	// Mock bd that returns two convoys: one empty, one with a ready issue.
	// Uses positional arg parsing to dispatch on convoy ID for dep commands.
	script := `#!/bin/sh
# Collect positional args (skip flags)
i=0
for arg in "$@"; do
  case "$arg" in
    --*) ;;
    *) eval "pos$i=\"$arg\""; i=$((i+1)) ;;
  esac
done

case "$pos0" in
  list)
    echo '[{"id":"hq-empty-mix","title":"Empty convoy"},{"id":"hq-feed-mix","title":"Feedable convoy"}]'
    exit 0
    ;;
  dep)
    # pos2 is the convoy ID (dep list <convoy-id> ...)
    case "$pos2" in
      hq-empty-mix)
        echo '[]'
        ;;
      hq-feed-mix)
        echo '[{"id":"gt-ready1","title":"Ready issue","status":"open","issue_type":"task","assignee":"","dependency_type":"tracks"}]'
        ;;
      *)
        echo '[]'
        ;;
    esac
    exit 0
    ;;
  show)
    # Return issue details for any show query
    echo '[{"id":"gt-ready1","title":"Ready issue","status":"open","issue_type":"task","assignee":"","blocked_by":[],"blocked_by_count":0,"dependencies":[]}]'
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
`
	if err := os.WriteFile(bdPath, []byte(script), 0755); err != nil {
		t.Fatalf("write mock bd: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stranded, err := findStrandedConvoys(townBeads)
	if err != nil {
		t.Fatalf("findStrandedConvoys() error: %v", err)
	}

	// Only the feedable convoy should appear — empty convoy is skipped
	if len(stranded) != 1 {
		t.Fatalf("expected 1 stranded convoy (empty skipped), got %d", len(stranded))
	}

	// Verify feedable convoy
	feedable := stranded[0]
	if feedable.ID != "hq-feed-mix" {
		t.Errorf("stranded convoy ID = %q, want %q", feedable.ID, "hq-feed-mix")
	}
	if feedable.ReadyCount != 1 {
		t.Errorf("feedable convoy ReadyCount = %d, want 1", feedable.ReadyCount)
	}
	if len(feedable.ReadyIssues) != 1 || feedable.ReadyIssues[0] != "gt-ready1" {
		t.Errorf("feedable convoy ReadyIssues = %v, want [gt-ready1]", feedable.ReadyIssues)
	}

	// Verify JSON encoding shape — empty slice encodes as [] not null
	jsonBytes, err := json.Marshal(stranded)
	if err != nil {
		t.Fatalf("json.Marshal(stranded): %v", err)
	}
	jsonStr := string(jsonBytes)
	if strings.Contains(jsonStr, `"ready_issues":null`) {
		t.Error("JSON output contains ready_issues:null — should be [] for empty convoys")
	}
}
