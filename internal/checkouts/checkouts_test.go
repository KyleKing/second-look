package checkouts_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/checkouts"
)

// errMissing stands in for the extension not being installed.
var errMissing = errors.New("exec: \"gh\": executable file not found in $PATH")

// answerer replays one dashboard run, and records what it was asked, since
// asking for the network or for a single directory would defeat the point of
// reading its cache.
type answerer struct {
	body []byte
	err  error
	args []string
}

func (a *answerer) Run(_ context.Context, args ...string) ([]byte, error) {
	a.args = args

	return a.body, a.err
}

func fleet(t *testing.T) []byte {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "fleet.json"))
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	return raw
}

// The ranking is the whole value of asking: several clones of one remote, and
// the order says which one costs least to review in.
func TestFindRanksEveryCloneAndWorktree(t *testing.T) {
	t.Parallel()

	a := &answerer{body: fleet(t)}

	got, err := checkouts.Find(t.Context(), a, "acme/app", "fix/the-thing")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		// The worktree already standing on the pull request head comes first,
		// then the clean clone, then the dirty checkout that would need a stash.
		"/home/me/Developer/acme/app-wt",
		"/home/me/Developer/acme/app-clone",
		"/home/me/Developer/acme/app",
	}

	if len(got) != len(want) {
		t.Fatalf("found %+v, want %d of them", got, len(want))
	}

	for i := range want {
		if got[i].Path != want[i] {
			t.Errorf("candidate %d is %s, want %s", i, got[i].Path, want[i])
		}
	}

	if !got[0].Worktree {
		t.Error("the worktree is not marked as one, so a reader cannot tell what they are picking")
	}

	if strings.Contains(strings.Join(a.args, " "), "--fresh") {
		t.Errorf("asked %v, which reaches the network for data the cache already has", a.args)
	}
}

// What Find answers short of a ranking: an empty list for a repository with no
// clone here, which is the cross-repository case; ErrNoRemotes for an older
// dashboard that prints no remote at all, since reporting no clone for a
// repository sitting right there would be wrong; and the install command where
// the extension is missing.
func TestFindShortOfARanking(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, repo string
		a          *answerer
		is         error
		says       string
	}{
		{name: "no clone", repo: "acme/unrelated", a: &answerer{body: fleet(t)}},
		{
			name: "no remotes", repo: "acme/app",
			a:  &answerer{body: []byte(`{"repos":[{"path":"/home/me/Developer/acme/app","branch":"main"}]}`)},
			is: checkouts.ErrNoRemotes,
		},
		{
			name: "no extension", repo: "acme/app", a: &answerer{err: errMissing},
			is: errMissing, says: "gh extension install",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := checkouts.Find(t.Context(), tc.a, tc.repo, "")
			if len(got) != 0 || !errors.Is(err, tc.is) || (err != nil) != (tc.is != nil) {
				t.Fatalf("found %+v and %v, want nothing and %v", got, err, tc.is)
			}

			if tc.says != "" && !strings.Contains(err.Error(), tc.says) {
				t.Errorf("answered %v, want it to name %q", err, tc.says)
			}
		})
	}
}
