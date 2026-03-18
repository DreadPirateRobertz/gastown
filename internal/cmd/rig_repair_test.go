package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/rig"
)

// setupRepairTown creates a minimal town structure for repair tests.
// Returns townRoot and sets up mayor/town.json so workspace detection works.
func setupRepairTown(t *testing.T) string {
	t.Helper()
	townRoot := t.TempDir()
	mayorDir := filepath.Join(townRoot, "mayor")
	if err := os.MkdirAll(mayorDir, 0755); err != nil {
		t.Fatalf("mkdir mayor: %v", err)
	}
	// Minimal town.json so workspace.FindFromCwdOrError can locate the root
	townJSON := map[string]string{"name": "test-town"}
	data, _ := json.Marshal(townJSON)
	if err := os.WriteFile(filepath.Join(mayorDir, "town.json"), data, 0644); err != nil {
		t.Fatalf("write town.json: %v", err)
	}
	// Restore cwd after test
	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	if err := os.Chdir(townRoot); err != nil {
		t.Fatalf("chdir to townRoot: %v", err)
	}
	return townRoot
}

func TestRunRigRepair_RigNotFound(t *testing.T) {
	setupRepairTown(t)

	cmd := &cobra.Command{}
	err := runRigRepair(cmd, []string{"nonexistent-rig"})
	if err == nil {
		t.Fatal("expected error for nonexistent rig, got nil")
	}
	// Should report rig not found
	msg := err.Error()
	if !contains(msg, "not found") && !contains(msg, "nonexistent-rig") {
		t.Errorf("error should mention rig not found, got: %v", err)
	}
}

func TestRunRigRepair_InvalidConfig(t *testing.T) {
	townRoot := setupRepairTown(t)

	// Create rig directory but with invalid config.json
	rigPath := filepath.Join(townRoot, "badrig")
	if err := os.MkdirAll(rigPath, 0755); err != nil {
		t.Fatalf("mkdir rigPath: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rigPath, "config.json"), []byte("not-json"), 0644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	cmd := &cobra.Command{}
	err := runRigRepair(cmd, []string{"badrig"})
	if err == nil {
		t.Fatal("expected error for invalid config, got nil")
	}
}

func TestRunRigRepair_NoPrefixConfigured(t *testing.T) {
	townRoot := setupRepairTown(t)

	// Create a rig with valid config but no beads prefix
	rigPath := filepath.Join(townRoot, "myrig")
	if err := os.MkdirAll(rigPath, 0755); err != nil {
		t.Fatalf("mkdir rigPath: %v", err)
	}
	cfg := &rig.RigConfig{
		Type:    "rig",
		Version: 1,
		Name:    "myrig",
		Beads:   &rig.BeadsConfig{Prefix: ""},
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(rigPath, "config.json"), data, 0644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	cmd := &cobra.Command{}
	err := runRigRepair(cmd, []string{"myrig"})
	if err == nil {
		t.Fatal("expected error for missing prefix, got nil")
	}
	if !contains(err.Error(), "prefix") {
		t.Errorf("error should mention missing prefix, got: %v", err)
	}
}

func TestRunRigRepair_DoltNotRunning(t *testing.T) {
	townRoot := setupRepairTown(t)

	// Create a valid rig with a prefix but no Dolt server
	rigPath := filepath.Join(townRoot, "myrig")
	if err := os.MkdirAll(rigPath, 0755); err != nil {
		t.Fatalf("mkdir rigPath: %v", err)
	}
	cfg := &rig.RigConfig{
		Type:    "rig",
		Version: 1,
		Name:    "myrig",
		Beads:   &rig.BeadsConfig{Prefix: "mr"},
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(rigPath, "config.json"), data, 0644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	// No Dolt server running — should fail with a helpful error.
	// (doltserver.IsRunning checks for PID file / port, neither exist in tmp dir)
	cmd := &cobra.Command{}
	err := runRigRepair(cmd, []string{"myrig"})
	if err == nil {
		t.Fatal("expected error when Dolt not running, got nil")
	}
	if !contains(err.Error(), "Dolt") && !contains(err.Error(), "running") {
		t.Errorf("error should mention Dolt not running, got: %v", err)
	}
}

