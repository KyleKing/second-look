package get

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/kyleking/aragonite/forge/github"
	"github.com/kyleking/aragonite/vcs"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
)

// Reasons a review cannot be opened where the caller is standing.
var (
	ErrNoPRForBranch = errors.New("this branch has no pull request; name one, or check one out")
	ErrNotOnHead     = errors.New("the checkout is not on the pull request head")
	ErrStaleReview   = errors.New("the prepared review was staged against an older head")
)

// Review is everything the review screen reads: the prepared review, the diff
// its comments anchor to, and where the review is written back.
type Review struct {
	Review  *artifact.Review
	Diff    *diff.Diff
	Path    string
	HeadSHA string
}

// Open reads a pull request into a review, creating the artifact and caching
// the diff only when they are missing.
//
// It never moves the working copy. Checking out a pull request is `gh pr
// checkout` or `second-look get`, and doing it as a side effect of opening a
// screen would move a tree the reader did not ask to move.
func Open(ctx context.Context, root string, number int) (*Review, error) {
	if !vcs.IsRepo(root) {
		return nil, fmt.Errorf("%s: %w", root, ErrNotARepo)
	}

	id, err := identify(ctx, root)
	if err != nil {
		return nil, err
	}

	pr, err := github.GetPR(ctx, root, number)
	if err != nil {
		return nil, fmt.Errorf("reading pull request #%d: %w", number, err)
	}

	if err := requireHead(ctx, root, pr.HeadSHA, number); err != nil {
		return nil, err
	}

	review, path, err := load(root, id, pr.Number, pr.HeadSHA)
	if err != nil {
		return nil, err
	}

	if review.HeadSHA != pr.HeadSHA {
		return nil, fmt.Errorf("%w: staged against %s, now at %s; run second-look get %d",
			ErrStaleReview, short(review.HeadSHA), short(pr.HeadSHA), number)
	}

	patch, err := patchFor(ctx, root, number, review.HeadSHA)
	if err != nil {
		return nil, err
	}

	return &Review{Review: review, Diff: diff.Parse(patch), Path: path, HeadSHA: pr.HeadSHA}, nil
}

// Current reports the pull request for the branch the checkout is on. Being on
// a branch with no pull request is the case worth naming, since it is what
// standing on the default branch looks like.
func Current(ctx context.Context, root string) (int, error) {
	if !vcs.IsRepo(root) {
		return 0, fmt.Errorf("%s: %w", root, ErrNotARepo)
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

func requireHead(ctx context.Context, root, want string, number int) error {
	head, err := vcs.HeadSHA(ctx, root)
	if err != nil {
		return fmt.Errorf("reading the checkout: %w", err)
	}

	if head != want {
		return fmt.Errorf("%w: at %s, #%d is at %s; run gh pr checkout %d",
			ErrNotOnHead, short(head), number, short(want), number)
	}

	return nil
}

// load reads the prepared review, writing a new one only when there is none.
// An existing review keeps the head it was staged against, so what its anchors
// were quoted from stays a fact rather than being restamped on every open.
func load(root string, id repo, number int, headSHA string) (*artifact.Review, string, error) {
	path := artifact.Path(root, number)

	review, err := artifact.Load(path)
	if err == nil {
		return review, path, nil
	}

	if !errors.Is(err, fs.ErrNotExist) {
		return nil, "", fmt.Errorf("reading the prepared review: %w", err)
	}

	review = &artifact.Review{
		Version: artifact.SchemaVersion, Host: "github.com",
		Owner: id.owner, Repo: id.name, Number: number, HeadSHA: headSHA,
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
func patchFor(ctx context.Context, root string, number int, want string) ([]byte, error) {
	if patch, err := artifact.LoadDiff(root, want); err == nil {
		return patch, nil
	}

	patch, err := github.PRDiff(ctx, root, number)
	if err != nil {
		return nil, fmt.Errorf("reading the diff: %w", err)
	}

	if err := artifact.SaveDiff(root, want, patch); err != nil {
		return nil, fmt.Errorf("caching the diff: %w", err)
	}

	return patch, nil
}
