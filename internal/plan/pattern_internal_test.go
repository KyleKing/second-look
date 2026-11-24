package plan

import (
	"regexp"
	"strings"
	"testing"
)

// normalizeByRegexp is normalize as three regular expressions, which is how it
// was written before it became one pass. The scan has to agree with it
// byte for byte, because a pattern's shape is keyed on the normalized line.
func normalizeByRegexp(text, own string) string {
	number := regexp.MustCompile(`\d+`)
	spaces := regexp.MustCompile(`\s+`)

	text = spaces.ReplaceAllString(strings.TrimSpace(text), " ")

	if len(own) >= 3 {
		text = identifier.ReplaceAllStringFunc(text, func(id string) string {
			if strings.Contains(strings.ToLower(id), own) {
				return "$"
			}

			return id
		})
	}

	return number.ReplaceAllString(text, "#")
}

func TestNormalize_MatchesRegexp(t *testing.T) {
	t.Parallel()

	tests := []struct{ text, own string }{
		{"", "user"},
		{"   ", "user"},
		{"\treturn  userID + 42\n", "user"},
		{"var UserCache9 = newUser(10, 20)", "user"},
		{"x1y22z333 9abc _u_ser", "abc"},
		{"a\vb\f\fc\r\nd", "ab"},
		{"naïve := café2 + 3", "caf"},
		{"__init__ 007 __", "init"},
		{"\xff\xfe12 bad utf8", "bad"},
		{"MUSTRENAME mustrename_2", "mustrename"},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			t.Parallel()

			if got, want := normalize(tt.text, tt.own), normalizeByRegexp(tt.text, tt.own); got != want {
				t.Errorf("normalize(%q, %q) = %q, want %q", tt.text, tt.own, got, want)
			}
		})
	}
}

func FuzzNormalize(f *testing.F) {
	f.Add("\treturn  userID + 42\n", "user")
	f.Add("x1y22z333 9abc", "abc")

	f.Fuzz(func(t *testing.T, text, own string) {
		own = strings.ToLower(own)
		if got, want := normalize(text, own), normalizeByRegexp(text, own); got != want {
			t.Errorf("normalize(%q, %q) = %q, want %q", text, own, got, want)
		}
	})
}

func BenchmarkNormalize(b *testing.B) {
	line := "\tresult := computeUser(item, 42, cfg) // a line worth marking"

	for b.Loop() {
		_ = normalize(line, "user")
	}
}
