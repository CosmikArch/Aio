package locks

import (
	"fmt"
	"os"
	"path/filepath"
	"play/internal/config"
	"time"
)

func lockPath(videoID string) string {
	return filepath.Join(config.CacheDir, fmt.Sprintf(".%s.lock", videoID))
}

func IsDownloadInProgress(videoID string) bool {
	lock := lockPath(videoID)
	st, err := os.Stat(lock)
	if os.IsNotExist(err) {
		return false
	}

	age := time.Since(st.ModTime()).Seconds()
	if age > config.LockStaleSeconds {
		os.Remove(lock)
		return false
	}
	return true
}

func AcquireDownloadLock(videoID string) string {
	lock := lockPath(videoID)
	if IsDownloadInProgress(videoID) {
		return ""
	}

	f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return "" // Lock exists or cannot be created
	}
	defer f.Close()

	// Write PID and timestamp
	fmt.Fprintf(f, "%d\n%d\n", os.Getpid(), time.Now().Unix())
	return lock
}
