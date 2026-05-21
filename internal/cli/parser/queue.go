package parser

import (
	"strings"

	"play/internal/cli/types"
)

// ParseQueue resolves the arguments that follow "play queue".
//
// Supported forms:
//
//	play queue add <query…>
func ParseQueue(args []string, loop bool) types.ParsedCommand {
	if len(args) >= 2 && args[0] == "add" {
		return types.ParsedCommand{
			Cmd:   types.CmdQueueAdd,
			Query: strings.Join(args[1:], " "),
			Loop:  loop,
		}
	}
	return types.ParsedCommand{Cmd: types.CmdHelp}
}
