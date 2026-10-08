package tui_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/blame"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/tui"
)

// aged answers the fixture patch's diff.go hunk with one line per ramp
// bucket, so the glyph each age draws can be checked against the frame
// itself.
func aged(_ context.Context) (blame.Map, error) {
	now := time.Now()

	return blame.Map{
		parsed: {
			14: {When: now.Add(-3 * 24 * time.Hour)},   // this week
			15: {When: now.Add(-15 * 24 * time.Hour)},  // this month
			16: {When: now.Add(-200 * 24 * time.Hour)}, // this year
		},
	}, nil
}

func blamedFixture(t *testing.T, b tui.Blamer) *tui.Model {
	t.Helper()

	r := &artifact.Review{
		Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
		HeadSHA: "a1b2c3d", Event: artifact.EventComment,
	}

	path := filepath.Join(t.TempDir(), "pr-42.toml")
	if err := artifact.Save(path, r); err != nil {
		t.Fatal(err)
	}

	sub := &counter{}

	m := tui.New(t.Context(), r, diff.Parse([]byte(patch)), path, sub.post, tui.WithBlame(b))
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	return m
}

func TestBlameColumnFollowsUThenB(t *testing.T) {
	t.Parallel()

	m := blamedFixture(t, aged)

	go2(m, 'u', 'b')

	frame := plain(m.Frame())
	for _, glyph := range []string{"░", "▒", "▓"} {
		if !strings.Contains(frame, glyph) {
			t.Errorf("no %q in the blame column:\n%s", glyph, frame)
		}
	}

	// The cycle walks the layout questions; the column is an overlay v leaves
	// alone.
	press(m, tea.KeyPressMsg{Code: 'v', Text: "v"})
	if !strings.Contains(plain(m.Frame()), "░") {
		t.Errorf("v dropped the blame column:\n%s", plain(m.Frame()))
	}

	go2(m, 'u', 'b')

	if strings.Contains(plain(m.Frame()), "░") {
		t.Errorf("the second u b did not take the column down:\n%s", plain(m.Frame()))
	}
}

func TestBlameWithoutACheckoutSaysSo(t *testing.T) {
	t.Parallel()

	m := treeFixture(t, tui.TreeNone)

	go2(m, 'u', 'b')

	if frame := plain(m.Frame()); !strings.Contains(frame, "claims the best clone") {
		t.Errorf("u b said nothing about having no history:\n%s", frame)
	}
}

var errShallow = errors.New("history ends at the shallow boundary")

func TestBlameFailureIsSpoken(t *testing.T) {
	t.Parallel()

	m := blamedFixture(t, func(context.Context) (blame.Map, error) {
		return nil, errShallow
	})

	go2(m, 'u', 'b')

	if frame := plain(m.Frame()); !strings.Contains(frame, "shallow boundary") {
		t.Errorf("a blame failure was not said:\n%s", frame)
	}
}
