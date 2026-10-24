package cli

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"ramseyer-finance/internal/cli/listener"
	"ramseyer-finance/internal/cli/spawn"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	serviceLabel         = "ramseyer-finance"
	loopbackHost         = "127.0.0.1"
	appPort              = 47823
	healthPath           = "/healthz"
	healthRequestTimeout = 1500 * time.Millisecond
	// On first launch the database is created and seeded which can take longer than the
	// previous five second window, especially on slower Windows machines or after a fresh
	// install. A longer timeout keeps the installer and start commands from giving up before
	// the server finishes initializing.
	serviceStartTimeout = 20 * time.Second
	// Stopping has to cover processes the service manager never started, so the port is
	// re-checked after the kill instead of trusting the manager's exit code.
	serviceStopTimeout  = 10 * time.Second
	healthRetryInterval = 250 * time.Millisecond
)

// AppPort reports the loopback port so the server defaults and the CLI cannot drift apart.
// Both sides previously hard-coded 47823 in separate packages.
func AppPort() int {
	return appPort
}

// LocalAppURL centralizes the browser address so command handlers cannot drift from health checks.
// Keeping both paths on the same loopback endpoint means a healthy service is the one the client sees.
func LocalAppURL() string {
	return fmt.Sprintf("http://%s:%d", loopbackHost, appPort)
}

// healthEndpoint is derived from LocalAppURL so the health probe always hits the same host and
// port the browser uses.
func healthEndpoint() string {
	return LocalAppURL() + healthPath
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

	command, err := startCommand()
	if err != nil {
		return err
	}
	if err := runStartCommand(command); err != nil {
		// The Windows logon task may be missing (creation failed silently) or refuse to run in
		// the current context. Falling back to a direct launch keeps the app usable, and the
		// file log captures the schtasks error. The task can still be registered with `enable`.
		if runtime.GOOS != "windows" {
			return err
		}
		log.Printf("scheduled task start failed, launching directly: %v", err)
		executable, execErr := os.Executable()
		if execErr != nil {
			return fmt.Errorf("%w; direct start failed: %v", err, execErr)
		}
		if spawnErr := spawn.Detached(executable, "serve"); spawnErr != nil {
			return fmt.Errorf("%w; direct start failed: %v", err, spawnErr)
		}
	}

	if err := waitForHealth(serviceStartTimeout); err != nil {
		return err
	}
	fmt.Println("Ramseyer Finance is running.")
	return nil
}

func startCommand() (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		// launchd keeps the agent alive with KeepAlive, so kickstart alone would restart a
		// process that stop only signalled. Stop boots the agent out, therefore start has to
		// bootstrap it again; kickstart then covers the already-loaded case.
		return exec.Command("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/%s", os.Getuid(), serviceLabel)), nil
	case "linux":
		return exec.Command("systemctl", "--user", "start", serviceLabel+".service"), nil
	case "windows":
		return exec.Command("schtasks", "/Run", "/TN", serviceLabel), nil
	default:
		return nil, fmt.Errorf("automatic service management is not supported on %s", runtime.GOOS)
	}
}

// runStartCommand first loads a booted-out launch agent and then starts it. Other platforms only
// need the single service manager command.
func runStartCommand(command *exec.Cmd) error {
	if runtime.GOOS == "darwin" {
		if err := bootstrapLaunchAgent(); err != nil {
			return err
		}
	}
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("start service: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func bootstrapLaunchAgent() error {
	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	plist := filepath.Join(homeDirectory, "Library", "LaunchAgents", serviceLabel+".plist")
	if _, err := os.Stat(plist); err != nil {
		return fmt.Errorf("locate launch agent: %w", err)
	}
	command := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), plist)
	if output, err := command.CombinedOutput(); err != nil {
		// Bootstrap fails when the agent is already loaded, which is expected after a start or
		// sign-in. Anything else means launchd refused the definition and must surface.
		outputText := string(output)
		if strings.Contains(outputText, "already") || strings.Contains(outputText, "service already loaded") {
			return nil
		}
		return fmt.Errorf("load launch agent: %w: %s", err, strings.TrimSpace(outputText))
	}
	return nil
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

// Stop asks the service manager to stop first so its restart policy stays in control, then
// verifies the port actually closed. The manager only knows about processes it launched, so a
// server started directly (the start fallback, a manual `serve`, or a development build) would
// otherwise keep answering the URL after stop printed success.
func Stop() error {
	command, err := stopCommand()
	if err != nil {
		return err
	}
	if output, err := command.CombinedOutput(); err != nil {
		// Not fatal on purpose. A directly started server is unknown to the manager, so a
		// "task does not exist" style error is expected there and the port check below is the
		// real source of truth for whether the app stopped.
		log.Printf("service manager stop failed: %v: %s", err, strings.TrimSpace(string(output)))
	}
	listener.Release(appPort, serviceLabel, healthy)
	if err := waitForPortClosed(serviceStopTimeout); err != nil {
		return err
	}
	fmt.Println("Ramseyer Finance has stopped.")
	return nil
}

func stopCommand() (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		// bootout unloads the agent instead of signalling it. KeepAlive would otherwise start
		// the process again immediately, which is why stop appeared to work while the URL
		// stayed reachable. The plist remains in LaunchAgents, so the next login loads it.
		return exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), serviceLabel)), nil
	case "linux":
		return exec.Command("systemctl", "--user", "stop", serviceLabel+".service"), nil
	case "windows":
		return exec.Command("schtasks", "/End", "/TN", serviceLabel), nil
	default:
		return nil, fmt.Errorf("automatic service management is not supported on %s", runtime.GOOS)
	}
}

func Health() error {
	if !healthy() {
		return fmt.Errorf("Ramseyer Finance is not responding at %s", healthEndpoint())
	}
	fmt.Printf("Ramseyer Finance is healthy and responding at %s.\n", LocalAppURL())
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
	response, err := client.Get(healthEndpoint())
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

// waitForPortClosed polls the health endpoint until it stops answering. The health probe is used
// instead of a raw TCP dial because it proves the app released the port rather than another
// process having taken it over mid-stop.
func waitForPortClosed(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !healthy() {
			return nil
		}
		time.Sleep(healthRetryInterval)
	}
	return fmt.Errorf("service stop failed: %d is still accepting connections", appPort)
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}
