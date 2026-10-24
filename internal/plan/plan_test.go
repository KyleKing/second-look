package plan_test

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/order"
	"github.com/kyleking/second-look/internal/plan"
)

// shown renders chapters the way a reader sees them, so a test asserts on the
// order and the modes rather than on the shape they are carried in.
func shown(chapters []plan.Chapter) string {
	var b strings.Builder

	for _, c := range chapters {
		fmt.Fprintf(&b, "%s %s %s:", c.Source.Glyph(), c.Mode, c.Name)

		for _, h := range c.Hunks {
			fmt.Fprintf(&b, " %s#%d", h.Path, h.Hunk)
		}

		b.WriteString("\n")
	}

	return b.String()
}

func hunk(path string, n int) order.Hunk {
	dir := "."
	if at := strings.LastIndex(path, "/"); at > 0 {
		dir = path[:at]
	}

	return order.Hunk{Ref: order.Ref{Path: path, Hunk: n}, Dir: dir}
}

func ref(path string, n int) order.Ref { return order.Ref{Path: path, Hunk: n} }

// diff is one change in the shapes every layer below reads: code in two
// directories, its tests, a migration, and a lockfile a machine wrote.
func diff() []order.Hunk {
	lock := hunk("go.sum", 6)
	lock.Made = true

	return []order.Hunk{
		hunk("models/user.go", 1),
		hunk("migrations/001.sql", 2),
		hunk("api/handler.go", 3),
		hunk("api/handler_test.go", 4),
		hunk("models/user.go", 5),
		lock,
	}
}

func clean() []plan.Commit {
	return []plan.Commit{
		{ID: "aaaaaaaa1", Subject: "feat: add the user model", Hunks: []order.Ref{
			ref("models/user.go", 1), ref("migrations/001.sql", 2), ref("go.sum", 6),
		}},
		{ID: "bbbbbbbb2", Subject: "feat: serve users", Hunks: []order.Ref{
			ref("api/handler.go", 3), ref("api/handler_test.go", 4),
		}},
		{ID: "cccccccc3", Subject: "feat: name users", Hunks: []order.Ref{ref("models/user.go", 5)}},
	}
}

func TestBuildCutsByCommitOnlyWhenTheHistoryIsClean(t *testing.T) {
	t.Parallel()

	byCommit := `⊙ read feat: add the user model: models/user.go#1 migrations/001.sql#2
⇢ fold feat: add the user model · generated: go.sum#6
⊙ read feat: serve users: api/handler.go#3 api/handler_test.go#4
⊙ read feat: name users: models/user.go#5
`

	for _, tc := range []struct {
		name    string
		commits func([]plan.Commit) []plan.Commit
		basis   string
	}{
		{"clean commits are the chapters", func(c []plan.Commit) []plan.Commit { return c }, "by commit: 3 commits"},
		{"one commit is the diff again", func(c []plan.Commit) []plan.Commit { return c[:1] }, "one commit"},
		{"a hunk no commit wrote", func(c []plan.Commit) []plan.Commit {
			c[2].Hunks = nil

			return c
		}, "1 hunk(s) no commit in the range wrote"},
		{"review rounds", func(c []plan.Commit) []plan.Commit {
			return append(c,
				plan.Commit{ID: "d", Subject: "fixup! feat: serve users", Hunks: []order.Ref{ref("api/handler.go", 3)}},
				plan.Commit{ID: "e", Subject: "address review comments", Hunks: []order.Ref{ref("models/user.go", 5)}})
		}, "2 of 5 subjects are fixups or review rounds"},
		{"one commit touching everything", func(c []plan.Commit) []plan.Commit {
			all := plan.Commit{ID: "f", Subject: "style: reformat"}
			for _, h := range diff() {
				all.Hunks = append(all.Hunks, h.Ref)
			}

			return append(c, all)
		}, "4 of 4 commits own no hunk another did not also touch"},
		{"too many commits", func(c []plan.Commit) []plan.Commit {
			for i := range 14 {
				c = append(c, plan.Commit{ID: strconv.Itoa(i), Subject: "chore: more", Hunks: c[0].Hunks[:1]})
			}

			return c
		}, "17 commits, past 15"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := plan.Build(plan.Input{Hunks: diff(), Commits: tc.commits(clean())})

			if !strings.Contains(p.Basis, tc.basis) {
				t.Errorf("the basis is %q, want it to say %q", p.Basis, tc.basis)
			}

			got := shown(p.Chapters)
			if fromCommits := strings.HasPrefix(p.Basis, "by commit"); fromCommits != (got == byCommit) {
				t.Errorf("the chapters do not match the basis %q:\n%s", p.Basis, got)
			}
		})
	}
}

// A pull request whose commits this checkout cannot map reads by symbol and
// directory, and the plan says which and why.
func TestBuildNamesWhyItHadNoCommits(t *testing.T) {
	t.Parallel()

	p := plan.Build(plan.Input{Hunks: diff(), NoCommits: "the commits are not in this checkout"})

	if p.Basis != "by symbol and directory: the commits are not in this checkout" {
		t.Errorf("the basis is %q", p.Basis)
	}

	if got, want := shown(p.Chapters)[:len("⇢ read models")], "⇢ read models"; got != want {
		t.Errorf("the first chapter is %q, want the directory group", got)
	}
}

func TestRulesFoldOrderAndMode(t *testing.T) {
	t.Parallel()

	const (
		models     = "⇢ read models: models/user.go#1 models/user.go#5\n"
		migrations = "⇢ read migrations: migrations/001.sql#2\n"
		api        = "⇢ read api: api/handler.go#3 api/handler_test.go#4\n"
		lockfile   = "⇢ fold generated: go.sum#6\n"
	)

	for _, tc := range []struct {
		name  string
		rules plan.Rules
		want  string
	}{
		{
			name: "what a machine wrote folds with no rules at all",
			want: models + migrations + api + lockfile,
		},
		{
			name:  "a fold glob covering a whole chapter folds it",
			rules: plan.Rules{Fold: []string{"migrations/**"}},
			want:  models + "§ fold migrations: migrations/001.sql#2\n" + api + lockfile,
		},
		{
			name:  "never fold wins over a fold and over what a machine wrote",
			rules: plan.Rules{Fold: []string{"*.sql"}, NeverFold: []string{"migrations/*", "go.sum"}},
			want:  models + migrations + api + "⇢ read generated: go.sum#6\n",
		},
		{
			name:  "a mode covering part of a chapter splits it in place",
			rules: plan.Rules{Modes: []plan.ModeRule{{Glob: "**/*_test.go", Mode: plan.ModeSkim}}},
			want: models + migrations + "⇢ read api: api/handler.go#3\n" +
				"§ skim api · **/*_test.go: api/handler_test.go#4\n" + lockfile,
		},
		{
			name:  "an after rule moves a chapter behind the last it names",
			rules: plan.Rules{After: []plan.After{{Read: "models/**", After: "api/**"}}},
			want:  migrations + api + models + lockfile,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := plan.Build(plan.Input{Hunks: diff(), Rules: tc.rules})
			if got := shown(p.Chapters); got != tc.want {
				t.Errorf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestOverlayMovesRenamesAndReports(t *testing.T) {
	t.Parallel()

	p := plan.Build(plan.Input{
		Hunks: diff(), Commits: clean(),
		Overlay: plan.Overlay{
			Claim: "users get a name",
			Chapters: []plan.Edit{
				{Chapter: "feat: name users", Name: "naming", Mode: plan.ModeShape, Source: plan.SourceAgent},
				{Chapter: "the schema", Hunks: []order.Ref{ref("migrations/001.sql", 2)}, Source: plan.SourceYou},
				{Chapter: "gone", Mode: plan.ModeSkim, Source: plan.SourceAgent},
				{Chapter: "naming", Feature: "names", Source: plan.SourceAgent},
			},
			Stops: []plan.Stop{
				{Ref: ref("api/handler.go", 3), What: "the entry point", Source: plan.SourceAgent},
				{Ref: ref("api/handler.go", 99), What: "a hunk the head lost", Source: plan.SourceAgent},
			},
		},
	})

	want := `✎ read the schema: migrations/001.sql#2
⊙ read feat: add the user model: models/user.go#1
⇢ fold feat: add the user model · generated: go.sum#6
⊙ read feat: serve users: api/handler.go#3 api/handler_test.go#4
◆ shape naming: models/user.go#5
`
	if got := shown(p.Chapters); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}

	if p.Claim != "users get a name" || len(p.Stops) != 1 {
		t.Errorf("the claim is %q and %d stop(s) stand", p.Claim, len(p.Stops))
	}

	if len(p.Features) != 1 || p.Features[0].Name != "names" || p.Features[0].Chapters[0] != 4 {
		t.Errorf("the features are %+v", p.Features)
	}

	for _, want := range []string{`chapter "gone" is not in the plan`, "api/handler.go#99 is not a hunk"} {
		if !strings.Contains(strings.Join(p.Problems, "\n"), want) {
			t.Errorf("%q is not among the problems: %q", want, p.Problems)
		}
	}
}

// TestEveryHunkLandsOnceUnderEveryLayer is the invariant: whichever layers apply, and
// however wrong their input, each hunk is in exactly one chapter.
func TestEveryHunkLandsOnceUnderEveryLayer(t *testing.T) {
	t.Parallel()

	commits := map[string][]plan.Commit{
		"no commits": nil,
		"clean":      clean(),
		"naming a hunk the diff lacks": append(clean(),
			plan.Commit{ID: "z", Subject: "feat: ghost", Hunks: []order.Ref{ref("ghost.go", 42)}}),
	}

	rules := map[string]plan.Rules{
		"no rules": {},
		"every rule": {
			Fold: []string{"*.sql"}, NeverFold: []string{"go.sum"},
			After: []plan.After{{Read: "models/**", After: "api/**"}},
			Modes: []plan.ModeRule{{Glob: "**/*_test.go", Mode: plan.ModeSkim}},
		},
	}

	overlays := map[string]plan.Overlay{
		"no overlay": {},
		"moves, twice and into nothing": {Chapters: []plan.Edit{
			{Chapter: "new", Hunks: []order.Ref{ref("api/handler.go", 3), ref("api/handler.go", 3)}},
			{Chapter: "elsewhere", Hunks: []order.Ref{ref("ghost.go", 7), ref("go.sum", 6)}},
		}},
	}

	for cn, c := range commits {
		for rn, r := range rules {
			for on, o := range overlays {
				t.Run(cn+"/"+rn+"/"+on, func(t *testing.T) {
					t.Parallel()

					placedOnce(t, plan.Build(plan.Input{Hunks: diff(), Commits: c, Rules: r, Overlay: o}))
				})
			}
		}
	}
}

func placedOnce(t *testing.T, p plan.Plan) {
	t.Helper()

	count := map[order.Ref]int{}

	for _, ch := range p.Chapters {
		if len(ch.Hunks) == 0 {
			t.Errorf("chapter %q is empty", ch.Name)
		}

		for _, h := range ch.Hunks {
			count[h]++
		}
	}

	for _, h := range diff() {
		if count[h.Ref] != 1 {
			t.Errorf("%s#%d is in %d chapters:\n%s", h.Path, h.Hunk, count[h.Ref], shown(p.Chapters))
		}
	}

	if len(count) != len(diff()) {
		t.Errorf("the plan carries %d hunks, the diff %d:\n%s", len(count), len(diff()), shown(p.Chapters))
	}
}

func TestRulesValidate(t *testing.T) {
	t.Parallel()

	ok := plan.Rules{
		Repo: "o/r", Fold: []string{"**/*.lock"},
		Modes: []plan.ModeRule{{Glob: "*.md", Mode: plan.ModeSkim}},
	}
	if err := ok.Validate(); err != nil {
		t.Errorf("a sound rule set was refused: %v", err)
	}

	bad := plan.Rules{
		Fold:  []string{"[oops"},
		After: []plan.After{{Read: "a"}},
		Modes: []plan.ModeRule{{Glob: "*.md", Mode: "squint"}},
	}

	err := bad.Validate()
	for _, want := range []error{plan.ErrNoRepo, plan.ErrGlob, plan.ErrAfter, plan.ErrMode} {
		if !errors.Is(err, want) {
			t.Errorf("%v is not reported in %v", want, err)
		}
	}
}

func TestMatch(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		glob, path string
		want       bool
	}{
		{"*.lock", "deep/in/the/tree/yarn.lock", true},
		{"docs/**", "docs/a/b.md", true},
		{"docs/**", "src/docs/b.md", false},
		{"**/migrations/*.sql", "db/migrations/001.sql", true},
		{"**/migrations/*.sql", "migrations/001.sql", true},
		{"cmd/*/main.go", "cmd/app/main.go", true},
		{"cmd/*/main.go", "cmd/app/sub/main.go", false},
	} {
		if got := plan.Match(tc.glob, tc.path); got != tc.want {
			t.Errorf("Match(%q, %q) = %v", tc.glob, tc.path, got)
		}
	}
}
