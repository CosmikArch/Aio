// Package types defines the command taxonomy for the play CLI.
//
// Every possible user-visible action is represented as a Command constant.
// The parser produces a ParsedCommand value; main.go dispatches on it.
// No other packages need to import main.go or each other for this purpose.
package types

// Command identifies a single, fully-resolved CLI action.
type Command int

const (
	// CmdUnknown is the zero value — should never reach the executor.
	CmdUnknown Command = iota

	// Top-level commands.
	CmdHelp   // play help
	CmdTUI    // play tui  (or play with no args on a TTY)
	CmdStop   // play stop
	CmdRecent // play recent

	// Subcommands of "queue".
	CmdQueueAdd // play queue add <query…>

	// Subcommands of "fav".
	CmdFavAdd  // play fav add <query…>
	CmdFavList // play fav list

	// Subcommands of "cache".
	CmdCacheStats  // play cache stats
	CmdCacheList   // play cache list
	CmdCacheExport // play cache export [file.json]
	CmdCacheSearch // play cache search <terms…>
	CmdCacheTag    // play cache tag [--dry-run]

	// Subcommands of "playlist".
	CmdPlaylistPlay   // play playlist <name>           — play immediately
	CmdPlaylistQueue  // play playlist queue <name>     — enqueue without stopping
	CmdPlaylistList   // play playlist list             — list saved playlists
	CmdPlaylistShow   // play playlist show <name>      — print playlist contents
	CmdPlaylistAdd    // play playlist add <name> <term> — append a line

	// Play a query (search or direct).
	CmdPlay // play <query…>  (default fallback)

	// Internal process hooks — never shown in usage.
	CmdInternalBgDownload  // --internal-bg-download <args…>
	CmdInternalQueueDaemon // --internal-queue-daemon [--loop]
)

// ParsedCommand is the fully-decoded result of parsing os.Args.
// The executor in main.go only reads this struct; it never touches os.Args.
type ParsedCommand struct {
	// Cmd is the resolved action to perform.
	Cmd Command

	// Query is the joined, user-visible search string or track name.
	// Populated for CmdPlay, CmdQueueAdd, CmdFavAdd, CmdInternalBgDownload.
	Query string

	// Loop is true when --loop / -loop / -l was present.
	Loop bool

	// DryRun is true when --dry-run was present (used by cache tag).
	DryRun bool

	// PlaylistName is the playlist name for CmdPlaylist* commands.
	PlaylistName string

	// RawArgs carries the unmodified tail of os.Args[1:] for internal hooks
	// that forward their own arguments to sub-processes.
	RawArgs []string
}
