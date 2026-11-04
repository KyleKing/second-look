package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"github.com/kyleking/second-look/internal/termimg"
)

var (
	errUsageImage = errors.New("usage: second-look image <file-or-url> [<more>]")
	errNoProtocol = errors.New("this terminal has no inline image protocol " +
		"(kitty, iTerm2, or sixel), so there is nothing to draw with")
	errFetchFailed = errors.New("the fetch did not answer 200")
)

// The constants ttySize and fetch work against when the terminal does not
// report them: the lines a hint takes, the fetch's timeout and byte cap, and
// the cell size assumed when no winsize answers.
const (
	hintLines    = 3
	fetchTimeout = 30
	fetchLimit   = 100 << 20
	fallbackCols = 80
	fallbackRows = 24
)

// imageCmd draws image files and URLs on the terminal itself, which is the one
// way a terminal that speaks an inline graphics protocol shows them without
// leaving the screen behind. The review screen runs it through ExecProcess so
// the escapes reach the real terminal rather than the pane's own pty.
func imageCmd(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errUsageImage
	}

	proto := termimg.Detect()
	if proto == termimg.ProtoNone {
		return errNoProtocol
	}

	cols, rows := ttySize(stdout)
	box := rows - hintLines

	for i, arg := range args {
		path, err := fetch(ctx, arg)
		if err != nil {
			return err
		}

		if _, err := io.WriteString(stdout, "\x1b[2J\x1b[H"); err != nil {
			return fmt.Errorf("writing the image: %w", err)
		}
		if err := termimg.Render(stdout, proto, path, cols, box); err != nil {
			return fmt.Errorf("drawing %s: %w", arg, err)
		}

		hint := filepath.Base(path)
		if len(args) > 1 {
			hint = fmt.Sprintf("%d/%d %s", i+1, len(args), hint)
		}

		if _, err := io.WriteString(stdout, "\n\n"+hint+" — any key, q closes\n"); err != nil {
			return fmt.Errorf("writing the image: %w", err)
		}

		if !nextImage(ctx, stdin) {
			break
		}
	}

	termimg.Delete(stdout, proto)

	return nil
}

// fetch resolves what the viewer is asked to draw into a file it can open: a
// URL is downloaded once into a temp file and a path is used as given.
func fetch(ctx context.Context, arg string) (string, error) {
	if !strings.HasPrefix(arg, "http://") && !strings.HasPrefix(arg, "https://") {
		return arg, nil
	}

	//nolint:gosec // the URL is the caller's own image ref, not untrusted input
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, arg, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", arg, err)
	}

	//nolint:gosec // same fetch as above: the URL is the caller's own
	resp, err := (&http.Client{Timeout: fetchTimeout * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", arg, err)
	}
	defer resp.Body.Close() //nolint:errcheck // the bytes are already taken or already failed

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching %s: %w (status %d)", arg, errFetchFailed, resp.StatusCode)
	}

	file, err := os.CreateTemp("", "second-look-image-*")
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", arg, err)
	}
	defer file.Close() //nolint:errcheck // a closed-over temp file reports nothing new

	if _, err := io.Copy(file, io.LimitReader(resp.Body, fetchLimit)); err != nil {
		return "", fmt.Errorf("fetching %s: %w", arg, err)
	}

	return file.Name(), nil
}

// nextImage waits for one keypress and reports whether to show the next image:
// q, esc, and ctrl+c stop, and anything else moves on.
func nextImage(ctx context.Context, stdin io.Reader) bool {
	file, ok := stdin.(*os.File)
	if !ok {
		return false
	}

	old, err := term.MakeRaw(int(file.Fd()))
	if err != nil {
		return false
	}
	defer term.Restore(int(file.Fd()), old) //nolint:errcheck // the tty state is a courtesy

	done := make(chan bool, 1)

	go func() {
		buf := make([]byte, 1)
		_, err := file.Read(buf)
		done <- err == nil && buf[0] != 'q' && buf[0] != 0x1b && buf[0] != 0x03
	}()

	select {
	case <-ctx.Done():
		return false
	case next := <-done:
		return next
	}
}

// ttySize answers the terminal's cell size for the box the image is drawn in,
// falling back to /dev/tty and then to a plain 80x24 when neither descriptor
// is a terminal.
func ttySize(stdout io.Writer) (int, int) {
	if file, ok := stdout.(*os.File); ok {
		if ws, err := unix.IoctlGetWinsize(int(file.Fd()), unix.TIOCGWINSZ); err == nil {
			return int(ws.Col), int(ws.Row)
		}
	}

	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		defer tty.Close() //nolint:errcheck // closing /dev/tty reports nothing

		if ws, err := unix.IoctlGetWinsize(int(tty.Fd()), unix.TIOCGWINSZ); err == nil {
			return int(ws.Col), int(ws.Row)
		}
	}

	return fallbackCols, fallbackRows
}
