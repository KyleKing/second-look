package get

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/kyleking/aragonite/vcs"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/prepared"
)

// Reasons a bare number resolves to no one review.
var (
	ErrAmbiguous     = errors.New("name the repository: owner/name#number")
	ErrNothingStaged = errors.New("nothing is staged for it, and this is not a checkout")
)

// Host is the only forge this reviews against. It is a constant rather than a
// field because the schema already records it and nothing reads a second value.
const Host = "github.com"

// Target is a pull request and where its local state is kept.
//
// Work is the checkout whose tree holds the code under review, and it is empty
// for a pull request no clone on this machine covers. Store is the directory
// .second-look sits under: the checkout when there is one, and the user state
// directory when there is not, so a review can be prepared, read, answered, and
// posted with no working copy at all.
type Target struct {
	Owner  string
	Repo   string
	Number int
	Work   string
	Store  string
}

// Detached reports a target with no working copy, which is what the shell key
// and the checkout key answer differently.
func (t Target) Detached() bool { return t.Work == "" }

// RepoID is the owner/name the review is filed against.
func (t Target) RepoID() string { return t.Owner + "/" + t.Repo }

// Remote is what gh is told the repository is. Inside a checkout it is nothing:
// gh reads the remotes itself and picks a fork's upstream correctly, where a
// name derived from one remote would not.
func (t Target) Remote() string {
	if t.Work != "" {
		return ""
	}

	return t.RepoID()
}

// Dir is where gh runs. A detached target runs it in the working directory,
// which names no repository, so the name Remote carries is what resolves it.
func (t Target) Dir() string {
	if t.Work == "" {
		return "."
	}

	return t.Work
}

// Here is the target for a pull request of the repository the checkout at root
// belongs to. Its state goes in the store like every other review, and an
// artifact tree left in the checkout is moved there on the way.
func Here(ctx context.Context, root string, number int) (Target, error) {
	if !vcs.IsRepo(root) {
		return Target{}, notARepo(root)
	}

	id, err := identify(ctx, root)
	if err != nil {
		return Target{}, err
	}

	t, err := Away(id.owner, id.name, number)
	if err != nil {
		return Target{}, err
	}

	if err := artifact.Adopt(root, t.Store); err != nil {
		return Target{}, fmt.Errorf("moving %s into the store: %w", root, err)
	}

	t.Work = root

	return t, nil
}

// Away is the target for a pull request whose repository is not checked out
// here. Its state goes under the user state directory, since there is no
// repository to put it in and the review still has to survive being closed.
func Away(owner, repo string, number int) (Target, error) {
	store, err := artifact.StateRoot(Host, owner, repo)
	if err != nil {
		//nolint:wrapcheck // StateRoot's own error already names the offending part
		return Target{}, err
	}

	return Target{Owner: owner, Repo: repo, Number: number, Store: store}, nil
}

// Staged is the target for a review already staged in the directory at root,
// reported false when there is none there.
//
// It is what a bare number means to the commands that read an existing review:
// an agent, or a person, standing in a directory that holds a prepared review
// reads that file, whether or not the directory is a checkout of anything. A
// review that will not parse still resolves, so the failure reported is the
// parse rather than the surroundings.
func Staged(ctx context.Context, root string, number int) (Target, bool) {
	t, ok := stagedHere(root, number)
	if !ok {
		return Target{}, false
	}

	// The directory is the working copy only when it is a checkout of the
	// repository the review names. gh reads the repository off the remotes of
	// wherever it runs, so standing in an unrelated clone would address that one.
	here, err := Here(ctx, root, number)
	if err != nil || (t.Owner != "" && !strings.EqualFold(here.RepoID(), t.RepoID())) {
		return t, true
	}

	t.Work = root
	if t.Owner == "" {
		t.Owner, t.Repo = here.Owner, here.Repo
	}

	return t, true
}

// stagedHere reads the review staged in a directory and answers for the store
// it belongs to, moving an artifact tree left there on the way. A review that
// will not parse names no repository, so it is read where it lies.
func stagedHere(root string, number int) (Target, bool) {
	if _, err := os.Stat(artifact.Path(root, number)); err != nil {
		return Target{}, false
	}

	r, err := artifact.Load(artifact.Path(root, number))
	if err != nil {
		return Target{Number: number, Store: root}, true
	}

	t, err := Away(r.Owner, r.Repo, number)
	if err != nil {
		return Target{Number: number, Store: root}, true
	}

	if err := artifact.Adopt(root, t.Store); err != nil {
		return Target{Number: number, Owner: r.Owner, Repo: r.Repo, Store: root}, true
	}

	return t, true
}

// Resolve picks between the two. A pull request of the repository the checkout
// at root belongs to is reviewed in that checkout, because that is where the
// diff cache, the read marks, and an agent all already look. Anything else is
// reviewed detached.
//
// An empty owner names the checkout's own repository, which is what a bare
// number on the command line means.
func Resolve(ctx context.Context, root, owner, repo string, number int) (Target, error) {
	if owner == "" {
		return Here(ctx, root, number)
	}

	here, err := Here(ctx, root, number)
	if err == nil && strings.EqualFold(here.RepoID(), owner+"/"+repo) {
		return here, nil
	}

	return Away(owner, repo, number)
}

// Lookup finds a staged review by number alone, which is what a bare number
// means outside a checkout: every review lives in the store, so it resolves
// wherever it is typed as long as one review answers to the number.
//
// Two repositories with the same number open is the case it refuses. Guessing
// there would read one pull request while saying the other's number.
func Lookup(number int) (Target, error) {
	home, err := artifact.StateHome()
	if err != nil {
		//nolint:wrapcheck // StateHome's own error already names what failed
		return Target{}, err
	}

	rows, err := prepared.All(home)
	if err != nil {
		return Target{}, fmt.Errorf("reading the store: %w", err)
	}

	var found []string

	for i := range rows {
		if rows[i].Number == number && rows[i].Repository != "" &&
			!slices.Contains(found, rows[i].Repository) {
			found = append(found, rows[i].Repository)
		}
	}

	switch len(found) {
	case 0:
		return Target{}, fmt.Errorf("#%d: %w", number, ErrNothingStaged)
	case 1:
		owner, repo, _ := strings.Cut(found[0], "/")

		return Away(owner, repo, number)
	}

	return Target{}, fmt.Errorf("#%d is staged for %s: %w",
		number, strings.Join(found, " and "), ErrAmbiguous)
}
