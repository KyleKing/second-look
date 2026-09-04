package tui_test

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/threads"
	"github.com/kyleking/second-look/internal/tui"
)

// botBody is what a review bot posts: routing metadata in an HTML comment, a
// labeled first line, and a collapsed section holding the script it ran. Read
// as raw text it is four screens between one line of code and the next.
const botBody = `<!-- coderabbit:state {"thread":"abc"} -->
_🩺 Stability_ | _🟠 Major_

split can fail now, so this has to check err.

<details>
<summary>Supported by static analysis</summary>

` + "```shell" + `
#!/bin/bash
set -eu
rg -n 'split\(' internal/vcs
sed -n '1,20p' internal/vcs/diff.go
printf '%s\n' 'checking the callers'
go build ./...
go vet ./internal/vcs
echo done
echo really done
echo the last line
echo and one more
` + "```" + `

Length of output: 47492
</details>`

func botModel(t *testing.T) *tui.Model {
	t.Helper()

	open := threads.Thread{
		Path: parsed, Side: artifact.SideRight, Line: 15,
		Notes: []threads.Note{{ID: 77, Author: "coderabbitai", Body: botBody}},
	}

	_, path, _ := fixtureWith(t, patch)
	m := tui.New(t.Context(), reviewAt(t, path), diff.Parse([]byte(patch)), path,
		func(context.Context, *artifact.Review) (string, error) { return "", nil },
		tui.WithThreads([]threads.Thread{open}))
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	return m
}

// The whole point of segmenting a body is that the screen shows what somebody
// said and holds back the machinery around it, without either being lost.
func TestABotsCommentArrivesFoldedAndOpensWhereItIsAsked(t *testing.T) {
	t.Parallel()

	m := botModel(t)
	frame := plain(m.Frame())

	for _, gone := range []string{"coderabbit:state", "<details>", "<summary>", "#!/bin/bash", "_🩺"} {
		if strings.Contains(frame, gone) {
			t.Errorf("%q reached the screen:\n%s", gone, frame)
		}
	}

	for _, want := range []string{"🩺 Stability | 🟠 Major", "split can fail now", "▸ Supported by static analysis"} {
		if !strings.Contains(frame, want) {
			t.Errorf("%q is missing:\n%s", want, frame)
		}
	}
}

func TestOpeningASectionSemiFoldsTheScriptInside(t *testing.T) {
	t.Parallel()

	m := botModel(t)
	onto(t, m, "Supported by static analysis")
	go2(m, 'z', 'a')

	frame := plain(m.Frame())
	if !strings.Contains(frame, "shell · 11 lines") {
		t.Fatalf("the section did not open onto its script:\n%s", frame)
	}

	// The head of the script says what it does; the rest waits, which is the
	// whole difference between a fold and a semi-fold.
	if !strings.Contains(frame, "#!/bin/bash") || !strings.Contains(frame, "5 more lines") {
		t.Errorf("the script is not drawn to its head:\n%s", frame)
	}

	if strings.Contains(frame, "echo the last line") {
		t.Errorf("the whole script was drawn:\n%s", frame)
	}

	onto(t, m, "shell · 11 lines")
	go2(m, 'z', 'a')

	if frame := plain(m.Frame()); !strings.Contains(frame, "echo the last line") {
		t.Errorf("za did not open the script:\n%s", frame)
	}
}
