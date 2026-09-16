package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/kyleking/second-look/internal/diag"
)

// Reasons a line cannot be asked about.
var (
	ErrNoServer = errors.New("no language server is configured for this file")
	ErrNoLine   = errors.New("that line is not in the file")
)

// hoverDeadline bounds one line's worth of questions. A warm server answers each
// in about a millisecond, so anything past this is a server that is still
// loading rather than one that is thinking.
const hoverDeadline = 10 * time.Second

// probes is the most symbols one line is asked about. A line with more names
// than this on it is not a line whose types anyone is reading off a screen.
const probes = 24

// Hover is what every name on one line resolves to.
//
// A review screen has no column cursor: a row is a line, and the question a
// reader has is which of the names on it is what they think it is. So the line
// is asked about at each name rather than at a point, which is the same answer
// an editor gives one hover at a time.
func (s *Session) Hover(ctx context.Context, doc Doc, line int) ([]diag.Symbol, error) {
	srv, ok := pick(s.servers, doc.Path)
	if !ok {
		return nil, fmt.Errorf("%s: %w", doc.Path, ErrNoServer)
	}

	ctx, cancel := context.WithTimeout(ctx, hoverDeadline)
	defer cancel()

	c, err := s.serverFor(ctx, srv)
	if err != nil {
		return nil, err
	}

	uri := s.uri(doc.Path)
	if err := s.open(c, srv, doc.Path, uri, doc.Text); err != nil {
		return nil, fmt.Errorf("%s: %w", srv.Name, err)
	}

	lines := strings.Split(doc.Text, "\n")
	if line < 1 || line > len(lines) {
		return nil, fmt.Errorf("%s line %d: %w", doc.Path, line, ErrNoLine)
	}

	return askLine(ctx, c, uri, lines[line-1], line-1)
}

// askLine asks about each name on one line, keeping one of each answer. A name
// used twice on a line resolves to the same text both times, and a server names
// the symbol in what it answers, so this collapses the repetition without
// collapsing two different names that happen to share a type.
func askLine(ctx context.Context, c *client, uri, text string, row int) ([]diag.Symbol, error) {
	var (
		out  []diag.Symbol
		seen = map[string]bool{}
	)

	for i, at := range names(text) {
		if i >= probes {
			break
		}

		raw, err := c.call(ctx, "textDocument/hover", map[string]any{
			documentKey: map[string]any{uriKey: uri},
			"position":  position{Line: row, Character: unitsTo(text, at)},
		})
		if err != nil {
			return out, fmt.Errorf("asking about %s: %w", uri, err)
		}

		name, says := readHover(raw, text, at)
		if says == "" || seen[says] {
			continue
		}

		seen[says] = true
		out = append(out, diag.Symbol{Name: name, Text: says})
	}

	return out, nil
}

// readHover reads one hover response into the line it belongs on. A server
// answers in markdown, and what is wanted is the signature inside its fence
// rather than the prose under it.
func readHover(raw json.RawMessage, text string, at int) (string, string) {
	var res struct {
		Contents struct {
			Value string `json:"value"`
		} `json:"contents"`
		Range lspRange `json:"range"`
	}

	if err := json.Unmarshal(raw, &res); err != nil || res.Contents.Value == "" {
		return "", ""
	}

	name := word(text, at)

	if r := res.Range; r.Start.Line == r.End.Line && r.Start.Character < r.End.Character {
		from, to := bytesTo(text, r.Start.Character), bytesTo(text, r.End.Character)
		if to <= len(text) {
			name = text[from:to]
		}
	}

	return name, signature(res.Contents.Value)
}

// signature is the declaration a hover leads with. Everything under the first
// fence is the doc comment, which a reader asking what a name is does not need
// a screen of.
func signature(value string) string {
	var (
		out    []string
		inside bool
	)

	for _, line := range strings.Split(value, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if inside {
				break
			}

			inside = true

			continue
		}

		if inside {
			out = append(out, strings.TrimSpace(line))
		}
	}

	if len(out) == 0 {
		// A server answering in plain text has no fence to read, so the first
		// line of what it said stands for it.
		return strings.TrimSpace(strings.Split(value, "\n")[0])
	}

	return strings.Join(out, " ")
}

// names is the offset of each identifier on a line, which is where a hover is
// asked from.
func names(text string) []int {
	var (
		out  []int
		open bool
	)

	for i, r := range text {
		switch {
		case !identRune(r):
			open = false
		case !open:
			open = true

			out = append(out, i)
		}
	}

	return out
}

// word is the identifier starting at an offset, which is what a server that
// answered without a range is taken to have answered about.
func word(text string, at int) string {
	if at >= len(text) {
		return ""
	}

	end := at
	for end < len(text) && identRune(rune(text[end])) {
		end++
	}

	return text[at:end]
}

// The protocol counts a character in UTF-16 code units and Go counts bytes, so
// every position crossing the wire is converted. A line of ASCII is the same
// number either way, which is why getting this wrong shows up only on the line
// that has an accent or an emoji in it.
func unitsTo(text string, at int) int {
	units := 0

	for i, r := range text {
		if i >= at {
			break
		}

		units += utf16.RuneLen(r)
	}

	return units
}

// bytesTo is the reverse: where in the string the given code unit starts. A
// unit past the end of the line answers with the end of it.
func bytesTo(text string, units int) int {
	left := units

	for i, r := range text {
		if left <= 0 {
			return i
		}

		left -= utf16.RuneLen(r)
	}

	return len(text)
}

// identRune is what counts as part of a name, which is every language's letters
// and digits plus the two punctuation marks some of them allow inside one.
func identRune(r rune) bool {
	return r == '_' || r == '$' ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
		r > 0x7f
}
