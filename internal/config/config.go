package config

import (
	"log"
	"os"
	"path/filepath"
	"sync"
)

var (
	CacheDir             string
	CacheDBFile          string
	MpvPidFile           string
	MpvSocketFile        string
	DaemonPidFile        string
	LogFile              string
	HistoryFile          string
	QueueFile            string
	FavoritesFile        string
	AudioFormat          = "m4a"
	CacheMaxBytes        = int64(500 * 1024 * 1024) // 500 MB hard cap; LRU eviction enforces this
	CacheExpiryDays      = 7
	LockStaleSeconds     = 3600.0
	MinValidFileBytes    = int64(50 * 1024)
	YtDlpSocketTimeout   = "8"
	YtDlpRetries         = 3
	YtDlpTimeout         = 30
	RetryBackoffSeconds  = 1
	MaxVideoDurationH    = 2
	FfprobeSkipWindowSec = 3600.0

	RequiredDeps = []string{"yt-dlp", "mpv", "ffmpeg", "ffprobe"}
	Logger       *log.Logger
)

// InitPaths resolves all runtime file paths and must be called once at
// startup before any other package code accesses path variables.
//
// Override the cache root with the PLAY_CACHE_DIR environment variable.
// Otherwise the OS-appropriate user cache directory is used:
//
//	Linux/macOS  ~/.cache/play/music   (via os.UserCacheDir)
//	Windows      %LocalAppData%\play\music
func InitPaths() {
	if override := os.Getenv("PLAY_CACHE_DIR"); override != "" {
		CacheDir = override
	} else {
		base, err := os.UserCacheDir()
		if err != nil {
			// Fallback: place next to home directory
			home, herr := os.UserHomeDir()
			if herr != nil {
				panic("play: cannot determine cache or home directory")
			}
			base = filepath.Join(home, ".cache")
		}
		CacheDir = filepath.Join(base, "play", "music")
	}

	CacheDBFile   = filepath.Join(CacheDir, ".cache.db")
	MpvPidFile    = filepath.Join(CacheDir, ".mpv.pid")
	MpvSocketFile = filepath.Join(CacheDir, ".mpv.sock")
	DaemonPidFile = filepath.Join(CacheDir, ".daemon.pid")
	LogFile = filepath.Join(CacheDir, "player.log")
	HistoryFile = filepath.Join(CacheDir, "history.log")
	QueueFile = filepath.Join(CacheDir, "queue.txt")
	FavoritesFile = filepath.Join(CacheDir, "favorites.txt")
}

type rotatingWriter struct {
	mu       sync.Mutex
	filename string
	maxBytes int64
	file     *os.File
	size     int64
}

func (w *rotatingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}

	if w.size+int64(len(p)) > w.maxBytes {
		w.rotate()
	}

	n, err = w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) open() error {
	info, err := os.Stat(w.filename)
	if err == nil {
		w.size = info.Size()
	}
	f, err := os.OpenFile(w.filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	w.file = f
	return err
}

func (w *rotatingWriter) rotate() {
	if w.file != nil {
		w.file.Close()
	}
	os.Rename(w.filename, w.filename+".1")
	w.open()
}

func SetupLogger() {
	os.MkdirAll(CacheDir, 0755)
	writer := &rotatingWriter{
		filename: LogFile,
		maxBytes: 1 * 1024 * 1024,
	}
	Logger = log.New(writer, "", log.LstdFlags)
}
