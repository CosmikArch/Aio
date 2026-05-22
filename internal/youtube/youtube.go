package youtube

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"play/internal/config"
	"play/internal/ui"
)

// TrackMeta holds every piece of metadata returned by a single yt-dlp call.
// Using a struct instead of multiple return values makes call sites readable
// and lets us add fields later without touching every caller.
type TrackMeta struct {
	VideoID    string // YouTube video ID, e.g. "dQw4w9WgXcQ"
	Title      string // video title
	Artist     string // music artist tag; falls back to Channel if absent
	Album      string // album tag if present, otherwise empty
	Channel    string // uploader / channel name — always populated
	UploadDate string // "YYYYMMDD" as returned by yt-dlp
	StreamURL  string // direct CDN audio URL (~6 h expiry)
}

// runYtDlpOnce executes yt-dlp once with the given args and a fresh timeout
// context. Wrapping each attempt in its own function lets us use defer to
// guarantee the context is cancelled on every exit path, including panics.
func runYtDlpOnce(args []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.YtDlpTimeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func runYtDlp(extraArgs []string) string {
	baseArgs := []string{
		"--quiet", "--no-warnings",
		"--socket-timeout", config.YtDlpSocketTimeout,
	}
	args := append(baseArgs, extraArgs...)

	for attempt := 1; attempt <= config.YtDlpRetries; attempt++ {
		out, err := runYtDlpOnce(args)
		if err == nil {
			return out
		}

		config.Logger.Printf("yt-dlp attempt %d failed: %v | output: %s", attempt, err, out)
		if attempt < config.YtDlpRetries {
			time.Sleep(time.Duration(config.RetryBackoffSeconds) * time.Second)
		}
	}
	return ""
}

// ytDlpArgs returns the standard argument list for a metadata fetch.
// The search target is already resolved (prefixed with ytsearch1: if needed).
func ytDlpArgs(searchTarget string) []string {
	return []string{
		"-f", "bestaudio",
		"--no-playlist",
		// Seven fields, one per line. Order must match parseMeta below.
		"--print", "%(id)s\n%(title)s\n%(url)s\n%(artist)s\n%(album)s\n%(channel)s\n%(upload_date)s",
		"--match-filter", fmt.Sprintf("!is_live & duration < %d", config.MaxVideoDurationH*3600),
		"--", searchTarget,
	}
}

// searchTarget returns the yt-dlp search target for a query:
// direct URLs are passed as-is; everything else is prefixed with ytsearch1:.
func searchTarget(query string) string {
	if strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://") {
		return query
	}
	return "ytsearch1:" + query
}

// parseMeta parses the seven-line output of a yt-dlp --print call into a
// TrackMeta. Returns a zero value if the output is malformed or the video
// ID / stream URL are missing.
func parseMeta(output string) TrackMeta {
	if output == "" {
		return TrackMeta{}
	}

	parts := strings.SplitN(output, "\n", 7)
	if len(parts) < 7 {
		return TrackMeta{}
	}

	clean := func(s string) string {
		s = strings.TrimSpace(s)
		if s == "NA" || s == "None" {
			return ""
		}
		return s
	}

	meta := TrackMeta{
		VideoID:    clean(parts[0]),
		Title:      clean(parts[1]),
		StreamURL:  clean(parts[2]),
		Artist:     clean(parts[3]),
		Album:      clean(parts[4]),
		Channel:    clean(parts[5]),
		UploadDate: clean(parts[6]),
	}

	if meta.VideoID == "" || meta.StreamURL == "" {
		return TrackMeta{}
	}
	if meta.Title == "" {
		meta.Title = "Unknown Title"
	}
	// NOTE: Artist is intentionally left empty when the tag is absent.
	// Channel is always populated separately. The display-time fallback
	// (artist = channel when artist == "") must be applied by callers, not here.
	// Conflating the two at fetch time makes it impossible to distinguish a
	// real music-artist tag from a channel-name stand-in once the value is stored.

	return meta
}

// FetchMetadata resolves a search query or YouTube URL to a TrackMeta,
// showing a spinner on stdout while yt-dlp runs. Use for foreground
// (interactive) calls where the user is waiting.
func FetchMetadata(query string) TrackMeta {
	target := searchTarget(query)
	spinner := ui.NewSpinner(fmt.Sprintf("Searching for '%s'…", query))
	spinner.Start()
	output := runYtDlp(ytDlpArgs(target))
	spinner.Stop()
	return parseMeta(output)
}

// FetchMetadataSilent resolves a search query or YouTube URL to a TrackMeta
// without displaying a spinner. Use for background / goroutine calls where
// writing to stdout would race with other terminal output.
func FetchMetadataSilent(query string) TrackMeta {
	return parseMeta(runYtDlp(ytDlpArgs(searchTarget(query))))
}

