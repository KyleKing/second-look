package main

import (
	"strings"
	"time"

	"github.com/kyleking/aragonite/tui/table"

	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/prepared"
	"github.com/kyleking/second-look/internal/tui"
)

// The staged queue's own columns. What a review holds shows from 100 cells, and
// the forge's word and the agent's word from 140: below that the inspector says
// them, so a narrow frame loses nothing one keypress cannot recover.
const (
	colHolds    = "holds"
	colRemote   = "remote"
	colAgent    = "agent"
	holdsFloor  = 8
	holdsCap    = 24
	holdsFrom   = 100
	remoteFrom  = 140
	remoteMin   = 14
	agentWidth  = 15
	reviewState = 12
)

// columns is the grid the staged reviews are drawn on, in the inbox's order so
// the tabs read down the same columns.
func (s *reviewsScreen) columns() []table.Column {
	holds := holdsFloor

	for i := range s.rows {
		holds = min(max(holds, humanize.Width(s.tail(&s.rows[i]))), holdsCap)
	}

	return []table.Column{
		refColumn(s.places()),
		{Key: colTitle, Min: titleFloor, Weight: titleWeight},
		{Key: colHolds, Min: holds, Max: holds, From: holdsFrom, Align: table.AlignRight},
		{Key: colAge, Min: ageWidth, Max: ageWidth, Align: table.AlignRight},
		{Key: colStatus, Min: reviewState, Max: reviewState},
		{Key: colRemote, Min: remoteMin, Max: statusMax, Weight: 1, From: remoteFrom},
		{Key: colAgent, Min: agentWidth, Max: agentWidth, From: remoteFrom},
	}
}

// places is where every staged review points.
func (s *reviewsScreen) places() []place {
	out := make([]place, 0, len(s.rows))

	for i := range s.rows {
		out = append(out, place{repo: s.rows[i].Repository, number: s.rows[i].Number})
	}

	return out
}

// reviewCells is one staged review on the grid. A review with a draft in it is
// the one to come back to, which is what the mark means on this screen.
func (s *reviewsScreen) reviewCells(
	r *prepared.Review, shared map[string]bool, now time.Time,
) map[string]tui.Cell {
	state := prepared.State(r)
	word, wordRole := s.remoteWord(r)

	return map[string]tui.Cell{
		colRef:    {Spans: place{repo: r.Repository, number: r.Number}.spans(shared)},
		colTitle:  titleCell(r),
		colHolds:  plain(s.tail(r), tui.RoleCell),
		colAge:    plain(humanize.Ago(r.Modified, now), tui.RoleAge),
		colStatus: statusCellOf(stateGlyph(state), state, stateRole(state)),
		colRemote: wordCell(remoteGlyph(wordRole), word, wordRole),
		colAgent:  wordCell(agentGlyph(s.agentRole(r)), s.agentWord(r), s.agentRole(r)),
	}
}

// titleCell is what the pull request is called, behind the mark. A review that
// could not be read says why where its title would be, and one whose title was
// never cached draws the placeholder.
func titleCell(r *prepared.Review) tui.Cell {
	lead := unreadMark(r.Blocked() || r.Broken != "")

	switch name := title(r); {
	case r.Broken != "":
		return tui.Cell{Spans: []tui.Span{lead, {Text: " ", Role: tui.RoleCell}, {Text: r.Broken, Role: tui.RoleText}}}
	case name == "":
		return tui.Cell{Spans: append([]tui.Span{lead, {Text: " ", Role: tui.RoleCell}}, tui.Pending().Spans...)}
	default:
		return titleSpans(name, lead)
	}
}

// statusCellOf is a glyph in its tone and a word beside it.
func statusCellOf(glyph, word string, role tui.Role) tui.Cell {
	return tui.Cell{Spans: []tui.Span{
		{Text: glyph, Role: role},
		{Text: " " + word, Role: tui.RoleCell},
	}}
}

// wordCell is statusCellOf for a word a row may not have.
func wordCell(glyph, word string, role tui.Role) tui.Cell {
	if word == "" {
		return tui.Cell{}
	}

	return statusCellOf(glyph, word, role)
}

// stateGlyph is the mark each review state draws with, which is what survives
// a terminal with no color.
func stateGlyph(state string) string {
	switch state {
	case prepared.StateReady:
		return "✓"
	case prepared.StateBlocked, prepared.StateUnreadable:
		return "✗"
	default:
		return "○"
	}
}

// remoteGlyph marks the forge's word by how loud it is.
func remoteGlyph(role tui.Role) string {
	switch role {
	case tui.RoleMerged, tui.RoleGood:
		return "✓"
	case tui.RoleBad:
		return "✗"
	default:
		return "○"
	}
}

// agentGlyph marks the session's word: a question waiting is the one to answer.
func agentGlyph(role tui.Role) string {
	switch role {
	case tui.RoleWarn:
		return "◆"
	case tui.RoleGood:
		return "✓"
	default:
		return "↻"
	}
}

// reviewAbout is what the inspector says of a staged review: what the grid
// dropped, then what it never had room for.
func (s *reviewsScreen) reviewAbout(r *prepared.Review, now time.Time) *tui.About {
	name := title(r)
	prefix := conventional.FindString(name)

	word, wordRole := s.remoteWord(r)

	ref := r.Where()
	if name == "" {
		name, ref = ref, ""
	}

	facts := []tui.Fact{
		{Text: r.Broken},
		{Text: s.tail(r), Column: colHolds},
		{Text: humanize.Old(r.Modified, now)},
		{Text: word, Role: wordRole, Column: colRemote},
		{Text: s.agentWord(r), Role: s.agentRole(r), Column: colAgent},
	}

	return &tui.About{
		Subject: strings.TrimPrefix(name, prefix),
		Prefix:  strings.TrimSuffix(prefix, ": "),
		Ref:     ref,
		Facts:   facts,
	}
}
