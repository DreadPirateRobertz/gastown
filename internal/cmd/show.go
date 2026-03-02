package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
)

func init() {
	showCmd.GroupID = GroupWork
	rootCmd.AddCommand(showCmd)
}

var showCmd = &cobra.Command{
	Use:   "show <bead-id> [flags]",
	Short: "Show details of a bead",
	Long: `Displays the full details of a bead by ID.

Delegates to 'bd show' - all bd show flags are supported.
Works with any bead prefix (gt-, bd-, hq-, etc.) and routes
to the correct beads database automatically.

Examples:
  gt show gt-abc123          # Show a gastown issue
  gt show hq-xyz789          # Show a town-level bead (convoy, mail, etc.)
  gt show bd-def456          # Show a beads issue
  gt show gt-abc123 --json   # Output as JSON
  gt show gt-abc123 -v       # Verbose output`,
	DisableFlagParsing: true, // Pass all flags through to bd show
	RunE:               runShow,
}

func runShow(cmd *cobra.Command, args []string) error {
	// Handle --help since DisableFlagParsing bypasses Cobra's help handling
	if helped, err := checkHelpFlag(cmd, args); helped || err != nil {
		return err
	}

	if len(args) == 0 {
		return fmt.Errorf("bead ID required\n\nUsage: gt show <bead-id> [flags]")
	}

	return execBdShow(args)
}

// execBdShow replaces the current process with 'bd show'.
// Resolves the correct working directory from the bead ID's prefix
// so that bd routes to the correct rig database (GH#2126).
func execBdShow(args []string) error {
	bdPath, err := exec.LookPath("bd")
	if err != nil {
		return fmt.Errorf("bd not found in PATH: %w", err)
	}

	// Resolve the rig directory from the bead ID prefix.
	// Without this, bd runs in the caller's cwd and only finds town-level
	// (hq-*) beads, failing for rig-prefixed beads like gt-*, bd-*, etc.
	if len(args) > 0 {
		beadDir := resolveBeadDir(args[0])
		if beadDir != "" && beadDir != "." {
			if err := os.Chdir(beadDir); err != nil {
				// Non-fatal: fall through to exec from current directory.
				// bd may still resolve via its own routing.
				fmt.Fprintf(os.Stderr, "Warning: could not chdir to %s: %v\n", beadDir, err)
			}
		}
	}

	// Build args: bd show <all-args>
	// argv[0] must be the program name for exec
	fullArgs := append([]string{"bd", "show"}, args...)

	return syscall.Exec(bdPath, fullArgs, os.Environ())
}
