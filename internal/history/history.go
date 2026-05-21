package history

import (
	"fmt"
	"os"
	"play/internal/config"
	"play/internal/ui"
	"strings"
	"time"
)

func Add(title string) {
	f, err := os.OpenFile(config.HistoryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04")
	f.WriteString(fmt.Sprintf("%s | %s\n", timestamp, title))
}

func ShowRecent(limit int) {
	data, err := os.ReadFile(config.HistoryFile)
	if err != nil {
		ui.SafePrintln("No history found.")
		return
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	ui.SafePrintln("🕰  Recently Played:")

	start := len(lines) - limit
	if start < 0 {
		start = 0
	}

	for _, line := range lines[start:] {
		if line != "" {
			ui.SafePrintln("  ", line)
		}
	}
}
