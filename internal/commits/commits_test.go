package commits_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/commits"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/plan"
)

// backend is one VCS the mapping is asked through: how a step is committed,
// and how the change since the base is diffed and mapped.
type backend struct {
	name   string
	bin    string
	init   func(t *testing.T, dir string)
	commit func(t *testing.T, dir, subject string)
	base   func(t *testing.T, dir string) string
	mapped func(t *testing.T, dir, base string) ([]plan.Commit, error)
}

func backends() []backend {
	return []backend{
		{
			name: "git", bin: "git",
			init: func(t *testing.T, dir string) { t.Helper(); cli(t, dir, "git", "init", "--quiet", "-b", "main") },
			commit: func(t *testing.T, dir, subject string) {
				t.Helper()
				cli(t, dir, "git", "add", "-A")
				cli(t, dir, "git", "commit", "--quiet", "-m", subject)
			},
			base: func(t *testing.T, dir string) string { t.Helper(); return cli(t, dir, "git", "rev-parse", "HEAD") },
			mapped: func(t *testing.T, dir, base string) ([]plan.Commit, error) {
				t.Helper()

				d := diff.Parse([]byte(cli(t, dir, "git", "diff", "--no-ext-diff", "--no-color", base, "HEAD")))

				return commits.Git(t.Context(), dir, base, "HEAD", d)
			},
		},
		{
			name: "jj", bin: "jj",
			init: func(t *testing.T, dir string) { t.Helper(); cli(t, dir, "jj", "git", "init", "--quiet") },
			commit: func(t *testing.T, dir, subject string) {
				t.Helper()
				cli(t, dir, "jj", "describe", "--quiet", "-m", subject)
				cli(t, dir, "jj", "new", "--quiet")
			},
			base: func(t *testing.T, dir string) string {
				t.Helper()

				return cli(t, dir, "jj", "log", "--no-graph", "-r", "@-", "-T", "commit_id")
			},
			mapped: func(t *testing.T, dir, base string) ([]plan.Commit, error) {
				t.Helper()

				d := diff.Parse([]byte(cli(t, dir, "jj", "diff", "--git", "--from", base, "--to", "@")))

				return commits.JJ(t.Context(), dir, base+"..@", d)
			},
		},
	}
}

func lines(from, to int, edit map[int]string) string {
	var b strings.Builder

	for i := from; i <= to; i++ {
		if s, ok := edit[i]; ok {
			b.WriteString(s)

			continue
		}

		fmt.Fprintf(&b, "l%d\n", i)
	}

	return b.String()
}

// Each commit's hunks come back under it however the lines moved: the second
// commit pushes the first's edit down two lines, deletes a file, and edits
// near the end, and each hunk still answers to the commit that wrote it.
func TestHunksAnswerToTheCommitThatWroteThem(t *testing.T) {
	t.Parallel()

	for _, b := range backends() {
		t.Run(b.name, func(t *testing.T) {
			t.Parallel()

			if _, err := exec.LookPath(b.bin); err != nil {
				t.Skipf("%s is not installed", b.bin)
			}

			dir := t.TempDir()
			b.init(t, dir)

			put(t, dir, "a.txt", lines(1, 20, nil))
			put(t, dir, "gone.txt", "bye\n")
			b.commit(t, dir, "base")
			base := b.base(t, dir)

			put(t, dir, "a.txt", lines(1, 20, map[int]string{8: "L8\n"}))
			put(t, dir, "b.txt", "new\n")
			b.commit(t, dir, "feat: one")

			put(t, dir, "a.txt", "top1\ntop2\n"+lines(1, 20, map[int]string{8: "L8\n", 20: "L20\n"}))
			if err := os.Remove(filepath.Join(dir, "gone.txt")); err != nil {
				t.Fatal(err)
			}
			b.commit(t, dir, "feat: two")

			got, err := b.mapped(t, dir, base)
			if err != nil {
				t.Fatal(err)
			}

			want := "feat: one: a.txt#2 b.txt#4\nfeat: two: a.txt#1 a.txt#3 gone.txt#5\n"
			if shown(got) != want {
				t.Errorf("got\n%s\nwant\n%s", shown(got), want)
			}
		})
	}
}

func shown(list []plan.Commit) string {
	var b strings.Builder

	for _, c := range list {
		b.WriteString(c.Subject + ":")

		for _, h := range c.Hunks {
			fmt.Fprintf(&b, " %s#%d", h.Path, h.Hunk)
		}

		b.WriteString("\n")
	}

	return b.String()
}

func put(t *testing.T, dir, name, body string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func cli(t *testing.T, dir, bin string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), bin, args...) // #nosec G204 -- a fixture's own constants
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"JJ_USER=test", "JJ_EMAIL=test@example.com")

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v: %v", bin, args, err)
	}

	return strings.TrimSpace(string(out))
}
