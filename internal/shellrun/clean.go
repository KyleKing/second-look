package shellrun

import (
	"regexp"
	"strings"
)

// maxTranscript is how much of a session is kept. The tail is what matters:
// a long build ends in the failure worth quoting, and a note nobody can read
// past is a note nobody reads.
const maxTranscript = 4000

// script writes a banner line at each end and echoes the shell's own exit,
// none of which is evidence of anything.
var (
	escapes = regexp.MustCompile(
		`\x1b\[[0-9;?]*[a-zA-Z]` + // CSI
			`|\x1b\][^\a\x1b]*(\a|\x1b\\)` + // OSC, either terminator
			`|\x1b[()][A-Za-z0-9]` + // charset selection
			`|[\x00-\x08\x0b\x0c\x0e-\x1f]`,
	) // stray control bytes
	banner = regexp.MustCompile(`(?m)^Script (started|done)[^\n]*\n?`)
)

// Clean turns a raw typescript into something worth reading in a note: no
// escape sequences, no carriage returns, no script(1) banner, and no trailing
// blank lines.
func Clean(raw []byte) string {
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	s = escapes.ReplaceAllString(s, "")
	s = banner.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r", "")

	lines := make([]string, 0, strings.Count(s, "\n")+1)
	for _, l := range strings.Split(s, "\n") {
		lines = append(lines, strings.TrimRight(l, " \t"))
	}

	s = strings.Trim(strings.Join(lines, "\n"), "\n")

	if len(s) > maxTranscript {
		// Cut at a line boundary so the note does not open mid-word.
		s = s[len(s)-maxTranscript:]
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		}

		s = "…\n" + s
	}

	return s
}
