package session

import (
	"os"
	"path/filepath"
	"time"
)

const (
	// HeartbeatStaleThreshold is the age at which a polecat heartbeat is
	// considered stale, indicating the agent process is likely dead.
	// Polecats touch their heartbeat file on every gt command invocation
	// (via UserPromptSubmit hook), so a 5-minute gap strongly signals death.
	HeartbeatStaleThreshold = 5 * time.Minute
)

// heartbeatsDir returns the directory for session heartbeat files.
func heartbeatsDir(townRoot string) string {
	return filepath.Join(townRoot, ".runtime", "heartbeats")
}

// heartbeatFile returns the path to a heartbeat file for a session.
func heartbeatFile(townRoot, sessionID string) string {
	return filepath.Join(heartbeatsDir(townRoot), sessionID)
}

// TouchHeartbeat creates or updates the heartbeat file for a session.
// The file's mtime serves as the heartbeat timestamp.
func TouchHeartbeat(townRoot, sessionID string) error {
	dir := heartbeatsDir(townRoot)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	path := heartbeatFile(townRoot, sessionID)
	now := time.Now()

	// Try to update mtime on existing file first (cheaper than create).
	if err := os.Chtimes(path, now, now); err == nil {
		return nil
	}

	// File doesn't exist — create it.
	return os.WriteFile(path, nil, 0644)
}

// HeartbeatAge returns how long ago the heartbeat was last updated.
// Returns a very large duration if the file doesn't exist or can't be read.
func HeartbeatAge(townRoot, sessionID string) time.Duration {
	path := heartbeatFile(townRoot, sessionID)
	info, err := os.Stat(path)
	if err != nil {
		return 24 * time.Hour * 365 // No heartbeat = very stale
	}
	return time.Since(info.ModTime())
}

// IsHeartbeatStale returns true if the session's heartbeat file is older
// than the given threshold, or doesn't exist at all.
func IsHeartbeatStale(townRoot, sessionID string, threshold time.Duration) bool {
	return HeartbeatAge(townRoot, sessionID) >= threshold
}

// CleanupHeartbeat removes the heartbeat file for a session.
func CleanupHeartbeat(townRoot, sessionID string) {
	_ = os.Remove(heartbeatFile(townRoot, sessionID))
}

// HeartbeatFilePath returns the filesystem path to a session's heartbeat file.
// Exported for callers that need to check file existence directly.
func HeartbeatFilePath(townRoot, sessionID string) string {
	return heartbeatFile(townRoot, sessionID)
}
