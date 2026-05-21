package cache

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"play/internal/config"

	_ "modernc.org/sqlite"
)

// IndexEntry holds every piece of resolved metadata for a played track.
type IndexEntry struct {
	VideoID    string    // YouTube video ID
	Title      string    // video / track title
	Artist     string    // music artist tag, or channel fallback
	Album      string    // album tag if present
	Channel    string    // uploader channel name
	UploadDate string    // "YYYYMMDD" as returned by yt-dlp
	Query      string    // raw human search string (never a video ID)
	SourceURL  string    // resolved YouTube watch URL
	SourceType string    // "search" | "url" | "id"
	CachedAt   time.Time
	LastUsed   time.Time
	HitCount   int
}

const schema = `
CREATE TABLE IF NOT EXISTS cache_index (
    key               TEXT    PRIMARY KEY,
    video_id          TEXT    NOT NULL,
    title             TEXT    NOT NULL DEFAULT '',
    artist            TEXT    NOT NULL DEFAULT '',
    album             TEXT    NOT NULL DEFAULT '',
    channel           TEXT    NOT NULL DEFAULT '',
    upload_date       TEXT    NOT NULL DEFAULT '',
    query             TEXT    NOT NULL DEFAULT '',
    source_url        TEXT    NOT NULL DEFAULT '',
    source_type       TEXT    NOT NULL DEFAULT '',
    cached_at         INTEGER NOT NULL,
    last_used         INTEGER NOT NULL,
    hit_count         INTEGER NOT NULL DEFAULT 0,
    stream_url        TEXT    NOT NULL DEFAULT '',
    stream_expires_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_video_id  ON cache_index (video_id);
CREATE INDEX IF NOT EXISTS idx_cached_at ON cache_index (cached_at);
CREATE INDEX IF NOT EXISTS idx_last_used ON cache_index (last_used);
`

// migrations adds columns introduced after the initial schema.
// SQLite errors on duplicate column names; ignoring those errors is the
// idiomatic migration pattern — no version table required.
// Rule: indexes on new columns must come AFTER their ALTER TABLE statement.
var migrations = []string{
	"ALTER TABLE cache_index ADD COLUMN artist            TEXT    NOT NULL DEFAULT ''",
	"CREATE INDEX IF NOT EXISTS idx_artist ON cache_index (artist)",
	"ALTER TABLE cache_index ADD COLUMN album             TEXT    NOT NULL DEFAULT ''",
	"ALTER TABLE cache_index ADD COLUMN channel           TEXT    NOT NULL DEFAULT ''",
	"ALTER TABLE cache_index ADD COLUMN upload_date       TEXT    NOT NULL DEFAULT ''",
	"ALTER TABLE cache_index ADD COLUMN query             TEXT    NOT NULL DEFAULT ''",
	"ALTER TABLE cache_index ADD COLUMN source_url        TEXT    NOT NULL DEFAULT ''",
	"ALTER TABLE cache_index ADD COLUMN source_type       TEXT    NOT NULL DEFAULT ''",
	"ALTER TABLE cache_index ADD COLUMN stream_url        TEXT    NOT NULL DEFAULT ''",
	"ALTER TABLE cache_index ADD COLUMN stream_expires_at INTEGER NOT NULL DEFAULT 0",
}

var (
	dbOnce sync.Once
	gdb    *sql.DB
)

func getDB() *sql.DB {
	dbOnce.Do(func() {
		if err := os.MkdirAll(config.CacheDir, 0755); err != nil {
			panic("cache: cannot create cache dir: " + err.Error())
		}

		d, err := sql.Open("sqlite", config.CacheDBFile)
		if err != nil {
			panic("cache: cannot open db: " + err.Error())
		}
		d.SetMaxOpenConns(1)

		for _, p := range []string{
			"PRAGMA journal_mode = WAL",
			"PRAGMA synchronous  = NORMAL",
			"PRAGMA busy_timeout = 5000",
			"PRAGMA foreign_keys = ON",
		} {
			if _, err := d.Exec(p); err != nil {
				panic("cache: pragma failed (" + p + "): " + err.Error())
			}
		}

		if _, err := d.Exec(schema); err != nil {
			panic("cache: schema init failed: " + err.Error())
		}

		for _, m := range migrations {
			d.Exec(m) // duplicate-column / duplicate-index errors intentionally ignored
		}

		gdb = d
		importLegacyJSON(d)
	})
	return gdb
}

func importLegacyJSON(d *sql.DB) {
	legacyPath := filepath.Join(config.CacheDir, ".index.json")
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return
	}

	type legacyEntry struct {
		VideoID string `json:"video_id"`
		Title   string `json:"title"`
	}
	var m map[string]legacyEntry
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}

	tx, err := d.Begin()
	if err != nil {
		return
	}
	now := time.Now().Unix()
	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO cache_index(key, video_id, title, cached_at, last_used, hit_count)
		VALUES (?, ?, ?, ?, ?, 0)
	`)
	if err != nil {
		tx.Rollback()
		return
	}
	defer stmt.Close()

	for key, e := range m {
		stmt.Exec(key, e.VideoID, e.Title, now, now)
	}
	if err := tx.Commit(); err != nil {
		tx.Rollback()
		return
	}
	os.Remove(legacyPath)
}

// LookupEntry returns the cached IndexEntry for key. On a hit it bumps
// last_used and hit_count in the background so the hot path is never blocked.
func LookupEntry(query string) (IndexEntry, bool) {
	d := getDB()

	var e IndexEntry
	var cachedAt, lastUsed int64

	err := d.QueryRow(`
		SELECT video_id, title, artist, album, channel, upload_date,
		       query, source_url, source_type,
		       cached_at, last_used, hit_count
		FROM cache_index WHERE key = ?
	`, query).Scan(
		&e.VideoID, &e.Title, &e.Artist, &e.Album, &e.Channel,
		&e.UploadDate, &e.Query, &e.SourceURL, &e.SourceType,
		&cachedAt, &lastUsed, &e.HitCount,
	)
	if err != nil {
		return IndexEntry{}, false
	}

	e.CachedAt = time.Unix(cachedAt, 0)
	e.LastUsed = time.Unix(lastUsed, 0)

	now := time.Now().Unix()
	go d.Exec(`
		UPDATE cache_index SET last_used = ?, hit_count = hit_count + 1
		WHERE key = ?
	`, now, query)

	return e, true
}

// SaveEntry persists a key → IndexEntry mapping. On conflict all metadata
// fields and last_used are refreshed; cached_at and hit_count are preserved.
func SaveEntry(key string, entry IndexEntry) {
	d := getDB()
	now := time.Now().Unix()

	d.Exec(`
		INSERT INTO cache_index
		    (key, video_id, title, artist, album, channel, upload_date,
		     query, source_url, source_type, cached_at, last_used, hit_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(key) DO UPDATE SET
		    video_id    = excluded.video_id,
		    title       = excluded.title,
		    artist      = excluded.artist,
		    album       = excluded.album,
		    channel     = excluded.channel,
		    upload_date = excluded.upload_date,
		    query       = excluded.query,
		    source_url  = excluded.source_url,
		    source_type = excluded.source_type,
		    last_used   = excluded.last_used
	`,
		key,
		entry.VideoID, entry.Title, entry.Artist, entry.Album,
		entry.Channel, entry.UploadDate,
		entry.Query, entry.SourceURL, entry.SourceType,
		now, now,
	)
}

// SaveStreamURL stores a short-lived CDN stream URL for all rows with this video_id.
func SaveStreamURL(videoID, streamURL string, ttl time.Duration) {
	if videoID == "" || streamURL == "" {
		return
	}
	d := getDB()
	expires := time.Now().Add(ttl).Unix()
	d.Exec(`
		UPDATE cache_index SET stream_url = ?, stream_expires_at = ?
		WHERE video_id = ?
	`, streamURL, expires, videoID)
}

// LookupStreamURL returns a non-expired direct stream URL for videoID, or "".
func LookupStreamURL(videoID string) string {
	if videoID == "" {
		return ""
	}
	d := getDB()
	now := time.Now().Unix()

	var url string
	d.QueryRow(`
		SELECT stream_url FROM cache_index
		WHERE video_id = ? AND stream_expires_at > ? AND stream_url != ''
		LIMIT 1
	`, videoID, now).Scan(&url)
	return url
}

// PurgeEntry removes every row whose video_id matches — O(log n) via index.
func PurgeEntry(videoID string) {
	d := getDB()
	d.Exec(`DELETE FROM cache_index WHERE video_id = ?`, videoID)
}

// ManifestEntry is the public JSON-serialisable shape of one cached track.
type ManifestEntry struct {
	VideoID    string `json:"video_id"`
	Title      string `json:"title"`
	Artist     string `json:"artist,omitempty"`
	Album      string `json:"album,omitempty"`
	Channel    string `json:"channel,omitempty"`
	UploadDate string `json:"upload_date,omitempty"`
	Query      string `json:"query,omitempty"`
	SourceURL  string `json:"source_url,omitempty"`
	SourceType string `json:"source_type,omitempty"`
	HitCount   int    `json:"hit_count"`
	CachedAt   string `json:"cached_at"`
	LastUsed   string `json:"last_used,omitempty"`
	Status     string `json:"status"`
}

// manifestQuery is the shared SELECT used by both ListManifest and SearchManifest.
// It groups by video_id, picks the human query from the non-ID key row, and
// aggregates timestamps across all keys for the same video.
const manifestQuery = `
	SELECT
	    video_id,
	    MAX(title)                                            AS title,
	    MAX(artist)                                          AS artist,
	    MAX(album)                                           AS album,
	    MAX(channel)                                         AS channel,
	    MAX(upload_date)                                     AS upload_date,
	    MAX(CASE WHEN key != video_id AND query != '' AND query != key
	            THEN query ELSE '' END)                      AS query,
	    MAX(source_url)                                      AS source_url,
	    MAX(source_type)                                     AS source_type,
	    SUM(hit_count)                                       AS hit_count,
	    MIN(cached_at)                                       AS cached_at,
	    MAX(last_used)                                       AS last_used
	FROM cache_index
`

func scanManifestRows(rows *sql.Rows) ([]ManifestEntry, error) {
	defer rows.Close()

	var entries []ManifestEntry
	for rows.Next() {
		var e ManifestEntry
		var cachedAt, lastUsed int64

		if err := rows.Scan(
			&e.VideoID, &e.Title, &e.Artist, &e.Album, &e.Channel,
			&e.UploadDate, &e.Query, &e.SourceURL, &e.SourceType,
			&e.HitCount, &cachedAt, &lastUsed,
		); err != nil {
			continue
		}

		e.CachedAt = time.Unix(cachedAt, 0).Format(time.RFC3339)
		if lastUsed > 0 {
			e.LastUsed = time.Unix(lastUsed, 0).Format(time.RFC3339)
		}
		e.Status = GetTrackStatus(e.VideoID).String()
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ListManifest returns one ManifestEntry per unique video_id, newest first.
func ListManifest() ([]ManifestEntry, error) {
	d := getDB()
	rows, err := d.Query(manifestQuery + `
		GROUP BY video_id
		ORDER BY cached_at DESC
	`)
	if err != nil {
		return nil, err
	}
	return scanManifestRows(rows)
}

// SearchManifest returns entries whose title, artist, album, channel, query,
// or video_id contain ALL of the provided terms (case-insensitive AND logic).
// An empty terms slice returns all entries, identical to ListManifest.
func SearchManifest(terms []string) ([]ManifestEntry, error) {
	if len(terms) == 0 {
		return ListManifest()
	}

	d := getDB()

	// Build a WHERE clause that ANDs one predicate block per term.
	// Each predicate ORs across all searchable columns.
	// Using LOWER() + LIKE for portable case-insensitive matching without
	// needing a COLLATE NOCASE column definition.
	var clauses []string
	var args []interface{}

	for _, term := range terms {
		pattern := "%" + strings.ToLower(term) + "%"
		clauses = append(clauses, `(
			LOWER(title)   LIKE ? OR
			LOWER(artist)  LIKE ? OR
			LOWER(album)   LIKE ? OR
			LOWER(channel) LIKE ? OR
			LOWER(query)   LIKE ? OR
			video_id        =   ?
		)`)
		args = append(args, pattern, pattern, pattern, pattern, pattern, term)
	}

	having := "HAVING " + strings.Join(clauses, " AND ")

	rows, err := d.Query(manifestQuery+`
		GROUP BY video_id
		`+having+`
		ORDER BY last_used DESC
	`, args...)
	if err != nil {
		return nil, err
	}
	return scanManifestRows(rows)
}
