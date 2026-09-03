// Package highlight lexes source into the few classes a diff is worth coloring
// by.
//
// It answers in byte ranges rather than in escape sequences, so the caller
// keeps every decision about color: a diff line already carries a background
// band and a word-level mark, and a highlighter that rendered its own colors
// would have to be unpicked to draw either.
package highlight

import (
	"path"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// Class is what a run of source is. It is a handful of cases rather than
// chroma's several hundred, because a diff is read for its shape: what is a
// name, what is fixed text, and what is not code at all.
type Class uint8

// The classes, which is every distinction the screen draws a color for.
const (
	// Plain is everything a grammar has nothing particular to say about.
	Plain Class = iota
	Comment
	Keyword
	String
	Number
	Name
	Function
	Type
	Punctuation
)

// Span is a run of one line under one class. Spans of a line are in order and
// do not overlap; a byte no span covers is Plain.
type Span struct {
	From  int
	To    int
	Class Class
}

// Lines lexes src as one unit and returns the spans of each line.
//
// The whole slice is lexed together because a line is not a program: a line
// inside a raw string or a block comment reads as code on its own, and the
// state that says otherwise is above it. What is still lost is the state above
// src itself, since a hunk is a fragment of a file, which is the limit this
// ships with.
//
// A path no grammar answers for yields nil, which draws as plain text.
func Lines(name string, src []string) [][]Span {
	// The grammar is picked by file name alone. Chroma also guesses from
	// content, which on a hunk guesses from a fragment, and a wrong grammar
	// colors a diff confidently and incorrectly.
	matched := lexers.Match(path.Base(name))
	if matched == nil {
		return nil
	}

	lexer := chroma.Coalesce(matched)

	joined := strings.Join(src, "\n")

	it, err := lexer.Tokenise(nil, joined)
	if err != nil {
		return nil
	}

	out := make([][]Span, len(src))
	at, line := 0, 0

	for _, tok := range it.Tokens() {
		class := classOf(tok.Type)

		for _, part := range strings.SplitAfter(tok.Value, "\n") {
			if part == "" {
				continue
			}

			text := strings.TrimSuffix(part, "\n")

			if line < len(out) && text != "" && class != Plain {
				out[line] = append(out[line], Span{From: at, To: at + len(text), Class: class})
			}

			if strings.HasSuffix(part, "\n") {
				at, line = 0, line+1

				continue
			}

			at += len(text)
		}
	}

	return out
}

func classOf(t chroma.TokenType) Class {
	// The literals share one category and differ by sub-category, so a number
	// tested against chroma.LiteralString answers true and colors as a string.
	switch {
	case t.InCategory(chroma.Comment):
		return Comment
	case t == chroma.KeywordType, t == chroma.NameClass, t == chroma.NameBuiltin:
		return Type
	case t.InSubCategory(chroma.NameFunction):
		return Function
	case t.InCategory(chroma.Keyword), t.InSubCategory(chroma.Operator):
		return Keyword
	case t.InSubCategory(chroma.LiteralNumber):
		return Number
	case t.InCategory(chroma.Literal):
		return String
	case t.InCategory(chroma.Name):
		return Name
	case t.InCategory(chroma.Punctuation):
		return Punctuation
	}

	return Plain
}
