//go:build windows

package queue

import "os"

func lockFile(f *os.File) {
	// Windows fallback: no-op
}

func unlockFile(f *os.File) {
	// Windows fallback: no-op
}
