package main

import (
	"strconv"
	"strings"

	"github.com/kyleking/aragonite/tui/table"

	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/tui"
)

// refFloor is the narrowest a ref column goes when the frame has no room.
const refFloor = 15

// place is what a row's ref points at: a repository and, where the subject has
// one, a number.
type place struct {
	repo   string
	number int
}

// sharedNames is the repository names that two owners share across places,
// which are the only ones a ref needs its owner to tell apart.
func sharedNames(places []place) map[string]bool {
	owners := map[string]string{}
	shared := map[string]bool{}

	for _, p := range places {
		owner, name, _ := strings.Cut(p.repo, "/")
		if seen, ok := owners[name]; ok && seen != owner {
			shared[name] = true
		}

		owners[name] = owner
	}

	return shared
}

// parts is the ref as its owner prefix, which is empty unless the name is
// shared, and the rest.
func (p place) parts(shared map[string]bool) (string, string) {
	owner, name, _ := strings.Cut(p.repo, "/")
	rest := name

	if p.number > 0 {
		rest += "#" + strconv.Itoa(p.number)
	}

	if shared[name] {
		return owner + "/", rest
	}

	return "", rest
}

// spans is the ref as a grid cell draws it: the owner receded, then the rest.
func (p place) spans(shared map[string]bool) []tui.Span {
	owner, rest := p.parts(shared)

	out := []tui.Span{{Text: rest, Role: tui.RoleCell}}
	if owner != "" {
		out = append([]tui.Span{{Text: owner, Role: tui.RoleQuiet}}, out...)
	}

	return out
}

// refColumn is the ref column, as wide as the longest ref in view up to the
// floor it keeps when there is no room, so every title starts in the same
// column whatever lands.
func refColumn(places []place) table.Column {
	shared := sharedNames(places)
	widest := 0

	for _, p := range places {
		owner, rest := p.parts(shared)
		widest = max(widest, humanize.Width(owner+rest))
	}

	widest = max(widest, 1)

	return table.Column{Key: colRef, Min: min(widest, refFloor), Max: widest, Weight: 1, Trim: table.TrimLeft}
}
