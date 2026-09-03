package prepared_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kyleking/second-look/internal/prepared"
)

// fixtures is cmd/second-look's review corpus, so what is counted here is the
// same TOML the end-to-end tests post.
var fixtures = filepath.Join("..", "..", "cmd", "second-look", "testdata", "review")

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
		//nolint:gosec // a constant directory and a constant name
		body, err := os.ReadFile(filepath.Join(fixtures, from))
		if err != nil {
			t.Fatal(err)
		}

		//nolint:gosec // dir is this test's own temporary directory
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

// TestDetachedFindsReviewsWithNoCheckout is what keeps a review prepared away
// from a clone findable. It lives three directories down under the state home,
// beside state that is not a review at all.
func TestAllFindsEveryReviewInTheStore(t *testing.T) {
	t.Parallel()

	home := t.TempDir()

	repo := filepath.Join(home, "github.com", "acme", "app")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatal(err)
	}

	staged := stage(t)
	if err := os.Rename(filepath.Join(staged, ".second-look"), filepath.Join(repo, ".second-look")); err != nil {
		t.Fatal(err)
	}

	// The state home also holds the queue's read marks and, at the depth a
	// repository sits at, whatever else is written there later.
	if err := os.WriteFile(filepath.Join(home, "conversations.toml"), []byte("x = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(home, "github.com", "acme", "nothing-staged"), 0o750); err != nil {
		t.Fatal(err)
	}

	rows, err := prepared.All(home)
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 3 {
		t.Fatalf("%d row(s), want the 3 reviews staged: %+v", len(rows), rows)
	}
}

// TestAllOnAnEmptyHome is the first run, where nothing has been reviewed
// away from a clone and the directory does not exist yet.
func TestAllOnAnEmptyHome(t *testing.T) {
	t.Parallel()

	rows, err := prepared.All(filepath.Join(t.TempDir(), "never-written"))
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 0 {
		t.Errorf("%d row(s) from a home that does not exist", len(rows))
	}
}

// A stack is what the artifact alone can see of one: a review whose base branch
// is another staged review's head. Every rule interacts with the others, so one
// set of rows exercises them together: a three-deep chain that forks, a review
// that shares a branch name with it in another repository, and one staged
// before the branches were recorded.
func TestSplitGroupsAStackBottomFirst(t *testing.T) {
	t.Parallel()

	rows := []prepared.Review{
		{Number: 4, Repository: "acme/api", HeadRef: "part-3", BaseRef: "part-2"},
		{Number: 1, Repository: "acme/api", HeadRef: "part-1", BaseRef: "main"},
		{Number: 3, Repository: "acme/api", HeadRef: "part-2", BaseRef: "part-1"},
		{Number: 5, Repository: "acme/api", HeadRef: "part-2b", BaseRef: "part-1"},
		{Number: 9, Repository: "acme/web", HeadRef: "part-2", BaseRef: "part-1"},
		{Number: 7, Repository: "acme/api"},
	}

	stacks, alone := prepared.Split(rows)

	if len(stacks) != 1 {
		t.Fatalf("%d stack(s), want the one chain: %+v", len(stacks), stacks)
	}

	if stacks[0].Onto != "main" {
		t.Errorf("the stack lands on %q, want main", stacks[0].Onto)
	}

	order := make([]int, 0, len(stacks[0].Rows))
	for _, r := range stacks[0].Rows {
		order = append(order, r.Number)
	}

	// The fork reads after the branch it hangs off, and the bottom comes first
	// either way: reading #3 before #1 is reading a diff against unseen changes.
	if want := []int{1, 3, 4, 5}; !slices.Equal(order, want) {
		t.Errorf("the stack reads %v, want %v", order, want)
	}

	left := make([]int, 0, len(alone))
	for _, r := range alone {
		left = append(left, r.Number)
	}

	// #9 names the same branches in another repository and #7 was staged before
	// the branches were recorded, so neither joins anything.
	if want := []int{9, 7}; !slices.Equal(left, want) {
		t.Errorf("%v stand alone, want %v", left, want)
	}
}
