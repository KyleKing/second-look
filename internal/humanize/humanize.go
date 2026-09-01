// Package humanize renders the values every queue in this tool prints: how
// stale something is, a string cut to fit a column, and a count with its noun.
//
// It is shared rather than copied because the review queue and the conversation
// queue sit next to each other on screen, and two implementations of "2h" would
// eventually disagree about what a day is.
package humanize

import (
	"strconv"
	"strings"
	"time"
)

// Plural writes a count with its noun. The plural is the noun with an s unless
// one is given, which is what a word like "search" needs.
func Plural(n int, one string, many ...string) string {
	if n == 1 {
		return "1 " + one
	}

	if len(many) > 0 {
		return strconv.Itoa(n) + " " + many[0]
	}

	return strconv.Itoa(n) + " " + one + "s"
}

// Ago is how stale something is, which is the field that decides what to look
// at when two rows are otherwise equal. A zero time renders empty rather than
// as a span since the epoch.
func Ago(then, now time.Time) string {
	if then.IsZero() {
		return ""
	}

	const (
		day  = 24 * time.Hour
		year = 365 * day
	)

	d := now.Sub(then)

	switch {
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < day:
		return strconv.Itoa(int(d.Hours())) + "h"
	case d < year:
		return strconv.Itoa(int(d/day)) + "d"
	}

	return strconv.Itoa(int(d/year)) + "y"
}

// Clip cuts a string to at most the given width, marking that it was cut.
func Clip(s string, to int) string {
	if to <= 0 {
		return ""
	}

	runes := []rune(s)
	if len(runes) <= to {
		return s
	}

	return string(runes[:to-1]) + "…"
}

// Width is how many columns a string occupies, counting runes rather than
// bytes so a multi-byte name does not push its column out.
func Width(s string) int { return len([]rune(s)) }

// FirstLine is the opening line of a comment body, which is as much of it as a
// queue row has space for. Markdown quotes and list markers are left alone; a
// body that opens with one is usually quoting the thing it answers, and cutting
// the marker would misreport what was said.
func FirstLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")

	for line := range strings.SplitSeq(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}

	return ""
}
