package queue

import (
	"os"
	"strings"
	"play/internal/config"
)

// Add safely appends a query to the queue file.
func Add(query string) {
	if query == "" {
		return
	}

	f, err := os.OpenFile(config.QueueFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	lockFile(f)
	defer unlockFile(f)

	_, _ = f.WriteString(query + "\n")
}

// Clear queue

func Clear() {
	_ = os.WriteFile(config.QueueFile, []byte(""), 0644)
}

// Pop removes and returns the first queue item.
func Pop() string {
	f, err := os.OpenFile(config.QueueFile, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return ""
	}
	defer f.Close()

	lockFile(f)
	defer unlockFile(f)

	data, err := os.ReadFile(config.QueueFile)
	if err != nil {
		return ""
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return ""
	}

	first := lines[0]
	rest := ""

	if len(lines) > 1 {
		rest = strings.Join(lines[1:], "\n") + "\n"
	}

	_ = f.Truncate(0)
	_, _ = f.Seek(0, 0)
	_, _ = f.WriteString(rest)

	return first
}

