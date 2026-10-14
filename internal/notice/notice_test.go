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
}

// run is what the test's fake gh saw, so Done and Ignore can be checked
// against the calls GitHub is owed rather than against a string of their own.
type run struct {
	args []string
}

type recorder struct{ runs []run }

func (r *recorder) Run(_ context.Context, _ string, args ...string) error {
	r.runs = append(r.runs, run{args: args})

	return nil
}

var _ ghrun.Runner = (*recorder)(nil)

// Done deletes the thread; Ignore subscribes it out and then deletes it, which
// is the one shape where order between the calls matters.
func TestIgnoreUnsubscribesThenDismisses(t *testing.T) {
	t.Parallel()

	gh := &recorder{}
	n := notice.Thread{ID: "1001", Repo: "acme/widgets", Number: 42}

	if err := notice.Ignore(context.Background(), gh, n); err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"api", "-X", "PUT", "/notifications/threads/1001/subscription", "-f", "ignored=true"},
		{"api", "-X", "DELETE", "/notifications/threads/1001"},
	}

	if len(gh.runs) != len(want) {
		t.Fatalf("made %d calls: %v", len(gh.runs), gh.runs)
	}

	for i, r := range gh.runs {
		if !slices.Equal(r.args, want[i]) {
			t.Errorf("call %d was %v, want %v", i, r.args, want[i])
		}
	}
}

// A refusal on the unsubscribe must stop the mark done, or the row leaves the
// stream while its mail keeps coming.
func TestIgnoreStopsWhenUnsubscribingFails(t *testing.T) {
	t.Parallel()

	gh := &failure{}
	n := notice.Thread{ID: "1001", Repo: "acme/widgets", Number: 42}

	if err := notice.Ignore(context.Background(), gh, n); err == nil {
		t.Fatal("Ignore swallowed the unsubscribe refusal")
	}
}

type failure struct{}

var errRefused = errors.New("refused")

func (failure) Run(context.Context, string, ...string) error {
	return errRefused
}

var _ ghrun.Runner = failure{}

func TestDecodeRejectsBadJSON(t *testing.T) {
	t.Parallel()

	if _, err := notice.Decode([]byte("{")); err == nil {
		t.Fatal("Decode read a truncated answer")
	}

	if _, err := notice.Decode([]byte(`[{"id":"1","updated_at":"yesterday"}]`)); err == nil ||
		!strings.Contains(err.Error(), "when") {
		t.Fatalf("a bad timestamp named nothing: %v", err)
	}
}
