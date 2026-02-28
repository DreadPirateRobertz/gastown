package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestVerifyWorktreeExists_ValidWorktree(t *testing.T) {
	// Create a real git repo + worktree to test against
	repoDir := t.TempDir()
	cmd := exec.Command("git", "init", "--initial-branch=main", repoDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"git", "-C", repoDir, "config", "user.email", "test@test.com"},
		{"git", "-C", repoDir, "config", "user.name", "Test"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repoDir, "README"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "-C", repoDir, "add", "."},
		{"git", "-C", repoDir, "commit", "-m", "init"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	// Create a worktree
	wtPath := filepath.Join(t.TempDir(), "wt")
	cmd = exec.Command("git", "-C", repoDir, "worktree", "add", "-b", "test-branch", wtPath, "main")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}

	// Should pass validation
	if err := verifyWorktreeExists(wtPath); err != nil {
		t.Errorf("verifyWorktreeExists on valid worktree: %v", err)
	}
}

func TestVerifyWorktreeExists_MissingDir(t *testing.T) {
	err := verifyWorktreeExists("/nonexistent/path/to/worktree")
	if err == nil {
		t.Error("expected error for missing directory")
	}
}

func TestVerifyWorktreeExists_MissingGitFile(t *testing.T) {
	dir := t.TempDir()
	err := verifyWorktreeExists(dir)
	if err == nil {
		t.Error("expected error for missing .git file")
	}
}

func TestVerifyWorktreeExists_BrokenGitFile(t *testing.T) {
	// Create a directory with a .git file that points to a nonexistent worktree
	dir := t.TempDir()
	gitFile := filepath.Join(dir, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: /nonexistent/.repo.git/worktrees/broken\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := verifyWorktreeExists(dir)
	if err == nil {
		t.Error("expected error for broken .git file pointing to nonexistent worktree entry")
	}
}
