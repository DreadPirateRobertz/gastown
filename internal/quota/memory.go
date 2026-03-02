package quota

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// UnifyResult tracks what happened for each project during memory unification.
type UnifyResult struct {
	ProjectPath     string   // e.g., "-Users-seanbearden-gt-laser-crew-aron"
	SharedDir       string   // canonical shared memory directory
	AccountsMerged  []string // accounts whose content was merged into shared
	SymlinksCreated []string // accounts that got new symlinks
	AlreadyLinked   []string // accounts already symlinked correctly
	Warnings        []string // non-fatal issues encountered
}

// UnifyMemory scans all account project dirs and replaces memory/
// directories with symlinks to a shared canonical location.
//
// accountsDir is typically ~/.claude-accounts
// sharedBase is typically ~/.claude/shared-memory
func UnifyMemory(accountsDir, sharedBase string, dryRun bool) ([]UnifyResult, error) {
	// Discover all projects across all accounts.
	// Structure: accountsDir/<account>/projects/<projectPath>/memory/
	projects, err := discoverProjects(accountsDir)
	if err != nil {
		return nil, fmt.Errorf("scanning accounts: %w", err)
	}

	var results []UnifyResult
	for projectPath, entries := range projects {
		result := unifyProject(projectPath, entries, sharedBase, dryRun)
		results = append(results, result)
	}
	return results, nil
}

// UnifyProjectMemoryForConfigDir unifies memory for all projects
// under a specific config dir. Called post-rotation to ensure the
// newly-active account's project dirs get symlinks.
//
// configDir is the CLAUDE_CONFIG_DIR for the rotated session (e.g.,
// ~/.claude-accounts/dev1). We scan all accounts so that existing
// real dirs from other accounts also get linked.
func UnifyProjectMemoryForConfigDir(accountsDir, sharedBase, configDir string) error {
	// Find which account this config dir belongs to.
	// configDir is like ~/.claude-accounts/dev1 — the account name is the last segment.
	rel, err := filepath.Rel(accountsDir, configDir)
	if err != nil {
		return fmt.Errorf("config dir %s not under accounts dir %s: %w", configDir, accountsDir, err)
	}
	// rel could be "dev1" or "dev1/subdir" — we only care about the top-level account.
	accountName := strings.SplitN(rel, string(os.PathSeparator), 2)[0]

	// Scan for projects under this specific account.
	projectsDir := filepath.Join(accountsDir, accountName, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No projects dir yet — nothing to unify.
		}
		return fmt.Errorf("reading projects dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projectPath := entry.Name()
		memoryDir := filepath.Join(projectsDir, projectPath, "memory")

		// Check if memory dir exists.
		info, err := os.Lstat(memoryDir)
		if err != nil {
			continue // No memory dir — nothing to do.
		}

		sharedDir := filepath.Join(sharedBase, projectPath)

		// Already a correct symlink?
		if info.Mode()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(memoryDir)
			if err == nil && target == sharedDir {
				continue // Already correct.
			}
		}

		// Unify this specific project across ALL accounts (not just this one).
		allProjects, err := discoverProjects(accountsDir)
		if err != nil {
			continue
		}
		if projectEntries, ok := allProjects[projectPath]; ok {
			_ = unifyProject(projectPath, projectEntries, sharedBase, false)
		} else {
			// Only this account has this project — still create the symlink.
			_ = unifyProject(projectPath, []projectEntry{{
				AccountName: accountName,
				MemoryDir:   memoryDir,
			}}, sharedBase, false)
		}
	}

	return nil
}

// projectEntry represents one account's memory dir for a project.
type projectEntry struct {
	AccountName string // e.g., "dev1", "cash"
	MemoryDir   string // full path to .../memory/
}

// discoverProjects scans accountsDir for all project memory directories.
// Returns map[projectPath][]projectEntry.
func discoverProjects(accountsDir string) (map[string][]projectEntry, error) {
	accounts, err := os.ReadDir(accountsDir)
	if err != nil {
		return nil, err
	}

	projects := make(map[string][]projectEntry)
	for _, acct := range accounts {
		if !acct.IsDir() {
			continue
		}
		projectsDir := filepath.Join(accountsDir, acct.Name(), "projects")
		projEntries, err := os.ReadDir(projectsDir)
		if err != nil {
			continue // Account may not have projects dir.
		}
		for _, proj := range projEntries {
			if !proj.IsDir() {
				continue
			}
			memoryDir := filepath.Join(projectsDir, proj.Name(), "memory")
			// Check if memory dir exists (as real dir or symlink).
			if _, err := os.Lstat(memoryDir); err == nil {
				projects[proj.Name()] = append(projects[proj.Name()], projectEntry{
					AccountName: acct.Name(),
					MemoryDir:   memoryDir,
				})
			}
		}
	}
	return projects, nil
}

// unifyProject creates a shared dir and replaces all account memory dirs
// with symlinks for a single project.
func unifyProject(projectPath string, entries []projectEntry, sharedBase string, dryRun bool) UnifyResult {
	sharedDir := filepath.Join(sharedBase, projectPath)
	result := UnifyResult{
		ProjectPath: projectPath,
		SharedDir:   sharedDir,
	}

	// Classify entries: already-linked vs real directories.
	var realDirs []projectEntry
	for _, entry := range entries {
		info, err := os.Lstat(entry.MemoryDir)
		if err != nil {
			continue
		}

		if info.Mode()&fs.ModeSymlink != 0 {
			// It's a symlink — check if it points to the right place.
			target, err := os.Readlink(entry.MemoryDir)
			if err == nil && target == sharedDir {
				result.AlreadyLinked = append(result.AlreadyLinked, entry.AccountName)
				continue
			}
			// Symlink to wrong target — treat as needing replacement.
		}

		realDirs = append(realDirs, entry)
	}

	if len(realDirs) == 0 {
		return result // All already linked.
	}

	if dryRun {
		for _, entry := range realDirs {
			result.SymlinksCreated = append(result.SymlinksCreated, entry.AccountName)
		}
		return result
	}

	// Ensure shared dir exists.
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to create shared dir: %v", err))
		return result
	}

	// Merge content from real dirs into shared dir.
	// Strategy: for MEMORY.md, pick the largest file. For other .md files,
	// copy if not already present in shared dir.
	mergeMemoryContent(realDirs, sharedDir, &result)

	// Replace each real dir with a symlink.
	for _, entry := range realDirs {
		if err := replaceWithSymlink(entry.MemoryDir, sharedDir); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"failed to symlink %s: %v", entry.AccountName, err))
			continue
		}
		result.SymlinksCreated = append(result.SymlinksCreated, entry.AccountName)
	}

	return result
}

// mergeMemoryContent copies files from real dirs into the shared dir.
// For MEMORY.md: picks the largest version as canonical.
// For other files: copies if not already present in shared.
func mergeMemoryContent(entries []projectEntry, sharedDir string, result *UnifyResult) {
	// Find the best MEMORY.md (largest file).
	var bestMemoryPath string
	var bestMemorySize int64
	var bestMemoryAccount string

	// Also track all other .md files across all entries.
	otherFiles := make(map[string]string) // filename -> source path (first seen wins)

	for _, entry := range entries {
		files, err := os.ReadDir(entry.MemoryDir)
		if err != nil {
			continue
		}

		for _, f := range files {
			if f.IsDir() {
				continue
			}
			srcPath := filepath.Join(entry.MemoryDir, f.Name())
			info, err := f.Info()
			if err != nil {
				continue
			}

			if f.Name() == "MEMORY.md" {
				if info.Size() > bestMemorySize {
					bestMemorySize = info.Size()
					bestMemoryPath = srcPath
					bestMemoryAccount = entry.AccountName
				}
			} else {
				if _, exists := otherFiles[f.Name()]; !exists {
					otherFiles[f.Name()] = srcPath
				}
			}
		}
		result.AccountsMerged = append(result.AccountsMerged, entry.AccountName)
	}

	// Copy best MEMORY.md to shared dir if shared doesn't already have one
	// (or if shared's version is smaller).
	if bestMemoryPath != "" {
		sharedMemory := filepath.Join(sharedDir, "MEMORY.md")
		shouldCopy := true

		if info, err := os.Stat(sharedMemory); err == nil {
			if info.Size() >= bestMemorySize {
				shouldCopy = false // Shared already has equal or larger version.
			}
		}

		if shouldCopy {
			if err := copyFile(bestMemoryPath, sharedMemory); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf(
					"failed to copy MEMORY.md from %s: %v", bestMemoryAccount, err))
			}
		}
	}

	// Copy other files if not present in shared dir.
	for name, srcPath := range otherFiles {
		destPath := filepath.Join(sharedDir, name)
		if _, err := os.Stat(destPath); err == nil {
			continue // Already exists in shared.
		}
		if err := copyFile(srcPath, destPath); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"failed to copy %s: %v", name, err))
		}
	}
}

// replaceWithSymlink removes the directory and creates a symlink in its place.
func replaceWithSymlink(memoryDir, sharedDir string) error {
	// Remove the existing directory (content already merged into shared).
	if err := os.RemoveAll(memoryDir); err != nil {
		return fmt.Errorf("removing %s: %w", memoryDir, err)
	}

	// Ensure parent directory exists (for newly-created account project dirs).
	parent := filepath.Dir(memoryDir)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("creating parent %s: %w", parent, err)
	}

	// Create symlink.
	if err := os.Symlink(sharedDir, memoryDir); err != nil {
		return fmt.Errorf("creating symlink: %w", err)
	}

	return nil
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
