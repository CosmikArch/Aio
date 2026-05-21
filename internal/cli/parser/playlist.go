package parser

import (
	"strings"

	"play/internal/cli/types"
)

// ParsePlaylist handles the "playlist" (alias "pl") subcommand family.
//
//	play playlist <name>              → CmdPlaylistPlay
//	play playlist queue <name>        → CmdPlaylistQueue
//	play playlist list                → CmdPlaylistList
//	play playlist show <name>         → CmdPlaylistShow
//	play playlist add <name> <term…>  → CmdPlaylistAdd  (Query = term)
func ParsePlaylist(args []string) types.ParsedCommand {
	if len(args) == 0 {
		return types.ParsedCommand{Cmd: types.CmdPlaylistList}
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list", "ls":
		return types.ParsedCommand{Cmd: types.CmdPlaylistList}

	case "show", "cat", "view":
		name := strings.Join(rest, " ")
		return types.ParsedCommand{Cmd: types.CmdPlaylistShow, PlaylistName: name}

	case "queue", "q":
		name := strings.Join(rest, " ")
		return types.ParsedCommand{Cmd: types.CmdPlaylistQueue, PlaylistName: name}

	case "add", "append":
		// play playlist add <name> <term…>
		// First positional arg after "add" is the playlist name;
		// the remainder is the search term to append.
		if len(rest) == 0 {
			return types.ParsedCommand{Cmd: types.CmdPlaylistList}
		}
		name := rest[0]
		term := strings.Join(rest[1:], " ")
		return types.ParsedCommand{
			Cmd:          types.CmdPlaylistAdd,
			PlaylistName: name,
			Query:        term,
		}

	default:
		// Anything else is treated as a playlist name to play immediately.
		// This allows the shortest form: `play playlist morning`
		name := strings.Join(args, " ")
		return types.ParsedCommand{Cmd: types.CmdPlaylistPlay, PlaylistName: name}
	}
}
