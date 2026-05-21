// Package playlist loads and plays .playlist files.
//
// File format
// -----------
// A playlist is a plain text file, one search term per line:
//
//	# evening mix
//	tame impala let it happen
//	four tet parallel
//	bicep glue
//
// Rules:
//   - Lines beginning with # are comments and are ignored.
//   - Blank lines are ignored.
//   - Each non-comment line is passed verbatim to cache.SearchManifest,
//     exactly as if the user had typed it at the CLI.  The first result
//     is used.  This means playlists are resilient to metadata changes
//     and are fully human-editable.
//
// Playlist files are stored in PlaylistDir (default ~/.config/play/playlists/).
// The extension is ".playlist"; callers may omit it when addressing a file by name.
//
// Usage
// -----
//
//	play playlist morning             → play morning.playlist immediately
//	play playlist queue morning       → enqueue every track without stopping playback
//	play playlist list                → list all saved playlists
//	play playlist show morning        → print the raw file contents
//	play playlist add morning "bicep glue" → append a line to an existing playlist

package playlist

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"play/internal/cache"
	"play/internal/config"
	"play/internal/queue"
)

// ---------------------------------------------------------------------------
// Paths
// ---------------------------------------------------------------------------

// Dir returns the directory that holds all playlist files.
// Uses PLAY_PLAYLIST_DIR env var if set, otherwise ~/.config/play/playlists/.
func Dir() string {
	if d := os.Getenv("PLAY_PLAYLIST_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback: store next to the cache dir.
		return filepath.Join(config.CacheDir, "playlists")
	}
	return filepath.Join(home, ".config", "play", "playlists")
}

// Path resolves a playlist name to its full path.
// If name already ends with ".playlist" it is used as-is.
// If name contains a path separator it is treated as an absolute or relative path.
func Path(name string) string {
	if strings.ContainsRune(name, os.PathSeparator) || filepath.IsAbs(name) {
		return name
	}
	if !strings.HasSuffix(name, ".playlist") {
		name += ".playlist"
	}
	return filepath.Join(Dir(), name)
}

// ---------------------------------------------------------------------------
// Core operations
// ---------------------------------------------------------------------------

// Load reads a playlist file and returns the non-empty, non-comment lines.
// Lines are returned exactly as written — no normalisation — so the caller
// can pass them to SearchManifest unchanged.
func Load(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("playlist: cannot open %q: %w", path, err)
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("playlist: read error in %q: %w", path, err)
	}
	return lines, nil
}

// Resolve turns a slice of search terms into a slice of cache.IndexEntry values.
// Each term is looked up via cache.SearchManifest; the first result is used.
//
// Terms that match nothing are collected and returned as a separate slice so
// the caller can report them to the user without aborting the whole playlist.
func Resolve(terms []string) (hits []cache.IndexEntry, misses []string) {
	for _, term := range terms {
		results, err := cache.SearchManifest([]string{term})
		if err != nil || len(results) == 0 {
			misses = append(misses, term)
			continue
		}
		// SearchManifest returns ManifestEntry; convert to IndexEntry so the
		// caller works with a single type.  Only the fields the queue needs
		// (VideoID, Title, LookupKey) are populated here.
		m := results[0]
		hits = append(hits, cache.IndexEntry{
			VideoID: m.VideoID,
			Title:   m.Title,
			// Use the human query as the lookup key when available, falling
			// back to the video ID (which the daemon can always resolve).
			Query: firstNonEmpty(m.Query, m.VideoID),
		})
	}
	return hits, misses
}

// Enqueue adds every entry in hits to the queue without touching playback.
// Returns the number of items enqueued.
func Enqueue(hits []cache.IndexEntry) int {
	for _, e := range hits {
		key := firstNonEmpty(e.Query, e.VideoID)
		queue.Add(key)
	}
	return len(hits)
}

// ---------------------------------------------------------------------------
// File management
// ---------------------------------------------------------------------------

// List returns the names (without the .playlist extension) of all playlists
// in Dir(), sorted alphabetically by filename.
func List() ([]string, error) {
	entries, err := os.ReadDir(Dir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no playlists yet — not an error
		}
		return nil, fmt.Errorf("playlist: cannot read playlist dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".playlist") {
			names = append(names, strings.TrimSuffix(name, ".playlist"))
		}
	}
	return names, nil
}

// AppendLine adds a single search term to an existing (or new) playlist file.
// The playlist directory is created automatically if it does not exist.
func AppendLine(name, term string) error {
	term = strings.TrimSpace(term)
	if term == "" {
		return fmt.Errorf("playlist: cannot append empty line")
	}

	path := Path(name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("playlist: cannot create playlist dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("playlist: cannot open %q for writing: %w", path, err)
	}
	defer f.Close()

	_, err = fmt.Fprintln(f, term)
	return err
}

// Show prints the raw contents of a playlist file to stdout, with line numbers.
func Show(name string) error {
	path := Path(name)
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("playlist %q not found", name)
	}
	defer f.Close()

	fmt.Printf("📋  %s\n\n", name)
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			fmt.Println(line) // print comments and blanks verbatim
		} else {
			n++
			fmt.Printf("  %2d.  %s\n", n, line)
		}
	}
	return sc.Err()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
