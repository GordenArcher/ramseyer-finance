package listener

import "time"

const (
	// Time allowed for a polite stop before forcing the process down.
	processTerminateGrace = 5 * time.Second
	// Time allowed for the forced stop to release the port.
	processForceGrace = 5 * time.Second
	pollInterval      = 250 * time.Millisecond
)

// Release stops whatever process owns port while isRunning reports the app still answering.
// Service managers only stop processes they started, so a directly started server, a manual
// serve, or a development build would otherwise keep the URL alive after stop printed success.
//
// The probe gates the kill because another program may hold the port. Only an app that still
// answers the health check is safe to stop, and that also makes repeated calls harmless.
func Release(port int, processName string, isRunning func() bool) {
	if !isRunning() {
		return
	}
	terminate(port, processName, false)
	if waitForClosed(isRunning, processTerminateGrace) {
		return
	}
	// A process stuck in a SQLite write can ignore a polite signal, and stop must not report a
	// state the port contradicts. Escalation is safe because the probe confirmed the app.
	terminate(port, processName, true)
	_ = waitForClosed(isRunning, processForceGrace)
}

// waitForClosed reports whether the app stopped answering within the timeout.
func waitForClosed(isRunning func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isRunning() {
			return true
		}
		time.Sleep(pollInterval)
	}
	return false
}
