package prepared_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kyleking/second-look/internal/prepared"
)

// stage builds a checkout holding the reviews and cache files named, and
// returns its root. The fixtures are cmd/second-look's, so what is counted here
// is the same TOML the end-to-end tests post.
func stage(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	dir := filepath.Join(root, ".second-look")

	if err := os.MkdirAll(filepath.Join(dir, "diff"), 0o750); err != nil {
		t.Fatal(err)
	}

	for name, from := range map[string]string{
		"pr-2.toml":  "reply.toml",
		"pr-42.toml": "staged.toml",
	} {
		body, err := os.ReadFile(filepath.Join("..", "..", "cmd", "second-look", "testdata", "review", from))
		if err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Everything a list has to ignore or report rather than skip: a review that
	// no longer parses, the cache beside it, and a file whose name is not a
	// pull request at all.
	write := func(path, body string) {
		t.Helper()

		if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write("pr-9.toml", "version = 1\nowner = \"broken\"\n")
	write("diff/abc.patch", "not a review")
	write("pr-.toml", "not a review")
	write("pr-0.toml", "not a review")
	write("notes.toml", "not a review")

	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "pr-42.toml"), old, old); err != nil {
		t.Fatal(err)
	}

	return root
}

func TestListReadsEveryStagedReviewNewestFirst(t *testing.T) {
	t.Parallel()

	rows, err := prepared.List(stage(t))
	if err != nil {
		t.Fatal(err)
	}

	got := make(map[int]prepared.Review, len(rows))

	numbers := make([]int, 0, len(rows))
	for i := range rows {
		got[rows[i].Number] = rows[i]
		numbers = append(numbers, rows[i].Number)
	}

	if len(rows) != 3 {
		t.Fatalf("listed %v, want the three pull requests and nothing from the cache", numbers)
	}

	if last := numbers[len(numbers)-1]; last != 42 {
		t.Errorf("the oldest review is #%d, want #42 last", last)
	}

	reply := got[2]
	if reply.Replies == 0 || reply.Repository != "KyleKing/second-look" || reply.Short() == "" {
		t.Errorf("the reply review did not read back: %+v", reply)
	}

	if reply.Blocked() {
		t.Errorf("a review with no draft reads as blocked: %+v", reply)
	}

	staged := got[42]
	if !staged.Blocked() {
		t.Errorf("a review carrying a draft does not block the submit: %+v", staged)
	}

	if staged.Total() != staged.Ready+staged.Draft+staged.Skip || staged.Total() == 0 {
		t.Errorf("the counts do not add up: %+v", staged)
	}

	// A file that no longer parses is the row most worth seeing, and the reason
	// lives past the first line of the failure, which only names the file.
	broken := got[9]
	if broken.Broken == "" || !strings.Contains(broken.Broken, "required") {
		t.Errorf("the unreadable review does not say why: %q", broken.Broken)
	}

	if broken.Where() != "#9" {
		t.Errorf("an unreadable review names itself %q, want #9", broken.Where())
	}
}

// A checkout nobody has staged a review in is not a failure, so the caller can
// tell it apart from a directory it could not read.
func TestListSaysWhenThereIsNoArtifactDirectory(t *testing.T) {
	t.Parallel()

	_, err := prepared.List(t.TempDir())
	if !errors.Is(err, prepared.ErrNoDir) {
		t.Fatalf("listing an empty checkout answered %v, want ErrNoDir", err)
	}
}

func TestWriteSaysWhenNothingIsStaged(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	if err := prepared.Write(&b, nil, time.Now()); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(b.String(), "nothing staged") {
		t.Errorf("an empty list printed %q", b.String())
	}
}

// The row is read by scanning it, so it carries what to do with the review
// before it carries what is in it.
func TestWriteReportsStateAndCounts(t *testing.T) {
	t.Parallel()

	rows, err := prepared.List(stage(t))
	if err != nil {
		t.Fatal(err)
	}

	var b strings.Builder
	if err := prepared.Write(&b, rows, time.Now()); err != nil {
		t.Fatal(err)
	}

	out := b.String()
	for _, want := range []string{"blocked", "unreadable", "1 reply", "1 draft", "@"} {
		if !strings.Contains(out, want) {
			t.Errorf("the listing never says %q:\n%s", want, out)
		}
	}

	if strings.Contains(out, "1 replies") {
		t.Errorf("a single reply is pluralized:\n%s", out)
	}
}
