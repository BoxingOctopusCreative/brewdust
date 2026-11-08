package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/phoenix-tui/phoenix/tea"

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
