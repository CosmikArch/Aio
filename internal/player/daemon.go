package player

import (
	"os"
	"os/exec"
	"strconv"
	"strings"

	"play/internal/config"
	"play/internal/history"
	"play/internal/queue"
	"play/internal/resolver"
)

// RunQueueDaemon is the detached background worker that drains the queue.
//
// For each query it pops, it delegates all resolution to resolver.Resolve —
// cache lookup, metadata repair, stream URL refresh, and persistence are all
// handled there. The daemon only decides *how* to play the result:
//   - FileReady true  → play from disk (zero network, instant start)
//   - FileReady false → start background download, stream from CDN URL
func RunQueueDaemon(loop bool) {
	config.SetupLogger()
	go CleanWorkDirs(config.CacheDir)

	for {
		query := queue.Pop()
		if query == "" {
			break
		}

		target := resolver.Resolve(query)
		if target.VideoID == "" {
			config.Logger.Printf("resolver: no result for %q — skipping", query)
			continue
		}

		history.Add(target.Title)

		if target.FileReady {
			// Audio is on disk — play directly, no network I/O.
			RunMpvSync(target.CachedFile, loop)
		} else {
			// File absent — start background fetch and stream from CDN URL
			// for immediate playback.
			CacheInBackground(target.VideoID, target.CachedFile)
			RunMpvSync(target.StreamURL, loop)
		}
	}

	os.Remove(config.DaemonPidFile)
}

func RunMpvSync(source string, loop bool) { runMpv(source, loop) }

func runMpv(source string, loop bool) {
	args := []string{
		"--no-video",
		"--really-quiet",
		"--audio-display=no",
		"--input-ipc-server=" + config.MpvSocketFile,
	}
	if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
		args = append(args, "--no-ytdl")
	}
	if loop {
		args = append(args, "--loop-file=inf")
	}
	args = append(args, source)

	cmd := exec.Command("mpv", args...)

	if devNull, err := os.Open(os.DevNull); err == nil {
		defer devNull.Close()
		cmd.Stdin = devNull
		cmd.Stdout = devNull
	}

	logFile, logErr := os.OpenFile(config.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if logErr == nil {
		defer logFile.Close()
		cmd.Stderr = logFile
	}

	if err := cmd.Start(); err != nil {
		if logErr == nil {
			logFile.WriteString("mpv start error for " + source + ": " + err.Error() + "\n")
		}
		return
	}

	os.WriteFile(config.MpvPidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0644)
	cmd.Wait()
	os.Remove(config.MpvPidFile)
}

