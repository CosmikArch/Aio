package cache

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"play/internal/config"
	"play/internal/ui"
)

// TagCachedFiles walks the cache directory, looks up metadata for every .m4a
// file from the DB, and writes MP4 tags directly into the file using ffmpeg.
//
// Tags written:
//   - title       → track title from DB
//   - artist      → artist field, falling back to channel name
//   - album_artist → channel name (uploader), falling back to artist
//   - album        → album field if present
//   - date         → upload_date reformatted as YYYY-MM-DD
//   - comment      → YouTube video ID (for traceability by tools like moe)
//
// The video ID is taken from the filename stem (e.g. "dQw4w9WgXcQ.m4a" →
// "dQw4w9WgXcQ") and used to query the DB. Files with no DB entry are skipped
// without modification.
//
// Each file is re-muxed in-place via a temporary file that is atomically
// renamed over the original on success, so a failed tag run never corrupts
// the cached audio.
//
// Pass dryRun=true to print what would be written without touching any files.
func TagCachedFiles(dryRun bool) {
	audioSuffix := "." + config.AudioFormat
	if audioSuffix == "." {
		ui.SafePrintln("cache tag: AudioFormat is not configured; aborting.")
		return
	}

	entries, err := os.ReadDir(config.CacheDir)
	if err != nil {
		ui.SafePrintf("cache tag: cannot read cache dir: %v\n", err)
		return
	}

	// Count only audio files so the summary line is meaningful.
	var total, tagged, skipped, failed int

	for _, e := range entries {
		name := e.Name()
		// Skip directories, dot-files (locks, DB, pid, sock), and any file
		// that is not a plain audio file — e.g. player.log, history.log,
		// queue.txt, favorites.txt, *.tmp, *.work.* scratch dirs, *.1
		// rotated logs, or any other non-audio file stored in the cache dir.
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, audioSuffix) {
			continue
		}
		total++

		videoID := strings.TrimSuffix(name, audioSuffix)
		filePath := filepath.Join(config.CacheDir, name)

		meta, ok := lookupMetaByVideoID(videoID)
		if !ok {
			ui.SafePrintf("  skip  %-11s  (no DB entry)\n", videoID)
			skipped++
			continue
		}

		artist := meta.Artist
		if artist == "" {
			artist = meta.Channel
		}
		albumArtist := meta.Channel
		if albumArtist == "" {
			albumArtist = artist
		}
		date := formatUploadDate(meta.UploadDate)

		if dryRun {
			ui.SafePrintf("  dry   %-11s  title=%q  artist=%q  album=%q  date=%s\n",
				videoID, meta.Title, artist, meta.Album, date)
			tagged++
			continue
		}

		if err := writeMP4Tags(filePath, meta.Title, artist, albumArtist, meta.Album, date, videoID); err != nil {
			ui.SafePrintf("  fail  %-11s  %v\n", videoID, err)
			failed++
		} else {
			ui.SafePrintf("  tag   %-11s  %q\n", videoID, meta.Title)
			tagged++
		}
	}

	if total == 0 {
		ui.SafePrintln("cache tag: no .m4a files found in cache.")
		return
	}

	verb := "tagged"
	if dryRun {
		verb = "would tag"
	}
	ui.SafePrintf("\n%d file(s) scanned: %d %s, %d skipped (no DB entry), %d failed\n",
		total, tagged, verb, skipped, failed)
}

// lookupMetaByVideoID queries the DB for the best available metadata row for
// a given video ID.  It uses MAX() aggregation so a video that has multiple
// cache_index keys (e.g. from both a search hit and a direct-ID lookup) still
// returns a single, fully-populated record.
func lookupMetaByVideoID(videoID string) (IndexEntry, bool) {
	d := getDB()

	var e IndexEntry
	err := d.QueryRow(`
		SELECT
		    video_id,
		    MAX(title)       AS title,
		    MAX(artist)      AS artist,
		    MAX(album)       AS album,
		    MAX(channel)     AS channel,
		    MAX(upload_date) AS upload_date
		FROM cache_index
		WHERE video_id = ?
		GROUP BY video_id
	`, videoID).Scan(
		&e.VideoID, &e.Title, &e.Artist, &e.Album,
		&e.Channel, &e.UploadDate,
	)
	if err != nil {
		return IndexEntry{}, false
	}
	return e, true
}

// formatUploadDate converts the "YYYYMMDD" string that yt-dlp emits into
// the "YYYY-MM-DD" form that most tag readers and players expect.
// Any input that is not exactly 8 bytes is returned unchanged.
func formatUploadDate(s string) string {
	if len(s) == 8 {
		return s[:4] + "-" + s[4:6] + "-" + s[6:]
	}
	return s
}

// writeMP4Tags re-muxes path in-place, replacing all metadata atoms with
// the supplied fields.  The flow is:
//
//  1. ffmpeg -i <original> -c copy -map_metadata -1 -metadata k=v … <tmp>
//  2. os.Rename(tmp, original)   ← atomic on same filesystem
//
// -map_metadata -1 clears any pre-existing tags so that stale or incorrect
// values left by yt-dlp (e.g. a missing artist field) are not carried over.
// Only non-empty values are written; blank tag atoms are not created.
func writeMP4Tags(path, title, artist, albumArtist, album, date, comment string) error {
	tmp := path + ".tag.tmp"

	// Always clean up the temp file on any error path.
	defer func() {
		if _, err := os.Stat(tmp); err == nil {
			os.Remove(tmp)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	args := []string{
		"-y",              // overwrite tmp without prompting
		"-loglevel", "error", // suppress ffmpeg's verbose banner
		"-i", path,
		"-c", "copy",       // audio passthrough — no re-encode
		"-map_metadata", "-1", // clear all existing metadata first
	}

	addMeta := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			args = append(args, "-metadata", k+"="+v)
		}
	}
	addMeta("title", title)
	addMeta("artist", artist)
	addMeta("album_artist", albumArtist)
	addMeta("album", album)
	addMeta("date", date)
	addMeta("comment", comment) // video ID — useful for moe / other tools

	args = append(args, tmp)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return fmt.Errorf("ffmpeg: %s", errMsg)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
