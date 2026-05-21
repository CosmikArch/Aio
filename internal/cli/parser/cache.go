package parser

import (
	"strings"

	"play/internal/cli/types"
)

// ParseCache resolves arguments following "play cache".
//
//	play cache stats
//	play cache list
//	play cache export [path.json]
//	play cache search <term…>
//	play cache tag [--dry-run]
//
// flags is passed in from StripFlags so --dry-run is never re-scanned here.
func ParseCache(args []string, flags Flags) types.ParsedCommand {
	if len(args) == 0 {
		return types.ParsedCommand{Cmd: types.CmdHelp}
	}
	switch args[0] {
	case "stats":
		return types.ParsedCommand{Cmd: types.CmdCacheStats}
	case "list":
		return types.ParsedCommand{Cmd: types.CmdCacheList}
	case "export":
		path := "-"
		if len(args) >= 2 {
			path = args[1]
		}
		return types.ParsedCommand{Cmd: types.CmdCacheExport, Query: path}
	case "search":
		if len(args) < 2 {
			return types.ParsedCommand{Cmd: types.CmdCacheList} // no terms → list all
		}
		return types.ParsedCommand{
			Cmd:   types.CmdCacheSearch,
			Query: strings.Join(args[1:], " "),
		}
	case "tag":
		return types.ParsedCommand{Cmd: types.CmdCacheTag, DryRun: flags.DryRun}
	}
	return types.ParsedCommand{Cmd: types.CmdHelp}
}

