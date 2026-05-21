// Package parser turns a []string of arguments into a types.ParsedCommand.
//
// The public surface is two functions:
//
//	Parse(args []string, tty bool) ParsedCommand   — pure, fully testable
//	ParseOS() ParsedCommand                         — thin shim for production use
//
// Neither function calls os.Exit or writes any output.
// Internal process hooks (--internal-*) are NOT handled here; main.go checks
// for them directly against os.Args before calling ParseOS(), so they never
// reach this package.
package parser

import (
	"os"
	"strings"

	"play/internal/cli/types"
)

// ParseOS is the production entry point. It reads os.Args[1:] and detects
// whether stdin is a TTY, then delegates to Parse.
func ParseOS() types.ParsedCommand {
	return Parse(os.Args[1:], isTTY())
}

// Parse is the pure, testable core. It accepts the argument slice and the TTY
// state as explicit inputs so callers (including tests) can control both
// without touching global state.
//
// tty should be true when stdin is an interactive terminal; it controls
// whether a no-argument invocation opens the TUI or prints usage.
func Parse(args []string, tty bool) types.ParsedCommand {
	// ── No arguments ─────────────────────────────────────────────────────────
	if len(args) == 0 {
		if tty {
			return types.ParsedCommand{Cmd: types.CmdTUI}
		}
		return types.ParsedCommand{Cmd: types.CmdHelp}
	}

	// ── Flag stripping ────────────────────────────────────────────────────────
	// Flags may appear anywhere; strip them first so routing never sees them.
	positional, flags := StripFlags(args)

	if len(positional) == 0 {
		return types.ParsedCommand{Cmd: types.CmdHelp}
	}

	// ── Shorthand routing ─────────────────────────────────────────────────────
	// Check for +/,  prefix syntax before doing full subcommand routing.
	if cmd, ok := ParseShorthand(positional, flags); ok {
		return cmd
	}

	// ── Subcommand routing ────────────────────────────────────────────────────
	sub, rest := positional[0], positional[1:]

	switch sub {
	case "help", "--help", "-h":
		return types.ParsedCommand{Cmd: types.CmdHelp}
	case "tui":
		return types.ParsedCommand{Cmd: types.CmdTUI}
	case "stop":
		return types.ParsedCommand{Cmd: types.CmdStop}
	case "recent":
		return types.ParsedCommand{Cmd: types.CmdRecent}
	case "queue":
		return ParseQueue(rest, flags.Loop)
	case "fav":
		return ParseFav(rest, flags.Loop)
	case "cache":
		return ParseCache(rest, flags)
	case "playlist", "pl":
		return ParsePlaylist(rest)
	default:
		query := strings.Join(append([]string{sub}, rest...), " ")
		return types.ParsedCommand{
			Cmd:   types.CmdPlay,
			Query: query,
			Loop:  flags.Loop,
		}
	}
}

// isTTY reports whether stdin is an interactive terminal.
// It lives here — not in Parse — so Parse itself has no OS dependency.
func isTTY() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

