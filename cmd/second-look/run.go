package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/conversations"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/get"
	"github.com/kyleking/second-look/internal/inbox"
	"github.com/kyleking/second-look/internal/post"
	"github.com/kyleking/second-look/internal/prepared"
	"github.com/kyleking/second-look/internal/skill"
	"github.com/kyleking/second-look/internal/threads"
	"github.com/kyleking/second-look/internal/tui"
)

var (
	// An agent that runs the one command the skill tells it not to lands here.
	// Bubble Tea's own refusal names a missing device, which says nothing about
	// what to run instead.
	errNoTerminal = errors.New("the review screen needs a terminal and there is none here; " +
		"read the prepared review with second-look show <pr>, or open the screen from a terminal")
	errNotAPRNumber   = errors.New("not a pull request number")
	errUnknownCommand = errors.New("unknown command; try second-look -h")
	errUsageComment   = errors.New("usage: second-look comment add <pr>")
	errUsageGet       = errors.New("usage: second-look get <pr>")
	errUsagePost      = errors.New("usage: second-look post <pr> [--dry-run|--only <id>]")
	errUsageReview    = errors.New("usage: second-look <pr>")
	errUsageInbox     = errors.New("usage: second-look inbox [--json]")
	errUsageThreads   = errors.New("usage: second-look threads [--json]")
	errUsageReviews   = errors.New("usage: second-look reviews [--json]")
	errUsageShow      = errors.New("usage: second-look show <pr> [--payload|--threads]")
	errUsageSkill     = errors.New("usage: second-look skill")
)

// The two flags every listing command takes, named so the switch and the
// terminal check cannot disagree about their spelling.
const (
	helpArg = "help"
	jsonArg = "--json"
)

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return reviewCurrent(ctx, stdout)
	}

	switch args[0] {
	case "-h":
		return write(stdout, shortHelp)
	case "--help", helpArg:
		return write(stdout, longHelp)
	case "get":
		return getCmd(ctx, args[1:], stdin, stdout)
	case "comment":
		return commentCmd(args[1:], stdin, stdout)
	case "show":
		return showCmd(args[1:], stdout)
	case "post":
		return postCmd(ctx, args[1:], stdout)
	case "inbox":
		return inboxCmd(ctx, args[1:], stdout)
	case "threads":
		return threadsCmd(ctx, args[1:], stdin, stdout)
	case "reviews":
		return reviewsCmd(ctx, args[1:], stdin, stdout)
	case "skill":
		return skillCmd(args[1:], stdout)
	default:
		return reviewCmd(ctx, args, stdout)
	}
}

// reviewCurrent opens the review for whatever the checkout is standing on.
// There is no default: on a branch with no pull request the answer is an error,
// not a guess at which one was meant.
func reviewCurrent(ctx context.Context, stdout io.Writer) error {
	number, err := get.Current(ctx, ".")
	if err != nil {
		return fmt.Errorf("opening this branch's review: %w", err)
	}

	return openReview(ctx, number, stdout)
}

func reviewCmd(ctx context.Context, args []string, stdout io.Writer) error {
	number, err := prNumber(args[0])
	if err != nil {
		return fmt.Errorf("%w: %q", errUnknownCommand, args[0])
	}

	if len(args) > 1 {
		return errUsageReview
	}

	return openReview(ctx, number, stdout)
}

// onATerminal reports whether a screen can be drawn. Both ends are checked
// because a run with either one redirected is a run whose output someone is
// reading as text.
func onATerminal() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd())
}

// currentRepo is the owner/name this checkout files reviews against, or empty
// when there is no reading it. It is empty rather than an error because the
// conversation queue works anywhere with a gh login, and only staging a reply
// needs to know which repository this is.
func currentRepo(ctx context.Context) string {
	name, err := get.Repository(ctx, ".")
	if err != nil {
		return ""
	}

	return name
}

func openReview(ctx context.Context, number int, stdout io.Writer) error {
	if !term.IsTerminal(os.Stdin.Fd()) && !term.IsTerminal(os.Stdout.Fd()) {
		return errNoTerminal
	}

	opened, err := get.Open(ctx, ".", number)
	if err != nil {
		return fmt.Errorf("opening #%d: %w", number, err)
	}

	// The alternate screen owns the terminal until the screen exits, so what the
	// post wrote is held back and reaches the scrollback afterwards. Writing it
	// as it happens draws over the frame.
	var log strings.Builder

	runErr := tui.Run(ctx, opened.Review, opened.Diff, opened.Path, submitter(opened.Path, &log),
		tui.WithThreads(opened.Threads), tui.WithSeen(opened.Read, opened.SeenPath),
		tui.WithSender(sender(opened.Path, &log)))

	// The log is written either way: a post that failed partway through still
	// names the endpoints it reached, which is what says whether anything
	// landed on GitHub.
	if err := write(stdout, log.String()); err != nil {
		return err
	}

	if runErr != nil {
		return fmt.Errorf("reviewing #%d: %w", number, runErr)
	}

	return nil
}

// openStaged opens a review the person chose off a list, moving the checkout
// onto the pull request first when that is what stands in the way.
//
// The tree is left alone otherwise: a review that opens where you are standing
// has no reason to touch it, and the move is only offered because choosing a row
// off a list is already saying which pull request you mean. The screen has given
// the terminal back by the time this runs, so the stash question can be asked.
func openStaged(ctx context.Context, number int, stdin io.Reader, stdout io.Writer) error {
	err := openReview(ctx, number, stdout)
	if !errors.Is(err, get.ErrNotOnHead) && !errors.Is(err, get.ErrStaleReview) {
		return err
	}

	if err := get.Prepare(ctx, stdout, ".", number, confirm(stdin, stdout)); err != nil {
		return fmt.Errorf("preparing #%d: %w", number, err)
	}

	return openReview(ctx, number, stdout)
}

// submitter posts from inside the review screen. What it returns is the one
// line the footer shows, so it stays short enough to survive a narrow frame,
// and the endpoints it touched go to the log instead.
func submitter(path string, log io.Writer) tui.Submitter {
	return func(ctx context.Context, r *artifact.Review) (string, error) {
		if err := post.Guard(ctx, ".", r); err != nil {
			return "", fmt.Errorf("submitting: %w", err)
		}

		if err := post.Run(ctx, post.GH(), path, r, log); err != nil {
			return "", fmt.Errorf("submitting: %w", err)
		}

		return fmt.Sprintf("posted to %s/%s #%d", r.Owner, r.Repo, r.Number), nil
	}
}

// sender posts one comment from inside the review screen. The guard runs first
// for the same reason it does for a whole review: a comment whose line moved
// would land on whatever now sits there.
func sender(path string, log io.Writer) tui.Sender {
	return func(ctx context.Context, r *artifact.Review, id string) (string, error) {
		if err := post.Guard(ctx, ".", r); err != nil {
			return "", fmt.Errorf("posting %s: %w", id, err)
		}

		if err := post.One(ctx, post.GH(), path, r, id, log); err != nil {
			return "", fmt.Errorf("posting %s: %w", id, err)
		}

		return "posted " + id + "; it is off the review now", nil
	}
}

// batch is what an agent writes on stdin. It is the schema's fields, local ones
// included, so drafting evidence and drafting the comment are one call.
type batch struct {
	Note     string             `json:"note,omitempty"`
	Body     string             `json:"body,omitempty"`
	Event    string             `json:"event,omitempty"`
	Comments []artifact.Comment `json:"comments"`
}

func getCmd(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) != 1 {
		return errUsageGet
	}

	number, err := prNumber(args[0])
	if err != nil {
		return err
	}

	if err := get.Prepare(ctx, stdout, ".", number, confirm(stdin, stdout)); err != nil {
		return fmt.Errorf("get %d: %w", number, err)
	}

	return nil
}

func commentCmd(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) != 2 || args[0] != "add" {
		return errUsageComment
	}

	path, err := artifactPath(args[1])
	if err != nil {
		return err
	}

	r, err := artifact.Load(path)
	if err != nil {
		return fmt.Errorf("loading the prepared review: %w", err)
	}

	var b batch

	dec := json.NewDecoder(stdin)
	dec.DisallowUnknownFields()

	if err := dec.Decode(&b); err != nil {
		return fmt.Errorf("reading the batch: %w", err)
	}

	if b.Note != "" {
		r.Note = b.Note
	}
	if b.Body != "" {
		r.Body = b.Body
	}
	if b.Event != "" {
		r.Event = b.Event
	}

	// Validate the whole review before writing any of it, so a bad batch changes
	// nothing rather than landing the comments ahead of the one that failed.
	staged := *r
	staged.Comments = append([]artifact.Comment(nil), r.Comments...)

	for i := range b.Comments {
		staged.Upsert(b.Comments[i])
	}

	if err := staged.Validate(); err != nil {
		return fmt.Errorf("the batch was rejected and nothing was written:\n%w", err)
	}

	cached, err := artifact.LoadDiff(".", staged.HeadSHA)
	if err != nil {
		return fmt.Errorf("the batch was rejected and nothing was written: %w", err)
	}

	if err := artifact.Resolve(staged.Comments, diff.Parse(cached)); err != nil {
		return fmt.Errorf("the batch was rejected and nothing was written:\n%w", err)
	}

	if err := artifact.Save(path, &staged); err != nil {
		return fmt.Errorf("saving the prepared review: %w", err)
	}

	return write(stdout, fmt.Sprintf("%d comment(s) staged, %d total\n", len(b.Comments), len(staged.Comments)))
}

func showCmd(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errUsageShow
	}

	flag, err := oneOf(args[1:], errUsageShow, "--payload", "--threads")
	if err != nil {
		return err
	}

	path, err := artifactPath(args[0])
	if err != nil {
		return err
	}

	r, err := artifact.Load(path)
	if err != nil {
		return fmt.Errorf("loading the prepared review: %w", err)
	}

	// --threads answers the one question a reply needs: which comment id does
	// this conversation hang from. It reads what `get` cached rather than
	// asking GitHub, so it costs nothing and matches what the screen shows.
	if flag == "--threads" {
		var open []threads.Thread
		if err := artifact.LoadThreads(".", r.HeadSHA, &open); err != nil {
			return fmt.Errorf("reading the cached review threads: %w", err)
		}

		return writeJSON(stdout, threads.Replyable(open))
	}

	payloadOnly := flag == "--payload"

	// --payload prints exactly what would be sent, so what stays local is
	// inspectable rather than promised.
	if payloadOnly {
		payload, replies, err := r.Payload()
		if err != nil {
			return fmt.Errorf("building the payload: %w", err)
		}

		return writeJSON(stdout, map[string]any{"review": payload, "replies": replies})
	}

	return writeJSON(stdout, r)
}

func postCmd(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errUsagePost
	}

	dry, only, err := postFlags(args[1:])
	if err != nil {
		return err
	}

	path, err := artifactPath(args[0])
	if err != nil {
		return err
	}

	r, err := artifact.Load(path)
	if err != nil {
		return fmt.Errorf("loading the prepared review: %w", err)
	}

	//nolint:wrapcheck // guardAnchors' own error already names what failed
	if err := post.Guard(ctx, ".", r); err != nil {
		return err
	}

	if only != "" {
		//nolint:wrapcheck // One's own error already names what failed
		return post.One(ctx, post.GH(), path, r, only, stdout)
	}

	if dry {
		//nolint:wrapcheck // DryRun's own error already names what failed
		return post.DryRun(stdout, r)
	}

	//nolint:wrapcheck // Run's own error already names what failed
	return post.Run(ctx, post.GH(), path, r, stdout)
}

// postFlags reads post's two optional flags. --only takes an id, and neither
// flag may accompany the other: a dry run of one comment is a different command
// nobody asked for, and guessing which was meant would post something.
func postFlags(args []string) (bool, string, error) {
	switch {
	case len(args) == 0:
		return false, "", nil
	case len(args) == 1 && args[0] == "--dry-run":
		return true, "", nil
	case len(args) == 2 && args[0] == "--only" && args[1] != "":
		return false, args[1], nil
	}

	return false, "", errUsagePost
}

// inboxLimit is how many pull requests each bucket asks for. A queue longer
// than this is not a queue, and the search costs the same either way.
const inboxLimit = 30

// threadsCmd prints the conversation queue: the discussions across your open
// pull requests that moved since you last looked, then the ones still waiting on
// you, then the ones waiting on somebody else.
//
// It never writes a read mark. Printing a queue is not reading it, and a run
// that emptied the first bucket by being run would hide the one thing the
// bucket is for.
func threadsCmd(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	asJSON, err := oneOf(args, errUsageThreads, jsonArg)
	if err != nil {
		return err
	}

	// A terminal gets the screen; a pipe or --json gets the text, so an agent
	// and a person run the same command.
	if asJSON != jsonArg && onATerminal() {
		return openThreads(ctx, stdin, stdout)
	}

	queue, err := conversations.Fetch(ctx, ".", conversations.DefaultLimit)
	if err != nil {
		return fmt.Errorf("reading your conversations: %w", err)
	}

	looked, err := loadLooked()
	if err != nil {
		return err
	}

	buckets := conversations.Buckets(queue, looked)

	if asJSON == jsonArg {
		return writeJSON(stdout, buckets)
	}

	//nolint:wrapcheck // Write's own error already names what failed
	return conversations.Write(stdout, buckets, time.Now())
}

func loadLooked() (*conversations.Looked, error) {
	path, err := conversations.LookedPath()
	if err != nil {
		return nil, fmt.Errorf("reading your read conversations: %w", err)
	}

	looked, err := conversations.LoadLooked(path)
	if err != nil {
		return nil, fmt.Errorf("reading your read conversations: %w", err)
	}

	return looked, nil
}

// reviewsCmd lists what is staged under .second-look in this checkout. The
// artifact is deleted when it posts, so everything it prints is unfinished.
func reviewsCmd(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	asJSON, err := oneOf(args, errUsageReviews, jsonArg)
	if err != nil {
		return err
	}

	if asJSON != jsonArg && onATerminal() {
		return openReviews(ctx, stdin, stdout)
	}

	rows, err := prepared.List(".")
	if err != nil && !errors.Is(err, prepared.ErrNoDir) {
		return fmt.Errorf("listing the staged reviews: %w", err)
	}

	if asJSON == jsonArg {
		return writeJSON(stdout, rows)
	}

	//nolint:wrapcheck // Write's own error already names what failed
	return prepared.Write(stdout, rows, time.Now())
}

// inboxCmd prints the review queue in three buckets. It reads GitHub and
// nothing local, so it works from anywhere with a gh login rather than only
// inside a checkout.
func inboxCmd(ctx context.Context, args []string, stdout io.Writer) error {
	asJSON, err := oneOf(args, errUsageInbox, jsonArg)
	if err != nil {
		return err
	}

	buckets := inbox.Buckets(ctx, ".", inboxLimit)

	if asJSON == jsonArg {
		return writeJSON(stdout, buckets)
	}

	//nolint:wrapcheck // Write's own error already names what failed
	return inbox.Write(stdout, buckets, time.Now())
}

// skillCmd prints the agent instructions the binary carries. Printing the
// content rather than a path is the point: a path into an install tree is
// version-pinned and breaks on the next upgrade, so the caller reads it fresh
// every time and there is no copy to go stale.
func skillCmd(args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return errUsageSkill
	}

	return write(stdout, skill.Content)
}

func write(w io.Writer, s string) error {
	if _, err := io.WriteString(w, s); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	return nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// Code quoted into an anchor or a body is full of < and >, and escaping them
	// makes the output unreadable for the person and the agent both.
	enc.SetEscapeHTML(false)

	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("writing JSON: %w", err)
	}

	return nil
}

func artifactPath(pr string) (string, error) {
	number, err := prNumber(pr)
	if err != nil {
		return "", err
	}

	return artifact.Path(".", number), nil
}

func prNumber(pr string) (int, error) {
	number, err := strconv.Atoi(strings.TrimPrefix(pr, "#"))
	if err != nil || number <= 0 {
		return 0, fmt.Errorf("%q is %w", pr, errNotAPRNumber)
	}

	return number, nil
}

// oneOf reads at most one flag out of a set, and refuses two at once: a command
// given both --payload and --threads was meant to do one of them.
func oneOf(args []string, usage error, want ...string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}

	if len(args) == 1 && slices.Contains(want, args[0]) {
		return args[0], nil
	}

	return "", usage
}
