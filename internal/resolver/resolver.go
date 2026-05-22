package resolver

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"play/internal/cache"
	"play/internal/config"
	"play/internal/youtube"
)

// StreamURLTTL is how long a freshly-fetched CDN stream URL is considered
// valid. yt-dlp returns signed URLs that typically expire after ~6 hours;
// keeping the TTL slightly under that avoids serving stale links.
const StreamURLTTL = 5 * time.Hour

// bgWork tracks all background goroutines started by this package.
// Call WaitBackground() before process exit to let in-flight repairs finish.
var bgWork sync.WaitGroup

// WaitBackground blocks until all background metadata-repair goroutines have
// finished. Call this during graceful shutdown so that in-flight index writes
// are not silently killed mid-transaction.
func WaitBackground() {
	bgWork.Wait()
}

// ClassifySource returns the SourceType for a raw query string without doing
// any network I/O. It is the single authoritative definition of this heuristic
// in the codebase — executor, daemon, and tests all call this rather than
// duplicating the logic.
func ClassifySource(query string) SourceType {
	if strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://") {
		return SourceURL
	}
	if IsVideoID(query) {
		return SourceID
	}
	return SourceSearch
}

// IsVideoID returns true for a bare 11-character YouTube video ID.
// YouTube IDs are exactly 11 characters drawn from [A-Za-z0-9_-].
// This is a structural heuristic, not a network check.
func IsVideoID(s string) bool {
	if len(s) != 11 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// WatchURL returns the canonical YouTube watch URL for a video ID.
func WatchURL(videoID string) string {
	return fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)
}

// cachedFilePath returns the expected on-disk path for a video ID's audio file.
func cachedFilePath(videoID string) string {
	return filepath.Join(config.CacheDir, fmt.Sprintf("%s.%s", videoID, config.AudioFormat))
}

// persistEntry writes entry under both the lookup key and the video ID so the
// index supports replay by either form. It is a no-op when key == entry.VideoID
// (only one write needed). Returns the first error encountered, if any.
func persistEntry(key string, entry cache.IndexEntry) error {
	if err := cache.SaveEntry(key, entry); err != nil {
		return fmt.Errorf("SaveEntry(%q): %w", key, err)
	}
	if key != entry.VideoID {
		if err := cache.SaveEntry(entry.VideoID, entry); err != nil {
			return fmt.Errorf("SaveEntry(%q): %w", entry.VideoID, err)
		}
	}
	return nil
}

// displayTitle returns a non-empty string suitable for showing the user.
// Falls back to the video ID when the title column is blank (legacy entries).
func displayTitle(entry cache.IndexEntry) string {
	if entry.Title != "" {
		return entry.Title
	}
	return entry.VideoID
}

// Resolve turns a raw user query into a PlaybackTarget.
//
// Resolution order:
//  1. Cache index lookup — if the query maps to a known video:
//     a. Audio file on disk and valid → zero-network cache hit.
//     b. File missing but stream URL still fresh → background download started,
//        stream URL returned for immediate playback.
//     c. Stream URL expired → refetch metadata by video URL (faster than
//        re-running the original search), then fall through to (b).
//  2. Full online fetch — query is unknown, or the index lookup failed.
//     yt-dlp resolves the query to a video, metadata is persisted, audio
//     download is started in the background.
//
// A zero-value PlaybackTarget (VideoID == "") means resolution failed; callers
// should surface an error to the user.
//
// Resolve never calls os.Exit. All error paths return the zero value so the
// caller decides how to handle failures.
func Resolve(query string) PlaybackTarget {
	// ── Step 1: cache index lookup ────────────────────────────────────────────
	if entry, ok := cache.LookupEntry(query); ok {
		return resolveFromEntry(query, entry)
	}

	// ── Step 2: full online fetch ─────────────────────────────────────────────
	return resolveOnline(query, ClassifySource(query))
}

// resolveFromEntry handles the three sub-cases for a known index entry.
func resolveFromEntry(lookupKey string, entry cache.IndexEntry) PlaybackTarget {
	cachedFile := cachedFilePath(entry.VideoID)

	// ── 1a: file on disk ──────────────────────────────────────────────────────
	if cache.IsCachedFileValid(cachedFile) {
		// Metadata repair is fire-and-forget: the file is ready to play right
		// now, so we return the target immediately and let the repair update the
		// index in the background for next time. This avoids blocking playback
		// with a yt-dlp call when the audio is already on disk.
		if needsRepair(entry) {
			bgWork.Add(1)
			go func() {
				defer bgWork.Done()
				repairMetadata(lookupKey, entry)
			}()
		}
		return PlaybackTarget{
			VideoID:    entry.VideoID,
			Title:      displayTitle(entry),
			Artist:     entry.Artist,
			Album:      entry.Album,
			Channel:    entry.Channel,
			UploadDate: entry.UploadDate,
			LookupKey:  lookupKey,
			SourceType: SourceType(entry.SourceType),
			SourceURL:  entry.SourceURL,
			CachedFile: cachedFile,
			FileReady:  true,
		}
	}

	// ── 1b: file absent, stream URL still fresh ───────────────────────────────
	if streamURL := cache.LookupStreamURL(entry.VideoID); streamURL != "" {
		return PlaybackTarget{
			VideoID:    entry.VideoID,
			Title:      displayTitle(entry),
			Artist:     entry.Artist,
			Album:      entry.Album,
			Channel:    entry.Channel,
			UploadDate: entry.UploadDate,
			LookupKey:  lookupKey,
			SourceType: SourceType(entry.SourceType),
			SourceURL:  entry.SourceURL,
			CachedFile: cachedFile,
			FileReady:  false,
			StreamURL:  streamURL,
		}
	}

	// ── 1c: stream URL expired — refetch by video URL ─────────────────────────
	// Using the canonical watch URL (not the original query) is faster: yt-dlp
	// fetches a specific video rather than running a search.
	meta := youtube.FetchMetadata(WatchURL(entry.VideoID))
	if meta.VideoID == "" {
		// Network failure or video removed. Signal failure to the caller.
		return PlaybackTarget{}
	}

	updated := cache.IndexEntry{
		VideoID:    meta.VideoID,
		Title:      meta.Title,
		Artist:     meta.Artist,
		Album:      meta.Album,
		Channel:    meta.Channel,
		UploadDate: meta.UploadDate,
		Query:      entry.Query,      // preserve original human query
		SourceURL:  WatchURL(meta.VideoID),
		SourceType: entry.SourceType, // preserve original source type
	}
	if err := persistEntry(lookupKey, updated); err != nil && config.Logger != nil {
		config.Logger.Printf("resolver: persistEntry after stream-URL refresh: %v", err)
	}
	if err := cache.SaveStreamURL(meta.VideoID, meta.StreamURL, StreamURLTTL); err != nil && config.Logger != nil {
		config.Logger.Printf("resolver: SaveStreamURL after stream-URL refresh: %v", err)
	}

	return PlaybackTarget{
		VideoID:    meta.VideoID,
		Title:      meta.Title,
		Artist:     meta.Artist,
		Album:      meta.Album,
		Channel:    meta.Channel,
		UploadDate: meta.UploadDate,
		LookupKey:  lookupKey,
		SourceType: SourceType(entry.SourceType),
		SourceURL:  WatchURL(meta.VideoID),
		CachedFile: cachedFile,
		FileReady:  false,
		StreamURL:  meta.StreamURL,
	}
}

// resolveOnline handles a query that is not in the cache index at all.
func resolveOnline(query string, sourceType SourceType) PlaybackTarget {
	meta := youtube.FetchMetadata(query)
	if meta.VideoID == "" {
		return PlaybackTarget{}
	}

	entry := cache.IndexEntry{
		VideoID:    meta.VideoID,
		Title:      meta.Title,
		Artist:     meta.Artist,
		Album:      meta.Album,
		Channel:    meta.Channel,
		UploadDate: meta.UploadDate,
		Query:      query,
		SourceURL:  WatchURL(meta.VideoID),
		SourceType: string(sourceType),
	}
	if err := persistEntry(query, entry); err != nil && config.Logger != nil {
		config.Logger.Printf("resolver: persistEntry for new query %q: %v", query, err)
	}
	if err := cache.SaveStreamURL(meta.VideoID, meta.StreamURL, StreamURLTTL); err != nil && config.Logger != nil {
		config.Logger.Printf("resolver: SaveStreamURL for %s: %v", meta.VideoID, err)
	}

	cachedFile := cachedFilePath(meta.VideoID)
	return PlaybackTarget{
		VideoID:    meta.VideoID,
		Title:      meta.Title,
		Artist:     meta.Artist,
		Album:      meta.Album,
		Channel:    meta.Channel,
		UploadDate: meta.UploadDate,
		LookupKey:  query,
		SourceType: sourceType,
		SourceURL:  WatchURL(meta.VideoID),
		CachedFile: cachedFile,
		FileReady:  cache.IsCachedFileValid(cachedFile),
		StreamURL:  meta.StreamURL,
	}
}

// needsRepair reports whether an index entry is missing metadata that a
// yt-dlp call could supply. The check is intentionally broad: an entry that
// has a title and channel but is missing upload_date, source_url, or album is
// still considered incomplete and will be repaired on the next cache hit.
// Without this breadth, partial legacy entries stay partial indefinitely
// because the repair path is the only mechanism that backfills richer fields.
func needsRepair(entry cache.IndexEntry) bool {
	return entry.Title == "" ||
		(entry.Artist == "" && entry.Channel == "") ||
		entry.UploadDate == "" ||
		entry.SourceURL == ""
}

// repairMetadata fetches missing metadata for a cached entry and writes it
// back to the index. It is always called as a tracked goroutine (via bgWork)
// so it must not interact with the UI (no spinner). Errors are logged rather
// than surfaced to the user — the repair is best-effort; the track is already
// playable by the time this runs.
func repairMetadata(lookupKey string, entry cache.IndexEntry) {
	meta := youtube.FetchMetadataSilent(WatchURL(entry.VideoID))
	if meta.VideoID == "" {
		return // network failure — index stays as-is, will retry next play
	}

	repaired := cache.IndexEntry{
		VideoID:    meta.VideoID,
		Title:      meta.Title,
		Artist:     meta.Artist,
		Album:      meta.Album,
		Channel:    meta.Channel,
		UploadDate: meta.UploadDate,
		Query:      entry.Query,
		SourceURL:  WatchURL(meta.VideoID),
		SourceType: entry.SourceType,
	}
	if err := persistEntry(lookupKey, repaired); err != nil && config.Logger != nil {
		config.Logger.Printf("repairMetadata: persistEntry for %s: %v", entry.VideoID, err)
	}
	if err := cache.SaveStreamURL(meta.VideoID, meta.StreamURL, StreamURLTTL); err != nil && config.Logger != nil {
		config.Logger.Printf("repairMetadata: SaveStreamURL for %s: %v", meta.VideoID, err)
	}
}
