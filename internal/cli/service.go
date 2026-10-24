package cli

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	serviceLabel         = "ramseyer-finance"
	healthURL            = "http://127.0.0.1:47823/healthz"
	localAppURL          = "http://127.0.0.1:47823"
	healthRequestTimeout = 1500 * time.Millisecond
	// On first launch the database is created and seeded which can take longer than the
	// previous five second window, especially on slower Windows machines or after a fresh
	// install. A longer timeout keeps the installer and start commands from giving up before
	// the server finishes initializing.
	serviceStartTimeout = 20 * time.Second
	healthRetryInterval = 250 * time.Millisecond
)

// LocalAppURL centralizes the browser address so command handlers cannot drift from health checks.
// Keeping both paths on the same loopback endpoint means a healthy service is the one the client sees.
func LocalAppURL() string {
	return localAppURL
}

// Enable keeps the server in the user's session rather than registering a machine-wide service.
// The database belongs to that user, and starting before sign-in would create a second ownership
// model that complicates backups and browser access.
func Enable() error {
	if err := writeServiceDefinition(); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		homeDirectory, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home directory: %w", err)
		}
		plist := filepath.Join(homeDirectory, "Library", "LaunchAgents", serviceLabel+".plist")
		command := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), plist)
		if output, err := command.CombinedOutput(); err != nil && !strings.Contains(string(output), "already") {
			return fmt.Errorf("enable startup service: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	return Start()
}

// Start delegates to the operating system service manager instead of spawning a child process.
// That keeps restart behavior consistent after login and prevents a terminal from owning a server
// that should continue after the terminal is closed.
func Start() error {
	if healthy() {
		fmt.Println("Ramseyer Finance is already running.")
		return nil
	}
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/%s", os.Getuid(), serviceLabel))
	case "linux":
		command = exec.Command("systemctl", "--user", "start", serviceLabel+".service")
	case "windows":
		command = exec.Command("schtasks", "/Run", "/TN", serviceLabel)
	default:
		return fmt.Errorf("automatic service management is not supported on %s", runtime.GOOS)
	}
	if output, err := command.CombinedOutput(); err != nil {
		if runtime.GOOS == "windows" {
			// The scheduled task may not exist (creation failed silently) or the logon task
			// may refuse to run in the current context. Falling back to launching the server
			// directly keeps the app usable, and the file log captures the schtasks error for
			// debugging. The task can still be registered later with `enable`.
			log.Printf("scheduled task start failed, launching directly: %v: %s", err, strings.TrimSpace(string(output)))
			if spawnErr := spawnServe(); spawnErr != nil {
				return fmt.Errorf("start service: %w: %s; direct start failed: %v", err, strings.TrimSpace(string(output)), spawnErr)
			}
		} else {
			return fmt.Errorf("start service: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	if err := waitForHealth(serviceStartTimeout); err != nil {
		return err
	}
	fmt.Println("Ramseyer Finance is running.")
	return nil
}

func spawnServe() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.Command(executable, "serve")
	command.SysProcAttr = detachedSysProcAttr()
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	return command.Start()
}

// EnsureRunning keeps desktop shortcuts quiet when the background service is already healthy.
// Starting only after the health check avoids showing a misleading "already running" message to
// a client who simply clicked the shortcut to open their work.
func EnsureRunning() error {
	if healthy() {
		return nil
	}
	return Start()
}

// Stop asks the service manager to stop the process so its restart policy remains in control.
// Killing a process by name could stop an unrelated development instance or leave stale state.
func Stop() error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("launchctl", "kill", "SIGTERM", fmt.Sprintf("gui/%d/%s", os.Getuid(), serviceLabel))
	case "linux":
		command = exec.Command("systemctl", "--user", "stop", serviceLabel+".service")
	case "windows":
		command = exec.Command("schtasks", "/End", "/TN", serviceLabel)
	default:
		return fmt.Errorf("automatic service management is not supported on %s", runtime.GOOS)
	}
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("stop service: %w: %s", err, strings.TrimSpace(string(output)))
	}
	fmt.Println("Ramseyer Finance has stopped.")
	return nil
}

func Health() error {
	if !healthy() {
		return fmt.Errorf("Ramseyer Finance is not responding at %s", healthURL)
	}
	fmt.Printf("Ramseyer Finance is healthy and responding at %s.\n", localAppURL)
	return nil
}

func Status() error {
	if healthy() {
		fmt.Println("Status: running (healthy).")
		return nil
	}
	fmt.Println("Status: stopped or not responding.")
	return nil
}

func writeServiceDefinition() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate installed binary: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve installed binary: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		homeDirectory, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home directory: %w", err)
		}
		directory := filepath.Join(homeDirectory, "Library", "LaunchAgents")
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
		content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array><string>%s</string><string>serve</string></array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/></dict></plist>`, serviceLabel, xmlEscape(executable))
		return os.WriteFile(filepath.Join(directory, serviceLabel+".plist"), []byte(content), 0o644)
	case "linux":
		config, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		directory := filepath.Join(config, "systemd", "user")
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
		content := fmt.Sprintf("[Unit]\nDescription=Ramseyer Finance\n\n[Service]\nExecStart=%s serve\nRestart=on-failure\n\n[Install]\nWantedBy=default.target\n", executable)
		if err := os.WriteFile(filepath.Join(directory, serviceLabel+".service"), []byte(content), 0o644); err != nil {
			return err
		}
		if output, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
			return fmt.Errorf("reload user services: %w: %s", err, strings.TrimSpace(string(output)))
		}
		if output, err := exec.Command("systemctl", "--user", "enable", serviceLabel+".service").CombinedOutput(); err != nil {
			return fmt.Errorf("enable service: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	case "windows":
		// A per-user scheduled task avoids an administrator prompt while still starting at login.
		// A Windows service would run outside the user's profile and risk using the wrong data path.
		arguments := fmt.Sprintf(`"%s" serve`, executable)
		command := exec.Command("schtasks", "/Create", "/F", "/SC", "ONLOGON", "/RL", "LIMITED", "/TN", serviceLabel, "/TR", arguments)
		if output, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("create startup task: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	default:
		return fmt.Errorf("automatic service management is not supported on %s", runtime.GOOS)
	}
}

func healthy() bool {
	client := &http.Client{Timeout: healthRequestTimeout}
	response, err := client.Get(healthURL)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func waitForHealth(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if healthy() {
			return nil
		}
		time.Sleep(healthRetryInterval)
	}
	return fmt.Errorf("service started but did not become healthy within %s", strconv.Itoa(int(timeout.Seconds()))+" seconds")
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}
