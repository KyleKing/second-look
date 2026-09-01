package get

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/kyleking/aragonite/forge/github"
	"github.com/kyleking/aragonite/vcs"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/seen"
	"github.com/kyleking/second-look/internal/threads"
)

// Reasons a review cannot be opened where the caller is standing.
var (
	ErrNoPRForBranch = errors.New("this branch has no pull request; name one, or check one out")
	ErrStaleReview   = errors.New("the prepared review was staged against an older head")
)

// Review is everything the review screen reads: the prepared review, the diff
// its comments anchor to, and where the review is written back.
type Review struct {
	Review *artifact.Review
	Diff   *diff.Diff
	// Threads is what is already open on the pull request, cached against the
	// head they anchor to. What is shown, never what is posted: an answer to
	// one is a comment staged like any other.
	Threads []threads.Thread
	// Read is which hunks have been read, and SeenPath is where that is written
	// back. It is keyed by hunk content rather than by head commit, so it
	// outlives a force-push on its own.
	Read     *seen.Set
	SeenPath string
	Path     string
	HeadSHA  string
	// Work is the checkout the code under review is readable in, empty when
	// there is none. OnHead says whether it is standing on the pull request
	// head, which is what decides whether a shell opened there would run
	// against the code the diff describes.
	Work   string
	OnHead bool
}

// Open reads a pull request into a review, creating the artifact and caching
// the diff only when they are missing.
//
// The working copy is neither moved nor required. Everything a review needs
// comes off the API: the diff its anchors are quoted from, the threads a reply
// answers, and the comment id a reply carries. What a tree adds is reading
// around the change and running it, so where the checkout is standing is
// reported rather than enforced, and moving it is a key in the screen.
func Open(ctx context.Context, t Target) (*Review, error) {
	pr, err := github.GetPR(ctx, t.Dir(), t.Remote(), t.Number)
	if err != nil {
		return nil, fmt.Errorf("reading pull request #%d: %w", t.Number, err)
	}

	standing, err := onHead(ctx, t, pr.HeadSHA)
	if err != nil {
		return nil, err
	}

	review, path, err := load(t, pr.HeadSHA)
	if err != nil {
		return nil, err
	}

	if review.HeadSHA != pr.HeadSHA {
		return nil, fmt.Errorf("%w: staged against %s, now at %s; run second-look get %d",
			ErrStaleReview, short(review.HeadSHA), short(pr.HeadSHA), t.Number)
	}

	patch, err := patchFor(ctx, t, review.HeadSHA)
	if err != nil {
		return nil, err
	}

	open, err := threadsFor(ctx, t, review.HeadSHA)
	if err != nil {
		return nil, err
	}

	seenPath := seen.Path(t.Store, t.Number)

	read, err := seen.Load(seenPath)
	if err != nil {
		return nil, fmt.Errorf("reading what has already been read: %w", err)
	}

	return &Review{
		Review: review, Diff: diff.Parse(patch), Threads: open,
		Read: read, SeenPath: seenPath, Path: path, HeadSHA: pr.HeadSHA,
		Work: t.Work, OnHead: standing,
	}, nil
}

// threadsFor reads the conversations open on the pull request, fetching them
// only when the cache has none. A review reached without a get would otherwise
// show an empty diff where a second pass has answers waiting.
func threadsFor(ctx context.Context, t Target, want string) ([]threads.Thread, error) {
	if _, err := os.Stat(artifact.ThreadsPath(t.Store, want)); err == nil {
		var open []threads.Thread
		if err := artifact.LoadThreads(t.Store, want, &open); err != nil {
			return nil, fmt.Errorf("reading the cached review threads: %w", err)
		}

		return open, nil
	}

	open, err := threads.Fetch(ctx, t.Dir(), t.Owner, t.Repo, t.Number)
	if err != nil {
		//nolint:wrapcheck // Fetch's own error already names the pull request
		return nil, err
	}

	if err := artifact.SaveThreads(t.Store, want, open); err != nil {
		return nil, fmt.Errorf("caching the review threads: %w", err)
	}

	return open, nil
}

// Current reports the pull request for the branch the checkout is on. Being on
// a branch with no pull request is the case worth naming, since it is what
// standing on the default branch looks like.
func Current(ctx context.Context, root string) (int, error) {
	if !vcs.IsRepo(root) {
		return 0, notARepo(root)
	}

	id, err := identify(ctx, root)
	if err != nil {
		return 0, err
	}

	branch, err := vcs.GetOperations(root).GetCurrentBranch(ctx, root)
	if err != nil {
		return 0, fmt.Errorf("reading the current branch: %w", err)
	}

	pr, err := github.GetPRForBranch(ctx, root, id.owner+"/"+id.name, branch, "")
	if err != nil {
		return 0, fmt.Errorf("%w: %s", ErrNoPRForBranch, branch)
	}

	return pr.Number, nil
}

// onHead reports whether the checkout is standing on the pull request head. A
// detached target has no checkout to stand anywhere.
func onHead(ctx context.Context, t Target, want string) (bool, error) {
	if t.Detached() {
		return false, nil
	}

	head, err := vcs.HeadSHA(ctx, t.Work)
	if err != nil {
		return false, fmt.Errorf("reading the checkout: %w", err)
	}

	return head == want, nil
}

// load reads the prepared review, writing a new one only when there is none.
// An existing review keeps the head it was staged against, so what its anchors
// were quoted from stays a fact rather than being restamped on every open.
func load(t Target, headSHA string) (*artifact.Review, string, error) {
	path := artifact.Path(t.Store, t.Number)

	review, err := artifact.Load(path)
	if err == nil {
		return review, path, nil
	}

	if !errors.Is(err, fs.ErrNotExist) {
		return nil, "", fmt.Errorf("reading the prepared review: %w", err)
	}

	review = &artifact.Review{
		Version: artifact.SchemaVersion, Host: Host,
		Owner: t.Owner, Repo: t.Repo, Number: t.Number, HeadSHA: headSHA,
	}

	if err := artifact.Save(path, review); err != nil {
		return nil, "", fmt.Errorf("writing the prepared review: %w", err)
	}

	return review, path, nil
}

// patchFor reads the diff the review's comments were anchored against, fetching
// it only when the cache has none. Callers reach it having already established
// that the sha is the pull request's current head, so a fetch answers for the
// same document the cache would have held.
func patchFor(ctx context.Context, t Target, want string) ([]byte, error) {
	if patch, err := artifact.LoadDiff(t.Store, want); err == nil {
		return patch, nil
	}

	patch, err := github.PRDiff(ctx, t.Dir(), t.Remote(), t.Number)
	if err != nil {
		return nil, fmt.Errorf("reading the diff: %w", err)
	}

	if err := artifact.SaveDiff(t.Store, want, patch); err != nil {
		return nil, fmt.Errorf("caching the diff: %w", err)
	}

	return patch, nil
}
