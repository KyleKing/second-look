package tui_test

import (
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/tui"
)

// The pane is not a browser, so the marks an agent writes get drawn rather
// than spelled out: emphasis and code lose their syntax, a heading loses its
// hashes, and a mark that never closes stays literal rather than being
// guessed at.
func TestAgentMarkdownDrawsMarksRatherThanSpellingThem(t *testing.T) {
	t.Parallel()

	// A short line keeps nothing but the text the marks wrapped: substring
	// checks alone would pass on leftover syntax or doubled words.
	got := tui.MarkdownLines("pre **bold** mid `code`, post", 80)
	if len(got) != 1 || plain(got[0]) != "pre bold mid code, post" {
		t.Fatalf("the line kept syntax, doubled, or split text: %q", plain(got[0]))
	}

	text := "**Staged:** `minor` at `email_setup.py:313`\n" +
		"- the `**bold**` thing and *slanted* and ~~gone~~\n" +
		"see [the diff](https://example.com/diff) here\n" +
		"## A heading\n" +
		"```\nraw `code` **kept** literal\n```\n" +
		"---\n" +
		"snake_case stays, **unclosed stays"

	got = tui.MarkdownLines(text, 60)

	var rendered strings.Builder
	for _, l := range got {
		rendered.WriteString(l + "\n")
	}

	if !strings.Contains(rendered.String(), "\x1b[") {
		t.Fatalf("no line drew a face:\n%q", rendered.String())
	}

	text = plain(rendered.String())
	for _, want := range []string{
		"Staged:", "minor", "email_setup.py:313", "bold", "slanted", "gone",
		"the diff", "A heading", "raw `code` **kept** literal",
		"snake_case stays, **unclosed stays",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the render lost %q:\n%s", want, text)
		}
	}

	for _, gone := range []string{"https://example.com", "**Staged:**"} {
		if strings.Contains(text, gone) {
			t.Errorf("the render kept the syntax %q:\n%s", gone, text)
		}
	}

	// The fenced line is one code chip; the rule is a drawn line, not dashes.
	if !strings.Contains(rendered.String(), "─") {
		t.Errorf("the rule did not draw:\n%s", rendered.String())
	}
}

// A wrapped bullet keeps its hang under styled spans, so a long item still
// reads as one item and not a paragraph with an asterisk in it.
func TestAgentMarkdownKeepsAListHang(t *testing.T) {
	t.Parallel()

	got := tui.MarkdownLines("- a finding **with emphasis** that wraps well past the width given", 30)

	if len(got) < 2 {
		t.Fatalf("the item did not wrap:\n%v", got)
	}

	if !strings.HasPrefix(plain(got[0]), "- a finding") {
		t.Errorf("the item's first line lost its marker: %q", plain(got[0]))
	}

	if !strings.HasPrefix(plain(got[1]), "  ") {
		t.Errorf("the wrapped line does not hang under the item's text: %q", plain(got[1]))
	}
}
