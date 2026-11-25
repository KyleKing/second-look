package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/diff"
	"github.com/kyleking/aragonite/seen"

	"github.com/kyleking/second-look/internal/humanize"
)

// Sharer is another tool's record of which hunks have been read, kept beside the review's own:
// jojo's per-change store, which jj-diff reads too. Import lays what it holds over the review's set
// without it reaching the review's own file, and answers how many hunks of live it newly shows read;
// Export writes back what the review changed since. The screen exports before each save and imports
// again after, since a save keeps only the review's own marks.
type Sharer interface {
	Import(s *seen.Set, live []seen.Ref) (int, error)
	Export(s *seen.Set) error
}

// WithShared keeps the review's read marks in step with s: the screen imports what s holds as it
// opens, and every save writes what changed through to it.
func WithShared(s Sharer) Option {
	return func(m *Model) { m.shared = s }
}

// SharedReader finds the other tool's record for the change at head behind the first frame, nil
// with no error where there is none, such as a pull request whose head no jj change in the checkout
// holds.
type SharedReader func(ctx context.Context, head string, d *diff.Diff) (Sharer, error)

// WithSharedReader looks for a record to share once the frame is up, which for a pull request is a
// jj call per change it spans.
func WithSharedReader(r SharedReader) Option {
	return func(m *Model) { m.sharedReader = r }
}

// sharedMsg is what looking for a shared record answered, for the diff it was asked about.
type sharedMsg struct {
	sharer Sharer
	diff   *diff.Diff
	err    error
}

func (m *Model) readShared() tea.Cmd {
	if m.sharedReader == nil || m.read == nil {
		return nil
	}

	read, ctx, head, d := m.sharedReader, m.ctx, m.review.HeadSHA, m.diff

	return func() tea.Msg {
		s, err := read(ctx, head, d)

		return sharedMsg{sharer: s, diff: d, err: err}
	}
}

// applyShared takes the record the reader found. An answer about a diff the screen has since replaced
// is dropped.
func (m *Model) applyShared(msg sharedMsg) {
	if msg.diff != m.diff {
		return
	}

	if msg.err != nil {
		m.say("reading jojo's read marks: "+msg.err.Error(), true)

		return
	}

	if m.takeShared(msg.sharer) > 0 {
		m.rebuild()
	}
}

// takeShared imports s's marks into the review's and keeps s for every later save, saying how many
// it brought in or why it could not, and answers how many.
func (m *Model) takeShared(s Sharer) int {
	m.shared = s
	if s == nil || m.read == nil {
		return 0
	}

	n, err := s.Import(m.read, m.allHunks())

	switch {
	case err != nil:
		m.say("reading jojo's read marks: "+err.Error(), true)
	case n > 0:
		m.say(humanize.Plural(n, "hunk")+" marked read in jojo", false)
	}

	return n
}

// reshare takes the record a restage brought, or where it brought none looks for one again, since a
// pull request's new head can span other changes.
func (m *Model) reshare(fresh *Restaged) tea.Cmd {
	if fresh.Shared == nil {
		m.shared = nil

		return m.readShared()
	}

	m.takeShared(fresh.Shared)

	return nil
}

// exportRead writes the review's read marks through to the shared record before the review's own
// save, which would drop what the record laid over them, and answers why it could not.
func (m *Model) exportRead() error {
	if m.shared == nil {
		return nil
	}

	if err := m.shared.Export(m.read); err != nil {
		return fmt.Errorf("exporting read marks: %w", err)
	}

	return nil
}

// reimport lays the shared record over the review's marks again after a save took it off, and says
// why the export before it or this could not, where either failed.
func (m *Model) reimport(exported error) {
	if m.shared == nil {
		return
	}

	_, err := m.shared.Import(m.read, m.allHunks())

	switch {
	case exported != nil:
		m.say("writing read marks for jojo: "+exported.Error(), true)
	case err != nil:
		m.say("reading jojo's read marks: "+err.Error(), true)
	}
}
