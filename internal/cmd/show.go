package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/workspace"
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
// Resolves the correct rig directory from the bead's prefix before exec,
// so that bd finds the right Dolt database for cross-rig beads.
func execBdShow(args []string) error {
	bdPath, err := exec.LookPath("bd")
	if err != nil {
		return fmt.Errorf("bd not found in PATH: %w", err)
	}

	// Resolve the rig directory from the bead ID prefix.
	// The first non-flag argument is the bead ID.
	if beadID := firstNonFlagArg(args); beadID != "" {
		if dir := resolveBeadDirForShow(beadID); dir != "" {
			// syscall.Exec replaces the process, so os.Chdir is the only
			// way to set the working directory for the new process.
			if err := os.Chdir(dir); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not chdir to %s: %v\n", dir, err)
			}
		}
	}

	// Build args: bd show <all-args>
	// argv[0] must be the program name for exec
	fullArgs := append([]string{"bd", "show"}, args...)

	return syscall.Exec(bdPath, fullArgs, os.Environ())
}

// resolveBeadDirForShow returns the rig directory for a bead ID using
// prefix-based routing. Returns empty string if routing is unavailable.
func resolveBeadDirForShow(beadID string) string {
	townRoot, err := workspace.FindFromCwd()
	if err != nil {
		return ""
	}
	prefix := beads.ExtractPrefix(beadID)
	if prefix == "" {
		return ""
	}
	if rigPath := beads.GetRigPathForPrefix(townRoot, prefix); rigPath != "" {
		return rigPath
	}
	// Fallback: consult rigs.json
	if rigDir := resolveBeadDirFromRigsJSON(townRoot, prefix); rigDir != "" {
		return rigDir
	}
	return ""
}

// firstNonFlagArg returns the first argument that doesn't start with '-'.
func firstNonFlagArg(args []string) string {
	for _, a := range args {
		if !isFlag(a) {
			return a
		}
	}
	return ""
}

// isFlag returns true if the argument looks like a CLI flag.
func isFlag(arg string) bool {
	return len(arg) > 0 && arg[0] == '-'
}
