//go:build !windows

package cmd

import (
	"os"
	"syscall"
)

// signalDaemonConfigReload sends SIGHUP to the daemon to trigger a full config reload.
func signalDaemonConfigReload(process *os.Process) error {
	return process.Signal(syscall.SIGHUP)
}
