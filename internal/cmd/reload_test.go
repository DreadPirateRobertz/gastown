package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/config"
)

func TestReloadValidatesConfig(t *testing.T) {
	townRoot := setupTestTownForConfig(t)

	// Create settings directory and config
	settingsDir := filepath.Join(townRoot, "settings")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("mkdir settings: %v", err)
	}
	settings := config.NewTownSettings()
	if err := config.SaveTownSettings(config.TownSettingsPath(townRoot), settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	// Change to town root so workspace.FindFromCwd works
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd) //nolint:errcheck
	if err := os.Chdir(townRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Run reload with --dry-run (no daemon to signal)
	reloadDryRun = true
	defer func() { reloadDryRun = false }()

	err := runReload(reloadCmd, nil)
	if err != nil {
		t.Fatalf("runReload dry-run failed: %v", err)
	}
}

func TestReloadInvalidConfig(t *testing.T) {
	townRoot := setupTestTownForConfig(t)

	// Create settings directory with invalid JSON
	settingsDir := filepath.Join(townRoot, "settings")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatalf("mkdir settings: %v", err)
	}
	settingsPath := filepath.Join(settingsDir, "config.json")
	if err := os.WriteFile(settingsPath, []byte("{invalid json"), 0644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd) //nolint:errcheck
	if err := os.Chdir(townRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	reloadDryRun = true
	defer func() { reloadDryRun = false }()

	err := runReload(reloadCmd, nil)
	if err == nil {
		t.Fatal("expected error for invalid config, got nil")
	}
}

func TestReloadNoDaemon(t *testing.T) {
	townRoot := setupTestTownForConfig(t)

	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd) //nolint:errcheck
	if err := os.Chdir(townRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	// Run reload without dry-run — daemon is not running, should succeed gracefully
	reloadDryRun = false
	err := runReload(reloadCmd, nil)
	if err != nil {
		t.Fatalf("runReload without daemon failed: %v", err)
	}
}
