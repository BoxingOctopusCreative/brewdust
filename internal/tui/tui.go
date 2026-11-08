// Package tui is brewdust's interactive interface, built on Phoenix.
package tui

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/phoenix-tui/phoenix/tea"
	"golang.org/x/term"

	"github.com/BoxingOctopusCreative/brewdust/internal/actions"
	"github.com/BoxingOctopusCreative/brewdust/internal/analyze"
	"github.com/BoxingOctopusCreative/brewdust/internal/app"
)

// Options configures the starting state.
type Options struct {
	Filter  app.Filter
	SortBy  string
	Workers int
}

// env is shared, mutable plumbing the immutable Model points at.
type env struct {
	ctx     context.Context
	prog    *tea.Program[Model]
	workers int
}

type (
	loadedMsg struct {
		snap *app.Snapshot
		err  error
	}
	doneMsg struct {
		total, failed int
		err           error
	}
	spinMsg struct{}
)

// Run starts the interface and blocks until the user quits.
func Run(ctx context.Context, opt Options) error {
	// Ctrl+C while brew runs (TUI suspended) should stop brew, not brewdust,
	// so detach from the CLI's interrupt-cancelled context.
	e := &env{ctx: context.WithoutCancel(ctx), workers: opt.Workers}
	m := newModel(e, opt)
	m.width, m.height = termSize()

	opts := []tea.Option[Model]{tea.WithAltScreen[Model]()}
	// Give Phoenix its own handle on the terminal. os.Stdin is a blocking
	// file that doesn't support read deadlines, so when Phoenix suspends, its
	// pending read can't be cancelled and would swallow the next keystroke
	// meant for brew (a sudo prompt, say) or our "press Enter". A separately
	// opened /dev/tty is pollable, so Phoenix can cancel the read cleanly.
	if tty, err := os.Open("/dev/tty"); err == nil {
		defer tty.Close()
		opts = append(opts, tea.WithInput[Model](tty))
	}
	p := tea.New(m, opts...)
	e.prog = p

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			w, h := termSize()
			_ = p.Send(tea.WindowSizeMsg{Width: w, Height: h})
		}
	}()
	return p.Run()
}

func termSize() (int, int) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 || h <= 0 {
		return 100, 30
	}
	return w, h
}

func (m Model) loadCmd() tea.Cmd {
	e := m.env
	return func() tea.Msg {
		snap, err := app.Load(e.ctx, e.workers)
		return loadedMsg{snap: snap, err: err}
	}
}

func spin() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(100 * time.Millisecond)
		return spinMsg{}
	}
}

// execute hands the terminal back to the user, runs the steps with live
// output, waits for Enter, then restores the interface.
func (m Model) execute(steps []actions.Step) tea.Cmd {
	e, prefix := m.env, m.snap.Prefix
	return func() tea.Msg {
		if err := e.prog.Suspend(); err != nil {
			return doneMsg{err: err}
		}
		out := os.Stdout
		fmt.Fprint(out, "\033[2J\033[H")
		failed := 0
		for _, s := range steps {
			fmt.Fprintf(out, "\033[1m==> %s\033[0m\n$ %s\n", s.Title, s.Command())
			if err := actions.Execute(e.ctx, prefix, s, out); err != nil {
				fmt.Fprintf(out, "\033[31merror:\033[0m %v\n", err)
				failed++
			}
			fmt.Fprintln(out)
		}
		if failed > 0 {
			fmt.Fprintf(out, "%d of %d step(s) failed.\n", failed, len(steps))
		} else {
			fmt.Fprintln(out, "Done.")
		}
		fmt.Fprint(out, "Press Enter to return to brewdust…")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		return doneMsg{total: len(steps), failed: failed, err: e.prog.Resume()}
	}
}

func key(p *analyze.Package) string { return p.Kind + ":" + p.Name }
