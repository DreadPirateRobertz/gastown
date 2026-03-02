package consensus

import (
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/config"
)

// ProviderInfo holds idle-detection parameters for an agent provider.
type ProviderInfo struct {
	Name         string // e.g. "claude", "gemini", "codex"
	PromptPrefix string // prompt prefix to poll for (empty = delay-based)
	DelayMs      int    // fixed delay fallback (0 = none)
}

// resolveProvider looks up GT_AGENT on the session and maps it to a ProviderInfo.
// Defaults to Claude if GT_AGENT is not set.
func resolveProvider(tmux TmuxClient, session string) ProviderInfo {
	agent, err := tmux.GetEnvironment(session, "GT_AGENT")
	if err != nil || agent == "" {
		agent = "claude"
	}

	preset := config.GetAgentPresetByName(agent)
	if preset != nil {
		return ProviderInfo{
			Name:         agent,
			PromptPrefix: preset.ReadyPromptPrefix,
			DelayMs:      preset.ReadyDelayMs,
		}
	}

	// Unknown agent — use delay-based detection.
	return ProviderInfo{
		Name:    agent,
		DelayMs: 5000,
	}
}

// isSessionIdle checks whether a session is idle using provider-aware detection.
//
// Strategy:
//  1. If the provider has a PromptPrefix, check last N pane lines for it.
//     For Claude, also check for "esc to interrupt" in the status bar.
//  2. Otherwise, fall back to the generic tmux.IsIdle (status bar check).
func isSessionIdle(tmux TmuxClient, session string, provider ProviderInfo) bool {
	if provider.PromptPrefix != "" {
		lines, err := tmux.CapturePaneLines(session, 5)
		if err != nil {
			return false
		}

		hasPrompt := false
		for _, line := range lines {
			if matchesPromptPrefixLocal(line, provider.PromptPrefix) {
				hasPrompt = true
			}
			// Claude-specific: if status bar says "esc to interrupt", agent is busy.
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, "esc to interrupt") {
				return false
			}
		}
		return hasPrompt
	}

	// No prompt prefix — fall back to generic tmux idle check.
	return tmux.IsIdle(session)
}

// waitForIdle polls until the session appears idle or times out.
//
// Three-tier detection:
//  1. If PromptPrefix is set, poll for it in the pane output.
//  2. If only DelayMs is set, wait that fixed duration.
//  3. If neither, return an error.
func waitForIdle(tmux TmuxClient, session string, provider ProviderInfo, timeout time.Duration) error {
	if provider.PromptPrefix != "" {
		return pollForPrompt(tmux, session, provider, timeout)
	}
	if provider.DelayMs > 0 {
		time.Sleep(time.Duration(provider.DelayMs) * time.Millisecond)
		return nil
	}
	return errNoDetection
}

// pollForPrompt checks the session's last lines for the provider prompt prefix,
// polling at 500ms intervals until found or timeout.
func pollForPrompt(tmux TmuxClient, session string, provider ProviderInfo, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	pollInterval := 500 * time.Millisecond

	for {
		lines, err := tmux.CapturePaneLines(session, 5)
		if err == nil {
			busy := false
			hasPrompt := false
			for _, line := range lines {
				if matchesPromptPrefixLocal(line, provider.PromptPrefix) {
					hasPrompt = true
				}
				trimmed := strings.TrimSpace(line)
				if strings.Contains(trimmed, "esc to interrupt") {
					busy = true
				}
			}
			if hasPrompt && !busy {
				return nil
			}
		}

		if time.Now().After(deadline) {
			return errIdleTimeout
		}
		time.Sleep(pollInterval)
	}
}

// matchesPromptPrefixLocal checks if a line matches a prompt prefix.
// Normalizes NBSP to regular space for matching.
func matchesPromptPrefixLocal(line, prefix string) bool {
	if prefix == "" {
		return false
	}
	trimmed := strings.TrimSpace(line)
	trimmed = strings.ReplaceAll(trimmed, "\u00a0", " ")
	normalizedPrefix := strings.ReplaceAll(prefix, "\u00a0", " ")
	pfx := strings.TrimSpace(normalizedPrefix)
	return strings.HasPrefix(trimmed, normalizedPrefix) || (pfx != "" && trimmed == pfx)
}

// stripPrompt removes trailing prompt lines and status bar lines from a response.
func stripPrompt(response string, provider ProviderInfo) string {
	if response == "" || provider.PromptPrefix == "" {
		return response
	}

	lines := strings.Split(response, "\n")

	// Remove trailing lines that are prompt or status bar.
	for len(lines) > 0 {
		last := lines[len(lines)-1]
		trimmed := strings.TrimSpace(last)
		if trimmed == "" || matchesPromptPrefixLocal(last, provider.PromptPrefix) || strings.HasPrefix(trimmed, "⏵⏵") {
			lines = lines[:len(lines)-1]
			continue
		}
		break
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}
