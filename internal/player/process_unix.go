//go:build !windows

package player

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"play/internal/config"
)

// getProcessName reads the process name from /proc on Linux.
// Returns an empty string on macOS or if the file is unreadable.
func getProcessName(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// killByPidFile sends SIGTERM to the process recorded in pidFile,
// verifying the running name matches expectedName when possible.
func killByPidFile(pidFile, expectedName string) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}

	pidStr := strings.TrimSpace(string(data))
	if pid, err := strconv.Atoi(pidStr); err == nil {
		name := getProcessName(pid)
		// name == "" on macOS (no /proc); fall through and kill anyway.
		if expectedName == "" || strings.Contains(name, expectedName) || name == "" {
			if proc, err := os.FindProcess(pid); err == nil {
				_ = proc.Signal(syscall.SIGTERM)
			}
		}
	}
	os.Remove(pidFile)
}

// isDaemonRunning checks whether the PID stored in DaemonPidFile still
// refers to a live process via signal 0.
func isDaemonRunning() bool {
	data, err := os.ReadFile(config.DaemonPidFile)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// startDetached starts cmd in a new session so it survives the parent
// process exiting (Unix daemonisation via Setsid).
func startDetached(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
