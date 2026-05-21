package cache

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"play/internal/config"
	"play/internal/locks"
	"strconv"
	"strings"
	"time"
)

func IsCachedFileValid(path string) bool {
	st, err := os.Stat(path)
	if err != nil || strings.HasSuffix(path, ".tmp") {
		return false
	}
	if st.Size() <= config.MinValidFileBytes {
		return false
	}

	// Tier 1 fast path: recently written files are trusted without ffprobe.
	age := time.Since(st.ModTime()).Seconds()
	if age <= config.FfprobeSkipWindowSec {
		return true
	}

	// Tier 2 slow path: verify audio duration via ffprobe.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", path)

	out, err := cmd.Output()
	if err != nil {
		return false
	}

	duration, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return err == nil && duration > 0
}

// EnforceSize evicts the least-recently-used audio files until the total
// cache size falls at or below config.CacheMaxBytes.
//
// Eviction order comes from the DB: each video ID is ranked by its most
// recent access time (MAX(last_used) across all index keys for that video).
// The stalest video is deleted first. Files that are currently downloading
// (have an active lock) are skipped to avoid corrupting an in-flight download.
//
// This is called by RunBackgroundDownloader immediately after a new file is
// committed to the cache — the only moment the cache grows — so the cap is
// enforced precisely when it can be breached.
func EnforceSize() {
	if config.CacheMaxBytes <= 0 {
		return // no limit configured
	}

	audioSuffix := "." + config.AudioFormat

	// Measure total size of every audio file currently on disk.
	entries, err := os.ReadDir(config.CacheDir)
	if err != nil {
		return
	}
	var totalBytes int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), audioSuffix) {
			continue
		}
		if info, err := e.Info(); err == nil {
			totalBytes += info.Size()
		}
	}

	if totalBytes <= config.CacheMaxBytes {
		return // already within limit — nothing to do
	}

	// Query the DB for every unique video ID, ranked by its most recent
	// access time across all query keys (LRU first).
	// Using MAX(last_used) per video ensures we don't evict a track that was
	// recently played just because one of its secondary keys is old.
	d := getDB()
	rows, err := d.Query(`
		SELECT video_id, MAX(last_used) AS last_used
		FROM cache_index
		GROUP BY video_id
		ORDER BY last_used ASC
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		if totalBytes <= config.CacheMaxBytes {
			break // limit satisfied
		}

		var videoID string
		var lastUsed int64
		if err := rows.Scan(&videoID, &lastUsed); err != nil {
			continue
		}

		// Never evict a file that is currently being written by the downloader.
		if locks.IsDownloadInProgress(videoID) {
			continue
		}

		filePath := filepath.Join(config.CacheDir,
			fmt.Sprintf("%s%s", videoID, audioSuffix))

		info, err := os.Stat(filePath)
		if err != nil {
			// File is gone from disk but index row still exists — clean up.
			PurgeEntry(videoID)
			continue
		}

		fileSize := info.Size()
		if err := os.Remove(filePath); err != nil {
			continue // another process may hold the file; skip silently
		}

		PurgeEntry(videoID)
		totalBytes -= fileSize

		if config.Logger != nil {
			config.Logger.Printf(
				"LRU evict: %s (last used %s, freed %.1f MB, cache now %.1f / %.0f MB)",
				videoID,
				time.Unix(lastUsed, 0).Format("2006-01-02 15:04"),
				float64(fileSize)/(1024*1024),
				float64(totalBytes)/(1024*1024),
				float64(config.CacheMaxBytes)/(1024*1024),
			)
		}
	}
}

func CleanStaleEntries() {
	if _, err := os.Stat(config.CacheDir); os.IsNotExist(err) {
		return
	}

	protected := map[string]bool{
		config.LogFile:              true,
		config.MpvPidFile:           true,
		config.MpvSocketFile:        true, // unix socket — must survive for the lifetime of mpv
		config.DaemonPidFile:        true,
		config.HistoryFile:          true,
		config.QueueFile:            true,
		config.FavoritesFile:        true,
		config.CacheDBFile:          true,
		config.CacheDBFile + "-wal": true,
		config.CacheDBFile + "-shm": true,
	}

	entries, err := os.ReadDir(config.CacheDir)
	if err != nil {
		return
	}

	now := time.Now()
	for _, entry := range entries {
		path := filepath.Join(config.CacheDir, entry.Name())
		if protected[path] {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		age := now.Sub(info.ModTime()).Seconds()
		name := entry.Name()

		if strings.HasSuffix(name, ".lock") {
			if age > config.LockStaleSeconds {
				os.Remove(path)
			}
		} else if strings.Contains(name, ".work.") || strings.HasSuffix(name, ".tmp") {
			if age > config.LockStaleSeconds {
				if entry.IsDir() {
					os.RemoveAll(path)
				} else {
					os.Remove(path)
				}
			}
		} else if info.Mode().IsRegular() && age > float64(config.CacheExpiryDays*86400) {
			os.Remove(path)
			if strings.HasSuffix(name, "."+config.AudioFormat) {
				videoID := strings.TrimSuffix(name, "."+config.AudioFormat)
				PurgeEntry(videoID)
			}
		}
	}

	// After time-based expiry, enforce the size cap with LRU eviction.
	EnforceSize()
}
