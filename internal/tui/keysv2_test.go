package tui_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/diff"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/tui"
)

// A key whose job moved in keymap v2 says where the job went for one release,
// rather than doing nothing or something else, at every size the screen runs at.
func TestMovedKey_SaysWhereItWentOnTheReview(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		key  tea.KeyPressMsg
		says string
	}{
		{tea.KeyPressMsg{Code: 'a', Text: "a"}, "a moved to i (comment)"},
		{tea.KeyPressMsg{Code: 'S', Text: "S"}, "S moved to P (publish)"},
		{tea.KeyPressMsg{Code: 'B', Text: "B"}, "B moved to bb (branches)"},
		{tea.KeyPressMsg{Code: 'D', Text: "D"}, "D moved to bd"},
		{tea.KeyPressMsg{Code: 'u', Text: "u"}, `u moved: the looks are under \`},
		{tea.KeyPressMsg{Code: 'y', Text: "y"}, "y moved to ctrl+y"},
		{tea.KeyPressMsg{Code: 'c', Text: "c"}, `c moved to \c`},
		{tea.KeyPressMsg{Code: 't', Text: "t"}, `t moved to \t`},
		{tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}, "ctrl+r moved to ctrl+l (reload)"},
	} {
		for _, width := range []int{100, 160} {
			t.Run(fmt.Sprintf("%s/%d", tc.key.String(), width), func(t *testing.T) {
				t.Parallel()

				m, _ := fixture(t)
				m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
				press(t, m, tc.key)

				if got := plain(m.Frame()); !strings.Contains(got, tc.says) {
					t.Errorf("%s did not say %q:\n%s", tc.key.String(), tc.says, got)
				}
			})
		}
	}
}

// ctrl+r keeps restaging for the release it moved in, so a habit still works
// while the notice teaches ctrl+l.
func TestMovedKey_CtrlRStillRestagesThisRelease(t *testing.T) {
	t.Parallel()

	m, path, _ := fixtureWith(t, patch, comment("c1", parsed, artifact.SideRight, 15, "check err"))

	fresh := reviewAt(t, path)
	fresh.HeadSHA = "9f2c4d1"

	m.SetRestage(func(context.Context) (*tui.Restaged, error) {
		return &tui.Restaged{Review: fresh, Diff: diff.Parse([]byte(pushed)), HeadSHA: fresh.HeadSHA}, nil
	})
	m.SawHead("9f2c4d1")
	press(t, m, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})

	if got := plain(m.Frame()); !strings.Contains(got, "splitAll(r)") {
		t.Errorf("ctrl+r no longer restages:\n%s", got)
	}
}

// i writes whatever prose belongs at the cursor: on an open thread it replies,
// the job e used to do there.
func TestInsert_RepliesOnAThread(t *testing.T) {
	t.Parallel()

	m := threadModel(t, "does this handle nil?")
	go2(t, m, ']', 't')
	press(t, m, tea.KeyPressMsg{Code: 'i', Text: "i"})

	if got := plain(m.Frame()); !strings.Contains(got, "replying to @coderabbitai") {
		t.Errorf("i on a thread did not open a reply:\n%s", got)
	}
}

// Y copies where the cursor stands as path:line, and says what it copied.
func TestYank_CopiesPathAndLine(t *testing.T) {
	t.Parallel()

	m, _ := fixture(t)
	go2(t, m, ']', 'h')
	pressKey(t, m, 'j')
	pressKey(t, m, 'Y')

	if got := plain(m.Frame()); !strings.Contains(got, "yanked "+parsed+":") {
		t.Errorf("Y did not yank path:line:\n%s", got)
	}
}

// The queue's moved keys answer the same way, and a bare bracket waits for
// its object rather than switching queues on its own.
func TestMovedKey_SaysWhereItWentOnTheQueue(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		keys []tea.KeyPressMsg
		says string
	}{
		{[]tea.KeyPressMsg{{Code: 'm', Text: "m"}}, "m moved to i"},
		{[]tea.KeyPressMsg{{Code: 'r', Text: "r"}}, "r moved to i"},
		{[]tea.KeyPressMsg{{Code: 'A', Text: "A"}}, "A moved to Pa (approve"},
		{[]tea.KeyPressMsg{{Code: 'u', Text: "u"}}, "u moved to ]u"},
		{[]tea.KeyPressMsg{{Code: 'U', Text: "U"}}, "U moved to [u"},
		{[]tea.KeyPressMsg{{Code: 'g', Text: "g"}, {Code: 'c', Text: "c"}}, "gc moved to :clean"},
		{[]tea.KeyPressMsg{{Code: ']', Text: "]"}}, "next [q]ueue"},
		{[]tea.KeyPressMsg{{Code: ']', Text: "]"}, {Code: 'q', Text: "q"}}, "this screen holds one queue"},
	} {
		t.Run(tc.says, func(t *testing.T) {
			t.Parallel()

			l := list(t, nil)
			for _, k := range tc.keys {
				l.Update(k)
			}

			if got := plain(l.ListFrame()); !strings.Contains(got, tc.says) {
				t.Errorf("the queue did not say %q:\n%s", tc.says, got)
			}
		})
	}
}
