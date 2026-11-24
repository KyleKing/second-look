package tui

import (
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/tui/coach"
	"github.com/kyleking/aragonite/tui/keyhint"
	"github.com/kyleking/aragonite/tui/keys"
	"github.com/kyleking/aragonite/tui/palette"
)

// paletteKey opens the command palette, the vocabulary's reserved key for it in
// every tool of the family.
const paletteKey = ":"

// submitWord is the next step's word for S, the footer's own.
const submitWord = "submit"

// screenGroup files the palette's own commands in the list.
const screenGroup = "the screen"

// jumpBack is the jump list's back key, which a tip teaches.
const jumpBack = "ctrl+o"

// The next steps' order within one tone, and the share of a narrow footer a
// tip may take.
const (
	rankUnread  = 2
	rankRulings = 1
	tipShare    = 2
)

// The palette's own commands, which no key presses: the coach's band and its
// tips. Each is an action no legend row names, so a run cannot be mistaken for
// a key to replay.
const (
	cmdNext    = ":next"
	cmdTipsOff = ":tips off"
	cmdTipsOn  = ":tips on"
)

// WithLedger is where the coach remembers what it has shown and what the
// reader has run, across reviews. Without it nothing outlives the screen.
func WithLedger(l *coach.Ledger) Option {
	return func(m *Model) { m.ledger = l }
}

// WithCoachLayout draws the next step and tips as a labeled band above the
// footer rather than in its status slot.
func WithCoachLayout(l coach.Layout) Option {
	return func(m *Model) { m.band = l == coach.Band }
}

// discover builds the palette and the coach once the options have run.
func (m *Model) discover() {
	m.pal = palette.New(m.styles.skin)
	m.welcome = reviewWelcome(m.keys)
	m.welcome.OpenIfDue(m.ledger)
	m.coach = coach.New(coach.Config[reviewState]{
		Ledger: m.ledger, Rules: reviewRules(), Tips: reviewTips(), Start: m.built,
		Detectors: []coach.Detector{
			coach.Repeat("j", walkRun, coach.Suggestion{
				ID: "review.hunk-next", Action: "] h", Key: "]h",
				Say: "jumps a hunk at a time, where j walks a line",
			}),
			coach.ViaPalette(viaPaletteRuns, func(action string) (string, string, bool) {
				if strings.HasPrefix(action, ":") {
					return "", "", false
				}

				return strings.ReplaceAll(action, " ", ""), "it", true
			}),
		},
	})
}

// walkRun is how many j presses in a row read as stepping a diff the long way,
// and viaPaletteRuns how many palette runs of one key teach the key.
const (
	walkRun        = 25
	viaPaletteRuns = 3
)

// gate answers whether the key k can do anything where the cursor is, and why
// not. The footer dims what it refuses, the legend and the palette say the
// reason, and pressing one of the keys whose handler would otherwise do
// nothing says the reason in the status line.
func (m *Model) gate(k string) keys.Availability {
	if a, ok := m.writeGate(k); ok {
		return a
	}

	if a, ok := m.fileGate(k); ok {
		return a
	}

	switch k {
	case "A":
		if m.asker == nil && !m.onCode() {
			return keys.Refuse("needs an agent in the config, or a line of the diff")
		}
	case "B":
		if m.repoDir == "" {
			return keys.Refuse("no checkout of this repository; C makes one")
		}
	case "T":
		if m.dispatcher == nil && m.agentStart == nil {
			return keys.Refuse("no agent configured; T writes the todo set to a file")
		}
	case "X", "K":
		if m.prober == nil {
			return keys.Refuse("no language server or checker for this review")
		}
	}

	return keys.Allow()
}

// fileGate is gate's half about the file under the cursor: a fixme needs a
// checkout and a line, and a lockfile question needs a lockfile.
func (m *Model) fileGate(k string) (keys.Availability, bool) {
	switch k {
	case "F":
		if m.workRoot == "" {
			return keys.Refuse("no checkout to write a fixme into"), true
		}

		if !m.onCode() {
			return keys.Refuse("needs a line of the diff"), true
		}
	case "L":
		if m.advisor == nil {
			return keys.Refuse("no advisory client here"), true
		}

		if file, _ := m.lockedAt(); file == "" {
			return keys.Refuse("no lockfile under the cursor"), true
		}
	default:
		return keys.Allow(), false
	}

	return keys.Allow(), true
}

// writeGate is gate's half about writing something on the cursor's row, split
// out so a file's own heading can differ from a line of code without growing
// gate's one switch.
func (m *Model) writeGate(k string) (keys.Availability, bool) {
	switch k {
	case "a":
		if !m.onCode() && !m.onFileHead() {
			return keys.Refuse("needs a line of the diff or a file's heading"), true
		}
	case "s", "V":
		if !m.onCode() {
			return keys.Refuse("needs a line of the diff"), true
		}
	case "m", "E":
		if m.current() < 0 {
			return keys.Refuse("no comment under the cursor; a writes one"), true
		}
	case "e":
		if !m.canWrite() {
			return keys.Refuse("nothing to write here; a starts a comment"), true
		}
	default:
		return keys.Allow(), false
	}

	return keys.Allow(), true
}

// inert reports a key with nothing to act on under the cursor.
func (m *Model) inert(k string) bool { return !m.gate(k).OK }

// refusing is the set of keys whose own handler does not explain itself where
// the gate refuses, so the press says the gate's reason instead: m would open
// its chord over nothing and only say so after the second key. Every other
// gated key's handler already names its own reason (a, s, V, E, F, L, B, X,
// K), or does something useful anyway (e on the review's prose edits it, and
// T writes the todo set to a file).
func refusing(k string) bool { return k == "m" }

// refuse answers a key the gate refuses where the cursor is, reporting whether
// it did, and logs the press so `second-look keys` can list keys reached for
// where they could not work.
func (m *Model) refuse(msg tea.KeyPressMsg) bool {
	k := msg.String()
	if m.pending != 0 || !refusing(k) {
		return false
	}

	a := m.gate(k)
	if a.OK {
		return false
	}

	m.say(k+"  "+a.Why, false)
	m.j.Gated(ctxReview, k)

	return true
}

// legend is the help tree with every root key the gate refuses marked off
// with its reason. A key on a page under a chord is that chord's object, and
// the chord's own row carries the dimming for it.
func (m *Model) legend() []keyhint.Hint {
	tree := m.helpTree()

	for i, h := range tree {
		if h.Key == "" || h.Head {
			continue
		}

		for _, k := range palette.Sequences(h.Key) {
			if a := m.gate(k); !a.OK {
				tree[i].Off, tree[i].Why = true, a.Why

				break
			}
		}
	}

	return tree
}

// paletteEntries is the legend flattened into the palette's rows, with recent
// runs from the ledger and, for the command palette, its own commands after
// the keys. The legend lists keys alone.
func (m *Model) paletteEntries(mode palette.Mode) []palette.Entry {
	es := palette.FromLegend(m.legend())
	if mode == palette.Browse {
		return palette.WithSeen(es, m.coach.Ledger(), false)
	}

	band := "show the next step as a band above the footer"
	if m.band {
		band = "show the next step in the footer's status slot"
	}

	es = append(es,
		palette.Entry{
			Action: cmdNext, What: band, Group: screenGroup,
			Prose: []string{"The band names each next step, why, and the key that takes it."},
		},
		palette.Entry{Action: cmdTipsOff, What: "stop tips about keys not yet used", Group: screenGroup},
		palette.Entry{Action: cmdTipsOn, What: "show tips about keys not yet used", Group: screenGroup},
		coach.WelcomeEntry(),
	)

	return palette.WithSeen(es, m.coach.Ledger(), false)
}

// paletteLayout is the drawer from 100 terminal columns and the strip below,
// judged by the terminal rather than by the margin-narrowed frame.
func paletteLayout(terminal int) palette.Layout {
	if terminal >= palette.DrawerFrom {
		return palette.Drawer
	}

	return palette.Strip
}

// openPalette opens the legend (Browse) or the command palette (Run) over the
// diff.
func (m *Model) openPalette(mode palette.Mode) {
	m.pal.Open(mode, m.paletteEntries(mode))
}

// runPalette carries out what the palette ran: one of its own commands, or a
// legend row's key sequence replayed as if it had been typed, which keeps a
// palette run and a key press one code path.
func (m *Model) runPalette(run palette.RunMsg) tea.Cmd {
	switch run.Action {
	case cmdNext:
		m.band = !m.band
		m.say("next steps "+map[bool]string{true: "in a band", false: "in the status line"}[m.band], false)

		return nil
	case cmdTipsOff, cmdTipsOn:
		m.coach.Ledger().SetTipsOff(run.Action == cmdTipsOff)
		m.say("tips "+strings.TrimPrefix(run.Action, ":tips "), false)

		return nil
	case coach.WelcomeAction:
		m.welcome.Open()

		return nil
	}

	m.coach.Observe(coach.Event{At: time.Now(), Action: run.Action, Palette: true})

	var cmds []tea.Cmd

	for _, k := range strings.Fields(run.Action) {
		_, cmd := m.handleKey(pressOf(k))
		cmds = append(cmds, cmd)
	}

	if run.From == palette.Run && m.status == "" {
		m.say("ran "+run.Key+" · next time press "+run.Key, false)
	}

	return tea.Batch(cmds...)
}

// pressOf is the key press Bubble Tea delivers for a key as it spells it:
// "j", "G", "ctrl+d", "space", "enter", "shift+tab".
func pressOf(k string) tea.KeyPressMsg {
	named := map[string]rune{
		"enter": tea.KeyEnter, escKey: tea.KeyEscape, tabKey: tea.KeyTab, spaceKey: tea.KeySpace,
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
		"backspace": tea.KeyBackspace,
	}

	var mod tea.KeyMod

	for {
		switch {
		case strings.HasPrefix(k, "ctrl+"):
			mod, k = mod|tea.ModCtrl, strings.TrimPrefix(k, "ctrl+")

			continue
		case strings.HasPrefix(k, "alt+"):
			mod, k = mod|tea.ModAlt, strings.TrimPrefix(k, "alt+")

			continue
		case strings.HasPrefix(k, "shift+"):
			mod, k = mod|tea.ModShift, strings.TrimPrefix(k, "shift+")

			continue
		}

		break
	}

	if code, ok := named[k]; ok {
		msg := tea.KeyPressMsg{Code: code, Mod: mod}
		if k == spaceKey {
			msg.Text = " "
		}

		return msg
	}

	r, _ := utf8.DecodeRuneInString(k)
	if mod != 0 {
		return tea.KeyPressMsg{Code: r, Mod: mod}
	}

	return tea.KeyPressMsg{Code: r, Text: k}
}

// reviewState is what the coach's rules read about the review.
type reviewState struct {
	unread, rulings, staged int
	inUnread, lockfile      bool
}

// state is the review as the coach's rules read it.
func (m *Model) state() reviewState {
	var s reviewState

	for i := range m.screen.rows {
		if m.isUnread(m.screen.rows[i]) {
			s.unread++
		}
	}

	if m.cursor >= 0 && m.cursor < len(m.screen.rows) {
		s.inUnread = m.inUnreadHunk(m.screen.rows[m.cursor])
	}

	for t := range m.threads {
		if m.awaitsRuling(t) {
			s.rulings++
		}
	}

	s.staged = len(m.review.Comments)

	if m.advisor != nil {
		_, pkgs := m.lockedAt()
		s.lockfile = pkgs != nil
	}

	return s
}

// reviewRules are the review's next steps, in the order a reading goes: read
// what is left, rule on what waits, then submit.
func reviewRules() []coach.Rule[reviewState] {
	plural := func(n int, one string) string {
		if n == 1 {
			return "1 " + one
		}

		return strconv.Itoa(n) + " " + one + "s"
	}

	return []coach.Rule[reviewState]{
		{
			ID: "review.unread", Action: "] u", Key: "]u", What: "next unread", Tone: coach.Next, Priority: rankUnread,
			When: func(s reviewState) bool { return s.unread > 0 && !s.inUnread },
			Say:  func(s reviewState) string { return plural(s.unread, "hunk") + " not yet read" },
			Why:  func(reviewState) string { return "space marks one read" },
		},
		{
			ID: "review.ruling", Action: "tab", Key: "tab", What: "what waits", Tone: coach.Next, Priority: rankRulings,
			When: func(s reviewState) bool { return s.rulings > 0 },
			Say: func(s reviewState) string {
				if s.rulings == 1 {
					return "1 finding waits on a ruling"
				}

				return plural(s.rulings, "finding") + " wait on a ruling"
			},
			Why: func(reviewState) string { return "r ready, t todo, x skip" },
		},
		{
			ID: "review.submit", Action: "S", Key: "S", What: submitWord, Tone: coach.Next,
			When: func(s reviewState) bool { return s.unread == 0 && s.rulings == 0 && s.staged > 0 },
			Say:  func(s reviewState) string { return "all read, " + plural(s.staged, "comment") + " staged" },
			Why:  func(reviewState) string { return "S then a, r, or c picks the event" },
		},
		{
			ID: "review.lockfile", Action: "L", Key: "L", What: "the lockfile", Tone: coach.Could,
			When: func(s reviewState) bool { return s.lockfile },
			Say:  func(reviewState) string { return "a lockfile is under the cursor" },
			Why:  func(reviewState) string { return "L names what it would send to osv.dev first" },
		},
	}
}

// reviewTips are keys worth knowing that a reader may not reach for, first the
// ones a first review needs.
func reviewTips() []coach.Suggestion {
	return []coach.Suggestion{
		{ID: "review.tip.palette", Action: paletteKey, Key: paletteKey, Say: "finds any command by name and runs it"},
		{ID: "review.tip.unread", Action: "] u", Key: "]u", Say: "jumps to the next hunk you have not read"},
		{ID: "review.tip.jumps", Action: jumpBack, Key: jumpBack, Say: "goes back to where you were before a jump"},
		{ID: "review.tip.only-new", Action: "U", Key: "U", Say: "shows only what is new since you last read"},
		{ID: "review.tip.outline", Action: "I", Key: "I", Say: "opens the outline of the review's chapters"},
		{ID: "review.tip.what", Action: "K", Key: "K", Say: "says what the symbol under the cursor is"},
	}
}

// moving reports a key that only moves, which the coach watches for a long
// way round but never counts as a command run.
func moving(k string) bool {
	switch k {
	case "j", "k", "up", "down", "ctrl+d", "ctrl+u", "ctrl+e", "ctrl+y", "pgup", "pgdown":
		return true
	}

	return false
}

// observe tells the coach about a key the review just dispatched: any key
// clears a tip, and one that completed a command is a run. A key that opened a
// chord waits for the key that completes it.
func (m *Model) observe(msg tea.KeyPressMsg, prefix rune) tea.Cmd {
	now := time.Now()
	m.coach.Press(now)

	if m.writing() || m.pal.IsOpen() || m.pending != 0 {
		return m.coach.WakeAt(now)
	}

	action := msg.String()
	if prefix != 0 {
		action = string(prefix) + " " + action
	}

	m.coach.Observe(coach.Event{At: now, Action: action, Motion: moving(action)})

	return m.coach.WakeAt(now)
}

// discoverMsg answers the messages the palette and the coach send back,
// reporting whether msg was one.
func (m *Model) discoverMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case palette.RunMsg:
		return tea.Batch(m.runPalette(msg), m.takeCmd()), true
	case palette.RefusedMsg:
		m.j.Gated(ctxHelp, msg.Action)

		return nil, true
	case coach.IdleMsg:
		return m.idle(msg), true
	}

	return nil, false
}

// idle asks the coach for a tip once the reader has stopped, saves the
// ledger, which is the natural lull to write it in, and arms the next ask when
// nothing showed.
func (m *Model) idle(msg coach.IdleMsg) tea.Cmd {
	busy := m.writing() || !m.quiet() || m.pending != 0 || m.asking != askNothing || m.picking != nil
	m.coach.Tip(msg.At, busy)

	if err := m.coach.Ledger().Save(); err != nil {
		m.say("could not save the coach's notes: "+err.Error(), true)
	}

	return m.coach.WakeAt(msg.At)
}

// nextStep is the step the coach points at, false when none holds.
func (m *Model) nextStep() (coach.Suggestion, bool) {
	return m.coach.Next(m.state(), func(action string) keys.Availability {
		return m.gate(strings.ReplaceAll(action, " ", ""))
	})
}

// quietFooter is the footer with no status to show: a tip until the next key,
// else the next step, else the hints alone. In the band layout the next steps
// and the tip draw above the hints instead.
func (m *Model) quietFooter() []string {
	if line := m.welcomeLine(); line != "" {
		return []string{" " + line}
	}

	hints := m.hints()
	tip, tipping := m.coach.Showing()
	next, ok := m.nextStep()

	if ok {
		hints = coach.Mark(hints, next)
	}

	if m.band {
		all := m.coach.All(m.state(), nil)

		var t *coach.Suggestion
		if tipping {
			t = &tip
		}

		lines := coach.BandLines(m.styles.skin, all, t, m.width)

		return append(lines, " "+keyhint.Fit(footerStyles(m.styles), hints, m.width-1))
	}

	// The next step takes room from the hints a narrow footer sheds first and
	// never from the live ones, since the hints are the map a reader orients
	// by; a dim key keeps its place while the line has room for it. A tip is
	// rare and lasts one key, so it may take more.
	lead := ""
	kept := slices.DeleteFunc(slices.Clone(hints), func(h keyhint.Hint) bool { return h.Priority <= shedFirst })
	live := slices.DeleteFunc(slices.Clone(kept), func(h keyhint.Hint) bool { return h.Off })
	room := m.width - textWidth(keyhint.Line(footerStyles(m.styles), live)) - statusGap - 1

	switch {
	case tipping:
		lead = coach.TipLine(m.styles.skin, tip, max(room, m.width/tipShare))
	case ok:
		// A step is drawn whole or not at all: cut short it reads as a broken
		// sentence, and the reader can always ask the band for it.
		if whole := coach.Line(m.styles.skin, next, m.width); textWidth(whole) <= room {
			lead = whole
		}
	}

	if lead == "" {
		return []string{" " + keyhint.Fit(footerStyles(m.styles), hints, m.width-1)}
	}

	if line := keyhint.Fit(footerStyles(m.styles), kept, m.width-textWidth(lead)-statusGap); line != "" {
		return []string{" " + lead + "  " + line}
	}

	return []string{" " + lead}
}
