package plan

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/kyleking/second-look/internal/order"
)

// One commit is the diff again, and past fifteen the chapters outnumber what they cover.
const (
	fewestCommits = 2
	mostCommits   = 15
)

// noise matches subjects that answer a review or a tool rather than describe the change.
var noise = regexp.MustCompile(`(?i)^(fixup!|squash!|amend!)|^wip\b` +
	`|\baddress(ed|es|ing)? (the )?(review|comments?|feedback)\b` +
	`|\bfix(es|ed)? (the )?lint\b|^(apply )?review (feedback|comments)`)

// known drops the refs a commit names that the diff does not carry.
func known(commits []Commit, hunks []order.Hunk) ([]Commit, []string) {
	in := make(map[order.Ref]bool, len(hunks))
	for _, h := range hunks {
		in[h.Ref] = true
	}

	var problems []string

	out := make([]Commit, 0, len(commits))

	for _, c := range commits {
		kept := c.Hunks[:0:0]

		for _, r := range c.Hunks {
			if in[r] {
				kept = append(kept, r)

				continue
			}

			problems = append(problems, fmt.Sprintf("commit %s: %s is not a hunk of this diff", short(c.ID), name(r)))
		}

		c.Hunks = kept
		out = append(out, c)
	}

	return out, problems
}

// messy says why a history should not be read commit by commit, or nothing
// when it can be.
func messy(commits []Commit, hunks []order.Hunk) string {
	n := len(commits)

	switch {
	case n == 0:
		return "no commits to read by"
	case n < fewestCommits:
		return "one commit"
	case n > mostCommits:
		return fmt.Sprintf("%d commits, past %d", n, mostCommits)
	}

	owners := map[order.Ref]int{}

	for _, c := range commits {
		for _, r := range c.Hunks {
			owners[r]++
		}
	}

	if orphans := countIf(hunks, func(h order.Hunk) bool { return owners[h.Ref] == 0 }); orphans > 0 {
		return fmt.Sprintf("%d hunk(s) no commit in the range wrote", orphans)
	}

	if noisy := countIf(commits, func(c Commit) bool { return noise.MatchString(c.Subject) }); noisy*5 > n {
		return fmt.Sprintf("%d of %d subjects are fixups or review rounds", noisy, n)
	}

	alone := countIf(commits, func(c Commit) bool {
		return slices.ContainsFunc(c.Hunks, func(r order.Ref) bool { return owners[r] == 1 })
	})

	if alone*4 < n*3 {
		return fmt.Sprintf("%d of %d commits own no hunk another did not also touch", n-alone, n)
	}

	return ""
}

// byCommit is one chapter per commit. A hunk two commits wrote is read with the
// later, which wrote the text the diff shows.
func byCommit(commits []Commit, hunks []order.Hunk) []Chapter {
	owner := map[order.Ref]int{}

	for i, c := range commits {
		for _, r := range c.Hunks {
			owner[r] = i
		}
	}

	out := make([]Chapter, len(commits))
	for i, c := range commits {
		subject := c.Subject
		if subject == "" {
			subject = short(c.ID)
		}

		out[i] = Chapter{Name: subject, Mode: ModeRead, Source: SourceCommit, Why: "commit " + short(c.ID)}
	}

	for _, h := range hunks {
		at := owner[h.Ref]
		out[at].Hunks = append(out[at].Hunks, h.Ref)
	}

	return slices.DeleteFunc(out, func(c Chapter) bool { return len(c.Hunks) == 0 })
}

func countIf[T any](items []T, keep func(T) bool) int {
	n := 0

	for _, it := range items {
		if keep(it) {
			n++
		}
	}

	return n
}

func short(id string) string {
	const width = 7

	if len(id) > width {
		return id[:width]
	}

	return id
}
