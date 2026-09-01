package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/kyleking/second-look/internal/conversations"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/resolve"
	"github.com/kyleking/second-look/internal/tui"
)

// errNotThisCheckout reports a reply to a conversation on a pull request this
// checkout is not standing on. The reply is staged into that pull request's own
// prepared review, which only exists in its own repository.
var errNotThisCheckout = errors.New("that pull request belongs to another repository")

// threadsScreen is the conversation queue on screen: read one, mark it read,
// resolve it, or hand off to the review screen to answer it.
type threadsScreen struct {
	ctx    context.Context //nolint:containedctx // it bounds the gh calls the keys make
	queue  *conversations.Queue
	looked *conversations.Looked
	path   string
	repo   string
	// buckets is a snapshot, taken when the queue is read and again on a
	// refresh. Which bucket a conversation is in turns on whether it has been
	// read, so recomputing it after every mark would move the row out from
	// under the cursor the moment you opened it.
	buckets []conversations.Bucket
	// reply is the pull request a reply was asked for, read back once the screen
	// closes. Staging an answer means opening the review screen, which cannot
	// happen while this one owns the terminal.
	reply int
}

// openThreads reads the queue, shows it, and writes the read marks back on the
// way out. The marks are saved once rather than on every keystroke: the file is
// user-level state that every checkout shares, and rewriting it under the
// cursor would make a crash mid-queue lose more than the keystroke in flight.
func openThreads(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	queue, err := conversations.Fetch(ctx, ".", conversations.DefaultLimit)
	if err != nil {
		return fmt.Errorf("reading your conversations: %w", err)
	}

	looked, err := loadLooked()
	if err != nil {
		return err
	}

	path, err := conversations.LookedPath()
	if err != nil {
		return fmt.Errorf("reading your read conversations: %w", err)
	}

	s := &threadsScreen{
		ctx: ctx, queue: queue, looked: looked, path: path, repo: currentRepo(ctx),
	}
	s.buckets = conversations.Buckets(queue, looked)

	list := tui.NewList("second-look conversations", s.sections, s.act).
		WithSubtitle(s.counts).
		WithHints(threadsHints).
		WithHelp(threadsHelp)

	_, runErr := tui.RunList(list)

	// The marks are worth keeping even when an action failed: what was read was
	// still read.
	if err := conversations.SaveLooked(s.path, s.looked, s.queue.Conversations); err != nil {
		return fmt.Errorf("saving what you read: %w", err)
	}

	if runErr != nil {
		return fmt.Errorf("reading your conversations: %w", runErr)
	}

	if s.reply > 0 {
		if err := write(stdout, fmt.Sprintf("opening #%d to stage the reply\n", s.reply)); err != nil {
			return err
		}

		return openStaged(ctx, s.reply, stdin, stdout)
	}

	return nil
}

// threadsHints is the footer, which advertises only the keys this screen offers.
var threadsHints = [][2]string{
	{"enter", "read"},
	{"space", "mark read"},
	{"r", "reply"},
	{"R", "resolve"},
	{"o", "GitHub"},
	{"tab", "group"},
	{"?", helpArg},
}

var threadsHelp = []string{
	"  j/k, ctrl+u/d, g/G   move, half page, top and bottom",
	"  tab                  the next group",
	"  enter                read the whole conversation, and mark it read",
	"  space                mark read without opening it",
	"  r                    leave and open the review screen to stage a reply",
	"  R                    thumbs-up it, and resolve the thread when there is one",
	"  o                    open it on GitHub",
	"  ctrl+r               read the queue again",
	"  q, esc               leave",
	"",
	"  ● marks a conversation that moved since you last read it.",
	"  A reply is staged into that pull request's prepared review and posts with it,",
	"  so r only works in the checkout the pull request belongs to. Standing on",
	"  another branch of it is fine: r moves the checkout, and asks first if that",
	"  would strand uncommitted work.",
}

// counts is the header's right-hand corner: how much is in the queue, and how
// much of it is still unread. The unread figure is live rather than from the
// snapshot, so it falls as rows are read.
func (s *threadsScreen) counts() string {
	unread := 0

	for i := range s.queue.Conversations {
		if !s.looked.Since(&s.queue.Conversations[i]) {
			unread++
		}
	}

	return fmt.Sprintf("%d conversations · %d unread", len(s.queue.Conversations), unread)
}

// sections turns the snapshot into rows. The unread mark is read live, so
// marking one clears its mark where it sits rather than moving it.
func (s *threadsScreen) sections() []tui.Section {
	now := time.Now()

	out := make([]tui.Section, 0, len(s.buckets))

	for i := range s.buckets {
		b := &s.buckets[i]
		rows := make([]tui.Row, 0, len(b.Items))

		for j := range b.Items {
			c := &b.Items[j]
			rows = append(rows, tui.Row{
				Key:    c.Key(),
				Left:   c.Where(),
				Mid:    c.Anchor(),
				Age:    humanize.Ago(c.Updated(), now),
				Tail:   tail(c),
				Under:  humanize.FirstLine(c.Last().Body),
				Unread: !s.looked.Since(c),
				Detail: detail(c),
			})
		}

		out = append(out, tui.Section{Name: b.Name, Rows: rows})
	}

	return out
}

// tail is whose turn it is and what the pull request is called. The author of
// the last comment comes first, because a thread a person last touched is a
// different obligation from one a bot last touched.
func tail(c *conversations.Conversation) string {
	const authorCap = 16

	var b strings.Builder

	b.WriteString(humanize.Clip(c.Last().Author, authorCap))

	if n := len(c.Notes); n > 1 {
		b.WriteString("  " + strconv.Itoa(n) + " replies")
	}

	if c.Outdated {
		b.WriteString("  outdated")
	}

	b.WriteString("  " + c.Title)

	return b.String()
}

// detail is the whole conversation, shown when a row is expanded. Every comment
// is there in order: the point of the queue is answering what was said, and the
// first line of the last one is rarely enough to do that.
func detail(c *conversations.Conversation) []string {
	var out []string

	for i := range c.Notes {
		n := &c.Notes[i]

		out = append(out, n.Author+":")

		for line := range strings.SplitSeq(strings.ReplaceAll(n.Body, "\r\n", "\n"), "\n") {
			out = append(out, "  "+line)
		}
	}

	return out
}

// find is the conversation a row stands for.
func (s *threadsScreen) find(key string) *conversations.Conversation {
	for i := range s.queue.Conversations {
		if s.queue.Conversations[i].Key() == key {
			return &s.queue.Conversations[i]
		}
	}

	return nil
}

func (s *threadsScreen) act(a tui.Action, row *tui.Row) (string, bool, error) {
	c := s.find(row.Key)
	if c == nil {
		return "", false, fmt.Errorf("%w: %s", errUnknownRow, row.Key)
	}

	switch a {
	case tui.ActMark, tui.ActChoose:
		s.looked.Mark(c, time.Now())

		return "read " + c.Where() + " " + c.Anchor(), false, nil
	case tui.ActBrowse:
		return s.browse(c)
	case tui.ActResolve:
		return s.resolve(c)
	case tui.ActReply:
		return s.stageReply(c)
	case tui.ActRefresh:
		return s.refresh()
	}

	return "", false, nil
}

func (s *threadsScreen) browse(c *conversations.Conversation) (string, bool, error) {
	if err := resolve.GH().Run(s.ctx, ".", "browse", "--repo", c.Repository, "-n", strconv.Itoa(c.Number)); err != nil {
		return "", false, fmt.Errorf("opening %s: %w", c.Where(), err)
	}

	return "opened " + c.Where(), false, nil
}

// resolve marks the conversation dealt with on GitHub and drops it from the
// queue, since the queue is what is still open and this no longer is.
func (s *threadsScreen) resolve(c *conversations.Conversation) (string, bool, error) {
	status, err := resolve.Run(s.ctx, resolve.GH(), ".", c)
	if err != nil {
		return "", false, fmt.Errorf("marking %s dealt with: %w", c.Where(), err)
	}

	s.drop(c.Key())

	return status, false, nil
}

// drop takes a conversation out of the queue and out of the snapshot, which is
// what a resolve leaves behind: the queue is what is still open, and this is not.
func (s *threadsScreen) drop(key string) {
	kept := make([]conversations.Conversation, 0, len(s.queue.Conversations))

	for i := range s.queue.Conversations {
		if s.queue.Conversations[i].Key() != key {
			kept = append(kept, s.queue.Conversations[i])
		}
	}

	s.queue.Conversations = kept

	for i := range s.buckets {
		rows := make([]conversations.Conversation, 0, len(s.buckets[i].Items))

		for j := range s.buckets[i].Items {
			if s.buckets[i].Items[j].Key() != key {
				rows = append(rows, s.buckets[i].Items[j])
			}
		}

		s.buckets[i].Items = rows
	}
}

// stageReply hands off to the review screen, which is where a reply is written
// and staged into the prepared review. Doing it here would mean a second editor
// flow and a second copy of the anchor rules, and the review screen already
// answers a thread with e.
func (s *threadsScreen) stageReply(c *conversations.Conversation) (string, bool, error) {
	if c.Kind != conversations.KindThread {
		return "", false, fmt.Errorf("%s: %w", c.Anchor(), errNotAThread)
	}

	if s.repo == "" || !strings.EqualFold(s.repo, c.Repository) {
		return "", false, fmt.Errorf("%s: %w; run second-look get %d there",
			c.Where(), errNotThisCheckout, c.Number)
	}

	s.reply = c.Number
	s.looked.Mark(c, time.Now())

	return "opening #" + strconv.Itoa(c.Number), true, nil
}

var errNotAThread = errors.New("only an inline thread takes a threaded reply; " +
	"answer a comment or a review body on GitHub, or resolve it with R")

func (s *threadsScreen) refresh() (string, bool, error) {
	queue, err := conversations.Fetch(s.ctx, ".", conversations.DefaultLimit)
	if err != nil {
		return "", false, fmt.Errorf("reading your conversations: %w", err)
	}

	s.queue = queue
	s.buckets = conversations.Buckets(queue, s.looked)

	return s.counts(), false, nil
}
