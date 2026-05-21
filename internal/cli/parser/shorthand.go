package parser

import (
	"strings"

	"play/internal/cli/types"
)

// ParseShorthand detects single-character prefix shorthands and resolves them
// into a ParsedCommand. ok is false when no shorthand is present, letting the
// caller fall through to normal subcommand routing.
//
// positional must already have flags stripped (call StripFlags first).
// The Flags value is passed in so Loop is forwarded to the resulting command.
//
// # Recognised shorthands
//
//	+  song          → CmdQueueAdd   (space between prefix and query)
//	+song            → CmdQueueAdd   (prefix glued to first query word)
//	,  song          → CmdPlay       (space between prefix and query)
//	,song            → CmdPlay       (prefix glued to first query word)
//
// The --loop flag pairs naturally with "+":
//
//	+ --loop song    (--loop is already in flags.Loop before this is called)
func ParseShorthand(positional []string, flags Flags) (cmd types.ParsedCommand, ok bool) {
	if len(positional) == 0 {
		return types.ParsedCommand{}, false
	}

	first := positional[0]
	rest := positional[1:]

	switch {
	// ── Queue-add shorthand ──────────────────────────────────────────────────

	case first == "+":
		// "+ song title" — query is everything after the standalone "+"
		return types.ParsedCommand{
			Cmd:   types.CmdQueueAdd,
			Query: strings.Join(rest, " "),
			Loop:  flags.Loop,
		}, true

	case strings.HasPrefix(first, "+"):
		// "+song" — strip the prefix, then prepend what remains to rest
		glued := first[1:] // same as TrimPrefix but avoids re-scanning
		parts := joinNonEmpty(glued, rest)
		return types.ParsedCommand{
			Cmd:   types.CmdQueueAdd,
			Query: strings.Join(parts, " "),
			Loop:  flags.Loop,
		}, true

	// ── Play shorthand ───────────────────────────────────────────────────────

	case first == ",":
		// ", song title" — query is everything after the standalone ","
		return types.ParsedCommand{
			Cmd:   types.CmdPlay,
			Query: strings.Join(rest, " "),
			Loop:  flags.Loop,
		}, true

	case strings.HasPrefix(first, ","):
		// ",song" — strip the prefix, then prepend what remains to rest
		glued := first[1:]
		parts := joinNonEmpty(glued, rest)
		return types.ParsedCommand{
			Cmd:   types.CmdPlay,
			Query: strings.Join(parts, " "),
			Loop:  flags.Loop,
		}, true
	}

	return types.ParsedCommand{}, false
}

// joinNonEmpty returns a slice starting with head (if non-empty) followed by
// tail. This handles the edge case where the user types just "+" or ","
// glued to nothing (e.g. the rare "+"), which would produce an empty glued
// string that we don't want to feed into the query as a blank word.
func joinNonEmpty(head string, tail []string) []string {
	if head == "" {
		return tail
	}
	return append([]string{head}, tail...)
}

