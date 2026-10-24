// Package commits answers which commit of a change wrote each hunk of its
// cumulative diff, which is what reading a change commit by commit needs.
//
// Git answers through blame of the post-image lines, restricted to the
// change's own commits. Jj answers through each revision's own diff, walking
// the lines back from the tip. A hunk that only removes lines has no
// post-image to blame, so it is the last commit that touched its file.
package commits

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/order"
	"github.com/kyleking/second-look/internal/plan"
	"github.com/kyleking/second-look/internal/seen"
)

const field = "\x1f"

// Git lists the commits in from..to, oldest first, and maps d's hunks onto them.
func Git(ctx context.Context, root, from, to string, d *diff.Diff) ([]plan.Commit, error) {
	out, err := run(ctx, root, "git", "log", "--reverse", "--topo-order", "--no-merges",
		"--format=%H"+field+"%s", from+".."+to)
	if err != nil {
		return nil, err
	}

	var list []plan.Commit

	for line := range strings.Lines(out) {
		id, subject, _ := strings.Cut(strings.TrimRight(line, "\n"), field)
		list = append(list, plan.Commit{ID: id, Subject: subject})
	}

	return Blame(ctx, root, to, list, d)
}

// Blame maps d's hunks onto commits already listed, blaming at tip. A line
// blame gives to a commit outside the list belongs to no listed commit.
func Blame(ctx context.Context, root, tip string, list []plan.Commit, d *diff.Diff) ([]plan.Commit, error) {
	if len(list) == 0 {
		return nil, nil
	}

	revs, err := bounded(ctx, root, tip, list)
	if err != nil {
		return nil, err
	}

	index := make(map[string]int, len(list))
	for i, c := range list {
		index[c.ID] = i
	}

	out := slices.Clone(list)

	for _, f := range d.Files {
		owned, err := blameFile(ctx, root, revs, &f, index)
		if err != nil {
			return nil, err
		}

		for ref, owners := range owned {
			for _, i := range owners {
				out[i].Hunks = append(out[i].Hunks, ref)
			}
		}
	}

	return inDiffOrder(out, d), nil
}

// bounded is tip with every parent of the list that is not itself listed
// excluded, which is the range the list spans however it was found.
func bounded(ctx context.Context, root, tip string, list []plan.Commit) ([]string, error) {
	args := make([]string, 0, len(list)+3)
	args = append(args, "rev-list", "--parents", "--no-walk")

	for _, c := range list {
		args = append(args, c.ID)
	}

	out, err := run(ctx, root, "git", args...)
	if err != nil {
		return nil, err
	}

	listed := map[string]bool{}
	for _, c := range list {
		listed[c.ID] = true
	}

	revs := []string{tip}

	for line := range strings.Lines(out) {
		for _, parent := range strings.Fields(line)[1:] {
			if !listed[parent] && !slices.Contains(revs, "^"+parent) {
				revs = append(revs, "^"+parent)
			}
		}
	}

	return revs, nil
}

func blameFile(ctx context.Context, root string, revs []string, f *diff.File, index map[string]int,
) (map[order.Ref][]int, error) {
	added := addedByHunk(f)
	path := pathOf(f)
	owned := map[order.Ref][]int{}

	by, err := blameLines(ctx, root, revs, path, added)
	if err != nil {
		return nil, err
	}

	last := ""

	for _, h := range hunksOf(f) {
		ref := order.Ref{Path: path, Hunk: h}

		lines, ok := added[h]
		if !ok {
			if last == "" {
				var err error
				if last, err = lastTouching(ctx, root, revs, f); err != nil {
					return nil, err
				}
			}

			if i, ok := index[last]; ok {
				owned[ref] = []int{i}
			}

			continue
		}

		for _, n := range lines {
			if i, ok := index[by[n]]; ok && !slices.Contains(owned[ref], i) {
				owned[ref] = append(owned[ref], i)
			}
		}
	}

	return owned, nil
}

// blameLines is the commit blame gives each added line of one file.
func blameLines(ctx context.Context, root string, revs []string, path string, added map[int][]int,
) (map[int]string, error) {
	var spans []string

	for _, lines := range added {
		for _, run := range runs(lines) {
			spans = append(spans, "-L", fmt.Sprintf("%d,%d", run[0], run[1]))
		}
	}

	if len(spans) == 0 {
		return map[int]string{}, nil
	}

	out, err := run(ctx, root, "git", slices.Concat([]string{"blame", "--porcelain"}, spans, revs,
		[]string{"--", path})...)
	if err != nil {
		return nil, err
	}

	return porcelain(out), nil
}

var header = regexp.MustCompile(`^([0-9a-f]{40,64}) \d+ (\d+)`)

// porcelain reads the commit blame gives each final line, which every line's header names.
func porcelain(out string) map[int]string {
	by := map[int]string{}

	for line := range strings.Lines(out) {
		m := header.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		n, err := strconv.Atoi(m[2])
		if err == nil {
			by[n] = m[1]
		}
	}

	return by
}

func lastTouching(ctx context.Context, root string, revs []string, f *diff.File) (string, error) {
	paths := []string{"--"}
	for _, p := range []string{f.OldPath, f.NewPath} {
		if p != "" && !slices.Contains(paths, p) {
			paths = append(paths, p)
		}
	}

	out, err := run(ctx, root, "git", slices.Concat([]string{"log", "-n1", "--format=%H"}, revs, paths)...)

	return strings.TrimSpace(out), err
}

// JJ lists the revisions a revset names that change something, oldest first,
// and maps d's hunks onto them through each revision's own diff.
func JJ(ctx context.Context, root, revset string, d *diff.Diff) ([]plan.Commit, error) {
	out, err := run(ctx, root, "jj", "log", "--no-graph", "--no-pager", "--reversed", "-r", revset,
		"-T", `commit_id ++ "`+field+`" ++ if(empty, "empty") ++ "`+field+`" ++ description.first_line() ++ "\n"`)
	if err != nil {
		return nil, err
	}

	var (
		list  []plan.Commit
		diffs []*diff.Diff
	)

	for line := range strings.Lines(out) {
		const fields = 3

		parts := strings.SplitN(strings.TrimRight(line, "\n"), field, fields)
		if len(parts) != fields || parts[1] == "empty" {
			continue
		}

		patch, err := run(ctx, root, "jj", "diff", "--git", "--no-pager", "-r", parts[0])
		if err != nil {
			return nil, err
		}

		list = append(list, plan.Commit{ID: parts[0], Subject: parts[2]})
		diffs = append(diffs, diff.Parse([]byte(patch)))
	}

	for _, f := range d.Files {
		added := addedByHunk(&f)

		for _, h := range hunksOf(&f) {
			ref := order.Ref{Path: pathOf(&f), Hunk: h}

			lines, ok := added[h]
			if !ok {
				if i := lastIn(diffs, &f); i >= 0 {
					list[i].Hunks = append(list[i].Hunks, ref)
				}

				continue
			}

			for _, i := range wrote(diffs, f.NewPath, lines) {
				list[i].Hunks = append(list[i].Hunks, ref)
			}
		}
	}

	return inDiffOrder(list, d), nil
}

// wrote walks post-image lines back from the last revision to the first, answering
// every revision that added one; a line a revision only carried is followed to its parent.
func wrote(diffs []*diff.Diff, path string, lines []int) []int {
	want := slices.Clone(lines)

	var owners []int

	for i := len(diffs) - 1; i >= 0 && len(want) > 0; i-- {
		f := fileNamed(diffs[i], path)
		if f == nil {
			continue
		}

		var before []int

		for _, n := range want {
			old, added := back(f, n)
			if added {
				if !slices.Contains(owners, i) {
					owners = append(owners, i)
				}

				continue
			}

			before = append(before, old)
		}

		want = before

		if f.OldPath != "" {
			path = f.OldPath
		}
	}

	slices.Sort(owners)

	return owners
}

// back is where post-image line n of f sat before f's change, or that f's
// change added it.
func back(f *diff.File, n int) (int, bool) {
	shift := 0

	for _, h := range blocks(f) {
		start := h.firstNew
		if start == 0 {
			start = h.firstOld + shift
		}

		switch {
		case n < start:
			return n - shift, false
		case n < start+h.newCount:
			at := slices.IndexFunc(h.lines, func(l diff.Line) bool { return l.New == n })
			if at < 0 {
				return n - shift, false
			}

			if h.lines[at].Kind == diff.KindAdd {
				return 0, true
			}

			return h.lines[at].Old, false
		}

		shift += h.newCount - h.oldCount
	}

	return n - shift, false
}

type block struct {
	lines              []diff.Line
	firstOld, firstNew int
	oldCount, newCount int
}

func blocks(f *diff.File) []block {
	var out []block

	for _, l := range f.Lines {
		if len(out) == 0 || out[len(out)-1].lines[0].Hunk != l.Hunk {
			out = append(out, block{})
		}

		b := &out[len(out)-1]
		b.lines = append(b.lines, l)

		if l.Kind != diff.KindAdd {
			b.oldCount++

			if b.firstOld == 0 {
				b.firstOld = l.Old
			}
		}

		if l.Kind != diff.KindRemove {
			b.newCount++

			if b.firstNew == 0 {
				b.firstNew = l.New
			}
		}
	}

	return out
}

func lastIn(diffs []*diff.Diff, f *diff.File) int {
	for i := len(diffs) - 1; i >= 0; i-- {
		for _, p := range []string{f.OldPath, f.NewPath} {
			if p != "" && fileNamed(diffs[i], p) != nil {
				return i
			}
		}
	}

	return -1
}

func fileNamed(d *diff.Diff, path string) *diff.File {
	for i := range d.Files {
		if pathOf(&d.Files[i]) == path {
			return &d.Files[i]
		}
	}

	return nil
}

// addedByHunk is the post-image lines each hunk of f added.
func addedByHunk(f *diff.File) map[int][]int {
	out := map[int][]int{}

	for _, l := range f.Lines {
		if l.Kind == diff.KindAdd {
			out[l.Hunk] = append(out[l.Hunk], l.New)
		}
	}

	return out
}

func hunksOf(f *diff.File) []int {
	var out []int

	for _, l := range f.Lines {
		if !slices.Contains(out, l.Hunk) {
			out = append(out, l.Hunk)
		}
	}

	return out
}

// runs folds ascending line numbers into inclusive ranges.
func runs(lines []int) [][2]int {
	var out [][2]int

	for _, n := range lines {
		if len(out) > 0 && out[len(out)-1][1] == n-1 {
			out[len(out)-1][1] = n

			continue
		}

		out = append(out, [2]int{n, n})
	}

	return out
}

// inDiffOrder puts each commit's hunks in the order the diff has them.
func inDiffOrder(list []plan.Commit, d *diff.Diff) []plan.Commit {
	at := map[order.Ref]int{}
	for i, r := range seen.Hunks(d) {
		at[order.Ref{Path: r.Path, Hunk: r.Hunk}] = i
	}

	for i := range list {
		slices.SortFunc(list[i].Hunks, func(a, b order.Ref) int { return at[a] - at[b] })
	}

	return list
}

func pathOf(f *diff.File) string {
	if f.NewPath != "" {
		return f.NewPath
	}

	return f.OldPath
}

func run(ctx context.Context, root, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- commit ids, a revset, and diff paths
	cmd.Dir = root

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", bin, args[0], err, strings.TrimSpace(stderr.String()))
	}

	return string(out), nil
}
