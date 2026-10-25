package tui

import (
	"context"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/kyleking/aragonite/tui/overlay"

	"github.com/kyleking/second-look/internal/labels"
)

// Labeling reaches the forge for the label picker: the labels a repository
// defines, and putting one on a pull request or taking it off.
type Labeling struct {
	Read func(ctx context.Context, repo string) ([]labels.Label, error)
	Set  func(ctx context.Context, at labels.Target, name string, on bool) error
}

func (l Labeling) ready() bool { return l.Read != nil && l.Set != nil }

// WithLabeling gives the review a label picker on l.
func WithLabeling(l Labeling) Option {
	return func(m *Model) { m.labeling = l }
}

type labelsReadMsg struct {
	at  labels.Target
	all []labels.Label
	err error
}

type labelSetMsg struct {
	at   labels.Target
	key  string
	name string
	on   bool
	err  error
}

func readLabels(ctx context.Context, l Labeling, at labels.Target) tea.Cmd {
	return func() tea.Msg {
		all, err := l.Read(ctx, at.Repo)

		return labelsReadMsg{at: at, all: all, err: err}
	}
}

func setLabel(ctx context.Context, l Labeling, at labels.Target, rowKey, name string, on bool) tea.Cmd {
	return func() tea.Msg {
		return labelSetMsg{at: at, key: rowKey, name: name, on: on, err: l.Set(ctx, at, name, on)}
	}
}

const (
	upKey   = "up"
	downKey = "down"
)

const labelRows = 10

const descriptionRoom = 48

type labelPicker struct {
	at         labels.Target
	all        []labels.Label
	loading    bool
	set        map[string]bool
	input      textinput.Model
	cursor     int
	note       string
	noteFailed bool
	err        string
}

func newLabelPicker(at labels.Target, has []string) (*labelPicker, tea.Cmd) {
	set := make(map[string]bool, len(has))
	for _, n := range has {
		set[n] = true
	}

	p := &labelPicker{at: at, loading: true, set: set, input: newPrompt()}

	return p, p.input.Focus()
}

func (p *labelPicker) hear(msg labelsReadMsg) {
	if p == nil || msg.at != p.at {
		return
	}

	p.loading = false

	if msg.err != nil {
		p.err = oneLine(msg.err)

		return
	}

	p.all = msg.all
}

func (p *labelPicker) shown() []labels.Label {
	want := strings.ToLower(strings.TrimSpace(p.input.Value()))

	var out []labels.Label

	for _, l := range p.all {
		if strings.Contains(strings.ToLower(l.Name), want) {
			out = append(out, l)
		}
	}

	return out
}

type labelStep struct {
	done bool
	name string
	on   bool
}

// key answers one key. Enter flips the label under the cursor and leaves the
// picker open, because a pull request is often given two at once.
func (p *labelPicker) key(msg tea.KeyPressMsg) labelStep {
	shown := p.shown()

	switch msg.String() {
	case escKey, "ctrl+c":
		return labelStep{done: true}
	case upKey, "ctrl+p":
		p.cursor = wrapIndex(p.cursor-1, len(shown))

		return labelStep{}
	case downKey, "ctrl+n":
		p.cursor = wrapIndex(p.cursor+1, len(shown))

		return labelStep{}
	case enterKey:
		if p.cursor >= len(shown) {
			return labelStep{}
		}

		name := shown[p.cursor].Name
		p.set[name] = !p.set[name]

		return labelStep{name: name, on: p.set[name]}
	}

	was := p.input.Value()
	p.input, _ = p.input.Update(msg)

	if p.input.Value() != was {
		p.cursor = 0
	}

	return labelStep{}
}

func wrapIndex(i, n int) int {
	if n == 0 {
		return 0
	}

	return (i%n + n) % n
}

func (p *labelPicker) settle(msg labelSetMsg) {
	if p == nil || msg.at != p.at {
		return
	}

	p.noteFailed = msg.err != nil
	if msg.err != nil {
		p.set[msg.name] = !msg.on
		p.note = oneLine(msg.err)

		return
	}

	p.note = labeledWord(msg.name, msg.on)
}

func labeledWord(name string, on bool) string {
	if on {
		return "added " + name
	}

	return "removed " + name
}

func (p *labelPicker) prompt(s styles, width int) string {
	head := s.key.Render("l") + s.footer.Render(" "+p.at.Repo+"#"+strconv.Itoa(p.at.Number)+" ")
	tail := s.footer.Render("  [enter] toggle  [esc] done")

	return cut(head+p.input.View()+tail, width)
}

func (p *labelPicker) lines(s styles) []string {
	var out []string

	switch shown := p.shown(); {
	case p.err != "":
		out = append(out, s.fail.Render(p.err))
	case p.loading:
		out = append(out, s.note.Render("reading the labels of "+p.at.Repo+"…"))
	case len(shown) == 0:
		out = append(out, s.note.Render("no label matches"))
	default:
		for i, l := range shown {
			out = append(out, p.row(s, l, i == p.cursor))
		}
	}

	return out
}

func (p *labelPicker) row(s styles, l labels.Label, cursor bool) string {
	bar, mark := " ", "  "
	if cursor {
		bar = s.cursor.Render(cursorBar)
	}

	if p.set[l.Name] {
		mark = s.ok.Render("✓") + " "
	}

	swatch := lipgloss.NewStyle().Foreground(lipgloss.Color("#" + l.Color)).Render("●")
	line := bar + mark + swatch + " " + l.Name

	if l.Description != "" {
		line += "  " + s.note.Render(cut(l.Description, descriptionRoom))
	}

	return line
}

func (p *labelPicker) box(s styles, body []string, width int) []string {
	rows := p.lines(s)
	if p.note != "" {
		face := s.note
		if p.noteFailed {
			face = s.fail
		}

		rows = append(rows, face.Render(cut(p.note, width-boxMargin)))
	}

	height := min(len(rows), labelRows)
	title := "labels " + p.at.Repo + "#" + strconv.Itoa(p.at.Number)
	box := overlay.Box(boxStyles(s), title, rows, height, max(0, p.cursor-height+1))

	return overlay.Blit(body, box, max(0, width-textWidth(box[0])-1), max(0, len(body)-len(box)))
}

func (m *Model) openLabels() tea.Cmd {
	if !m.labeling.ready() || m.review.Local {
		m.say("labels are set on a pull request, and this screen has none to ask", true)

		return nil
	}

	at := labels.Target{Repo: m.review.Owner + "/" + m.review.Repo, Number: m.review.Number}

	var focus tea.Cmd

	m.labeler, focus = newLabelPicker(at, m.about.Labels)

	return tea.Batch(focus, readLabels(m.ctx, m.labeling, at))
}

func (m *Model) labelKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	step := m.labeler.key(msg)

	switch {
	case step.done:
		m.labeler = nil
	case step.name != "":
		at := m.labeler.at
		m.about.Labels = labels.With(m.about.Labels, step.name, step.on)
		m.labeler.note, m.labeler.noteFailed = labeledWord(step.name, step.on)+"…", false

		return m, setLabel(m.ctx, m.labeling, at, "", step.name, step.on)
	}

	return m, nil
}

func (m *Model) applyLabelSet(msg labelSetMsg) {
	m.labeler.settle(msg)

	if msg.err == nil {
		return
	}

	m.about.Labels = labels.With(m.about.Labels, msg.name, !msg.on)
	m.say(oneLine(msg.err), true)
}

func (l *List) openLabels(row *Row) tea.Cmd {
	if !l.labeling.ready() || l.labeled == nil {
		l.say("nothing here can do that", true)

		return nil
	}

	at, has, ok := l.labeled(row)
	if !ok {
		l.say("this row is not a pull request", true)

		return nil
	}

	var focus tea.Cmd

	l.labeler, focus = newLabelPicker(at, has)
	l.labelRow = row.Key

	return tea.Batch(focus, readLabels(context.Background(), l.labeling, at))
}

func (l *List) pickLabel(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	step := l.labeler.key(msg)

	switch {
	case step.done:
		l.labeler = nil
	case step.name != "":
		l.relabel(l.labelRow, step.name, step.on)
		l.rebuild()
		l.labeler.note, l.labeler.noteFailed = labeledWord(step.name, step.on)+"…", false

		return l, setLabel(context.Background(), l.labeling, l.labeler.at, l.labelRow, step.name, step.on)
	}

	return l, nil
}

func (l *List) applyLabelSet(msg labelSetMsg) {
	l.labeler.settle(msg)

	if msg.err == nil {
		return
	}

	l.relabel(msg.key, msg.name, !msg.on)
	l.rebuild()
	l.say(oneLine(msg.err), true)
}

func (l *List) labelOver(frame string) string {
	lines := strings.Split(frame, "\n")
	last := len(lines) - 1

	return strings.Join(append(l.labeler.box(l.styles, lines[:last], l.width), lines[last]), "\n")
}
