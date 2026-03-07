package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRateLimitStatePersistence(t *testing.T) {
	townRoot := t.TempDir()
	runtimeDir := filepath.Join(townRoot, "mayor", ".runtime")
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Initially no state
	state := loadRateLimitState(townRoot)
	if state != nil {
		t.Error("expected nil state initially")
	}

	// Save state
	now := time.Now().UTC().Truncate(time.Second)
	resetAt := now.Add(2 * time.Hour)
	saveState := &RateLimitState{
		DetectedAt:       now,
		ResetsAt:         resetAt,
		RawResetText:     "5pm (America/Los_Angeles)",
		AffectedSessions: []string{"gt-rockryder", "gt-witness", "hq-deacon"},
	}
	if err := saveRateLimitState(townRoot, saveState); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Load state
	loaded := loadRateLimitState(townRoot)
	if loaded == nil {
		t.Fatal("expected non-nil state after save")
	}
	if loaded.RawResetText != "5pm (America/Los_Angeles)" {
		t.Errorf("RawResetText = %q, want %q", loaded.RawResetText, "5pm (America/Los_Angeles)")
	}
	if len(loaded.AffectedSessions) != 3 {
		t.Errorf("AffectedSessions = %d, want 3", len(loaded.AffectedSessions))
	}
	if loaded.Recovered {
		t.Error("expected Recovered=false")
	}

	// Clear state
	clearRateLimitState(townRoot)
	if loadRateLimitState(townRoot) != nil {
		t.Error("expected nil state after clear")
	}
}

func TestIsPolecatSession(t *testing.T) {
	// isPolecatSession uses ParseSessionName which requires the prefix registry.
	// Since the daemon test suite gates on Docker via TestMain, we only test
	// the infrastructure sessions that have fixed prefixes (hq-).
	tests := []struct {
		session string
		want    bool
	}{
		{"hq-deacon", false},
		{"hq-mayor", false},
	}
	for _, tt := range tests {
		t.Run(tt.session, func(t *testing.T) {
			got := isPolecatSession(tt.session)
			if got != tt.want {
				t.Errorf("isPolecatSession(%q) = %v, want %v", tt.session, got, tt.want)
			}
		})
	}
}

func TestRateLimitScanPatterns(t *testing.T) {
	// Verify patterns are compiled successfully
	if len(rateLimitScanPatterns) == 0 {
		t.Fatal("expected at least one compiled rate limit pattern")
	}

	// Test known rate limit messages
	messages := []string{
		"You've hit your usage limit",
		"You've hit your rate limit",
		"Stop and wait for limit to reset",
		"Add funds to continue with extra usage",
		"API Error: Rate limit reached",
	}
	for _, msg := range messages {
		matched := false
		for _, re := range rateLimitScanPatterns {
			if re.MatchString(msg) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("message %q did not match any rate limit pattern", msg)
		}
	}

	// Test non-rate-limit messages
	nonMessages := []string{
		"Hello world",
		"Running tests...",
		"$ git status",
	}
	for _, msg := range nonMessages {
		for _, re := range rateLimitScanPatterns {
			if re.MatchString(msg) {
				t.Errorf("non-rate-limit message %q matched pattern %v", msg, re)
			}
		}
	}
}

func TestResetTimeExtraction(t *testing.T) {
	tests := []struct {
		line     string
		wantText string
	}{
		{"You're out of extra usage · resets 5pm (America/Los_Angeles)", "5pm (America/Los_Angeles)"},
		{"resets 3:00 AM PST", "3:00 AM PST"},
		{"No reset time here", ""},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			m := rateLimitResetPattern.FindStringSubmatch(tt.line)
			got := ""
			if len(m) >= 2 {
				got = m[1]
			}
			// Trim for comparison
			got = trimS(got)
			if got != tt.wantText {
				t.Errorf("extracted %q, want %q", got, tt.wantText)
			}
		})
	}
}

func trimS(s string) string {
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
