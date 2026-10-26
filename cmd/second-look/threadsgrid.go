package main

import (
	"strconv"
	"strings"
	"time"

	"github.com/kyleking/aragonite/tui/table"

	"github.com/kyleking/second-look/internal/conversations"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/notice"
	"github.com/kyleking/second-look/internal/tui"
)

// colAnchor is where a conversation sits: a file and line, or the surface's name.
const (
	colAnchor  = "anchor"
	anchorMin  = 14
	anchorMax  = 18
	anchorFrom = 100
)

// columns is the grid the conversation queue is drawn on, in the inbox's order
// so the two tabs read down the same columns.
func (s *threadsScreen) columns() []table.Column {
	return []table.Column{
		refColumn(s.places()),
		{Key: colTitle, Min: titleFloor, Weight: titleWeight},
		{Key: colAnchor, Min: anchorMin, Max: anchorMax, From: anchorFrom, Trim: table.TrimLeft},
		{Key: colAuthor, Min: authorWidth, Max: authorWidth, From: authorFrom},
		{Key: colAge, Min: ageWidth, Max: ageWidth, Align: table.AlignRight},
		{Key: colStatus, Min: statusMin, Max: statusMax, Weight: 1},
	}
}

// places is where every notification and conversation on screen points.
func (s *threadsScreen) places() []place {
	out := make([]place, 0, len(s.notices))

	for i := range s.notices {
		out = append(out, place{repo: s.notices[i].Repo, number: s.notices[i].Number})
	}

	for i := range s.buckets {
		for j := range s.buckets[i].Items {
			c := &s.buckets[i].Items[j]
			out = append(out, place{repo: c.Repository, number: c.Number})
		}
	}

	return out
}

// unreadMark leads a title that moved since it was read, and holds the cell
// open on one that did not.
func unreadMark(unread bool) tui.Span {
	if unread {
		return tui.Span{Text: "●", Role: tui.RoleUnread}
	}

	return tui.Span{Text: " ", Role: tui.RoleCell}
}

// conversationCells is one conversation on the grid. The unread mark is read
// live, so marking one clears its mark where it sits.
func (s *threadsScreen) conversationCells(
	c *conversations.Conversation, shared map[string]bool, now time.Time,
) map[string]tui.Cell {
	return map[string]tui.Cell{
		colRef: {Spans: place{repo: c.Repository, number: c.Number}.spans(shared)},
		colTitle: {Spans: []tui.Span{
			unreadMark(!s.looked.Since(c)),
			{Text: " ", Role: tui.RoleCell},
			{Text: humanize.FirstLine(c.Last().Body), Role: tui.RoleText},
		}},
		colAnchor: plain(c.Anchor(), tui.RoleCell),
		colAuthor: plain(c.Last().Author, tui.RoleCell),
		colAge:    plain(humanize.Ago(c.Updated(), now), tui.RoleAge),
		colStatus: fact(conversationStatus(c, s.local[c.Where()])),
	}
}

// conversationStatus is what is notable about a conversation beyond its words:
// work already staged on the pull request, how long the thread has run, and
// whether its line has moved. The long form spells out what the short one cuts.
func conversationStatus(c *conversations.Conversation, k inbox.Known) tui.Cell {
	words := func(old string) []tui.Span {
		var spans []tui.Span

		parts := make([]string, 0, 3)

		if w := holdingWord(k); w != "" {
			spans = append(spans, tui.Span{Text: "● ", Role: tui.RoleUnread})
			parts = append(parts, w)
		}

		if n := len(c.Notes); n > 1 {
			parts = append(parts, strconv.Itoa(n)+" replies")
		}

		if c.Outdated {
			parts = append(parts, old)
		}

		if len(parts) == 0 {
			return nil
		}

		return append(spans, tui.Span{Text: strings.Join(parts, " · "), Role: tui.RoleCell})
	}

	return tui.Cell{Spans: words("old"), Long: words("outdated"), LongAt: statusLong}
}

// conversationAbout is what the inspector says of a conversation: what the
// grid dropped, then what it never had room for, such as the pull request's own
// title.
func (s *threadsScreen) conversationAbout(c *conversations.Conversation, now time.Time) *tui.About {
	facts := []tui.Fact{
		{Text: c.Last().Author, Column: colAuthor},
		{Text: c.Anchor(), Column: colAnchor},
		{Text: humanize.Old(c.Updated(), now)},
	}

	if !s.looked.Since(c) {
		facts = append(facts, tui.Fact{Text: "unread"})
	}

	if h := strings.TrimSpace(holding(s.local[c.Where()])); h != "" {
		facts = append(facts, tui.Fact{Text: h})
	}

	if n := len(c.Notes); n > 1 {
		facts = append(facts, tui.Fact{Text: strconv.Itoa(n) + " replies"})
	}

	if c.Outdated {
		facts = append(facts, tui.Fact{Text: "outdated"})
	}

	return &tui.About{
		Subject: humanize.FirstLine(c.Last().Body),
		Ref:     c.Where(),
		Facts:   append(facts, tui.Fact{Text: why(c.Why)}, tui.Fact{Text: c.Title}),
	}
}

// why is what put a conversation in front of you.
func why(w conversations.Why) string {
	var parts []string

	if w.Yours {
		parts = append(parts, "your pull request")
	}

	if w.Mentioned {
		parts = append(parts, "mentions you")
	}

	if w.Spoke {
		parts = append(parts, "you replied")
	}

	return strings.Join(parts, ", ")
}

// noticeCells is a notification on the grid. It has no anchor and no author, so
// those columns stay empty, and the reason it arrived takes the status.
func noticeCells(n *notice.Thread, shared map[string]bool, now time.Time) map[string]tui.Cell {
	return map[string]tui.Cell{
		colRef:    {Spans: place{repo: n.Repo, number: n.Number}.spans(shared)},
		colTitle:  {Spans: titleSpans(n.Title, unreadMark(true))},
		colAge:    plain(humanize.Ago(n.Updated, now), tui.RoleAge),
		colStatus: plain(noticeReason(n.Reason), tui.RoleCell),
	}
}

// noticeAbout is what the inspector says of a notification.
func noticeAbout(n *notice.Thread, now time.Time) *tui.About {
	prefix := conventional.FindString(n.Title)

	return &tui.About{
		Subject: n.Title[len(prefix):],
		Prefix:  strings.TrimSuffix(prefix, ": "),
		Ref:     n.Where(),
		Facts: []tui.Fact{
			{Text: noticeReason(n.Reason)},
			{Text: humanize.Old(n.Updated, now)},
		},
	}
}
