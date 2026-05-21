package favorites

import (
	"os"
	"play/internal/config"
	"play/internal/ui"
	"strings"
)

func Add(query string) {
	f, err := os.OpenFile(config.FavoritesFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(query + "\n")
}

func List() {
	data, err := os.ReadFile(config.FavoritesFile)
	if err != nil {
		ui.SafePrintln("No favorites found.")
		return
	}

	ui.SafePrintln("⭐ Favorites:")
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			ui.SafePrintln("  ", line)
		}
	}
}
