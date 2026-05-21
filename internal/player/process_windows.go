//go:build windows

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

// killByPidFile forcefully terminates the process recorded in pidFile.
// On Windows, graceful SIGTERM is unavailable so we use Kill directly.
// expectedName is accepted for API compatibility but not checked because
// Windows has no cheap /proc equivalent without CGo.
func killByPidFile(pidFile, _ string) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pidStr := strings.TrimSpace(string(data))
	if pid, err := strconv.Atoi(pidStr); err == nil {
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Kill()
		}
	}
	os.Remove(pidFile)
}

// isDaemonRunning queries tasklist to check if the PID stored in
// DaemonPidFile corresponds to a live process.
// os.FindProcess on Windows always succeeds regardless of whether the
// process actually exists, so we use an external query instead.
func isDaemonRunning() bool {
	data, err := os.ReadFile(config.DaemonPidFile)
	if err != nil {
		return false
	}
	pidStr := strings.TrimSpace(string(data))
	if pidStr == "" {
		return false
	}
	out, err := exec.Command(
		"tasklist",
		"/FI", fmt.Sprintf("PID eq %s", pidStr),
		"/NH", "/FO", "CSV",
	).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), pidStr)
}

// detachedProcess is the Win32 DETACHED_PROCESS creation flag.
// It prevents the child from inheriting the parent's console, equivalent
// to Unix's Setsid for the purposes of background daemon spawning.
const detachedProcess = 0x00000008

// startDetached starts cmd as a detached Windows process that survives
// the parent exiting. No .exe suffix is needed; Go/PATH resolution handles it.
func startDetached(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess,
	}
	return cmd.Start()
}
