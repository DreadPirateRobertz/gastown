package daemon

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/quota"
	"github.com/steveyegge/gastown/internal/session"
)

// RateLimitState tracks detected rate limits and their reset times.
// Persisted to mayor/.runtime/rate-limit-state.json.
type RateLimitState struct {
	// DetectedAt is when the rate limit was first detected.
	DetectedAt time.Time `json:"detected_at"`

	// ResetsAt is the parsed reset time (when the rate limit should lift).
	ResetsAt time.Time `json:"resets_at"`

	// RawResetText is the original reset time text from the rate limit message.
	RawResetText string `json:"raw_reset_text,omitempty"`

	// AffectedSessions lists tmux sessions that were rate-limited.
	AffectedSessions []string `json:"affected_sessions"`

	// Recovered indicates that recovery has been attempted.
	Recovered bool `json:"recovered"`
}

// rateLimitStateFile returns the path to the rate limit state file.
func rateLimitStateFile(townRoot string) string {
	return filepath.Join(townRoot, constants.DirMayor, constants.DirRuntime, "rate-limit-state.json")
}

// loadRateLimitState loads persisted rate limit state. Returns nil if no state exists.
func loadRateLimitState(townRoot string) *RateLimitState {
	data, err := os.ReadFile(rateLimitStateFile(townRoot))
	if err != nil {
		return nil
	}
	var state RateLimitState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil
	}
	return &state
}

// saveRateLimitState persists rate limit state to disk.
func saveRateLimitState(townRoot string, state *RateLimitState) error {
	path := rateLimitStateFile(townRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// clearRateLimitState removes the rate limit state file.
func clearRateLimitState(townRoot string) {
	_ = os.Remove(rateLimitStateFile(townRoot))
}

// rateLimitScanPatterns are compiled patterns for detecting rate limit messages.
// These match the patterns from constants.DefaultRateLimitPatterns.
var rateLimitScanPatterns []*regexp.Regexp

func init() {
	for _, p := range constants.DefaultRateLimitPatterns {
		re, err := regexp.Compile("(?i)" + p)
		if err != nil {
			continue
		}
		rateLimitScanPatterns = append(rateLimitScanPatterns, re)
	}
}

// rateLimitResetPattern extracts reset time from rate limit messages.
var rateLimitResetPattern = regexp.MustCompile(`(?i)\bresets\s+(.+)`)

// checkRateLimitRecovery scans for rate-limited sessions and schedules recovery.
//
// Flow:
//  1. If existing state says reset time has passed → recover (kill stuck sessions)
//  2. If existing state says still rate-limited → skip (wait for reset)
//  3. If no state → scan all sessions for rate limit indicators
//  4. If rate-limited sessions found → parse reset time, persist state
//
// Recovery kills the stuck sessions. The daemon's normal heartbeat patrols
// (ensureDeaconRunning, ensureWitnessesRunning, checkPolecatSessionHealth, etc.)
// automatically restart everything on the next cycle.
func (d *Daemon) checkRateLimitRecovery() {
	// Phase 1: Check existing state for recovery opportunity.
	state := loadRateLimitState(d.config.TownRoot)
	if state != nil && !state.Recovered {
		if !state.ResetsAt.IsZero() && time.Now().After(state.ResetsAt) {
			// Reset time has passed — recover!
			d.recoverFromRateLimit(state)
			return
		}
		// Still rate-limited. Log and wait.
		remaining := time.Until(state.ResetsAt).Round(time.Second)
		d.logger.Printf("Rate limit active, resets in %s (%s)", remaining, state.RawResetText)
		return
	}

	// Phase 2: If we recently recovered, keep the state for one more cycle
	// so we don't immediately re-detect stale rate limit messages in panes
	// that haven't been restarted yet.
	if state != nil && state.Recovered {
		if time.Since(state.ResetsAt) < 10*time.Minute {
			return // Grace period after recovery
		}
		// Grace period expired, clear state
		clearRateLimitState(d.config.TownRoot)
		return
	}

	// Phase 3: Scan all sessions for rate limit indicators.
	d.scanForRateLimits()
}

// scanForRateLimits checks all Gas Town tmux sessions for rate limit messages.
func (d *Daemon) scanForRateLimits() {
	sessions, err := d.tmux.ListSessions()
	if err != nil {
		return
	}

	var rateLimitedSessions []string
	var resetText string

	for _, sess := range sessions {
		if !session.IsKnownSession(sess) {
			continue
		}

		// Capture bottom of pane where rate limit messages appear.
		content, err := d.tmux.CapturePane(sess, 20)
		if err != nil {
			continue
		}

		lines := strings.Split(content, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			for _, re := range rateLimitScanPatterns {
				if re.MatchString(line) {
					rateLimitedSessions = append(rateLimitedSessions, sess)
					// Extract reset time if not yet found
					if resetText == "" {
						if m := rateLimitResetPattern.FindStringSubmatch(line); len(m) >= 2 {
							resetText = strings.TrimSpace(m[1])
						}
					}
					goto nextSession
				}
			}
		}
	nextSession:
	}

	if len(rateLimitedSessions) == 0 {
		return
	}

	// Parse the reset time
	now := time.Now()
	var resetsAt time.Time
	if resetText != "" {
		parsed, err := quota.ParseResetTime(resetText, now)
		if err == nil {
			resetsAt = parsed
			// If the parsed time is in the past (e.g., "resets 5pm" but it's 6pm),
			// the limit may have already expired. Add a small buffer and try recovery.
			if resetsAt.Before(now) {
				resetsAt = now.Add(1 * time.Minute)
			}
		}
	}

	// If we couldn't parse a reset time, default to checking again in 30 minutes.
	if resetsAt.IsZero() {
		resetsAt = now.Add(30 * time.Minute)
		d.logger.Printf("RATE LIMIT DETECTED: %d sessions affected, could not parse reset time, will retry in 30m",
			len(rateLimitedSessions))
	} else {
		d.logger.Printf("RATE LIMIT DETECTED: %d sessions affected, resets at %s (%s from now)",
			len(rateLimitedSessions), resetsAt.Format("15:04 MST"), time.Until(resetsAt).Round(time.Minute))
	}

	state := &RateLimitState{
		DetectedAt:       now,
		ResetsAt:         resetsAt,
		RawResetText:     resetText,
		AffectedSessions: rateLimitedSessions,
	}

	if err := saveRateLimitState(d.config.TownRoot, state); err != nil {
		d.logger.Printf("Warning: failed to save rate limit state: %v", err)
	}
}

// recoverFromRateLimit kills all rate-limited sessions so the daemon's
// normal patrol cycle can restart them with fresh sessions.
func (d *Daemon) recoverFromRateLimit(state *RateLimitState) {
	d.logger.Printf("RATE LIMIT RECOVERY: reset time passed (%s), killing %d stuck sessions",
		state.ResetsAt.Format("15:04 MST"), len(state.AffectedSessions))

	killed := 0
	for _, sess := range state.AffectedSessions {
		alive, err := d.tmux.HasSession(sess)
		if err != nil || !alive {
			continue // Session already dead
		}

		// Use gt session restart for polecats (preserves work-on-hook).
		// Kill other sessions directly (daemon will restart them).
		if isPolecatSession(sess) {
			d.restartPolecatSession(sess)
		} else {
			if err := d.tmux.KillSession(sess); err != nil {
				d.logger.Printf("  Failed to kill %s: %v", sess, err)
				continue
			}
		}
		d.logger.Printf("  Killed rate-limited session: %s", sess)
		killed++
	}

	d.logger.Printf("RATE LIMIT RECOVERY: killed %d/%d sessions, daemon will restart on next heartbeat",
		killed, len(state.AffectedSessions))

	// Mark as recovered
	state.Recovered = true
	if err := saveRateLimitState(d.config.TownRoot, state); err != nil {
		d.logger.Printf("Warning: failed to save rate limit recovery state: %v", err)
	}
}

// restartPolecatSession restarts a polecat session via gt session restart.
// This preserves the work-on-hook assignment.
func (d *Daemon) restartPolecatSession(sess string) {
	// Parse the session name to get rig/name for gt session restart.
	identity, err := session.ParseSessionName(sess)
	if err != nil || identity == nil {
		// Can't determine identity — fall back to kill
		_ = d.tmux.KillSession(sess)
		return
	}
	address := identity.Address()
	if address == "" {
		_ = d.tmux.KillSession(sess)
		return
	}

	cmd := exec.Command(d.gtPath, "session", "restart", address, "--force") //nolint:gosec // G204: args are constructed internally
	cmd.Dir = d.config.TownRoot
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		d.logger.Printf("  Failed to restart polecat %s: %v, falling back to kill", sess, err)
		_ = d.tmux.KillSession(sess)
	}
}

// isPolecatSession checks if a tmux session name belongs to a polecat.
func isPolecatSession(sess string) bool {
	identity, err := session.ParseSessionName(sess)
	if err != nil || identity == nil {
		return false
	}
	return identity.Role == session.RolePolecat
}
