package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
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
	open *ref
}

var reviewsHelp = []string{
	"  j/k, ctrl+u/d, g/G   move, half page, top and bottom",
	"  enter                open the review screen for it",
	"  ctrl+r               read the directory again",
	"  q, esc               leave",
	"",
	"  blocked means a comment is still a draft, which stops the submit.",
	"  Every review here is unfinished: the file is deleted when it posts.",
	"  A review with no checkout of its repository is listed in its own group and",
	"  opens the same way, from the API.",
}

// openReviews shows the staged reviews and opens whichever one was chosen, once
// this screen has given the terminal back.
func openReviews(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	rows, err := staged()
	if err != nil {
		return err
	}

	s := &reviewsScreen{ctx: ctx, rows: rows}

	list := tui.NewList("second-look staged reviews", s.sections, s.act).
		WithSubtitle(s.counts).
		WithHints([][2]string{{"enter", "open"}, {"ctrl+r", "refresh"}, {"?", helpArg}}).
		WithHelp(reviewsHelp)

	if _, err := tui.RunList(list); err != nil {
		return fmt.Errorf("listing the staged reviews: %w", err)
	}

	if s.open != nil {
		return openRef(ctx, *s.open, stdin, stdout)
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

// sections splits by where the review is kept, because that is what says
// whether the code under review is on this disk. Within a group recency is the
// order and no further bucket earns its place.
func (s *reviewsScreen) sections() []tui.Section {
	now := time.Now()
	here := make([]tui.Row, 0, len(s.rows))

	var away []tui.Row

	for i := range s.rows {
		r := &s.rows[i]
		row := tui.Row{
			// The key names the repository as well as the number, since the same
			// number in two repositories is two rows.
			Key:  r.Where(),
			Left: r.Where(),
			Mid:  prepared.State(r),
			Age:  humanize.Ago(r.Modified, now),
			Tail: holds(r),
			// A review with a draft in it is the one to come back to, which is
			// what the unread mark means on this screen.
			Unread: r.Blocked() || r.Broken != "",
		}

		if r.Detached {
			away = append(away, row)

			continue
		}

		here = append(here, row)
	}

	out := []tui.Section{{Name: "staged under .second-look", Rows: here}}
	if len(away) > 0 {
		out = append(out, tui.Section{Name: "staged with no checkout", Rows: away})
	}

	return out
}

// holds is what the review carries, or why it could not be read.
func holds(r *prepared.Review) string {
	if r.Broken != "" {
		return r.Broken
	}

	return prepared.Holds(r)
}

func (s *reviewsScreen) act(a tui.Action, row *tui.Row) (string, bool, error) {
	switch a {
	case tui.ActChoose:
		return s.choose(row.Key)
	case tui.ActRefresh:
		rows, err := staged()
		if err != nil {
			return "", false, err
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
// cannot own the terminal at once, so which review was chosen is carried out
// rather than the screen opening it from inside.
func (s *reviewsScreen) choose(key string) (string, bool, error) {
	for i := range s.rows {
		if s.rows[i].Where() != key {
			continue
		}

		if s.rows[i].Broken != "" {
			return "", false, fmt.Errorf("%s: %w: %s", key, errUnreadable, s.rows[i].Broken)
		}

		owner, name, _ := strings.Cut(s.rows[i].Repository, "/")
		s.open = &ref{owner: owner, repo: name, number: s.rows[i].Number}

		return "opening " + key, true, nil
	}

	return "", false, fmt.Errorf("%w: %s", errUnknownRow, key)
}
