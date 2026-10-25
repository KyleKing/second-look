package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kyleking/aragonite/tui/table"

	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/inbox"
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
	statusMax   = 26
	refFloor    = 15
	titleFloor  = 30
	authorFrom  = 112
	sizeFrom    = 100
	titleWeight = 3
)

// columns is the grid the inbox rows are drawn on. The ref column is as wide as
// the longest ref in view, up to the floor it keeps when there is no room, so
// every title starts in the same column whatever lands.
func (s *inboxScreen) columns() []table.Column {
	ref := max(s.refWidth(), 1)

	return []table.Column{
		{Key: colRef, Min: min(ref, refFloor), Max: ref, Weight: 1, Trim: table.TrimLeft},
		{Key: colTitle, Min: titleFloor, Weight: titleWeight},
		{Key: colAuthor, Min: authorWidth, Max: authorWidth, From: authorFrom},
		{Key: colAge, Min: ageWidth, Max: ageWidth, Align: table.AlignRight},
		{Key: colSize, Min: sizeWidth, Max: sizeWidth, From: sizeFrom, Align: table.AlignRight},
		{Key: colStatus, Min: statusMin, Max: statusMax, Weight: 1},
	}
}

// refWidth is the widest ref any lane holds.
func (s *inboxScreen) refWidth() int {
	shared := s.sharedNames()
	widest := 0

	for i := range s.lanes {
		for j := range s.lanes[i].Items {
			owner, name := refParts(&s.lanes[i].Items[j], shared)
			widest = max(widest, humanize.Width(owner+name))
		}
	}

	return widest
}

// sharedNames is the repository names that two owners share across the queue,
// which are the only ones a ref needs its owner to tell apart.
func (s *inboxScreen) sharedNames() map[string]bool {
	owners := map[string]string{}
	shared := map[string]bool{}

	for i := range s.lanes {
		for j := range s.lanes[i].Items {
			owner, name, _ := strings.Cut(s.lanes[i].Items[j].Repository, "/")
			if seen, ok := owners[name]; ok && seen != owner {
				shared[name] = true
			}

			owners[name] = owner
		}
	}

	return shared
}

// refParts is a ref as its owner prefix, which is empty unless the name is
// shared, and the rest.
func refParts(p *inbox.PullRequest, shared map[string]bool) (string, string) {
	owner, name, _ := strings.Cut(p.Repository, "/")
	rest := name + "#" + strconv.Itoa(p.Number)

	if shared[name] {
		return owner + "/", rest
	}

	return "", rest
}

// cells is one row on the grid. A fact that has not arrived draws a dot in its
// column, so landing it changes nothing's width.
func (s *inboxScreen) cells(
	p *inbox.PullRequest, k inbox.Known, shared map[string]bool, now time.Time,
) map[string]tui.Cell {
	_, have := s.facts[keyOf(p)]
	owner, rest := refParts(p, shared)

	ref := []tui.Span{{Text: rest, Role: tui.RoleCell}}
	if owner != "" {
		ref = append([]tui.Span{{Text: owner, Role: tui.RoleQuiet}}, ref...)
	}

	return map[string]tui.Cell{
		colRef:    {Spans: ref},
		colTitle:  {Spans: titleSpans(p.Title, mark(k))},
		colAuthor: plain(p.Author, tui.RoleCell),
		colAge:    plain(humanize.Ago(p.Updated, now), tui.RoleAge),
		colSize:   sizeCell(k),
		colStatus: statusCell(p, k, have),
	}
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

// titleSpans is a title behind its mark, with a conventional-commit prefix
// receded so the subject is what the eye lands on.
func titleSpans(title string, lead tui.Span) []tui.Span {
	spans := []tui.Span{lead, {Text: " ", Role: tui.RoleCell}}

	prefix := conventional.FindString(title)
	if prefix != "" {
		spans = append(spans, tui.Span{Text: prefix, Role: tui.RoleQuiet})
	}

	return append(spans, tui.Span{Text: title[len(prefix):], Role: tui.RoleText})
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
			return statusOf("◌", "draft", "draft", inbox.ToneMuted)
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
