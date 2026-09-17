package tui_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/advisory"
	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/tui"
)

const goSumPatch = `diff --git a/go.sum b/go.sum
index 1111111..2222222 100644
--- a/go.sum
+++ b/go.sum
@@ -1,5 +1,5 @@
 example.com/kept v1.0.0 h1:mmmmmmmm=
-example.com/moved v0.12.0 h1:cccccccc=
-example.com/moved v0.12.0/go.mod h1:dddddddd=
-example.com/left v0.0.20 h1:eeeeeeee=
+example.com/moved v0.13.0 h1:iiiiiiii=
+example.com/moved v0.13.0/go.mod h1:jjjjjjjj=
+example.com/came v0.16.0 h1:kkkkkkkk=
`

const minifiedPatch = `diff --git a/web/app.min.js b/web/app.min.js
index 1111111..2222222 100644
--- a/web/app.min.js
+++ b/web/app.min.js
@@ -1,2 +1,2 @@
-var a=1,b=2;
+var a=1,b=3;
 var c=4;
`

// A lockfile is folded like anything else a machine wrote, and what it says
// while folded is the difference: the dependencies it changed rather than a
// hunk count, and never the hashes, which is the reading the fold exists to
// save.
func TestFoldedGeneratedFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		patch  string
		want   []string
		unwant []string
	}{
		{
			name:  "a lockfile reads as its dependencies",
			patch: goSumPatch,
			want: []string{
				"3 dependencies",
				"example.com/moved", "v0.12.0 → v0.13.0",
				"example.com/came", "added v0.16.0",
				"example.com/left", "removed v0.0.20",
			},
			unwant: []string{"h1:", "example.com/kept"},
		},
		{
			name:   "a format nothing here reads keeps counting hunks",
			patch:  minifiedPatch,
			want:   []string{"1 hunk folded"},
			unwant: []string{"var a=1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m, _, _ := fixtureWith(t, tc.patch)
			frame := plain(m.Frame())

			for _, want := range tc.want {
				if !strings.Contains(frame, want) {
					t.Errorf("the frame does not carry %q:\n%s", want, frame)
				}
			}

			for _, unwant := range tc.unwant {
				if strings.Contains(frame, unwant) {
					t.Errorf("the frame carries %q and should not:\n%s", unwant, frame)
				}
			}
		})
	}
}

// advisor stands in for osv.dev. What the API answers is covered against a
// server in internal/advisory; what is worth driving here is the consent and
// what the screen does with the answer.
type advisor struct {
	notes map[advisory.Package][]advisory.Note
	err   error
	asked [][]advisory.Package
}

func (a *advisor) Ask(_ context.Context, pkgs []advisory.Package) (map[advisory.Package][]advisory.Note, error) {
	a.asked = append(a.asked, pkgs)

	return a.notes, a.err
}

func lockfileWith(t *testing.T, a tui.Advisor) *tui.Model {
	t.Helper()

	path := filepath.Join(t.TempDir(), "pr-42.toml")

	r := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
	}
	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	m := tui.New(t.Context(), r, diff.Parse([]byte(goSumPatch)), path, (&counter{}).post,
		tui.WithAdvisor(a))
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

	// The cursor opens on the review's own body, and the question is asked of
	// the lockfile the cursor is standing on.
	pressKey(m, ']')
	pressKey(m, 'f')

	return m
}

// Every package name in the lockfile leaves the laptop when this is asked, and
// nothing else this screen draws reaches anywhere but GitHub. So the first L
// says what will be sent and asks, and only the second one sends it.
func TestAdvisoriesAreNotAskedUntilTheReaderSaysTwice(t *testing.T) {
	t.Parallel()

	moved := advisory.Package{Ecosystem: "Go", Name: "example.com/moved", Version: "v0.13.0"}
	a := &advisor{notes: map[advisory.Package][]advisory.Note{moved: {{
		ID: "GO-2023-1737", Summary: "a thing is wrong", Severity: "high", Fixed: "v0.14.0",
	}}}}

	m := lockfileWith(t, a)

	if !strings.Contains(plain(m.Frame()), "L to ask osv.dev") {
		t.Errorf("the lockfile does not offer the question:\n%s", plain(m.Frame()))
	}

	pressKey(m, 'L')

	if len(a.asked) != 0 {
		t.Fatalf("one L sent %+v, and nothing should leave on one key", a.asked)
	}

	if !strings.Contains(plain(m.Frame()), "2 Go packages to osv.dev") {
		t.Errorf("the confirmation does not say what it will send:\n%s", plain(m.Frame()))
	}

	pressKey(m, 'L')

	if len(a.asked) != 1 || len(a.asked[0]) != 2 {
		t.Fatalf("the second L sent %+v, want the two versions it moved to", a.asked)
	}

	frame := plain(m.Frame())
	for _, want := range []string{
		"high", "GO-2023-1737", "fixed in v0.14.0", "a thing is wrong",
	} {
		if !strings.Contains(frame, want) {
			t.Errorf("the advisory is missing %q:\n%s", want, frame)
		}
	}

	// The file's own row has to count what was found: an answer of nothing and
	// an answer of six read the same otherwise, and the row is what a reader
	// sees once the file is folded again.
	if !strings.Contains(fileRow(t, frame, "go.sum"), "1 package with something known against it") {
		t.Errorf("the lockfile row does not count what was found:\n%s", frame)
	}
}

func fileRow(t *testing.T, frame, name string) string {
	t.Helper()

	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, name+"  ") {
			return line
		}
	}

	return ""
}

// Being offline, being rate limited, and a private registry all read the same
// way, and a card that went quiet would leave the reader thinking the lockfile
// is clean.
func TestAdvisoriesSayWhenTheQuestionWasRefused(t *testing.T) {
	t.Parallel()

	m := lockfileWith(t, &advisor{err: advisory.ErrRefused})

	pressKey(m, 'L')
	pressKey(m, 'L')

	if !strings.Contains(plain(m.Frame()), "osv.dev: "+advisory.ErrRefused.Error()) {
		t.Errorf("a refused question left the row saying nothing:\n%s", plain(m.Frame()))
	}
}
