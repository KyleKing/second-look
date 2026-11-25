package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/kyleking/aragonite/diff"

	"github.com/kyleking/second-look/internal/highlight"
	"github.com/kyleking/second-look/internal/labels"
)

// The helpers here are small enough that a frame says little about them, and
// each one decides something a frame would only show at one exact width.

func checkZPageHeadsBothGroups(t *testing.T) {
	t.Helper()

	page := zPage()
	if len(page) < 5 {
		t.Fatalf("the z page is %+v", page)
	}

	for i, want := range []struct {
		key, what string
		head      bool
	}{
		{"", "the line at", true},
		{"z", "the middle", false},
		{"t", "the top", false},
		{"b", "the bottom", false},
		{"", foldVerb, true},
	} {
		if h := page[i]; h.Key != want.key || h.What != want.what || h.Head != want.head {
			t.Errorf("hint %d is %+v, want %+v", i, h, want)
		}
	}

	for _, h := range page[5:] {
		if h.Head {
			t.Errorf("a fold object %+v draws as a heading", h)
		}
	}
}

func TestPureHelpers(t *testing.T) {
	t.Parallel()

	for name, check := range map[string]func(*testing.T){
		"carded":         checkCarded,
		"trailingWord":   checkTrailingWord,
		"pathOf":         checkPathOf,
		"dirOf":          checkDirOf,
		"span":           checkSpan,
		"covered":        checkCovered,
		"isHeading":      checkIsHeading,
		"mdWordEdge":     checkMdWordEdge,
		"Labeling.ready": checkLabelingReady,
		"cardOf":         checkCardOf,
		"windowed":       checkWindowed,
		"zPage":          checkZPageHeadsBothGroups,
		"lineBuilder":    checkLineBuilderClassesOnlyWhatItColors,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			check(t)
		})
	}
}

func checkCarded(t *testing.T) {
	t.Helper()

	if !carded(cardFloor) || carded(cardFloor-1) {
		t.Errorf(
			"carded flips at the wrong width: %v at %d, %v below",
			carded(cardFloor),
			cardFloor,
			carded(cardFloor-1),
		)
	}
}

func checkTrailingWord(t *testing.T) {
	t.Helper()

	for before, want := range map[string]string{
		"": "", "café @kyle": "@kyle", "see pkg/a_b.go": "pkg/a_b.go", "end ": "",
	} {
		if got := trailingWord(before); got != want {
			t.Errorf("trailingWord(%q) = %q, want %q", before, got, want)
		}
	}
}

func checkPathOf(t *testing.T) {
	t.Helper()

	for _, tc := range []struct {
		f    diff.File
		want string
	}{
		{diff.File{OldPath: "go.sum", NewPath: "go.sum"}, "go.sum"},
		{diff.File{OldPath: "old/yarn.lock", NewPath: "new/yarn.lock"}, "new/yarn.lock"},
		{diff.File{OldPath: "gone/yarn.lock"}, "gone/yarn.lock"},
	} {
		if got := pathOf(&tc.f); got != tc.want {
			t.Errorf("pathOf(%+v) = %q, want %q", tc.f, got, tc.want)
		}
	}
}

func checkDirOf(t *testing.T) {
	t.Helper()

	for p, want := range map[string]string{
		"internal/tui/model.go": "internal/tui",
		"go.mod":                rootDir,
		"/go.mod":               rootDir,
		"a/b":                   "a",
	} {
		if got := dirOf(p); got != want {
			t.Errorf("dirOf(%q) = %q, want %q", p, got, want)
		}
	}
}

func checkSpan(t *testing.T) {
	t.Helper()

	for _, tc := range []struct {
		at, lo, hi int
		want       float64
	}{
		{5, 0, 10, 0.5},
		{5, 4, 8, 0.25},
		{1, 4, 8, 0},
		{20, 4, 8, 1},
		{3, 3, 3, 0},
	} {
		if got := span(tc.at, tc.lo, tc.hi); got != tc.want {
			t.Errorf("span(%d, %d, %d) = %v, want %v", tc.at, tc.lo, tc.hi, got, tc.want)
		}
	}
}

func checkCovered(t *testing.T) {
	t.Helper()

	spans := []diff.Span{{From: 2, To: 4}, {From: 8, To: 9}}
	for at, want := range map[int]bool{0: false, 2: true, 3: true, 4: false, 8: true, 9: false} {
		if got := covered(spans, at); got != want {
			t.Errorf("covered(%d) = %v, want %v", at, got, want)
		}
	}

	if covered(nil, 0) {
		t.Error("nothing covers a line with no spans")
	}
}

func checkIsHeading(t *testing.T) {
	t.Helper()

	for line, want := range map[string]bool{
		"# Title": true, "###### six": true, "####### seven": false,
		"#hashtag": false, "#": false, "plain": false, "## ": true,
	} {
		if got := isHeading(line); got != want {
			t.Errorf("isHeading(%q) = %v, want %v", line, got, want)
		}
	}
}

func checkMdWordEdge(t *testing.T) {
	t.Helper()

	for _, tc := range []struct {
		line string
		at   int
		want bool
	}{
		{"_word_", 0, true},
		{"a _word_", 2, true},
		{"a\t_word_", 2, true},
		{"(_word_)", 1, true},
		{"[_word_]", 1, true},
		{`"_word_"`, 1, true},
		{"'_word_'", 1, true},
		{"snake_case", 5, false},
		{"a _ b", 2, false},
		{"a __b", 2, false},
		{"a _", 2, false},
	} {
		if got := mdWordEdge(tc.line, tc.at); got != tc.want {
			t.Errorf("mdWordEdge(%q, %d) = %v, want %v", tc.line, tc.at, got, tc.want)
		}
	}
}

func checkLabelingReady(t *testing.T) {
	t.Helper()

	var none Labeling

	read := Labeling{Read: noLabels}
	set := Labeling{Set: noSet}
	full := Labeling{Read: noLabels, Set: noSet}

	if none.ready() || read.ready() || set.ready() || !full.ready() {
		t.Errorf("ready is %v %v %v %v, want only both halves to be ready",
			none.ready(), read.ready(), set.ready(), full.ready())
	}
}

func checkCardOf(t *testing.T) {
	t.Helper()

	cases := []struct {
		r    row
		want blockKey
		ok   bool
	}{
		{row{kind: rowPeek}, blockKey{}, false},
		{row{kind: rowPeek, role: roleTop}, blockKey{rowPeek, 0}, true},
		{row{kind: rowThread, thread: 3, role: roleTop}, blockKey{rowThread, 3}, true},
		{row{kind: rowAsk, ask: 2, role: roleTop}, blockKey{rowAsk, 2}, true},
	}

	for i := range cases {
		tc := &cases[i]
		if got, ok := cardOf(tc.r); got != tc.want || ok != tc.ok {
			t.Errorf("cardOf(%+v) = %+v %v, want %+v %v", tc.r, got, ok, tc.want, tc.ok)
		}
	}
}

func checkWindowed(t *testing.T) {
	t.Helper()

	cs := make([]cluster, 5)
	for i := range cs {
		cs[i] = cluster{label: strconv.Itoa(i), segs: []seg{{weight: i + 1, read: i%2 == 0}}}
	}

	for _, tc := range []struct {
		at, near int
		want     string
	}{
		{2, 0, "+2(3 false) 2 +2(9 false)"},
		{2, 1, "+1(1 true) 1 2 3 +1(5 true)"},
		{0, 1, "0 1 +3(12 false)"},
		{4, 2, "+2(3 false) 2 3 4"},
		{2, 2, "0 1 2 3 4"},
	} {
		var words []string
		for _, c := range windowed(cs, tc.at, tc.near) {
			word := c.label
			if c.more > 0 {
				word += fmt.Sprintf("(%d %v)", c.segs[0].weight, c.segs[0].read)
			}

			words = append(words, word)
		}

		if got := strings.Join(words, " "); got != tc.want {
			t.Errorf("windowed(at %d, near %d) = %q, want %q", tc.at, tc.near, got, tc.want)
		}
	}
}

func checkLineBuilderClassesOnlyWhatItColors(t *testing.T) {
	t.Helper()

	var l lineBuilder

	l.add("plain ", highlight.Plain)
	l.add("", highlight.Keyword)
	l.add("kw", highlight.Keyword)

	if got := l.spans; len(got) != 1 || got[0] != (highlight.Span{From: 6, To: 8, Class: highlight.Keyword}) {
		t.Errorf("spans are %+v, want the keyword alone", got)
	}

	for _, tc := range []struct {
		name  string
		n     graphNode
		width int
		text  string
		class highlight.Class
	}{
		{"a plain place", graphNode{where: "a.go:3"}, 10, " a.go:3", highlight.Plain},
		{"a faint place", graphNode{where: "a.go:3", faint: true}, 10, " a.go:3", highlight.Comment},
		{"the rest", graphNode{where: "a.go:3", rest: true}, 10, " a.go:3", highlight.Comment},
		{"no room", graphNode{where: "a.go:3", faint: true}, 0, "", highlight.Plain},
	} {
		var b lineBuilder

		b.where(tc.n, tc.width)

		if b.b.String() != tc.text {
			t.Errorf("%s wrote %q, want %q", tc.name, b.b.String(), tc.text)
		}

		var want []highlight.Span
		if tc.class != highlight.Plain {
			want = []highlight.Span{{From: 1, To: len(tc.text), Class: tc.class}}
		}

		if fmt.Sprint(b.spans) != fmt.Sprint(want) {
			t.Errorf("%s classed %+v, want %+v", tc.name, b.spans, want)
		}
	}
}

func noLabels(context.Context, string) ([]labels.Label, error) { return nil, nil }

func noSet(context.Context, labels.Target, string, bool) error { return nil }
