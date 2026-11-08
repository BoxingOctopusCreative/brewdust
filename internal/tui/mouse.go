package tui

// Mouse input. Phoenix v0.2.4 defines tea.MouseMsg but never enables mouse
// reporting or parses mouse sequences, so brewdust does both itself: the
// terminal is put into SGR mouse mode, and ttyReader sits between /dev/tty and
// Phoenix, lifting mouse reports out of the byte stream and delivering them as
// messages while passing everything else (keys) through untouched.

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// 1000: report presses/releases; 1002: also motion while a button is
	// held (for dragging); 1006: SGR encoding (no 223-column limit).
	mouseOn  = "\x1b[?1000h\x1b[?1002h\x1b[?1006h"
	mouseOff = "\x1b[?1006l\x1b[?1002l\x1b[?1000l"
)

type mouseButton int

const (
	btnNone mouseButton = iota
	btnLeft
	btnMiddle
	btnRight
	btnWheelUp
	btnWheelDown
)

type mouseAction int

const (
	mousePress mouseAction = iota
	mouseRelease
	mouseMotion
)

// mouseMsg is a mouse event in 0-based screen cells. (tea.MouseMsg would drop
// the modifier keys on its way through Phoenix, so brewdust uses its own.)
type mouseMsg struct {
	X, Y             int
	Button           mouseButton
	Action           mouseAction
	Shift, Alt, Ctrl bool
}

// parseSGRMouse decodes the body of "\x1b[<b;x;y" + final ('M' press or
// motion, 'm' release).
func parseSGRMouse(body []byte, final byte) (mouseMsg, bool) {
	parts := bytes.Split(body, []byte(";"))
	if len(parts) != 3 {
		return mouseMsg{}, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(string(p))
		if err != nil {
			return mouseMsg{}, false
		}
		n[i] = v
	}
	b := n[0]
	ev := mouseMsg{X: n[1] - 1, Y: n[2] - 1, Shift: b&4 != 0, Alt: b&8 != 0, Ctrl: b&16 != 0}
	switch {
	case b&64 != 0:
		ev.Action = mousePress
		if b&1 == 0 {
			ev.Button = btnWheelUp
		} else {
			ev.Button = btnWheelDown
		}
		return ev, true
	case b&32 != 0:
		ev.Action = mouseMotion
	case final == 'm':
		ev.Action = mouseRelease
	default:
		ev.Action = mousePress
	}
	switch b & 3 {
	case 0:
		ev.Button = btnLeft
	case 1:
		ev.Button = btnMiddle
	case 2:
		ev.Button = btnRight
	default:
		ev.Button = btnNone
	}
	return ev, true
}

// extractMouse removes complete SGR mouse reports from data, returning the
// remaining bytes, the events, and any incomplete report at the end, which
// the caller should prepend to the next read.
func extractMouse(data []byte) (rest []byte, events []mouseMsg, partial []byte) {
	prefix := []byte("\x1b[<")
	for len(data) > 0 {
		i := bytes.Index(data, prefix)
		if i < 0 {
			// A trailing "\x1b" or "\x1b[" could be the start of a report
			// split across reads, but is just as likely the Esc key; pass it
			// on rather than delaying Esc.
			rest = append(rest, data...)
			break
		}
		rest = append(rest, data[:i]...)
		data = data[i+len(prefix):]
		end := bytes.IndexAny(data, "Mm")
		if end < 0 {
			if len(data) < 16 && isMouseBody(data) {
				partial = append(append([]byte(nil), prefix...), data...)
			} else {
				rest = append(append(rest, prefix...), data...)
			}
			break
		}
		if ev, ok := parseSGRMouse(data[:end], data[end]); ok {
			events = append(events, ev)
		} else {
			rest = append(append(rest, prefix...), data[:end+1]...)
		}
		data = data[end+1:]
	}
	return rest, events, partial
}

func isMouseBody(b []byte) bool {
	for _, c := range b {
		if (c < '0' || c > '9') && c != ';' {
			return false
		}
	}
	return true
}

// ttyReader feeds Phoenix's input. It reads the terminal with short deadlines
// so that it can be paused: while brew runs with the terminal handed over,
// nothing here consumes the user's keystrokes.
type ttyReader struct {
	tty    *os.File
	send   func(mouseMsg)
	paused atomic.Bool

	mu      sync.Mutex
	out     []byte
	partial []byte
}

func newTTYReader(tty *os.File, send func(mouseMsg)) *ttyReader {
	return &ttyReader{tty: tty, send: send}
}

func (r *ttyReader) Read(p []byte) (int, error) {
	buf := make([]byte, 1024)
	for {
		if r.paused.Load() {
			// Phoenix treats EOF as "stop reading"; it starts a fresh
			// reader on Resume.
			return 0, io.EOF
		}
		r.mu.Lock()
		if len(r.out) > 0 {
			n := copy(p, r.out)
			r.out = r.out[n:]
			r.mu.Unlock()
			return n, nil
		}
		r.mu.Unlock()

		_ = r.tty.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, err := r.tty.Read(buf)
		if n > 0 {
			r.mu.Lock()
			data := append(r.partial, buf[:n]...)
			rest, events, partial := extractMouse(data)
			r.partial = partial
			r.out = append(r.out, rest...)
			r.mu.Unlock()
			for _, ev := range events {
				r.send(ev)
			}
		}
		switch {
		case err == nil, errors.Is(err, os.ErrDeadlineExceeded):
		case errors.Is(err, io.EOF):
			// A terminal doesn't really end; a zero-byte read happens while
			// its mode is being switched. Passing EOF on would stop Phoenix
			// reading input for good.
			time.Sleep(10 * time.Millisecond)
		default:
			return 0, err
		}
	}
}

// pause stops reading the terminal (and drops anything unread).
func (r *ttyReader) pause() {
	r.paused.Store(true)
	r.mu.Lock()
	r.out, r.partial = nil, nil
	r.mu.Unlock()
}

func (r *ttyReader) resume() { r.paused.Store(false) }
