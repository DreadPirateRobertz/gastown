package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPreAcceptWorkspaceTrust(t *testing.T) {
	// Use a temp dir as fake home to avoid modifying real ~/.claude.json.
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	workDir := "/tmp/test-workspace"

	// Test 1: Creates ~/.claude.json from scratch.
	if err := PreAcceptWorkspaceTrust(workDir); err != nil {
		t.Fatalf("PreAcceptWorkspaceTrust failed on empty home: %v", err)
	}

	claudeJSON := filepath.Join(tmpHome, ".claude.json")
	data, err := os.ReadFile(claudeJSON)
	if err != nil {
		t.Fatalf("failed to read %s: %v", claudeJSON, err)
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("failed to parse %s: %v", claudeJSON, err)
	}

	var projects map[string]map[string]json.RawMessage
	if err := json.Unmarshal(doc["projects"], &projects); err != nil {
		t.Fatalf("failed to parse projects: %v", err)
	}

	proj, ok := projects[workDir]
	if !ok {
		t.Fatalf("workDir %s not found in projects", workDir)
	}

	var trusted bool
	if err := json.Unmarshal(proj["hasTrustDialogAccepted"], &trusted); err != nil {
		t.Fatalf("failed to parse hasTrustDialogAccepted: %v", err)
	}
	if !trusted {
		t.Errorf("expected hasTrustDialogAccepted=true, got false")
	}

	// Test 2: Idempotent — calling again doesn't error or corrupt.
	if err := PreAcceptWorkspaceTrust(workDir); err != nil {
		t.Fatalf("PreAcceptWorkspaceTrust idempotent call failed: %v", err)
	}

	// Test 3: Preserves existing keys in ~/.claude.json.
	// Add a custom key to the file.
	doc["testKey"] = json.RawMessage(`"preserved"`)
	out, _ := json.MarshalIndent(doc, "", "  ")
	os.WriteFile(claudeJSON, out, 0600)

	anotherDir := "/tmp/another-workspace"
	if err := PreAcceptWorkspaceTrust(anotherDir); err != nil {
		t.Fatalf("PreAcceptWorkspaceTrust for second dir failed: %v", err)
	}

	data, _ = os.ReadFile(claudeJSON)
	json.Unmarshal(data, &doc)

	// Verify testKey preserved.
	var testVal string
	if err := json.Unmarshal(doc["testKey"], &testVal); err != nil || testVal != "preserved" {
		t.Errorf("existing key was not preserved: got %q, err=%v", testVal, err)
	}

	// Verify both dirs are trusted.
	json.Unmarshal(doc["projects"], &projects)
	for _, dir := range []string{workDir, anotherDir} {
		proj, ok := projects[dir]
		if !ok {
			t.Errorf("dir %s not found in projects", dir)
			continue
		}
		var t2 bool
		json.Unmarshal(proj["hasTrustDialogAccepted"], &t2)
		if !t2 {
			t.Errorf("expected hasTrustDialogAccepted=true for %s", dir)
		}
	}

	// Test 4: Preserves existing project fields.
	projects[workDir]["allowedTools"] = json.RawMessage(`[]`)
	projBytes, _ := json.Marshal(projects)
	doc["projects"] = json.RawMessage(projBytes)
	out, _ = json.MarshalIndent(doc, "", "  ")
	os.WriteFile(claudeJSON, out, 0600)

	if err := PreAcceptWorkspaceTrust(workDir); err != nil {
		t.Fatalf("PreAcceptWorkspaceTrust with existing fields failed: %v", err)
	}

	data, _ = os.ReadFile(claudeJSON)
	json.Unmarshal(data, &doc)
	json.Unmarshal(doc["projects"], &projects)
	if string(projects[workDir]["allowedTools"]) != "[]" {
		t.Errorf("existing project field allowedTools was not preserved")
	}
}
