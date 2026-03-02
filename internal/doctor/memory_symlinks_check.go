package doctor

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// MemorySymlinksCheck detects non-symlinked memory dirs across claude-accounts
// that contain .md files, suggesting `gt quota unify-memory` to share them.
type MemorySymlinksCheck struct {
	BaseCheck
}

// NewMemorySymlinksCheck creates a new memory symlinks check.
func NewMemorySymlinksCheck() *MemorySymlinksCheck {
	return &MemorySymlinksCheck{
		BaseCheck: BaseCheck{
			CheckName:        "memory-symlinks",
			CheckDescription: "agent memory dirs use shared symlinks",
			CheckCategory:    CategoryConfig,
		},
	}
}

// Run checks for non-symlinked memory directories with content.
func (c *MemorySymlinksCheck) Run(ctx *CheckContext) *CheckResult {
	home, err := os.UserHomeDir()
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "could not determine home directory",
		}
	}

	accountsDir := filepath.Join(home, ".claude-accounts")
	if _, err := os.Stat(accountsDir); os.IsNotExist(err) {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "no claude-accounts directory",
		}
	}

	var realDirs []string  // account/project paths with real (non-symlinked) memory dirs
	var brokenLinks []string // account/project paths with broken symlinks
	var symlinked int

	accounts, err := os.ReadDir(accountsDir)
	if err != nil {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: "could not read accounts directory",
		}
	}

	for _, acct := range accounts {
		if !acct.IsDir() {
			continue
		}
		projectsDir := filepath.Join(accountsDir, acct.Name(), "projects")
		projects, err := os.ReadDir(projectsDir)
		if err != nil {
			continue
		}

		for _, proj := range projects {
			if !proj.IsDir() {
				continue
			}
			memoryDir := filepath.Join(projectsDir, proj.Name(), "memory")
			info, err := os.Lstat(memoryDir)
			if err != nil {
				continue
			}

			if info.Mode()&fs.ModeSymlink != 0 {
				// Verify the symlink target actually exists.
				if _, statErr := os.Stat(memoryDir); statErr != nil {
					brokenLinks = append(brokenLinks, fmt.Sprintf("%s/%s (broken symlink)", acct.Name(), proj.Name()))
				} else {
					symlinked++
				}
				continue
			}

			// Real directory — check if it has any .md files.
			if hasMDFiles(memoryDir) {
				realDirs = append(realDirs, fmt.Sprintf("%s/%s", acct.Name(), proj.Name()))
			}
		}
	}

	if len(realDirs) == 0 && len(brokenLinks) == 0 {
		msg := "all memory dirs use shared symlinks"
		if symlinked == 0 {
			msg = "no memory directories found"
		}
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: msg,
		}
	}

	details := make([]string, 0, len(realDirs)+len(brokenLinks))
	for _, d := range brokenLinks {
		details = append(details, d)
	}
	for _, d := range realDirs {
		details = append(details, d)
	}

	totalIssues := len(realDirs) + len(brokenLinks)
	msg := fmt.Sprintf("%d memory dir(s) not using shared symlinks", len(realDirs))
	if len(brokenLinks) > 0 {
		msg = fmt.Sprintf("%d issue(s): %d non-symlinked, %d broken symlinks", totalIssues, len(realDirs), len(brokenLinks))
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: msg,
		Details: details,
		FixHint: "Run 'gt quota unify-memory' to share memory across accounts",
	}
}

// hasMDFiles returns true if the directory contains at least one .md file.
func hasMDFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
			return true
		}
	}
	return false
}
