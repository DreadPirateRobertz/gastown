package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/consensus"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/tmux"
)

var (
	consensusTimeout  time.Duration
	consensusJSON     bool
	consensusDryRun   bool
	consensusSessions []string
)

func init() {
	consensusCmd.Flags().DurationVar(&consensusTimeout, "timeout", 5*time.Minute, "Per-session wait timeout")
	consensusCmd.Flags().BoolVar(&consensusJSON, "json", false, "Output results as JSON")
	consensusCmd.Flags().BoolVar(&consensusDryRun, "dry-run", false, "Show target sessions without sending")
	consensusCmd.Flags().StringSliceVar(&consensusSessions, "session", nil, "Target specific sessions (repeatable)")
	rootCmd.AddCommand(consensusCmd)
}

var consensusCmd = &cobra.Command{
	Use:     "consensus <prompt>",
	Aliases: []string{"fanout"},
	GroupID: GroupWork,
	Short:   "Fan-out a prompt to multiple sessions and collect responses",
	Long: `Send the same prompt to multiple AI agent sessions in parallel
and collect their responses for comparison. Supports Claude, Gemini,
Codex, and other providers via GT_AGENT session detection.

By default, targets all idle crew and polecat sessions.
Use --session to target specific sessions.

Examples:
  gt consensus "What time is it?"
  gt consensus --timeout 10m "Summarize the current PR"
  gt consensus --session gt-crew-bear --session gt-crew-cat "test prompt"
  gt consensus --dry-run "show targets"
  gt consensus --json "prompt" | jq .`,
	Args: cobra.ExactArgs(1),
	RunE: runConsensus,
}

func runConsensus(cmd *cobra.Command, args []string) error {
	prompt := args[0]
	if prompt == "" {
		return fmt.Errorf("prompt cannot be empty")
	}

	// Resolve target sessions.
	sessions, err := resolveConsensusSessions()
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		fmt.Println("No idle sessions found to target.")
		return nil
	}

	// Dry run: show what would be targeted with provider info.
	if consensusDryRun {
		t := tmux.NewTmux()
		fmt.Printf("Would fan-out to %d session(s):\n\n", len(sessions))
		for _, s := range sessions {
			agent, err := t.GetEnvironment(s, "GT_AGENT")
			if err != nil || agent == "" {
				agent = "claude"
			}
			detection := "prompt"
			if preset := config.GetAgentPresetByName(agent); preset != nil {
				if preset.ReadyPromptPrefix == "" {
					detection = fmt.Sprintf("delay (%dms)", preset.ReadyDelayMs)
				}
			}
			fmt.Printf("  %-30s [%s, %s]\n", s, agent, detection)
		}
		fmt.Printf("\nPrompt: %s\n", prompt)
		fmt.Printf("Timeout: %s\n", consensusTimeout)
		return nil
	}

	// Run the consensus.
	t := tmux.NewTmux()
	runner := consensus.NewRunner(t)

	fmt.Printf("Sending prompt to %d session(s)...\n\n", len(sessions))

	result := runner.Run(consensus.Request{
		Prompt:   prompt,
		Sessions: sessions,
		Timeout:  consensusTimeout,
	})

	// Output results.
	if consensusJSON {
		return outputConsensusJSON(result)
	}
	outputConsensusText(result)
	return nil
}

// resolveConsensusSessions determines which sessions to target.
// Uses provider-aware idle detection instead of Claude-specific IsIdle.
func resolveConsensusSessions() ([]string, error) {
	t := tmux.NewTmux()

	// If explicit sessions specified, use those (check idle status per provider).
	if len(consensusSessions) > 0 {
		var idle []string
		for _, s := range consensusSessions {
			if isSessionIdleForConsensus(t, s) {
				idle = append(idle, s)
			} else {
				fmt.Printf("%s %s (not idle, skipping)\n", style.WarningPrefix, s)
			}
		}
		return idle, nil
	}

	// Default: all idle crew + polecat sessions.
	agents, err := getAgentSessions(true) // include polecats
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}

	// Exclude self.
	self := os.Getenv("BD_ACTOR")

	var sessions []string
	for _, agent := range agents {
		if agent.Type != AgentCrew && agent.Type != AgentPolecat {
			continue
		}
		name := formatAgentName(agent)
		if self != "" && name == self {
			continue
		}
		if isSessionIdleForConsensus(t, agent.Name) {
			sessions = append(sessions, agent.Name)
		}
	}
	return sessions, nil
}

// isSessionIdleForConsensus checks idle status using provider-aware detection.
// Reads GT_AGENT from the session to determine the correct idle detection strategy.
func isSessionIdleForConsensus(t *tmux.Tmux, session string) bool {
	agent, err := t.GetEnvironment(session, "GT_AGENT")
	if err != nil || agent == "" {
		agent = "claude"
	}

	preset := config.GetAgentPresetByName(agent)
	if preset == nil {
		// Unknown agent, fall back to generic IsIdle.
		return t.IsIdle(session)
	}

	// If the provider has a ReadyPromptPrefix, check pane lines for it.
	if preset.ReadyPromptPrefix != "" {
		lines, err := t.CapturePaneLines(session, 5)
		if err != nil {
			return false
		}
		hasPrompt := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			trimmed = strings.ReplaceAll(trimmed, "\u00a0", " ")
			normalizedPrefix := strings.ReplaceAll(preset.ReadyPromptPrefix, "\u00a0", " ")
			pfx := strings.TrimSpace(normalizedPrefix)
			if strings.HasPrefix(trimmed, normalizedPrefix) || (pfx != "" && trimmed == pfx) {
				hasPrompt = true
			}
			// Claude-specific: if status bar says "esc to interrupt", agent is busy.
			if strings.Contains(strings.TrimSpace(line), "esc to interrupt") {
				return false
			}
		}
		return hasPrompt
	}

	// No prompt prefix — fall back to generic IsIdle.
	return t.IsIdle(session)
}

func outputConsensusJSON(result *consensus.Result) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func outputConsensusText(result *consensus.Result) {
	for i, sr := range result.Sessions {
		if i > 0 {
			fmt.Println(strings.Repeat("─", 60))
		}

		statusIcon := style.SuccessPrefix
		switch sr.Status {
		case consensus.StatusTimeout:
			statusIcon = style.WarningPrefix
		case consensus.StatusError, consensus.StatusRateLimited:
			statusIcon = style.ErrorPrefix
		case consensus.StatusNotIdle:
			statusIcon = style.WarningPrefix
		}

		providerLabel := ""
		if sr.Provider != "" {
			providerLabel = fmt.Sprintf(" [%s]", sr.Provider)
		}
		fmt.Printf("%s %s%s  (%s, %s)\n", statusIcon, sr.Session, providerLabel, sr.Status, sr.Duration.Round(time.Millisecond))

		if sr.Error != "" {
			fmt.Printf("  Error: %s\n", sr.Error)
		}
		if sr.Response != "" {
			fmt.Println()
			// Indent response lines for readability.
			for _, line := range strings.Split(sr.Response, "\n") {
				fmt.Printf("  %s\n", line)
			}
			fmt.Println()
		}
	}

	fmt.Printf("\nTotal: %d session(s), %s\n", len(result.Sessions), result.Duration.Round(time.Millisecond))
}
