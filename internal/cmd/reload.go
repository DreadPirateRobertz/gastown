package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/daemon"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/workspace"
)

var reloadCmd = &cobra.Command{
	Use:     "reload",
	GroupID: GroupConfig,
	Short:   "Hot-reload configuration and environment variables",
	Long: `Reload config.json and env vars without restarting agent sessions.

Validates configuration files, signals the daemon to reload its config
(patrol config, env vars, operational settings), and updates tmux
global environment variables so running sessions pick up changes.

What gets reloaded:
  - Town settings (settings/config.json)
  - Patrol/daemon config (mayor/daemon.json)
  - Environment variables from daemon.json env section
  - Tmux global environment variables

Examples:
  gt reload              # Reload all config
  gt reload --dry-run    # Validate config without applying`,
	RunE: runReload,
}

var reloadDryRun bool

func init() {
	reloadCmd.Flags().BoolVar(&reloadDryRun, "dry-run", false, "Validate config files without applying changes")
	rootCmd.AddCommand(reloadCmd)
}

func runReload(cmd *cobra.Command, args []string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}

	// 1. Validate town settings
	settingsPath := config.TownSettingsPath(townRoot)
	townSettings, err := config.LoadOrCreateTownSettings(settingsPath)
	if err != nil {
		return fmt.Errorf("invalid town settings (%s): %w", settingsPath, err)
	}
	fmt.Printf("%s Town settings validated (%s)\n", style.Bold.Render("✓"), settingsPath)

	// 2. Validate patrol/daemon config
	patrolConfig := daemon.LoadPatrolConfig(townRoot)
	patrolPath := daemon.PatrolConfigFile(townRoot)
	if patrolConfig != nil {
		fmt.Printf("%s Patrol config validated (%s)\n", style.Bold.Render("✓"), patrolPath)
	} else {
		fmt.Printf("%s No patrol config found (using defaults)\n", style.Dim.Render("-"))
	}

	// 3. Validate rig configs
	rigsPath := filepath.Join(townRoot, "mayor", "rigs.json")
	if _, err := config.LoadRigsConfig(rigsPath); err != nil {
		fmt.Printf("%s Rigs config warning: %v\n", style.WarningPrefix, err)
	} else {
		fmt.Printf("%s Rigs config validated (%s)\n", style.Bold.Render("✓"), rigsPath)
	}

	if reloadDryRun {
		fmt.Printf("\n%s Dry run complete — config files are valid\n", style.Bold.Render("✓"))
		return nil
	}

	// 4. Update tmux global env vars from daemon.json
	envCount := 0
	if patrolConfig != nil {
		t := tmux.NewTmux()
		for k, v := range patrolConfig.Env {
			if err := t.SetGlobalEnvironment(k, v); err != nil {
				fmt.Printf("%s Failed to set tmux env %s: %v\n", style.WarningPrefix, k, err)
			} else {
				envCount++
			}
		}
	}

	// Also propagate key town settings as env vars
	if townSettings.DefaultAgent != "" {
		t := tmux.NewTmux()
		if err := t.SetGlobalEnvironment("GT_DEFAULT_AGENT", townSettings.DefaultAgent); err != nil {
			fmt.Printf("%s Failed to set GT_DEFAULT_AGENT: %v\n", style.WarningPrefix, err)
		} else {
			envCount++
		}
	}

	if envCount > 0 {
		fmt.Printf("%s Updated %d tmux environment variable(s)\n", style.Bold.Render("✓"), envCount)
	}

	// 5. Signal daemon to reload config
	running, pid, err := daemon.IsRunning(townRoot)
	if err != nil {
		return fmt.Errorf("checking daemon status: %w", err)
	}
	if running {
		process, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("finding daemon process (PID %d): %w", pid, err)
		}
		if err := signalDaemonConfigReload(process); err != nil {
			return fmt.Errorf("signaling daemon to reload: %w", err)
		}
		fmt.Printf("%s Daemon signaled to reload config (PID %d)\n", style.Bold.Render("✓"), pid)
	} else {
		fmt.Printf("%s Daemon not running (config will be loaded on next start)\n", style.Dim.Render("-"))
	}

	fmt.Printf("\n%s Configuration reloaded\n", style.Bold.Render("✓"))
	return nil
}
