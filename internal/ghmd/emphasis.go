package ghmd

import "strings"

// emphasis takes the inline emphasis markers out of a line.
//
// A terminal row is drawn with one face, so the emphasis itself cannot be kept,
// and `_🩺 Stability_ | _🟠 Major_` spelled out is worse to read than the words
// alone. Backticks stay: they mark code, a terminal has no other way to say so,
// and a reader already knows what they mean.
//
// The delimiter has to close on the same line, and an underscore has to sit
// against a word boundary on both ends, so snake_case_names outside a fence are
// left as they were.
func emphasis(line string) string {
	var b strings.Builder

	code := false
	open := map[byte]int{}

	for i := 0; i < len(line); {
		if line[i] == '`' {
			code = !code
			b.WriteByte(line[i])
			i++

			continue
		}

		if !code {
			if run := delimiter(line, i, open); run > 0 {
				i += run

				continue
			}
		}

		b.WriteByte(line[i])
		i++
	}

	return b.String()
}

// delimiter is how many bytes of emphasis marker start at i, and zero where
// what is there is a character somebody wrote. It records what it opened, since
// the same run reads as an opener or a closer depending on what is already
// open: an underscore against a word closes emphasis and does not start it,
// which is what leaves snake_case alone.
func delimiter(line string, i int, open map[byte]int) int {
	mark := line[i]
	if mark != '*' && mark != '_' {
		return 0
	}

	run := 0
	for i+run < len(line) && line[i+run] == mark {
		run++
	}

	if open[mark] == run && !spacey(line, i-1) {
		delete(open, mark)

		return run
	}

	if open[mark] != 0 || spacey(line, i+run) || !strings.Contains(line[i+run:], strings.Repeat(string(mark), run)) {
		return 0
	}

	if mark == '_' && wordish(line, i-1) {
		return 0
	}

	open[mark] = run

	return run
}

// spacey reports whether the byte at an index is a space, treating the ends of
// the line as spaces. A marker with a space inside it is punctuation.
func spacey(line string, at int) bool {
	if at < 0 || at >= len(line) {
		return true
	}

	return line[at] == ' ' || line[at] == '\t'
}

// wordish reports whether the byte at an index is part of a word, treating the
// ends of the line as boundaries.
func wordish(line string, at int) bool {
	if at < 0 || at >= len(line) {
		return false
	}

	c := line[at]

	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
