package player

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"play/internal/cache"
	"play/internal/config"
	"play/internal/locks"
)

// CleanWorkDirs removes any leftover <videoID>.work.<random> temp directories
// from the cache dir. These are created during background downloads and
// normally deleted by a defer inside RunBackgroundDownloader. If the process
// was killed mid-download the defer never ran, leaving the directory behind.
//
// No timestamp heuristic is needed: the .work.* pattern is exclusively used
// as in-progress scratch space. A directory matching it either belongs to a
// currently-running download (whose lock file is still present) or is stale
// garbage. We skip any videoID that still holds an active lock so we never
// interfere with a live download.
func CleanWorkDirs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Match pattern: <videoID>.work.<random>
		dotWork := strings.Index(name, ".work.")
		if dotWork < 0 {
			continue
		}
		videoID := name[:dotWork]
		// Skip if a download lock is still active for this videoID —
		// the owning process is still running and owns this directory.
		if locks.IsDownloadInProgress(videoID) {
			continue
		}
		os.RemoveAll(filepath.Join(dir, name))
	}
}

// RunBackgroundDownloader handles the actual download process in the detached worker.
func RunBackgroundDownloader(args []string) {
	if len(args) != 3 {
		os.Exit(2)
	}

	videoID := args[0]
	outputPath := args[1]
	lockFile := args[2]

	config.SetupLogger()
	cacheDir := filepath.Dir(outputPath)

	// Create unique temporary work directory
	tempDir, err := os.MkdirTemp(cacheDir, fmt.Sprintf("%s.work.*", videoID))
	if err != nil {
		config.Logger.Printf("Failed to create temp dir for %s", videoID)
		os.Remove(lockFile)
		os.Exit(1)
	}

	defer func() {
		os.RemoveAll(tempDir)
		os.Remove(lockFile)
	}()

	tempBase := filepath.Join(tempDir, "download")
	videoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "yt-dlp",
		"--quiet", "--no-warnings", "--no-playlist",
		"-f", "bestaudio",
		"--extract-audio",
		"--audio-format", config.AudioFormat,
		"-o", fmt.Sprintf("%s.%%(ext)s", tempBase),
		"--", videoURL,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		config.Logger.Printf("Download failed for %s: %v | %s", videoID, err, string(out))
		os.Exit(1)
	}

	downloadedFile := fmt.Sprintf("%s.%s", tempBase, config.AudioFormat)
	if _, err := os.Stat(downloadedFile); os.IsNotExist(err) {
		config.Logger.Printf("Download finished but output file missing for %s", videoID)
		os.Exit(1)
	}

	// Atomic rename: the file becomes visible in the cache dir in one syscall.
	if err := os.Rename(downloadedFile, outputPath); err != nil {
		config.Logger.Printf("Failed to move downloaded file to cache: %v", err)
		os.Exit(1)
	}

	// A new file just entered the cache — enforce the size cap immediately.
	// This is the only moment the cache grows, so LRU eviction belongs here
	// rather than on a polling timer.
	cache.EnforceSize()

	os.Exit(0)
}
