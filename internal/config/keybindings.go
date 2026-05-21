package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// KeyBindings holds every configurable key for the TUI. Each field is the
// string exactly as returned by bubbletea's KeyMsg.String() — e.g. "ctrl+c",
// "space", "enter", "L", ",".
//
// Multiple keys for the same action are supported by separating them with a
// pipe character in the config file:  pause = "space|p"
//
// The zero value is not useful; always call LoadKeybindings() first.
type KeyBindings struct {
	// Navigation (all contexts)
	NavUp       []string // default: up, k
	NavDown     []string // default: down, j
	NavLeft     []string // default: left, h, shift+tab
	NavRight    []string // default: right, l, tab
	TabFav      []string // default: 1
	TabHistory  []string // default: 2
	TabQueue    []string // default: 3
	TabSearch   []string // default: 4
	TabPlayback []string // default: 5
	Quit        []string // default: q, ctrl+c

	// List actions
	Play    []string // default: enter
	AddQueue []string // default: a
	Delete  []string // default: d
	Refresh []string // default: r

	// Playback controls
	Pause    []string // default: space, p
	SeekBack []string // default: ,
	SeekFwd  []string // default: .
	SeekBackLong []string // default: [
	SeekFwdLong  []string // default: ]
	Mute     []string // default: m
	Loop     []string // default: L
	Stop     []string // default: s
	Skip     []string // default: n

	// Search
	SearchClear []string // default: esc
}

// Keys is the package-level singleton populated by LoadKeybindings.
var Keys KeyBindings

// keybindingsFile returns the path to the user keybindings file.
// It lives next to the config dir (~/.config/play/keybindings.conf).
func keybindingsFile() string {
	if override := os.Getenv("PLAY_CONFIG_DIR"); override != "" {
		return filepath.Join(override, "keybindings.conf")
	}
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "play", "keybindings.conf")
}

// defaults returns a map of action name → default key list. This is the
// single source of truth for built-in defaults.
func defaults() map[string][]string {
	return map[string][]string{
		"nav_up":         {"up", "k"},
		"nav_down":       {"down", "j"},
		"nav_left":       {"left", "h", "shift+tab"},
		"nav_right":      {"right", "l", "tab"},
		"tab_favorites":  {"1"},
		"tab_history":    {"2"},
		"tab_queue":      {"3"},
		"tab_search":     {"4"},
		"tab_playback":   {"5"},
		"quit":           {"q", "ctrl+c"},
		"play":           {"enter"},
		"add_queue":      {"a"},
		"delete":         {"d"},
		"refresh":        {"r"},
		"pause":          {"space", "p"},
		"seek_back":      {","},
		"seek_fwd":       {"."},
		"seek_back_long": {"["},
		"seek_fwd_long":  {"]"},
		"mute":           {"m"},
		"loop":           {"L"},
		"stop":           {"s"},
		"skip":           {"n"},
		"search_clear":   {"esc"},
	}
}

// parseKeys splits a pipe-separated key string into individual key names,
// stripping whitespace and ignoring empty segments.
func parseKeys(raw string) []string {
	// Strip surrounding quotes if present.
	raw = strings.TrimSpace(raw)
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		raw = raw[1 : len(raw)-1]
	}
	var out []string
	for _, part := range strings.Split(raw, "|") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// LoadKeybindings reads the keybindings config file (if it exists) and
// populates the global Keys variable. Unrecognised keys in the file are
// silently ignored. Missing entries keep their compiled-in defaults.
//
// File format (INI-style, section header optional):
//
//	[keybindings]
//	pause       = space|p
//	seek_back   = ,
//	quit        = q|ctrl+c
//
// Lines starting with '#' or ';' are comments. Inline comments after '#' are
// also stripped.
func LoadKeybindings() {
	d := defaults()

	path := keybindingsFile()
	if path != "" {
		if data, err := os.Open(path); err == nil {
			defer data.Close()
			scanner := bufio.NewScanner(data)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				// Skip comments and section headers.
				if line == "" || line[0] == '#' || line[0] == ';' || line[0] == '[' {
					continue
				}
				// Strip inline comments.
				if idx := strings.Index(line, " #"); idx != -1 {
					line = strings.TrimSpace(line[:idx])
				}
				idx := strings.IndexByte(line, '=')
				if idx < 0 {
					continue
				}
				key := strings.TrimSpace(line[:idx])
				val := strings.TrimSpace(line[idx+1:])
				if _, known := d[key]; known && val != "" {
					d[key] = parseKeys(val)
				}
			}
		}
	}

	Keys = KeyBindings{
		NavUp:        d["nav_up"],
		NavDown:      d["nav_down"],
		NavLeft:      d["nav_left"],
		NavRight:     d["nav_right"],
		TabFav:       d["tab_favorites"],
		TabHistory:   d["tab_history"],
		TabQueue:     d["tab_queue"],
		TabSearch:    d["tab_search"],
		TabPlayback:  d["tab_playback"],
		Quit:         d["quit"],
		Play:         d["play"],
		AddQueue:     d["add_queue"],
		Delete:       d["delete"],
		Refresh:      d["refresh"],
		Pause:        d["pause"],
		SeekBack:     d["seek_back"],
		SeekFwd:      d["seek_fwd"],
		SeekBackLong: d["seek_back_long"],
		SeekFwdLong:  d["seek_fwd_long"],
		Mute:         d["mute"],
		Loop:         d["loop"],
		Stop:         d["stop"],
		Skip:         d["skip"],
		SearchClear:  d["search_clear"],
	}
}

// WriteDefaultKeybindings writes a fully-commented default keybindings file
// to the config path, only if the file does not already exist.
// Called once at startup so users have a ready-to-edit reference.
func WriteDefaultKeybindings() {
	path := keybindingsFile()
	if path == "" {
		return
	}
	if _, err := os.Stat(path); err == nil {
		return // Already exists — don't overwrite user customisations.
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	const template = `# play keybindings configuration
# ─────────────────────────────────────────────────────────────────────────────
# Each line has the form:
#
#   action = key1|key2|...
#
# Key names match bubbletea's KeyMsg.String() output, e.g.:
#   enter, space, esc, backspace, tab, shift+tab
#   ctrl+c, ctrl+h
#   up, down, left, right
#   a-z  A-Z  0-9  punctuation as-is (, . [ ] ; ' etc.)
#
# Separate multiple keys for the same action with a pipe: pause = space|p
# Lines starting with # or ; are comments.
# ─────────────────────────────────────────────────────────────────────────────

[keybindings]

# ── Navigation ───────────────────────────────────────────────────────────────
nav_up         = up|k
nav_down       = down|j
nav_left       = left|h|shift+tab
nav_right      = right|l|tab

# Jump directly to a tab by number
tab_favorites  = 1
tab_history    = 2
tab_queue      = 3
tab_search     = 4
tab_playback   = 5

quit           = q|ctrl+c

# ── List actions (Favorites / History / Queue tabs) ───────────────────────────
play           = enter       # Play the selected item immediately
add_queue      = a           # Append selected item to the play queue
delete         = d           # Remove selected item (not available in History)
refresh        = r           # Reload all lists from disk

# ── Playback controls (also work from any non-search tab) ────────────────────
pause          = space|p     # Toggle pause / resume
seek_back      = ,           # Seek backward 5 seconds
seek_fwd       = .           # Seek forward 5 seconds
seek_back_long = [           # Seek backward 30 seconds
seek_fwd_long  = ]           # Seek forward 30 seconds
mute           = m           # Toggle mute
loop           = L           # Toggle loop (applies to next track start)
stop           = s           # Stop playback entirely
skip           = n           # Skip to the next queued track

# ── Search tab ───────────────────────────────────────────────────────────────
search_clear   = esc         # Clear search input / return to Favorites tab
`
	os.WriteFile(path, []byte(template), 0644)
}
