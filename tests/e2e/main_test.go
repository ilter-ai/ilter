package e2e

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ilter-ai/ilter/internal/features/pii"
)

var (
	devReachableOnce   sync.Once
	devReachableResult bool
)

// devReachable checks if the dev instance (ilter serve) is running.
// The result is cached so the check runs at most once per package run.
func devReachable() bool {
	devReachableOnce.Do(func() {
		for _, addr := range []string{"localhost:8181", "localhost:9191"} {
			conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
			if err != nil {
				return // devReachableResult stays false
			}
			conn.Close()
		}
		devReachableResult = true
	})
	return devReachableResult
}

// requireDev skips the test when the dev server is not running.
func requireDev(t *testing.T) {
	t.Helper()
	if !devReachable() {
		t.Skip("e2e: dev instance not reachable on :8181/:9191")
	}
}

func TestMain(m *testing.M) {
	pii.LoadPatterns(pii.DefaultPIIPatterns)
	m.Run()
}
