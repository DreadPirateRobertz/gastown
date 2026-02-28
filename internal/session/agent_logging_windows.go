//go:build windows

package session

// ActivateAgentLogging is a no-op on Windows since agent logging relies on
// Unix-specific process management (Setsid, SIGTERM).
func ActivateAgentLogging(sessionID, workDir, runID string) error {
	return nil
}
