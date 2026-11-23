package brief_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/kyleking/aragonite/diff"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/brief"
	"github.com/kyleking/second-look/internal/threads"
)

// binary is a file the diff carries no lines for, which prints its note.
const binary = "diff --git a/logo.png b/logo.png\nBinary files a/logo.png and b/logo.png differ\n"

// The blank context line is a single space, so the patch is built from a list
// rather than written as a literal: a fixer strips a trailing space and the
// diff would stop parsing.
var patch = strings.Join([]string{
	"diff --git a/pkg/read.go b/pkg/read.go",
	"--- a/pkg/read.go",
	"+++ b/pkg/read.go",
	"@@ -1,4 +1,5 @@",
	" package pkg",
	" ",
	"-func Read() error { return nil }",
	"+func Read(path string) error {",
	"+\treturn open(path)",
	"+}",
	"",
}, "\n")

func review() *artifact.Review {
	return &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "acme", Repo: "widget", Number: 7,
		Comments: []artifact.Comment{{
			ID: "wrap", Path: "pkg/read.go", Line: 4, Side: artifact.SideRight,
			Body: "This drops the error.", Note: "ran the suite",
			Severity: "major", Status: artifact.StatusReady,
		}, {
			ID: "gone", Path: "pkg/read.go", Line: 3, Side: artifact.SideLeft,
			Body: "Why did this go?", Severity: "question", Status: artifact.StatusDraft,
		}},
	}
}

// lines joins expected output, so a line ending in a space survives the
// whitespace fixers.
func lines(ls ...string) string { return strings.Join(ls, "\n") }

// The marked diff is the change and what is said about it as one thing, with a
// comment on a removed line marked where it was removed rather than beside
// whatever now sits at that number, and a file with no lines saying why.
func TestDiffMarksEveryComment(t *testing.T) {
	t.Parallel()

	want := lines(
		"pkg/read.go",
		"  @@ -1,4 +1,5 @@",
		"      1   package pkg",
		"      2   ",
		"      3 - func Read() error { return nil }",
		"    <<< gone  question/draft",
		"      3 + func Read(path string) error {",
		"      4 + \treturn open(path)",
		"    <<< wrap  major/ready",
		"      5 + }",
		"",
		"logo.png",
		"  binary",
		"",
		"",
	)

	if got := brief.Diff(diff.Parse([]byte(patch+binary)), review(), nil); got != want {
		t.Errorf("the marked diff reads\n%s\nwant\n%s", got, want)
	}
}

// A reply that anchors nowhere of its own is marked on the line its thread
// hangs from.
func TestDiffMarksAReplyOnItsThreadsLine(t *testing.T) {
	t.Parallel()

	r := review()
	r.Comments = append(r.Comments, artifact.Comment{
		ID: "reply", InReplyTo: 91, Body: "Fixed in the next push.",
		Status: artifact.StatusReady,
	})

	open := []threads.Thread{{
		Path: "pkg/read.go", Side: artifact.SideRight, Line: 4,
		Notes: []threads.Note{{ID: 91, Author: "kyleking", Body: "Does this drop the error?"}},
	}}

	out := brief.Diff(diff.Parse([]byte(patch)), r, open)
	if !strings.Contains(out, "      4 + \treturn open(path)\n    <<< wrap  major/ready\n    <<< reply  ready\n") {
		t.Errorf("the anchorless reply is not marked under its thread's line:\n%s", out)
	}
}

// Comment is the whole context of one finding: the note that never leaves the
// laptop is labeled rather than merged in, the exchange about it follows, and
// a window narrows the hunk to the lines around the anchor on its own side.
func TestComment(t *testing.T) {
	t.Parallel()

	r := review()
	r.Comments[0].Turns = []artifact.Turn{{Author: "agent", Body: "wrapped it"}, {Author: "kyleking", Body: "thanks"}}
	r.Comments[1].SkipReason = "out of scope"

	wrapHead := lines(
		"wrap  major/ready  pkg/read.go RIGHT 4",
		"",
		"  This drops the error.",
		"",
		"NOTE (never posted)",
		"  ran the suite",
		"",
		"TURNS",
		"  @agent",
		"    wrapped it",
		"  @kyleking",
		"    thanks",
		"",
		"pkg/read.go",
		"  @@ -1,4 +1,5 @@",
		"",
	)
	goneHead := lines(
		"gone  question/draft  pkg/read.go LEFT 3",
		"",
		"  Why did this go?",
		"",
		"SKIPPED: out of scope",
		"",
		"pkg/read.go",
		"  @@ -1,4 +1,5 @@",
		"",
	)

	for _, tc := range commentCases(wrapHead, goneHead) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := brief.Comment(r, tc.id, diff.Parse([]byte(patch)), nil, tc.around)
			if err != nil {
				t.Fatal(err)
			}

			if got != tc.want {
				t.Errorf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}

	t.Run("an unknown id", func(t *testing.T) {
		t.Parallel()

		if _, err := brief.Comment(r, "nope", diff.Parse([]byte(patch)), nil, 0); !errors.Is(err, brief.ErrNoComment) {
			t.Errorf("an unknown id answered %v", err)
		}
	})

	t.Run("a line the diff no longer carries", func(t *testing.T) {
		t.Parallel()

		moved := review()
		moved.Comments[0].Line = 40

		got, err := brief.Comment(moved, "wrap", diff.Parse([]byte(patch)), nil, 0)
		if err != nil ||
			!strings.HasSuffix(got, "\nThe diff does not carry this line; the head has moved under it.\n") {
			t.Errorf("a moved anchor read %q (%v)", got, err)
		}
	})
}

// commentCase is one comment asked for in full or in a window around it.
type commentCase struct {
	name   string
	id     string
	around int
	want   string
}

func commentCases(wrapHead, goneHead string) []commentCase {
	return []commentCase{
		{"the whole hunk", "wrap", 0, wrapHead + lines(
			"      1   package pkg",
			"      2   ",
			"      3 - func Read() error { return nil }",
			"      3 + func Read(path string) error {",
			">>    4 + \treturn open(path)",
			"      5 + }",
			"",
		)},
		{"a line either side", "wrap", 1, wrapHead + lines(
			"      3 + func Read(path string) error {",
			">>    4 + \treturn open(path)",
			"      5 + }",
			"",
		)},
		{"a removed line", "gone", 0, goneHead + lines(
			"      1   package pkg",
			"      2   ",
			">>    3 - func Read() error { return nil }",
			"      3 + func Read(path string) error {",
			"      4 + \treturn open(path)",
			"      5 + }",
			"",
		)},
		{"a removed line's window counts old lines", "gone", 1, goneHead + lines(
			"      2   ",
			">>    3 - func Read() error { return nil }",
			"",
		)},
	}
}

// Owed is every todo comment as one document, each one under a rule, and
// nothing past the count when none is waiting.
func TestOwed(t *testing.T) {
	t.Parallel()

	r := review()
	r.HeadSHA = "abc123"

	if got := brief.Owed(
		r,
		diff.Parse([]byte(patch)),
		nil,
	); got != "acme/widget#7 at abc123\n0 comment(s) waiting on you.\n" {
		t.Errorf("nothing owed reads %q", got)
	}

	r.Comments[1].Status = artifact.StatusTodo
	got := brief.Owed(r, diff.Parse([]byte(patch)), nil)

	one, err := brief.Comment(r, "gone", diff.Parse([]byte(patch)), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(
		got,
		"acme/widget#7 at abc123\n1 comment(s) waiting on you.\n\nAnswer each one with a turn",
	) ||
		!strings.Contains(got, "    second-look comment add acme/widget#7 <<'JSON'\n") ||
		!strings.HasSuffix(got, "\n\n"+strings.Repeat("=", 72)+"\n"+one+"\n") ||
		strings.Contains(got, "wrap  major") {
		t.Errorf("one owed comment reads\n%s", got)
	}
}

// A reply is only readable beside what it answers, so the cached thread is
// printed with it, on its thread's line and hunk since it carries no anchor of
// its own, and a thread that is no longer cached says so rather than leaving
// the reply looking like it addresses nothing.
func TestCommentShowsTheThreadItAnswers(t *testing.T) {
	t.Parallel()

	r := review()
	r.Comments = append(r.Comments, artifact.Comment{
		ID: "reply", InReplyTo: 91, Body: "Fixed in the next push.",
		Status: artifact.StatusReady,
	})

	open := []threads.Thread{{
		Path: "pkg/read.go", Side: artifact.SideRight, Line: 4,
		Notes: []threads.Note{{ID: 91, Author: "kyleking", Body: "Does this drop the error?"}},
	}}

	out, err := brief.Comment(r, "reply", diff.Parse([]byte(patch)), open, 0)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"reply  ready  pkg/read.go RIGHT 4\n", ">>    4 + \treturn open(path)\n",
		"IT ANSWERS", "@kyleking", "Does this drop the error?",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q is missing from:\n%s", want, out)
		}
	}

	if strings.Contains(out, "head has moved") {
		t.Errorf("a reply on a line the diff carries read as moved:\n%s", out)
	}

	out, err = brief.Comment(r, "reply", diff.Parse([]byte(patch)), nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "not in the cached threads") || strings.Contains(out, "head has moved") {
		t.Errorf("a reply with no cached thread said nothing about it:\n%s", out)
	}
}

func TestFixesListsEachTodoReplyWithItsFinding(t *testing.T) {
	t.Parallel()

	r := review()
	r.HeadSHA = "abc123"
	r.Comments = []artifact.Comment{
		{ID: "reply-1", InReplyTo: 1, Body: "will fix: wrap it", Status: artifact.StatusTodo},
		{ID: "reply-2", InReplyTo: 2, Body: "thanks", Status: artifact.StatusReady},
		{ID: "own", Path: "pkg/read.go", Line: 4, Body: "my own note", Status: artifact.StatusTodo},
	}

	ts := []threads.Thread{
		{Path: "pkg/read.go", Line: 4, Notes: []threads.Note{{ID: 1, Author: "doggo", Body: "Wrap the error."}}},
		{Path: "pkg/other.go", Line: 9, Notes: []threads.Note{{ID: 2, Author: "doggo", Body: "Rename."}}},
	}

	want := `acme/widget#7 at abc123
1 review finding the author agreed to fix. The replies are already posted.

1. pkg/read.go:4
finding:
  Wrap the error.
reply:
  will fix: wrap it
`
	if got := brief.Fixes(r, ts); got != want {
		t.Errorf("prompt =\n%s\nwant\n%s", got, want)
	}
}
