package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
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
// Strips inherited BEADS_DIR and resolves the rig directory from the bead's
// prefix so bd's native routing finds rig-level beads correctly.
func execBdShow(args []string) error {
	bdPath, err := exec.LookPath("bd")
	if err != nil {
		return fmt.Errorf("bd not found in PATH: %w", err)
	}

	// Strip inherited BEADS_DIR to enable bd's native prefix-based routing.
	// Without this, an inherited BEADS_DIR (e.g., pointing to HQ) overrides
	// routing and causes "bead not found" for rig-level beads. See #2126.
	env := filterEnvKey(os.Environ(), "BEADS_DIR")

	// Resolve the rig directory from the first non-flag argument (the bead ID).
	// This ensures bd runs from the correct rig directory where it can discover
	// the rig's .beads database via cwd-based lookup.
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			if dir := resolveBeadDir(arg); dir != "" && dir != "." {
				_ = os.Chdir(dir)
			}
			break
		}
	}

	// Build args: bd show <all-args>
	// argv[0] must be the program name for exec
	fullArgs := append([]string{"bd", "show"}, args...)

	return syscall.Exec(bdPath, fullArgs, env)
}
