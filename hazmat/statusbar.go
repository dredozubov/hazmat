package main

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"

	"github.com/fatih/color"
	"golang.org/x/term"
)

// statusBar renders a tmux-style bottom bar showing hazmat containment status
// and active packs. It sets a terminal scroll region to exclude the bottom line
// and redraws on SIGWINCH.
type statusBar struct {
	packNames  []string
	projectDir string

	mu     sync.Mutex
	active bool
}

func newStatusBar(packNames []string, projectDir string) *statusBar {
	return &statusBar{
		packNames:  packNames,
		projectDir: projectDir,
	}
}

// Start renders the bar and begins listening for resize events. Returns a
// cleanup function that restores the terminal. Returns a no-op cleanup if
// stderr is not a terminal.
func (s *statusBar) Start() func() {
	fd := int(os.Stderr.Fd())
	if !term.IsTerminal(fd) {
		return func() {}
	}

	s.active = true
	s.render()

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)

	// Catch SIGINT/SIGTERM so Go's default handler doesn't terminate us
	// before the deferred cleanup runs. The child process receives the
	// signal through the process group and handles it.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-winch:
				s.render()
			case <-interrupt:
				// Swallow — child handles the signal.
			case <-done:
				signal.Stop(winch)
				signal.Stop(interrupt)
				return
			}
		}
	}()

	return func() {
		close(done)
		s.restore()
	}
}

// render draws the status bar on the terminal's bottom line and sets the
// scroll region to rows 1..h-1, keeping the bar outside the scrollable area.
func (s *statusBar) render() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return
	}

	fd := int(os.Stderr.Fd())
	w, h, err := term.GetSize(fd)
	if err != nil || w < 20 || h < 3 {
		return
	}

	var buf bytes.Buffer

	// Set scroll region to exclude the bottom line.
	fmt.Fprintf(&buf, "\033[1;%dr", h-1)
	// Save cursor, move to the bar row.
	fmt.Fprintf(&buf, "\0337\033[%d;1H", h)

	s.writeBar(&buf, w)

	// Restore cursor inside the scroll region.
	buf.WriteString("\0338")

	os.Stderr.Write(buf.Bytes())
}

func (s *statusBar) writeBar(buf *bytes.Buffer, w int) {
	if color.NoColor {
		s.writeBarPlain(buf, w)
	} else {
		s.writeBarColor(buf, w)
	}
}

// writeBarColor renders the bar with 256-color styling on a dark background.
func (s *statusBar) writeBarColor(buf *bytes.Buffer, w int) {
	const (
		bg      = "\033[48;5;236m"  // dark gray
		amber   = "\033[38;5;214m"  // ☢ symbol
		white   = "\033[38;5;255m"  // HAZMAT text
		gray    = "\033[38;5;240m"  // separator
		green   = "\033[38;5;114m"  // pack names
		lgray   = "\033[38;5;245m"  // project path
		bold    = "\033[1m"
		boldOff = "\033[22m"
		reset   = "\033[0m"
	)

	buf.WriteString(bg)

	// Left: " ☢ HAZMAT"
	fmt.Fprintf(buf, " %s☢%s%s HAZMAT%s", amber, white, bold, boldOff)
	used := 9 // visible: " ☢ HAZMAT"

	if len(s.packNames) > 0 {
		packs := strings.Join(s.packNames, ", ")
		fmt.Fprintf(buf, "%s │ %s%s", gray, green, packs)
		used += 3 + utf8.RuneCountInString(packs)
	}

	// Right: project directory.
	proj := shortenDir(s.projectDir)
	projW := utf8.RuneCountInString(proj) + 2 // leading + trailing space

	pad := w - used - projW
	if pad < 1 {
		// Terminal too narrow for the right side; fill the rest.
		pad = w - used
		if pad < 0 {
			pad = 0
		}
		buf.WriteString(strings.Repeat(" ", pad))
	} else {
		buf.WriteString(strings.Repeat(" ", pad))
		fmt.Fprintf(buf, "%s %s ", lgray, proj)
	}

	buf.WriteString(reset)
}

// writeBarPlain renders the bar using reverse video only (NO_COLOR mode).
func (s *statusBar) writeBarPlain(buf *bytes.Buffer, w int) {
	buf.WriteString("\033[7m") // reverse video

	left := " ☢ HAZMAT"
	used := 9

	if len(s.packNames) > 0 {
		packs := strings.Join(s.packNames, ", ")
		left += " │ " + packs
		used += 3 + utf8.RuneCountInString(packs)
	}
	buf.WriteString(left)

	proj := shortenDir(s.projectDir)
	projW := utf8.RuneCountInString(proj) + 2

	pad := w - used - projW
	if pad < 1 {
		pad = w - used
		if pad < 0 {
			pad = 0
		}
		buf.WriteString(strings.Repeat(" ", pad))
	} else {
		buf.WriteString(strings.Repeat(" ", pad))
		buf.WriteString(" " + proj + " ")
	}

	buf.WriteString("\033[0m")
}

// restore resets the scroll region and clears the bar line.
func (s *statusBar) restore() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.active {
		return
	}
	s.active = false

	fd := int(os.Stderr.Fd())
	_, h, err := term.GetSize(fd)
	if err != nil {
		// Best-effort: reset scroll region even if we can't get the size.
		os.Stderr.Write([]byte("\033[r"))
		return
	}

	var buf bytes.Buffer
	buf.WriteString("\033[r")
	fmt.Fprintf(&buf, "\0337\033[%d;1H\033[K\0338", h)
	os.Stderr.Write(buf.Bytes())
}

// shortenDir returns a display-friendly path: replaces the home prefix with ~
// and truncates long paths with a leading ellipsis.
func shortenDir(path string) string {
	home, err := os.UserHomeDir()
	if err == nil {
		if path == home {
			return "~"
		}
		if strings.HasPrefix(path, home+"/") {
			path = "~/" + path[len(home)+1:]
		}
	}
	const maxLen = 40
	runes := []rune(path)
	if len(runes) > maxLen {
		path = "…" + string(runes[len(runes)-(maxLen-1):])
	}
	return path
}
