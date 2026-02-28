package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTouchHeartbeat_CreatesFile(t *testing.T) {
	townRoot := t.TempDir()
	sessionID := "gt-test-session"

	if err := TouchHeartbeat(townRoot, sessionID); err != nil {
		t.Fatalf("TouchHeartbeat: %v", err)
	}

	path := heartbeatFile(townRoot, sessionID)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("heartbeat file not created: %v", err)
	}
}

func TestTouchHeartbeat_UpdatesMtime(t *testing.T) {
	townRoot := t.TempDir()
	sessionID := "gt-test-session"

	if err := TouchHeartbeat(townRoot, sessionID); err != nil {
		t.Fatalf("first touch: %v", err)
	}

	path := heartbeatFile(townRoot, sessionID)
	info1, _ := os.Stat(path)

	// Set mtime to the past
	past := time.Now().Add(-10 * time.Minute)
	_ = os.Chtimes(path, past, past)

	if err := TouchHeartbeat(townRoot, sessionID); err != nil {
		t.Fatalf("second touch: %v", err)
	}

	info2, _ := os.Stat(path)
	if !info2.ModTime().After(info1.ModTime().Add(-1 * time.Second)) {
		t.Errorf("mtime not updated: before=%v after=%v", info1.ModTime(), info2.ModTime())
	}
}

func TestHeartbeatAge_NoFile(t *testing.T) {
	townRoot := t.TempDir()
	age := HeartbeatAge(townRoot, "nonexistent")
	if age < 24*time.Hour {
		t.Errorf("expected very large age for missing file, got %v", age)
	}
}

func TestHeartbeatAge_FreshFile(t *testing.T) {
	townRoot := t.TempDir()
	sessionID := "gt-test-fresh"

	if err := TouchHeartbeat(townRoot, sessionID); err != nil {
		t.Fatalf("TouchHeartbeat: %v", err)
	}

	age := HeartbeatAge(townRoot, sessionID)
	if age > 2*time.Second {
		t.Errorf("expected fresh heartbeat, got age %v", age)
	}
}

func TestIsHeartbeatStale_Fresh(t *testing.T) {
	townRoot := t.TempDir()
	sessionID := "gt-test-stale"

	if err := TouchHeartbeat(townRoot, sessionID); err != nil {
		t.Fatalf("TouchHeartbeat: %v", err)
	}

	if IsHeartbeatStale(townRoot, sessionID, HeartbeatStaleThreshold) {
		t.Error("fresh heartbeat should not be stale")
	}
}

func TestIsHeartbeatStale_Old(t *testing.T) {
	townRoot := t.TempDir()
	sessionID := "gt-test-old"

	if err := TouchHeartbeat(townRoot, sessionID); err != nil {
		t.Fatalf("TouchHeartbeat: %v", err)
	}

	// Set mtime to the past
	path := heartbeatFile(townRoot, sessionID)
	past := time.Now().Add(-10 * time.Minute)
	_ = os.Chtimes(path, past, past)

	if !IsHeartbeatStale(townRoot, sessionID, HeartbeatStaleThreshold) {
		t.Error("old heartbeat should be stale")
	}
}

func TestIsHeartbeatStale_NoFile(t *testing.T) {
	townRoot := t.TempDir()
	if !IsHeartbeatStale(townRoot, "nonexistent", HeartbeatStaleThreshold) {
		t.Error("missing heartbeat should be stale")
	}
}

func TestCleanupHeartbeat(t *testing.T) {
	townRoot := t.TempDir()
	sessionID := "gt-test-cleanup"

	if err := TouchHeartbeat(townRoot, sessionID); err != nil {
		t.Fatalf("TouchHeartbeat: %v", err)
	}

	CleanupHeartbeat(townRoot, sessionID)

	path := heartbeatFile(townRoot, sessionID)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("heartbeat file should be removed after cleanup")
	}
}

func TestCleanupHeartbeat_NoFile(t *testing.T) {
	townRoot := t.TempDir()
	// Should not panic or error on missing file.
	CleanupHeartbeat(townRoot, "nonexistent")
}

func TestHeartbeatsDir(t *testing.T) {
	got := heartbeatsDir("/town")
	want := filepath.Join("/town", ".runtime", "heartbeats")
	if got != want {
		t.Errorf("heartbeatsDir() = %q, want %q", got, want)
	}
}
