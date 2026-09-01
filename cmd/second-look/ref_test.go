package main_test

import (
	"testing"

	main "github.com/kyleking/second-look/cmd/second-look"
)

// TestParseRef covers the three shapes a pull request is named by. The URL is
// the one worth pinning: it is what a browser and a comment both hand over, and
// what a browser appends to it is somebody else's fragment rather than part of
// the number.
func TestParseRef(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   string
		want string
	}{
		{in: "42", want: "#42"},
		{in: "#42", want: "#42"},
		{in: " 42 ", want: "#42"},
		{in: "acme/app#7", want: "acme/app#7"},
		{in: "https://github.com/acme/app/pull/7", want: "acme/app#7"},
		{in: "https://github.com/acme/app/pull/7/files", want: "acme/app#7"},
		{
			in:   "https://github.com/acme/app/pull/7#discussion_r123",
			want: "acme/app#7",
		},
		{in: "https://github.example.com/acme/app/pull/7?w=1", want: "acme/app#7"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			got, err := main.RefString(tc.in)
			if err != nil {
				t.Fatalf("%q: %v", tc.in, err)
			}

			if got != tc.want {
				t.Errorf("%q parsed as %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseRefRefusesWhatIsNotOne(t *testing.T) {
	t.Parallel()

	for _, in := range []string{
		"", "0", "-3", "inbox", "acme#7", "acme/app", "acme/app#x",
		"acme/team/app#7", "https://github.com/acme/app/issues/7",
		"https://github.com/acme/app/pull/x",
	} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()

			if got, err := main.RefString(in); err == nil {
				t.Errorf("%q parsed as %s, want a refusal", in, got)
			}
		})
	}
}
