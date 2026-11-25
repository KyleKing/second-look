package tui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/tui/coach"
	"github.com/kyleking/aragonite/tui/keyhint"
)

// welcomeTitle is the card's top edge on every screen: the first launch is
// one per reader, whichever screen it opens on.
const welcomeTitle = "Welcome to second-look"

// firstKey is the key a binding is pressed with first, which is what a card
// teaching the binding names.
func firstKey(b key.Binding) string {
	if ks := b.Keys(); len(ks) > 0 {
		return ks[0]
	}

	return b.Help().Key
}

// reviewWelcome is the review screen's first-run card: the keys a first
// review is read and written with, labeled from the keymap. A local review
// (local) has nothing to submit to, so its card ends on the note it keeps.
func reviewWelcome(k keyMap, local bool) *coach.Welcome {
	forward := firstKey(k.Forward)

	w := &coach.Welcome{
		Title: welcomeTitle,
		Lead: "Read a pull request hunk by hunk, stage comments as you go, " +
			"and submit them as one review.",
		Starters: []keyhint.Hint{
			{Key: forward + "h", What: "next hunk"},
			{Key: firstKey(k.Seen), What: "mark the hunk read"},
			{Key: forward + "u", What: "next hunk you have not read"},
			{Key: firstKey(k.Write), What: "add a comment on the line"},
			{Key: firstKey(k.Submit), What: "submit the review"},
		},
		Help:    firstKey(k.Help),
		Palette: paletteKey,
	}

	if local {
		w.Lead = "Read your own change hunk by hunk and leave notes on it as you go. " +
			"Under jj, a hunk read here is read in jojo and jj-diff too."
		w.Starters = w.Starters[:len(w.Starters)-1]
	}

	return w
}

// queueWelcome is the queue's first-run card: the keys that read a queue.
func queueWelcome(k keyMap, l listKeys) *coach.Welcome {
	return &coach.Welcome{
		Title: welcomeTitle,
		Lead: "Your review queue: pull requests waiting on you, the conversations " +
			"on them, and reviews staged here.",
		Starters: []keyhint.Hint{
			{Key: firstKey(k.Accept), What: "open the row: review it, or read the conversation"},
			{Key: firstKey(l.Mark), What: "mark the row read"},
			{Key: firstKey(l.Unread), What: "next row that still needs a look"},
			{Key: firstKey(k.Forward), What: "next queue"},
			{Key: firstKey(k.Search), What: "narrow the rows to a word"},
		},
		Help:    firstKey(k.Help),
		Palette: paletteKey,
	}
}

// welcomed answers a key while the card is up: any key takes the card down and
// the dismissal is saved at once, since a short first session never reaches the
// idle lull the ledger is otherwise written in. It reports whether the key
// should go on to do what it does, which only the card's help and palette keys
// do, so a first keystroke never stages or posts anything, and what saving said.
func welcomed(w *coach.Welcome, l *coach.Ledger, k string) (bool, error) {
	if !w.Showing() {
		return true, nil
	}

	pass := w.Key(l, k, time.Now())
	if err := l.Save(); err != nil {
		return pass, fmt.Errorf("saving the coach's notes: %w", err)
	}

	return pass, nil
}

// welcomeLine is the card as one footer line, for a terminal too small to
// draw it over the diff, and empty where the card is drawn or down.
func (m *Model) welcomeLine() string {
	// The body's height is read off the whole screen rather than through
	// viewHeight, which asks the footer this line is part of.
	if !m.welcome.Showing() || coach.WelcomeFits(m.width, m.height-headerRows-1) {
		return ""
	}

	return m.welcome.Line(m.styles.skin, m.width-1)
}

// welcomeKey answers a key while the card is up, reporting whether the key
// goes on to act.
func (m *Model) welcomeKey(msg tea.KeyPressMsg) bool {
	if !m.welcome.Showing() {
		return true
	}

	pass, err := welcomed(m.welcome, m.ledger, msg.String())
	if err != nil {
		m.say(err.Error(), true)
	}

	return pass
}
