// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package tui

import (
	"fmt"
	"sort"
	"time"

	tea "github.com/phoenix-tui/phoenix/tea"

	"github.com/BoxingOctopusCreative/brewdust/internal/actions"
	"github.com/BoxingOctopusCreative/brewdust/internal/analyze"
	"github.com/BoxingOctopusCreative/brewdust/internal/app"
)

type mode int

const (
	modeLoading mode = iota
	modeList
	modeSearch
	modeDetail
	modeDialog
	modeHelp
	modeRunning // the progress pane for removal or cleanup
)

type dialogKind int

const (
	dialogRemove dialogKind = iota
	dialogClean
)

type dialog struct {
	kind    dialogKind
	targets []*analyze.Package // remove: what the user picked
	zap     bool
	cats    map[actions.Category]bool // clean: enabled categories
	steps   []actions.Step
	blocked map[string][]string
}

// preset is a named starting filter the user cycles through with `v`.
type preset struct {
	name  string
	apply func(f *app.Filter)
}

var presets = []preset{
	{"all", func(f *app.Filter) {}},
	{"requested", func(f *app.Filter) { f.Requested = true }},
	{"dependencies", func(f *app.Filter) { f.Needed = true }},
	{"idle ≥ 90d", func(f *app.Filter) { f.StaleDays = 90 }},
	{"idle ≥ 1y", func(f *app.Filter) { f.StaleDays = 365 }},
	{"orphans", func(f *app.Filter) { f.Orphans = true }},
	{"broken casks", func(f *app.Filter) { f.Broken = true }},
	{"deprecated", func(f *app.Filter) { f.Retiring = true }},
	{"no formula", func(f *app.Filter) { f.NoFormula = true }},
}

var kinds = []string{"all", analyze.KindFormula, analyze.KindCask}

// Model is the Phoenix model for the whole interface.
type Model struct {
	env           *env
	width, height int

	mode    mode
	spinner int
	err     error
	status  string

	snap   *app.Snapshot
	filter app.Filter
	preset int // index into presets, or -1 for the filter given on the command line
	sortBy string
	rows   []*analyze.Package
	cursor int
	offset int
	marked map[string]bool

	dlg *dialog
	run *runState

	drag        dragState
	lastClick   int // row index of the last click, for double-click
	lastClickAt time.Time
}

func newModel(e *env, opt Options) Model {
	f := opt.Filter
	if f.Kind == "" {
		f.Kind = "all"
	}
	sortBy := opt.SortBy
	if sortBy == "" {
		sortBy = "size"
	}
	p := 0
	if f.Requested || f.Orphans || f.Broken || f.Retiring || f.NoFormula || f.Needed || f.StaleDays > 0 {
		p = -1
	}
	return Model{env: e, mode: modeLoading, filter: f, preset: p, sortBy: sortBy, marked: map[string]bool{}, lastClick: -1}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadCmd(), spin())
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampScroll()
		return m, nil

	case spinMsg:
		if m.mode == modeLoading || (m.mode == modeRunning && m.run != nil && !m.run.finished) {
			m.spinner++
			return m, spin()
		}
		return m, nil

	case loadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.mode = modeList
			return m, nil
		}
		m.err = nil
		m.snap = msg.snap
		m.mode = modeList
		// Drop marks for packages that no longer exist.
		live := map[string]bool{}
		for _, p := range m.snap.Packages {
			live[key(p)] = true
		}
		for k := range m.marked {
			if !live[k] {
				delete(m.marked, k)
			}
		}
		m.refresh("")
		return m, nil

	case doneMsg:
		m.width, m.height = termSize()
		switch {
		case msg.err != nil:
			m.status = "Couldn't restore the terminal: " + msg.err.Error()
		case msg.failed > 0:
			m.status = fmt.Sprintf("%d of %d step(s) failed; rescanned.", msg.failed, msg.total)
		default:
			m.status = "Done; rescanned."
		}
		m.mode = modeLoading
		return m, tea.Batch(m.loadCmd(), spin())

	case tea.KeyMsg:
		return m.handleKey(msg)

	case mouseMsg:
		return m.handleMouse(msg)

	case runEventMsg:
		return m.handleRunEvent(msg.ev)
	}
	return m, nil
}

func isQuit(k tea.KeyMsg) bool {
	return k.Type == tea.KeyCtrlC || (k.Type == tea.KeyRune && k.Ctrl && k.Rune == 'c')
}

func (m Model) handleKey(k tea.KeyMsg) (Model, tea.Cmd) {
	if m.mode == modeRunning && m.run != nil {
		return m.keyRun(k) // ctrl+c there means "stop", not "quit"
	}
	if isQuit(k) {
		return m, tea.Quit()
	}
	switch m.mode {
	case modeLoading:
		if k.Type == tea.KeyRune && k.Rune == 'q' {
			return m, tea.Quit()
		}
	case modeSearch:
		return m.keySearch(k)
	case modeDetail, modeHelp:
		if m.mode == modeDetail && k.Type == tea.KeyRune && (k.Rune == 'd' || k.Rune == 'x') {
			m.mode = modeList
			saved := m.marked
			m.marked = map[string]bool{} // remove just the package being viewed
			m2, cmd := m.openRemove()
			m2.marked = saved
			return m2, cmd
		}
		if k.Type == tea.KeyEsc || k.Type == tea.KeyEnter || (k.Type == tea.KeyRune && (k.Rune == 'q' || k.Rune == '?')) {
			m.mode = modeList
		}
	case modeDialog:
		return m.keyDialog(k)
	case modeList:
		return m.keyList(k)
	}
	return m, nil
}

func (m Model) keyList(k tea.KeyMsg) (Model, tea.Cmd) {
	m.status = ""
	page := m.listHeight() - 1
	switch k.Type {
	case tea.KeyUp:
		m.move(-1)
	case tea.KeyDown:
		m.move(1)
	case tea.KeyPgUp:
		m.move(-page)
	case tea.KeyPgDown:
		m.move(page)
	case tea.KeyHome:
		m.move(-len(m.rows))
	case tea.KeyEnd:
		m.move(len(m.rows))
	case tea.KeySpace:
		m.toggleMark()
	case tea.KeyEnter:
		if m.current() != nil {
			m.mode = modeDetail
		}
	case tea.KeyEsc:
		switch {
		case len(m.marked) > 0:
			m.marked = map[string]bool{}
		case m.filter.Query != "":
			m.filter.Query = ""
			m.refresh(m.currentKey())
		}
	case tea.KeyDelete, tea.KeyBackspace:
		return m.openRemove()
	case tea.KeyRune:
		if k.Ctrl {
			switch k.Rune {
			case 'd':
				m.move(page / 2)
			case 'u':
				m.move(-page / 2)
			}
			return m, nil
		}
		switch k.Rune {
		case 'q':
			return m, tea.Quit()
		case 'k':
			m.move(-1)
		case 'j':
			m.move(1)
		case 'g':
			m.move(-len(m.rows))
		case 'G':
			m.move(len(m.rows))
		case ' ':
			m.toggleMark()
		case 'a':
			m.toggleAll()
		case '/':
			m.mode = modeSearch
		case 's':
			m.sortBy = next(app.SortKeys, m.sortBy)
			m.refresh(m.currentKey())
		case 't':
			m.filter.Kind = next(kinds, m.filter.Kind)
			m.refresh(m.currentKey())
		case 'v':
			m.cyclePreset()
		case 'D':
			m.filter.Direct = !m.filter.Direct
			m.refresh(m.currentKey())
			if m.filter.Direct {
				m.status = "Idle now ignores dependents (each package's own use only)."
			} else {
				m.status = "Idle now includes use by dependents."
			}
		case 'd', 'x':
			return m.openRemove()
		case 'c':
			return m.openClean()
		case 'r':
			m.mode = modeLoading
			return m, tea.Batch(m.loadCmd(), spin())
		case '?':
			m.mode = modeHelp
		}
	}
	return m, nil
}

func (m Model) keySearch(k tea.KeyMsg) (Model, tea.Cmd) {
	cur := m.currentKey()
	switch k.Type {
	case tea.KeyEnter:
		m.mode = modeList
	case tea.KeyEsc:
		m.filter.Query = ""
		m.mode = modeList
	case tea.KeyBackspace, tea.KeyDelete:
		if r := []rune(m.filter.Query); len(r) > 0 {
			m.filter.Query = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		m.filter.Query += " "
	case tea.KeyRune:
		if !k.Ctrl && !k.Alt {
			m.filter.Query += string(k.Rune)
		}
	default:
		return m, nil
	}
	m.refresh(cur)
	return m, nil
}

func (m Model) keyDialog(k tea.KeyMsg) (Model, tea.Cmd) {
	d := m.dlg
	cancel := k.Type == tea.KeyEsc || (k.Type == tea.KeyRune && (k.Rune == 'n' || k.Rune == 'q'))
	run := k.Type == tea.KeyEnter || (k.Type == tea.KeyRune && k.Rune == 'y')
	switch {
	case cancel:
		m.dlg, m.mode = nil, modeList
	case run:
		if len(d.steps) == 0 {
			return m, nil
		}
		// Marks stay put: the rescan afterwards drops those that were removed,
		// so anything that failed is still marked for another try.
		title := "Removing packages"
		if d.kind == dialogClean {
			title = "Cleaning up"
		}
		m.dlg, m.mode = nil, modeList
		return m.startRun(title, d.steps)
	case k.Type == tea.KeyRune && d.kind == dialogRemove && k.Rune == 'z':
		d.zap = !d.zap
		m.planRemove()
	case k.Type == tea.KeyRune && d.kind == dialogClean && k.Rune >= '1' && int(k.Rune-'1') < len(actions.Categories):
		c := actions.Categories[k.Rune-'1']
		d.cats[c] = !d.cats[c]
		m.planClean()
	}
	return m, nil
}

func (m *Model) openRemove() (Model, tea.Cmd) {
	var targets []*analyze.Package
	if len(m.marked) > 0 {
		for _, p := range m.snap.Packages {
			if m.marked[key(p)] {
				targets = append(targets, p)
			}
		}
	} else if p := m.current(); p != nil {
		targets = append(targets, p)
	}
	if len(targets) == 0 {
		return *m, nil
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].SizeBytes > targets[j].SizeBytes })
	m.dlg = &dialog{kind: dialogRemove, targets: targets}
	m.planRemove()
	m.mode = modeDialog
	return *m, nil
}

func (m *Model) planRemove() {
	plan := actions.PlanRemoval(m.dlg.targets, actions.RemoveOptions{Zap: m.dlg.zap})
	m.dlg.steps, m.dlg.blocked = plan.Steps, plan.Blocked
}

func (m *Model) openClean() (Model, tea.Cmd) {
	if m.snap == nil {
		return *m, nil
	}
	cats := map[actions.Category]bool{}
	for _, c := range actions.Categories {
		cats[c] = true
	}
	m.dlg = &dialog{kind: dialogClean, cats: cats}
	m.planClean()
	m.mode = modeDialog
	return *m, nil
}

func (m *Model) planClean() {
	var cats []actions.Category
	for _, c := range actions.Categories {
		if m.dlg.cats[c] {
			cats = append(cats, c)
		}
	}
	m.dlg.steps = actions.PlanCleanup(m.snap.Packages, cats)
}

// refresh re-filters and re-sorts, keeping the cursor on keep if it is still visible.
func (m *Model) refresh(keep string) {
	if m.snap == nil {
		return
	}
	m.rows = m.filter.Apply(m.snap.Packages)
	_ = app.Sort(m.rows, m.sortBy, m.filter)
	if keep != "" {
		for i, p := range m.rows {
			if key(p) == keep {
				m.cursor = i
				break
			}
		}
	}
	m.clampScroll()
}

func (m *Model) cyclePreset() {
	cur := m.currentKey()
	m.preset = (m.preset + 1) % len(presets)
	f := m.baseFilter()
	presets[m.preset].apply(&f)
	m.filter = f
	m.refresh(cur)
}

// baseFilter keeps the settings that views don't control.
func (m Model) baseFilter() app.Filter {
	return app.Filter{Kind: m.filter.Kind, Query: m.filter.Query, Direct: m.filter.Direct, MinBytes: m.filter.MinBytes}
}

func (m *Model) move(delta int) {
	m.cursor += delta
	m.clampScroll()
}

func (m *Model) clampScroll() {
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if max := len(m.rows) - h; m.offset > max {
		m.offset = max
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *Model) toggleMark() {
	if p := m.current(); p != nil {
		if m.marked[key(p)] {
			delete(m.marked, key(p))
		} else {
			m.marked[key(p)] = true
		}
		m.move(1)
	}
}

func (m *Model) toggleAll() {
	all := len(m.rows) > 0
	for _, p := range m.rows {
		all = all && m.marked[key(p)]
	}
	for _, p := range m.rows {
		if all {
			delete(m.marked, key(p))
		} else {
			m.marked[key(p)] = true
		}
	}
}

func (m Model) current() *analyze.Package {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor]
	}
	return nil
}

func (m Model) currentKey() string {
	if p := m.current(); p != nil {
		return key(p)
	}
	return ""
}

func (m Model) markedBytes() int64 {
	var n int64
	for _, p := range m.snap.Packages {
		if m.marked[key(p)] {
			n += p.SizeBytes
		}
	}
	return n
}

func (m Model) presetName() string {
	if m.preset < 0 {
		return "custom"
	}
	return presets[m.preset].name
}

func next(list []string, cur string) string {
	for i, s := range list {
		if s == cur {
			return list[(i+1)%len(list)]
		}
	}
	return list[0]
}

// presetIndex returns the index of the named view tab.
func presetIndex(name string) int {
	for i, p := range presets {
		if p.name == name {
			return i
		}
	}
	return 0
}
