package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/diff"
)

// Asker is a one-shot question put to an agent: the prompt in, the answer's
// text back. It shares the dispatch configuration's argv — the question is
// the last argument and the answer comes back on stdout rather than to a
// file — so a review that can hand work off can also ask.
type Asker func(ctx context.Context, prompt string) (string, error)

// WithAsker gives the review its A chord: p for a path through the diff, c
// for a decision card on the hunk under the cursor, d for whether the diff
// is what it claims. Without one the chord says so rather than opening.
func WithAsker(a Asker) Option {
	return func(m *Model) { m.asker = a }
}

// askObjects are what A accepts: the three questions a reviewer asks an agent
// about a diff that is already on screen.
func askObjects() [][2]string {
	return [][2]string{
		{"p", "a path through"},
		{"c", "this hunk, decided"},
		{"d", "what it claims"},
	}
}

// askedMsg carries an asker's answer, titled by what was asked.
type askedMsg struct {
	title string
	text  string
	err   error
}

// saidLines draws an asker's answer: the question's title, then the text it
// answered with. It is prose, not a list to act on, so what runs over the
// frame is counted rather than scrolled to.
func (m *Model) saidLines() []string {
	room := max(minTroubleWidth, m.width-indent*2)
	fits := m.height - hoverChrome

	out := []string{m.styles.head.Render(m.showing.title), ""}
	lines := wrap(m.showing.said, room)

	for i, line := range lines {
		if len(out) >= fits {
			out = append(out, "", m.styles.footer.Render("  "+plural(len(lines)-i, "more line")+" not shown"))

			break
		}

		out = append(out, m.styles.body.Render("  "+line))
	}

	return append(out, "", m.styles.footer.Render("  any key to go back"))
}

// adviseKey answers the second key of the A chord.
func (m *Model) adviseKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg.String() {
	case "p":
		cmd = m.advise("a path through "+m.name(), m.pathPrompt())
	case "c":
		title, prompt, ok := m.cardPrompt()
		if !ok {
			m.say("A c asks about a hunk, and the cursor is not in one", false)

			return m, nil
		}

		cmd = m.advise(title, prompt)
	case "d":
		claim := m.claim()
		if claim == "" {
			m.say("nothing says what the change is meant to do; e on the review body writes one", true)

			return m, nil
		}

		cmd = m.advise("does "+m.name()+" do what it claims", m.driftPrompt(claim))
	default:
		m.say("nothing to ask for "+msg.String()+"; "+hintLine(styles{}, askObjects()), false)
	}

	return m, cmd
}

// advise asks the question behind the first frame like every other slow read:
// an agent answers in seconds, and a screen that held them would look dead.
func (m *Model) advise(title, prompt string) tea.Cmd {
	m.say("asking…", false)

	ask, ctx := m.asker, m.ctx

	return func() tea.Msg {
		text, err := ask(ctx, prompt)

		return askedMsg{title: title, text: text, err: err}
	}
}

// applyAsked puts the answer in the same overlay a line question leaves up:
// it is read, not acted on, so any key leaves it.
func (m *Model) applyAsked(msg askedMsg) {
	switch {
	case msg.err != nil:
		m.say(msg.title+": "+msg.err.Error(), true)
	case strings.TrimSpace(msg.text) == "":
		m.say("the agent said nothing", false)
	default:
		m.showing = &answerMsg{title: msg.title, said: msg.text}
		m.say("", false)
	}
}

// claim is what the change says it is for: the pull request's own title and
// body where there is one, else the review body, which on a local review is
// the only place an intent is written down.
func (m *Model) claim() string {
	if text := strings.TrimSpace(m.about.Title + "\n" + m.about.Body); text != "" {
		return text
	}

	return strings.TrimSpace(m.review.Body)
}

// askMap is the diff in the shape a question is asked over: each file's path
// and counts, and the hunk headers, which carry the name of the function a
// hunk sits in where the diff knows one.
func (m *Model) askMap(budget int) string {
	var b strings.Builder

	for _, f := range m.diff.Files {
		path := f.NewPath
		if path == "" {
			path = f.OldPath
		}

		adds, dels := 0, 0
		hunks := map[int]bool{}

		for _, l := range f.Lines {
			switch l.Kind {
			case diff.KindAdd:
				adds++
			case diff.KindRemove:
				dels++
			}

			hunks[l.Hunk] = true
		}

		fmt.Fprintf(&b, "%s +%d -%d\n", path, adds, dels)

		for h := range len(m.diff.Headers) {
			if !hunks[h+1] {
				continue
			}

			b.WriteString("  " + m.diff.Headers[h] + "\n")
		}

		if b.Len() > budget {
			b.WriteString("…\n")

			return b.String()
		}
	}

	return b.String()
}

// pathPrompt asks for the order the hunks are best read in, which is the
// question O answers from structure alone and this asks of what the change
// is.
func (m *Model) pathPrompt() string {
	return `A reviewer is about to read this diff. Order the files into a reading
path: the sequence in which the change explains itself, with the type or
contract first and the call sites and tests after. One line per file, in
order, as "<path> — <what a reader gets from it here, in a dozen words>".
Name nothing else.

` + m.askMap(adviseBudget)
}

// cardPrompt asks about the hunk under the cursor: what it does, the one
// thing to check, and the one question the author should answer. The card
// turns "read everything" into "answer this", which is the job a review
// actually is.
func (m *Model) cardPrompt() (string, string, bool) {
	r := m.screen.rows[m.cursor]
	if r.path == "" || r.hunk == 0 {
		return "", "", false
	}

	var b strings.Builder
	header := ""
	if r.hunk-1 < len(m.diff.Headers) {
		header = m.diff.Headers[r.hunk-1]
	}

	fmt.Fprintf(&b, "File: %s\n%s\n", r.path, header)

	for _, f := range m.diff.Files {
		if f.NewPath != r.path && f.OldPath != r.path {
			continue
		}

		for _, l := range f.Lines {
			if l.Hunk != r.hunk {
				continue
			}

			b.WriteString(string(l.Kind) + l.Text + "\n")
			if b.Len() > adviseBudget {
				break
			}
		}
	}

	return "what " + r.path + " asks of you", `A reviewer is standing on one hunk of a change. Answer in three short
lines and nothing else:

changed:  what the hunk does, in one sentence
verify:   the one thing a reviewer should check to believe it
question: the one question the author should be able to answer

` + b.String(), true
}

// driftPrompt asks the question a PR description never gets: whether the diff
// is the change it claims to be.
func (m *Model) driftPrompt(claim string) string {
	return `A change claims to be this:

` + claim + `

Here is the diff it ships:

` + m.askMap(adviseBudget) + `

Answer in a few lines, plainly: what does the diff do that the claim does not
mention, and what does the claim promise that the diff never touches? If the
two agree, say so in one line rather than inventing a difference.
`
}

// adviseBudget is how much diff a question may carry. An agent reads fast
// but a prompt that is the whole repository prices the answer out of a
// prototype's reach.
const adviseBudget = 24000
