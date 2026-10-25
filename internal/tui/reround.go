package tui

import (
	"regexp"
	"slices"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/order"
)

// reach is how many lines either side of the one a thread hangs from must still
// read the same for the thread to count as untouched.
const reach = 2

var fixedIn = regexp.MustCompile("(?i)\\bfixed in `?([0-9a-f]{7,40})\\b")

// namedCommit is the commit a comment says it fixed the matter in, or empty.
func namedCommit(body string) string {
	if m := fixedIn.FindStringSubmatch(body); m != nil {
		return m[1]
	}

	return ""
}

// answer is whether a todo has been dealt with since a round, and the commit
// that did it where one is known.
type answer struct {
	sha string
	ok  bool
}

// evidence is what a todo thread's answer is decided from.
type evidence struct {
	// named is the commit the thread's latest comment says fixed it.
	named string
	// touched is whether the lines the thread hangs from changed since the round.
	touched bool
	// wrote is the last commit to write those lines, where the commits are known.
	wrote string
	// head is the commit the review is on.
	head string
}

// answeredBy decides a todo's answer. A commit the thread names is believed
// before the diff, and a diff that left the lines alone answers nothing.
func answeredBy(e evidence) answer {
	switch {
	case e.named != "":
		return answer{sha: e.named, ok: true}
	case !e.touched:
		return answer{}
	case e.wrote != "":
		return answer{sha: e.wrote, ok: true}
	}

	return answer{sha: e.head, ok: true}
}

// touched reports whether the lines around an anchor read differently in cur
// than anywhere in old, and which hunk of cur the anchor sits in. A line cur no
// longer carries counts as touched, since a push that removed it answered it.
func touched(old, cur *diff.Diff, a anchor) (bool, int) {
	f := fileOf(cur, a.path)
	if f == nil {
		return true, 0
	}

	img := imageOf(f, a.side)

	at := slices.IndexFunc(img, func(l diff.Line) bool { return numberOn(l, a.side) == a.line })
	if at < 0 {
		return true, 0
	}

	before := fileOf(old, a.path)
	if before == nil {
		return true, img[at].Hunk
	}

	return !holds(imageOf(before, a.side), neighbors(img, at)), img[at].Hunk
}

func fileOf(d *diff.Diff, path string) *diff.File {
	for i := range d.Files {
		f := &d.Files[i]
		if f.NewPath == path || (f.NewPath == "" && f.OldPath == path) {
			return f
		}
	}

	return nil
}

// imageOf is the lines of a file as one side of the diff reads them.
func imageOf(f *diff.File, side string) []diff.Line {
	gone := byte(diff.KindAdd)
	if side == artifact.SideRight {
		gone = diff.KindRemove
	}

	out := make([]diff.Line, 0, len(f.Lines))

	for _, l := range f.Lines {
		if l.Kind != gone {
			out = append(out, l)
		}
	}

	return out
}

func numberOn(l diff.Line, side string) int {
	if side == artifact.SideLeft {
		return l.Old
	}

	return l.New
}

// neighbors is the line at i and up to reach lines either side of it from the
// same hunk.
func neighbors(img []diff.Line, i int) []diff.Line {
	from, to := i, i

	for from > 0 && i-from < reach && img[from-1].Hunk == img[i].Hunk {
		from--
	}

	for to+1 < len(img) && to-i < reach && img[to+1].Hunk == img[i].Hunk {
		to++
	}

	return img[from : to+1]
}

// holds reports whether want appears in img as an unbroken run of one hunk.
func holds(img, want []diff.Line) bool {
	for i := 0; i+len(want) <= len(img); i++ {
		if reads(img[i:i+len(want)], want) {
			return true
		}
	}

	return false
}

func reads(got, want []diff.Line) bool {
	for i := range want {
		if got[i].Text != want[i].Text || got[i].Hunk != got[0].Hunk {
			return false
		}
	}

	return true
}

// lastWriter is the final commit listed to have written the hunk, or empty.
func (m *Model) lastWriter(path string, hunk int) string {
	ref := order.Ref{Path: path, Hunk: hunk}

	for i := len(m.commits) - 1; i >= 0; i-- {
		if slices.Contains(m.commits[i].Hunks, ref) {
			return m.commits[i].ID
		}
	}

	return ""
}

// answersSince is how each todo thread stands against a round's diff, by thread
// index.
func (m *Model) answersSince(old *diff.Diff) map[int]answer {
	out := map[int]answer{}

	for i := range m.threads {
		if m.verdictOfThread(i) != vTodo {
			continue
		}

		t := &m.threads[i]
		was, hunk := touched(old, m.diff, anchor{path: t.Path, side: t.Side, line: t.Line})
		e := evidence{touched: was, head: m.review.HeadSHA}

		if was && hunk > 0 {
			e.wrote = m.lastWriter(t.Path, hunk)
		}

		if n := len(t.Notes); n > 0 {
			e.named = namedCommit(t.Notes[n-1].Body)
		}

		out[i] = answeredBy(e)
	}

	return out
}

// answeredOf is how many of the answers are yes, and how many there are.
func answeredOf(answers map[int]answer) (int, int) {
	n := 0

	for _, a := range answers {
		if a.ok {
			n++
		}
	}

	return n, len(answers)
}

func (a answer) word() string {
	if !a.ok {
		return "not answered"
	}

	return "answered in " + short(a.sha)
}
