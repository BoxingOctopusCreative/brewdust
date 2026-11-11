// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/phoenix-tui/phoenix/tea"

	"github.com/BoxingOctopusCreative/brewdust/internal/actions"
	"github.com/BoxingOctopusCreative/brewdust/internal/analyze"
	"github.com/BoxingOctopusCreative/brewdust/internal/app"
)

func TestFit(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		want string
	}{
		{"abc", 5, "abc  "},
		{"abcdef", 4, "abc…"},
		{"a → b → c", 6, "a → b…"},
		{"", 3, "   "},
	}
	for _, c := range cases {
		if got := fit(c.in, c.w); got != c.want {
			t.Errorf("fit(%q, %d) = %q, want %q", c.in, c.w, got, c.want)
		}
	}
	if got := visibleWidth(paint(stTitle, "brewdust")); got != 8 {
		t.Errorf("visibleWidth ignores escapes: got %d", got)
	}
}

func TestFrameClipsAndUsesCRLF(t *testing.T) {
	m := Model{height: 2}
	got := m.frame("one\ntwo\nthree\n")
	want := "\x1b[Hone\x1b[K\r\ntwo\x1b[K\x1b[J"
	if got != want {
		t.Errorf("frame = %q, want %q", got, want)
	}
}

func press(m Model, keys ...tea.KeyMsg) Model {
	for _, k := range keys {
		m, _ = m.Update(k)
	}
	return m
}

func r(c rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRune, Rune: c} }

func TestMarkFilterAndRemoveDialog(t *testing.T) {
	now := time.Now()
	pkgs := []*analyze.Package{
		{Name: "openssl", Kind: analyze.KindFormula, SizeBytes: 300, InstalledAt: now, Dependents: []string{"curl"}, AllDependents: []string{"curl"}},
		{Name: "curl", Kind: analyze.KindFormula, SizeBytes: 200, InstalledAt: now, OnRequest: true},
		{Name: "calibre", Kind: analyze.KindCask, SizeBytes: 100, InstalledAt: now, MissingApps: []string{"/Applications/calibre.app"}},
	}
	m := newModel(&env{ctx: context.Background()}, Options{})
	m.width, m.height = 120, 30
	m, _ = m.Update(loadedMsg{snap: &app.Snapshot{Prefix: "/opt/homebrew", Now: now, Packages: pkgs}})

	if m.mode != modeList || len(m.rows) != 3 || m.rows[0].Name != "openssl" {
		t.Fatalf("after load: mode=%v rows=%d", m.mode, len(m.rows))
	}
	if !strings.Contains(m.View(), "curl") {
		t.Error("list view should show packages")
	}

	// Mark openssl only: removal must skip it because curl needs it.
	m = press(m, tea.KeyMsg{Type: tea.KeySpace}, r('d'))
	if m.mode != modeDialog || len(m.dlg.steps) != 0 || m.dlg.blocked["openssl"] == nil {
		t.Fatalf("dialog: mode=%v steps=%d blocked=%v", m.mode, len(m.dlg.steps), m.dlg.blocked)
	}
	if !strings.Contains(m.View(), "needed by curl") {
		t.Error("dialog should explain the skip")
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyEsc})

	// Mark curl too: now both go in one brew uninstall.
	m = press(m, tea.KeyMsg{Type: tea.KeySpace}, r('d'))
	if len(m.dlg.steps) != 1 || len(m.dlg.steps[0].Items) != 2 {
		t.Fatalf("expected one step removing both, got %+v", m.dlg.steps)
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyEsc}, tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.marked) != 0 {
		t.Error("esc should clear marks")
	}

	// Search and the broken-cask view.
	m = press(m, r('/'), r('c'), r('a'), r('l'), tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.rows) != 1 || m.rows[0].Name != "calibre" {
		t.Errorf("search: rows=%v", m.rows)
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	for m.presetName() != "broken casks" {
		m = press(m, r('v'))
	}
	if len(m.rows) != 1 || m.rows[0].Name != "calibre" {
		t.Errorf("broken view: rows=%v", m.rows)
	}

	// Cleanup dialog toggles categories.
	m = press(m, r('c'))
	if m.dlg == nil || m.dlg.kind != dialogClean || len(m.dlg.steps) != 1 {
		t.Fatalf("clean dialog steps = %+v", m.dlg)
	}
	m = press(m, r('3'))
	if len(m.dlg.steps) != 0 {
		t.Errorf("toggling broken off should leave nothing, got %d", len(m.dlg.steps))
	}
}

func TestPaneIsExactSize(t *testing.T) {
	lines := pane(30, 5, paneSpec{title: "Packages", right: "1/9", footer: paint(stDim, "3 shown"), focused: true,
		body: []string{"short", strings.Repeat("x", 60), paint(stMark, "● colored")}, thumbAt: 1, thumbLen: 2})
	if len(lines) != 5 {
		t.Fatalf("got %d lines", len(lines))
	}
	for i, l := range lines {
		if w := visibleWidth(l); w != 30 {
			t.Errorf("line %d is %d wide: %q", i, w, stripANSI(l))
		}
	}
}

func TestOverlayKeepsWidth(t *testing.T) {
	base := []string{paint(stDim, strings.Repeat("a", 20)), strings.Repeat("b", 20)}
	out := overlay(base, []string{paint(stMark, "XYZ")}, 5, 1, 20)
	if got := stripANSI(out[1]); got != "bbbbbXYZbbbbbbbbbbbb" {
		t.Errorf("overlay = %q", got)
	}
	if visibleWidth(out[0]) != 20 {
		t.Error("untouched row changed width")
	}
}

func TestNeededLabelKeepsCount(t *testing.T) {
	p := &analyze.Package{Dependents: []string{"qtconnectivity", "a", "b", "c", "d", "e", "f", "g"}}
	if got := neededLabel(p, 16); got != "qtconnectivi… +7" {
		t.Errorf("neededLabel = %q", got)
	}
	if got := neededLabel(&analyze.Package{}, 6); got != "—     " {
		t.Errorf("none = %q", got)
	}
}

func TestRunProgressEvents(t *testing.T) {
	m := loaded(t, 3)
	ch := make(chan any)
	rs := &runState{title: "Removing packages", started: time.Now(), stop: &atomic.Bool{}, events: ch}
	for _, name := range []string{"a", "b", "c"} {
		rs.tasks = append(rs.tasks, taskState{task: actions.Task{Label: name, Action: "Uninstalling", Bytes: 100}})
	}
	m.run, m.mode = rs, modeRunning

	m, _ = m.handleRunEvent(evStart{0})
	m, _ = m.handleRunEvent(evLine{"==> Uninstalling a"})
	m, _ = m.handleRunEvent(evDone{i: 0})
	m, _ = m.handleRunEvent(evStart{1})
	m, _ = m.handleRunEvent(evDone{i: 1, err: errors.New("exit status 1"),
		output: "==> Uninstalling b\nsudo: a terminal is required to read the password\n"})

	// Esc while running asks to stop rather than closing or quitting.
	m, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil || !m.run.stop.Load() || m.mode != modeRunning {
		t.Fatalf("esc should request a stop: stop=%v mode=%v", m.run.stop.Load(), m.mode)
	}
	m, _ = m.handleRunEvent(evEnd{})

	ok, failed, skipped, freed := m.run.counts()
	if ok != 1 || failed != 1 || skipped != 1 || freed != 100 {
		t.Errorf("counts = %d ok, %d failed, %d skipped, %d freed", ok, failed, skipped, freed)
	}
	b := m.run.tasks[1]
	if !b.needsTTY || !strings.Contains(b.err, "terminal is required") {
		t.Errorf("failed task: needsTTY=%v err=%q", b.needsTTY, b.err)
	}
	view := m.View()
	for _, want := range []string{"Stopped after 2 of 3", "retry failed in terminal", "need your password"} {
		if !strings.Contains(stripANSI(view), want) {
			t.Errorf("view missing %q", want)
		}
	}

	// Enter closes and rescans.
	m, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.run != nil || m.mode != modeLoading || cmd == nil || !strings.Contains(m.status, "1 failed") {
		t.Errorf("close: run=%v mode=%v status=%q", m.run, m.mode, m.status)
	}
}

func TestDeprecatedAndNoFormula(t *testing.T) {
	now := time.Now()
	pkgs := []*analyze.Package{
		{Name: "qt@5", Kind: analyze.KindFormula, SizeBytes: 300, InstalledAt: now, OnRequest: true, Deprecated: true,
			DeprecationDate: "2026-05-19", DisableDate: "2027-05-19", StatusReason: "is not supported upstream"},
		{Name: "libsoup@2", Kind: analyze.KindFormula, SizeBytes: 200, InstalledAt: now, OnRequest: true, Deprecated: true,
			Disabled: true, DisableDate: "2026-09-05", StatusReason: "is not supported upstream", Replacement: "libsoup"},
		{Name: "gone", Kind: analyze.KindFormula, SizeBytes: 100, InstalledAt: now, Version: "2.0", NoFormula: true,
			Origin: "from untapped someone/tools", Rack: "/opt/homebrew/Cellar/gone"},
		{Name: "wpscan", Kind: analyze.KindFormula, InstalledAt: now, NoFormula: true, Rack: "/opt/homebrew/Cellar/wpscan"},
		{Name: "curl", Kind: analyze.KindFormula, SizeBytes: 50, InstalledAt: now, OnRequest: true},
	}
	m := newModel(&env{ctx: context.Background()}, Options{})
	m.width, m.height = 140, 40
	m, _ = m.Update(loadedMsg{snap: &app.Snapshot{Prefix: "/opt/homebrew", Now: now, Packages: pkgs}})

	for m.presetName() != "deprecated" {
		m = press(m, r('v'))
	}
	if len(m.rows) != 2 || m.rows[0].Name != "qt@5" {
		t.Fatalf("deprecated view: rows=%v", m.rows)
	}
	qt := strings.Join(m.detailLines(m.rows[0], 60, true), "\n")
	for _, want := range []string{"deprecated since 2026-05-19", "qt@5 is not supported upstream", "to be disabled 2027-05-19"} {
		if !strings.Contains(qt, want) {
			t.Errorf("qt@5 details lack %q:\n%s", want, qt)
		}
	}
	soup := strings.Join(m.detailLines(m.rows[1], 60, true), "\n")
	for _, want := range []string{"disabled since 2026-09-05", "brew suggests libsoup instead"} {
		if !strings.Contains(soup, want) {
			t.Errorf("libsoup@2 details lack %q:\n%s", want, soup)
		}
	}

	m = press(m, r('v'))
	if m.presetName() != "no formula" || len(m.rows) != 2 {
		t.Fatalf("no formula view: %s rows=%v", m.presetName(), m.rows)
	}
	var gone, empty *analyze.Package
	for _, p := range m.rows {
		if p.Name == "gone" {
			gone = p
		} else {
			empty = p
		}
	}
	if d := strings.Join(m.detailLines(gone, 60, true), "\n"); !strings.Contains(d, "not found · from untapped someone/tools") {
		t.Errorf("gone details:\n%s", d)
	}
	if d := strings.Join(m.detailLines(empty, 60, true), "\n"); !strings.Contains(d, "none · empty Cellar directory") {
		t.Errorf("wpscan details:\n%s", d)
	}

	disk := strings.Join(m.diskPane(44, diskPaneH), "\n")
	if !strings.Contains(disk, "Deprecated") || !strings.Contains(disk, "2 kegs") {
		t.Errorf("disk pane:\n%s", disk)
	}

	// The fourth cleanup category handles both kegs.
	m = press(m, r('c'))
	if m.dlg == nil || len(m.dlg.steps) != 2 || m.dlg.steps[0].Category != actions.NoFormula {
		t.Fatalf("clean dialog steps = %+v", m.dlg)
	}
	m = press(m, r('4'))
	if len(m.dlg.steps) != 0 {
		t.Errorf("toggling no formula off should leave nothing, got %d", len(m.dlg.steps))
	}
}
