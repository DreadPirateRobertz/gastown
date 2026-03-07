// Tests for wl_schema_evolution.go — version parsing, classification, and the
// checkSchemaEvolution gating logic that blocks MAJOR bumps without --upgrade.
package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseSchemaVersion_Valid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input      string
		wantMajor  int
		wantMinor  int
	}{
		{"1.0", 1, 0},
		{"2.3", 2, 3},
		{"0.0", 0, 0},
		{"10.42", 10, 42},
		{" 1.0 ", 1, 0}, // leading/trailing space
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			major, minor, err := ParseSchemaVersion(tt.input)
			if err != nil {
				t.Fatalf("ParseSchemaVersion(%q) unexpected error: %v", tt.input, err)
			}
			if major != tt.wantMajor || minor != tt.wantMinor {
				t.Errorf("ParseSchemaVersion(%q) = (%d, %d), want (%d, %d)",
					tt.input, major, minor, tt.wantMajor, tt.wantMinor)
			}
		})
	}
}

func TestParseSchemaVersion_Invalid(t *testing.T) {
	t.Parallel()
	cases := []string{"1", "1.0.0", "", "abc", "1.x", "x.0"}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			_, _, err := ParseSchemaVersion(input)
			if err == nil {
				t.Errorf("ParseSchemaVersion(%q) expected error, got nil", input)
			}
		})
	}
}

func TestClassifySchemaChange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		local    string
		upstream string
		want     SchemaChangeKind
	}{
		// Unchanged
		{"1.0", "1.0", SchemaUnchanged},
		{"2.5", "2.5", SchemaUnchanged},
		// Minor bump
		{"1.0", "1.1", SchemaMinorChange},
		{"1.0", "1.9", SchemaMinorChange},
		{"2.0", "2.1", SchemaMinorChange},
		// Major bump
		{"1.0", "2.0", SchemaMajorChange},
		{"1.5", "2.0", SchemaMajorChange},
		{"1.9", "3.0", SchemaMajorChange},
		// Downgrade (local newer) — treated as unchanged
		{"1.1", "1.0", SchemaUnchanged},
		{"2.0", "1.9", SchemaUnchanged},
	}
	for _, tt := range tests {
		t.Run(tt.local+"->"+tt.upstream, func(t *testing.T) {
			t.Parallel()
			got, err := ClassifySchemaChange(tt.local, tt.upstream)
			if err != nil {
				t.Fatalf("ClassifySchemaChange(%q, %q) unexpected error: %v", tt.local, tt.upstream, err)
			}
			if got != tt.want {
				t.Errorf("ClassifySchemaChange(%q, %q) = %v, want %v",
					tt.local, tt.upstream, got, tt.want)
			}
		})
	}
}

func TestClassifySchemaChange_InvalidVersion(t *testing.T) {
	t.Parallel()
	_, err := ClassifySchemaChange("bad", "1.0")
	if err == nil {
		t.Error("expected error for invalid local version")
	}
	_, err = ClassifySchemaChange("1.0", "bad")
	if err == nil {
		t.Error("expected error for invalid upstream version")
	}
}

// TestCheckSchemaEvolution_MajorBlocksWithoutUpgrade verifies that a MAJOR
// version bump is rejected unless the caller passes upgrade=true.
func TestCheckSchemaEvolution_MajorBlocksWithoutUpgrade(t *testing.T) {
	t.Parallel()

	// We need a dolt binary and a fork dir with _meta. If dolt isn't installed,
	// skip — this is an integration-style test.
	doltPath, err := exec.LookPath("dolt")
	if err != nil {
		t.Skip("dolt not in PATH")
	}

	forkDir := setupDoltWithMeta(t, doltPath, "1.0")
	setupUpstreamRef(t, doltPath, forkDir, "2.0")

	// Without --upgrade: should block
	_, err = checkSchemaEvolution(doltPath, forkDir, false)
	if err == nil {
		t.Fatal("expected MAJOR bump to be blocked without --upgrade")
	}

	// With --upgrade: should succeed
	result, err := checkSchemaEvolution(doltPath, forkDir, true)
	if err != nil {
		t.Fatalf("MAJOR bump with --upgrade should succeed: %v", err)
	}
	if result.Kind != SchemaMajorChange {
		t.Errorf("expected SchemaMajorChange, got %v", result.Kind)
	}
}

// TestCheckSchemaEvolution_MinorAutoApplies verifies that MINOR bumps
// are allowed through without error.
func TestCheckSchemaEvolution_MinorAutoApplies(t *testing.T) {
	t.Parallel()

	doltPath, err := exec.LookPath("dolt")
	if err != nil {
		t.Skip("dolt not in PATH")
	}

	forkDir := setupDoltWithMeta(t, doltPath, "1.0")
	setupUpstreamRef(t, doltPath, forkDir, "1.1")

	result, err := checkSchemaEvolution(doltPath, forkDir, false)
	if err != nil {
		t.Fatalf("MINOR bump should not error: %v", err)
	}
	if result.Kind != SchemaMinorChange {
		t.Errorf("expected SchemaMinorChange, got %v", result.Kind)
	}
	if result.LocalVer != "1.0" || result.UpstreamVer != "1.1" {
		t.Errorf("expected versions 1.0 → 1.1, got %s → %s", result.LocalVer, result.UpstreamVer)
	}
}

// TestCheckSchemaEvolution_NoMeta verifies that forks without _meta
// (pre-versioned) pass through without error or version info.
func TestCheckSchemaEvolution_NoMeta(t *testing.T) {
	t.Parallel()

	doltPath, err := exec.LookPath("dolt")
	if err != nil {
		t.Skip("dolt not in PATH")
	}

	// Init a dolt repo without creating _meta — simulates a pre-versioned fork.
	forkDir := t.TempDir()
	runDolt(t,doltPath, forkDir, "init", "--name", "test", "--email", "test@test.com")

	result, err := checkSchemaEvolution(doltPath, forkDir, false)
	if err != nil {
		t.Fatalf("pre-versioned fork should not error: %v", err)
	}
	if result.Kind != SchemaUnchanged {
		t.Errorf("expected SchemaUnchanged for pre-versioned fork, got %v", result.Kind)
	}
}

// TestVerifyPostMergeSchema_Mismatch verifies that a version mismatch after
// merge is detected and reported.
func TestVerifyPostMergeSchema_Mismatch(t *testing.T) {
	t.Parallel()

	doltPath, err := exec.LookPath("dolt")
	if err != nil {
		t.Skip("dolt not in PATH")
	}

	// Create a fork with version 1.0, then verify against expected 1.1.
	// This simulates a merge that failed to update _meta.
	forkDir := setupDoltWithMeta(t, doltPath, "1.0")

	err = verifyPostMergeSchema(doltPath, forkDir, "1.1")
	if err == nil {
		t.Fatal("expected error for version mismatch")
	}
}

// TestVerifyPostMergeSchema_Match verifies success when versions agree.
func TestVerifyPostMergeSchema_Match(t *testing.T) {
	t.Parallel()

	doltPath, err := exec.LookPath("dolt")
	if err != nil {
		t.Skip("dolt not in PATH")
	}

	forkDir := setupDoltWithMeta(t, doltPath, "1.1")

	err = verifyPostMergeSchema(doltPath, forkDir, "1.1")
	if err != nil {
		t.Fatalf("matching versions should not error: %v", err)
	}
}

// TestVerifyPostMergeSchema_EmptyExpected verifies no-op when no version
// expectation is set (pre-versioned fork case).
func TestVerifyPostMergeSchema_EmptyExpected(t *testing.T) {
	t.Parallel()

	err := verifyPostMergeSchema("/nonexistent", "/nonexistent", "")
	if err != nil {
		t.Fatalf("empty expected version should be no-op: %v", err)
	}
}

// TestDetectSchemaChanges_NoDolt verifies graceful degradation when dolt
// isn't available or the path is wrong.
func TestDetectSchemaChanges_NoDolt(t *testing.T) {
	t.Parallel()

	result := detectSchemaChanges("/nonexistent-dolt", "/nonexistent-dir")
	if result != "" {
		t.Errorf("expected empty result for invalid paths, got %q", result)
	}
}

// --- test helpers ---

// setupDoltWithMeta creates a temp dolt repo with a _meta table containing
// the given schema version. Returns the directory path.
func setupDoltWithMeta(t *testing.T, doltPath, version string) string {
	t.Helper()
	dir := t.TempDir()

	runDolt(t,doltPath, dir, "init", "--name", "test", "--email", "test@test.com")
	runDolt(t,doltPath, dir, "sql", "-q",
		"CREATE TABLE _meta (`key` VARCHAR(64) PRIMARY KEY, value TEXT)")
	runDolt(t,doltPath, dir, "sql", "-q",
		"INSERT INTO _meta (`key`, value) VALUES ('schema_version', '"+version+"')")
	runDolt(t,doltPath, dir, "add", ".")
	runDolt(t,doltPath, dir, "commit", "-m", "init with schema "+version)

	return dir
}

// setupUpstreamRef creates a branch "upstream/main" with a different schema
// version in _meta. This simulates having fetched from upstream.
func setupUpstreamRef(t *testing.T, doltPath, dir, version string) {
	t.Helper()

	// Create a branch to act as upstream/main
	runDolt(t,doltPath, dir, "checkout", "-b", "upstream/main")
	runDolt(t,doltPath, dir, "sql", "-q",
		"UPDATE _meta SET value = '"+version+"' WHERE `key` = 'schema_version'")
	runDolt(t,doltPath, dir, "add", ".")
	runDolt(t,doltPath, dir, "commit", "-m", "upstream bump to "+version)

	// Return to main
	runDolt(t,doltPath, dir, "checkout", "main")
}

// runDolt executes a dolt command in the given directory and fails the test on error.
func runDolt(t *testing.T, doltPath, dir string, args ...string) {
	t.Helper()

	// Ensure HOME is set to a temp dir to avoid polluting user's dolt config.
	// Also set DOLT_ROOT_PATH so dolt doesn't create .dolt in real home.
	cmd := exec.Command(doltPath, args...)
	cmd.Dir = dir
	doltHome := filepath.Join(dir, ".dolt-home")
	_ = os.MkdirAll(doltHome, 0o755)
	cmd.Env = append(os.Environ(),
		"HOME="+doltHome,
		"DOLT_ROOT_PATH="+doltHome,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dolt %v failed: %v\n%s", args, err, out)
	}
}
