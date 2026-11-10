// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/BoxingOctopusCreative/brewdust/internal/analyze"
	"github.com/BoxingOctopusCreative/brewdust/internal/app"
)

func TestExtractMouse(t *testing.T) {
	in := []byte("j\x1b[<0;10;5Mk\x1b[<0;10;5m\x1b[<32;12;7M\x1b[<65;3;4M\x1b[A\x1b[<2")
	rest, evs, partial := extractMouse(in)
	if string(rest) != "jk\x1b[A" {
		t.Errorf("rest = %q", rest)
	}
	want := []mouseMsg{
		{X: 9, Y: 4, Button: btnLeft, Action: mousePress},
		{X: 9, Y: 4, Button: btnLeft, Action: mouseRelease},
		{X: 11, Y: 6, Button: btnLeft, Action: mouseMotion},
		{X: 2, Y: 3, Button: btnWheelDown, Action: mousePress},
	}
	if !reflect.DeepEqual(evs, want) {
		t.Errorf("events = %+v\nwant %+v", evs, want)
	}
	if string(partial) != "\x1b[<2" {
		t.Errorf("partial = %q", partial)
	}

	// The partial report completes on the next read.
	rest, evs, partial = extractMouse(append(partial, []byte(";1;1M")...))
	if len(rest) != 0 || len(partial) != 0 || len(evs) != 1 || evs[0].Button != btnRight {
		t.Errorf("continuation: rest=%q evs=%+v partial=%q", rest, evs, partial)
	}

	// Modifiers and a lone Esc key.
	_, evs, _ = extractMouse([]byte("\x1b[<16;1;1M"))
	if !evs[0].Ctrl {
		t.Error("ctrl bit not decoded")
	}
	if rest, _, _ := extractMouse([]byte("\x1b")); string(rest) != "\x1b" {
		t.Errorf("Esc should pass straight through, got %q", rest)
	}
}

func loaded(t *testing.T, n int) Model {
	t.Helper()
	now := time.Now()
	var pkgs []*analyze.Package
	for i := 0; i < n; i++ {
		pkgs = append(pkgs, &analyze.Package{Name: fmt.Sprintf("pkg%03d", i), Kind: analyze.KindFormula,
			SizeBytes: int64(1e9 - i*1e6), InstalledAt: now, OnRequest: true})
	}
	m := newModel(&env{ctx: context.Background()}, Options{})
	m.width, m.height = 140, 30
	m, _ = m.Update(loadedMsg{snap: &app.Snapshot{Prefix: "/opt/homebrew", Now: now, Packages: pkgs}})
	return m
}

func click(m Model, x, y int, mods ...string) Model {
	ev := mouseMsg{X: x, Y: y, Button: btnLeft, Action: mousePress}
	for _, mod := range mods {
		switch mod {
		case "ctrl":
			ev.Ctrl = true
		case "shift":
			ev.Shift = true
		}
	}
	m, _ = m.Update(ev)
	m, _ = m.Update(mouseMsg{X: x, Y: y, Button: btnLeft, Action: mouseRelease})
	return m
}

func TestClickSelectAndMark(t *testing.T) {
	m := loaded(t, 100)
	g := m.geom()

	m = click(m, 20, g.rowTop+4)
	if m.cursor != 4 {
		t.Fatalf("click row 4: cursor = %d", m.cursor)
	}
	m = click(m, 2, g.rowTop+6) // the ● column
	if !m.marked["formula:pkg006"] || m.cursor != 6 {
		t.Errorf("mark column click: marked=%v cursor=%d", m.marked, m.cursor)
	}
	m = click(m, 20, g.rowTop+9, "shift")
	for i := 6; i <= 9; i++ {
		if !m.marked[fmt.Sprintf("formula:pkg%03d", i)] {
			t.Errorf("shift-click should mark pkg%03d", i)
		}
	}
	m = click(m, 20, g.rowTop+9, "ctrl")
	if m.marked["formula:pkg009"] {
		t.Error("ctrl-click should toggle the mark off")
	}

	// Right click toggles a mark without moving the cursor.
	m, _ = m.Update(mouseMsg{X: 20, Y: g.rowTop + 1, Button: btnRight, Action: mousePress})
	if !m.marked["formula:pkg001"] || m.cursor != 9 {
		t.Errorf("right click: marked=%v cursor=%d", m.marked["formula:pkg001"], m.cursor)
	}

	// Double click zooms the details pane.
	m = click(m, 20, g.rowTop+2)
	m = click(m, 20, g.rowTop+2)
	if m.mode != modeDetail || m.cursor != 2 {
		t.Errorf("double click: mode=%v cursor=%d", m.mode, m.cursor)
	}
	m = click(m, 20, 10)
	if m.mode != modeList {
		t.Error("click in zoom should return to the list")
	}
}

func TestScrollbarDragAndWheel(t *testing.T) {
	m := loaded(t, 200)
	g := m.geom()
	at, length, ok := m.thumb()
	if !ok || at != 0 {
		t.Fatalf("thumb at=%d len=%d ok=%v", at, length, ok)
	}

	// Grab the thumb and drag it to the bottom of the track.
	m, _ = m.Update(mouseMsg{X: g.scrollX, Y: g.rowTop, Button: btnLeft, Action: mousePress})
	if !m.drag.active {
		t.Fatal("press on thumb should start a drag")
	}
	m, _ = m.Update(mouseMsg{X: g.scrollX + 30, Y: g.h + 5, Button: btnLeft, Action: mouseMotion})
	if want := len(m.rows) - g.rows; m.offset != want {
		t.Errorf("dragged to bottom: offset = %d, want %d", m.offset, want)
	}
	if m.cursor < m.offset {
		t.Errorf("cursor %d should follow into view (offset %d)", m.cursor, m.offset)
	}
	m, _ = m.Update(mouseMsg{X: 0, Y: 0, Button: btnLeft, Action: mouseRelease})
	if m.drag.active {
		t.Error("release should end the drag")
	}

	// Clicking the track jumps; the wheel scrolls three rows.
	m = click(m, g.scrollX, g.rowTop)
	if m.offset != 0 {
		t.Errorf("track click at top: offset = %d", m.offset)
	}
	m, _ = m.Update(mouseMsg{X: 10, Y: g.rowTop, Button: btnWheelDown, Action: mousePress})
	if m.offset != wheelStep {
		t.Errorf("wheel down: offset = %d", m.offset)
	}
}

func TestClickTabsHeaderAndStatus(t *testing.T) {
	m := loaded(t, 20)
	g := m.geom()

	// Find the "requested" tab by scanning the tab bar.
	for x := 0; x < g.w; x++ {
		if m.tabHit(x) == 1 {
			m = click(m, x, 0)
			break
		}
	}
	if m.presetName() != "requested" {
		t.Errorf("tab click: preset = %s", m.presetName())
	}

	for x := 0; x < g.listW; x++ {
		if m.headerSort(x, g) == "name" {
			m = click(m, x, g.headerY)
			break
		}
	}
	if m.sortBy != "name" {
		t.Errorf("header click: sort = %s", m.sortBy)
	}

	for x := 0; x < g.w; x++ {
		if m.statusHit(x) == "?" {
			m = click(m, x, g.h-1)
			break
		}
	}
	if m.mode != modeHelp {
		t.Errorf("status pill click: mode = %v", m.mode)
	}
	m = click(m, 5, 5)
	if m.mode != modeList {
		t.Error("click should close help")
	}
}

func TestClickDialog(t *testing.T) {
	m := loaded(t, 20)
	m = press(m, r('d'))
	content := m.dialogLines()
	x, y, _, _ := m.floatRect(content)
	last := y + 1 + len(content) - 1

	m = click(m, x+2+visibleWidth(pill("y", "run"))+3, last) // esc pill
	if m.mode != modeList || m.dlg != nil {
		t.Errorf("cancel pill: mode=%v", m.mode)
	}
	m = press(m, r('d'))
	m = click(m, 0, 1) // outside the floating pane
	if m.mode != modeList {
		t.Error("click outside should cancel")
	}
}
