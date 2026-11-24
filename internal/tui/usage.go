package tui

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/kyleking/aragonite/keyusage"
	"github.com/kyleking/aragonite/tui/keyhint"
)

// StaleAfter is how long a key may go unpressed before `second-look keys`
// lists it as neglected, matching keyusage's top brightening level.
const StaleAfter = 28 * 24 * time.Hour

const (
	ctxReview  = "review"
	ctxQueue   = "queue"
	ctxHelp    = ctxReview + "/help"
	ctxAbout   = ctxReview + "/about"
	ctxOutline = ctxReview + "/outline"
	ctxGraph   = ctxReview + "/graph"
	ctxThread  = ctxReview + "/thread"
)

// keyContext names where the key about to be dispatched will land, following
// handleKey's order. A chord's second key shares its leader's context.
func (m *Model) keyContext(msg tea.KeyPressMsg) string {
	switch {
	case m.asking != askNothing || m.picking != nil:
		return ctxReview + "/prompt"
	case m.pending != 0:
		return ctxReview
	case m.pal.IsOpen():
		return ctxHelp
	case m.aboutOpen:
		return ctxAbout
	case m.outline.open:
		return ctxOutline
	case m.showing != nil:
		return ctxReview + "/hover"
	case m.threadHere() >= 0 && key.Matches(msg, m.keys.verdicts()...):
		return ctxThread
	case m.graphHere() != nil && key.Matches(msg, m.keys.graph()...):
		return ctxGraph
	}

	return ctxReview
}

// keyContext is the queue tab being read, or the legend or label picker over it.
func (l *List) keyContext() string {
	switch {
	case l.pal.IsOpen():
		return queueCtx("help")
	case l.labeler != nil:
		return queueCtx("labels")
	case len(l.tabs) > 0:
		return queueCtx(l.tabs[l.at].Name)
	}

	return ctxQueue
}

func queueCtx(name string) string { return ctxQueue + "/" + name }

func (k keyMap) verdicts() []key.Binding { return []key.Binding{k.Ready, k.Todo, k.Skip, k.Resolve} }

func (k keyMap) graph() []key.Binding {
	return []key.Binding{k.Down, k.Up, k.Callers, k.Callees, k.Accept}
}

// Keymap is every context keyContext names, built from the tables the screens
// dispatch and draw their legends from. Only the Name and Go of each tab are read.
func Keymap(tabs []Tab) keyusage.Keymap {
	km := keyusage.Keymap{}
	keys := defaultKeyMap()

	km[ctxReview] = reviewBindings(keys)
	km[ctxThread] = bindings(keys.verdicts())
	km[ctxGraph] = bindings(keys.graph())
	km[ctxHelp] = helpBindings()
	km[ctxAbout] = append(
		fields(keys, "Down", "Up", "HalfDown", "HalfUp", "Top"),
		keyusage.Binding{Keys: []string{"z"}, What: "fold", Kids: pairs(foldObjects())},
	)
	km[ctxOutline] = fields(keys, "Down", "Up", "HalfDown", "HalfUp", "Top", "Bottom", "NextNote", "PrevNote",
		"Accept", "Back", "Outline", "Quit", "View", "Comments", "Threads", "Trouble")

	km[ctxQueue] = queueBindings(keys, 0, nil)

	for i := range tabs {
		km[queueCtx(tabs[i].Name)] = queueBindings(keys, len(tabs), tabs[i].Go)
	}

	return km
}

var leaderKids = map[string]func() [][2]string{
	"Ask":      allAskObjects,
	"Backward": func() [][2]string { return objects("[") },
	"Forward":  func() [][2]string { return objects("]") },
	"Look":     lookObjects,
	"React":    reactObjects,
	"State":    states,
	"Submit":   events,
	"Write":    severities,
}

// reviewBindings is every binding the diff answers directly: the ones another
// context owns, and the keys only a pane or the agent session take, are left to theirs.
func reviewBindings(keys keyMap) []keyusage.Binding {
	owned := []string{
		"Back", "Callees", "Callers", "DropQueued", "Draft", "EndPane", "Expand", "Interrupt",
		"Ready", "Resolve", "SendSet", "Skip", "Todo", "Top",
	}

	var out []keyusage.Binding

	v := reflect.ValueOf(keys)
	for i := range v.NumField() {
		name := v.Type().Field(i).Name
		if slices.Contains(owned, name) {
			continue
		}

		b := binding(bindingAt(v, i))

		switch kids, ok := leaderKids[name]; {
		case ok:
			b.Kids = pairs(kids())
		case name == "Zed":
			b.Kids = zKids()
		}

		out = append(out, b)
	}

	return append(out,
		keyusage.Binding{Keys: []string{"g"}, What: "go", Kids: pairs(goObjects())},
		keyusage.Binding{Keys: []string{homeKey}, What: keys.Top.Help().Desc},
	)
}

func zKids() []keyusage.Binding {
	var out []keyusage.Binding

	for _, h := range zPage() {
		if !h.Head && h.Key != "" {
			out = append(out, keyusage.Binding{Keys: []string{h.Key}, What: h.What})
		}
	}

	return out
}

// helpBindings are the legend's scroll keys and the keys that open one of its pages.
func helpBindings() []keyusage.Binding {
	out := []keyusage.Binding{
		{Keys: []string{upKey}, What: "scroll up"},
		{Keys: []string{downKey}, What: "scroll down"},
		{Keys: []string{pgUpKey, "ctrl+u"}, What: "page up"},
		{Keys: []string{pgDownKey, "ctrl+d"}, What: "page down"},
		{Keys: []string{homeKey}, What: "first page"},
		{Keys: []string{endKey}, What: "last page"},
		{Keys: []string{escKey}, What: "leave the page"},
		{Keys: []string{"?", ctrlCKey}, What: "close"},
	}

	for _, h := range helpTree(nil) {
		if len(h.Kids) == 0 {
			continue
		}

		b := keyusage.Binding{Keys: []string{h.Key}, What: h.What}
		for _, k := range h.Kids {
			if !k.Head && k.Key != "" {
				b.Kids = append(b.Kids, keyusage.Binding{Keys: []string{k.Key}, What: k.What})
			}
		}

		out = append(out, b)
	}

	return out
}

func queueBindings(keys keyMap, tabs int, goes []GoKey) []keyusage.Binding {
	out := fields(keys, "Up", "Down", "HalfUp", "HalfDown", "PeekUp", "PeekDown", "Bottom", "Forward", "Backward",
		"Search", "Help", "Accept", "Label", "Merge", "Quit")

	lv := reflect.ValueOf(defaultListKeys())
	for i := range lv.NumField() {
		out = append(out, binding(bindingAt(lv, i)))
	}

	top := make([]keyusage.Binding, 0, len(goes)+1)
	top = append(top, keyusage.Binding{Keys: []string{"g"}, What: topWord})
	for _, o := range goes {
		top = append(top, keyusage.Binding{Keys: []string{o.Key}, What: o.What})
	}

	out = append(out,
		keyusage.Binding{Keys: []string{"g"}, What: "go", Kids: top},
		keyusage.Binding{Keys: []string{homeKey}, What: keys.Top.Help().Desc},
	)

	for n := 1; n <= tabs; n++ {
		out = append(out, keyusage.Binding{Keys: []string{strconv.Itoa(n)}, What: "tab " + strconv.Itoa(n)})
	}

	return out
}

// bindingAt is field i of a struct holding only key.Binding fields.
func bindingAt(v reflect.Value, i int) key.Binding {
	if b, ok := v.Field(i).Interface().(key.Binding); ok {
		return b
	}

	return key.Binding{}
}

func binding(b key.Binding) keyusage.Binding {
	return keyusage.Binding{Keys: b.Keys(), What: b.Help().Desc}
}

func bindings(bs []key.Binding) []keyusage.Binding {
	out := make([]keyusage.Binding, len(bs))
	for i, b := range bs {
		out[i] = binding(b)
	}

	return out
}

// fields reads the named bindings off a keyMap.
func fields(keys keyMap, names ...string) []keyusage.Binding {
	v := reflect.ValueOf(keys)
	out := make([]keyusage.Binding, 0, len(names))

	for _, n := range names {
		f, _ := v.Type().FieldByName(n)
		out = append(out, binding(bindingAt(v, f.Index[0])))
	}

	return out
}

func pairs(ps [][2]string) []keyusage.Binding {
	out := make([]keyusage.Binding, len(ps))
	for i, p := range ps {
		out[i] = keyusage.Binding{Keys: []string{p[0]}, What: p[1]}
	}

	return out
}

// fresher says which legend keys have not been pressed lately.
type fresher struct {
	now  time.Time
	seen keyusage.Usage
	keys keyusage.Keymap
}

// newFresher is nil, which brightens nothing, when there is no history to read.
func newFresher(seen *keyusage.Usage, tabs []Tab) *fresher {
	if seen == nil {
		return nil
	}

	return &fresher{seen: *seen, keys: Keymap(tabs), now: time.Now()}
}

// fresh is the brightening level of the binding that keys press in ctx. A key
// the keymap does not bind stays plain, so a compound legend row does too.
func (f *fresher) fresh(ctx string, keys ...string) int {
	if f == nil || len(keys) == 0 {
		return 0
	}

	bs, path := f.keys[ctx], make([]string, 0, len(keys))

	for _, k := range keys {
		i := slices.IndexFunc(bs, func(b keyusage.Binding) bool { return slices.Contains(b.Keys, k) })
		if i < 0 {
			return 0
		}

		path, bs = append(path, bs[i].Keys[0]), bs[i].Kids
	}

	return f.seen.Level(ctx, strings.Join(path, " "), f.now)
}

// brighten marks the stale hints, and the stale pages under them, on a copy.
func (f *fresher) brighten(ctx string, hints []keyhint.Hint, under ...string) []keyhint.Hint {
	if f == nil {
		return hints
	}

	out := slices.Clone(hints)
	for i := range out {
		h := &out[i]
		if h.Head || h.Key == "" {
			continue
		}

		at := append(slices.Clone(under), h.Key)
		h.Fresh = f.fresh(ctx, at...)
		h.Kids = f.brighten(ctx, h.Kids, at...)
	}

	return out
}
