package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"play/internal/config"
	"play/internal/ui"
	"strings"
)

// printTable is the shared renderer for both list and search results.
func printTable(entries []ManifestEntry, header string) {
	const (
		wStatus = 2
		wID     = 11
		wArtist = 20
		wTitle  = 26
		wQuery  = 22
		wType   = 6
	)

	trunc := func(s string, n int) string {
		// Count runes not bytes so multi-byte icons don't corrupt alignment.
		runes := []rune(s)
		if len(runes) <= n {
			return s
		}
		return string(runes[:n-1]) + "…"
	}

	if header != "" {
		ui.SafePrintln(header)
	}

	col := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %s",
		wStatus, "St",
		wID, "Video ID",
		wType, "Source",
		wArtist, "Artist",
		wTitle, "Title",
		wQuery, "Query",
		"Cached At",
	)
	ui.SafePrintln(col)
	ui.SafePrintln(strings.Repeat("─", len(col)+2))

	for _, e := range entries {
		status := GetTrackStatus(e.VideoID).Icon()
		artist := e.Artist
		if artist == "" {
			artist = e.Channel
		}
		query := e.Query
		if query == "" {
			query = "—"
		}
		srcType := e.SourceType
		if srcType == "" {
			srcType = "—"
		}
		cachedAt := e.CachedAt
		if len(cachedAt) > 16 {
			cachedAt = cachedAt[:16] // drop timezone for width
		}

		ui.SafePrintf("%-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %s\n",
			wStatus, status,
			wID, trunc(e.VideoID, wID),
			wType, trunc(srcType, wType),
			wArtist, trunc(artist, wArtist),
			wTitle, trunc(e.Title, wTitle),
			wQuery, trunc(query, wQuery),
			cachedAt,
		)
	}
}

// PrintManifest lists all cached tracks with status icons and a summary line.
func PrintManifest() {
	entries, err := ListManifest()
	if err != nil {
		ui.SafePrintf("Error reading manifest: %v\n", err)
		return
	}
	if len(entries) == 0 {
		ui.SafePrintln("No cached tracks found.")
		return
	}

	printTable(entries, "")

	ui.SafePrintf("\n%d track(s)", len(entries))
	if config.CacheMaxBytes > 0 {
		ui.SafePrintf(" · limit %.0f MB", float64(config.CacheMaxBytes)/(1024*1024))
	}
	ui.SafePrintln("\n")
	ui.SafePrintf("  Status: %s cached  %s downloading  %s partial  %s missing  %s stale lock\n",
		StatusCached.Icon(), StatusDownloading.Icon(),
		StatusPartial.Icon(), StatusMissing.Icon(), StatusStaleLock.Icon())
}

// PrintSearch runs a case-insensitive search and prints matching tracks.
func PrintSearch(terms []string) {
	entries, err := SearchManifest(terms)
	if err != nil {
		ui.SafePrintf("Error searching cache: %v\n", err)
		return
	}
	if len(entries) == 0 {
		ui.SafePrintf("No results for: %s\n", strings.Join(terms, " "))
		return
	}

	header := fmt.Sprintf("Results for %q:", strings.Join(terms, " "))
	printTable(entries, header)
	ui.SafePrintf("\n%d result(s)\n", len(entries))
}

// ExportManifestJSON writes the full manifest as JSON to path (or stdout for "-").
func ExportManifestJSON(path string) {
	entries, err := ListManifest()
	if err != nil {
		ui.SafePrintf("Error reading manifest: %v\n", err)
		return
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		ui.SafePrintf("Error encoding JSON: %v\n", err)
		return
	}
	data = append(data, '\n')

	if path == "-" || path == "" {
		os.Stdout.Write(data)
		return
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		ui.SafePrintf("Error writing %s: %v\n", path, err)
		return
	}
	ui.SafePrintf("Manifest exported → %s (%d tracks)\n", path, len(entries))
}

