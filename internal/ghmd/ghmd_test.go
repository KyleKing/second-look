package ghmd_test

import (
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/ghmd"
)

// bot is the shape a review bot actually posts: a labeled first line, routing
// metadata nobody should see, a collapsed section, and fenced tool output long
// enough to bury everything under it. Every rule is exercised against this one
// body because the rules interact, and each of them reads as a rule only when
// the others are in the room.
func bot() string {
	return strings.Join([]string{
		"<!-- coderabbit:state {\"thread\":\"abc\"} -->",
		"_🩺 Stability & Availability_ | _🟠 Major_ | _🏗️ Heavy lift_",
		"",
		"The workflow group cancels the older run, so `prod` wins and the",
		"loser's report job posts CANCELED.",
		"",
		"| Newer run | Older run | Outcome |",
		"|-----------|-----------|---------|",
		"| prod | prod | The older run is canceled |",
		"",
		"<details>",
		"<summary>🔎 Supported by static analysis</summary>",
		"",
		"🏁 Script executed:",
		"",
		"```shell",
		"#!/bin/bash",
		"set -eu",
		"sed -n '88,110p' .github/ACTIONS.md",
		"```",
		"",
		"Length of output: 47492",
		"",
		"---",
		"",
		"Repository: coverbasedev/irm",
		"</details>",
	}, "\n")
}

func kinds(bs []ghmd.Block) []ghmd.Kind {
	out := make([]ghmd.Kind, len(bs))
	for i := range bs {
		out[i] = bs[i].Kind
	}

	return out
}

func TestABotsCommentComesApartIntoWhatItActuallyIs(t *testing.T) {
	t.Parallel()

	got := ghmd.Parse(bot())

	want := []ghmd.Kind{ghmd.Prose, ghmd.Prose, ghmd.Table, ghmd.Details}
	if len(got) != len(want) {
		t.Fatalf("wanted %d blocks, got %d: %v", len(want), len(got), kinds(got))
	}

	for i := range want {
		if got[i].Kind != want[i] {
			t.Errorf("block %d is %v, wanted %v", i, got[i].Kind, want[i])
		}
	}

	if head := got[0].Lines[0]; head != "🩺 Stability & Availability | 🟠 Major | 🏗️ Heavy lift" {
		t.Errorf("the emphasis markers are still in the label: %q", head)
	}

	if strings.Contains(strings.Join(got[0].Lines, "\n"), "coderabbit:state") {
		t.Error("the routing metadata reached the reader")
	}

	if got[3].Summary != "🔎 Supported by static analysis" {
		t.Errorf("the section folds to %q", got[3].Summary)
	}

	inner := kinds(got[3].Blocks)
	if want := []ghmd.Kind{ghmd.Prose, ghmd.Code, ghmd.Prose, ghmd.Rule, ghmd.Prose}; !same(inner, want) {
		t.Errorf("the section holds %v, wanted %v", inner, want)
	}

	fence := got[3].Blocks[1]
	if fence.Lang != "shell" {
		t.Errorf("the fence is tagged %q", fence.Lang)
	}

	if want := "sed -n '88,110p' .github/ACTIONS.md"; fence.Lines[2] != want {
		t.Errorf("the fence lost its content: %q", fence.Lines[2])
	}
}

// The cases a body has to survive without losing a line, each of which a
// stricter reading of markdown gets wrong on text bots really post.
func TestNothingIsSwallowedByAShapeItDoesNotHave(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		body  string
		kinds []ghmd.Kind
		first string
	}{
		{
			name:  "an unclosed fence runs to the end",
			body:  "before\n\n```go\nfunc main() {}",
			kinds: []ghmd.Kind{ghmd.Prose, ghmd.Code},
			first: "before",
		},
		{
			name:  "a fence inside a longer fence is content",
			body:  "````\n```go\nx := 1\n```\n````",
			kinds: []ghmd.Kind{ghmd.Code},
			first: "```go",
		},
		{
			name:  "an underscore inside a word is a word",
			body:  "the value of read_budget_total is wrong",
			kinds: []ghmd.Kind{ghmd.Prose},
			first: "the value of read_budget_total is wrong",
		},
		{
			name:  "a marker with no closer is a character",
			body:  "2 * 3 is six",
			kinds: []ghmd.Kind{ghmd.Prose},
			first: "2 * 3 is six",
		},
		{
			name:  "emphasis inside backticks is code",
			body:  "call `wrap(*_body_)` first",
			kinds: []ghmd.Kind{ghmd.Prose},
			first: "call `wrap(*_body_)` first",
		},
		{
			name:  "a quote loses its markers and keeps its words",
			body:  "> it returns a bare error\n> so the caller cannot tell",
			kinds: []ghmd.Kind{ghmd.Quote},
			first: "it returns a bare error",
		},
		{
			name:  "a section with no summary still holds its blocks",
			body:  "<details>\n\nplain enough\n</details>",
			kinds: []ghmd.Kind{ghmd.Details},
			first: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ghmd.Parse(tc.body)
			if !same(kinds(got), tc.kinds) {
				t.Fatalf("read as %v, wanted %v", kinds(got), tc.kinds)
			}

			if tc.first == "" {
				return
			}

			if len(got[0].Lines) == 0 || got[0].Lines[0] != tc.first {
				t.Errorf("first line is %q, wanted %q", got[0].Lines, tc.first)
			}
		})
	}
}

func same(a, b []ghmd.Kind) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
