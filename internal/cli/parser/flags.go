package parser

// Flags holds every recognised boolean flag that can appear anywhere in the
// argument list. Keeping them in one struct means the rest of the parser
// never has to scan args for flag strings — it just reads fields.
type Flags struct {
	Loop   bool // --loop | -loop | -l
	DryRun bool // --dry-run
}

// StripFlags walks args exactly once. It collects every recognised flag into
// the returned Flags value and returns all non-flag tokens in their original
// order as positional.
//
// Flags may appear anywhere — before, after, or between positional tokens —
// and are consumed regardless of position.
//
//	StripFlags([]string{"--loop", "play", "some", "song"})
//	→ positional=["play","some","song"], flags={Loop:true}
//
//	StripFlags([]string{"cache", "tag", "--dry-run"})
//	→ positional=["cache","tag"], flags={DryRun:true}
func StripFlags(args []string) (positional []string, flags Flags) {
	positional = make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--loop", "-loop", "-l":
			flags.Loop = true
		case "--dry-run":
			flags.DryRun = true
		default:
			positional = append(positional, a)
		}
	}
	return positional, flags
}

