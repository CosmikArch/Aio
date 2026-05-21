package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"play/internal/config"
	"time"
)

// TrackStatus represents the on-disk state of a cached audio file.
type TrackStatus int

const (
	StatusCached      TrackStatus = iota // ✓  file present and valid
	StatusDownloading                    // ⌛  lock file fresh — download in progress
	StatusPartial                        // ⚠  file exists but suspiciously small
	StatusMissing                        // ✖  no file, no lock
	StatusStaleLock                      // 🔒  lock file exists but process is dead
)

// Icon returns the display icon for a TrackStatus.
func (s TrackStatus) Icon() string {
	switch s {
	case StatusCached:
		return "✓"
	case StatusDownloading:
		return "⌛"
	case StatusPartial:
		return "⚠"
	case StatusStaleLock:
		return "🔒"
	default:
		return "✖"
	}
}

func (s TrackStatus) String() string {
	switch s {
	case StatusCached:
		return "cached"
	case StatusDownloading:
		return "downloading"
	case StatusPartial:
		return "partial"
	case StatusStaleLock:
		return "stale lock"
	default:
		return "missing"
	}
}

// GetTrackStatus inspects the filesystem to determine the current state of a
// video's audio file without touching the database.
func GetTrackStatus(videoID string) TrackStatus {
	audioPath := filepath.Join(config.CacheDir,
		fmt.Sprintf("%s.%s", videoID, config.AudioFormat))
	lockPath := filepath.Join(config.CacheDir,
		fmt.Sprintf(".%s.lock", videoID))

	// Check for lock file first — it signals an in-progress or stale download.
	if lockInfo, err := os.Stat(lockPath); err == nil {
		age := time.Since(lockInfo.ModTime()).Seconds()
		if age <= config.LockStaleSeconds {
			return StatusDownloading // lock is fresh, process is alive
		}
		return StatusStaleLock // lock exists but is older than stale threshold
	}

	// No lock — check the audio file.
	info, err := os.Stat(audioPath)
	if err != nil {
		return StatusMissing
	}
	if info.Size() <= config.MinValidFileBytes {
		return StatusPartial // file exists but too small to be a real track
	}
	return StatusCached
}
