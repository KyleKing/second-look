package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kyleking/second-look/internal/checkouts"
	"github.com/kyleking/second-look/internal/conversations"
)

// errNoneChosen is every offered checkout turned down, which is a decision
// rather than a failure to find one.
var errNoneChosen = errors.New("no checkout was chosen")

// answer opens the review that stages a reply to a conversation, in whichever
// checkout of its repository this laptop has and in none when it has none.
//
// Reading the queue needs no checkout, and neither does answering: the reply
// carries the comment id it addresses, so it posts without a tree. A checkout is
// still worth standing in when there is one, because that is where the diff
// cache, the read marks, and an agent already look, so it is found when the
// reply is asked for rather than being a precondition for the queue.
func answer(
	ctx context.Context, c *conversations.Conversation, here string, stdin io.Reader, stdout io.Writer,
) error {
	owner, name, _ := strings.Cut(c.Repository, "/")
	at := ref{owner: owner, repo: name, number: c.Number}

	if here != "" && strings.EqualFold(here, c.Repository) {
		return openRef(ctx, at, stdin, stdout)
	}

	root, err := pick(ctx, c, stdin, stdout)
	if err != nil {
		return fmt.Errorf("staging a reply to %s: %w", c.Where(), err)
	}

	if root == "" {
		if err := write(stdout, "no checkout of "+c.Repository+
			" here; reviewing "+c.Where()+" from the API\n"); err != nil {
			return err
		}

		return openRef(ctx, at, stdin, stdout)
	}

	if err := write(stdout, "opening "+c.Where()+" in "+root+"\n"); err != nil {
		return err
	}

	// Everything downstream reads ".": the prepared review, the diff cache, the
	// anchor guard, and the shell ! hands the terminal to. Moving the process is
	// one line where plumbing a root through all of them would eventually miss
	// one and write a review's state into the wrong repository.
	if err := os.Chdir(root); err != nil {
		return fmt.Errorf("moving to %s: %w", root, err)
	}

	return openRef(ctx, at, stdin, stdout)
}

// pick is the checkout the review happens in, or empty for a repository this
// laptop has no clone of, which is reviewed from the API instead.
//
// One candidate is used without asking, since opening a review there moves
// nothing on its own, and several means the ranking is a guess that the person
// at the keyboard settles.
func pick(
	ctx context.Context, c *conversations.Conversation, stdin io.Reader, stdout io.Writer,
) (string, error) {
	found, err := checkouts.Find(ctx, checkouts.Dashboard(), c.Repository, c.HeadRef)
	if err != nil {
		// The dashboard is how a clone is found rather than how a review
		// happens, so one that cannot be run costs the checkout and not the
		// answer.
		if err := write(stdout, err.Error()+"\n"); err != nil {
			return "", err
		}

		return "", nil
	}

	if len(found) == 0 {
		return "", nil
	}

	if len(found) == 1 {
		return found[0].Path, nil
	}

	ask := confirm(stdin, stdout)

	for i := range found {
		yes, err := ask("Review in " + describe(&found[i]) + "?")
		if err != nil {
			return "", err
		}

		if yes {
			return found[i].Path, nil
		}
	}

	return "", errNoneChosen
}

// describe is what a candidate costs to use, which is what the answer turns on.
func describe(c *checkouts.Checkout) string {
	var b strings.Builder

	b.WriteString(c.Path)
	b.WriteString(" (")

	switch {
	case c.Dirty:
		b.WriteString("uncommitted changes, on " + c.Branch)
	case c.Branch != "":
		b.WriteString("clean, on " + c.Branch)
	default:
		b.WriteString("clean")
	}

	if c.Worktree {
		b.WriteString(", a worktree")
	}

	b.WriteString(")")

	return b.String()
}

// errNotAThread is what r answers on the two surfaces GitHub threads no reply
// off.
var errNotAThread = errors.New("only an inline thread takes a threaded reply; " +
	"answer a comment or a review body on GitHub, or resolve it with R")
