package main

import (
	"archive/zip"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// main is the entry point for the standalone updater binary. It is compiled
// separately from the main application and is invoked by the running app when
// a new release ZIP has been downloaded and is ready to apply. The updater
// accepts four required arguments (pid of the app to wait for, path to the
// downloaded ZIP, the target installation directory, and the launcher to
// restart after the update) and performs a sequenced update: wait for the old
// process to exit, extract the new release to a temp directory, replace the
// existing installation files, and relaunch the application. The updater is
// intentionally Windows-only; cross-platform support is not needed because the
// primary release target is Windows.
func main() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "updater is only supported on windows")
		os.Exit(1)
	}

	// Parse the four required arguments from the command line. The running
	// application passes these when it spawns the updater process after
	// successfully downloading a new release ZIP.
	pid := flag.Int("pid", 0, "process id of the running app")
	zipPath := flag.String("zip", "", "downloaded update zip path")
	targetDir := flag.String("target", "", "application install directory")
	relaunchPath := flag.String("relaunch", "", "launcher or executable to restart")
	flag.Parse()

	// All four arguments are mandatory. If any are missing or invalid, the
	// updater exits immediately with an error rather than attempting a partial
	// update that could leave the installation in a broken state.
	if *pid <= 0 || strings.TrimSpace(*zipPath) == "" || strings.TrimSpace(*targetDir) == "" || strings.TrimSpace(*relaunchPath) == "" {
		fmt.Fprintln(os.Stderr, "missing required updater arguments")
		os.Exit(1)
	}

	if err := runUpdate(*pid, *zipPath, *targetDir, *relaunchPath); err != nil {
		fmt.Fprintf(os.Stderr, "update failed: %v\n", err)
		os.Exit(1)
	}
}

// runUpdate orchestrates the three-phase update process: wait for the existing
// application process to terminate, extract and apply the new release files,
// and relaunch the application. Each phase is implemented as a separate function
// so failures can be traced to a specific step. A temporary working directory
// is created for the extraction and is cleaned up on exit regardless of success
// or failure.
func runUpdate(pid int, zipPath, targetDir, relaunchPath string) error {
	// Phase 1: wait for the running application to exit. The timeout of 90
	// seconds gives the app a generous window to finish any in-progress
	// database operations and clean up resources. If the process doesn't
	// exit within the timeout, the update is aborted rather than risking
	// file-in-use errors during the replacement phase.
	if err := waitForProcessExit(pid, 90*time.Second); err != nil {
		return err
	}

	// Create a temporary directory for extracting the new release. Using a
	// temp directory instead of extracting directly into the target keeps the
	// extraction isolated—if extraction fails partway through, the existing
	// installation is untouched. The directory name uses a wildcard pattern
	// so the OS generates a unique suffix.
	workingDir, err := os.MkdirTemp("", "ramseyer-finance-apply-*")
	if err != nil {
		return fmt.Errorf("create updater temp directory: %w", err)
	}
	defer os.RemoveAll(workingDir)

	// Phase 2: extract the ZIP into the temp directory and determine the
	// root of the extracted content (handling the common case where the ZIP
	// contains a single top-level folder).
	extractedRoot, err := unzipRelease(zipPath, filepath.Join(workingDir, "extracted"))
	if err != nil {
		return err
	}

	// Phase 3: replace the existing installation files with the new ones,
	// then relaunch the application so the user sees the updated version.
	if err := replaceInstallRoot(extractedRoot, targetDir); err != nil {
		return err
	}

	return relaunchApplication(relaunchPath)
}

// waitForProcessExit polls the Windows task list every 700ms until the target
// process is no longer running or the timeout is reached. The polling interval
// is short enough to minimise delay after the app exits but long enough to
// avoid excessive CPU usage during the wait. The function uses tasklist.exe
// with a PID filter for a lightweight check that doesn't require elevated
// privileges or external dependencies.
func waitForProcessExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		running, err := isProcessRunning(pid)
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		time.Sleep(700 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for app process %d to exit", pid)
}

// isProcessRunning checks whether a process with the given PID exists in the
// Windows task list. It runs `tasklist /FI "PID eq N" /NH` and inspects the
// output. tasklist returns "No tasks are running" or an empty result when the
// PID is not found; any other output containing the PID indicates the process
// is still alive. The function does not require the process to be owned by the
// same user, so it works correctly even if the updater is launched with
// different credentials.
func isProcessRunning(pid int) (bool, error) {
	output, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/NH").CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("query running process: %w", err)
	}
	text := strings.ToLower(string(output))
	if strings.Contains(text, "no tasks are running") {
		return false, nil
	}
	// The PID is matched with surrounding whitespace or punctuation to avoid
	// false positives (e.g., PID 12 matching against PID 1234). tasklist
	// formats PIDs with surrounding spaces or followed by a comma.
	return strings.Contains(text, fmt.Sprintf(" %d ", pid)) || strings.Contains(text, fmt.Sprintf(",%d", pid)), nil
}

// unzipRelease extracts a ZIP archive into the given destination directory and
// returns the effective root path of the extracted content. It handles ZIP slip
// attacks by validating that every file path inside the archive stays within
// the destination directory after cleaning. If the ZIP contains a single
// top-level directory (the typical GitHub release ZIP layout), the function
// returns the path to that inner directory so callers don't need to guess the
// folder name. Directories and files are created with 0755 permissions, which
// is appropriate for application files on Windows.
func unzipRelease(zipPath, destinationDir string) (string, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("open update zip: %w", err)
	}
	defer reader.Close()

	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return "", fmt.Errorf("create extraction directory: %w", err)
	}

	for _, file := range reader.File {
		targetPath := filepath.Join(destinationDir, file.Name)
		// Prevent ZIP slip attacks: ensure the resolved target path is
		// within the destination directory. The check uses filepath.Clean
		// on both paths to neutralise "../" sequences before comparison.
		if !strings.HasPrefix(filepath.Clean(targetPath), filepath.Clean(destinationDir)+string(os.PathSeparator)) &&
			filepath.Clean(targetPath) != filepath.Clean(destinationDir) {
			return "", errors.New("zip contains invalid path")
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return "", fmt.Errorf("create extracted directory: %w", err)
			}
			continue
		}

		// Ensure parent directories exist before creating the file.
		// Some ZIP tools omit directory entries and only include file
		// entries, so MkdirAll is called defensively for every file.
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return "", fmt.Errorf("create extracted parent directory: %w", err)
		}

		source, err := file.Open()
		if err != nil {
			return "", fmt.Errorf("open zipped file: %w", err)
		}

		destination, err := os.Create(targetPath)
		if err != nil {
			source.Close()
			return "", fmt.Errorf("create extracted file: %w", err)
		}

		if _, err := io.Copy(destination, source); err != nil {
			source.Close()
			destination.Close()
			return "", fmt.Errorf("extract file: %w", err)
		}
		source.Close()
		if err := destination.Close(); err != nil {
			return "", fmt.Errorf("close extracted file: %w", err)
		}
	}

	// If the ZIP wrapped everything in a single top-level folder (as GitHub
	// release ZIPs typically do), return the path to that inner folder so the
	// caller can work directly with the release contents. Otherwise return
	// the destination directory itself.
	entries, err := os.ReadDir(destinationDir)
	if err != nil {
		return "", fmt.Errorf("read extracted release root: %w", err)
	}
	if len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(destinationDir, entries[0].Name()), nil
	}
	return destinationDir, nil
}

// replaceInstallRoot replaces the existing installation files with the
// extracted release files. It first ensures the target root directory exists,
// then removes the known managed files and directories from the previous
// installation before copying the new ones in. The managed targets list is
// explicit rather than doing a blanket delete of the target directory—this
// avoids accidentally removing user data if the target directory is
// misconfigured or if the user has added custom files alongside the app.
func replaceInstallRoot(sourceRoot, targetRoot string) error {
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return fmt.Errorf("ensure target root: %w", err)
	}

	// Only remove the files and directories that are known to be part of the
	// application distribution. User-created files in the target directory
	// are left untouched. Each removal failure is silently ignored because
	// the file may not exist in the current installation (e.g., an older
	// version that didn't include a particular doc file).
	managedTargets := []string{"app", "docs", "README.md", "README-Windows.txt", "START-Ramseyer-Finance.bat"}
	for _, name := range managedTargets {
		_ = os.RemoveAll(filepath.Join(targetRoot, name))
	}

	return copyTree(sourceRoot, targetRoot)
}

// copyTree recursively copies all files and directories from sourceRoot to
// targetRoot using filepath.Walk. It preserves the directory structure
// relative to the source root and creates parent directories as needed. Each
// file is copied byte-for-byte; file permissions from the source are not
// explicitly preserved—files are created with default permissions, which on
// Windows is typically sufficient for application files. The function skips
// the root directory itself (where relativePath would be ".") to avoid
// attempting to create the target root as a child of itself.
func copyTree(sourceRoot, targetRoot string) error {
	return filepath.Walk(sourceRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relativePath, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		if relativePath == "." {
			return nil
		}

		destinationPath := filepath.Join(targetRoot, relativePath)
		if info.IsDir() {
			return os.MkdirAll(destinationPath, 0o755)
		}

		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
			return err
		}

		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer sourceFile.Close()

		destinationFile, err := os.Create(destinationPath)
		if err != nil {
			return err
		}

		if _, err := io.Copy(destinationFile, sourceFile); err != nil {
			destinationFile.Close()
			return err
		}
		return destinationFile.Close()
	})
}

// relaunchApplication starts the updated application. If the relaunch path
// ends with .bat or .cmd (the launcher script), it uses `cmd /C start` to open
// it in a new window, which is the expected behaviour for a double-click
// launcher. For direct executables, it starts the process directly. The
// function uses Start() rather than Run() so the updater can exit immediately
// after spawning the new process; it does not wait for the application to
// finish launching.
func relaunchApplication(relaunchPath string) error {
	lowerPath := strings.ToLower(relaunchPath)
	if strings.HasSuffix(lowerPath, ".bat") || strings.HasSuffix(lowerPath, ".cmd") {
		return exec.Command("cmd", "/C", "start", "", relaunchPath).Start()
	}
	return exec.Command(relaunchPath).Start()
}
