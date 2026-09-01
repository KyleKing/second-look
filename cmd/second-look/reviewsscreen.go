package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/prepared"
	"github.com/kyleking/second-look/internal/tui"
)

// errUnknownRow reports a row the screen no longer has data for, which is a
// fault in the screen rather than anything a person can act on.
var (
	errUnknownRow = errors.New("that row is no longer in the list")
	errUnreadable = errors.New("the file cannot be read")
)

// reviewsScreen lists what is staged under .second-look and opens one.
//
// The artifact is deleted the moment a review posts, so every row is unfinished
// work: either the review being written, or one whose head has since moved and
// which will refuse to post until it is prepared again.
type reviewsScreen struct {
	ctx  context.Context //nolint:containedctx // it bounds the reread a refresh makes
	rows []prepared.Review
	open int
}

var reviewsHelp = []string{
	"  j/k, ctrl+u/d, g/G   move, half page, top and bottom",
	"  enter                open the review screen for it",
	"  ctrl+r               read the directory again",
	"  q, esc               leave",
	"",
	"  blocked means a comment is still a draft, which stops the submit.",
	"  Every review here is unfinished: the file is deleted when it posts.",
}

// openReviews shows the staged reviews and opens whichever one was chosen, once
// this screen has given the terminal back.
func openReviews(ctx context.Context, stdout io.Writer) error {
	rows, err := prepared.List(".")
	if err != nil && !errors.Is(err, prepared.ErrNoDir) {
		return fmt.Errorf("listing the staged reviews: %w", err)
	}

	s := &reviewsScreen{ctx: ctx, rows: rows}

	list := tui.NewList("second-look staged reviews", s.sections, s.act).
		WithSubtitle(s.counts).
		WithHints([][2]string{{"enter", "open"}, {"ctrl+r", "refresh"}, {"?", helpArg}}).
		WithHelp(reviewsHelp)

	if _, err := tui.RunList(list); err != nil {
		return fmt.Errorf("listing the staged reviews: %w", err)
	}

	if s.open > 0 {
		return openReview(ctx, s.open, stdout)
	}

	return nil
}

func (s *reviewsScreen) counts() string {
	blocked := 0
	for i := range s.rows {
		if s.rows[i].Blocked() {
			blocked++
		}
	}

	if blocked == 0 {
		return strconv.Itoa(len(s.rows)) + " staged"
	}

	return fmt.Sprintf("%d staged · %d blocked", len(s.rows), blocked)
}

// sections is one group, because a handful of staged reviews sorted by recency
// needs no buckets and inventing some would be noise.
func (s *reviewsScreen) sections() []tui.Section {
	now := time.Now()
	rows := make([]tui.Row, 0, len(s.rows))

	for i := range s.rows {
		r := &s.rows[i]
		rows = append(rows, tui.Row{
			Key:  strconv.Itoa(r.Number),
			Left: r.Where(),
			Mid:  prepared.State(r),
			Age:  humanize.Ago(r.Modified, now),
			Tail: holds(r),
			// A review with a draft in it is the one to come back to, which is
			// what the unread mark means on this screen.
			Unread: r.Blocked() || r.Broken != "",
		})
	}

	return []tui.Section{{Name: "staged under .second-look", Rows: rows}}
}

// holds is what the review carries, or why it could not be read.
func holds(r *prepared.Review) string {
	if r.Broken != "" {
		return r.Broken
	}

	return prepared.Holds(r)
}

func (s *reviewsScreen) act(a tui.Action, row *tui.Row) (string, bool, error) {
	number, err := strconv.Atoi(row.Key)
	if err != nil {
		return "", false, fmt.Errorf("%w: %s", errUnknownRow, row.Key)
	}

	switch a {
	case tui.ActChoose:
		return s.choose(number)
	case tui.ActRefresh:
		rows, err := prepared.List(".")
		if err != nil && !errors.Is(err, prepared.ErrNoDir) {
			return "", false, fmt.Errorf("listing the staged reviews: %w", err)
		}

		s.rows = rows

		return s.counts(), false, nil
	case tui.ActMark, tui.ActBrowse, tui.ActReply, tui.ActResolve:
		return "", false, errNotHere
	}

	return "", false, nil
}

// errNotHere covers the keys the conversation queue offers and this screen does
// not. Saying so beats a key that silently does nothing.
var errNotHere = errors.New("that key belongs to the conversation queue; " +
	"enter opens a review, ? lists the keys")

// choose leaves the screen so the review can open. Two Bubble Tea programs
// cannot own the terminal at once, so the number is carried out rather than the
// screen opening it from inside.
func (s *reviewsScreen) choose(number int) (string, bool, error) {
	for i := range s.rows {
		if s.rows[i].Number != number {
			continue
		}

		if s.rows[i].Broken != "" {
			return "", false, fmt.Errorf("#%d: %w: %s", number, errUnreadable, s.rows[i].Broken)
		}

		s.open = number

		return "opening #" + strconv.Itoa(number), true, nil
	}

	return "", false, fmt.Errorf("%w: #%d", errUnknownRow, number)
}
