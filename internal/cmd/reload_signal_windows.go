//go:build windows

package cmd

import (
	"fmt"
	"os"
)

// signalDaemonConfigReload is a no-op on Windows since SIGHUP is not available.
func signalDaemonConfigReload(process *os.Process) error {
	return fmt.Errorf("daemon config reload signal not supported on Windows")
}
