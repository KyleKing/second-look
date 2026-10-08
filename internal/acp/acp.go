// Package acp runs one agent over the Agent Client Protocol: a subprocess
// speaking JSON-RPC on stdio, so one configured argv reaches Devin's own acp
// command, Claude through its adapter, or whatever else speaks the wire.
//
// The session is a channel rather than a stream: updates land on the
// transcript and permission asks surface as a Permit, and the screen re-reads
// on Changed rather than being called back, the same hand-off the pty pane
// uses.
package acp

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	wire "github.com/caelis-labs/acp-go-sdk"
)

// ErrBusy is a prompt sent while a turn is still running. A second one is
// refused rather than queued because what the agent does with it is its own.
var ErrBusy = errors.New("a turn is still running")

// ErrNoAdapter is a Start asked to run nothing: no agent was configured.
var ErrNoAdapter = errors.New("no adapter to run")

// Entry is one block of the transcript the pane draws. Tool entries carry a
// Status word the agent reports; the other kinds leave it empty.
type Entry struct {
	Kind   EntryKind
	Text   string
	Status string
}

// EntryKind is which voice a transcript line speaks in.
type EntryKind int

const (
	// You is a prompt the reviewer sent.
	You EntryKind = iota
	// Agent is what the agent said.
	Agent
	// Thought is the agent's reasoning, where it shares it.
	Thought
	// Tool is a call it made, keeping the id so its updates patch in place.
	Tool
	// Plan is its task list, replaced whole on every update.
	Plan
	// Note is the client speaking for itself: a session that would not load,
	// an ended run. It is not something the agent said.
	Note
)

// Permit is an agent waiting on permission to run a tool call. Answer carries
// the picked option's id; an empty one tells the agent the ask was canceled.
type Permit struct {
	Title   string
	Tool    string
	Options []Option
	answer  chan string
}

// Option is one of the choices an agent offered with its ask. Kind is the
// wire's own word for it — allow_once, allow_always, reject_once — which is
// what a decline picks out of the list rather than canceling the turn.
type Option struct {
	ID   string
	Name string
	Kind string
}

// Answer gives the option with this id, where the agent offered it.
func (p *Permit) Answer(id string) { p.answer <- id }

// Decline picks the first reject the agent offered, or cancels the ask where
// it offered none.
func (p *Permit) Decline() {
	for _, o := range p.Options {
		if strings.HasPrefix(o.Kind, "reject") {
			p.answer <- o.ID

			return
		}
	}

	p.answer <- ""
}

// Session is one live conversation: the adapter subprocess, the connection
// over its stdio, and the transcript the screen draws.
type Session struct {
	cmd  *exec.Cmd
	conn *wire.ClientSideConnection

	mu      sync.Mutex
	id      wire.SessionId
	name    string
	canLoad bool
	busy    bool
	closing bool
	lines   []Entry
	seen    map[string]int
	permit  *Permit
	stderr  tail

	woke chan struct{}
	dead chan error
}

// Start runs argv, initializes the connection, and opens a session in cwd —
// loading the one named by loadID where the agent supports it and starting a
// new one where it does not. An agent whose auth the host does not manage
// finds its own credentials, the way `devin acp` reads `devin auth login`'s.
func Start(ctx context.Context, argv []string, cwd, loadID string) (*Session, error) {
	if len(argv) == 0 {
		return nil, ErrNoAdapter
	}

	s := &Session{
		seen: map[string]int{},
		woke: make(chan struct{}, 1),
		dead: make(chan error, 1),
	}

	//nolint:gosec // the argv is the caller's own config
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Stderr = &s.stderr

	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("opening stdin on %s: %w", argv[0], err)
	}

	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("opening stdout on %s: %w", argv[0], err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", argv[0], err)
	}

	s.cmd = cmd
	s.conn = wire.NewClientSideConnection(s, in, out)

	res, err := s.conn.Initialize(ctx, wire.InitializeRequest{
		ProtocolVersion: wire.ProtocolVersionNumber,
		ClientInfo:      &wire.Implementation{Name: "second-look"},
	})
	if err != nil {
		s.Close()

		return nil, fmt.Errorf("initializing %s: %w", argv[0], err)
	}

	s.name = argv[0]
	if res.AgentInfo != nil && res.AgentInfo.Name != "" {
		s.name = res.AgentInfo.Name
	}
	s.canLoad = res.AgentCapabilities.LoadSession

	if loadID != "" && s.canLoad {
		if _, err := s.conn.LoadSession(ctx, wire.LoadSessionRequest{
			SessionId:  wire.SessionId(loadID),
			Cwd:        cwd,
			McpServers: []wire.McpServer{},
		}); err == nil {
			s.id = wire.SessionId(loadID)
		} else {
			s.note("the recorded session would not load; this is a new one")
		}
	}

	if s.id == "" {
		made, err := s.conn.NewSession(ctx, wire.NewSessionRequest{
			Cwd:        cwd,
			McpServers: []wire.McpServer{},
		})
		if err != nil {
			s.Close()

			return nil, fmt.Errorf("opening a session: %w", err)
		}
		s.id = made.SessionId
	}

	go s.reap()

	return s, nil
}

// Prompt sends text and waits out the turn, appending a You line as it goes.
// The updates it streamed are already on the transcript; what comes back is
// the agent's stop reason.
func (s *Session) Prompt(ctx context.Context, text string) (string, error) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()

		return "", ErrBusy
	}
	s.busy = true
	s.lines = append(s.lines, Entry{Kind: You, Text: text})
	s.mu.Unlock()
	s.wake()

	defer func() {
		s.mu.Lock()
		s.busy = false
		s.mu.Unlock()
		s.wake()
	}()

	res, err := s.conn.Prompt(ctx, wire.PromptRequest{
		SessionId: s.id,
		Prompt:    []wire.ContentBlock{wire.TextBlock(text)},
	})
	if err != nil {
		return "", fmt.Errorf("prompting the agent: %w", err)
	}

	return string(res.StopReason), nil
}

// Cancel asks the agent to stop the turn in flight.
func (s *Session) Cancel(ctx context.Context) error {
	if err := s.conn.Cancel(ctx, wire.CancelNotification{SessionId: s.id}); err != nil {
		return fmt.Errorf("canceling the turn: %w", err)
	}

	return nil
}

// Close ends the session and the adapter. A dead notification may still be
// read, reporting nothing.
func (s *Session) Close() {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()

	if s.conn != nil {
		//nolint:errcheck // closed is the wanted state either way
		_ = s.conn.Close()
	}

	if s.cmd != nil && s.cmd.Process != nil {
		//nolint:errcheck // same
		_ = s.cmd.Process.Kill()
	}
}

// ID is the session's id, which a review records so the next open can load
// it back.
func (s *Session) ID() string { return string(s.id) }

// Name is the agent's own name, or the adapter's argv head where it did not
// give one.
func (s *Session) Name() string { return s.name }

// Busy reports a turn in flight.
func (s *Session) Busy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.busy
}

// Entries is the transcript so far. The copy is the point: the pane reads it
// while the agent keeps writing.
func (s *Session) Entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]Entry(nil), s.lines...)
}

// Pending is the permission ask waiting on an answer, or none.
func (s *Session) Pending() *Permit {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.permit
}

// Changed fires when there is something new to draw, coalesced to one pending
// signal because a wake redraws the whole transcript anyway.
func (s *Session) Changed() <-chan struct{} { return s.woke }

// Dead carries the adapter's end once: nil for a clean exit or a deliberate
// Close, the failure otherwise.
func (s *Session) Dead() <-chan error { return s.dead }

// SessionUpdate is the agent streaming into the transcript. It is fast on
// purpose: the notification queue it runs on is bounded.
func (s *Session) SessionUpdate(_ context.Context, n wire.SessionNotification) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch u := n.Update; {
	case u.AgentMessageChunk != nil:
		s.append("agent:"+mid(u.AgentMessageChunk.MessageId), Agent, blockText(u.AgentMessageChunk.Content))
	case u.AgentThoughtChunk != nil:
		s.append("thought:"+mid(u.AgentThoughtChunk.MessageId), Thought, blockText(u.AgentThoughtChunk.Content))
	case u.ToolCall != nil:
		s.tool(string(u.ToolCall.ToolCallId), toolTitle(u.ToolCall.Name, u.ToolCall.Title), string(u.ToolCall.Status))
	case u.ToolCallUpdate != nil:
		title := ""
		if u.ToolCallUpdate.Title != nil {
			title = *u.ToolCallUpdate.Title
		}

		status := ""
		if u.ToolCallUpdate.Status != nil {
			status = string(*u.ToolCallUpdate.Status)
		}

		s.tool(string(u.ToolCallUpdate.ToolCallId), title, status)
	case u.Plan != nil:
		s.plan(u.Plan.Entries)
	}

	s.wake()

	return nil
}

// RequestPermission is the agent asking before it runs a tool call. The ask
// publishes as a Permit and the reply is whoever answers it — the pane, or a
// canceled turn if the ask outlives the connection.
func (s *Session) RequestPermission(
	ctx context.Context, req wire.RequestPermissionRequest,
) (wire.RequestPermissionResponse, error) {
	p := &Permit{answer: make(chan string, 1)}
	if req.ToolCall.Title != nil {
		p.Title = *req.ToolCall.Title
	}
	if req.ToolCall.Name != nil {
		p.Tool = *req.ToolCall.Name
	}

	for _, o := range req.Options {
		p.Options = append(p.Options, Option{ID: string(o.OptionId), Name: o.Name, Kind: string(o.Kind)})
	}

	s.mu.Lock()
	s.permit = p
	s.mu.Unlock()
	s.wake()

	var id string

	select {
	case id = <-p.answer:
	case <-ctx.Done():
	}

	s.mu.Lock()
	s.permit = nil
	s.mu.Unlock()
	s.wake()

	if id == "" {
		return wire.RequestPermissionResponse{Outcome: wire.NewRequestPermissionOutcomeCancelled()}, nil
	}

	return wire.RequestPermissionResponse{
		Outcome: wire.NewRequestPermissionOutcomeSelected(wire.PermissionOptionId(id)),
	}, nil
}

// append grows the entry keyed by id, or opens it. Message chunks carry their
// own id so a streamed reply lands in one block rather than a line a chunk.
func (s *Session) append(id string, kind EntryKind, text string) {
	if at, ok := s.seen[id]; ok {
		s.lines[at].Text += text

		return
	}

	s.seen[id] = len(s.lines)
	s.lines = append(s.lines, Entry{Kind: kind, Text: text})
}

// tool opens or patches a tool entry: the id says which, and an empty field
// keeps what was there because an update that omits it is leaving it alone.
func (s *Session) tool(id, title, status string) {
	at, ok := s.seen["tool:"+id]
	if !ok {
		at = len(s.lines)
		s.seen["tool:"+id] = at
		s.lines = append(s.lines, Entry{Kind: Tool})
	}

	if title != "" {
		s.lines[at].Text = title
	}

	if status != "" {
		s.lines[at].Status = status
	}
}

// plan replaces the plan entry whole, the same way the wire replaces it.
func (s *Session) plan(entries []wire.PlanEntry) {
	var b strings.Builder

	for _, e := range entries {
		fmt.Fprintf(&b, "%s %s\n", e.Status, e.Content)
	}

	if at, ok := s.seen["plan"]; ok {
		s.lines[at].Text = strings.TrimRight(b.String(), "\n")

		return
	}

	s.seen["plan"] = len(s.lines)
	s.lines = append(s.lines, Entry{Kind: Plan, Text: strings.TrimRight(b.String(), "\n")})
}

func (s *Session) note(text string) {
	s.lines = append(s.lines, Entry{Kind: Note, Text: text})
	s.wake()
}

func (s *Session) wake() {
	select {
	case s.woke <- struct{}{}:
	default:
	}
}

// reap reports the adapter's end once: a clean exit or a deliberate Close
// reads as nil, a crash carries what its stderr last said.
func (s *Session) reap() {
	err := s.cmd.Wait()
	<-s.conn.Done()

	s.mu.Lock()
	closing := s.closing
	s.mu.Unlock()

	switch {
	case closing:
		err = nil
	case err != nil && s.stderr.String() != "":
		err = fmt.Errorf("%w — %s", err, s.stderr.String())
	case err == nil:
		err = s.conn.Err()
	}

	s.dead <- err
}

func mid(id *wire.MessageId) string {
	if id == nil {
		return ""
	}

	return string(*id)
}

func blockText(b wire.ContentBlock) string {
	if b.Text != nil {
		return b.Text.Text
	}

	return ""
}

// The agent's own title says more than its tool's name; the name is the
// fallback where the call carries no title.
func toolTitle(name *string, title string) string {
	if title != "" {
		return title
	}

	if name != nil {
		return *name
	}

	return ""
}

// tailCap is how much stderr is kept: enough for the message a crash leaves
// and no more.
const tailCap = 4096

// tail is the last few kilobytes of the adapter's stderr, kept so a crash
// can say why rather than report a bare exit.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.buf = append(t.buf, p...)
	if len(t.buf) > tailCap {
		t.buf = t.buf[len(t.buf)-tailCap:]
	}

	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return strings.TrimSpace(string(t.buf))
}
