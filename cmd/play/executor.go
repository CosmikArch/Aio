package main

import (
	"fmt"
	"os"
	"strings"

	"play/internal/cache"
	"play/internal/cli/types"
	"play/internal/favorites"
	"play/internal/history"
	"play/internal/player"
	"play/internal/playlist"
	"play/internal/queue"
	"play/internal/resolver"
	"play/internal/tui"
	"play/internal/ui"
)

func printUsage() {
	fmt.Printf(`CLI background music player.

Usage:
  play [query...]                     Play a track
  play tui                            Open interactive TUI
  play stop                           Stop playback
  play recent                         Show recent tracks
  play queue add <query…>             Add to queue
  play fav add <query…>               Add to favorites
  play fav list                       List favorites
  play playlist <name>                Play a playlist immediately
  play playlist queue <name>          Enqueue a playlist without stopping
  play playlist list                  List saved playlists
  play playlist show <name>           Print playlist contents
  play playlist add <name> <term…>    Append a line to a playlist
  play cache stats                    Show cache size and DB stats
  play cache list                     List all cached tracks with status
  play cache search <term…>           Search cached tracks
  play cache export [file.json]       Export manifest as JSON (stdout if omitted)
  play cache tag [--dry-run]          Write DB metadata as MP4 tags into cached .m4a files
  play help                           Show help

Flags:
  --loop                              Loop current track
`)
}

func execute(cmd types.ParsedCommand) {
	switch cmd.Cmd {

	case types.CmdHelp:
		printUsage()

	case types.CmdTUI:
		if err := tui.Run(); err != nil {
			ui.SafePrintf("TUI error: %v\n", err)
			os.Exit(1)
		}

	case types.CmdStop:
		player.StopAll()
		ui.SafePrintln("⏹ Stopped playback.")

	case types.CmdRecent:
		history.ShowRecent(10)

	case types.CmdQueueAdd:
		queue.Add(cmd.Query)
		ui.SafePrintf("➕ Added to queue: %s\n", cmd.Query)
		player.EnsureDaemon()

	case types.CmdFavAdd:
		favorites.Add(cmd.Query)
		ui.SafePrintf("⭐ Saved to favorites: %s\n", cmd.Query)

	case types.CmdFavList:
		favorites.List()

	case types.CmdCacheStats:
		cache.PrintStats()

	case types.CmdCacheList:
		cache.PrintManifest()

	case types.CmdCacheExport:
		cache.ExportManifestJSON(cmd.Query)

	case types.CmdCacheSearch:
		terms := strings.Fields(cmd.Query)
		cache.PrintSearch(terms)

	case types.CmdCacheTag:
		cache.TagCachedFiles(cmd.DryRun)

	case types.CmdPlaylistPlay:
		executePlaylistPlay(cmd.PlaylistName, cmd.Loop)

	case types.CmdPlaylistQueue:
		executePlaylistQueue(cmd.PlaylistName)

	case types.CmdPlaylistList:
		executePlaylistList()

	case types.CmdPlaylistShow:
		if err := playlist.Show(cmd.PlaylistName); err != nil {
			ui.SafePrintf("Error: %v\n", err)
			os.Exit(1)
		}

	case types.CmdPlaylistAdd:
		if err := playlist.AppendLine(cmd.PlaylistName, cmd.Query); err != nil {
			ui.SafePrintf("Error: %v\n", err)
			os.Exit(1)
		}
		ui.SafePrintf("➕  Added %q to playlist %q\n", cmd.Query, cmd.PlaylistName)

	case types.CmdPlay:
		executePlay(cmd.Query, cmd.Loop)

	default:
		printUsage()
		os.Exit(1)
	}
}

// executePlay is the thin entry point for interactive play commands.
//
// All resolution — cache lookup, metadata repair, stream refresh, persistence
// — is handled by resolver.Resolve. This function's only jobs are:
//  1. Ask the resolver what to play.
//  2. Report failure to the user.
//  3. Print the track title.
//  4. Hand the lookup key to the player so the daemon can find it.
func executePlay(query string, loop bool) {
	target := resolver.Resolve(query)
	if target.VideoID == "" {
		ui.SafePrintln("Error: No results found.")
		os.Exit(1)
	}

	ui.SafePrintf("▶  %s\n", target.Title)

	// Queue the LookupKey (the query the resolver used to find this track),
	// not the video ID, so the daemon's queue.Pop() returns a key that the
	// index can look up directly — avoiding a redundant yt-dlp search.
	player.PlayNew(target.LookupKey, loop)
}

// executePlaylistPlay loads and plays a playlist immediately.
// Stops current playback, clears the queue, enqueues all resolved tracks,
// then starts the daemon — mirroring PlayNew but for a whole playlist.
func executePlaylistPlay(name string, loop bool) {
	path := playlist.Path(name)
	terms, err := playlist.Load(path)
	if err != nil {
		ui.SafePrintf("Error: %v\n", err)
		os.Exit(1)
	}
	if len(terms) == 0 {
		ui.SafePrintf("Playlist %q is empty.\n", name)
		return
	}

	hits, misses := playlist.Resolve(terms)
	reportMisses(misses)

	if len(hits) == 0 {
		ui.SafePrintln("Error: no tracks in the playlist could be resolved.")
		os.Exit(1)
	}

	player.StopAll()
	queue.Clear()
	n := playlist.Enqueue(hits)
	player.StartDaemon(loop)

	ui.SafePrintf("▶  Playing playlist %q — %d track(s)", name, n)
	if len(misses) > 0 {
		ui.SafePrintf(", %d unresolved", len(misses))
	}
	ui.SafePrintln("")
}

// executePlaylistQueue enqueues a playlist without interrupting current playback.
func executePlaylistQueue(name string) {
	path := playlist.Path(name)
	terms, err := playlist.Load(path)
	if err != nil {
		ui.SafePrintf("Error: %v\n", err)
		os.Exit(1)
	}
	if len(terms) == 0 {
		ui.SafePrintf("Playlist %q is empty.\n", name)
		return
	}

	hits, misses := playlist.Resolve(terms)
	reportMisses(misses)

	n := playlist.Enqueue(hits)
	player.EnsureDaemon()

	ui.SafePrintf("➕  Queued %d track(s) from playlist %q", n, name)
	if len(misses) > 0 {
		ui.SafePrintf(", %d unresolved", len(misses))
	}
	ui.SafePrintln("")
}

// executePlaylistList prints all saved playlists.
func executePlaylistList() {
	names, err := playlist.List()
	if err != nil {
		ui.SafePrintf("Error: %v\n", err)
		os.Exit(1)
	}
	if len(names) == 0 {
		ui.SafePrintf("No playlists found in %s\n", playlist.Dir())
		ui.SafePrintln("Create one with: play playlist add <name> <search term>")
		return
	}
	ui.SafePrintf("📋  Playlists in %s\n\n", playlist.Dir())
	for _, n := range names {
		ui.SafePrintf("  %s\n", n)
	}
}

// reportMisses prints a warning for each unresolved playlist term.
func reportMisses(misses []string) {
	for _, m := range misses {
		ui.SafePrintf("⚠️   No cache match for %q — skipping\n", m)
	}
}

