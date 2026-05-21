// Package tui provides an optional interactive terminal UI for play.
// It is launched either via "play tui" or automatically when play is invoked
// with no arguments inside an interactive terminal (TTY auto-detection).
package tui

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"play/internal/config"
	"play/internal/player"
	"play/internal/queue"
)

// ── Tab identifiers ───────────────────────────────────────────────────────────

type tabIndex int

const (
	tabFavorites tabIndex = iota
	tabHistory
	tabQueue
	tabSearch
	tabPlayback
	numTabs = 5
)

var tabLabels = [numTabs]string{"⭐ Favorites", "🕰  History", "📋 Queue", " 🔍 Search", "🎛  Playback"}

// ── Data model ────────────────────────────────────────────────────────────────

// item carries both what is displayed and the raw query sent to the player.
type item struct {
	display string
	query   string
}

// searchState tracks the search tab's input and results.
type searchState struct {
	input   string
	results []item
	cursor  int
	loading bool
	err     string
}

// playbackState mirrors what mpv is doing (best-effort polling).
type playbackState struct {
	playing        bool
	paused         bool
	looping        bool
	muted          bool
	nowPlaying     string

	elapsed        time.Duration
	elapsedAtPause time.Duration
	startedAt      time.Time
}

type model struct {
	activeTab tabIndex
	lists     [numTabs][]item
	cursor    [numTabs]int
	width     int
	height    int
	message   string

	search   searchState
	playback playbackState
}

// ── Key matching helper ───────────────────────────────────────────────────────

// is reports whether the incoming key matches any key in the binding slice.
func is(key string, binding []string) bool {
	for _, b := range binding {
		if key == b {
			return true
		}
	}
	return false
}

// keyLabel returns a human-readable label for a binding, e.g. "space|p".
func keyLabel(binding []string) string {
	return strings.Join(binding, "|")
}

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	clrPurple    = lipgloss.Color("#7C3AED")
	clrDimGray   = lipgloss.Color("#6B7280")
	clrLightGray = lipgloss.Color("#D1D5DB")
	clrGreen     = lipgloss.Color("#10B981")
	clrWhite     = lipgloss.Color("#FFFFFF")
	clrRed       = lipgloss.Color("#EF4444")
	clrYellow    = lipgloss.Color("#F59E0B")
	clrCyan      = lipgloss.Color("#06B6D4")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(clrPurple)

	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(clrWhite).
			Background(clrPurple).
			Padding(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(clrDimGray).
				Padding(0, 1)

	dividerStyle = lipgloss.NewStyle().
			Foreground(clrDimGray)

	cursorStyle = lipgloss.NewStyle().
			Foreground(clrPurple).
			Bold(true)

	normalStyle = lipgloss.NewStyle().
			Foreground(clrLightGray)

	dimStyle = lipgloss.NewStyle().
			Foreground(clrDimGray)

	helpStyle = lipgloss.NewStyle().
			Foreground(clrDimGray)

	messageStyle = lipgloss.NewStyle().
			Foreground(clrGreen).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(clrRed).
			Bold(true)

	outerBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(clrDimGray).
			Padding(0, 1)

	playingStyle = lipgloss.NewStyle().
			Foreground(clrGreen).
			Bold(true)

	mutedStyle = lipgloss.NewStyle().
			Foreground(clrRed).
			Bold(true)

	loopingStyle = lipgloss.NewStyle().
			Foreground(clrCyan).
			Bold(true)

	labelStyle = lipgloss.NewStyle().
			Foreground(clrDimGray)

	valueStyle = lipgloss.NewStyle().
			Foreground(clrLightGray).
			Bold(true)

	highlightStyle = lipgloss.NewStyle().
			Foreground(clrYellow).
			Bold(true)

	inputActiveStyle = lipgloss.NewStyle().
				Foreground(clrWhite).
				Background(lipgloss.Color("#3B1FA3")).
				Padding(0, 1)
)

// ── Ticks ─────────────────────────────────────────────────────────────────────

type elapsedTickMsg time.Time
type aliveTickMsg time.Time

func elapsedTickCmd() tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg {
		return elapsedTickMsg(t)
	})
}

func aliveTickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return aliveTickMsg(t)
	})
}

type tickMsg struct{}
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(_ time.Time) tea.Msg {
		return tickMsg{}
	})
}

// ── Search result message ─────────────────────────────────────────────────────

type searchResultMsg struct {
	results []item
	err     string
}

// ── File I/O helpers ──────────────────────────────────────────────────────────
func loadFavorites() []item {
	data, err := os.ReadFile(config.FavoritesFile)
	if err != nil {
		return nil
	}
	var items []item
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			items = append(items, item{display: line, query: line})
		}
	}
	return items
}

func loadHistory() []item {
	data, err := os.ReadFile(config.HistoryFile)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var items []item
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if line == "" {
			continue
		}
		q := line
		if idx := strings.Index(line, " | "); idx != -1 {
			q = line[idx+3:]
		}
		items = append(items, item{display: line, query: q})
	}
	return items
}

func loadQueue() []item {
	data, err := os.ReadFile(config.QueueFile)
	if err != nil {
		return nil
	}
	var items []item
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			items = append(items, item{display: line, query: line})
		}
	}
	return items
}

func writeLines(path string, items []item) {
	var sb strings.Builder
	for _, it := range items {
		sb.WriteString(it.query)
		sb.WriteByte('\n')
	}
	os.WriteFile(path, []byte(sb.String()), 0644)
}

// ── mpv helpers ───────────────────────────────────────────────────────────────

func mpvPid() int {
	data, err := os.ReadFile(config.MpvPidFile)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

func isMpvRunning() bool { return mpvPid() > 0 }

func sendMpvIPC(jsonCmd string) bool {
	conn, err := net.DialTimeout("unix", config.MpvSocketFile, time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	_, err = fmt.Fprintf(conn, "%s\n", jsonCmd)
	return err == nil
}

func seekMpv(deltaSec int) bool {
	return sendMpvIPC(fmt.Sprintf(`{"command":["seek",%d,"relative"]}`, deltaSec))
}

func pauseMpv() bool { return sendMpvIPC(`{"command":["cycle","pause"]}`) }
func muteMpv() bool  { return sendMpvIPC(`{"command":["cycle","mute"]}`) }

// ── YouTube search ────────────────────────────────────────────────────────────

func doSearch(query string) tea.Cmd {
	return func() tea.Msg {
		if query == "" {
			return searchResultMsg{}
		}
		ctx, cancel := context.WithTimeout(context.Background(),
			time.Duration(config.YtDlpTimeout)*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "yt-dlp",
			"--quiet", "--no-warnings",
			"--socket-timeout", config.YtDlpSocketTimeout,
			"--no-playlist",
			"--print", "%(id)s\t%(title)s\t%(duration_string)s",
			"--match-filter", fmt.Sprintf("!is_live & duration < %d", config.MaxVideoDurationH*3600),
			fmt.Sprintf("ytsearch8:%s", query),
		)
		out, err := cmd.Output()
		if err != nil {
			return searchResultMsg{err: "Search failed: " + err.Error()}
		}
		var results []item
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) < 2 {
				continue
			}
			videoID := strings.TrimSpace(parts[0])
			title := strings.TrimSpace(parts[1])
			dur := ""
			if len(parts) == 3 {
				dur = strings.TrimSpace(parts[2])
			}
			display := title
			if dur != "" {
				display = fmt.Sprintf("%s  [%s]", title, dur)
			}
			url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)
			results = append(results, item{display: display, query: url})
		}
		return searchResultMsg{results: results}
	}
}

// ── Bubble Tea model ──────────────────────────────────────────────────────────

func InitialModel() model {
	m := model{}
	m.lists[tabFavorites] = loadFavorites()
	m.lists[tabHistory] = loadHistory()
	m.lists[tabQueue] = loadQueue()
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		elapsedTickCmd(),
		aliveTickCmd(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tickMsg:
		if m.playback.playing {
			m.playback.elapsed = time.Since(m.playback.startedAt)
		}
		wasPlaying := m.playback.playing
		m.playback.playing = isMpvRunning()
		if wasPlaying && !m.playback.playing {
			m.playback.elapsed = 0
		}
		return m, tickCmd()

	case searchResultMsg:
		m.search.loading = false
		m.search.cursor = 0
		if msg.err != "" {
			m.search.err = msg.err
			m.search.results = nil
		} else {
			m.search.err = ""
			m.search.results = msg.results
			if len(msg.results) == 0 {
				m.search.err = "No results found."
			}
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		if m.activeTab == tabSearch {
			return m.updateSearch(msg)
		}
		if m.activeTab == tabPlayback {
			return m.updatePlayback(msg)
		}
		return m.updateGeneral(msg)
	}
	return m, nil
}

// ── Tab-switching helper used by all three update functions ───────────────────

func (m model) applyTabSwitch(key string) (model, bool) {
	k := &config.Keys
	switch {
	case is(key, k.TabFav):
		m.activeTab = tabFavorites
	case is(key, k.TabHistory):
		m.activeTab = tabHistory
	case is(key, k.TabQueue):
		m.activeTab = tabQueue
	case is(key, k.TabSearch):
		m.activeTab = tabSearch
	case is(key, k.TabPlayback):
		m.activeTab = tabPlayback
	case is(key, k.NavRight):
		m.activeTab = (m.activeTab + 1) % numTabs
	case is(key, k.NavLeft):
		m.activeTab = (m.activeTab + numTabs - 1) % numTabs
	default:
		return m, false
	}
	m.message = ""
	return m, true
}

// ── Search update ─────────────────────────────────────────────────────────────

func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := &config.Keys
	key := msg.String()

	// ctrl+c is always a hard quit.
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	// Tab / direct-number navigation.
	if m, ok := m.applyTabSwitch(key); ok {
		return m, nil
	}

	switch {
	case is(key, k.Quit):
		return m, tea.Quit

	case is(key, k.SearchClear):
		if m.search.input != "" {
			m.search.input = ""
			m.search.results = nil
			m.search.err = ""
		} else {
			m.activeTab = tabFavorites
			m.message = ""
		}

	case key == "backspace" || key == "ctrl+h":
		if len(m.search.input) > 0 {
			r := []rune(m.search.input)
			m.search.input = string(r[:len(r)-1])
		}

	case is(key, k.Play):
		if len(m.search.results) > 0 {
			sel := m.search.results[m.search.cursor]
			player.PlayNew(sel.query, false)
			m.message = "▶  " + sel.display
			m.playback.playing = true
			m.playback.looping = false
			m.playback.nowPlaying = sel.display
			m.playback.startedAt = time.Now()
			m.playback.elapsed = 0
		} else if m.search.input != "" {
			m.search.loading = true
			m.search.err = ""
			m.search.results = nil
			return m, doSearch(m.search.input)
		}

	case is(key, k.NavUp):
		if m.search.cursor > 0 {
			m.search.cursor--
		}

	case is(key, k.NavDown):
		if m.search.cursor < len(m.search.results)-1 {
			m.search.cursor++
		}

	case is(key, k.AddQueue):
		if len(m.search.results) > 0 {
			sel := m.search.results[m.search.cursor]
			queue.Add(sel.query)
			m.lists[tabQueue] = loadQueue()
			player.EnsureDaemon()
			m.message = "➕ Added to queue: " + truncate(sel.display, 40)
		}

	default:
		// Printable single characters go into the search input.
		if utf8.RuneCountInString(key) == 1 {
			m.search.input += key
		}
	}
	
	// FIX: Added previously missing return statement here.
	return m, nil
}

// ── Playback update ───────────────────────────────────────────────────────────

func (m model) updatePlayback(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := &config.Keys
	key := msg.String()

	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m, ok := m.applyTabSwitch(key); ok {
		return m, nil
	}

	switch {
	case is(key, k.Quit):
		return m, tea.Quit

	case is(key, k.Pause):
		if isMpvRunning() {
			pauseMpv()
			m.message = "Toggled pause."
		} else {
			m.message = "Nothing is playing."
		}

	case is(key, k.SeekBack):
		if isMpvRunning() {
			seekMpv(-5)
			m.message = "⏪ –5s"
		} else {
			m.message = "Nothing is playing."
		}

	case is(key, k.SeekFwd):
		if isMpvRunning() {
			seekMpv(5)
			m.message = "⏩ +5s"
		} else {
			m.message = "Nothing is playing."
		}

	case is(key, k.SeekBackLong):
		if isMpvRunning() {
			seekMpv(-30)
			m.message = "⏪ –30s"
		}

	case is(key, k.SeekFwdLong):
		if isMpvRunning() {
			seekMpv(30)
			m.message = "⏩ +30s"
		}

	case is(key, k.Mute):
		if isMpvRunning() {
			m.playback.muted = !m.playback.muted
			muteMpv()
			if m.playback.muted {
				m.message = "🔇 Muted."
			} else {
				m.message = "🔊 Unmuted."
			}
		} else {
			m.message = "Nothing is playing."
		}

	case is(key, k.Loop):
		m.playback.looping = !m.playback.looping
		if m.playback.looping {
			m.message = "🔁 Loop enabled (takes effect on next track)."
		} else {
			m.message = "Loop disabled."
		}

	case is(key, k.Stop):
		player.StopAll()
		m.playback.playing = false
		m.playback.elapsed = 0
		m.message = "⏹  Stopped."

	case is(key, k.Skip):
		pid := mpvPid()
		if pid > 0 {
			if proc, err := os.FindProcess(pid); err == nil {
				proc.Signal(os.Interrupt)
			}
			m.message = "⏭  Skipping…"
		} else {
			m.message = "Nothing to skip."
		}
	}
	return m, nil
}

// ── General update (Favorites / History / Queue) ──────────────────────────────

func (m model) updateGeneral(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := &config.Keys
	key := msg.String()

	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m, ok := m.applyTabSwitch(key); ok {
		return m, nil
	}

	switch {
	case is(key, k.Quit):
		return m, tea.Quit

	case is(key, k.NavUp):
		if m.cursor[m.activeTab] > 0 {
			m.cursor[m.activeTab]--
		}

	case is(key, k.NavDown):
		if m.cursor[m.activeTab] < len(m.lists[m.activeTab])-1 {
			m.cursor[m.activeTab]++
		}

	case is(key, k.Play):
		items := m.lists[m.activeTab]
		if len(items) == 0 {
			break
		}
		sel := items[m.cursor[m.activeTab]]
		player.PlayNew(sel.query, m.playback.looping)
		m.message = "▶  " + sel.query
		m.playback.playing = true
		m.playback.nowPlaying = sel.query
		m.playback.startedAt = time.Now()
		m.playback.elapsed = 0

	case is(key, k.AddQueue):
		items := m.lists[m.activeTab]
		if len(items) == 0 {
			m.message = "Nothing to add."
			break
		}
		sel := items[m.cursor[m.activeTab]]
		queue.Add(sel.query)
		m.lists[tabQueue] = loadQueue()
		player.EnsureDaemon()
		m.message = "➕ Added to queue: " + truncate(sel.query, 40)

	case is(key, k.Delete):
		if m.activeTab == tabHistory {
			m.message = "History is read-only."
			break
		}
		items := m.lists[m.activeTab]
		if len(items) == 0 {
			break
		}
		i := m.cursor[m.activeTab]
		m.lists[m.activeTab] = append(items[:i:i], items[i+1:]...)
		if i > 0 && i >= len(m.lists[m.activeTab]) {
			m.cursor[m.activeTab]--
		}
		switch m.activeTab {
		case tabFavorites:
			writeLines(config.FavoritesFile, m.lists[tabFavorites])
		case tabQueue:
			writeLines(config.QueueFile, m.lists[tabQueue])
		}
		m.message = "Removed."

	case is(key, k.Refresh):
		m.lists[tabFavorites] = loadFavorites()
		m.lists[tabHistory] = loadHistory()
		m.lists[tabQueue] = loadQueue()
		for t := range m.cursor {
			if m.cursor[t] >= len(m.lists[t]) && m.cursor[t] > 0 {
				m.cursor[t] = len(m.lists[t]) - 1
			}
		}
		m.message = "Refreshed."

	case is(key, k.Stop):
		player.StopAll()
		m.playback.playing = false
		m.playback.elapsed = 0
		m.message = "⏹  Stopped."

	case is(key, k.Pause):
		if isMpvRunning() {
			pauseMpv()
			m.message = "Toggled pause."
		} else {
			m.message = "Nothing is playing."
		}
	}
	return m, nil
}

// ── View helpers ──────────────────────────────────────────────────────────────

func truncate(s string, maxLen int) string {
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen-1]) + "…"
}

func (m model) View() string {
	innerW := m.width - 4
	if innerW < 40 {
		innerW = 40
	}

	nowPlayingStr := ""
	if m.playback.nowPlaying != "" {
		icon := "▶"
		if !m.playback.playing {
			icon = "⏸"
		}
		np := truncate(m.playback.nowPlaying, innerW/2)
		nowPlayingStr = "  " + dimStyle.Render(icon+" "+np)
	}
	titleLine := titleStyle.Render("🎵 play") + nowPlayingStr

	var tabBar strings.Builder
	for i := 0; i < numTabs; i++ {
		label := fmt.Sprintf("%d %s", i+1, tabLabels[i])
		if tabIndex(i) == m.activeTab {
			tabBar.WriteString(activeTabStyle.Render(label))
		} else {
			tabBar.WriteString(inactiveTabStyle.Render(label))
		}
		if i < numTabs-1 {
			tabBar.WriteString("  ")
		}
	}
	divider := dividerStyle.Render(strings.Repeat("─", innerW))

	var content string
	switch m.activeTab {
	case tabSearch:
		content = m.viewSearch(innerW)
	case tabPlayback:
		content = m.viewPlayback(innerW)
	default:
		content = m.viewList(innerW)
	}

	body := fmt.Sprintf("%s\n\n%s\n%s\n\n%s\n\n%s",
		titleLine,
		tabBar.String(),
		divider,
		content,
		m.viewHelp(),
	)

	if m.width > 0 {
		return outerBorder.Width(innerW).Render(body)
	}
	return outerBorder.Render(body)
}

func (m model) viewList(innerW int) string {
	items := m.lists[m.activeTab]
	cursor := m.cursor[m.activeTab]

	var listBuf strings.Builder
	if len(items) == 0 {
		listBuf.WriteString(dimStyle.Render("  (empty)"))
	} else {
		visibleLines := m.height - 11
		if visibleLines < 5 {
			visibleLines = 5
		}
		if visibleLines > len(items) {
			visibleLines = len(items)
		}
		start := 0
		if cursor >= visibleLines {
			start = cursor - visibleLines + 1
		}
		end := start + visibleLines

		if start > 0 {
			listBuf.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more above\n", start)))
		}
		for i := start; i < end && i < len(items); i++ {
			line := truncate(items[i].display, innerW-4)
			if i == cursor {
				listBuf.WriteString(cursorStyle.Render("▶ " + line))
			} else {
				listBuf.WriteString(normalStyle.Render("  " + line))
			}
			if i < end-1 && i < len(items)-1 {
				listBuf.WriteString("\n")
			}
		}
		remaining := len(items) - end
		if remaining > 0 {
			listBuf.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  ↓ %d more below", remaining)))
		}
	}
	return listBuf.String()
}

func (m model) viewSearch(innerW int) string {
	k := &config.Keys
	var b strings.Builder

	inputW := innerW - 12
	if inputW < 20 {
		inputW = 20
	}
	inputText := m.search.input
	if len([]rune(inputText)) > inputW {
		runes := []rune(inputText)
		inputText = string(runes[len(runes)-inputW:])
	}
	box := inputActiveStyle.Width(inputW).Render(inputText + "█")
	b.WriteString(labelStyle.Render("Search: ") + box + "\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf(
		"  type · %s search/play · %s/%s nav · %s add-to-queue · %s clear",
		keyLabel(k.Play),
		keyLabel(k.NavUp), keyLabel(k.NavDown),
		keyLabel(k.AddQueue),
		keyLabel(k.SearchClear),
	)) + "\n\n")

	if m.search.loading {
		b.WriteString(dimStyle.Render("  🔍 Searching YouTube…"))
		return b.String()
	}
	if m.search.err != "" {
		b.WriteString(errorStyle.Render("  " + m.search.err))
		return b.String()
	}
	if len(m.search.results) == 0 {
		if m.search.input == "" {
			b.WriteString(dimStyle.Render("  Enter a query and press Enter to search."))
		} else {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  Press %s to search.", keyLabel(k.Play))))
		}
		return b.String()
	}

	visibleLines := m.height - 15
	if visibleLines < 3 {
		visibleLines = 3
	}
	cursor := m.search.cursor
	start := 0
	if cursor >= visibleLines {
		start = cursor - visibleLines + 1
	}
	end := start + visibleLines

	for i := start; i < end && i < len(m.search.results); i++ {
		line := truncate(m.search.results[i].display, innerW-4)
		if i == cursor {
			b.WriteString(cursorStyle.Render("▶ " + line))
		} else {
			b.WriteString(normalStyle.Render("  " + line))
		}
		if i < end-1 && i < len(m.search.results)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m model) viewPlayback(innerW int) string {
	k := &config.Keys
	var b strings.Builder

	b.WriteString(labelStyle.Render("  Now Playing") + "\n")
	if m.playback.nowPlaying != "" {
		b.WriteString("  " + highlightStyle.Render(truncate(m.playback.nowPlaying, innerW-4)) + "\n")
	} else {
		b.WriteString("  " + dimStyle.Render("(nothing)") + "\n")
	}

	b.WriteString("\n")
	status := dimStyle.Render("⏹  Stopped")
	if m.playback.playing {
		status = playingStyle.Render("▶  Playing")
	}
	loopStatus := dimStyle.Render("loop off")
	if m.playback.looping {
		loopStatus = loopingStyle.Render("🔁 loop on")
	}
	muteStatus := dimStyle.Render("🔊 unmuted")
	if m.playback.muted {
		muteStatus = mutedStyle.Render("🔇 muted")
	}
	b.WriteString(fmt.Sprintf("  %s   %s   %s\n", status, loopStatus, muteStatus))

	if m.playback.playing && m.playback.elapsed > 0 {
		el := int(m.playback.elapsed.Seconds())
		b.WriteString(fmt.Sprintf("\n  "+labelStyle.Render("Elapsed: ")+valueStyle.Render("%d:%02d"), el/60, el%60))
		b.WriteString("\n")
	}

	sep := dividerStyle.Render(strings.Repeat("─", innerW-2))
	b.WriteString("\n  " + sep + "\n\n")

	// Controls table: key labels come from the live config, not hardcoded strings.
	controls := []struct {
		keys []string
		desc string
	}{
		{k.Pause, "Pause / Resume"},
		{k.SeekBack, "Seek backward 5s"}, {k.SeekFwd, "Seek forward 5s"},
		{k.SeekBackLong, "Seek backward 30s"},
		{k.SeekFwdLong, "Seek forward 30s"},
		{k.Mute, "Toggle mute"},
		{k.Loop, "Toggle loop (next track)"},
		{k.Skip, "Skip to next track"},
		{k.Stop, "Stop playback"},
	}
	for _, c := range controls {
		label := fmt.Sprintf("%-14s", keyLabel(c.keys))
		b.WriteString("  " + highlightStyle.Render(label) + "  " + normalStyle.Render(c.desc) + "\n")
	}

	b.WriteString("\n  " + dimStyle.Render("Seek/pause use mpv IPC socket. Loop applies to next track."))
	return b.String()
}

func (m model) viewHelp() string {
	k := &config.Keys
	if m.message != "" {
		return messageStyle.Render(m.message)
	}
	type kv struct{ k, v string }
	var pairs []kv
	switch m.activeTab {
	case tabSearch:
		pairs = []kv{
			{"type", "query"}, {keyLabel(k.Play), "search/play"},
			{keyLabel(k.AddQueue), "add-to-queue"}, {keyLabel(k.SearchClear), "clear"},
			{keyLabel(k.NavRight), "switch tab"}, {"ctrl+c", "quit"},
		}
	case tabPlayback:
		pairs = []kv{
			{keyLabel(k.Pause), "pause"},
			{keyLabel(k.SeekBack) + "/" + keyLabel(k.SeekFwd), "seek±5s"},
			{keyLabel(k.SeekBackLong) + "/" + keyLabel(k.SeekFwdLong), "seek±30s"},
			{keyLabel(k.Mute), "mute"},
			{keyLabel(k.Loop), "loop"},
			{keyLabel(k.Skip), "skip"},
			{keyLabel(k.Stop), "stop"},
			{keyLabel(k.Quit), "quit"},
		}
	default:
		pairs = []kv{
			{keyLabel(k.NavUp) + "/" + keyLabel(k.NavDown), "nav"}, {keyLabel(k.Play), "play"},
			{keyLabel(k.AddQueue), "add-to-queue"},
			{keyLabel(k.Delete), "delete"},
			{keyLabel(k.Refresh), "refresh"},
			{keyLabel(k.Pause), "pause"}, {keyLabel(k.Stop), "stop"},
			{keyLabel(k.Quit), "quit"},
		}
	}
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = p.k + " " + p.v
	}
	return helpStyle.Render(strings.Join(parts, "  ·  "))
}

// Run starts the Bubble Tea program in full-screen (alt-screen) mode.
func Run() error {
	p := tea.NewProgram(InitialModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

