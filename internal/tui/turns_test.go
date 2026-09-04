package tui_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/tui"
)

func exchange(n int) []artifact.Turn {
	out := make([]artifact.Turn, 0, n)

	for i := range n {
		out = append(out, artifact.Turn{
			Author: []string{"kyleking", "claude"}[i%2],
			Body:   strings.TrimSpace(strings.Repeat("turn "+string(rune('a'+i))+" says something. ", 6)),
		})
	}

	return out
}

// onto walks the cursor down to the first row carrying want, so a test can act
// on a row without knowing how many lines sit above it.
func onto(t *testing.T, m *tui.Model, want string) {
	t.Helper()

	const give = 300

	for range give {
		for _, l := range strings.Split(plain(m.Frame()), "\n") {
			if strings.HasPrefix(l, "▌") && strings.Contains(l, want) {
				return
			}
		}

		press(m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	}

	t.Fatalf("never reached a row carrying %q:\n%s", want, plain(m.Frame()))
}

// A turn thread is collapsed until it is asked for, because a comment three
// rounds deep is a page of prose between the reader and the next hunk. What
// shows is the last turn trimmed, one line of the turn before it, and a count
// of everything older.
func TestTurnsCollapseUntilAsked(t *testing.T) {
	t.Parallel()

	c := comment("c1", parsed, artifact.SideRight, 15, "the finding")
	c.Turns = exchange(4)

	m, _ := fixture(t, c)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	frame := plain(m.Frame())

	if !strings.Contains(frame, "2 turns earlier") {
		t.Errorf("the older turns are not counted:\n%s", frame)
	}

	if strings.Contains(frame, "turn a says") {
		t.Errorf("an older turn was drawn in full:\n%s", frame)
	}

	if !strings.Contains(frame, "turn d says") {
		t.Errorf("the last turn is missing:\n%s", frame)
	}

	if !strings.Contains(frame, "…") {
		t.Errorf("a trimmed turn does not say it was trimmed:\n%s", frame)
	}
}

// za on the turns opens the whole exchange, which is the affordance the
// collapsed shape exists to have.
func TestTurnsOpenOnFold(t *testing.T) {
	t.Parallel()

	c := comment("c1", parsed, artifact.SideRight, 15, "the finding")
	c.Turns = exchange(4)

	m, _ := fixture(t, c)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})

	onto(t, m, "TURNS")

	press(m, tea.KeyPressMsg{Code: 'z', Text: "z"})
	press(m, tea.KeyPressMsg{Code: 'a', Text: "a"})

	frame := plain(m.Frame())
	if !strings.Contains(frame, "turn a says") {
		t.Errorf("za did not open the exchange:\n%s", frame)
	}
}

// A comment handed back to an agent is a fifth state, and it reads as its own
// thing rather than as a draft: a draft is a comment nobody has ruled on.
func TestTodoIsItsOwnState(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "the finding"))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	onto(t, m, "MAJOR")

	press(m, tea.KeyPressMsg{Code: 'm', Text: "m"})
	press(m, tea.KeyPressMsg{Code: 't', Text: "t"})

	frame := plain(m.Frame())

	for _, want := range []string{"MAJOR  todo", "1 todo"} {
		if !strings.Contains(frame, want) {
			t.Errorf("%q is missing after m t:\n%s", want, frame)
		}
	}
}

// T with nothing marked says so rather than writing an empty set out.
func TestDispatchWithNothingOwedSaysSo(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t, comment("c1", parsed, artifact.SideRight, 15, "the finding"))
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	press(m, tea.KeyPressMsg{Code: 'T', Text: "T"})

	if frame := plain(m.Frame()); !strings.Contains(frame, "nothing is marked todo") {
		t.Errorf("T with nothing owed said:\n%s", frame)
	}
}

// The second hand-over on one pull request has to reach the agent that already
// read the diff, so T carries the session the agent recorded on the review. The
// first one carries none, because there is nothing to resume yet.
func TestDispatchCarriesTheSessionTheAgentRecorded(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name    string
		session string
	}{
		{"a first hand-over", ""},
		{"a follow-up", "abc-123"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			var (
				handed  string
				written string
			)

			store := t.TempDir()
			owed := comment("c1", parsed, artifact.SideRight, 15, "the finding")
			owed.Status = artifact.StatusTodo

			review := &artifact.Review{
				Version: artifact.SchemaVersion, Owner: "kyleking", Repo: "jj-diff", Number: 42,
				HeadSHA: "a1b2c3d", Event: artifact.EventComment,
				Agent:    artifact.Agent{Session: c.session},
				Comments: []artifact.Comment{owed},
			}

			path := filepath.Join(t.TempDir(), "pr-42.toml")
			if err := artifact.Save(path, review); err != nil {
				t.Fatal(err)
			}

			m := tui.New(t.Context(), review, diff.Parse([]byte(patch)), path,
				func(context.Context, *artifact.Review) (string, error) { return "", nil },
				tui.WithStore(store),
				tui.WithDispatcher(func(_ context.Context, set, session string) (string, error) {
					handed, written = session, set

					return "handed over", nil
				}))
			m.Init()
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

			press(m, tea.KeyPressMsg{Code: 'T', Text: "T"})

			if handed != c.session {
				t.Errorf("T handed over the session %q, want %q", handed, c.session)
			}

			if written == "" {
				t.Error("T ran the agent without writing the set out")
			}
		})
	}
}
