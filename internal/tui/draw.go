// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

// Drawing primitives for a Zellij-style layout: framed panes with titles in
// the border, pill-shaped tabs and key hints, gauges, and floating panes
// composited over a dimmed background. Everything works on strings that may
// contain SGR color escapes, measuring only what is visible.

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/phoenix-tui/phoenix/style"
	"github.com/unilibs/uniwidth"
)

const reset = "\x1b[0m"

// escapeLen returns the length of the CSI escape sequence starting at s[0], or 0.
func escapeLen(s string) int {
	if len(s) < 2 || s[0] != 0x1b || s[1] != '[' {
		return 0
	}
	for i := 2; i < len(s); i++ {
		if c := s[i]; (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return i + 1
		}
	}
	return 0
}

// stripANSI removes color escapes.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if n := escapeLen(s[i:]); n > 0 {
			i += n
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// visibleWidth measures text as displayed, ignoring color escapes.
func visibleWidth(s string) int { return uniwidth.StringWidth(stripANSI(s)) }

// cut returns the visible columns [from, to) of s. Every escape sequence is
// kept, wherever it falls, so the colors in effect at `from` carry over.
func cut(s string, from, to int) string {
	var b strings.Builder
	col := 0
	for i := 0; i < len(s); {
		if n := escapeLen(s[i:]); n > 0 {
			b.WriteString(s[i : i+n])
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w := uniwidth.RuneWidth(r)
		if col >= from && col+w <= to {
			b.WriteRune(r)
		}
		col += w
		i += size
	}
	return b.String()
}

// fitANSI pads or truncates colored text to exactly w visible columns.
func fitANSI(s string, w int) string {
	if w <= 0 {
		return ""
	}
	vw := visibleWidth(s)
	if vw <= w {
		return s + strings.Repeat(" ", w-vw)
	}
	out := cut(s, 0, w-1) + reset + "…"
	if pad := w - visibleWidth(out); pad > 0 {
		out += strings.Repeat(" ", pad)
	}
	return out
}

// fit truncates or pads plain text to exactly w display columns.
func fit(text string, w int) string {
	if w <= 0 {
		return ""
	}
	tw := uniwidth.StringWidth(text)
	if tw <= w {
		return text + strings.Repeat(" ", w-tw)
	}
	var b strings.Builder
	used := 0
	for _, r := range text {
		rw := uniwidth.RuneWidth(r)
		if used+rw > w-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	b.WriteString("…")
	used++
	return b.String() + strings.Repeat(" ", w-used)
}

func fitRight(text string, w int) string {
	if tw := uniwidth.StringWidth(text); tw < w {
		return strings.Repeat(" ", w-tw) + text
	}
	return fit(text, w)
}

func truncate(s string, w int) string {
	if w < 4 {
		w = 4
	}
	if uniwidth.StringWidth(s) <= w {
		return s
	}
	return strings.TrimRight(fit(s, w), " ")
}

// wrap breaks plain text into lines of at most w columns at spaces.
func wrap(text string, w int) []string {
	if w < 8 {
		w = 8
	}
	var lines []string
	cur := ""
	for _, word := range strings.Fields(text) {
		switch {
		case cur == "":
			cur = word
		case uniwidth.StringWidth(cur)+1+uniwidth.StringWidth(word) <= w:
			cur += " " + word
		default:
			lines = append(lines, cur)
			cur = word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	for i, l := range lines {
		lines[i] = truncate(l, w)
	}
	return lines
}

func paint(s style.Style, text string) string {
	if text == "" {
		return ""
	}
	return style.Render(s, text)
}

// paneSpec describes one framed pane.
type paneSpec struct {
	title   string // drawn into the top border
	right   string // drawn into the top border, right-aligned (e.g. "12/429")
	footer  string // drawn into the bottom border
	focused bool
	body    []string
	// Scroll thumb drawn on the right border: rows [thumbAt, thumbAt+thumbLen).
	thumbAt, thumbLen int
}

// pane draws a rounded frame of exactly w×h with its title in the top border,
// like Zellij's pane frames. Body lines are padded with one space each side.
func pane(w, h int, p paneSpec) []string {
	if w < 6 || h < 2 {
		return nil
	}
	frame := stFrame
	titleSt := stFrameTitle
	if p.focused {
		frame, titleSt = stFrameFocus, stFrameTitleFocus
	}
	edge := func(s string) string { return paint(frame, s) }

	// Top border: ╭─ title ───────── right ─╮
	title := ""
	if p.title != "" {
		title = " " + truncate(stripANSI(p.title), w-8) + " "
	}
	right := ""
	if p.right != "" && visibleWidth(title)+uniwidth.StringWidth(p.right)+8 <= w {
		right = " " + p.right + " "
	}
	fill := w - 2 - 1 - visibleWidth(title) - visibleWidth(right) - 1
	if fill < 0 {
		fill = 0
	}
	top := edge("╭─") + paint(titleSt, title) + edge(strings.Repeat("─", fill)) + paint(stFrameDim, right) + edge("─╮")

	// Bottom border: ╰─ footer ──────────╯
	footer := ""
	if p.footer != "" {
		footer = " " + fitANSI(p.footer, min(visibleWidth(p.footer), w-6)) + " "
	}
	bfill := w - 3 - visibleWidth(footer)
	if bfill < 0 {
		bfill = 0
	}
	bottom := edge("╰─") + footer + edge(strings.Repeat("─", bfill)+"╯")

	out := []string{top}
	inner := w - 4
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(p.body) {
			line = p.body[i]
		}
		rightEdge := edge("│")
		if p.thumbLen > 0 && i >= p.thumbAt && i < p.thumbAt+p.thumbLen {
			rightEdge = paint(stThumb, "┃")
		}
		out = append(out, edge("│")+" "+fitANSI(line, inner)+reset+" "+rightEdge)
	}
	return append(out, bottom)
}

// sideBySide joins two columns of equal height.
func sideBySide(left, right []string) []string {
	out := make([]string, max(len(left), len(right)))
	for i := range out {
		if i < len(left) {
			out[i] = left[i]
		}
		if i < len(right) {
			out[i] += right[i]
		}
	}
	return out
}

// overlay composites box onto base with its top-left corner at (x, y).
func overlay(base, box []string, x, y, width int) []string {
	out := append([]string(nil), base...)
	for i, line := range box {
		row := y + i
		if row < 0 || row >= len(out) {
			continue
		}
		bw := visibleWidth(line)
		left := fitANSI(cut(out[row], 0, x), x)
		rightPart := cut(out[row], x+bw, width)
		out[row] = left + reset + line + reset + rightPart
	}
	return out
}

// dimAll strips colors and greys out lines, for the background behind a
// floating pane.
func dimAll(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = paint(stBackdrop, stripANSI(l))
	}
	return out
}

// pill renders a Zellij-style key hint: a bright key segment and a label.
func pill(key, label string) string {
	return paint(stPillKey, " "+key+" ") + paint(stPillLabel, " "+label+" ")
}

// bar fills a full-width line with a background, placing left and right parts.
func bar(left, right string, width int, bg style.Style) string {
	lw, rw := visibleWidth(left), visibleWidth(right)
	if lw+rw+1 > width {
		right, rw = "", 0
	}
	if lw > width {
		left, lw = fitANSI(left, width), width
	}
	gap := width - lw - rw
	return left + paint(bg, strings.Repeat(" ", gap)) + right
}

// pills joins pills with a background-coloured space, dropping any that
// don't fit in width.
func pills(items []string, width int, bg style.Style) string {
	var b strings.Builder
	used := 0
	for _, it := range items {
		w := visibleWidth(it) + 1
		if used+w > width {
			break
		}
		b.WriteString(it + paint(bg, " "))
		used += w
	}
	return b.String()
}

var eighths = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

// gauge draws a horizontal bar w cells wide filled to frac (0..1), with
// eighth-cell precision, in the given style over a dim track.
func gauge(frac float64, w int, fill style.Style) string {
	return gaugeOn(frac, w, fill, stTrack)
}

// gaugeOn is gauge with an explicit track style, e.g. to keep a row
// highlight's background behind the bar.
func gaugeOn(frac float64, w int, fill, track style.Style) string {
	if w <= 0 {
		return ""
	}
	frac = math.Max(0, math.Min(1, frac))
	cells := frac * float64(w)
	full := int(cells)
	part := int((cells - float64(full)) * 8)
	s := strings.Repeat("█", full)
	used := full
	if full < w && part > 0 {
		s += eighths[part]
		used++
	}
	return paint(fill, s) + paint(track, strings.Repeat("─", w-used))
}

// logFrac places n on a log scale from 100 kB up to max, so a 6 GB toolchain
// doesn't flatten every other package's bar to nothing.
func logFrac(n, max int64) float64 {
	const floor = 1e5
	if n <= floor || max <= floor {
		return 0
	}
	return math.Log10(float64(n)/floor) / math.Log10(float64(max)/floor)
}
