// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"fmt"
	"strings"

	"github.com/phoenix-tui/phoenix/style"
	"github.com/unilibs/uniwidth"

	"github.com/BoxingOctopusCreative/brewdust/internal/actions"
	"github.com/BoxingOctopusCreative/brewdust/internal/analyze"
	"github.com/BoxingOctopusCreative/brewdust/internal/report"
)

// Palette, loosely after Zellij's default theme: green frame on the focused
// pane, grey frames elsewhere, orange key hints on dark pills.
var (
	colAccent = style.RGB(255, 175, 95)
	colGreen  = style.RGB(152, 195, 121)
	colYellow = style.RGB(229, 192, 123)
	colRed    = style.RGB(224, 108, 117)
	colBlue   = style.RGB(97, 175, 239)
	colDim    = style.RGB(128, 128, 128)
	colFrame  = style.RGB(92, 92, 92)
	colBarBg  = style.RGB(36, 36, 36)
	colPillBg = style.RGB(64, 64, 64)
	colInk    = style.RGB(24, 24, 24)
	colText   = style.RGB(220, 220, 220)

	stTitle  = style.New().Foreground(colAccent).Bold(true)
	stDim    = style.New().Foreground(colDim)
	stBold   = style.New().Bold(true)
	stHeader = style.New().Foreground(colDim).Bold(true)
	stMark   = style.New().Foreground(colAccent).Bold(true)
	colSelBg = style.RGB(52, 61, 70)
	stSel    = style.New().Background(colSelBg).Foreground(style.RGB(255, 255, 255)).Bold(true)
	stWarn   = style.New().Foreground(colYellow)
	stErr    = style.New().Foreground(colRed).Bold(true)
	stKey    = style.New().Foreground(colAccent)
	stDeps   = style.New().Foreground(colBlue)

	stFrame           = style.New().Foreground(colFrame)
	stFrameFocus      = style.New().Foreground(colGreen)
	stFrameTitle      = style.New().Foreground(colText)
	stFrameTitleFocus = style.New().Foreground(colGreen).Bold(true)
	stFrameDim        = style.New().Foreground(colDim)
	stThumb           = style.New().Foreground(colGreen).Bold(true)
	stTrack           = style.New().Foreground(style.RGB(70, 70, 70))
	stBackdrop        = style.New().Foreground(style.RGB(80, 80, 80))

	stBarBg     = style.New().Background(colBarBg)
	stBarText   = style.New().Background(colBarBg).Foreground(colDim)
	stLogo      = style.New().Background(colAccent).Foreground(colInk).Bold(true)
	stTab       = style.New().Background(colPillBg).Foreground(colText)
	stTabActive = style.New().Background(colGreen).Foreground(colInk).Bold(true)
	stPillKey   = style.New().Background(colAccent).Foreground(colInk).Bold(true)
	stPillLabel = style.New().Background(colPillBg).Foreground(colText)

	stGaugeAccent = style.New().Foreground(colAccent)
	stGaugeGreen  = style.New().Foreground(colGreen)
	stGaugeYellow = style.New().Foreground(colYellow)
	stGaugeRed    = style.New().Foreground(colRed)
	stGaugeBlue   = style.New().Foreground(colBlue)
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func idleColor(days int) style.Style {
	switch {
	case days < 30:
		return stGaugeGreen
	case days < 180:
		return stGaugeYellow
	default:
		return stGaugeRed
	}
}

// Layout constants: one row each for the tab bar and status bar.
const chromeRows = 2

// sideWidth is the width of the right-hand column, or 0 when the terminal is
// too narrow for it.
func (m Model) sideWidth() int {
	if m.width < 100 {
		return 0
	}
	return min(max(m.width*32/100, 38), 56)
}

// listHeight is the number of package rows that fit in the packages pane.
func (m Model) listHeight() int {
	// chrome, pane borders, column header
	return max(m.height-chromeRows-2-1, 1)
}

// View implements the Phoenix model.
func (m Model) View() string {
	w, h := max(m.width, 40), max(m.height, 10)
	bodyH := h - chromeRows

	var body []string
	if m.mode == modeDetail && m.current() != nil {
		p := m.current()
		body = pane(w, bodyH, paneSpec{
			title:   "Details · " + p.Name,
			right:   "zoomed",
			focused: true,
			body:    m.detailLines(p, w-4, true),
			footer:  paint(stDim, "esc to return"),
		})
	} else {
		body = m.mainPanes(w, bodyH)
	}

	lines := append([]string{m.tabBar(w)}, body...)
	lines = append(lines, m.statusBar(w))

	if title, content, ok := m.floating(); ok {
		lines = m.float(dimAll(lines), title, content, w, h)
	}
	return m.frame(strings.Join(lines, "\n"))
}

// frame turns content into a full-screen redraw. Phoenix's alt-screen
// renderer writes View() verbatim with the terminal in raw mode, so the view
// must home the cursor, use CR+LF, and erase whatever the last frame left.
func (m Model) frame(content string) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if m.height > 0 && len(lines) > m.height {
		lines = lines[:m.height]
	}
	return "\x1b[H" + strings.Join(lines, "\x1b[K\r\n") + "\x1b[K\x1b[J"
}

// tabBar is the top line: logo, one tab per view, and totals on the right.
func (m Model) tabBar(w int) string {
	left := paint(stLogo, " brewdust ") + paint(stBarBg, " ")
	var tabs []string
	if m.preset < 0 {
		tabs = append(tabs, paint(stTabActive, " custom "))
	}
	for i, p := range presets {
		st := stTab
		if i == m.preset {
			st = stTabActive
		}
		tabs = append(tabs, paint(st, " "+p.name+" "))
	}
	left += pills(tabs, w-visibleWidth(left), stBarBg)

	right := ""
	if m.snap != nil {
		var total int64
		for _, p := range m.snap.Packages {
			total += p.SizeBytes
		}
		right = fmt.Sprintf("%d packages · %s ", len(m.snap.Packages), report.Bytes(total))
	}
	if m.mode == modeLoading {
		right = spinnerFrames[m.spinner%len(spinnerFrames)] + " scanning  " + right
	}
	return bar(left, paint(stBarText, right), w, stBarBg)
}

// statusKeys is the mode pill and the key hints shown for the current mode.
func (m Model) statusKeys() (string, style.Style, [][2]string) {
	modeName, modeSt := "NORMAL", stTabActive
	var keys [][2]string
	switch {
	case m.mode == modeSearch:
		modeName, modeSt = "SEARCH", style.New().Background(colYellow).Foreground(colInk).Bold(true)
		keys = [][2]string{{"type", "filter"}, {"enter", "keep"}, {"esc", "clear"}}
	case m.mode == modeDialog && m.dlg != nil:
		modeName, modeSt = "CONFIRM", stLogo
		keys = [][2]string{{"y", "run"}, {"esc", "cancel"}}
		if m.dlg.kind == dialogRemove && hasCask(m.dlg.targets) {
			keys = append(keys, [2]string{"z", "zap"})
		}
		if m.dlg.hasCategories() {
			keys = append(keys, [2]string{fmt.Sprintf("1-%d", len(m.dlg.catList)), "toggle"})
		}
	case m.mode == modeHelp:
		modeName, modeSt = "HELP", style.New().Background(colBlue).Foreground(colInk).Bold(true)
		keys = [][2]string{{"esc", "back"}}
	case m.mode == modeRunning && m.run != nil:
		modeName, modeSt = "RUNNING", stLogo
		if m.run.finished {
			modeName, modeSt = "DONE", stTabActive
		}
		keys = m.runKeys()
	case m.mode == modeDetail:
		modeName = "ZOOM"
		keys = [][2]string{{"esc", "back"}, {"d", "remove"}}
	case m.mode == modeLoading:
		modeName, modeSt = "SCAN", style.New().Background(colBlue).Foreground(colInk).Bold(true)
		keys = [][2]string{{"q", "quit"}}
	default:
		keys = [][2]string{
			{"↑↓", "move"}, {"space", "mark"}, {"d", "remove"}, {"c", "clean"}, {"/", "search"},
			{"v", "view"}, {"t", "type"}, {"s", "sort"}, {"enter", "zoom"}, {"?", "help"}, {"q", "quit"},
		}
	}
	return modeName, modeSt, keys
}

// statusBar is the bottom line: a mode pill and key hints, like Zellij's.
// The hints are clickable (see statusHit).
func (m Model) statusBar(w int) string {
	modeName, modeSt, keys := m.statusKeys()
	items := []string{paint(modeSt, " "+modeName+" ")}
	for _, k := range keys {
		items = append(items, pill(k[0], k[1]))
	}
	return bar(pills(items, w, stBarBg), "", w, stBarBg)
}

// statusHit returns the key of the status-bar hint at column x, or "".
func (m Model) statusHit(x int) string {
	modeName, _, keys := m.statusKeys()
	pos := uniwidth.StringWidth(" "+modeName+" ") + 1
	for _, k := range keys {
		w := visibleWidth(pill(k[0], k[1]))
		if x >= pos && x < pos+w {
			return k[0]
		}
		pos += w + 1
	}
	return ""
}

// mainPanes lays out the package list and, if there's room, the side column.
func (m Model) mainPanes(w, h int) []string {
	side := m.sideWidth()
	left := m.listPane(w-side, h)
	if side == 0 {
		return left
	}
	diskH := 0
	if h >= 24 {
		diskH = diskPaneH
	}
	right := m.detailPane(side, h-diskH)
	if diskH > 0 {
		right = append(right, m.diskPane(side, diskH)...)
	}
	return sideBySide(left, right)
}

type columns struct {
	name, needed, notes int
	bar                 bool
}

// neededW is the NEEDED BY column's width when shown.
const neededW = 16

// columns divides the pane's inner width. Space goes, in order, to a
// minimum name width, NEEDED BY, the size bar, the rest of the name, and
// finally NOTES; whatever doesn't fit is dropped.
func (m Model) columns(inner int) columns {
	want := 12
	for _, p := range m.rows {
		want = max(want, uniwidth.StringWidth(p.Name))
	}
	want = min(want, 28)
	// mark(2) kind(7) size(9) last(10) idle(6), with 2-space gaps
	rem := inner - (2 + 2 + 7 + 2 + 9 + 2 + 10 + 2 + 6)
	c := columns{name: min(want, 14)}
	rem -= c.name
	if rem >= neededW+2 {
		c.needed, rem = neededW, rem-neededW-2
	}
	if rem >= 7 {
		c.bar, rem = true, rem-7
	}
	grow := min(want-c.name, max(rem, 0))
	c.name, rem = c.name+grow, rem-grow
	if rem >= 10 {
		c.notes = rem - 2
	}
	if rem < 0 {
		c.name = max(c.name+rem, 8)
	}
	return c
}

func (m Model) listPane(w, h int) []string {
	inner := w - 4
	spec := paneSpec{title: "Packages", focused: m.mode == modeList || m.mode == modeSearch}

	if m.snap == nil {
		msg := []string{""}
		if m.err != nil {
			msg = append(msg, paint(stErr, "Error: ")+m.err.Error(), "", paint(stDim, "r to retry · q to quit"))
		} else {
			msg = append(msg,
				paint(stKey, spinnerFrames[m.spinner%len(spinnerFrames)])+" Scanning Homebrew packages…",
				"",
				paint(stDim, "Sizing kegs and reading access times; this takes a few seconds."))
		}
		spec.body = msg
		return pane(w, h, spec)
	}

	// Title carries the active filters, Zellij-style.
	var bits []string
	if m.filter.Kind != "all" {
		bits = append(bits, m.filter.Kind)
	}
	bits = append(bits, "by "+m.sortBy)
	if m.filter.Direct {
		bits = append(bits, "direct use")
	}
	if m.mode == modeSearch {
		bits = append(bits, "/"+m.filter.Query+"▏")
	} else if m.filter.Query != "" {
		bits = append(bits, "/"+m.filter.Query)
	}
	spec.title = "Packages · " + strings.Join(bits, " · ")
	if len(m.rows) > 0 {
		spec.right = fmt.Sprintf("%d/%d", m.cursor+1, len(m.rows))
	}

	c := m.columns(inner)
	hdr := "  " + fit("NAME", c.name) + "  " + fit("KIND", 7) + "  " + fitRight("SIZE", 9)
	if c.bar {
		hdr += " " + strings.Repeat(" ", 6)
	}
	hdr += "  " + fit("LAST USED", 10) + "  " + fitRight("IDLE", 6)
	if c.needed > 0 {
		hdr += "  " + fit("NEEDED BY", c.needed)
	}
	if c.notes > 0 {
		hdr += "  " + fit("NOTES", c.notes)
	}
	body := []string{paint(stHeader, hdr)}

	var maxSize int64
	for _, p := range m.rows {
		maxSize = max(maxSize, p.SizeBytes)
	}
	rows := m.listHeight()
	if len(m.rows) == 0 {
		body = append(body, "", paint(stDim, "No packages match this view."), paint(stDim, "Press v to change view or esc to clear the search."))
	}
	end := min(m.offset+rows, len(m.rows))
	for i := m.offset; i < end; i++ {
		body = append(body, m.row(m.rows[i], i == m.cursor, c, inner, maxSize))
	}
	spec.body = body

	// Scroll thumb on the right border, over the row area (below the header).
	if at, length, ok := m.thumb(); ok {
		spec.thumbAt, spec.thumbLen = 1+at, length // +1: below the column header
	}
	spec.footer = m.listFooter()
	return pane(w, h, spec)
}

func (m Model) listFooter() string {
	switch {
	case m.err != nil:
		return paint(stErr, "error: "+m.err.Error())
	case m.status != "":
		return paint(stWarn, m.status)
	case len(m.marked) > 0:
		return paint(stMark, fmt.Sprintf("● %d marked · %s", len(m.marked), report.Bytes(m.markedBytes()))) +
			paint(stDim, " · d remove · esc clear")
	default:
		var shown int64
		for _, p := range m.rows {
			shown += p.SizeBytes
		}
		return paint(stDim, fmt.Sprintf("%d shown · %s", len(m.rows), report.Bytes(shown)))
	}
}

func (m Model) row(p *analyze.Package, selected bool, c columns, inner int, maxSize int64) string {
	mark := "  "
	if m.marked[key(p)] {
		mark = "● "
	}
	last := "—"
	if p.LastUsed != nil {
		last = p.LastUsed.Local().Format("2006-01-02")
	}
	idleDays := m.filter.Idle(p)
	notes := ""
	if c.notes > 0 {
		notes = fit(report.Notes(p, m.filter.Direct), c.notes)
	}
	name, kind, size := fit(p.Name, c.name), fit(p.Kind, 7), fitRight(report.Bytes(p.SizeBytes), 9)
	lastS, idle := fit(last, 10), fitRight(report.Idle(idleDays), 6)
	needed := neededLabel(p, c.needed)

	if selected {
		line := mark + name + "  " + kind + "  " + size
		if c.bar {
			line += " " + strings.Repeat(" ", 6)
		}
		line += "  " + lastS + "  " + idle
		if c.needed > 0 {
			line += "  " + needed
		}
		if c.notes > 0 {
			line += "  " + notes
		}
		line = paint(stSel, fit(line, inner))
		if c.bar {
			// Keep the size bar visible on the highlighted row.
			at := 2 + c.name + 2 + 7 + 2 + 9 + 1
			line = cut(line, 0, at) + gaugeOn(logFrac(p.SizeBytes, maxSize), 6, stGaugeAccent.Background(colSelBg), stTrack.Background(colSelBg)) + cut(line, at+6, inner)
		}
		return line
	}
	line := paint(stMark, mark) + name + "  " + paint(stDim, kind) + "  " + size
	if c.bar {
		line += " " + gauge(logFrac(p.SizeBytes, maxSize), 6, stGaugeAccent)
	}
	line += "  " + lastS + "  " + paint(idleColor(idleDays), idle)
	if c.needed > 0 {
		st := stDim
		if len(p.Dependents) > 0 {
			st = stDeps
		}
		line += "  " + paint(st, needed)
	}
	if c.notes > 0 {
		line += "  " + paint(stDim, notes)
	}
	return line
}

func (m Model) detailPane(w, h int) []string {
	spec := paneSpec{title: "Details"}
	if p := m.current(); p != nil {
		spec.title = "Details · " + p.Name
		spec.body = m.detailLines(p, w-4, false)
		spec.footer = paint(stDim, "enter to zoom")
	} else if m.snap != nil {
		spec.body = []string{paint(stDim, "Nothing selected.")}
	}
	return pane(w, h, spec)
}

// detailLines describes one package; full adds paths and complete lists.
func (m Model) detailLines(p *analyze.Package, w int, full bool) []string {
	const lw = 12
	vw := max(w-lw, 10)
	var lines []string
	add := func(k, v string) { lines = append(lines, paint(stDim, fit(k, lw))+v) }
	addWrapped := func(k string, items []string, limit int) {
		text := strings.Join(items, ", ")
		if !full && len(items) > limit {
			text = strings.Join(items[:limit], ", ") + fmt.Sprintf(" +%d more", len(items)-limit)
		}
		for i, l := range wrap(text, vw) {
			if i == 0 {
				add(k, l)
			} else {
				add("", l)
			}
		}
	}

	head := paint(stBold, p.Name) + paint(stDim, "  "+p.Kind+" "+p.Version)
	lines = append(lines, head)
	if p.Desc != "" {
		for _, l := range wrap(p.Desc, w) {
			lines = append(lines, paint(stDim, l))
		}
	}
	lines = append(lines, "")

	var maxSize int64
	for _, q := range m.snap.Packages {
		maxSize = max(maxSize, q.SizeBytes)
	}
	add("Size", fitRight(report.Bytes(p.SizeBytes), 9)+" "+gauge(logFrac(p.SizeBytes, maxSize), min(max(vw-10, 4), 40), stGaugeAccent))

	idleDays := m.filter.Idle(p)
	add("Idle", fitRight(report.Idle(idleDays), 9)+" "+gauge(float64(idleDays)/730, min(max(vw-10, 4), 40), idleColor(idleDays)))
	if p.ActiveVia != "" && !m.filter.Direct {
		add("", paint(stDim, truncate("own use "+report.Idle(p.DirectIdleDays)+", active via "+p.ActiveVia, vw)))
	}

	if p.LastUsed != nil {
		add("Last used", p.LastUsed.Local().Format("2006-01-02 15:04"))
		if full {
			for _, l := range wrap(p.LastUsedEvidence, vw) {
				add("", paint(stDim, l))
			}
		} else {
			add("", paint(stDim, truncate(p.LastUsedEvidence, vw)))
		}
	} else {
		add("Last used", paint(stDim, "not since install"))
	}
	add("Installed", p.InstalledAt.Local().Format("2006-01-02"))
	switch {
	case p.Kind == analyze.KindCask:
		add("Source", "cask")
	case p.OnRequest:
		add("Source", "installed by you")
	case p.Orphan:
		add("Source", paint(stWarn, "orphaned dependency"))
	default:
		add("Source", "dependency")
	}
	switch {
	case len(p.Dependents) > 0:
		addWrapped("Needed by", p.Dependents, 6)
		if extra := len(p.AllDependents) - len(p.Dependents); extra > 0 {
			add("", paint(stDim, fmt.Sprintf("+%d more indirectly (%d in all)", extra, len(p.AllDependents))))
		}
	case p.Kind == analyze.KindFormula:
		add("Needed by", paint(stDim, "nothing installed"))
	}
	if len(p.Dependencies) > 0 {
		addWrapped("Depends on", p.Dependencies, 6)
	}
	if p.OldVersions > 0 {
		add("Old vers.", paint(stWarn, fmt.Sprintf("%d · %s reclaimable", p.OldVersions, report.Bytes(p.ReclaimableBytes))))
		if full {
			for _, op := range p.OldPaths {
				add("", paint(stDim, truncate(op, vw)))
			}
		}
	}
	if p.HeldVersions > 0 {
		add("Old vers.", fmt.Sprintf("%d · %s, kept for now", p.HeldVersions, report.Bytes(p.HeldBytes)))
		for _, l := range wrap("brew cleanup skips these: "+p.HeldReason, vw) {
			add("", paint(stDim, l))
		}
		if full {
			for _, hp := range p.HeldPaths {
				add("", paint(stDim, truncate(hp, vw)))
			}
		}
	}
	for _, a := range p.MissingApps {
		add("Missing", paint(stWarn, truncate(a, vw)))
		add("", paint(stDim, "deleted outside brew · c to clean up"))
	}
	if p.NoFormula {
		if p.EmptyKeg() {
			add("Formula", paint(stWarn, "none · empty Cellar directory"))
		} else {
			add("Formula", paint(stWarn, truncate("not found · "+p.Origin, vw)))
		}
		hint := "brew can't upgrade it · c to clean up"
		if len(p.AllDependents) > 0 {
			hint = "brew can't upgrade it · still needed, so c leaves it"
		}
		add("", paint(stDim, truncate(hint, vw)))
	}
	if p.Unlinked {
		add("Linked", paint(stWarn, "no · f to link"))
		add("", paint(stDim, truncate("not keg-only, but its commands aren't on PATH", vw)))
	}
	if p.BadPermissions() {
		for i, l := range wrap(strings.TrimPrefix(report.Permissions(p), "permissions: "), vw) {
			if i == 0 {
				add("Perms", paint(stWarn, l))
			} else {
				add("", paint(stWarn, l))
			}
		}
		add("", paint(stDim, truncate("brew may fail to upgrade or remove it · f to fix", vw)))
	}
	if p.Retiring() {
		state, since := "deprecated", p.DeprecationDate
		if p.Disabled {
			state, since = "disabled", p.DisableDate
		}
		if since != "" {
			state += " since " + since
		}
		add("Status", paint(stWarn, state))
		if p.StatusReason != "" {
			for _, l := range wrap(p.Name+" "+p.StatusReason, vw) {
				add("", paint(stDim, l))
			}
		}
		if !p.Disabled && p.DisableDate != "" {
			add("", paint(stDim, "to be disabled "+p.DisableDate))
		}
		if p.Replacement != "" {
			add("", paint(stDim, truncate("brew suggests "+p.Replacement+" instead", vw)))
		}
	}
	if p.ScanErrors > 0 {
		add("Unreadable", fmt.Sprintf("%d entries; size may be low", p.ScanErrors))
	}
	return lines
}

// diskPane summarises where the space goes, with gauges against the total.
func (m Model) diskPane(w, h int) []string {
	spec := paneSpec{title: "Disk"}
	if m.snap == nil {
		return pane(w, h, spec)
	}
	var total, formulae, casks, old, orphans, stale, retiring int64
	broken, noFormula, needsFix := 0, 0, 0
	for _, p := range m.snap.Packages {
		total += p.SizeBytes
		old += p.ReclaimableBytes
		if p.Kind == analyze.KindFormula {
			formulae += p.SizeBytes
		} else {
			casks += p.SizeBytes
		}
		if p.Orphan {
			orphans += p.SizeBytes
		}
		if p.Broken() {
			broken++
		}
		if p.Retiring() {
			retiring += p.SizeBytes
		}
		if p.NoFormula {
			noFormula++
		}
		if p.NeedsFix() {
			needsFix++
		}
		if m.filter.Idle(p) >= 365 {
			stale += p.SizeBytes
		}
	}
	inner := w - 4
	gw := max(inner-11-10, 4)
	row := func(label string, n int64, st style.Style, extra string) string {
		val := report.Bytes(n)
		if extra != "" {
			val = extra
		}
		return paint(stDim, fit(label, 10)) + " " + gauge(float64(n)/float64(max(total, 1)), gw, st) + " " + fitRight(val, 9)
	}
	brokenText := "none"
	if broken > 0 {
		brokenText = plural(broken, "cask")
	}
	fixText := "none"
	if needsFix > 0 {
		fixText = plural(needsFix, "package")
	}
	noFormulaText := "none"
	if noFormula > 0 {
		noFormulaText = plural(noFormula, "keg")
	}
	spec.body = []string{
		row("Formulae", formulae, stGaugeBlue, ""),
		row("Casks", casks, stGaugeBlue, ""),
		row("Idle ≥ 1y", stale, stGaugeRed, ""),
		row("Old vers.", old, stGaugeYellow, ""),
		row("Orphans", orphans, stGaugeYellow, ""),
		paint(stDim, fit("Broken", 10)) + " " + fitRight(brokenText, inner-11),
		row("Deprecated", retiring, stGaugeYellow, ""),
		paint(stDim, fit("No formula", 10)) + " " + fitRight(noFormulaText, inner-11),
		paint(stDim, fit("Needs fix", 10)) + " " + fitRight(fixText, inner-11),
		row("Marked", m.markedBytes(), stGaugeAccent, ""),
	}
	spec.right = report.Bytes(total)
	var hints []string
	if old+orphans > 0 || broken > 0 || noFormula > 0 {
		hints = append(hints, "c to clean up")
	}
	if needsFix > 0 {
		hints = append(hints, "f to fix")
	}
	if len(hints) > 0 {
		spec.footer = paint(stDim, strings.Join(hints, " · "))
	}
	return pane(w, h, spec)
}

// floating returns the floating pane to draw over everything, if any.
func (m Model) floating() (string, []string, bool) {
	switch {
	case m.mode == modeDialog && m.dlg != nil:
		switch m.dlg.kind {
		case dialogRemove:
			return "Remove packages", m.dialogLines(), true
		case dialogFix:
			return "Fix", m.dialogLines(), true
		}
		return "Clean up", m.dialogLines(), true
	case m.mode == modeHelp:
		return "Help", helpLines(), true
	case m.mode == modeRunning && m.run != nil:
		return m.run.title, m.runLines(), true
	}
	return "", nil, false
}

// float centres a focused pane over the (dimmed) screen, like Zellij's
// floating panes.
func (m Model) float(base []string, title string, content []string, w, h int) []string {
	x, y, bw, bh := m.floatRect(content)
	box := pane(bw, bh, paneSpec{title: title, focused: true, body: content})
	return overlay(base, box, x, y, w)
}

func (m Model) dialogLines() []string {
	d := m.dlg
	var lines []string
	maxCmd := max(m.width-20, 20)
	listItems := func(items []string, limit int) {
		for i, it := range items {
			if i == limit {
				lines = append(lines, paint(stDim, fmt.Sprintf("    … and %d more", len(items)-limit)))
				break
			}
			lines = append(lines, "    "+it)
		}
	}

	if d.hasCategories() {
		size := map[actions.Category]int64{}
		found := map[actions.Category]bool{}
		count := map[actions.Category]int{}
		for _, s := range d.steps {
			size[s.Category] += s.Bytes
			found[s.Category] = true
			count[s.Category] = max(count[s.Category], len(s.Items))
		}
		for i, c := range d.catList {
			box := paint(stDim, "[ ]")
			extra := ""
			if d.cats[c] {
				box = paint(stMark, "[x]")
				extra = "nothing to do"
				switch {
				case found[c] && c.IsFix():
					extra = plural(count[c], "package")
				case found[c]:
					extra = "~" + report.Bytes(size[c])
				}
			}
			lines = append(lines, fmt.Sprintf("%s %s %s  %s", paint(stKey, fmt.Sprint(i+1)), box, c.Describe(), paint(stDim, extra)))
		}
		lines = append(lines, "")
	}

	// Share the remaining rows between the steps' item lists so the total
	// always stays on screen.
	avail := m.height - chromeRows - 2 - 2
	fixed := len(lines) + 2*len(d.steps) + 3
	if len(d.blocked) > 0 {
		fixed += 2 + min(len(d.blocked), 5) + 1
	}
	perStep := 1
	if len(d.steps) > 0 {
		perStep = max(1, (avail-fixed)/len(d.steps)-1)
	}
	for _, s := range d.steps {
		size := "  ~" + report.Bytes(s.Bytes)
		if s.Category.IsFix() {
			size = ""
		}
		lines = append(lines, paint(stBold, s.Title)+paint(stDim, size))
		listItems(s.Items, perStep)
		lines = append(lines, paint(stDim, "  $ "+truncate(s.Command(), maxCmd)))
	}

	if len(d.blocked) > 0 {
		lines = append(lines, "", paint(stWarn, "Skipped, still needed by other packages:"))
		var bl []string
		for _, p := range d.targets {
			if by, ok := d.blocked[p.Name]; ok {
				bl = append(bl, fmt.Sprintf("%s (needed by %s)", p.Name, truncate(strings.Join(by, ", "), maxCmd-20)))
			}
		}
		listItems(bl, 5)
	}

	lines = append(lines, "")
	if len(d.steps) == 0 {
		lines = append(lines, paint(stDim, "Nothing to do. esc to go back."))
		return lines
	}
	total := paint(stBold, "Total ~"+report.Bytes(actions.TotalBytes(d.steps)))
	if d.kind == dialogFix {
		total = paint(stDim, "Changes only Cellar/<formula> and Caskroom/<cask> directories.")
		for _, s := range d.steps {
			if len(s.Chown) > 0 {
				total = paint(stDim, "sudo needs your password: t runs that step in the terminal if asked.")
			}
		}
	}
	if d.kind == dialogRemove && hasCask(d.targets) {
		z := "zap off"
		if d.zap {
			z = "zap on: also delete app settings & caches"
		}
		total += paint(stDim, "  ·  "+z)
	}
	lines = append(lines, total, "", pill("y", "run")+" "+pill("esc", "cancel"))
	return lines
}

func hasCask(pkgs []*analyze.Package) bool {
	for _, p := range pkgs {
		if p.Kind == analyze.KindCask {
			return true
		}
	}
	return false
}

func helpLines() []string {
	rows := [][2]string{
		{"↑/↓  k/j", "move"},
		{"pgup/pgdn  ctrl+u/d", "page"},
		{"g / G", "top / bottom"},
		{"space", "mark or unmark package"},
		{"a", "mark or unmark everything shown"},
		{"enter", "zoom the details pane"},
		{"/", "search names and descriptions (esc clears)"},
		{"v", "next view tab"},
		{"t", "cycle type: all, formula, cask"},
		{"s", "cycle sort: size, idle, name, deps"},
		{"D", "toggle whether dependents count as use"},
		{"d / x / delete", "remove marked packages (or the selected one)"},
		{"c", "clean up old versions, orphans, broken casks and kegs with no formula"},
		{"f", "fix unlinked kegs and broken permissions"},
		{"r", "rescan"},
		{"esc", "clear marks, then search"},
		{"q / ctrl+c", "quit"},
	}
	mouse := [][2]string{
		{"click", "select a package (double-click to zoom)"},
		{"right- or ctrl-click", "mark or unmark (or click the ● column)"},
		{"shift-click", "mark everything from the selection to here"},
		{"drag scrollbar", "scroll; click the track to jump"},
		{"wheel", "scroll the list"},
		{"click tab / header", "switch view / sort by that column"},
		{"click NEEDED BY", "sort by most depended-on"},
		{"click Disk row", "clean up or fix that category"},
		{"click a key hint", "same as pressing it"},
	}
	var lines []string
	for _, r := range rows {
		lines = append(lines, paint(stKey, fit(r[0], 22))+r[1])
	}
	lines = append(lines, "", paint(stBold, "Mouse"))
	for _, r := range mouse {
		lines = append(lines, paint(stKey, fit(r[0], 22))+r[1])
	}
	return append(lines, "",
		paint(stDim, "Removing or cleaning shows a confirmation first, then runs brew in a"),
		paint(stDim, "progress pane, one package at a time; esc stops after the current one."))
}

// neededLabel fits "first-dependent +N" into w columns, shortening the name
// rather than losing the count.
func neededLabel(p *analyze.Package, w int) string {
	if len(p.Dependents) < 2 {
		return fit(report.NeededBy(p), w)
	}
	more := fmt.Sprintf(" +%d", len(p.Dependents)-1)
	return fit(strings.TrimRight(fit(p.Dependents[0], w-len(more)), " ")+more, w)
}

// plural formats a count of things: "1 keg", "2 kegs".
func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}
