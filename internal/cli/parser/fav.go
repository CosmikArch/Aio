package parser

import (
	"strings"

	"play/internal/cli/types"
)

// ParseFav resolves the arguments that follow "play fav".
//
// Supported forms:
//
//	play fav add <query…>
//	play fav list
func ParseFav(args []string, loop bool) types.ParsedCommand {
	if len(args) == 0 {
		return types.ParsedCommand{Cmd: types.CmdHelp}
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return types.ParsedCommand{Cmd: types.CmdHelp}
		}
		return types.ParsedCommand{
			Cmd:   types.CmdFavAdd,
			Query: strings.Join(args[1:], " "),
			Loop:  loop,
		}
	case "list":
		return types.ParsedCommand{Cmd: types.CmdFavList}
	default:
		return types.ParsedCommand{Cmd: types.CmdHelp}
	}
}
