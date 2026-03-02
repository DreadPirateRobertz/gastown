package quota

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUnifyMemory_BasicMergeAndSymlink(t *testing.T) {
	tmp := t.TempDir()
	accountsDir := filepath.Join(tmp, "claude-accounts")
	sharedBase := filepath.Join(tmp, "shared-memory")

	// Set up two accounts with the same project.
	project := "-Users-test-gt-laser-crew-aron"
	for _, acct := range []string{"dev1", "cash"} {
		memDir := filepath.Join(accountsDir, acct, "projects", project, "memory")
		if err := os.MkdirAll(memDir, 0755); err != nil {
			t.Fatal(err)
		}
		content := "# " + acct + " memory"
		if acct == "cash" {
			content = "# cash memory\nThis one is larger with more content."
		}
		if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Also add a unique file in dev1.
	if err := os.WriteFile(
		filepath.Join(accountsDir, "dev1", "projects", project, "memory", "debugging.md"),
		[]byte("# debugging notes"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	// Run unify.
	results, err := UnifyMemory(accountsDir, sharedBase, false)
	if err != nil {
		t.Fatalf("UnifyMemory: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.ProjectPath != project {
		t.Errorf("expected project %s, got %s", project, r.ProjectPath)
	}
	if len(r.SymlinksCreated) != 2 {
		t.Errorf("expected 2 symlinks created, got %d: %v", len(r.SymlinksCreated), r.SymlinksCreated)
	}
	if len(r.Warnings) > 0 {
		t.Errorf("unexpected warnings: %v", r.Warnings)
	}

	// Verify shared dir has the larger MEMORY.md.
	sharedMemory := filepath.Join(sharedBase, project, "MEMORY.md")
	data, err := os.ReadFile(sharedMemory)
	if err != nil {
		t.Fatalf("reading shared MEMORY.md: %v", err)
	}
	if string(data) != "# cash memory\nThis one is larger with more content." {
		t.Errorf("expected cash memory content (larger), got: %s", string(data))
	}

	// Verify debugging.md was copied.
	debugFile := filepath.Join(sharedBase, project, "debugging.md")
	if _, err := os.Stat(debugFile); err != nil {
		t.Errorf("debugging.md not copied to shared dir: %v", err)
	}

	// Verify both accounts now have symlinks.
	for _, acct := range []string{"dev1", "cash"} {
		memDir := filepath.Join(accountsDir, acct, "projects", project, "memory")
		info, err := os.Lstat(memDir)
		if err != nil {
			t.Fatalf("lstat %s: %v", memDir, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s memory dir is not a symlink", acct)
		}
		target, err := os.Readlink(memDir)
		if err != nil {
			t.Fatalf("readlink %s: %v", memDir, err)
		}
		expectedTarget := filepath.Join(sharedBase, project)
		if target != expectedTarget {
			t.Errorf("%s symlink target = %s, want %s", acct, target, expectedTarget)
		}
	}
}

func TestUnifyMemory_DryRun(t *testing.T) {
	tmp := t.TempDir()
	accountsDir := filepath.Join(tmp, "claude-accounts")
	sharedBase := filepath.Join(tmp, "shared-memory")

	project := "-Users-test-gt-project"
	memDir := filepath.Join(accountsDir, "dev1", "projects", project, "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	results, err := UnifyMemory(accountsDir, sharedBase, true)
	if err != nil {
		t.Fatalf("UnifyMemory dry-run: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if len(results[0].SymlinksCreated) != 1 {
		t.Errorf("dry-run should report 1 symlink to create, got %d", len(results[0].SymlinksCreated))
	}

	// Verify no actual changes were made.
	info, err := os.Lstat(memDir)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("dry-run should not create symlinks")
	}
	if _, err := os.Stat(filepath.Join(sharedBase, project)); !os.IsNotExist(err) {
		t.Error("dry-run should not create shared dir")
	}
}

func TestUnifyMemory_AlreadyLinked(t *testing.T) {
	tmp := t.TempDir()
	accountsDir := filepath.Join(tmp, "claude-accounts")
	sharedBase := filepath.Join(tmp, "shared-memory")

	project := "-Users-test-gt-project"
	sharedDir := filepath.Join(sharedBase, project)
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sharedDir, "MEMORY.md"), []byte("shared content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create symlink for dev1.
	projectDir := filepath.Join(accountsDir, "dev1", "projects", project)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sharedDir, filepath.Join(projectDir, "memory")); err != nil {
		t.Fatal(err)
	}

	results, err := UnifyMemory(accountsDir, sharedBase, false)
	if err != nil {
		t.Fatalf("UnifyMemory: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if len(r.AlreadyLinked) != 1 || r.AlreadyLinked[0] != "dev1" {
		t.Errorf("expected dev1 already linked, got: %v", r.AlreadyLinked)
	}
	if len(r.SymlinksCreated) != 0 {
		t.Errorf("expected 0 symlinks created, got %d", len(r.SymlinksCreated))
	}
}

func TestUnifyMemory_EmptyAccountsDir(t *testing.T) {
	tmp := t.TempDir()
	accountsDir := filepath.Join(tmp, "claude-accounts")
	sharedBase := filepath.Join(tmp, "shared-memory")

	if err := os.MkdirAll(accountsDir, 0755); err != nil {
		t.Fatal(err)
	}

	results, err := UnifyMemory(accountsDir, sharedBase, false)
	if err != nil {
		t.Fatalf("UnifyMemory: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestUnifyMemory_NoAccountsDir(t *testing.T) {
	tmp := t.TempDir()
	accountsDir := filepath.Join(tmp, "nonexistent")
	sharedBase := filepath.Join(tmp, "shared-memory")

	_, err := UnifyMemory(accountsDir, sharedBase, false)
	if err == nil {
		t.Error("expected error for nonexistent accounts dir")
	}
}

func TestUnifyProjectMemoryForConfigDir(t *testing.T) {
	tmp := t.TempDir()
	accountsDir := filepath.Join(tmp, "claude-accounts")
	sharedBase := filepath.Join(tmp, "shared-memory")

	// Set up dev1 with a project.
	project := "-Users-test-gt-laser-crew-aron"
	memDir := filepath.Join(accountsDir, "dev1", "projects", project, "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("dev1 memory"), 0644); err != nil {
		t.Fatal(err)
	}

	configDir := filepath.Join(accountsDir, "dev1")
	err := UnifyProjectMemoryForConfigDir(accountsDir, sharedBase, configDir)
	if err != nil {
		t.Fatalf("UnifyProjectMemoryForConfigDir: %v", err)
	}

	// Verify symlink was created.
	info, err := os.Lstat(memDir)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("memory dir should be a symlink after unify")
	}

	// Verify content was copied to shared.
	data, err := os.ReadFile(filepath.Join(sharedBase, project, "MEMORY.md"))
	if err != nil {
		t.Fatalf("reading shared MEMORY.md: %v", err)
	}
	if string(data) != "dev1 memory" {
		t.Errorf("unexpected shared content: %s", string(data))
	}
}

func TestUnifyProjectMemoryForConfigDir_NoProjects(t *testing.T) {
	tmp := t.TempDir()
	accountsDir := filepath.Join(tmp, "claude-accounts")
	sharedBase := filepath.Join(tmp, "shared-memory")

	// Create account dir but no projects subdir.
	if err := os.MkdirAll(filepath.Join(accountsDir, "dev1"), 0755); err != nil {
		t.Fatal(err)
	}

	configDir := filepath.Join(accountsDir, "dev1")
	err := UnifyProjectMemoryForConfigDir(accountsDir, sharedBase, configDir)
	if err != nil {
		t.Fatalf("expected no error for missing projects dir, got: %v", err)
	}
}

func TestUnifyMemory_MultipleProjects(t *testing.T) {
	tmp := t.TempDir()
	accountsDir := filepath.Join(tmp, "claude-accounts")
	sharedBase := filepath.Join(tmp, "shared-memory")

	projects := []string{
		"-Users-test-gt-laser-crew-aron",
		"-Users-test-gt-shuffle-crew-bear",
	}

	for _, project := range projects {
		memDir := filepath.Join(accountsDir, "dev1", "projects", project, "memory")
		if err := os.MkdirAll(memDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("content for "+project), 0644); err != nil {
			t.Fatal(err)
		}
	}

	results, err := UnifyMemory(accountsDir, sharedBase, false)
	if err != nil {
		t.Fatalf("UnifyMemory: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Verify both projects got symlinked.
	for _, project := range projects {
		memDir := filepath.Join(accountsDir, "dev1", "projects", project, "memory")
		info, err := os.Lstat(memDir)
		if err != nil {
			t.Fatalf("lstat %s: %v", memDir, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("project %s memory dir is not a symlink", project)
		}
	}
}

func TestUnifyMemory_SharedDirPreexistsWithContent(t *testing.T) {
	tmp := t.TempDir()
	accountsDir := filepath.Join(tmp, "claude-accounts")
	sharedBase := filepath.Join(tmp, "shared-memory")

	project := "-Users-test-gt-project"

	// Pre-create shared dir with a newer MEMORY.md (canonical version).
	sharedDir := filepath.Join(sharedBase, project)
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}
	largeContent := "# Shared memory\nThis is the canonical larger version with lots of content."
	sharedMemPath := filepath.Join(sharedDir, "MEMORY.md")
	if err := os.WriteFile(sharedMemPath, []byte(largeContent), 0644); err != nil {
		t.Fatal(err)
	}
	// Give shared file a future ModTime so it's clearly "newer".
	futureTime := time.Now().Add(1 * time.Hour)
	if err := os.Chtimes(sharedMemPath, futureTime, futureTime); err != nil {
		t.Fatal(err)
	}

	// Create an account with a smaller, older MEMORY.md.
	memDir := filepath.Join(accountsDir, "dev1", "projects", project, "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatal(err)
	}
	acctMemPath := filepath.Join(memDir, "MEMORY.md")
	if err := os.WriteFile(acctMemPath, []byte("small"), 0644); err != nil {
		t.Fatal(err)
	}
	// Give account file an older ModTime.
	pastTime := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(acctMemPath, pastTime, pastTime); err != nil {
		t.Fatal(err)
	}

	results, err := UnifyMemory(accountsDir, sharedBase, false)
	if err != nil {
		t.Fatalf("UnifyMemory: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Verify shared MEMORY.md was NOT overwritten (it was newer).
	data, err := os.ReadFile(filepath.Join(sharedDir, "MEMORY.md"))
	if err != nil {
		t.Fatalf("reading shared MEMORY.md: %v", err)
	}
	if string(data) != largeContent {
		t.Errorf("shared MEMORY.md was overwritten: %s", string(data))
	}
}

func TestUnifyMemory_SymlinkToWrongTarget(t *testing.T) {
	tmp := t.TempDir()
	accountsDir := filepath.Join(tmp, "claude-accounts")
	sharedBase := filepath.Join(tmp, "shared-memory")

	project := "-Users-test-gt-project"

	// Create shared dir.
	sharedDir := filepath.Join(sharedBase, project)
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a symlink pointing to the wrong place.
	wrongTarget := filepath.Join(tmp, "wrong-target")
	if err := os.MkdirAll(wrongTarget, 0755); err != nil {
		t.Fatal(err)
	}
	projectDir := filepath.Join(accountsDir, "dev1", "projects", project)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(wrongTarget, filepath.Join(projectDir, "memory")); err != nil {
		t.Fatal(err)
	}

	results, err := UnifyMemory(accountsDir, sharedBase, false)
	if err != nil {
		t.Fatalf("UnifyMemory: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Verify symlink was replaced with correct target.
	memDir := filepath.Join(projectDir, "memory")
	target, err := os.Readlink(memDir)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != sharedDir {
		t.Errorf("symlink target = %s, want %s", target, sharedDir)
	}
}

func TestUnifyProjectMemoryForConfigDir_FallbackPathIsNoOp(t *testing.T) {
	tmp := t.TempDir()
	accountsDir := filepath.Join(tmp, "claude-accounts")
	sharedBase := filepath.Join(tmp, "shared-memory")

	// Set up an account with a project.
	project := "-Users-test-gt-project"
	memDir := filepath.Join(accountsDir, "dev1", "projects", project, "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Pass ~/.claude (not under accountsDir) — should be a no-op.
	claudeDir := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}

	err := UnifyProjectMemoryForConfigDir(accountsDir, sharedBase, claudeDir)
	if err != nil {
		t.Fatalf("expected no error for fallback path, got: %v", err)
	}

	// Verify the memory dir was NOT symlinked (no-op).
	info, err := os.Lstat(memDir)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("fallback path should not trigger unification")
	}
}

func TestUnifyMemory_AtomicReplaceSafety(t *testing.T) {
	tmp := t.TempDir()
	accountsDir := filepath.Join(tmp, "claude-accounts")
	sharedBase := filepath.Join(tmp, "shared-memory")

	project := "-Users-test-gt-project"
	memDir := filepath.Join(accountsDir, "dev1", "projects", project, "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Run unify — should succeed and leave no .bak remnants.
	results, err := UnifyMemory(accountsDir, sharedBase, false)
	if err != nil {
		t.Fatalf("UnifyMemory: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if len(results[0].Warnings) > 0 {
		t.Errorf("unexpected warnings: %v", results[0].Warnings)
	}

	// Verify no .bak directories left behind.
	bakDir := memDir + ".bak"
	if _, err := os.Stat(bakDir); !os.IsNotExist(err) {
		t.Errorf("backup directory should be cleaned up, but still exists: %s", bakDir)
	}
}
