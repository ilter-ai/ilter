package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
)

func TestInitWizard_EnterKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PTY e2e test in short mode")
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ilter.db")
	bin := filepath.Join(dir, "ilter")

	cmd := exec.Command("go", "build", "-o", bin, "../cmd/ilter")
	cmd.Dir = ".."
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "build: %s", string(out))

	cmd = exec.Command(bin, "init")
	cmd.Dir = ".."
	cmd.Env = append(
		os.Environ(),
		"ILTER_STORAGE_PATH="+dbPath,
		"TERM=xterm-256color",
	)

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 30, Cols: 120})
	require.NoError(t, err)

	var (
		outputMu sync.Mutex
		output   bytes.Buffer
	)
	outCh := make(chan string, 100)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := f.Read(buf)
			if readErr != nil {
				close(outCh)
				return
			}
			outputMu.Lock()
			output.Write(buf[:n])
			outputMu.Unlock()
			select {
			case outCh <- string(buf[:n]):
			default:
			}
		}
	}()

	getOutput := func() string {
		outputMu.Lock()
		defer outputMu.Unlock()
		return output.String()
	}

	waitFor := func(sub string) string {
		for {
			outStr := getOutput()
			if strings.Contains(outStr, sub) {
				return outStr
			}
			select {
			case _, ok := <-outCh:
				if !ok {
					return getOutput()
				}
			case <-time.After(30 * time.Second):
				t.Fatalf("timeout waiting for %q in:\n%s", sub, getOutput())
			}
		}
	}

	send := func(keys string) {
		_, _ = f.Write([]byte(keys))
		time.Sleep(150 * time.Millisecond)
	}

	waitFor("OpenCode Zen")
	t.Log("selecting providers (defaults: opencode_go, opencode_zen)")
	send("\r")

	waitFor("Unique name")
	t.Log("filling provider details (2 providers × 5 fields)")
	for i := 0; i < 10; i++ {
		send("\r")
	}

	waitFor("Active Routing Strategy")
	t.Log("selecting strategy")
	send("\r")

	waitFor("Loop Detection")
	t.Log("feature flags + port config")
	for i := 0; i < 7; i++ {
		send("\r")
	}

	waitFor("Seed applied")
	t.Log("Wizard completed!")
	f.Close()

	require.NoError(t, cmd.Wait(), "exit 0 = Enter key works")
}
