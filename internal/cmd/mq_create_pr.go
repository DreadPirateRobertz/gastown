package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/style"
)

var mqCreatePRCmd = &cobra.Command{
	Use:   "create-pr <rig> <mr-id>",
	Short: "Create a GitHub PR from a merge request",
	Long: `Create a GitHub PR instead of merging directly.

Used by PR-mode rigs where the refinery creates PRs for human review
instead of merging directly to the target branch.

This command:
  1. Reads the MR bead to get branch, target, and source issue
  2. Creates a GitHub PR from the polecat branch to the target branch
  3. Closes the MR bead with reason "pr-created: #<pr-number>"
  4. Deletes the original polecat branch (after pushing to a clean PR branch)

The PR title and body are generated from the source issue metadata.

Examples:
  gt mq create-pr gastown gt-mr-abc123`,
	Args: cobra.ExactArgs(2),
	RunE: runMQCreatePR,
}

func init() {
	mqCmd.AddCommand(mqCreatePRCmd)
}

func runMQCreatePR(_ *cobra.Command, args []string) error {
	rigName := args[0]
	mrID := args[1]

	mgr, r, _, err := getRefineryManager(rigName)
	if err != nil {
		return err
	}

	// Find the MR
	mr, err := mgr.FindMR(mrID)
	if err != nil {
		return fmt.Errorf("finding MR: %w", err)
	}

	if mr.Branch == "" {
		return fmt.Errorf("MR %s has no branch", mrID)
	}

	// Determine target branch
	target := mr.TargetBranch
	if target == "" {
		target = "main"
	}

	// Get source issue title for PR description
	b := beads.New(r.BeadsPath())
	issueTitle := mr.IssueID
	if mr.IssueID != "" {
		if issue, err := b.Show(mr.IssueID); err == nil {
			issueTitle = issue.Title
		}
	}

	// Get git client for the rig
	rigGit, err := getRigGit(r.Path)
	if err != nil {
		return fmt.Errorf("getting rig git: %w", err)
	}

	// Create a clean PR branch name from the polecat branch.
	// polecat/goose/gt-abc@xyz → gt/gt-abc
	prBranch := buildPRBranchName(mr.Branch, mr.IssueID)

	// Push polecat branch to the clean PR branch name
	refspec := fmt.Sprintf("origin/%s:refs/heads/%s", mr.Branch, prBranch)
	fmt.Printf("Pushing to PR branch: %s\n", prBranch)
	if err := rigGit.Push("origin", refspec, false); err != nil {
		// If the clean branch push fails, try creating PR from the polecat branch directly
		fmt.Printf("  %s Could not create clean PR branch: %v\n", style.Warning.Render("⚠"), err)
		fmt.Printf("  Using polecat branch directly: %s\n", mr.Branch)
		prBranch = mr.Branch
	}

	// Create GitHub PR using gh CLI
	prTitle := fmt.Sprintf("%s (%s)", issueTitle, mr.IssueID)
	prBody := fmt.Sprintf("Source issue: %s\nWorker: %s\nMR: %s\n\nCreated by Gas Town Refinery.",
		mr.IssueID, mr.Worker, mrID)

	prNumber, prURL, err := createGitHubPR(r.Path, prBranch, target, prTitle, prBody)
	if err != nil {
		return fmt.Errorf("creating GitHub PR: %w", err)
	}

	fmt.Printf("%s Created PR #%d: %s\n", style.Bold.Render("✓"), prNumber, prURL)

	// Close MR bead with PR reference
	closeReason := fmt.Sprintf("pr-created: #%d", prNumber)
	if err := b.CloseWithReason(closeReason, mrID); err != nil {
		fmt.Printf("  %s closing MR bead: %v\n", style.Warning.Render("⚠"), err)
	} else {
		fmt.Printf("%s MR bead closed: %s\n", style.Bold.Render("✓"), mrID)
	}

	// Close the source issue with PR reference
	if mr.IssueID != "" {
		sourceCloseReason := fmt.Sprintf("PR created: #%d", prNumber)
		if err := b.ForceCloseWithReason(sourceCloseReason, mr.IssueID); err != nil {
			// Check if already closed
			if issue, showErr := b.Show(mr.IssueID); showErr == nil && beads.IssueStatus(issue.Status).IsTerminal() {
				fmt.Printf("  %s Source issue already closed: %s\n", style.Dim.Render("○"), mr.IssueID)
			} else {
				fmt.Printf("  %s closing source issue: %v\n", style.Warning.Render("⚠"), err)
			}
		} else {
			fmt.Printf("%s Source issue closed: %s\n", style.Bold.Render("✓"), mr.IssueID)
		}
	}

	// Delete original polecat branch if it differs from PR branch
	if prBranch != mr.Branch {
		if err := rigGit.DeleteRemoteBranch("origin", mr.Branch); err != nil {
			fmt.Printf("  %s deleting polecat branch: %v\n", style.Warning.Render("⚠"), err)
		} else {
			fmt.Printf("%s Deleted polecat branch: %s\n", style.Bold.Render("✓"), mr.Branch)
		}
	}

	// Also clean up local branch
	if err := rigGit.DeleteBranch(mr.Branch, true); err != nil {
		_ = err // Not a warning — local branch often doesn't exist
	}

	return nil
}

// buildPRBranchName creates a clean branch name for the GitHub PR.
// polecat/goose/gt-abc@xyz → gt/gt-abc
// polecat/nux/gt-abc → gt/gt-abc
func buildPRBranchName(polecatBranch, issueID string) string {
	// Use issue ID if available for a clean name
	if issueID != "" {
		return "gt/" + issueID
	}
	// Strip polecat prefix and timestamp suffix
	name := strings.TrimPrefix(polecatBranch, "polecat/")
	// Remove worker name: "goose/gt-abc@xyz" → "gt-abc@xyz"
	if idx := strings.Index(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	// Remove timestamp suffix: "gt-abc@xyz" → "gt-abc"
	if idx := strings.Index(name, "@"); idx >= 0 {
		name = name[:idx]
	}
	return "gt/" + name
}

// createGitHubPR creates a GitHub PR using the gh CLI.
// Returns the PR number, URL, and any error.
func createGitHubPR(workDir, head, base, title, body string) (int, string, error) {
	args := []string{
		"pr", "create",
		"--head", head,
		"--base", base,
		"--title", title,
		"--body", body,
	}

	cmd := exec.Command("gh", args...)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("gh pr create failed: %w\nOutput: %s", err, string(out))
	}

	// gh pr create outputs the PR URL on success
	prURL := strings.TrimSpace(string(out))

	// Parse PR number from URL (e.g., https://github.com/owner/repo/pull/123)
	prNumber := 0
	if parts := strings.Split(prURL, "/"); len(parts) > 0 {
		lastPart := parts[len(parts)-1]
		if _, err := fmt.Sscanf(lastPart, "%d", &prNumber); err != nil {
			// URL parsing failed, use 0
			prNumber = 0
		}
	}

	return prNumber, prURL, nil
}
