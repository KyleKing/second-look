package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/blob"
	"github.com/kyleking/second-look/internal/brief"
	"github.com/kyleking/second-look/internal/config"
	"github.com/kyleking/second-look/internal/conversations"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/get"
	"github.com/kyleking/second-look/internal/ghrun"
	"github.com/kyleking/second-look/internal/humanize"
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
	errUsageContext   = errors.New("usage: second-look context <pr> <comment-id>")
	errUsageTodo      = errors.New("usage: second-look todo <pr>")
	errUsageShow      = errors.New("usage: second-look show <pr> [--diff|--payload|--threads]")
	errUsageSkill     = errors.New("usage: second-look skill")
)

// The two flags every listing command takes, named so the switch and the
// terminal check cannot disagree about their spelling.
const (
	helpArg = "help"
	jsonArg = "--json"
)

// The keys named in more than one screen's legend, so the three cannot drift.
const (
	enterKey   = "enter"
	refreshKey = "ctrl+r"
)

// The keys all three list screens share. They are one tui.List, so a reader
// should not have to learn three vocabularies for the same keys.
func helpMove() [][2]string {
	return [][2]string{
		{"j/k, ctrl+u/d, g/G", "move, half page, top and bottom"},
		{"ctrl+e/ctrl+y", "scroll without moving the cursor"},
		{"1/2/3, ] / [", "the queue to read: the inbox, the conversations, what is staged here"},
	}
}

func helpGroup() [][2]string {
	return [][2]string{
		{"tab", "the next group"},
		{"/", "narrow to the rows carrying a word; esc puts them back"},
	}
}

func helpLeave() [][2]string {
	return [][2]string{{"q, esc", "leave"}}
}

// prose is the sentence or two a legend needs past its keys, as rows carrying
// no key of their own.
func prose(lines ...string) [][2]string {
	out := make([][2]string, 0, len(lines)+1)
	out = append(out, [2]string{})

	for _, l := range lines {
		out = append(out, [2]string{"", l})
	}

	return out
}

// helpFor assembles a legend from the shared keys, a screen's own, and its
// closing prose.
func helpFor(parts ...[][2]string) [][2]string {
	var out [][2]string
	for _, p := range parts {
		out = append(out, p...)
	}

	return out
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return reviewCurrent(ctx, stdin, stdout)
	}

	switch args[0] {
	case "-h":
		return write(stdout, shortHelp)
	case "--help", helpArg:
		return write(stdout, longHelp)
	case "get":
		return getCmd(ctx, args[1:], stdin, stdout)
	case "comment":
		return commentCmd(ctx, args[1:], stdin, stdout)
	case "show":
		return showCmd(ctx, args[1:], stdout)
	case "context":
		return contextCmd(ctx, args[1:], stdout)
	case "todo":
		return todoCmd(ctx, args[1:], stdout)
	case "post":
		return postCmd(ctx, args[1:], stdout)
	case "inbox":
		return inboxCmd(ctx, args[1:], stdin, stdout)
	case "threads":
		return threadsCmd(ctx, args[1:], stdin, stdout)
	case "reviews":
		return reviewsCmd(ctx, args[1:], stdin, stdout)
	case "skill":
		return skillCmd(args[1:], stdout)
	default:
		return reviewCmd(ctx, args, stdin, stdout)
	}
}

// reviewCurrent opens the review for whatever the checkout is standing on.
// There is no default: on a branch with no pull request the answer is an error,
// not a guess at which one was meant.
func reviewCurrent(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	number, err := get.Current(ctx, ".")
	if err != nil {
		return fmt.Errorf("opening this branch's review: %w", err)
	}

	return openRef(ctx, ref{number: number}, stdin, stdout)
}

func reviewCmd(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	r, err := parseRef(args[0])
	if err != nil {
		return fmt.Errorf("%w: %q", errUnknownCommand, args[0])
	}

	if len(args) > 1 {
		return errUsageReview
	}

	return openRef(ctx, r, stdin, stdout)
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

// openRef opens the review for a pull request named on the command line or
// chosen off a list.
func openRef(ctx context.Context, r ref, stdin io.Reader, stdout io.Writer) error {
	t, err := get.Resolve(ctx, ".", r.owner, r.repo, r.number)
	if err != nil {
		return fmt.Errorf("opening %s: %w", r, err)
	}

	return openReview(ctx, t, stdin, stdout)
}

// openReview draws the review screen, and answers C by moving the working copy
// onto the pull request and drawing it again.
//
// The move happens out here because the screen has to give the terminal back
// first: the stash question needs stdin, and two programs cannot own the
// terminal at once.
func openReview(ctx context.Context, t get.Target, stdin io.Reader, stdout io.Writer) error {
	for {
		again, err := review(ctx, t, stdout)
		if err != nil || !again {
			return err
		}

		if err := get.Prepare(ctx, stdout, t, confirm(stdin, stdout)); err != nil {
			return fmt.Errorf("checking out #%d: %w", t.Number, err)
		}
	}
}

// review draws the screen once and reports whether it was left through C.
func review(ctx context.Context, t get.Target, stdout io.Writer) (bool, error) {
	if !term.IsTerminal(os.Stdin.Fd()) && !term.IsTerminal(os.Stdout.Fd()) {
		return false, errNoTerminal
	}

	opened, err := get.Open(ctx, t)
	if err != nil {
		return false, fmt.Errorf("opening #%d: %w", t.Number, err)
	}

	// The alternate screen owns the terminal until the screen exits, so what the
	// post wrote is held back and reaches the scrollback afterwards. Writing it
	// as it happens draws over the frame.
	var log strings.Builder

	opts := []tui.Option{
		tui.WithThreads(opened.Threads), tui.WithSeen(opened.Read, opened.SeenPath),
		tui.WithSender(sender(t, opened.Path, &log)), tui.WithTree(tree(opened)),
		tui.WithMerger(merger(t)), tui.WithStore(t.Store), tui.WithOpener(opener(t)),
		// A config that will not parse leaves the built-in patterns rather than
		// stopping a review, the same as it leaves the built-in buckets.
		tui.WithGenerated(generatedPatterns()),
	}

	if d := dispatcher(); d != nil {
		opts = append(opts, tui.WithDispatcher(d))
	}

	reader := blob.Reader{Work: opened.Work, Repo: t.RepoID(), SHA: opened.Review.HeadSHA}
	opts = append(opts, tui.WithBlobs(reader.Read))

	// A review read out of the cache reached the screen without asking GitHub
	// anything, so the screen asks behind the first frame instead.
	if opened.Unverified {
		opts = append(opts, tui.WithHeadCheck(func(ctx context.Context) (string, error) {
			return get.CurrentHead(ctx, t)
		}))
	}

	out, runErr := tui.Run(ctx, opened.Review, opened.Diff, opened.Path,
		submitter(t, opened.Path, &log), opts...)

	// The log is written either way: a post that failed partway through still
	// names the endpoints it reached, which is what says whether anything
	// landed on GitHub.
	if err := write(stdout, log.String()); err != nil {
		return false, err
	}

	if runErr != nil {
		return false, fmt.Errorf("reviewing #%d: %w", t.Number, runErr)
	}

	return out.Checkout, nil
}

// tree is where the working copy stands, which is what the shell key can use
// and the checkout key can offer.
func tree(opened *get.Review) tui.Tree {
	switch {
	case opened.Work == "":
		return tui.TreeNone
	case opened.OnHead:
		return tui.TreeOnHead
	default:
		return tui.TreeElsewhere
	}
}

// submitter posts from inside the review screen. What it returns is the one
// line the footer shows, so it stays short enough to survive a narrow frame,
// and the endpoints it touched go to the log instead.
func submitter(t get.Target, path string, log io.Writer) tui.Submitter {
	return func(ctx context.Context, r *artifact.Review) (string, error) {
		if err := post.Guard(ctx, t.Dir(), t.Remote(), r); err != nil {
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
func sender(t get.Target, path string, log io.Writer) tui.Sender {
	return func(ctx context.Context, r *artifact.Review, id string) (string, error) {
		if err := post.Guard(ctx, t.Dir(), t.Remote(), r); err != nil {
			return "", fmt.Errorf("posting %s: %w", id, err)
		}

		if err := post.One(ctx, post.GH(), path, r, id, log); err != nil {
			return "", fmt.Errorf("posting %s: %w", id, err)
		}

		return "posted " + id + "; it is off the review now", nil
	}
}

// merger squash-merges from inside the review screen, which is the one place
// the decision has the diff behind it: the key is only reachable after the
// review is read, and it refuses while anything is still staged.
func merger(t get.Target) tui.Merger {
	return func(ctx context.Context, r *artifact.Review) (string, error) {
		args := []string{"pr", "merge", strconv.Itoa(r.Number), "--squash", "--delete-branch"}
		if t.Remote() != "" {
			args = append(args, "--repo", t.Remote())
		}

		if err := ghrun.GH().Run(ctx, t.Dir(), args...); err != nil {
			return "", fmt.Errorf("merging #%d: %w", r.Number, err)
		}

		// A merged pull request cannot be reviewed again, so what is staged for
		// it is dead weight rather than unfinished work.
		if err := prepared.DiscardAt(t.Store, r.Number); err != nil {
			return "", fmt.Errorf("merged #%d, and clearing what was staged for it: %w", r.Number, err)
		}

		return fmt.Sprintf("merged %s/%s #%d", r.Owner, r.Repo, r.Number), nil
	}
}

// opener shows the pull request in a browser, which is what to do with a review
// the moment it posts: the thread it opened is on GitHub now and nothing local
// carries it any more.
func opener(t get.Target) tui.Opener {
	return func(ctx context.Context, r *artifact.Review) error {
		args := []string{"browse", strconv.Itoa(r.Number)}
		if t.Remote() != "" {
			args = append(args, "--repo", t.Remote())
		}

		if err := ghrun.GH().Run(ctx, t.Dir(), args...); err != nil {
			return fmt.Errorf("opening #%d: %w", r.Number, err)
		}

		return nil
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

	t, err := target(ctx, args[0])
	if err != nil {
		return err
	}

	if err := get.Prepare(ctx, stdout, t, confirm(stdin, stdout)); err != nil {
		return fmt.Errorf("get %s: %w", args[0], err)
	}

	return nil
}

// stage adds a batch to the review and reports how many of them it held back.
// A comment written by something other than the author is a proposal about
// someone else's code, so it is a draft whatever it arrived as, and the author
// rules on each one. A skip is left alone: that is a finding already declined.
func stage(into *artifact.Review, batch []artifact.Comment) int {
	held := 0

	for i := range batch {
		c := batch[i]
		if c.Status == artifact.StatusReady {
			c.Status = artifact.StatusDraft
			held++
		}

		// Turns append rather than replace, so answering a comment does not
		// need the whole exchange resent and cannot lose the half already there.
		if old := into.Find(c.ID); old != nil {
			c.Turns = append(slices.Clone(old.Turns), c.Turns...)
		}

		into.Upsert(c)
	}

	return held
}

func commentCmd(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) != 2 || args[0] != "add" {
		return errUsageComment
	}

	t, err := target(ctx, args[1])
	if err != nil {
		return err
	}

	path := artifact.Path(t.Store, t.Number)

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

	held := stage(&staged, b.Comments)

	if err := staged.Validate(); err != nil {
		return fmt.Errorf("the batch was rejected and nothing was written:\n%w", err)
	}

	cached, err := artifact.LoadDiff(t.Store, staged.HeadSHA)
	if err != nil {
		return fmt.Errorf("the batch was rejected and nothing was written: %w", err)
	}

	if err := artifact.Resolve(staged.Comments, diff.Parse(cached)); err != nil {
		return fmt.Errorf("the batch was rejected and nothing was written:\n%w", err)
	}

	if err := artifact.Save(path, &staged); err != nil {
		return fmt.Errorf("saving the prepared review: %w", err)
	}

	out := fmt.Sprintf("%s staged, %d total\n",
		humanize.Plural(len(b.Comments), "comment"), len(staged.Comments))
	if held > 0 {
		out += humanize.Plural(held, "comment") + " held as draft for the author to rule on\n"
	}

	return write(stdout, out)
}

func showCmd(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errUsageShow
	}

	flag, err := oneOf(args[1:], errUsageShow, "--diff", "--payload", "--threads")
	if err != nil {
		return err
	}

	t, err := target(ctx, args[0])
	if err != nil {
		return err
	}

	path := artifact.Path(t.Store, t.Number)

	r, err := artifact.Load(path)
	if err != nil {
		return fmt.Errorf("loading the prepared review: %w", err)
	}

	// --threads answers the one question a reply needs: which comment id does
	// this conversation hang from. It reads what `get` cached rather than
	// asking GitHub, so it costs nothing and matches what the screen shows.
	if flag == "--threads" {
		var open []threads.Thread
		if err := artifact.LoadThreads(t.Store, r.HeadSHA, &open); err != nil {
			return fmt.Errorf("reading the cached review threads: %w", err)
		}

		return writeJSON(stdout, threads.Replyable(open))
	}

	// --diff is the code the review is about, with each comment marked on the
	// line it anchors to. An agent given a path and a line number has to go and
	// find the change itself, and what it finds is not what the screen shows.
	if flag == "--diff" {
		cached, err := artifact.LoadDiff(t.Store, r.HeadSHA)
		if err != nil {
			return fmt.Errorf("reading the cached diff: %w", err)
		}

		return write(stdout, brief.Diff(diff.Parse(cached), r))
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

// contextCmd is one comment with everything around it: the hunk it anchors in,
// the note that never posts, and the conversation it answers. It is what an
// agent asked about a finding needs and what `show` alone cannot give it.
func contextCmd(ctx context.Context, args []string, stdout io.Writer) error {
	const wants = 2

	if len(args) != wants {
		return errUsageContext
	}

	t, err := target(ctx, args[0])
	if err != nil {
		return err
	}

	r, d, open, err := readAll(t)
	if err != nil {
		return err
	}

	out, err := brief.Comment(r, args[1], d, open, 0)
	if err != nil {
		return fmt.Errorf("reading %s: %w", args[1], err)
	}

	return write(stdout, out)
}

// todoCmd prints every comment an agent still owes work on, each with the
// context reading it needs. It is what T in the review screen writes out, so
// the set an agent drains is the same set either way.
func todoCmd(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errUsageTodo
	}

	t, err := target(ctx, args[0])
	if err != nil {
		return err
	}

	r, d, open, err := readAll(t)
	if err != nil {
		return err
	}

	return write(stdout, brief.Owed(r, d, open))
}

// readAll is the review with what was cached against its head: the diff and the
// open conversations. Three commands need all three and none of them differ in
// how they ask.
func readAll(t get.Target) (*artifact.Review, *diff.Diff, []threads.Thread, error) {
	r, err := artifact.Load(artifact.Path(t.Store, t.Number))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("loading the prepared review: %w", err)
	}

	cached, err := artifact.LoadDiff(t.Store, r.HeadSHA)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("reading the cached diff: %w", err)
	}

	var open []threads.Thread
	if err := artifact.LoadThreads(t.Store, r.HeadSHA, &open); err != nil {
		return nil, nil, nil, fmt.Errorf("reading the cached review threads: %w", err)
	}

	return r, diff.Parse(cached), open, nil
}

func postCmd(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errUsagePost
	}

	dry, only, err := postFlags(args[1:])
	if err != nil {
		return err
	}

	t, err := target(ctx, args[0])
	if err != nil {
		return err
	}

	path := artifact.Path(t.Store, t.Number)

	r, err := artifact.Load(path)
	if err != nil {
		return fmt.Errorf("loading the prepared review: %w", err)
	}

	//nolint:wrapcheck // guardAnchors' own error already names what failed
	if err := post.Guard(ctx, t.Dir(), t.Remote(), r); err != nil {
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

// inboxLimit is how many pull requests each bucket asks for when the config
// names no limit of its own. A queue longer than this is not a queue, and the
// search costs the same either way.
const inboxLimit = 30

// queue is the review queue: the sections the config names, or the three
// built-in buckets when it names none.
//
// A config that cannot be read is reported and the built-in buckets are used, so
// a typo in a file leaves a working queue rather than no queue. Every other
// failure here belongs to one bucket and is printed in it.
func queue(ctx context.Context, out io.Writer) []inbox.Bucket {
	cfg, err := configured(out)
	if err != nil {
		return nil
	}

	return runQueue(ctx, cfg)
}

// configured reads the config, reporting a file that cannot be read and falling
// back to the built-in buckets: a typo in a file should leave a working queue
// rather than no queue.
//
// The report goes to stderr, because --json puts a document on stdout and a
// warning in front of it is a document nothing can parse. It is read before the
// screen opens, since nothing can be written to a terminal the alternate screen
// owns.
func configured(out io.Writer) (*config.Config, error) {
	cfg, err := loadConfig()
	if err == nil {
		return cfg, nil
	}

	if err := write(out, err.Error()+"\n"); err != nil {
		return nil, err
	}

	return &config.Config{}, nil
}

func runQueue(ctx context.Context, cfg *config.Config) []inbox.Bucket {
	if len(cfg.Sections) == 0 {
		return inbox.Buckets(ctx, ".", queueLimit(cfg))
	}

	return inbox.Configured(ctx, ".", configSections(cfg), queueLimit(cfg))
}

// planQueue is the same queue as a plan the screen runs itself, one search at a
// time, so it can draw each as it lands.
func planQueue(cfg *config.Config) []inbox.Bucket {
	if len(cfg.Sections) == 0 {
		return inbox.Plan(queueLimit(cfg))
	}

	return inbox.PlanFor(configSections(cfg), queueLimit(cfg))
}

func queueLimit(cfg *config.Config) int {
	if cfg.Limit <= 0 {
		return inboxLimit
	}

	return cfg.Limit
}

func configSections(cfg *config.Config) []inbox.Section {
	out := make([]inbox.Section, 0, len(cfg.Sections))
	for i := range cfg.Sections {
		out = append(out, inbox.Section{Name: cfg.Sections[i].Name, Query: cfg.Sections[i].Query})
	}

	return out
}

// generatedPatterns is what this repository says it writes by machine, and
// nothing where the config is missing or unreadable.
// Dispatcher runs the configured command over the written-out todo set. It is
// nil where the config names none, and T then writes the file and says where it
// is rather than starting anything.
//
// The command's own output goes to a log beside the set, since the screen owns
// the terminal while it runs and a line of an agent's reasoning drawn over the
// frame is worse than none.
func dispatcher() tui.Dispatcher {
	const logPerm = 0o600

	cfg, err := loadConfig()
	if err != nil || len(cfg.Dispatch) == 0 {
		return nil
	}

	argv := slices.Clone(cfg.Dispatch)

	return func(ctx context.Context, path string) (string, error) {
		log := strings.TrimSuffix(path, ".md") + ".log"

		//nolint:gosec // the log sits beside the set this same run wrote
		f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, logPerm)
		if err != nil {
			return "", fmt.Errorf("opening %s: %w", log, err)
		}
		defer f.Close() //nolint:errcheck // the child holds its own handle

		cmd := exec.CommandContext(ctx, argv[0], append(argv[1:], path)...) // #nosec G204 -- the caller's own config
		cmd.Stdout, cmd.Stderr = f, f

		if err := cmd.Start(); err != nil {
			return "", fmt.Errorf("running %s: %w", argv[0], err)
		}

		return fmt.Sprintf("%s is reading %s; its output goes to %s", argv[0], path, log), nil
	}
}

func generatedPatterns() []string {
	cfg, err := loadConfig()
	if err != nil {
		return nil
	}

	return cfg.Generated
}

func loadConfig() (*config.Config, error) {
	path, err := config.Path()
	if err != nil {
		return nil, fmt.Errorf("reading your config: %w", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("reading your config: %w", err)
	}

	return cfg, nil
}

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
		return openQueue(ctx, tabConversations, stdin, stdout)
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
		return openQueue(ctx, tabReviews, stdin, stdout)
	}

	rows, err := staged()
	if err != nil {
		return err
	}

	if asJSON == jsonArg {
		return writeJSON(stdout, rows)
	}

	//nolint:wrapcheck // Write's own error already names what failed
	return prepared.Write(stdout, rows, time.Now())
}

// staged is every review on disk: the store's, and anything an artifact tree in
// the working directory still holds. The second is a leftover by definition,
// since opening a review moves what it finds into the store, and it lists
// because an unfinished review nobody can see is one that is lost.
func staged() ([]prepared.Review, error) {
	if err := artifact.AdoptHere("."); err != nil {
		return nil, fmt.Errorf("listing the staged reviews: %w", err)
	}

	home, err := artifact.StateHome()
	if err != nil {
		return nil, fmt.Errorf("listing the staged reviews: %w", err)
	}

	rows, err := prepared.All(home)
	if err != nil {
		return nil, fmt.Errorf("listing the staged reviews: %w", err)
	}

	stray, err := prepared.List(".")
	if err != nil && !errors.Is(err, prepared.ErrNoDir) {
		return nil, fmt.Errorf("listing the staged reviews: %w", err)
	}

	for i := range stray {
		stray[i].Stray = true
	}

	return append(rows, stray...), nil
}

// inboxCmd prints the review queue in three buckets. It reads GitHub and
// nothing local, so it works from anywhere with a gh login rather than only
// inside a checkout.
func inboxCmd(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	asJSON, err := oneOf(args, errUsageInbox, jsonArg)
	if err != nil {
		return err
	}

	if asJSON != jsonArg && onATerminal() {
		return openQueue(ctx, tabInbox, stdin, stdout)
	}

	buckets := queue(ctx, os.Stderr)

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

// target is which pull request a command names and where its state lives.
//
// A bare number takes the review staged in the working directory when there is
// one, so a directory holding nothing but .second-look still answers for it.
func target(ctx context.Context, arg string) (get.Target, error) {
	r, err := parseRef(arg)
	if err != nil {
		return get.Target{}, err
	}

	if r.here() {
		if t, ok := get.Staged(ctx, ".", r.number); ok {
			return t, nil
		}
	}

	t, err := get.Resolve(ctx, ".", r.owner, r.repo, r.number)
	if err == nil {
		return t, nil
	}

	if r.here() {
		if t, look := get.Lookup(r.number); look == nil {
			return t, nil
		}
	}

	return get.Target{}, fmt.Errorf("%s: %w", r, err)
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
