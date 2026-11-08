package tui

import (
	"time"

	tea "github.com/phoenix-tui/phoenix/tea"

	"github.com/BoxingOctopusCreative/brewdust/internal/actions"
)

const (
	doubleClick = 400 * time.Millisecond
	wheelStep   = 3
)

// dragState tracks a scrollbar drag in progress.
type dragState struct {
	active bool
	grab   int // row within the thumb that the pointer holds
}

func (m Model) handleMouse(ev mouseMsg) (Model, tea.Cmd) {
	if m.snap == nil {
		if ev.Action == mousePress && ev.Y == m.geom().h-1 {
			return m.pressStatusKey(ev.X)
		}
		return m, nil
	}
	g := m.geom()

	// A drag keeps going wherever the pointer wanders until the button is released.
	if m.drag.active {
		switch ev.Action {
		case mouseMotion:
			m.dragThumb(ev.Y, g)
		case mouseRelease:
			m.drag.active = false
		}
		return m, nil
	}
	if ev.Action != mousePress {
		return m, nil
	}
	if ev.Y == g.h-1 && ev.Button == btnLeft {
		return m.pressStatusKey(ev.X)
	}

	switch m.mode {
	case modeHelp, modeDetail:
		if ev.Button == btnLeft {
			m.mode = modeList
		}
		return m, nil
	case modeDialog:
		return m.mouseDialog(ev)
	case modeList, modeSearch:
		return m.mouseList(ev, g)
	}
	return m, nil
}

func (m Model) mouseList(ev mouseMsg, g geom) (Model, tea.Cmd) {
	inList := ev.X < g.listW
	inRows := ev.Y >= g.rowTop && ev.Y < g.rowTop+g.rows

	switch ev.Button {
	case btnWheelUp, btnWheelDown:
		if inList {
			step := wheelStep
			if ev.Button == btnWheelUp {
				step = -step
			}
			m.scrollBy(step)
		}
		return m, nil

	case btnRight:
		if inList && inRows {
			if i := m.offset + ev.Y - g.rowTop; i < len(m.rows) {
				m.toggleMarkAt(i)
			}
		}
		return m, nil

	case btnLeft:
	default:
		return m, nil
	}

	switch {
	case ev.Y == 0: // tab bar
		if i := m.tabHit(ev.X); i >= 0 {
			m.setPreset(i)
		}

	case inList && ev.Y == g.headerY:
		if by := m.headerSort(ev.X, g); by != "" && by != m.sortBy {
			m.sortBy = by
			m.refresh(m.currentKey())
		}

	case ev.X == g.scrollX && inRows:
		if at, length, ok := m.thumb(); ok {
			row := ev.Y - g.rowTop
			if row >= at && row < at+length {
				m.drag = dragState{active: true, grab: row - at}
			} else {
				// Clicking the track jumps there, then keeps dragging.
				m.drag = dragState{active: true, grab: length / 2}
				m.dragThumb(ev.Y, g)
			}
		}

	case inList && inRows:
		i := m.offset + ev.Y - g.rowTop
		if i >= len(m.rows) {
			return m, nil
		}
		if m.mode == modeSearch {
			m.mode = modeList
		}
		m.status = ""
		switch {
		case ev.X <= 3 || ev.Ctrl: // the ● column, or ctrl-click
			m.toggleMarkAt(i)
		case ev.Shift:
			m.markRange(m.cursor, i)
		}
		now := time.Now()
		double := m.lastClick == i && now.Sub(m.lastClickAt) < doubleClick
		m.cursor, m.lastClick, m.lastClickAt = i, i, now
		m.clampScroll()
		if double && ev.X > 3 && !ev.Ctrl && !ev.Shift {
			m.lastClickAt = time.Time{}
			m.mode = modeDetail
		}

	case !inList && g.diskTop >= 0 && ev.Y > g.diskTop && ev.Y < g.diskTop+diskPaneH-1:
		// Disk pane rows: Formulae, Casks, Idle, Old, Orphans, Broken, Marked.
		switch ev.Y - g.diskTop - 1 {
		case 3:
			return m.openCleanFor(actions.OldVersions)
		case 4:
			return m.openCleanFor(actions.Orphans)
		case 5:
			return m.openCleanFor(actions.Broken)
		default:
			return m.openCleanFor(actions.Categories...)
		}

	case !inList && ev.Y > 0 && ev.Y < g.h-1:
		if m.current() != nil { // the Details pane zooms
			m.mode = modeDetail
		}
	}
	return m, nil
}

// mouseDialog handles clicks on floating panes: outside closes, the pills at
// the bottom run or cancel, category lines toggle, the total line toggles zap.
func (m Model) mouseDialog(ev mouseMsg) (Model, tea.Cmd) {
	if ev.Button != btnLeft {
		return m, nil
	}
	content := m.dialogLines()
	x, y, w, h := m.floatRect(content)
	if ev.X < x || ev.X >= x+w || ev.Y < y || ev.Y >= y+h {
		return m.keyDialog(tea.KeyMsg{Type: tea.KeyEsc})
	}
	line := ev.Y - y - 1
	last := len(content) - 1
	runW := visibleWidth(pill("y", "run"))
	switch {
	case line == last && len(m.dlg.steps) > 0:
		col := ev.X - x - 2
		switch {
		case col >= 0 && col < runW:
			return m.keyDialog(tea.KeyMsg{Type: tea.KeyRune, Rune: 'y'})
		case col > runW && col <= runW+visibleWidth(pill("esc", "cancel")):
			return m.keyDialog(tea.KeyMsg{Type: tea.KeyEsc})
		}
	case m.dlg.kind == dialogClean && line >= 0 && line < len(actions.Categories):
		return m.keyDialog(tea.KeyMsg{Type: tea.KeyRune, Rune: rune('1' + line)})
	case m.dlg.kind == dialogRemove && line == last-2 && hasCask(m.dlg.targets):
		return m.keyDialog(tea.KeyMsg{Type: tea.KeyRune, Rune: 'z'})
	}
	return m, nil
}

// pressStatusKey treats a click on a status-bar hint as pressing that key.
func (m Model) pressStatusKey(x int) (Model, tea.Cmd) {
	var k tea.KeyMsg
	switch name := m.statusHit(x); name {
	case "", "↑↓", "type", "1-3":
		return m, nil
	case "space":
		k = tea.KeyMsg{Type: tea.KeySpace}
	case "enter":
		k = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		k = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		k = tea.KeyMsg{Type: tea.KeyRune, Rune: []rune(name)[0]}
	}
	return m.handleKey(k)
}

// dragThumb moves the list so the thumb sits under the pointer.
func (m *Model) dragThumb(y int, g geom) {
	_, length, ok := m.thumb()
	if !ok {
		return
	}
	track := g.rows - length
	at := min(max(y-g.rowTop-m.drag.grab, 0), track)
	maxOffset := len(m.rows) - g.rows
	if track > 0 {
		m.offset = (at*maxOffset + track/2) / track
	}
	m.keepCursorInView()
}

func (m *Model) scrollBy(n int) {
	m.offset = min(max(m.offset+n, 0), max(len(m.rows)-m.listHeight(), 0))
	m.keepCursorInView()
}

// keepCursorInView moves the cursor (not the view) so it stays visible.
func (m *Model) keepCursorInView() {
	rows := m.listHeight()
	if m.cursor < m.offset {
		m.cursor = m.offset
	}
	if m.cursor >= m.offset+rows {
		m.cursor = m.offset + rows - 1
	}
	m.cursor = min(max(m.cursor, 0), max(len(m.rows)-1, 0))
}

func (m *Model) toggleMarkAt(i int) {
	k := key(m.rows[i])
	if m.marked[k] {
		delete(m.marked, k)
	} else {
		m.marked[k] = true
	}
}

func (m *Model) markRange(a, b int) {
	if a > b {
		a, b = b, a
	}
	for i := max(a, 0); i <= b && i < len(m.rows); i++ {
		m.marked[key(m.rows[i])] = true
	}
}

func (m *Model) setPreset(i int) {
	cur := m.currentKey()
	m.preset = i
	f := m.baseFilter()
	presets[i].apply(&f)
	m.filter = f
	m.refresh(cur)
}

// openCleanFor opens the cleanup dialog with only the given categories on.
func (m *Model) openCleanFor(cats ...actions.Category) (Model, tea.Cmd) {
	mm, cmd := m.openClean()
	for _, c := range actions.Categories {
		mm.dlg.cats[c] = false
	}
	for _, c := range cats {
		mm.dlg.cats[c] = true
	}
	mm.planClean()
	return mm, cmd
}
