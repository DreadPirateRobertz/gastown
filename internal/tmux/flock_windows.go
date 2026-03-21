//go:build windows

package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// windowsFlockMu serializes acquireFlockLock calls within a single process on
// Windows. tmux does not run natively on Windows; this stub satisfies the
// build and provides intra-process mutual exclusion for test scenarios.
var windowsFlockMu sync.Mutex

// acquireFlockLock is a Windows stub that uses an in-process mutex.
// Cross-process locking is not needed on Windows because tmux is not
// available there; this implementation keeps the build green for CI.
func acquireFlockLock(lockPath string, timeout time.Duration) (func(), error) {
	dir := filepath.Dir(lockPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating lock dir: %w", err)
	}

	acquired := make(chan struct{}, 1)
	go func() {
		windowsFlockMu.Lock()
		acquired <- struct{}{}
	}()

	select {
	case <-acquired:
		return func() { windowsFlockMu.Unlock() }, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout after %s waiting for flock", timeout)
	}
}
