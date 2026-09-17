package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kyleking/second-look/internal/diag"
)

// How long a pass waits on servers.
//
// The floor is not a delay to be tuned away. A server that answers the instant
// a file opens is usually answering before it has loaded the project, and the
// answer that matters arrives after it has, so a quiet line before the floor is
// not an answer. The settle is how long a quiet line has to stay quiet after
// that, and the deadline bounds a server that never settles at all.
const (
	warm     = 2 * time.Second
	settle   = time.Second
	deadline = 30 * time.Second
)

// The two keys the protocol spells everywhere.
const (
	uriKey      = "uri"
	documentKey = "textDocument"
)

// Doc is one file as it reads after the change.
//
// The text travels rather than being read from disk, because a review is of a
// commit and a checkout is of whatever branch it is on. What the server resolves
// around it still comes from disk, which is the limit this ships with: imports
// are answered by the tree as it stands.
type Doc struct {
	Path string
	Text string
}

// project is one running server: a language server and the directory it was
// started in. A monorepo holds several projects one server speaks for, and a
// file is answered by the one it belongs to.
type project struct {
	name string
	root string
}

// Session is the servers a review has running, started on demand and kept until
// the review is left.
//
// One server per language for the whole review, because a language server pays
// for a project once: the first file cost about three seconds against a
// TypeScript monorepo and every file after it a fraction of one.
type Session struct {
	// base bounds every server started here. A server is a child of this
	// process and outlives it if nobody ends it, so canceling this reaps them
	// even where Close is never reached.
	base    context.Context //nolint:containedctx // it bounds the server processes, not one call
	cancel  context.CancelFunc
	root    string
	servers []Server

	mu      sync.Mutex
	running map[project]*client
	// opened is every document a server has been told about, so a second pass
	// over the same file changes it rather than opening it twice.
	opened map[string]int
}

// New prepares a session rooted at a checkout. Nothing starts until something
// is asked.
func New(ctx context.Context, root string, servers []Server) *Session {
	base, cancel := context.WithCancel(context.WithoutCancel(ctx))

	// A server is told where its project is as an absolute path, and a caller
	// standing in the checkout names it as ".".
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}

	return &Session{
		base: base, cancel: cancel, root: root, servers: servers,
		running: map[project]*client{}, opened: map[string]int{},
	}
}

// Close ends every server.
func (s *Session) Close() {
	s.cancel()

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, c := range s.running {
		c.close()
	}

	clear(s.running)
	clear(s.opened)
}

// Notes is what every server has to say about these documents.
//
// A server that fails is reported and does not stop the others: one language of
// a polyglot change going unanswered is worth saying, and is not worth losing
// the rest of the pass over.
func (s *Session) Notes(ctx context.Context, docs []Doc) ([]diag.Note, error) {
	groups := map[project][]Doc{}
	known := map[string]Server{}

	for _, d := range docs {
		srv, ok := pick(s.servers, d.Path)
		if !ok {
			continue
		}

		at := project{name: srv.Name, root: srv.rootFor(s.root, d.Path)}
		known[srv.Name] = srv
		groups[at] = append(groups[at], d)
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		out  []diag.Note
		errs []error
	)

	// Every project waits out the same floor, so asking them one after another
	// costs that wait once per project: a change touching four packages of a
	// monorepo would take four times as long to say the same thing.
	for at, group := range groups {
		wg.Add(1)

		go func() {
			defer wg.Done()

			notes, err := s.ask(ctx, known[at.name], at.root, group)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				errs = append(errs, err)

				return
			}

			out = append(out, notes...)
		}()
	}

	wg.Wait()

	return out, errors.Join(errs...)
}

// ask opens every document one server answers for and collects what it
// publishes about them.
func (s *Session) ask(ctx context.Context, srv Server, root string, docs []Doc) ([]diag.Note, error) {
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	c, err := s.serverFor(ctx, srv, root)
	if err != nil {
		return nil, err
	}

	want := map[string]string{}

	for _, d := range docs {
		uri := s.uri(d.Path)
		want[uri] = d.Path

		if err := s.open(c, srv, d.Path, uri, d.Text); err != nil {
			return nil, fmt.Errorf("%s: %w", srv.Name, err)
		}
	}

	return collect(ctx, c, srv.Name, want), nil
}

// collect reads publishes until every file has answered and the line has gone
// quiet, or until the deadline. A server republishes a file's whole list, so
// the last word for a path replaces the one before it.
func collect(ctx context.Context, c *client, source string, want map[string]string) []diag.Note {
	latest := map[string][]diagnostic{}

	// Two timers rather than one clock read: floor is the wait before a quiet
	// line means anything, and quiet is that line. Go 1.23 timers drain
	// themselves on Stop and Reset, so both are reused rather than remade.
	floor, quiet := time.NewTimer(warm), time.NewTimer(settle)
	defer floor.Stop()
	defer quiet.Stop()
	quiet.Stop()

	var warmed, armed bool

	// arm starts the quiet line, and is a no-op until the floor has passed and
	// every file asked about has answered.
	arm := func() {
		if armed = warmed && answered(latest, want); armed {
			quiet.Reset(settle)
		}
	}

	for {
		var ticking <-chan time.Time
		if armed {
			ticking = quiet.C
		}

		select {
		case <-ctx.Done():
			return notesOf(latest, want, source)
		case <-c.closed:
			return notesOf(latest, want, source)
		case <-ticking:
			return notesOf(latest, want, source)
		case <-floor.C:
			warmed = true

			arm()
		case p := <-c.notes:
			// A server publishes for every file it loaded on the way to the
			// ones asked about, and a monorepo never stops. Only a file this
			// pass asked about restarts the quiet line.
			if _, ours := want[p.URI]; !ours {
				continue
			}

			quiet.Stop()

			latest[p.URI] = p.Diagnostics

			arm()
		}
	}
}

// answered reports whether every file asked about has been published for. A
// server answers for files nobody asked about too, which is why this counts the
// asked-for ones rather than the arrivals.
func answered(latest map[string][]diagnostic, want map[string]string) bool {
	for uri := range want {
		if _, ok := latest[uri]; !ok {
			return false
		}
	}

	return true
}

func notesOf(latest map[string][]diagnostic, want map[string]string, source string) []diag.Note {
	var out []diag.Note

	for uri, ds := range latest {
		path, ok := want[uri]
		if !ok {
			continue
		}

		for _, d := range ds {
			out = append(out, diag.Note{
				Path:     path,
				Line:     d.Range.Start.Line + 1,
				End:      d.Range.End.Line + 1,
				Source:   named(d.Source, source),
				Code:     codeOf(d.Code),
				Message:  strings.TrimSpace(d.Message),
				Severity: severityOf(d.Severity),
			})
		}
	}

	return out
}

func named(from, fallback string) string {
	if from != "" {
		return from
	}

	return fallback
}

// codeOf reads a rule name that the protocol allows to be a string or a number.
func codeOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return strconv.Itoa(n)
	}

	return ""
}

// The protocol's own severity scale, which counts from one and in the opposite
// direction to the words.
const (
	lspError = iota + 1
	lspWarning
	lspInfo
	lspHint
)

// severityOf maps the protocol's scale. A diagnostic carrying none is a
// warning, which is the protocol's own reading of an absent severity.
func severityOf(n int) diag.Severity {
	switch n {
	case lspError:
		return diag.Error
	case lspWarning:
		return diag.Warning
	case lspInfo:
		return diag.Info
	case lspHint:
		return diag.Hint
	}

	return diag.Warning
}

// serverFor is the running server for a language, started and initialized on
// first use.
func (s *Session) serverFor(ctx context.Context, srv Server, root string) (*client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	at := project{name: srv.Name, root: root}

	if c, ok := s.running[at]; ok {
		select {
		case <-c.closed:
			delete(s.running, at)
		default:
			return c, nil
		}
	}

	// The process outlives the call that started it, so it is bounded by the
	// session rather than by one pass's deadline.
	//nolint:contextcheck // the server is the session's, not this call's
	c, err := dial(s.base, root, srv.Argv, srv.Settings)
	if err != nil {
		return nil, err
	}

	if err := handshake(ctx, c, root); err != nil {
		c.close()

		return nil, fmt.Errorf("%s: %w", srv.Name, err)
	}

	s.running[at] = c

	return c, nil
}

func handshake(ctx context.Context, c *client, root string) error {
	_, err := c.call(ctx, "initialize", map[string]any{
		"processId": os.Getpid(),
		// rootPath beside rootUri, because a server that resolves its own
		// toolchain out of the workspace (tsserver finding typescript) reads
		// the deprecated one and exits where it is absent.
		"rootUri":  fileURI(root),
		"rootPath": root,
		"workspaceFolders": []map[string]any{
			{uriKey: fileURI(root), "name": filepath.Base(root)},
		},
		"capabilities": map[string]any{
			documentKey: map[string]any{
				"publishDiagnostics": map[string]any{},
				"hover":              map[string]any{"contentFormat": []string{"plaintext", "markdown"}},
			},
			"workspace": map[string]any{"workspaceFolders": true},
		},
	})
	if err != nil {
		return err
	}

	if err := c.notify("initialized", map[string]any{}); err != nil {
		return err
	}

	if len(c.settings) == 0 {
		return nil
	}

	return c.notify("workspace/didChangeConfiguration", map[string]any{"settings": c.settings})
}

// open tells a server about a document, or about a new version of one it
// already holds.
func (s *Session) open(c *client, srv Server, path, uri, text string) error {
	s.mu.Lock()
	version := s.opened[uri] + 1
	s.opened[uri] = version
	s.mu.Unlock()

	if version > 1 {
		return c.notify("textDocument/didChange", map[string]any{
			documentKey:      map[string]any{uriKey: uri, "version": version},
			"contentChanges": []map[string]any{{"text": text}},
		})
	}

	return c.notify("textDocument/didOpen", map[string]any{
		documentKey: map[string]any{
			uriKey: uri, "languageId": srv.languageID(path), "version": version, "text": text,
		},
	})
}

// uri is a path as the protocol spells it, which is an absolute file URL.
func (s *Session) uri(path string) string {
	full := s.root
	if path != "" {
		full = filepath.Join(s.root, path)
	}

	return fileURI(full)
}

func fileURI(full string) string {
	return (&url.URL{Scheme: "file", Path: full}).String()
}
