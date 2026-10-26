//go:build windows

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const windowsUpdateTestTimeout = 15 * time.Second

// TestReplaceOnWindowsMovesStagedBinary exercises the detached handoff on a real Windows
// shell. The target is a batch file rather than the app so the test can observe both the
// replacement and the relaunch through a marker without starting a server.
func TestReplaceOnWindowsMovesStagedBinary(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "Ramseyer Finance", "bin")
	stageDir := filepath.Join(root, "Ramseyer Finance Update")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("create install directory: %v", err)
	}
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatalf("create staging directory: %v", err)
	}

	current := filepath.Join(installDir, "ramseyer-finance.cmd")
	replacement := filepath.Join(stageDir, "ramseyer-finance.cmd")
	marker := filepath.Join(root, "updated.txt")

	if err := os.WriteFile(current, []byte("@echo off\r\necho old\r\n"), 0o600); err != nil {
		t.Fatalf("write installed binary: %v", err)
	}
	if err := os.WriteFile(replacement, []byte("@echo off\r\necho updated > \""+marker+"\"\r\n"), 0o600); err != nil {
		t.Fatalf("write staged binary: %v", err)
	}

	if err := replaceOnWindows(current, replacement, stageDir); err != nil {
		t.Fatalf("start Windows updater: %v", err)
	}

	waitForWindowsUpdateTest(t, func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}, "updated binary was not started")

	content, err := os.ReadFile(current)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if !strings.Contains(string(content), "updated") {
		t.Fatalf("installed binary was not replaced: %q", content)
	}

	waitForWindowsUpdateTest(t, func() bool {
		_, err := os.Stat(stageDir)
		return os.IsNotExist(err)
	}, "staging directory was not removed")
}

func waitForWindowsUpdateTest(t *testing.T, ready func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(windowsUpdateTestTimeout)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal(message)
}
