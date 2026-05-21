package ui

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"
)

var stdoutLock sync.Mutex

func SafePrintln(a ...any) {
	stdoutLock.Lock()
	defer stdoutLock.Unlock()
	fmt.Println(a...)
}

func SafePrintf(format string, a ...any) {
	stdoutLock.Lock()
	defer stdoutLock.Unlock()
	fmt.Printf(format, a...)
}

// clearLine erases the current terminal line.
// On Windows, ANSI escape codes are not supported in all terminals (cmd.exe,
// older PowerShell), so we fall back to a carriage-return + space padding.
func clearLine() {
	if runtime.GOOS == "windows" {
		fmt.Print("\r" + strings.Repeat(" ", 79) + "\r")
	} else {
		fmt.Print("\r\033[2K")
	}
}

type Spinner struct {
	message string
	frames  []rune

	mu       sync.Mutex
	running  bool
	stopChan chan struct{}
	wg       sync.WaitGroup // BUG FIX: ensures Stop() blocks until animate() has exited
}

func NewSpinner(message string) *Spinner {
	return &Spinner{
		message: message,
		frames:  []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"),
	}
}

func (s *Spinner) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return
	}

	s.running = true
	s.stopChan = make(chan struct{})

	s.wg.Add(1)
	go s.animate()
}

func (s *Spinner) animate() {
	defer s.wg.Done() // BUG FIX: signals Stop() that the goroutine has fully exited

	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	frameIndex := 0

	for {
		select {
		case <-s.stopChan:
			stdoutLock.Lock()
			clearLine()
			stdoutLock.Unlock()
			return

		case <-ticker.C:
			char := s.frames[frameIndex%len(s.frames)]
			stdoutLock.Lock()
			clearLine()
			fmt.Printf("%c %s", char, s.message)
			stdoutLock.Unlock()
			frameIndex++
		}
	}
}

func (s *Spinner) Stop() {
	s.mu.Lock()

	if !s.running {
		s.mu.Unlock()
		return
	}

	s.running = false
	close(s.stopChan)
	// Release the lock before Wait() so that a concurrent Start() call is not
	// blocked while we wait for the goroutine to finish printing its last frame.
	s.mu.Unlock()

	// BUG FIX: previously Stop() closed stopChan and immediately cleared the
	// terminal line while still holding s.mu. The animate() goroutine could
	// still be mid-tick and would print one more spinner frame AFTER the clear,
	// leaving a stray character on-screen. Waiting for the goroutine to confirm
	// it has exited (via the WaitGroup) guarantees the line is truly clean
	// before we do the final erasure. Note: s.mu is released before Wait() to
	// avoid blocking Start() if it is called concurrently.
	s.wg.Wait()

	stdoutLock.Lock()
	clearLine()
	stdoutLock.Unlock()
}
