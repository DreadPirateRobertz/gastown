// Package claude provides Claude Code configuration management.
package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// PreAcceptWorkspaceTrust ensures the workspace trust dialog won't appear for the
// given working directory by writing hasTrustDialogAccepted=true into ~/.claude.json.
//
// Claude Code stores per-project trust state in ~/.claude.json under:
//
//	{ "projects": { "/path/to/dir": { "hasTrustDialogAccepted": true, ... } } }
//
// Claude Code walks parent directories when checking trust, so trusting a parent
// directory covers all subdirectories. This function sets trust for the exact
// directory provided to avoid over-broad trust grants.
//
// This is the primary mechanism for suppressing the "Quick safety check... Yes, I
// trust this folder" dialog that blocks automated agent sessions (Claude Code v2.1.55+).
// The tmux-based AcceptWorkspaceTrustDialog in tmux.go remains as a fallback.
func PreAcceptWorkspaceTrust(workDir string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}
	claudeJSON := filepath.Join(homeDir, ".claude.json")

	// Read existing file (or start fresh).
	var doc map[string]json.RawMessage
	data, err := os.ReadFile(claudeJSON)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("reading %s: %w", claudeJSON, err)
		}
		doc = make(map[string]json.RawMessage)
	} else {
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parsing %s: %w", claudeJSON, err)
		}
	}

	// Parse the projects map.
	var projects map[string]map[string]json.RawMessage
	if raw, ok := doc["projects"]; ok {
		if err := json.Unmarshal(raw, &projects); err != nil {
			return fmt.Errorf("parsing projects in %s: %w", claudeJSON, err)
		}
	}
	if projects == nil {
		projects = make(map[string]map[string]json.RawMessage)
	}

	// Resolve to absolute path for consistent key.
	absDir, err := filepath.Abs(workDir)
	if err != nil {
		absDir = workDir
	}

	// Check if already trusted — skip write if so.
	if proj, ok := projects[absDir]; ok {
		if raw, ok := proj["hasTrustDialogAccepted"]; ok {
			var trusted bool
			if json.Unmarshal(raw, &trusted) == nil && trusted {
				return nil // Already trusted, nothing to do.
			}
		}
	}

	// Ensure project entry exists and set trust.
	if projects[absDir] == nil {
		projects[absDir] = make(map[string]json.RawMessage)
	}
	projects[absDir]["hasTrustDialogAccepted"] = json.RawMessage("true")

	// Write projects back into doc.
	projBytes, err := json.Marshal(projects)
	if err != nil {
		return fmt.Errorf("marshaling projects: %w", err)
	}
	doc["projects"] = json.RawMessage(projBytes)

	// Write file back.
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", claudeJSON, err)
	}
	if err := os.WriteFile(claudeJSON, out, 0600); err != nil {
		return fmt.Errorf("writing %s: %w", claudeJSON, err)
	}

	return nil
}
