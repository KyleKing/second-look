package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/tui/coach"
	"github.com/kyleking/aragonite/tui/keyhint"
	"github.com/kyleking/aragonite/tui/keys"
	"github.com/kyleking/aragonite/tui/palette"
)

// QueueState is what the queue's coach rules read about where the cursor is.
type QueueState struct {
	// Tab is the queue being read and Section the lane the cursor sits in.
	Tab     string
	Section string
	// Unread counts the rows of the tab that moved since they were last read.
	Unread int
	// OnRow is a cursor on a row rather than a heading, and RowUnread that row
	// having moved since it was read.
	OnRow     bool
	RowUnread bool
}

// The queue's row keys, which a gate refuses where the cursor is on no row.
var rowKeys = []string{enterKey, spaceKey, "o", "i", "R", "C", "P", "Pa", "d", "M", "l", "Y"}

// unreadKey is the motion that walks to the next row still needing a look,
// which the queue's generic rule names, and unreadAction how it is replayed.
const (
	unreadKey    = "]u"
	unreadAction = "] u"
)

// WithLedger is where the queue's coach and welcome card remember what they
// have shown, shared with the review screen under the same program name.
func (l *List) WithLedger(led *coach.Ledger) *List {
	l.ledger = led
	l.discover()

	return l
}

// discover builds the palette, the coach over every tab's rules, and the
// first-run card. It runs again when a ledger arrives.
func (l *List) discover() {
	l.pal = palette.New(l.styles.skin)
	l.welcome = queueWelcome(l.keys, l.list)
	l.welcome.OpenIfDue(l.ledger)

	rules := []coach.Rule[QueueState]{{
		ID: "queue.unread", Action: unreadAction, Key: unreadKey, What: "next unread", Tone: coach.Next,
		When: func(s QueueState) bool { return s.Unread > 0 && !s.RowUnread },
		Say: func(s QueueState) string {
			if s.Unread == 1 {
				return "1 row moved since you read it"
			}

			return strconv.Itoa(s.Unread) + " rows moved since you read them"
		},
		Why: func(QueueState) string { return "space marks one read without opening it" },
	}}

	for i := range l.tabs {
		for _, r := range l.tabs[i].Rules {
			name, when := l.tabs[i].Name, r.When
			r.Priority++
			r.When = func(s QueueState) bool { return s.Tab == name && when != nil && when(s) }
			rules = append(rules, r)
		}
	}

	l.coach = coach.New(coach.Config[QueueState]{
		Ledger: l.ledger, Rules: rules, Tips: queueTips(), Start: l.clock(),
	})
}

// queueTips are the queue's keys worth knowing that a reader may not reach for.
func queueTips() []coach.Suggestion {
	return []coach.Suggestion{
		{ID: "queue.tip.palette", Action: paletteKey, Key: paletteKey, Say: "finds any command by name and runs it"},
		{ID: "queue.tip.unread", Action: unreadAction, Key: unreadKey, Say: "jumps to the next row you have not read"},
		{ID: "queue.tip.focus", Action: "f", Key: "f", Say: "reads one repository across all three queues"},
		{ID: "queue.tip.filter", Action: "/", Key: "/", Say: "narrows the rows to the ones carrying a word"},
	}
}

// queueState is the queue as the coach's rules read it.
func (l *List) queueState() QueueState {
	s := QueueState{}
	if len(l.tabs) > 0 {
		s.Tab = l.tabs[l.at].Name
	}

	for i := range l.lines {
		if r := l.lines[i].row; r != nil && r.Unread && l.lines[i].detail == "" {
			s.Unread++
		}
	}

	if row := l.current(); row != nil {
		s.OnRow, s.RowUnread = true, row.Unread
	}

	for i := min(l.cursor, len(l.lines)-1); i >= 0; i-- {
		if sec := l.lines[i].section; sec != nil {
			s.Section = sec.Name

			break
		}
	}

	return s
}

// gate answers whether key k can do anything where the cursor is, and why
// not: the palette and legend say the reason beside the key, and a press of a
// row key with no row under the cursor says it in the footer.
func (l *List) gate(k string) keys.Availability {
	if a, ok := l.screenGate(k); ok {
		return a
	}

	row := l.current()

	if len(l.tabs) > 0 && l.tabs[l.at].Why != nil {
		if why := l.tabs[l.at].Why(row, k); why != "" {
			return keys.Refuse(why)
		}
	}

	if l.inert != nil && slices.Contains(l.inert(row), k) {
		return keys.Refuse("nothing for " + k + " to do on this row")
	}

	return keys.Allow()
}

// screenGate is gate's half about the screen rather than the row: a row key
// with no row, a queue key with one queue, labels where rows carry none, and
// leaving a focus that is not there.
func (l *List) screenGate(k string) (keys.Availability, bool) {
	switch {
	case l.current() == nil && slices.Contains(rowKeys, k):
		return keys.Refuse("no row under the cursor; j/k moves to one"), true
	case len(l.tabs) < several && slices.Contains([]string{"]q", "[q", "]q/[q", "1", "2", "3", "1/2/3"}, k):
		return keys.Refuse("this screen holds one queue"), true
	case k == "l" && l.labeled == nil:
		return keys.Refuse("this queue's rows carry no labels"), true
	case k == "F" && l.focused == "":
		return keys.Refuse("no repository in focus; f focuses the row's"), true
	}

	return keys.Allow(), false
}

// refuse answers a key the gate refuses for want of a row or a repository in
// focus, which would otherwise do nothing at all, and reports whether it did.
// A key whose action explains its own refusal (M on a pull request that is not
// ready) is left to it.
func (l *List) refuse(k string) bool {
	if !slices.Contains(rowKeys, k) && k != "l" && k != "F" {
		return false
	}

	a := l.gate(k)
	if a.OK || (l.current() != nil && k != "l" && k != "F") {
		return false
	}

	l.say(k+"  "+a.Why, false)

	return true
}

// legend is the tab's help as the palette's tree: one heading, a row per key,
// and the prose under it, with every key the gate refuses marked off.
func (l *List) legend() []keyhint.Hint {
	help := l.helpLines
	if help == nil {
		help = defaultListHelp()
	}

	name := "the queue"
	if len(l.tabs) > 0 && l.tabs[l.at].Name != "" {
		name = l.tabs[l.at].Name
	}

	rows := legendRows(help)
	tree := make([]keyhint.Hint, 0, len(rows)+1)
	tree = append(tree, keyhint.Hint{Head: true, What: name})

	for _, row := range rows {
		if row.Key != "" {
			for _, k := range palette.Sequences(row.Key) {
				if a := l.gate(strings.ReplaceAll(k, " ", "")); !a.OK {
					row.Off, row.Why = true, a.Why

					break
				}
			}
		}

		tree = append(tree, row)
	}

	return tree
}

// legendRows splits the legend's rows into one per key label: "f, F" is two
// rows, and a row naming three keys or more with as many words split by commas
// pairs them up ("j/k, ctrl+u/d, gg/G" moves, a half page, top and bottom). A
// row about the filter prompt names no key a press can reach, so it is prose.
func legendRows(help [][2]string) []keyhint.Hint {
	const pairFrom = 3

	var out []keyhint.Hint

	for _, h := range help {
		switch {
		case h[0] == "" && h[1] == "":
			continue
		case h[0] == "" || strings.HasPrefix(h[1], "in the prompt"):
			out = append(out, keyhint.Hint{What: strings.TrimSpace(h[0] + " " + h[1])})

			continue
		}

		labels := strings.Split(h[0], ", ")
		whats := strings.Split(h[1], ", ")

		for i, k := range labels {
			what := h[1]
			if len(labels) >= pairFrom && len(whats) == len(labels) {
				what = whats[i]
			}

			out = append(out, keyhint.Hint{Key: strings.ReplaceAll(k, " / ", "/"), What: what})
		}
	}

	return out
}

// paletteEntries is the legend flattened into the palette's rows, with the
// palette's own commands after the keys for the command palette.
func (l *List) paletteEntries(mode palette.Mode) []palette.Entry {
	es := palette.FromLegend(l.legend())
	if mode == palette.Run {
		es = append(es, l.commands()...)
		es = append(es,
			palette.Entry{Action: cmdTipsOff, What: "stop tips about keys not yet used", Group: screenGroup},
			palette.Entry{Action: cmdTipsOn, What: "show tips about keys not yet used", Group: screenGroup},
			coach.WelcomeEntry(),
		)
	}

	return palette.WithSeen(es, l.ledger, false)
}

// openPalette opens the legend (Browse) or the command palette (Run).
func (l *List) openPalette(mode palette.Mode) {
	l.pal.SetLayout(paletteLayout(l.width + pageSides*l.padX))
	l.pal.Open(mode, l.paletteEntries(mode))
}

// runPalette carries out what the palette ran: one of its own commands, or a
// legend row's keys replayed as if typed, which keeps a palette run and a key
// press one code path.
func (l *List) runPalette(run palette.RunMsg) tea.Cmd {
	if cmd, ok := l.runCommand(run.Action); ok {
		return cmd
	}

	switch run.Action {
	case cmdTipsOff, cmdTipsOn:
		l.ledger.SetTipsOff(run.Action == cmdTipsOff)
		l.say("tips "+strings.TrimPrefix(run.Action, ":tips "), false)

		return nil
	case coach.WelcomeAction:
		l.welcome.Open()

		return nil
	}

	l.coach.Observe(coach.Event{At: l.clock(), Action: run.Action, Palette: true})
	l.status = ""

	var cmds []tea.Cmd

	for _, k := range strings.Fields(run.Action) {
		_, cmd := l.handleKey(pressOf(k))
		cmds = append(cmds, cmd)
	}

	if run.From == palette.Run && l.status == "" {
		l.say("ran "+run.Key+" · next time press "+run.Key, false)
	}

	return tea.Batch(cmds...)
}

// listMotion reports a key that only moves the cursor, which the coach does
// not count as a command run.
func listMotion(k string) bool {
	return moving(k) || k == "g g" || k == "G" || k == tabKey
}

// observe tells the coach about a key the queue just dispatched, and arms the
// next tip's wake.
func (l *List) observe(k string) tea.Cmd {
	now := l.clock()
	l.coach.Press(now)

	if l.pal.IsOpen() || l.labeler != nil || l.filter.typing || l.pending != 0 {
		return l.coach.WakeAt(now)
	}

	l.coach.Observe(coach.Event{At: now, Action: k, Motion: listMotion(k)})

	return l.coach.WakeAt(now)
}

// discoverMsg answers the messages the palette and the coach send back,
// reporting whether msg was one.
func (l *List) discoverMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case palette.RunMsg:
		return l.runPalette(msg), true
	case palette.RefusedMsg:
		return nil, true
	case coach.IdleMsg:
		busy := l.pal.IsOpen() || l.labeler != nil || l.filter.typing || l.pending != 0
		l.coach.Tip(msg.At, busy)

		if err := l.ledger.Save(); err != nil {
			l.say("could not save the coach's notes: "+err.Error(), true)
		}

		return l.coach.WakeAt(msg.At), true
	}

	return nil, false
}

// coachFooter is the footer's line when no status holds it: the welcome card
// as a line on a terminal too small for the card, a tip until the next key,
// else the next step where it fits whole beside the live hints. It is empty
// where none applies, which leaves the frame's own footer.
func (l *List) coachFooter() string {
	if !coach.WelcomeFits(l.width, l.paletteRows()) {
		if line := l.welcome.Line(l.styles.skin, l.width-1); line != "" {
			return " " + line
		}
	}

	hints := l.footerHints()
	tip, tipping := l.coach.Showing()
	next, ok := l.coach.Next(l.queueState(), func(a string) keys.Availability { return l.gate(a) })

	if !tipping && !ok {
		return ""
	}

	if ok {
		hints = coach.Mark(hints, next)
	}

	live := slices.DeleteFunc(slices.Clone(hints), func(h keyhint.Hint) bool { return h.Off })
	room := l.width - textWidth(keyhint.Line(footerStyles(l.styles), live)) - statusGap - 1

	lead := ""

	switch {
	case tipping:
		lead = coach.TipLine(l.styles.skin, tip, max(room, l.width/tipShare))
	case ok:
		if whole := coach.Line(l.styles.skin, next, l.width); textWidth(whole) <= room {
			lead = whole
		}
	}

	if lead == "" {
		return ""
	}

	if line := keyhint.Fit(footerStyles(l.styles), hints, l.width-textWidth(lead)-statusGap-1); line != "" {
		return " " + lead + "  " + line
	}

	return " " + lead
}
