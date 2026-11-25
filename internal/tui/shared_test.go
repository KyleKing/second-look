package tui_test

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/diff"
	"github.com/kyleking/aragonite/seen"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/tui"
)

// jojoRecord stands in for jojo's store: it holds a set of read hunks, imports them all, and keeps
// what each export sent.
type jojoRecord struct {
	read    map[seen.ID]bool
	exports int
}

func (j *jojoRecord) Import(s *seen.Set, live []seen.Ref) (int, error) {
	n := 0

	for _, r := range live {
		if j.read[r.ID] && !s.Has(r.ID) {
			s.Mark(true, r.ID)
			n++
		}
	}

	return n, nil
}

func (j *jojoRecord) Export(s *seen.Set) error {
	j.exports++

	for id := range j.read {
		j.read[id] = s.Has(id)
	}

	return nil
}

// unread counts the hunks the record holds as not read.
func (j *jojoRecord) unread() int {
	n := 0

	for _, read := range j.read {
		if !read {
			n++
		}
	}

	return n
}

// sharedReview is readable's review with a record shared with jojo, given up front as a local
// review gives it, or found behind the first frame as a pull request's is.
func sharedReview(t *testing.T, patch string, rec *jojoRecord, found bool) *tui.Model {
	t.Helper()

	r := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "pr-42.toml")

	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	marks, err := seen.Load(filepath.Join(dir, "seen.toml"))
	if err != nil {
		t.Fatal(err)
	}

	share := tui.WithShared(rec)
	if found {
		share = tui.WithSharedReader(func(context.Context, string, *diff.Diff) (tui.Sharer, error) { return rec, nil })
	}

	m := tui.New(t.Context(), r, diff.Parse([]byte(patch)), path, (&counter{}).post,
		tui.WithSeen(marks, filepath.Join(dir, "seen.toml")), share)

	if cmd := m.Init(); cmd != nil && found {
		landFirst(m, cmd)
	}

	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	return m
}

// landFirst runs each of a batch's commands once and feeds back what answers within a second.
// Init starts watchers that never answer, so it cannot be drained to the end.
func landFirst(m *tui.Model, cmd tea.Cmd) {
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		return
	}

	answers := make(chan tea.Msg, len(batch))

	for _, c := range batch {
		if c != nil {
			go func() { answers <- c() }()
		}
	}

	deadline := time.After(time.Second)

	for {
		select {
		case msg := <-answers:
			if msg != nil {
				m.Update(msg)
			}
		case <-deadline:
			return
		}
	}
}

// readLine is the frame's read count, "read N of M hunks".
func readLine(frame string) string {
	return regexp.MustCompile(`\bread \d+ of \d+ hunks`).FindString(frame)
}

// TestSharedMarks_ReadInJojoOpenRead is the dogfood report's "read 0 of 11": hunks jojo marked
// read open as read, whether the record came with the review or was found behind the first frame,
// and a mark taken back in the review is written back to jojo.
func TestSharedMarks_ReadInJojoOpenRead(t *testing.T) {
	t.Parallel()

	for _, found := range []bool{false, true} {
		name := map[bool]string{false: "local", true: "pull request"}[found]

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			patch := longPatch(t)
			rec := &jojoRecord{read: map[seen.ID]bool{}}

			for _, r := range seen.Hunks(diff.Parse([]byte(patch))) {
				rec.read[r.ID] = true
			}

			m := sharedReview(t, patch, rec, found)

			var read, total int
			if _, err := fmt.Sscanf(readLine(plain(m.Frame())), "read %d of %d hunks", &read, &total); err != nil ||
				read != total {
				t.Fatalf("hunks jojo marked read do not open read (%d of %d):\n%s", read, total, plain(m.Frame()))
			}

			press(t, m, tea.KeyPressMsg{Code: ']', Text: "]"})
			press(t, m, tea.KeyPressMsg{Code: 'h', Text: "h"})
			press(t, m, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})

			if rec.exports == 0 {
				t.Fatal("marking a hunk wrote nothing back to jojo")
			}

			if n := rec.unread(); n != 1 {
				t.Errorf("jojo holds %d unread hunks after the review unmarked one, want 1", n)
			}
		})
	}
}
