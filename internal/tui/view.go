package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/phoenix-tui/phoenix/style"
	"github.com/unilibs/uniwidth"

	"github.com/BoxingOctopusCreative/brewdust/internal/actions"
	"github.com/BoxingOctopusCreative/brewdust/internal/analyze"
	"github.com/BoxingOctopusCreative/brewdust/internal/report"
)

var (
	colAccent = style.RGB(255, 175, 95)
	colDim    = style.RGB(135, 135, 135)
	colGreen  = style.RGB(135, 215, 135)
	colYellow = style.RGB(230, 200, 100)
	colRed    = style.RGB(240, 110, 110)

	stTitle  = style.New().Foreground(colAccent).Bold(true)
	stDim    = style.New().Foreground(colDim)
	stBold   = style.New().Bold(true)
	stHeader = style.New().Foreground(colDim).Bold(true)
	stMark   = style.New().Foreground(colAccent).Bold(true)
	stSel    = style.New().Background(style.RGB(58, 58, 88)).Foreground(style.RGB(255, 255, 255)).Bold(true)
	stWarn   = style.New().Foreground(colYellow)
	stErr    = style.New().Foreground(colRed).Bold(true)
	stKey    = style.New().Foreground(colAccent)
	stBorder = style.New().Foreground(colAccent)
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// visibleWidth measures text as displayed, ignoring color escapes.
func visibleWidth(s string) int { return uniwidth.StringWidth(ansiRE.ReplaceAllString(s, "")) }

// box draws a rounded border around lines. (Phoenix's Border counts escape
// sequences as width, which misaligns the right edge of colored content.)
func box(lines []string, maxWidth int) string {
	inner := 0
	for _, l := range lines {
		if w := visibleWidth(l); w > inner {
			inner = w
		}
	}
	if maxWidth > 4 && inner > maxWidth-4 {
		inner = maxWidth - 4
	}
	edge := func(s string) string { return paint(stBorder, s) }
	out := []string{edge("╭" + strings.Repeat("─", inner+2) + "╮")}
	for _, l := range lines {
		pad := inner - visibleWidth(l)
		if pad < 0 {
			pad = 0
		}
		out = append(out, edge("│")+" "+l+strings.Repeat(" ", pad)+" "+edge("│"))
	}
	out = append(out, edge("╰"+strings.Repeat("─", inner+2)+"╯"))
	return strings.Join(out, "\n")
}

func paint(s style.Style, text string) string {
	if text == "" {
		return ""
	}
	return style.Render(s, text)
}

// fit truncates or pads text to exactly w display columns.
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

func idleColor(days int) style.Style {
	switch {
	case days < 30:
		return style.New().Foreground(colGreen)
	case days < 180:
		return style.New().Foreground(colYellow)
	default:
		return style.New().Foreground(colRed)
	}
}

// View implements the Phoenix model.
func (m Model) View() string {
	var body string
	switch {
	case m.mode == modeLoading && m.snap == nil:
		body = m.viewLoading()
	case m.err != nil && m.snap == nil:
		body = paint(stErr, "Error: ") + m.err.Error() + "\n\n" + paint(stDim, "r to retry · q to quit")
	case m.mode == modeDetail:
		body = m.viewDetail()
	case m.mode == modeHelp:
		body = viewHelp()
	case m.mode == modeDialog:
		body = m.viewDialog()
	default:
		body = m.viewList()
	}
	return m.frame(m.header() + "\n" + body)
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

func (m Model) header() string {
	title := paint(stTitle, "brewdust")
	if m.snap == nil {
		return title
	}
	var total, reclaim int64
	for _, p := range m.snap.Packages {
		total += p.SizeBytes
		reclaim += p.ReclaimableBytes
	}
	stats := fmt.Sprintf("  %d packages · %s", len(m.snap.Packages), report.Bytes(total))
	if reclaim > 0 {
		stats += fmt.Sprintf(" · %s in old versions", report.Bytes(reclaim))
	}
	line := title + paint(stDim, stats)
	if m.mode == modeLoading {
		line += "  " + paint(stKey, spinnerFrames[m.spinner%len(spinnerFrames)]+" rescanning…")
	}
	return line
}

func (m Model) viewLoading() string {
	return fmt.Sprintf("\n  %s Scanning Homebrew packages…\n\n  %s",
		paint(stKey, spinnerFrames[m.spinner%len(spinnerFrames)]),
		paint(stDim, "Sizing kegs and reading access times; this takes a few seconds."))
}

// listHeight is the number of package rows that fit on screen.
func (m Model) listHeight() int {
	// header, filter bar, column header, status line, key hints
	h := m.height - 5
	if h < 1 {
		return 1
	}
	return h
}

type columns struct{ name, notes int }

func (m Model) columns() columns {
	name := 12
	for _, p := range m.rows {
		if w := uniwidth.StringWidth(p.Name); w > name {
			name = w
		}
	}
	if name > 28 {
		name = 28
	}
	// mark(2) name kind(7) size(9) last(10) idle(6), two spaces between each
	fixed := 2 + name + 2 + 7 + 2 + 9 + 2 + 10 + 2 + 6 + 2
	notes := m.width - fixed
	if notes < 0 {
		name += notes
		if name < 8 {
			name = 8
		}
		notes = 0
	}
	return columns{name: name, notes: notes}
}

func (m Model) viewList() string {
	var b strings.Builder

	// Filter bar.
	label := func(k, v string) string { return paint(stDim, k+" ") + v }
	bar := []string{
		label("view", m.presetName()),
		label("type", m.filter.Kind),
		label("sort", m.sortBy),
	}
	if m.filter.Direct {
		bar = append(bar, label("idle", "direct only"))
	}
	if m.mode == modeSearch {
		bar = append(bar, label("search", m.filter.Query+paint(stKey, "▏")))
	} else if m.filter.Query != "" {
		bar = append(bar, label("search", m.filter.Query))
	}
	b.WriteString(strings.Join(bar, paint(stDim, "  │  ")) + "\n")

	c := m.columns()
	hdr := "  " + fit("NAME", c.name) + "  " + fit("KIND", 7) + "  " + fitRight("SIZE", 9) + "  " +
		fit("LAST USED", 10) + "  " + fitRight("IDLE", 6)
	if c.notes > 0 {
		hdr += "  " + fit("NOTES", c.notes)
	}
	b.WriteString(paint(stHeader, hdr) + "\n")

	h := m.listHeight()
	if len(m.rows) == 0 {
		b.WriteString(paint(stDim, "  No packages match this view.") + "\n")
		h--
	}
	end := m.offset + h
	if end > len(m.rows) {
		end = len(m.rows)
	}
	for i := m.offset; i < end; i++ {
		b.WriteString(m.row(m.rows[i], i == m.cursor, c) + "\n")
	}
	for i := end - m.offset; i < h; i++ {
		b.WriteString("\n")
	}

	b.WriteString(m.statusLine() + "\n")
	b.WriteString(hints(
		"↑↓", "move", "space", "mark", "d", "remove", "c", "clean up", "/", "search",
		"v", "view", "t", "type", "s", "sort", "enter", "details", "?", "help", "q", "quit"))
	return b.String()
}

func (m Model) row(p *analyze.Package, selected bool, c columns) string {
	mark := "  "
	if m.marked[key(p)] {
		mark = "● "
	}
	last := "—"
	if p.LastUsed != nil {
		last = p.LastUsed.Local().Format("2006-01-02")
	}
	idleDays := m.filter.Idle(p)
	cells := []string{
		fit(p.Name, c.name),
		fit(p.Kind, 7),
		fitRight(report.Bytes(p.SizeBytes), 9),
		fit(last, 10),
		fitRight(report.Idle(idleDays), 6),
	}
	notes := ""
	if c.notes > 0 {
		notes = fit(report.Notes(p, m.filter.Direct), c.notes)
	}

	if selected {
		line := mark + strings.Join(cells, "  ")
		if c.notes > 0 {
			line += "  " + notes
		}
		return paint(stSel, fit(line, m.width))
	}
	cells[4] = paint(idleColor(idleDays), cells[4])
	line := paint(stMark, mark) + strings.Join(cells, "  ")
	if c.notes > 0 {
		line += "  " + paint(stDim, notes)
	}
	return line
}

func (m Model) statusLine() string {
	switch {
	case m.err != nil:
		return paint(stErr, "Error: ") + m.err.Error()
	case m.status != "":
		return paint(stWarn, m.status)
	case len(m.marked) > 0:
		return paint(stMark, fmt.Sprintf("%d marked · %s", len(m.marked), report.Bytes(m.markedBytes()))) +
			paint(stDim, "  (d to remove, a to toggle all shown, esc to clear)")
	default:
		pos := ""
		if len(m.rows) > 0 {
			pos = fmt.Sprintf("%d/%d", m.cursor+1, len(m.rows))
		}
		var shown int64
		for _, p := range m.rows {
			shown += p.SizeBytes
		}
		return paint(stDim, fmt.Sprintf("%s shown · %s", pos, report.Bytes(shown)))
	}
}

func hints(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, paint(stKey, pairs[i])+" "+paint(stDim, pairs[i+1]))
	}
	return strings.Join(parts, paint(stDim, " · "))
}

func (m Model) viewDetail() string {
	p := m.current()
	if p == nil {
		return ""
	}
	var lines []string
	add := func(k, v string) {
		lines = append(lines, paint(stDim, fit(k, 13))+v)
	}
	lines = append(lines, paint(stBold, p.Name)+paint(stDim, "  "+p.Kind+" "+p.Version), "")
	if p.Desc != "" {
		lines = append(lines, p.Desc, "")
	}
	add("Size", report.Bytes(p.SizeBytes))
	if p.OldVersions > 0 {
		add("Old versions", fmt.Sprintf("%d, %s reclaimable", p.OldVersions, report.Bytes(p.ReclaimableBytes)))
		for _, op := range p.OldPaths {
			add("", paint(stDim, op))
		}
	}
	add("Installed", p.InstalledAt.Local().Format("2006-01-02 15:04"))
	if p.LastUsed != nil {
		add("Last used", p.LastUsed.Local().Format("2006-01-02 15:04")+paint(stDim, "  ("+p.LastUsedEvidence+")"))
	} else {
		add("Last used", "no use detected since install")
	}
	idle := report.Idle(p.IdleDays)
	if p.ActiveVia != "" {
		idle += paint(stDim, fmt.Sprintf("  (own use: %s; kept active by %s)", report.Idle(p.DirectIdleDays), p.ActiveVia))
	}
	add("Idle", idle)
	switch {
	case p.Kind == analyze.KindCask:
		add("Installed by", "cask")
	case p.OnRequest:
		add("Installed by", "you (on request)")
	case p.Orphan:
		add("Installed by", paint(stWarn, "dependency, no longer needed by anything"))
	default:
		add("Installed by", "dependency")
	}
	if len(p.Dependents) > 0 {
		add("Needed by", wrapList(p.Dependents, m.width-14))
	}
	for _, a := range p.MissingApps {
		add("Missing app", paint(stWarn, a)+paint(stDim, "  (deleted outside brew; c to clean up)"))
	}
	if p.ScanErrors > 0 {
		add("Unreadable", fmt.Sprintf("%d entries (permissions); size may be low", p.ScanErrors))
	}
	lines = append(lines, "", hints("esc", "back", "d", "remove"))
	return strings.Join(lines, "\n")
}

func wrapList(items []string, width int) string {
	if width < 20 {
		width = 20
	}
	var lines []string
	cur := ""
	for _, it := range items {
		cand := it
		if cur != "" {
			cand = cur + ", " + it
		}
		if uniwidth.StringWidth(cand) > width && cur != "" {
			lines = append(lines, cur+",")
			cur = it
		} else {
			cur = cand
		}
	}
	lines = append(lines, cur)
	return strings.Join(lines, "\n"+strings.Repeat(" ", 13))
}

func (m Model) viewDialog() string {
	d := m.dlg
	var lines []string
	listItems := func(items []string, max int) {
		for i, it := range items {
			if i == max {
				lines = append(lines, paint(stDim, fmt.Sprintf("    … and %d more", len(items)-max)))
				break
			}
			lines = append(lines, "    "+it)
		}
	}

	if d.kind == dialogRemove {
		lines = append(lines, paint(stTitle, "Remove packages"), "")
	} else {
		lines = append(lines, paint(stTitle, "Clean up"), "")
		bytes := map[actions.Category]int64{}
		found := map[actions.Category]bool{}
		for _, s := range d.steps {
			bytes[s.Category] += s.Bytes
			found[s.Category] = true
		}
		for i, c := range actions.Categories {
			box := "[ ]"
			if d.cats[c] {
				box = paint(stMark, "[x]")
			}
			extra := ""
			if d.cats[c] {
				extra = "nothing to do"
				if found[c] {
					extra = "~" + report.Bytes(bytes[c])
				}
			}
			lines = append(lines, fmt.Sprintf("%s %s %s  %s", paint(stKey, fmt.Sprint(i+1)), box, c.Describe(), paint(stDim, extra)))
		}
		lines = append(lines, "")
	}

	// Share the remaining screen rows between the steps' item lists, so the
	// total and key hints at the bottom always stay visible.
	fixed := 1 + 2 + len(lines) + 2*len(d.steps) + 4 // header, border, above, step title+command, footer
	if len(d.blocked) > 0 {
		fixed += 2 + min(len(d.blocked), 5) + 1
	}
	perStep := 1
	if len(d.steps) > 0 {
		perStep = max(1, (m.height-fixed)/len(d.steps)-1) // -1 leaves room for "… and N more"
	}
	for _, s := range d.steps {
		lines = append(lines, paint(stBold, s.Title)+paint(stDim, "  ~"+report.Bytes(s.Bytes)))
		listItems(s.Items, perStep)
		lines = append(lines, paint(stDim, "  $ "+truncate(s.Command(), m.width-12)))
	}

	if len(d.blocked) > 0 {
		lines = append(lines, "", paint(stWarn, "Skipped, still needed by other packages:"))
		var bl []string
		for _, p := range d.targets {
			if by, ok := d.blocked[p.Name]; ok {
				bl = append(bl, fmt.Sprintf("%s (needed by %s)", p.Name, truncate(strings.Join(by, ", "), m.width-30)))
			}
		}
		listItems(bl, 5)
	}

	lines = append(lines, "")
	if len(d.steps) == 0 {
		lines = append(lines, paint(stDim, "Nothing to do."), "", hints("esc", "back"))
	} else {
		lines = append(lines, paint(stBold, "Total: ~"+report.Bytes(actions.TotalBytes(d.steps))), "")
		h := []string{"y/enter", "run", "esc", "cancel"}
		if d.kind == dialogRemove && hasCask(d.targets) {
			z := "zap: off"
			if d.zap {
				z = "zap: on (also delete app settings & caches)"
			}
			h = append(h, "z", z)
		}
		if d.kind == dialogClean {
			h = append(h, "1-3", "toggle")
		}
		lines = append(lines, hints(h...))
	}
	return box(lines, m.width)
}

func truncate(s string, w int) string {
	if w < 10 {
		w = 10
	}
	if uniwidth.StringWidth(s) <= w {
		return s
	}
	return strings.TrimRight(fit(s, w), " ")
}

func hasCask(pkgs []*analyze.Package) bool {
	for _, p := range pkgs {
		if p.Kind == analyze.KindCask {
			return true
		}
	}
	return false
}

func viewHelp() string {
	rows := [][2]string{
		{"↑/↓  k/j", "move"},
		{"pgup/pgdn  ctrl+u/d", "page"},
		{"g / G", "top / bottom"},
		{"space", "mark or unmark package"},
		{"a", "mark or unmark everything shown"},
		{"enter", "package details"},
		{"/", "search names and descriptions (esc clears)"},
		{"v", "cycle views: all, requested, idle ≥ 90d, idle ≥ 1y, orphans, broken casks"},
		{"t", "cycle type: all, formula, cask"},
		{"s", "cycle sort: size, idle, name"},
		{"D", "toggle whether dependents count as use"},
		{"d / x / delete", "remove marked packages (or the selected one)"},
		{"c", "clean up old versions, orphans and broken casks"},
		{"r", "rescan"},
		{"esc", "clear marks, then search"},
		{"q / ctrl+c", "quit"},
	}
	var lines []string
	lines = append(lines, paint(stTitle, "Keys"), "")
	for _, r := range rows {
		lines = append(lines, paint(stKey, fit(r[0], 22))+r[1])
	}
	lines = append(lines, "",
		paint(stDim, "Removing or cleaning shows a confirmation first, then runs brew in"),
		paint(stDim, "the normal terminal so you can see its output, and rescans afterwards."),
		"", hints("esc", "back"))
	return box(lines, 0)
}
