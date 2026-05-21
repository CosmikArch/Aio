package player

import (
	"os"
	"syscall"
	"play/internal/config"
	"strconv"
	"strings"
)

// mpvPid reads the PID from the configured mpv pid file.
func mpvPid() int {
	data, err := os.ReadFile(config.MpvPidFile)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

// IsMpvRunning checks if the PID file exists AND the process is currently alive.
func IsMpvRunning() bool {
	pid := mpvPid()
	if pid <= 0 {
		return false
	}
	
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	
	// Signal 0 is a POSIX standard way to check process existence without sending a real signal.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

