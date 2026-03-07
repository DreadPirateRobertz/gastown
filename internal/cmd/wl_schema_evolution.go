// Package cmd implements CLI commands for Gas Town.
//
// wl_schema_evolution.go handles schema versioning for wasteland (wl) sync.
// Wasteland databases use a _meta table with a "schema_version" key in
// MAJOR.MINOR format. When upstream bumps the version, gt wl sync must decide
// whether to auto-apply (MINOR) or gate behind --upgrade (MAJOR).
//
// The flow:
//  1. checkSchemaEvolution detects version deltas and blocks MAJOR bumps.
//  2. detectSchemaChanges previews what tables/columns changed (informational).
//  3. After dolt merge, verifyPostMergeSchema confirms the version landed.
package cmd

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// SchemaChangeKind describes the type of schema change between two semver strings.
type SchemaChangeKind int

const (
	// SchemaUnchanged means local and upstream versions are identical or local is ahead.
	SchemaUnchanged SchemaChangeKind = iota
	// SchemaMinorChange means upstream added columns or tables (backwards-compatible).
	SchemaMinorChange
	// SchemaMajorChange means upstream made breaking changes that require explicit upgrade.
	SchemaMajorChange
)

// ParseSchemaVersion parses a "MAJOR.MINOR" version string into its components.
func ParseSchemaVersion(s string) (major, minor int, err error) {
	parts := strings.SplitN(strings.TrimSpace(s), ".", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid schema version %q: expected MAJOR.MINOR", s)
	}
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid major version in %q: %w", s, err)
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid minor version in %q: %w", s, err)
	}
	return major, minor, nil
}

// ClassifySchemaChange compares local and upstream version strings and returns
// the kind of change. Downgrades (upstream older than local) return SchemaUnchanged.
func ClassifySchemaChange(local, upstream string) (SchemaChangeKind, error) {
	localMajor, localMinor, err := ParseSchemaVersion(local)
	if err != nil {
		return SchemaUnchanged, fmt.Errorf("local version: %w", err)
	}
	upMajor, upMinor, err := ParseSchemaVersion(upstream)
	if err != nil {
		return SchemaUnchanged, fmt.Errorf("upstream version: %w", err)
	}

	switch {
	case upMajor > localMajor:
		return SchemaMajorChange, nil
	case upMajor == localMajor && upMinor > localMinor:
		return SchemaMinorChange, nil
	default:
		return SchemaUnchanged, nil
	}
}

// readDoltSchemaVersion reads schema_version from the _meta table of a local
// dolt fork. asOf specifies the branch/ref (e.g. "HEAD" or "upstream/main").
// Returns ("", nil) when the _meta table or schema_version row does not exist.
func readDoltSchemaVersion(doltPath, forkDir, asOf string) (string, error) {
	var query string
	if asOf == "" || asOf == "HEAD" {
		query = "SELECT value FROM _meta WHERE `key` = 'schema_version';"
	} else {
		query = fmt.Sprintf(
			"SELECT value FROM _meta AS OF '%s' WHERE `key` = 'schema_version';",
			asOf,
		)
	}

	cmd := exec.Command(doltPath, "sql", "-r", "csv", "-q", query)
	cmd.Dir = forkDir
	out, err := cmd.Output()
	if err != nil {
		// _meta may not exist on older forks — treat as unknown, not fatal.
		return "", nil
	}

	// Output format: "value\n<version>\n"
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return "", nil
	}
	version := strings.TrimSpace(lines[1])
	return version, nil
}

// SchemaEvolutionResult captures the outcome of a schema version check so the
// caller (wl_sync) can decide what to show and whether to proceed.
type SchemaEvolutionResult struct {
	Kind        SchemaChangeKind
	LocalVer    string
	UpstreamVer string
}

// checkSchemaEvolution compares local and upstream _meta.schema_version and
// returns a structured result. For MAJOR bumps it returns an error unless
// upgrade is true, preventing accidental breaking migrations.
//
// Precondition: caller has already run `dolt fetch upstream` so that
// upstream/main is available for AS OF queries.
//
// Returns a zero result (SchemaUnchanged) when the fork lacks a _meta table
// or schema_version row (pre-versioned fork) — the pull proceeds without
// interruption.
func checkSchemaEvolution(doltPath, forkDir string, upgrade bool) (SchemaEvolutionResult, error) {
	localVer, err := readDoltSchemaVersion(doltPath, forkDir, "HEAD")
	if err != nil || localVer == "" {
		return SchemaEvolutionResult{}, nil // pre-versioned fork — skip check
	}

	upstreamVer, err := readDoltSchemaVersion(doltPath, forkDir, "upstream/main")
	if err != nil || upstreamVer == "" {
		return SchemaEvolutionResult{}, nil // upstream has no version info — skip check
	}

	kind, err := ClassifySchemaChange(localVer, upstreamVer)
	if err != nil {
		return SchemaEvolutionResult{}, fmt.Errorf("schema version check: %w", err)
	}

	result := SchemaEvolutionResult{
		Kind:        kind,
		LocalVer:    localVer,
		UpstreamVer: upstreamVer,
	}

	switch kind {
	case SchemaUnchanged:
		// nothing to report
	case SchemaMinorChange:
		fmt.Printf("  Schema: %s → %s (MINOR — auto-applying)\n", localVer, upstreamVer)
	case SchemaMajorChange:
		if !upgrade {
			return result, fmt.Errorf(
				"upstream schema version %s is a MAJOR upgrade from your local %s\n\n"+
					"This may require manual data migration. To proceed:\n\n"+
					"  gt wl sync --upgrade\n\n"+
					"Review the upstream CHANGELOG before upgrading.",
				upstreamVer, localVer,
			)
		}
		fmt.Printf("  Schema: %s → %s (MAJOR — upgrading as requested)\n", localVer, upstreamVer)
	}

	return result, nil
}

// detectSchemaChanges runs `dolt diff --schema` between HEAD and upstream/main
// to produce a human-readable summary of table/column changes. Returns empty
// string when there are no schema differences or when the diff fails
// (non-fatal — the merge may still succeed via Dolt's built-in schema merge).
func detectSchemaChanges(doltPath, forkDir string) string {
	cmd := exec.Command(doltPath, "diff", "--schema", "HEAD", "upstream/main")
	cmd.Dir = forkDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	diff := strings.TrimSpace(string(out))
	if diff == "" {
		return ""
	}
	return diff
}

// verifyPostMergeSchema checks that the local _meta.schema_version matches the
// expected upstream version after a merge. This catches partial merges where
// data merged but the version row didn't update (e.g., due to a merge conflict
// on _meta that was auto-resolved incorrectly).
//
// Returns nil when versions match or when _meta is absent (pre-versioned fork).
func verifyPostMergeSchema(doltPath, forkDir, expectedVer string) error {
	if expectedVer == "" {
		return nil // no version expectation — nothing to verify
	}

	actualVer, err := readDoltSchemaVersion(doltPath, forkDir, "HEAD")
	if err != nil {
		return nil // can't read — non-fatal, might be pre-versioned
	}

	if actualVer == "" {
		// _meta row disappeared during merge — warn but don't block
		fmt.Printf("  ⚠ schema_version missing from _meta after merge (expected %s)\n", expectedVer)
		return nil
	}

	if actualVer != expectedVer {
		return fmt.Errorf(
			"schema version mismatch after merge: expected %s, got %s\n\n"+
				"The merge may have conflicted on the _meta table. Check:\n"+
				"  dolt conflicts resolve _meta --theirs\n"+
				"  dolt add _meta && dolt commit -m 'resolve schema version conflict'",
			expectedVer, actualVer,
		)
	}

	return nil
}
