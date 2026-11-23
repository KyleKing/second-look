package notice_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kyleking/second-look/internal/ghrun"
	"github.com/kyleking/second-look/internal/notice"
)

// The fixture names invented repositories, since the real stream carries
// private ones and this repository is public. What it pins is the decode:
// the subject's pull request or issue number lives at the tail of its API
// URL, and a check suite's tail is a number that names nothing to open.
func TestDecode(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "notifications.json"))
	if err != nil {
		t.Fatal(err)
	}

	threads, err := notice.Decode(raw)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	if len(threads) != 3 {
		t.Fatalf("read %d notifications, want 3", len(threads))
	}

	first := threads[0]
	if first.Repo != "acme/widgets" || first.Number != 42 || first.Comment != 555 {
		t.Errorf("the pull request decoded as %+v", first)
	}

	if got := first.Where(); got != "acme/widgets#42" {
		t.Errorf("Where is %q", got)
	}

	if got := first.Key(); got != "n:1001" {
		t.Errorf("Key is %q", got)
	}

	last := threads[2]
	if last.Number != 0 {
		t.Errorf("a check suite's tail is not a number gh browse can open: %+v", last)
	}

	if _, err := notice.Decode([]byte("{")); err == nil {
		t.Error("Decode read a truncated answer")
	}

	if _, err := notice.Decode([]byte(`[{"id":"1","updated_at":"yesterday"}]`)); err == nil ||
		!strings.Contains(err.Error(), "when") {
		t.Errorf("a bad timestamp named nothing: %v", err)
	}
}

// recorder is a gh that keeps the calls it was asked to make, and refuses the
// one numbered failAt (from 1) where that is set.
type recorder struct {
	runs   [][]string
	failAt int
}

var errRefused = errors.New("refused")

func (r *recorder) Run(_ context.Context, _ string, args ...string) error {
	r.runs = append(r.runs, args)
	if len(r.runs) == r.failAt {
		return errRefused
	}

	return nil
}

var _ ghrun.Runner = (*recorder)(nil)

// Done deletes the thread; Ignore subscribes it out and then deletes it, which
// is the one shape where order between the calls matters. A refusal on the
// unsubscribe must stop the mark done, or the row leaves the stream while its
// mail keeps coming.
func TestDoneAndIgnore(t *testing.T) {
	t.Parallel()

	n := notice.Thread{ID: "1001", Repo: "acme/widgets", Number: 42}
	unsubscribe := []string{"api", "-X", "PUT", "/notifications/threads/1001/subscription", "-f", "ignored=true"}
	dismiss := []string{"api", "-X", "DELETE", "/notifications/threads/1001"}

	for _, tc := range []struct {
		name   string
		act    func(context.Context, ghrun.Runner, notice.Thread) error
		failAt int
		want   [][]string
		err    error
	}{
		{"done", notice.Done, 0, [][]string{dismiss}, nil},
		{"done refused", notice.Done, 1, [][]string{dismiss}, errRefused},
		{"ignore", notice.Ignore, 0, [][]string{unsubscribe, dismiss}, nil},
		{"ignore, unsubscribe refused", notice.Ignore, 1, [][]string{unsubscribe}, errRefused},
		{"ignore, dismiss refused", notice.Ignore, 2, [][]string{unsubscribe, dismiss}, errRefused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gh := &recorder{failAt: tc.failAt}
			if err := tc.act(context.Background(), gh, n); !errors.Is(err, tc.err) {
				t.Errorf("answered %v, want %v", err, tc.err)
			}

			if !slices.EqualFunc(gh.runs, tc.want, slices.Equal) {
				t.Errorf("made %v, want %v", gh.runs, tc.want)
			}
		})
	}
}
