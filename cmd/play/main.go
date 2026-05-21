package main

import (
	"os"
	"os/exec"

	"play/internal/cli/parser"
	"play/internal/cli/types"
	"play/internal/config"
	"play/internal/player"
	"play/internal/ui"
)

// installHint returns a platform-agnostic install suggestion for a missing tool.
var installHint = map[string]string{
	"yt-dlp":  "https://github.com/yt-dlp/yt-dlp#installation",
	"mpv":     "https://mpv.io/installation/",
	"ffmpeg":  "https://ffmpeg.org/download.html",
	"ffprobe": "https://ffmpeg.org/download.html (bundled with ffmpeg)",
}

func checkDependencies() {
	missing := false
	for _, dep := range config.RequiredDeps {
		if _, err := exec.LookPath(dep); err != nil {
			if hint := installHint[dep]; hint != "" {
				ui.SafePrintf("Error: %q not found in PATH — install it from: %s\n", dep, hint)
			} else {
				ui.SafePrintf("Error: %q not found in PATH\n", dep)
			}
			missing = true
		}
	}
	if missing {
		os.Exit(1)
	}
}

func main() {
	// InitPaths MUST be first — every branch below, including the internal
	// hooks, depends on CacheDir, QueueFile, MpvSocketFile, etc. being set.
	// Previously the hooks returned before InitPaths was reached, leaving all
	// path variables as empty strings and silently breaking all file I/O.
	config.InitPaths()

	// ── Internal process hooks ────────────────────────────────────────────────
	// Intercepted directly from os.Args so the parser never sees them.
	// InitPaths above is the only setup they need.
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "--internal-bg-download":
			player.RunBackgroundDownloader(os.Args[2:])
			return
		case "--internal-queue-daemon":
			loop := len(os.Args) >= 3 && os.Args[2] == "--loop"
			player.RunQueueDaemon(loop)
			return
		}
	}

	// ── Normal startup ────────────────────────────────────────────────────────
	config.WriteDefaultKeybindings()
	config.LoadKeybindings()
	config.SetupLogger()
	checkDependencies()

	cmd := parser.ParseOS()

	if cmd.Cmd == types.CmdInternalBgDownload || cmd.Cmd == types.CmdInternalQueueDaemon {
		return
	}

	execute(cmd)
}
