package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/get"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/lease"
	"github.com/kyleking/second-look/internal/prepared"
	"github.com/kyleking/second-look/internal/tui"
)

// releaseClaim drops the claim the sitting holds on repo when focus leaves it.
// The release itself is unconditional, the warning it can carry is not: a
// repository whose staged reviews still hold drafts says so, since the lease
// was the marker that the work was in hand.
func releaseClaim(repo string) tea.Cmd {
	return func() tea.Msg {
		h := lease.Ours(get.Host, repo)
		if h == nil {
			return nil
		}

		h.Release()

		word := "released " + filepath.Base(h.Record.Path)
		if n := blockedReviews(repo); n > 0 {
			return tui.StatusMsg{
				Text:   fmt.Sprintf("%s; %s on %s still blocked", word, humanize.Plural(n, "review"), repo),
				Failed: true,
			}
		}

		return tui.StatusMsg{Text: word}
	}
}

// releaseClaims drops every claim the sitting holds, which is what leaving the
// queue owes, and warns about each repository that still holds drafts.
func releaseClaims(stdout io.Writer) error {
	released, err := lease.ReleaseAll()
	if err != nil {
		return fmt.Errorf("releasing the sitting's checkouts: %w", err)
	}

	if len(released) == 0 {
		return nil
	}

	var b strings.Builder

	for i := range released {
		fmt.Fprintf(&b, "released %s", released[i].Path)
		if n := blockedReviews(released[i].Repo); n > 0 {
			fmt.Fprintf(&b, "; %s on %s still blocked", humanize.Plural(n, "review"), released[i].Repo)
		}

		b.WriteByte('\n')
	}

	return write(stdout, b.String())
}

// blockedReviews counts staged reviews on repo that would refuse to post,
// which is what "still blocked" means. A store that cannot be read counts
// nothing rather than failing the release that already happened.
func blockedReviews(repo string) int {
	home, err := artifact.StateHome()
	if err != nil {
		return 0
	}

	rows, err := prepared.All(home)
	if err != nil {
		return 0
	}

	var n int

	for i := range rows {
		if rows[i].Repository == repo && rows[i].Blocked() {
			n++
		}
	}

	return n
}
