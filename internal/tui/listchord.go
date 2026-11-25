package tui

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/tui/keyhint"
	"github.com/kyleking/aragonite/tui/palette"
)

// The list's motion objects: u a row still needing a look, q a queue.
const (
	objUnread = "u"
	objQueue  = "q"
)

// listPrefix is the chord a key opens on a list screen, false for any other
// key.
func listPrefix(k string) (rune, bool) {
	switch k {
	case "g", "]", "[", "z", "P":
		r, _ := utf8.DecodeRuneInString(k)

		return r, true
	}

	return 0, false
}

// chordPage is what a list chord accepts, as key and word pairs.
func chordPage(prefix rune) [][2]string {
	switch prefix {
	case ']', '[':
		next := "next"
		if prefix == '[' {
			next = "previous"
		}

		return [][2]string{{objUnread, next + " row needing a look"}, {objQueue, next + " queue"}}
	case 'z':
		return [][2]string{{"a", "show or hide the folded lane"}}
	case 'P':
		return [][2]string{{"a", "approve, after a confirm"}}
	}

	return [][2]string{{"g", topWord}}
}

// chordCaption is the chord's page drawn as keyhints while it waits.
func chordCaption(prefix rune) string {
	return string(prefix) + keyhint.Gap + keyhint.Line(keyhint.Styles{}, asHints(chordPage(prefix)))
}

// completeChord answers a chord's second key: what the page offered, or a
// cancel that says what the chord takes. A key the chord used to take says
// where it went.
func (l *List) completeChord(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	prefix := l.pending
	l.pending = 0

	if msg.String() == escKey {
		l.say("", false)

		return l, nil
	}

	k := msg.String()

	switch prefix {
	case ']', '[':
		step := 1
		if prefix == '[' {
			step = -1
		}

		if k == objUnread || k == objQueue {
			l.motion = k
			cmd := l.step(k, step)

			return l, cmd
		}
	case 'z':
		if k == "a" {
			return l.run(ActFold, l.current())
		}
	case 'P':
		if k == "a" {
			if why := l.gate("Pa"); !why.OK {
				l.say("Pa  "+why.Why, false)

				return l, nil
			}

			return l.run(ActApprove, l.current())
		}
	case 'g':
		switch k {
		case "g":
			l.to(0)
			l.touched = true
			cmd := l.rest()

			return l, cmd
		case "c", "s":
			l.say("g"+k+" moved to :"+cleanCommand(k == "s"), false)

			return l, nil
		}
	}

	l.say("no "+string(prefix)+" for "+k+"; "+hintLine(chordPage(prefix)), false)

	return l, nil
}

// cleanCommand is the palette command a clean runs as.
func cleanCommand(stale bool) string {
	if stale {
		return "clean stale"
	}

	return "clean"
}

// step makes one list motion: to the next row needing a look, or the next
// queue.
func (l *List) step(object string, dir int) tea.Cmd {
	if object == objQueue {
		if len(l.tabs) < several {
			l.say("this screen holds one queue", false)

			return nil
		}

		return l.switchTo((l.at + dir + len(l.tabs)) % len(l.tabs))
	}

	l.say("", false)
	l.nextUnread(dir)
	l.touched = true

	return l.rest()
}

// repeat answers n and N: the last ]/[ motion again, forward or back, which is
// the next row needing a look until another has been made.
func (l *List) repeat(dir int) tea.Cmd {
	object := l.motion
	if object == "" {
		object = objUnread
	}

	return l.step(object, dir)
}

// movedKey answers a key whose job moved this release with where it went;
// ctrl+r still reads the queue again for one release.
func (l *List) movedKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()

	mv, ok := moveNotice(l.km, k)
	if !ok {
		return nil, false
	}

	if k == ctrlR {
		cmd := l.requery()
		l.say(mv.Notice+"; ctrl+r still refreshes this release", false)

		return cmd, true
	}

	l.say(mv.Notice, false)

	return nil, true
}

// requery reads the queue again: the loader's searches where the screen
// fills in as they answer, else the tab's own refresh.
func (l *List) requery() tea.Cmd {
	if l.loader != nil {
		return l.reload()
	}

	if row := l.current(); row != nil {
		_, cmd := l.run(ActRefresh, row)

		return cmd
	}

	return nil
}

// reload re-runs the loader's searches.
func (l *List) reload() tea.Cmd {
	l.armed = true
	cmd := l.loader.Start()
	l.status, l.failed = "", false

	return cmd
}

// yank copies the row's reference (owner/repo#number) through the terminal's
// clipboard, for pasting into a chat or a commit.
func (l *List) yank() tea.Cmd {
	row := l.current()
	if row == nil || !strings.Contains(row.Key, "#") {
		l.say("nothing to yank on this row", false)

		return nil
	}

	l.say("yanked "+row.Key, false)

	return tea.SetClipboard(row.Key)
}

// commands are the tab's palette commands, run by name rather than by a key.
func (l *List) commands() []palette.Entry {
	out := make([]palette.Entry, 0, len(l.goes))

	for _, o := range l.goes {
		out = append(out, palette.Entry{Action: ":" + o.Key, What: o.What, Group: screenGroup})
	}

	return out
}

// runCommand runs one of the tab's palette commands, reporting whether action
// named one.
func (l *List) runCommand(action string) (tea.Cmd, bool) {
	for _, o := range l.goes {
		if action == ":"+o.Key {
			_, cmd := l.run(o.Act, l.current())

			return cmd, true
		}
	}

	return nil, false
}
