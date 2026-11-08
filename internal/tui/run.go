package tui

// Running removal and cleanup inside the TUI. Tasks run one at a time in a
// background goroutine with brew detached from the terminal; progress comes
// back as events through a channel that a Phoenix command drains one message
// at a time, and a floating pane shows a progress bar, per-task status and
// the tail of brew's output.

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/phoenix-tui/phoenix/tea"

	"github.com/BoxingOctopusCreative/brewdust/internal/actions"
	"github.com/BoxingOctopusCreative/brewdust/internal/report"
)

type taskStatus int

const (
	taskPending taskStatus = iota
	taskRunning
	taskOK
	taskFailed
	taskSkipped
)

type taskState struct {
	task     actions.Task
	status   taskStatus
	err      string // the most telling line of output, if it failed
	needsTTY bool   // failed only because brew wanted to prompt
}

type runState struct {
	title    string
	tasks    []taskState
	current  int
	log      []string // tail of brew's output
	started  time.Time
	ended    time.Time
	stop     *atomic.Bool
	stopping bool
	finished bool
	events   <-chan any
}

type (
	evStart struct{ i int }
	evLine  struct{ line string }
	evDone  struct {
		i      int
		err    error
		output string
	}
	evEnd struct{}

	runEventMsg struct{ ev any }
)

const logKeep = 50

func waitRun(ch <-chan any) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return runEventMsg{evEnd{}}
		}
		return runEventMsg{ev}
	}
}

// startRun begins running the steps' tasks and opens the progress pane.
func (m Model) startRun(title string, steps []actions.Step) (Model, tea.Cmd) {
	tasks := actions.AllTasks(steps)
	if len(tasks) == 0 {
		return m, nil
	}
	ch := make(chan any, 256)
	stop := &atomic.Bool{}
	go runTasks(m.env.ctx, m.snap.Prefix, tasks, ch, stop)

	rs := &runState{title: title, started: time.Now(), stop: stop, events: ch}
	for _, t := range tasks {
		rs.tasks = append(rs.tasks, taskState{task: t})
	}
	m.run, m.mode, m.status = rs, modeRunning, ""
	return m, tea.Batch(waitRun(ch), spin())
}

func runTasks(ctx context.Context, prefix string, tasks []actions.Task, ch chan<- any, stop *atomic.Bool) {
	defer close(ch)
	for i, t := range tasks {
		if stop.Load() {
			break
		}
		ch <- evStart{i}
		w := &lineWriter{emit: func(l string) { ch <- evLine{l} }}
		err := actions.ExecuteTask(ctx, prefix, t, w, true)
		w.Flush()
		ch <- evDone{i: i, err: err, output: w.all.String()}
	}
	ch <- evEnd{}
}

// lineWriter turns brew's output into lines (splitting on \r as well, for
// progress meters) and keeps a copy for diagnosing failures.
type lineWriter struct {
	emit func(string)
	buf  []byte
	all  strings.Builder
}

func (w *lineWriter) Write(p []byte) (int, error) {
	if w.all.Len() < 64<<10 {
		w.all.Write(p)
	}
	for _, c := range p {
		if c == '\n' || c == '\r' {
			w.Flush()
			continue
		}
		w.buf = append(w.buf, c)
	}
	return len(p), nil
}

func (w *lineWriter) Flush() {
	if line := strings.TrimSpace(stripANSI(string(w.buf))); line != "" {
		w.emit(line)
	}
	w.buf = w.buf[:0]
}

func (m Model) handleRunEvent(ev any) (Model, tea.Cmd) {
	r := m.run
	if r == nil {
		return m, nil
	}
	switch ev := ev.(type) {
	case evStart:
		r.current = ev.i
		r.tasks[ev.i].status = taskRunning
	case evLine:
		r.log = append(r.log, ev.line)
		if len(r.log) > logKeep {
			r.log = r.log[len(r.log)-logKeep:]
		}
	case evDone:
		t := &r.tasks[ev.i]
		if ev.err == nil {
			t.status = taskOK
		} else {
			t.status = taskFailed
			t.needsTTY = actions.NeedsTerminal(ev.output)
			t.err = failureLine(ev.output, ev.err)
		}
	case evEnd:
		r.finished, r.ended = true, time.Now()
		for i := range r.tasks {
			if r.tasks[i].status == taskPending {
				r.tasks[i].status = taskSkipped
			}
		}
		return m, nil
	}
	return m, waitRun(r.events)
}

// failureLine picks the line of brew output that best explains a failure.
func failureLine(output string, err error) string {
	lines := strings.Split(strings.TrimSpace(stripANSI(output)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "Error:") || strings.HasPrefix(l, "sudo:") {
			return strings.TrimSpace(strings.TrimPrefix(l, "Error:"))
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return err.Error()
}

func (r *runState) counts() (ok, failed, skipped int, freed int64) {
	for _, t := range r.tasks {
		switch t.status {
		case taskOK:
			ok++
			freed += t.task.Bytes
		case taskFailed:
			failed++
		case taskSkipped:
			skipped++
		}
	}
	return
}

func (r *runState) failedTasks() []actions.Task {
	var out []actions.Task
	for _, t := range r.tasks {
		if t.status == taskFailed {
			out = append(out, t.task)
		}
	}
	return out
}

func (r *runState) needsTTY() bool {
	for _, t := range r.tasks {
		if t.needsTTY {
			return true
		}
	}
	return false
}

// runKeys are the actions offered at the bottom of the progress pane.
func (m Model) runKeys() [][2]string {
	r := m.run
	switch {
	case r == nil:
		return nil
	case !r.finished && r.stopping:
		return [][2]string{{"ctrl+c", "quit now"}}
	case !r.finished:
		return [][2]string{{"esc", "stop after this step"}}
	}
	keys := [][2]string{{"enter", "close"}}
	if len(r.failedTasks()) > 0 {
		keys = append(keys, [2]string{"t", "retry failed in terminal"})
	}
	return keys
}

func (m Model) keyRun(k tea.KeyMsg) (Model, tea.Cmd) {
	r := m.run
	if !r.finished {
		switch {
		case isQuit(k) && r.stopping:
			return m, tea.Quit() // brew finishes its current task on its own
		case isQuit(k), k.Type == tea.KeyEsc, k.Type == tea.KeyRune && (k.Rune == 'q' || k.Rune == 's'):
			r.stopping = true
			r.stop.Store(true)
		}
		return m, nil
	}
	switch {
	case isQuit(k):
		return m, tea.Quit()
	case k.Type == tea.KeyEnter, k.Type == tea.KeyEsc, k.Type == tea.KeyRune && k.Rune == 'q':
		return m.closeRun()
	case k.Type == tea.KeyRune && k.Rune == 't' && len(r.failedTasks()) > 0:
		var steps []actions.Step
		for _, t := range r.failedTasks() {
			steps = append(steps, t.AsStep())
		}
		m.run, m.mode = nil, modeList
		return m, m.execute(steps)
	}
	return m, nil
}

// closeRun dismisses the progress pane and rescans.
func (m Model) closeRun() (Model, tea.Cmd) {
	ok, failed, _, freed := m.run.counts()
	m.status = fmt.Sprintf("%d done, ~%s freed; rescanned.", ok, report.Bytes(freed))
	if failed > 0 {
		m.status = fmt.Sprintf("%d done, %d failed, ~%s freed; rescanned.", ok, failed, report.Bytes(freed))
	}
	m.run, m.mode = nil, modeLoading
	return m, tea.Batch(m.loadCmd(), spin())
}

// runWidth is the inner width of the progress pane.
func (m Model) runWidth() int { return min(max(m.geom().w-12, 40), 84) }

func (m Model) runLines() []string {
	r := m.run
	w := m.runWidth()
	ok, failed, skipped, freed := r.counts()
	done := ok + failed
	total := len(r.tasks)
	elapsed := time.Since(r.started)
	if r.finished {
		elapsed = r.ended.Sub(r.started)
	}
	secs := fmt.Sprintf("%ds", int(elapsed.Seconds()))

	// Headline.
	var head string
	switch {
	case !r.finished:
		t := r.tasks[r.current].task
		verb := t.Action + " " + t.Label + "…"
		if r.stopping {
			verb += "  (stopping after this step)"
		}
		head = paint(stKey, spinnerFrames[m.spinner%len(spinnerFrames)]) + " " + paint(stBold, truncate(verb, w-12))
	case skipped > 0:
		head = paint(stWarn, fmt.Sprintf("Stopped after %d of %d", done, total))
	case failed > 0:
		head = paint(stErr, fmt.Sprintf("✗ Finished with %d failed", failed))
	default:
		head = paint(stGaugeGreen, "✓ Done")
	}
	head = fitANSI(head, w-len(secs)) + paint(stDim, secs)

	frac := float64(done) / float64(max(total, 1))
	fill := stGaugeGreen
	if failed > 0 {
		fill = stGaugeYellow
	}
	stats := fmt.Sprintf(" %d/%d · ~%s freed", done, total, report.Bytes(freed))
	bar := gauge(frac, w-len([]rune(stats)), fill) + paint(stDim, stats)

	lines := []string{head, bar, ""}

	// Task list, windowed around the current task.
	logN := 5
	g := m.geom()
	avail := g.h - chromeRows - 2
	fixed := len(lines) + 1 + 1 + logN + 2
	if r.finished && r.needsTTY() {
		fixed += 2
	}
	maxTasks := max(avail-fixed, 3)
	start := 0
	if total > maxTasks {
		start = min(max(r.current-maxTasks/2, 0), total-maxTasks)
	}
	labelW := min(28, w/3)
	for i := start; i < min(start+maxTasks, total); i++ {
		lines = append(lines, m.taskLine(r.tasks[i], w, labelW))
	}
	if total > maxTasks {
		lines[len(lines)-1] = paint(stDim, fmt.Sprintf("  … %d tasks in all", total))
	}

	if r.finished && r.needsTTY() {
		lines = append(lines, "", paint(stWarn, truncate("Some steps need your password. Press t to run them in the terminal.", w)))
	}

	lines = append(lines, "", paint(stHeader, "brew output"))
	tail := r.log
	if len(tail) > logN {
		tail = tail[len(tail)-logN:]
	}
	for _, l := range tail {
		lines = append(lines, paint(stDim, truncate(l, w)))
	}
	for i := len(tail); i < logN; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, "", pillRow(m.runKeys()))
	return lines
}

func (m Model) taskLine(t taskState, w, labelW int) string {
	var icon, note string
	switch t.status {
	case taskPending:
		icon = paint(stDim, "·")
	case taskRunning:
		icon = paint(stKey, spinnerFrames[m.spinner%len(spinnerFrames)])
		note = paint(stKey, strings.ToLower(t.task.Action))
	case taskOK:
		icon = paint(stGaugeGreen, "✓")
		note = paint(stDim, "done")
	case taskFailed:
		icon = paint(stErr, "✗")
		note = paint(stGaugeRed, t.err)
	case taskSkipped:
		icon = paint(stDim, "–")
		note = paint(stDim, "skipped")
	}
	line := icon + " " + fit(t.task.Label, labelW) + " " + fitRight(report.Bytes(t.task.Bytes), 9) + "  " + note
	return fitANSI(line, w)
}

// pillRow renders key hints as pills; pillHit finds which one is at col.
func pillRow(keys [][2]string) string {
	var parts []string
	for _, k := range keys {
		parts = append(parts, pill(k[0], k[1]))
	}
	return strings.Join(parts, " ")
}

func pillHit(keys [][2]string, col int) string {
	pos := 0
	for _, k := range keys {
		w := visibleWidth(pill(k[0], k[1]))
		if col >= pos && col < pos+w {
			return k[0]
		}
		pos += w + 1
	}
	return ""
}
