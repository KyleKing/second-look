package main

import (
	"context"
	"errors"
	"fmt"
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

// reviewsHints is the footer, which advertises only the keys this screen offers.
var reviewsHints = [][2]string{{enterKey, "open"}, {refreshKey, "refresh"}, {"?", helpArg}}

var reviewsHelp = helpFor(helpMove(), [][2]string{
	{enterKey, "open the review screen for it"},
	{"/", "narrow to the rows carrying a word; esc puts them back"},
	{refreshKey, "read the directory again"},
}, helpLeave(), prose(
	"blocked means a comment is still a draft, which stops the submit.",
	"Every review here is unfinished: the file is deleted when it posts.",
	"A review with no checkout of its repository is listed in its own group and",
	"opens the same way, from the API.",
	"A pull request based on another one staged here is grouped with it, bottom",
	"first, which is the order the diffs read in.",
))

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

// sections puts each stack in its own group, then splits what is left by where
// the review is kept, because that is what says whether the code under review is
// on this disk. Within a group recency is the order and no further bucket earns
// its place.
func (s *reviewsScreen) sections() []tui.Section {
	now := time.Now()
	stacks, alone := prepared.Split(s.rows)

	out := make([]tui.Section, 0, len(stacks)+2)

	for i := range stacks {
		rows := make([]tui.Row, 0, len(stacks[i].Rows))
		for j := range stacks[i].Rows {
			rows = append(rows, reviewRow(&stacks[i].Rows[j], now))
		}

		out = append(out, tui.Section{Name: stackName(&stacks[i]), Rows: rows})
	}

	here := make([]tui.Row, 0, len(alone))

	var away []tui.Row

	for i := range alone {
		row := reviewRow(&alone[i], now)

		if alone[i].Detached {
			away = append(away, row)

			continue
		}

		here = append(here, row)
	}

	out = append(out, tui.Section{Name: "staged under .second-look", Rows: here})
	if len(away) > 0 {
		out = append(out, tui.Section{Name: "staged with no checkout", Rows: away})
	}

	return out
}

// stackName says what the chain lands on and that its order is the reading
// order, since the group being a stack is the only reason it is not in the list
// underneath.
func stackName(st *prepared.Stack) string {
	return "stacked onto " + st.Onto + ", bottom first"
}

func reviewRow(r *prepared.Review, now time.Time) tui.Row {
	return tui.Row{
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
	case tui.ActMark, tui.ActBrowse, tui.ActReply, tui.ActResolve,
		tui.ActCheckout, tui.ActComment, tui.ActApprove:
		return "", false, errNotHere
	}

	return "", false, nil
}

// The keys another list screen offers and this one does not. Saying so beats a
// key that silently does nothing.
var (
	errNotHere = errors.New("that key belongs to the conversation queue; " +
		"enter opens a review, ? lists the keys")
	errNotInInbox = errors.New("that key belongs to the conversation queue; " +
		"enter reviews the pull request, o opens it on GitHub, ? lists the keys")
	errNoPullRequest  = errors.New("this row is a search that failed, not a pull request")
	errNoCheckoutHere = errors.New("no clone of it is on this laptop, so there is nothing to check out; " +
		"enter reviews it from the API instead")
	errNotOnAConversation = errors.New("that key belongs to the inbox, which lists pull requests; " +
		"r answers this conversation and R marks it dealt with")
)

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
