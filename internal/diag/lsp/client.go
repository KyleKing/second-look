// Package lsp asks a language server what it makes of the files under review.
//
// The server is the one already installed for the editor, started as a
// subprocess and spoken to over its stdio. That is the same bargain the tool
// makes with gh, git, ast-grep, and $EDITOR: it works where the program is
// installed, and where it is not the review is what it was before.
//
// A file is opened with the text the caller hands over rather than read from
// disk, which is what the protocol's didOpen means by an unsaved buffer. The
// working tree is never written to, and a checkout left on another branch can
// still be asked about the commit under review.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// ErrClosed reports a server that has gone away, which is what every call in
// flight is answered with once its process exits.
var ErrClosed = errors.New("the language server is not running")

// errNoLength reports a message with no Content-Length header, which is a
// stream that is not the protocol rather than a message to skip.
var errNoLength = errors.New("a frame arrived with no length")

// The envelope every message carries.
const (
	rpcKey     = "jsonrpc"
	rpcVersion = "2.0"
)

// frame is one JSON-RPC message, in the shape both directions take. The three
// pointers separate a null result from an absent one, which is how a response
// carrying nothing is told from a request.
type frame struct {
	ID     *json.RawMessage `json:"id,omitempty"`
	Method string           `json:"method,omitempty"`
	Params json.RawMessage  `json:"params,omitempty"`
	Result json.RawMessage  `json:"result,omitempty"`
	Error  *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

// client is one running server and the conversation with it.
//
// Every read happens on one goroutine, because a response and a notification
// arrive on the same pipe and a caller waiting for the first would otherwise
// have to decide what to do with the second.
type client struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	mu    sync.Mutex
	next  int
	waits map[int]chan frame
	// notes carries every publishDiagnostics the server sends, which arrive
	// unasked for and long after the call that caused them.
	notes  chan published
	closed chan struct{}
	once   sync.Once
	// settings is what a server asking for its configuration is answered with.
	// It is written before the server is spoken to and only read after, so the
	// reader goroutine needs no lock to answer from it.
	settings map[string]any
}

// published is one file's diagnostics as the server currently sees them. A
// server republishes the whole list for a file, so the last one received for a
// path is the answer rather than one to add to.
type published struct {
	URI         string       `json:"uri"`
	Diagnostics []diagnostic `json:"diagnostics"`
}

type diagnostic struct {
	Range    lspRange        `json:"range"`
	Severity int             `json:"severity"`
	Code     json.RawMessage `json:"code"`
	Source   string          `json:"source"`
	Message  string          `json:"message"`
}

type lspRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

// position is zero-based in both axes, which is the protocol's own counting and
// not a person's.
type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// dial starts a server and begins reading from it. The context bounds the
// process: canceling it kills the server, which is what closing a review does.
func dial(ctx context.Context, dir string, argv []string, settings map[string]any) (*client, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- a configured server command
	cmd.Dir = dir

	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("starting %s: %w", argv[0], err)
	}

	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("starting %s: %w", argv[0], err)
	}

	// A server's stderr is its log, which is several lines per keystroke for
	// some of them and belongs nowhere near a terminal drawing a review.
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", argv[0], err)
	}

	c := &client{
		cmd: cmd, in: in, waits: map[int]chan frame{},
		notes: make(chan published, notesBuffer), closed: make(chan struct{}),
		settings: settings,
	}

	go c.read(out)

	return c, nil
}

// notesBuffer is how many publishes may pile up before the reader would block.
// A server answers for every file it loaded, not only the ones asked about, so
// this is generous: a full buffer would stall the reader and with it every
// response behind it.
const notesBuffer = 256

// read routes every message the server sends until the pipe closes.
func (c *client) read(out io.Reader) {
	defer c.shut()

	// A server's initialize response carries every capability it has, which for
	// gopls is past the default limit.
	br := bufio.NewReaderSize(out, readBuffer)

	for {
		body, err := readFrame(br)
		if err != nil {
			return
		}

		var f frame
		if err := json.Unmarshal(body, &f); err != nil {
			continue
		}

		c.route(f)
	}
}

const readBuffer = 1 << 16

func (c *client) route(f frame) {
	switch {
	case f.ID != nil && f.Method == "workspace/configuration":
		c.answer(*f.ID, c.configuration(f.Params))
	case f.ID != nil && f.Method != "":
		// A request from the server. Answering nothing at all is what stalls
		// gopls, which waits on workspace/configuration before it loads a
		// package, so every one is answered and none is acted on.
		c.answer(*f.ID, nil)
	case f.ID != nil:
		c.deliver(f)
	case f.Method == "textDocument/publishDiagnostics":
		var p published
		if err := json.Unmarshal(f.Params, &p); err != nil {
			return
		}

		select {
		case c.notes <- p:
		default:
		}
	}
}

// deliver hands a response to whoever is waiting for it.
func (c *client) deliver(f frame) {
	var id int
	if err := json.Unmarshal(*f.ID, &id); err != nil {
		return
	}

	c.mu.Lock()
	ch, ok := c.waits[id]
	delete(c.waits, id)
	c.mu.Unlock()

	if ok {
		ch <- f
	}
}

// answer replies to a server's request. A reply that cannot be written means
// the pipe is gone, so the client shuts rather than leaving a server waiting on
// an answer that will never arrive.
func (c *client) answer(id json.RawMessage, result any) {
	if err := c.write(map[string]any{rpcKey: rpcVersion, "id": id, "result": result}); err != nil {
		c.shut()
	}
}

// configuration answers a server asking what it is configured with, one entry
// per item and in the order asked. A server that pulls its settings this way
// asks before it loads a package, which is why an unconfigured server is still
// answered: null is the protocol's word for "use your own default".
func (c *client) configuration(params json.RawMessage) []any {
	var p struct {
		Items []struct {
			Section string `json:"section"`
		} `json:"items"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil
	}

	out := make([]any, 0, len(p.Items))

	for _, item := range p.Items {
		out = append(out, section(c.settings, item.Section))
	}

	return out
}

// section walks a dotted path into the settings, which is how the protocol
// names one part of them.
func section(settings map[string]any, path string) any {
	var at any = settings

	if path == "" {
		return at
	}

	for _, key := range strings.Split(path, ".") {
		held, ok := at.(map[string]any)
		if !ok {
			return nil
		}

		if at, ok = held[key]; !ok {
			return nil
		}
	}

	return at
}

// call sends a request and waits for its response.
func (c *client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan frame, 1)
	c.waits[id] = ch
	c.mu.Unlock()

	err := c.write(map[string]any{rpcKey: rpcVersion, "id": id, "method": method, "params": params})
	if err != nil {
		c.mu.Lock()
		delete(c.waits, id)
		c.mu.Unlock()

		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: %w", method, ctx.Err())
	case <-c.closed:
		return nil, fmt.Errorf("%s: %w", method, ErrClosed)
	case f := <-ch:
		if f.Error != nil {
			return nil, fmt.Errorf("%s: %w", method, f.Error)
		}

		return f.Result, nil
	}
}

func (c *client) notify(method string, params any) error {
	return c.write(map[string]any{rpcKey: rpcVersion, "method": method, "params": params})
}

func (c *client) write(msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("encoding a request: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	select {
	case <-c.closed:
		return ErrClosed
	default:
	}

	if _, err := fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return fmt.Errorf("writing to the server: %w", err)
	}

	if _, err := c.in.Write(body); err != nil {
		return fmt.Errorf("writing to the server: %w", err)
	}

	return nil
}

// shut releases everyone waiting. It runs once however the server ended, so a
// crashed server answers its callers rather than hanging them.
func (c *client) shut() {
	c.once.Do(func() {
		close(c.closed)

		//nolint:errcheck // the pipe is being abandoned; a failed close changes nothing
		_ = c.in.Close()
	})
}

// close ends the server. The protocol's shutdown is skipped: the process is
// ours alone, it holds nothing that needs writing back, and a server that has
// stopped answering would make leaving a review wait for it.
func (c *client) close() {
	c.shut()

	//nolint:errcheck // killing an already-dead server is the expected case
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}

	//nolint:errcheck // what a killed server exited with is not an answer
	_ = c.cmd.Wait()
}

// readFrame reads one header-delimited message body.
func readFrame(br *bufio.Reader) ([]byte, error) {
	n := -1

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("reading from the server: %w", err)
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}

		name, value, ok := strings.Cut(line, ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "content-length") {
			continue
		}

		if n, err = strconv.Atoi(strings.TrimSpace(value)); err != nil {
			return nil, fmt.Errorf("reading a frame length: %w", err)
		}
	}

	if n < 0 {
		return nil, errNoLength
	}

	body := make([]byte, n)
	if _, err := io.ReadFull(br, body); err != nil {
		return nil, fmt.Errorf("reading a frame: %w", err)
	}

	return body, nil
}
