// Package react leaves an emoji on a comment, and takes it back.
//
// It is a GraphQL mutation because REST reacts to an issue comment and to
// nothing else that appears in a review: a review comment's endpoint is
// undocumented and a review body has none at all. So every surface here is
// addressed by node id.
package react

import (
	"context"
	"errors"
	"fmt"

	"github.com/kyleking/second-look/internal/ghrun"
)

// ErrNoSubject reports a comment carrying no node id, which is what a review
// staged before reactions were fetched holds. Preparing it again reads them.
var ErrNoSubject = errors.New("this comment carries no id to react to; run second-look get to read it again")

// Emoji is one of the eight reactions GitHub takes, the key that leaves it, and
// what it looks like on screen.
type Emoji struct {
	// Key is the second key of the chord, the first letter of the name where
	// that letter is free.
	Key string
	// Content is GitHub's own name, which is what the mutation takes.
	Content string
	Glyph   string
	Name    string
}

// Emojis are the eight, in the order GitHub's own picker shows them.
func Emojis() []Emoji {
	return []Emoji{
		{Key: "t", Content: "THUMBS_UP", Glyph: "👍", Name: "up"},
		{Key: "d", Content: "THUMBS_DOWN", Glyph: "👎", Name: "down"},
		{Key: "l", Content: "LAUGH", Glyph: "😄", Name: "laugh"},
		{Key: "p", Content: "HOORAY", Glyph: "🎉", Name: "hooray"},
		{Key: "c", Content: "CONFUSED", Glyph: "😕", Name: "confused"},
		{Key: "h", Content: "HEART", Glyph: "❤️", Name: "heart"},
		{Key: "r", Content: "ROCKET", Glyph: "🚀", Name: "rocket"},
		{Key: "e", Content: "EYES", Glyph: "👀", Name: "eyes"},
	}
}

// ByKey is the emoji a key names, and false for any other key.
func ByKey(key string) (Emoji, bool) {
	for _, e := range Emojis() {
		if e.Key == key {
			return e, true
		}
	}

	return Emoji{}, false
}

// ErrNoSuchEmoji reports a content name none of the eight carries.
var ErrNoSuchEmoji = errors.New("no reaction by that name")

// ByContent is the emoji GitHub's own name refers to.
func ByContent(content string) (Emoji, bool) {
	for _, e := range Emojis() {
		if e.Content == content {
			return e, true
		}
	}

	return Emoji{}, false
}

// Glyph is how a content name is drawn, or the name itself for one GitHub added
// since this was written.
func Glyph(content string) string {
	for _, e := range Emojis() {
		if e.Content == content {
			return e.Glyph
		}
	}

	return content
}

const add = `mutation($id:ID!,$content:ReactionContent!){
  addReaction(input:{subjectId:$id,content:$content}){reaction{content}}
}`

const remove = `mutation($id:ID!,$content:ReactionContent!){
  removeReaction(input:{subjectId:$id,content:$content}){reaction{content}}
}`

// Set leaves the emoji on the comment, or takes it back where mine says it is
// already there. GitHub refuses a duplicate, so reacting twice has to mean
// undoing rather than repeating.
func Set(ctx context.Context, r ghrun.Runner, root, nodeID string, e Emoji, mine bool) error {
	if nodeID == "" {
		return ErrNoSubject
	}

	query := add
	if mine {
		query = remove
	}

	err := r.Run(ctx, root, "api", "graphql", "-F", "id="+nodeID, "-F", "content="+e.Content,
		"-f", "query="+query)
	if err != nil {
		return fmt.Errorf("reacting %s: %w", e.Glyph, err)
	}

	return nil
}
