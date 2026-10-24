//go:build !windows

package listener

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// terminate targets the PID that owns the listening socket rather than a process name, so an
// unrelated build with a similar name is not touched.
func terminate(port int, _ string, force bool) {
	signal := syscall.SIGTERM
	signalFlag := "-TERM"
	if force {
		signal = syscall.SIGKILL
		signalFlag = "-KILL"
	}

	pids := listeningPIDs(port)
	if len(pids) == 0 {
		// lsof is missing on some minimal Linux images, and fuser still matches by port.
		_ = exec.Command("fuser", "-k", signalFlag, fmt.Sprintf("%d/tcp", port)).Run()
		return
	}
	for _, pid := range pids {
		_ = syscall.Kill(pid, signal)
	}
}

func listeningPIDs(port int) []int {
	output, err := exec.Command("lsof", "-ti", fmt.Sprintf("tcp:%d", port), "-sTCP:LISTEN").Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, field := range strings.Fields(string(output)) {
		pid, convErr := strconv.Atoi(field)
		if convErr != nil {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}
