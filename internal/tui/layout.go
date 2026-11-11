// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import "github.com/unilibs/uniwidth"

// geom is where things are on screen, in 0-based cells. View and the mouse
// handler both derive positions from here so clicks land where things are drawn.
type geom struct {
	w, h    int
	listW   int // packages pane width (it starts at x=0)
	side    int // right column width, 0 if hidden
	headerY int // column header row
	rowTop  int // first package row
	rows    int // package rows visible
	scrollX int // the packages pane's right border, where the thumb is
	diskTop int // top border row of the Disk pane, -1 if hidden
}

const diskPaneH = 12

func (m Model) geom() geom {
	w, h := max(m.width, 40), max(m.height, 10)
	side := m.sideWidth()
	bodyH := h - chromeRows
	g := geom{
		w: w, h: h, side: side, listW: w - side,
		headerY: 2, rowTop: 3, rows: m.listHeight(), diskTop: -1,
	}
	g.scrollX = g.listW - 1
	if side > 0 && bodyH >= 24 {
		g.diskTop = 1 + bodyH - diskPaneH
	}
	return g
}

// thumb returns the scroll thumb's position and length within the row area.
func (m Model) thumb() (at, length int, ok bool) {
	rows, total := m.listHeight(), len(m.rows)
	if total <= rows {
		return 0, 0, false
	}
	length = max(1, rows*rows/total)
	at = (rows - length) * m.offset / max(1, total-rows)
	return at, length, true
}

// tabHit returns the preset index of the tab at column x, or -2 for none.
func (m Model) tabHit(x int) int {
	pos := uniwidth.StringWidth(" brewdust ") + 1
	if m.preset < 0 {
		pos += uniwidth.StringWidth(" custom ") + 1
	}
	for i, p := range presets {
		w := uniwidth.StringWidth(" " + p.name + " ")
		if x >= pos && x < pos+w {
			return i
		}
		pos += w + 1
	}
	return -2
}

// headerSort returns the sort key for a click on the column header at x.
func (m Model) headerSort(x int, g geom) string {
	c := m.columns(g.listW - 4)
	pos := 2 + 2 // border, padding, mark column
	span := func(w int) (int, int) {
		a := pos
		pos += w + 2
		return a, a + w
	}
	if a, b := span(c.name); x >= a && x < b {
		return "name"
	}
	span(7) // kind
	sizeW := 9
	if c.bar {
		sizeW += 7
	}
	if a, b := span(sizeW); x >= a && x < b {
		return "size"
	}
	if a, b := span(10 + 2 + 6); x >= a && x < b { // last used + idle
		return "idle"
	}
	if c.needed > 0 {
		if a, b := span(c.needed); x >= a && x < b {
			return "deps"
		}
	}
	return ""
}

// floatRect is where a floating pane with these lines is drawn.
func (m Model) floatRect(content []string) (x, y, w, h int) {
	g := m.geom()
	for _, l := range content {
		w = max(w, visibleWidth(l))
	}
	w = min(w+4, g.w-4)
	h = min(len(content)+2, g.h-chromeRows)
	return (g.w - w) / 2, 1 + (g.h-chromeRows-h)/2, w, h
}
