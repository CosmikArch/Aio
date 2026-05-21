package player

import (
	"os"
	"os/exec"
	"strconv"

	"play/internal/config"
	"play/internal/locks"
	"play/internal/queue"
)

// StopAll terminates any running daemon and mpv process.
func StopAll() {
	killByPidFile(config.DaemonPidFile, "play")
	killByPidFile(config.MpvPidFile, "mpv")
}

// PlayNew stops any current playback, clears the queue, enqueues query,
// and starts a fresh daemon.
func PlayNew(query string, loop bool) {
	StopAll()
	queue.Clear()
	queue.Add(query)
	StartDaemon(loop)
}

// EnsureDaemon starts the queue daemon only if it is not already running.
func EnsureDaemon() {
	if isDaemonRunning() {
		return
	}
	StartDaemon(false)
}

// StartDaemon re-launches the current executable as a detached background
// worker that processes the queue. No .exe suffix is needed on Windows;
// Go resolves the path automatically.
func StartDaemon(loop bool) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	args := []string{"--internal-queue-daemon"}
	if loop {
		args = append(args, "--loop")
	}

	cmd := exec.Command(exe, args...)
	if err := startDetached(cmd); err == nil {
		_ = os.WriteFile(config.DaemonPidFile,
			[]byte(strconv.Itoa(cmd.Process.Pid)), 0644)
	}
}

// CacheInBackground launches a detached worker that downloads videoID to
// outputPath via yt-dlp. A lock file prevents duplicate downloads.
func CacheInBackground(videoID, outputPath string) {
	// BUG FIX: was calling undefined acquireDownloadLock (lowercase) —
	// the function lives in the locks package as AcquireDownloadLock.
	lockFile := locks.AcquireDownloadLock(videoID)
	if lockFile == "" {
		return // download already in progress
	}

	exe, err := os.Executable()
	if err != nil {
		os.Remove(lockFile)
		return
	}

	cmd := exec.Command(exe, "--internal-bg-download", videoID, outputPath, lockFile)

	// Route stderr to the rotating log file so download errors are visible.
	// BUG FIX: logFile was previously opened inside an `if` initialiser, making
	// it impossible to defer Close() on it. Declaring it outside the block lets
	// us defer the close so the FD is released when CacheInBackground returns.
	logFile, logErr := os.OpenFile(config.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if logErr == nil {
		cmd.Stderr = logFile
		defer logFile.Close()
	}

	if err := startDetached(cmd); err != nil {
		os.Remove(lockFile)
	}
}
