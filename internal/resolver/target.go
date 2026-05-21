// Package resolver turns a raw user query into a fully-resolved PlaybackTarget.
//
// It owns:
//   - query classification (search term / YouTube URL / bare video ID)
//   - cache lookup and file validation
//   - metadata repair for legacy index entries
//   - stream-URL refresh when the CDN link has expired
//   - persistence: writing updated entries back to the cache index
//
// Nothing above this layer (executor, daemon, TUI) should import youtube or
// touch cache.SaveEntry directly. Everything below this layer (player, cache,
// youtube) has no knowledge of resolution strategy.
package resolver

import "time"

// SourceType describes how the user originally expressed their intent.
// It is attached to the resolved target so downstream layers (cache tagging,
// manifest display) know the provenance of each entry.
type SourceType string

const (
	SourceSearch SourceType = "search" // human text, e.g. "never gonna give you up"
	SourceURL    SourceType = "url"    // full https://www.youtube.com/watch?v=… URL
	SourceID     SourceType = "id"     // bare 11-character YouTube video ID
)

// PlaybackTarget is the fully-resolved, ready-to-play description of a track.
//
// After Resolve() returns a non-zero target, the caller knows:
//   - exactly which file to play (CachedFile) or which URL to stream (StreamURL)
//   - all human-readable metadata (Title, Artist, …)
//   - whether the audio is already on disk (FileReady) or still downloading
//
// No further network calls or cache queries are needed by the playback layer.
type PlaybackTarget struct {
	// VideoID is the canonical YouTube video ID (always populated).
	VideoID string

	// Title / Artist / Album / Channel / UploadDate are human-readable fields
	// derived from yt-dlp metadata. Title is always non-empty; the others may
	// be empty for older index entries that pre-date the metadata columns.
	Title      string
	Artist     string
	Album      string
	Channel    string
	UploadDate string

	// LookupKey is the query string that was used to find (or store) this
	// entry in the cache index. It is the key that must be queued so the
	// daemon can look it up on pop. For new entries this equals the raw user
	// query; for cache hits it equals whatever key produced the index hit.
	LookupKey string

	// SourceType describes the user's original input form.
	// "search" | "url" | "id"
	SourceType SourceType

	// SourceURL is the canonical https://www.youtube.com/watch?v=<ID> URL.
	SourceURL string

	// CachedFile is the absolute path where the audio file lives (or will
	// live after the background download completes). Always populated.
	CachedFile string

	// FileReady is true when CachedFile already exists and passed validation.
	// False means a background download has been started; the caller should
	// stream from StreamURL instead.
	FileReady bool

	// StreamURL is a short-lived CDN audio URL (~6 h TTL). Used for immediate
	// playback when FileReady is false, and refreshed in the index on every
	// non-cached resolution so the daemon can skip yt-dlp on the next run.
	StreamURL string

	// StreamExpiry is when StreamURL becomes stale. Zero means unknown / not set.
	StreamExpiry time.Time
}

