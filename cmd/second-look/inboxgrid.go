package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/kyleking/aragonite/tui/table"

	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/labels"
	"github.com/kyleking/second-look/internal/tui"
)

// The inbox's columns, left to right.
const (
	colRef    = "ref"
	colTitle  = "title"
	colAuthor = "author"
	colAge    = "age"
	colSize   = "size"
	colStatus = "status"
)

// wordDraft is what a draft pull request reads as, before and after the facts land.
const wordDraft = "draft"

// wordStaged is what a review staged here with nothing in it yet reads as, and
// the name of the tab and lane that hold such reviews.
const wordStaged = "staged"

// The grid's widths. A change is drawn in a count of at most four cells and a
// sign, so the size column holds two of those and the space between them. The
// long status word needs a cell this wide, and a status never grows past it.
const (
	authorWidth = 10
	ageWidth    = 4
	countWidth  = 5
	sizeWidth   = 2*countWidth + 1
	statusMin   = 2 + inbox.ShortWidth
	statusLong  = 24
	titleLong   = 40
	statusMax   = 26
	titleFloor  = 30
	authorFrom  = 112
	sizeFrom    = 100
	titleWeight = 3
)

// columns is the grid the inbox rows are drawn on.
func (s *inboxScreen) columns() []table.Column {
	return []table.Column{
		refColumn(s.places()),
		{Key: colTitle, Min: titleFloor, Weight: titleWeight},
		{Key: colAuthor, Min: authorWidth, Max: authorWidth, From: authorFrom},
		{Key: colAge, Min: ageWidth, Max: ageWidth, Align: table.AlignRight},
		{Key: colSize, Min: sizeWidth, Max: sizeWidth, From: sizeFrom, Align: table.AlignRight},
		{Key: colStatus, Min: statusMin, Max: statusMax, Weight: 1},
	}
}

// places is where every pull request in the lanes points.
func (s *inboxScreen) places() []place {
	var out []place

	for i := range s.lanes {
		for j := range s.lanes[i].Items {
			p := &s.lanes[i].Items[j]
			out = append(out, place{repo: p.Repository, number: p.Number})
		}
	}

	return out
}

// cells is one row on the grid. A fact that has not arrived draws a dot in its
// column, so landing it changes nothing's width.
func (s *inboxScreen) cells(
	p *inbox.PullRequest, k inbox.Known, shared map[string]bool, now time.Time,
) map[string]tui.Cell {
	_, have := s.facts[keyOf(p)]

	return map[string]tui.Cell{
		colRef:    {Spans: place{repo: p.Repository, number: p.Number}.spans(shared)},
		colTitle:  titleSpans(p.Title, mark(k)),
		colAuthor: plain(p.Author, tui.RoleCell),
		colAge:    plain(humanize.Ago(p.Updated, now), tui.RoleAge),
		colSize:   fact(sizeCell(k)),
		colStatus: fact(statusCell(p, k, have)),
	}
}

// about is what the inspector says of a row: everything the grid dropped or
// never had room for, in the order a reader asks for it. The change's size is
// said only where the grid hid it.
func about(p *inbox.PullRequest, k inbox.Known, now time.Time) *tui.About {
	prefix := conventional.FindString(p.Title)

	a := tui.About{
		Subject: p.Title[len(prefix):],
		Prefix:  strings.TrimSuffix(prefix, ": "),
		Ref:     keyOf(p),
		Facts: []tui.Fact{
			{Text: p.Author},
			{Text: humanize.Old(p.Updated, now)},
			statusFact(p, k),
			{Text: k.Who},
		},
	}

	if k.Stack.Size > 0 {
		a.Facts = append(a.Facts,
			tui.Fact{Text: fmt.Sprintf("stack %d of %d", k.Stack.Position, k.Stack.Size)},
			tui.Fact{Text: strings.TrimPrefix(readFirst(k.Stack), " · ")})
	}

	a.Facts = append(a.Facts, tui.Fact{Text: strings.TrimSpace(holding(k))})

	if k.Rated {
		a.Facts = append(a.Facts, tui.Fact{Text: "cost " + rated(k)})
	}

	a.Facts = append(a.Facts, tui.Fact{Text: labels.Tags(p.Labels)})

	if measured(k) {
		a.Facts = append(a.Facts, tui.Fact{
			Text: "+" + humanize.Count(k.Added) + " -" + humanize.Count(k.Removed), Column: colSize,
		})
	}

	return &a
}

// statusFact is the long word for why the row sits where it does, in its tone.
func statusFact(p *inbox.PullRequest, k inbox.Known) tui.Fact {
	switch {
	case k.Long != "":
		return tui.Fact{Text: k.Long, Role: toneRole(k.Tone)}
	case p.Draft:
		return tui.Fact{Text: wordDraft, Role: tui.RoleMuted}
	}

	return tui.Fact{}
}

// fact marks a cell the forge supplies, which a row drawn from a snapshot recedes.
func fact(c tui.Cell) tui.Cell {
	c.Fact = true

	return c
}

func plain(text string, role tui.Role) tui.Cell {
	return tui.Cell{Spans: []tui.Span{{Text: text, Role: role}}}
}

// mark is the one cell ahead of a title: priority, else where the row sits in
// its stack, else nothing.
func mark(k inbox.Known) tui.Span {
	switch {
	case k.Priority:
		return tui.Span{Text: "!", Role: tui.RolePriority}
	case k.Stack.Size == 0:
		return tui.Span{Text: " ", Role: tui.RoleCell}
	case !k.Stack.Above():
		return tui.Span{Text: "╭", Role: tui.RoleQuiet}
	case k.Stack.Position == k.Stack.Size:
		return tui.Span{Text: "╰", Role: tui.RoleQuiet}
	}

	return tui.Span{Text: "├", Role: tui.RoleQuiet}
}

var conventional = regexp.MustCompile(`^[A-Za-z]+(\([^)]*\))?!?: `)

// titleSpans is a title behind its mark. A conventional-commit prefix is receded
// and kept only where the column is wide enough to spare it, since the inspector
// shows it in full.
func titleSpans(title string, lead tui.Span) tui.Cell {
	head := []tui.Span{lead, {Text: " ", Role: tui.RoleCell}}
	prefix := conventional.FindString(title)
	subject := tui.Span{Text: title[len(prefix):], Role: tui.RoleText}

	short := append(append([]tui.Span{}, head...), subject)
	if prefix == "" {
		return tui.Cell{Spans: short}
	}

	long := append(append([]tui.Span{}, head...), tui.Span{Text: prefix, Role: tui.RoleQuiet}, subject)

	return tui.Cell{Spans: short, Long: long, LongAt: titleLong}
}

func sizeCell(k inbox.Known) tui.Cell {
	if !measured(k) {
		return tui.Pending()
	}

	count := func(sign string, n int) string {
		word := sign + humanize.Count(n)

		return strings.Repeat(" ", max(0, countWidth-humanize.Width(word))) + word
	}

	return tui.Cell{Spans: []tui.Span{
		{Text: count("+", k.Added), Role: tui.RoleAdd},
		{Text: " ", Role: tui.RoleCell},
		{Text: count("-", k.Removed), Role: tui.RoleRemove},
	}}
}

// statusCell is why the row sits where it does. A draft is the one reason the
// search answers by itself, so it reads before the facts arrive.
func statusCell(p *inbox.PullRequest, k inbox.Known, have bool) tui.Cell {
	if k.Short == "" {
		switch {
		case p.Draft:
			return statusOf("◌", wordDraft, wordDraft, inbox.ToneMuted)
		case !have:
			return tui.Pending()
		}

		return tui.Cell{}
	}

	return statusOf(k.Glyph, k.Short, k.Long, k.Tone)
}

func statusOf(glyph, short, long string, tone inbox.Tone) tui.Cell {
	words := func(word string) []tui.Span {
		if glyph == "" {
			return []tui.Span{{Text: word, Role: tui.RoleCell}}
		}

		return []tui.Span{
			{Text: glyph, Role: toneRole(tone)},
			{Text: " " + word, Role: tui.RoleCell},
		}
	}

	return tui.Cell{Spans: words(short), Long: words(long), LongAt: statusLong}
}

func toneRole(t inbox.Tone) tui.Role {
	switch t {
	case inbox.ToneGood:
		return tui.RoleGood
	case inbox.ToneNew:
		return tui.RoleNew
	case inbox.ToneWarn:
		return tui.RoleWarn
	case inbox.ToneBad:
		return tui.RoleBad
	case inbox.ToneMuted:
		return tui.RoleMuted
	case inbox.ToneMerged:
		return tui.RoleMerged
	case inbox.ToneNone:
	}

	return tui.RoleCell
}
