package humanize_test

import (
	"testing"

	"github.com/kyleking/second-look/internal/humanize"
)

func TestFirstLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{"the opening line", "the first\nthe second", "the first"},
		{"past the blank ones", "\n\n  said it  ", "said it"},
		{
			"past a bot's own labeling",
			"_🎯 Functional Correctness_ | _🟡 Minor_ | _⚡ Quick win_\n\nThe error is dropped here.",
			"The error is dropped here.",
		},
		{"one emphasized sentence is what was said", "_this is wrong_", "_this is wrong_"},
		{"a table row is not a banner", "a | b", "a | b"},
		{"nothing at all", "  \n\n", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := humanize.FirstLine(tc.body); got != tc.want {
				t.Errorf("FirstLine() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPlural(t *testing.T) {
	t.Parallel()

	tests := []struct {
		got  string
		want string
	}{
		{humanize.Plural(1, "hunk"), "1 hunk"},
		{humanize.Plural(0, "hunk"), "0 hunks"},
		{humanize.Plural(2, "hunk"), "2 hunks"},
		{humanize.Plural(2, "search", "searches"), "2 searches"},
		{humanize.Plural(1, "search", "searches"), "1 search"},
	}

	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("= %q, want %q", tc.got, tc.want)
		}
	}
}
