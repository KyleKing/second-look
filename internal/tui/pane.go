package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// pane is a program running on a pty inside the frame. The emulator is the
// screen the program draws on and the keyboard it reads from, so the model
// only has to draw the emulator and forward keys. The child owns the input
// while it runs — the same hand-off as tea.ExecProcess, except the frame
// stays up beside it.
type pane struct {
	emu  *vt.SafeEmulator
	tty  *os.File
	cmd  *exec.Cmd
	done chan error
	// woke is signaled when the emulator's contents changed, buffered to one
	// because every wake redraws the whole pane anyway.
	woke chan struct{}
	// cursor is whether the program wants a cursor drawn, which nvim toggles
	// as it changes shape, and shape is the one it asked for.
	cursor atomic.Bool
	shape  atomic.Int64
}

// errNoProgram is a pane asked to run nothing.
var errNoProgram = errors.New("no program to run")

// startPane runs argv on a pty w by h cells. The emulator answers the child's
// terminal queries itself, so a program that asks before drawing does not
// stall waiting on a reply nothing else was going to send.
func startPane(ctx context.Context, argv []string, w, h int) (*pane, error) {
	if len(argv) == 0 {
		return nil, errNoProgram
	}

	p := &pane{
		emu:  vt.NewSafeEmulator(w, h),
		done: make(chan error, 1),
		woke: make(chan struct{}, 1),
	}
	p.emu.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(v bool) { p.cursor.Store(v) },
		CursorStyle:      func(s vt.CursorStyle, _ bool) { p.shape.Store(int64(s)) },
	})

	//nolint:gosec // the command is the user's own EDITOR and the path is our temp file
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	// The emulator speaks xterm plus the modern input modes, and answers
	// truecolor asks whether or not the outer terminal does.
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")

	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: cells(h), Cols: cells(w)})
	if err != nil {
		return nil, fmt.Errorf("starting %s: %w", argv[0], err)
	}

	p.tty, p.cmd = tty, cmd

	go p.pump()
	// The emulator's read side is what a real terminal sends: keys, replies to
	// the child's queries, pasted text. It all belongs on the child's stdin.
	//nolint:errcheck // the copy ends when either side closes, which is the pane ending
	go func() { _, _ = io.Copy(tty, p.emu) }()

	return p, nil
}

// cells is a pane dimension as a winsize says it, clamped rather than wrapped:
// a frame past 65535 cells or one below zero would otherwise name the other
// end of the range.
func cells(n int) uint16 {
	return uint16(min(max(n, 0), math.MaxUint16))
}

// pump moves the child's output onto the emulated screen, then reports the
// exit once the pty has nothing left to say.
func (p *pane) pump() {
	buf := make([]byte, 32*1024)

	for {
		n, err := p.tty.Read(buf)
		if n > 0 {
			if _, werr := p.emu.Write(buf[:n]); werr != nil {
				break
			}

			select {
			case p.woke <- struct{}{}:
			default:
			}
		}

		if err != nil {
			break
		}
	}

	// Wait belongs after the read side has drained: a wait taken while the
	// pty still holds output can lose the child's last frame.
	p.done <- p.cmd.Wait()

	select {
	case p.woke <- struct{}{}:
	default:
	}
}

// watch blocks until the pane has something new to draw or the child exits.
// It is the model's redraw signal, re-armed on every wake.
//
//nolint:ireturn // the pump reports through the program's own message channel
func (p *pane) watch() tea.Msg {
	select {
	case err := <-p.done:
		return paneGoneMsg{err: err}
	case <-p.woke:
		return paneWakeMsg{}
	}
}

// paneWakeMsg is new output on the pane's screen.
type paneWakeMsg struct{}

// paneGoneMsg is the child exiting, which ends the hand-off.
type paneGoneMsg struct{ err error }

// send forwards one keypress the way a real terminal would encode it for the
// modes the child has set.
func (p *pane) send(k tea.KeyPressMsg) {
	key := tea.Key(k)

	if key.Mod&uv.ModAlt != 0 {
		// Alt is an escape prefix on the wire, whichever path encodes the key.
		p.emu.SendText("\x1b")
		key.Mod &^= uv.ModAlt
	}

	if key.Text != "" {
		// SendKey encodes nothing for a printable key carrying a modifier, so
		// the text the press produced is sent directly rather than trusting
		// the code table to have it.
		p.emu.SendText(key.Text)

		return
	}

	p.emu.SendKey(uv.KeyPressEvent(uv.Key{
		Mod: key.Mod, Code: key.Code,
		ShiftedCode: key.ShiftedCode, BaseCode: key.BaseCode,
	}))
}

func (p *pane) resize(w, h int) {
	p.emu.Resize(w, h)
	//nolint:errcheck // a resize the child never hears costs a repaint, nothing more
	_ = pty.Setsize(p.tty, &pty.Winsize{Rows: cells(h), Cols: cells(w)})
}

// stop ends the child and leaves the teardown to the pump, which still
// drains the pty and reports through paneGoneMsg, where the closing happens.
func (p *pane) stop() {
	if p.cmd.Process != nil {
		//nolint:errcheck // the child is already gone often enough to not be news
		_ = p.cmd.Process.Kill()
	}
}

// kill ends a pane the screen is done with without waiting for the child.
func (p *pane) kill() {
	if p.cmd.Process != nil {
		//nolint:errcheck // the child is already gone often enough to not be news
		_ = p.cmd.Process.Kill()
	}

	//nolint:errcheck // the emulator's close is cleanup, not a decision
	_ = p.emu.Close()

	//nolint:errcheck // a closed pty is the wanted state either way
	_ = p.tty.Close()
}

// row is one screen line as cells.
func (p *pane) row(y int) uv.Line {
	w := p.emu.Width()
	row := make(uv.Line, w)

	for x := range w {
		row[x] = uv.EmptyCell
		if c := p.emu.CellAt(x, y); c != nil {
			row[x] = *c.Clone()
		}
	}

	return row
}

// lines is the emulated screen as styled rows, with the program's cursor drawn
// where it wants one, in the shape it asked for: a cell has no cursor overlay,
// so an underline is the cell's and a bar is a glyph at its left edge.
func (p *pane) lines() []string {
	at := p.emu.CursorPosition()
	out := make([]string, 0, p.emu.Height())

	for y := range p.emu.Height() {
		row := p.row(y)

		if p.cursor.Load() && at.Y == y && at.X < len(row) {
			c := &row[at.X]

			switch vt.CursorStyle(p.shape.Load()) {
			case vt.CursorUnderline:
				c.Style.Underline = ansi.UnderlineSingle
			case vt.CursorBar:
				c.Content, c.Width = "▏", 1
			default:
				c.Style.Attrs |= uv.AttrReverse
			}
		}

		out = append(out, row.Render())
	}

	return out
}

// transcript is the session as the pane rendered it: scrollback plus the
// screen's last frame. Rendering is what resolves the escape sequences a raw
// byte log would carry, so the note gets what ran rather than how it was
// drawn.
func (p *pane) transcript() string {
	sb := p.emu.Scrollback().Lines()
	rows := make([]string, 0, len(sb)+p.emu.Height())

	for _, l := range sb {
		rows = append(rows, l.String())
	}
	for y := range p.emu.Height() {
		rows = append(rows, p.row(y).String())
	}

	return strings.TrimRight(strings.Join(rows, "\n"), " \n")
}
