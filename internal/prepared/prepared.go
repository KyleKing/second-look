// Package prepared lists the reviews staged under .second-look/ in a checkout.
//
// A prepared review is invisible until you remember its number. The artifact is
// deleted the moment it posts, so whatever is on disk is unfinished work by
// definition, and a review staged last week against a head that has since moved
// is the case worth finding before it is the case that refuses to post.
package prepared

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/humanize"
)

// Review is one staged review, as a list row needs it.
type Review struct {
	Path       string    `json:"path"`
	Number     int       `json:"number"`
	Repository string    `json:"repository"`
	Event      string    `json:"event"`
	HeadSHA    string    `json:"head_sha"`
	Modified   time.Time `json:"modified"`

	Ready int `json:"ready"`
	Draft int `json:"draft"`
	Skip  int `json:"skip"`
	// Replies is how many comments answer an existing thread rather than
	// opening one. A review that is all replies posts through a different
	// endpoint, which is worth seeing before submitting it.
	Replies int `json:"replies"`

	// Body is set when the review carries a review-level comment, which posts
	// even with no inline comment under it.
	Body bool `json:"body"`

	// Broken is why a file on disk could not be read as a review. The row still
	// lists: a file that no longer parses is the one most worth knowing about,
	// and dropping it silently would hide the only report anyone gets.
	Broken string `json:"broken,omitempty"`
}

// Total is how many comments the review carries, skipped ones included.
func (r *Review) Total() int { return r.Ready + r.Draft + r.Skip }

// Blocked reports whether submitting would be refused as it stands. A draft
// blocks the submit rather than posting or vanishing, so a review with one is
// staged but not finished.
func (r *Review) Blocked() bool { return r.Draft > 0 }

// Where names the pull request, falling back to the number alone for a file
// that could not be read.
func (r *Review) Where() string {
	if r.Repository == "" {
		return "#" + strconv.Itoa(r.Number)
	}

	return fmt.Sprintf("%s#%d", r.Repository, r.Number)
}

// Short is the head commit at the length a person reads.
func (r *Review) Short() string {
	const shortSHA = 7

	if len(r.HeadSHA) < shortSHA {
		return r.HeadSHA
	}

	return r.HeadSHA[:shortSHA]
}

// The words State answers with. They are named because the row renderer pads
// the column to the longest of them and the list screen prints the same set.
const (
	StateBlocked    = "blocked"
	StateEmpty      = "empty"
	StateReady      = "ready"
	StateUnreadable = "unreadable"
)

// State is the one word that says what to do with the review: a draft blocks
// the submit, and everything else is ready to post.
func State(r *Review) string {
	switch {
	case r.Broken != "":
		return StateUnreadable
	case r.Blocked():
		return StateBlocked
	case r.Ready > 0 || r.Body:
		return StateReady
	default:
		return StateEmpty
	}
}

// ErrNoDir reports a checkout with no artifact directory, which is what a
// repository nobody has staged a review in looks like.
var ErrNoDir = errors.New("no .second-look directory here")

// List reads every staged review in the checkout at root, newest first.
//
// Recency is the order because the artifact is working state: the review being
// written is the one to reopen, and the one from three weeks ago is the one to
// decide about.
func List(root string) ([]Review, error) {
	dir := filepath.Join(root, artifact.Dir)

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%s: %w", dir, ErrNoDir)
	}

	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}

	var out []Review

	for _, e := range entries {
		number, ok := prNumber(e)
		if !ok {
			continue
		}

		out = append(out, read(filepath.Join(dir, e.Name()), number))
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Modified.After(out[j].Modified)
	})

	return out, nil
}

// prNumber reads the pull request number out of a staged review's filename.
// Everything else in the directory is cache -- the diff, the threads, the read
// marks -- and none of it is a review.
func prNumber(e fs.DirEntry) (int, bool) {
	if e.IsDir() {
		return 0, false
	}

	name := e.Name()

	rest, ok := strings.CutPrefix(name, "pr-")
	if !ok {
		return 0, false
	}

	rest, ok = strings.CutSuffix(rest, ".toml")
	if !ok {
		return 0, false
	}

	number, err := strconv.Atoi(rest)
	if err != nil || number <= 0 {
		return 0, false
	}

	return number, true
}

func read(path string, number int) Review {
	row := Review{Path: path, Number: number}

	if info, err := os.Stat(path); err == nil {
		row.Modified = info.ModTime()
	}

	r, err := artifact.Load(path)
	if err != nil {
		row.Broken = oneLine(path, err.Error())

		return row
	}

	row.Repository = r.Owner + "/" + r.Repo
	row.Event = r.Event
	row.HeadSHA = r.HeadSHA
	row.Body = strings.TrimSpace(r.Body) != ""

	for i := range r.Comments {
		c := &r.Comments[i]

		switch c.Status {
		case artifact.StatusReady:
			row.Ready++
		case artifact.StatusDraft:
			row.Draft++
		case artifact.StatusSkip:
			row.Skip++
		}

		if c.InReplyTo != 0 {
			row.Replies++
		}
	}

	return row
}

// oneLine flattens a load failure onto the row, dropping the line that names
// the file. A load error opens with the path and puts the reason underneath, and
// the row already says which file it is, so keeping the path would spend the
// width on the one thing the reader can already see.
func oneLine(path, s string) string {
	const limit = 90

	var kept []string

	for line := range strings.SplitSeq(s, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ":"))
		if line == "" || strings.HasSuffix(line, path) {
			continue
		}

		kept = append(kept, line)
	}

	return humanize.Clip(strings.Join(kept, "; "), limit)
}
