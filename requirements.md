# second-look requirements

Drafted 2026-08-19, revised 2026-08-20. Evidence for the prior-art claims lives in
[research/prior-art-2026-08.md](research/prior-art-2026-08.md), which is agent research
with light human review. The screen and keymap design lives in [DESIGN.md](DESIGN.md).

## The problem

Reviewing a diff on github.com is a shallow read. The viewed checkbox is per file and
resets when the author pushes, there is no way to search only the parts I have not read
yet, a moved function shows up as a delete plus an unrelated insert, and files arrive in
alphabetical order regardless of how they relate. So I skim, and the review I post says
LGTM whether or not I looked.

Separately, I already prepare reviews with Claude Code through the `change-review`
skill, staging comments in a markdown file and posting them by hand. That works and it
is not reproducible: the file has no schema, nothing checks a comment still anchors to
the line it cited, and posting is a series of `gh` calls I supervise.

The tool fixes the second problem first, because that is the part with no existing
implementation and the part I use every day.

A second look is what the review is supposed to be and usually is not. Binary
`second-look`, aliased to `sl`.

## Scope

Review targets, in priority order:

1. A GitHub pull request, ending in a posted review, whether or not it is cloned on this
   laptop
2. Local uncommitted or branch changes, with no posting endpoint, where comments either
   stay local or land as `[TODO:` markers in the source
3. jj revisions and arbitrary git ranges

Other forges (GitLab first) stay out of the first cut. The forge layer gets an interface
so adding one later is not a rewrite.

## Decisions

### Language and shape

**Go, built fresh.** tuicr covers a large fraction of this already and it is Rust, its
persistence format is undocumented, and none of the three unbuilt capabilities are in
it. I own the Go stack, the Bubble Tea idiom, and the release pipeline (brew tap, gh
extension), so the overlap is worth paying for.

**A CLI, not a daemon.** One process that starts fast, holds no state between
invocations, and reads everything it needs from `.second-look/` and the cache. A daemon
buys a warm index and costs memory, a lifecycle, and a class of stale-state bugs. If
start time turns out to be the problem, the fix is a better cache rather than a resident
process.

**`--help` is the LLM's entry point.** `-h` prints the short form for a human who
forgot a flag. `--help` prints everything an agent needs to drive the tool with no other
documentation: every subcommand, the JSON shapes it accepts and emits, and the errors it
raises. The tool is self-documenting or Claude Code cannot use it reliably.

**Headless core, TUI first.** The core owns the diff model, hunk identity, seen-state,
and the review artifact, and it exposes both a Go API and a CLI over JSON. The Bubble Tea
TUI is the first consumer. An nvim plugin later shells out to the same CLI, which is
what keeps the review artifact frontend-agnostic. gh-repo-dashboard's three front ends
over one set of internals is the pattern to copy.

**Claude Code drives the drafting.** No API key in the tool and no prompt in the tool.
The CLI exposes subcommands and JSON the way `hunk session comment apply` does, and my
`change-review` skill drives it, so the voice rules stay in one place. hunk.dev's
interface is the reference to beat.

### Storage and state

**The artifact lives in the repo at `.second-look/`.** Gitignored globally, so an agent
finds it without knowing a state path. It is not designed to be committed and nothing
breaks if it is, because it holds no secrets.

**TOML on disk, JSON at both edges.** A person edits the file, so it is TOML: comments
are allowed, multi-line strings stay readable, and a hand-edit does not mean counting
brackets. The agent that drafts comments and the `gh` CLI that posts them both speak
JSON, so JSON is the input and the output and never the thing anyone edits.

**Every field is posted or local, declared once.** The payload builder reads only the
posted fields, which makes the split structural rather than a list someone maintains.
Local fields carry what the review is actually built on: the command that proved a
finding and what it printed, the doubt, the reason a finding was declined. A `skip` with
its reason stays in the file, so a considered-and-declined finding reads as considered.

**Seen-state is a content hash, and range-diff was measured out.** The rule was to
delegate to `git range-diff` before inventing anything, falling back to a content hash
only for hunks it could not map. Built and tested both ways, range-diff answers nothing
the hash does not.

It reports per commit, and calls a pair identical only when the patch matches byte for
byte, context included. Rebase a branch onto a commit that touches an unrelated file and
range-diff says `=` while the two cumulative diffs are byte-identical, so the hash already
carries the mark. Rebase onto a commit that touches the hunk's own context and range-diff
says `!` while the hunk's text differs, so the hash correctly leaves it unread. The two
conditions are the same condition. Applying range-diff's verdict per hunk rather than per
commit would mean attributing a cumulative-diff hunk to one commit, which is blame-level
work with its own failure modes, and it is not obviously better than reading the hunk
again.

So a hunk is identified by the file it belongs to plus every line of the hunk, kinds
included, with the line numbers left out. A hunk that slides down the file stays read; a
hunk whose text changed comes back unread. Marks live in `.second-look/seen/pr-<n>.toml`,
keyed by that hash rather than by head commit, and are pruned to the hunks the current
diff still carries. Cached diffs are collected when the PR merges or after a TTL.

**Cache aggressively**, following gh-repo-dashboard's model. Every forge call, every
parsed diff, and every derived index is cacheable, scoped by repo and PR, invalidated on
a new head SHA. Keyed by upstream so parallel checkouts of one remote share a read.

### Shared code

**`kyleking/aragonite`** is the shared Go layer, named for the mineral coral skeletons
are built from, alongside calcipy and corallium.

Extracted from gh-repo-dashboard, which already has all three with tests (~8.7k lines):

- `forge/` — GitHub through the `gh` CLI: pull requests, reviews, comments, CI
- `vcs/` — git and jj behind one interface, with diff, identity, and stamp
- `cache/` — disk cache with TTL and scoping

Extracted from gh-repo-dashboard as the second consumer needs them:

- `filter/` — the predicate, query, and sort engine behind both tools' pull request lists
- `tui/table` — already depends on nothing but lipgloss and uniseg

Extracted from wavez later, when second-look has a real query list rather than an
imagined one:

- `codeintel/` — per-project SQLite store of symbols, edges, trigram FTS, and
  line-to-test coverage, which is what where-used, blast radius, and relation-grouping
  all query

`astgrep` stays in wavez. It is a thin runner over the ast-grep binary and second-look
can rewrite the part it needs.

Development happens against local checkouts through a `go.work`. gh-repo-dashboard stays
unpushed until the extraction is green, and an older version stays pinned in the
meantime.

### The inbox

Both tools keep their own inbox and share everything underneath it. gh-repo-dashboard
answers "what is the state of my repositories" and second-look answers "what do I owe a
review", which are different questions that happen to read the same data.

That boundary is also the division of labour. gh-repo-dashboard owns what is on disk:
which clones exist, which worktree holds which branch, what is dirty, what is behind. It
already finds the peer checkouts of one remote, which is the hard half. second-look owns
the review and the conversations, and asks gh-repo-dashboard for the disk rather than
growing a second directory scanner. What both read (pull requests, CI, the diff) is
cached once in aragonite, so neither is the other's server and neither warms a cache the
other cannot use.

Shared, in aragonite: one per-laptop cache, so opening either tool warms the other, and
the filter, search, and sort engine, which gh-repo-dashboard's `internal/filters` already
implements as a generic `Predicate[T]` over a query language. Generic TUI helpers go
under `tui/`, starting with the table, and only become their own module if something that
is not a git tool needs them.

Not shared: the screens. second-look's inbox is a task list in three buckets, ordered
pending my review, then reviewed and open, then reviewed and merged.

The merged bucket has a consequence worth stating: the review record outlives the pull
request. Cached diffs are collected on merge, and what I said about a change is what I
want to find later, so the artifact and the cached diff have separate lifetimes.

### Branch and working tree

A dirty tree never blocks a review. Being on the wrong commit blocks only what actually
reads the tree.

When HEAD does not match the PR head, show the mismatch, the specific resolution as a
keybinding, and how many in-progress comments survive it. Never act silently. Comments
that cannot be re-anchored after a move are surfaced rather than dropped.

### Reviewing needs no checkout

Reading a diff, writing comments on it, answering a conversation, and posting all of it
need GitHub and a place to keep a file. None of them need a working tree: the diff the
anchors are quoted from is the pull request's own diff, which comes from the API, and a
threaded reply carries a comment id rather than a line. So a checkout is what the tool
asks for when the reader wants to read around the change or run it, and nothing else.

Two consequences. The queue spans repositories, so the artifact for a pull request with
no clone on this laptop lives in the user state directory, keyed by owner, repository, and
number, and `post` finds it from anywhere. An artifact keeps living in the checkout's
`.second-look/` when there is one, because that is where an agent looks for it and where
the diff cache and the read marks already are.

Checking out is then a verb rather than a precondition, and it is lazy: offered from the
review screen when reading the tree is the thing being asked for, and never done to open
a screen. A checkout that has to move is a checkout that asks first.

### Which clone, when there are several

One remote can be cloned several times on one laptop, plus its worktrees, and asking a
person to remember which directory holds which branch is the thing gh-repo-dashboard
already answers. So second-look asks it rather than scanning directories itself:
`gh repo-dashboard --cli` prints every checkout it knows with its remote identity, branch,
worktrees, and working-tree state, from cache, so the answer costs no network.

Ranking, when more than one clone answers: the one already standing on the pull request
head, then a clean one, then the one whose worktree can take a new branch, and the reader
picks when the ranking is a guess. A dirty tree is offered the stash question rather than
being ruled out.

### Images

Deferred, honestly. `gh` cannot upload an image and posting to a public host is not an
option. For now, write the markdown image syntax into the comment, and on post open both
the comment in the browser and the directory holding the images, so the drag is one
motion. Only do this when the review actually contains image links. Automating it later
means Playwright driving GitHub's own editor rather than an LLM, and that is a spike, not
a plan.

## Requirements

### Must

The review pipeline, which is the walking skeleton:

- Fetch a PR and build a review artifact under `.second-look/`, holding the diff I
  reviewed, the head SHA, and an empty comment set
- A documented JSON schema for the artifact, versioned, hand-editable in `$EDITOR`
  without the tool running
- Claude Code adds, edits, and removes comments through CLI subcommands taking JSON on
  stdin, with validation that rejects a batch rather than silently dropping a comment
- Every comment anchors to a file plus a hunk or line, and the anchor is validated
  against the live diff before posting. prr's byte-for-byte guard is the bar
- Post the whole review in one atomic call to `/reviews`, so a partial failure leaves
  nothing posted
- Submit is a keybinding. Dropping to a shell to post is a failure of the design
- Shelling out is fine and lazygit is the bar: the subprocess owns the terminal, its
  stdout and stderr are seen as intended, and the TUI restores cleanly around it
- Comments carry evidence, not only a citation. A common flow is running the code under
  review, by hand or through Claude Code, then writing a comment from what happened. The
  schema needs a place for that output and the TUI needs a way to attach it
- Local fields, shown while reviewing and never sent. The split is declared once in the
  schema and enforced by the payload builder, which reads only the posted fields, so a
  private note cannot leak by being forgotten
- An unknown field is refused, in a batch or in the file. A misspelled key is a hand-edit
  that will not do what its author meant, and a field the schema does not know is one the
  split cannot classify
- Draft text persists to the artifact and never posts. A draft present at submit time
  blocks the submit and is flagged for manual review

Navigation, only as far as the pipeline needs:

- Jump between hunks, and mark a hunk or a file seen
- Seen-state survives a force-push through range-diff or interdiff
- Seen-state fully replaces per-commit browsing. No commit list until it proves necessary

### Should

- Search restricted to seen or unseen hunks. No tool anywhere does this
- A comment view: every comment, searchable and skimmable, resolved collapsed to a count
  per file
- Group files by relation rather than alphabetically. Also unbuilt anywhere. Directory
  adjacency is universally useful and package boundaries are strict in a monorepo, so
  grouping has to respect both
- A review inbox as a task list in three buckets, ordered pending my review, reviewed and
  open, then reviewed and merged, with per-PR metadata that makes triage possible without
  opening it. Searchable and sortable, and available from the CLI as well as the TUI
- Stacked reviews shown as a stack, with the ability to move between them from the inbox
- Replace [gh-dash](https://github.com/dlvhdr/gh-dash), which is the tool this one has to
  be better than to be worth opening. What it does that second-look does not yet: sections
  driven by arbitrary search queries rather than three fixed buckets, issues beside pull
  requests, and the verbs (checkout, comment, approve, merge, close) reachable from the
  list. What second-look does that it cannot: read the diff properly, keep what was read,
  hold a review while it is written, and answer conversations
- Move between reviews without a checkout and without leaving the screen, which is the
  whole reason a dashboard is faster than a browser tab. Opening a pull request from any
  list costs one API read, not a clone and a branch switch
- A review-cost rating, deterministic, described below
- Toggle whitespace and syntax-aware diffs inside a session, which no reviewed tool
  offers as a toggle
- Side-by-side view as a toggle
- Jump to a definition or its usages outside the diff, and come back. This is the same
  motion as seeing the walking skeleton of a change, so it earns real screen space rather
  than a popup

### Could

- Move detection with a side-by-side of the origin file. Unbuilt anywhere, and mergiraf's
  AST reconciliation is the closest working reference
- Diagrams generated from the symbol graph, shown in the context pane: where-used, blast
  radius, call and sequence views
- Posting standalone comments rather than a full review
- GitLab, once the forge interface has been exercised once
- An nvim frontend over the same CLI

### Won't, for now

- Any LLM call inside the tool
- A daemon, a server, or anything that outlives the process
- Rebuilding a diff viewer that hunk.dev or tuicr already does well, if wrapping one
  turns out cheaper than building one

## Review cost

Linear's version misses because it counts lines. One line of Pulumi is usually harder
than twenty lines plus fifty-five whitespace changes in a TS component.

`wavez/_ai_/notes/is-it-risky-deterministically.md` already researched this with
citations and its conclusions carry over. No deterministic "is this safe" oracle exists,
and even Meta's RADAR, the one system that lands diffs with no human review, stacks
deterministic gates with an ML score, an LLM, and a rejection window rather than finding
a classifier that replaces them. Two signals are cheap and deterministic, and neither is
diff size.

The rating is deterministic and produces a number, computed over the diff with comments
and whitespace-only changes excluded first:

- **Changed-symbol extraction** with tree-sitter maps hunks to their enclosing function
  or class and classifies each change as body-only, signature, new, or deleted. A
  signature change escalates on its own. This is the multiplier that makes the other two
  signals precise
- **Capability delta**: does the diff introduce a new instance of a dangerous operation
  class that the base did not have. Two scans plus a set difference, seconds per PR. Its
  honest meaning is "no new capability visible to syntax," which still separates the
  cases that matter
- **Blast radius**: the transitive importer count of each changed non-test file. Real,
  and the wavez note is clear that import graphs overcount, that module granularity is
  the reliable granularity, and that dynamic imports and runtime-registered handlers
  undercount. Treat it as a later addition rather than a first-cut input, and cache the
  graph by base SHA

The rating is advisory. It ranks the inbox and it never decides anything.

## Open questions

**Whether the shared pull request type moves too.** Both the filter atoms and the cache
payloads are typed on gh-repo-dashboard's `models.PRInfo`, so sharing either means moving
that type into `forge` and having gh-repo-dashboard alias it back. That is the same seam
the cache extraction used and it is a larger blast radius, since `models` is imported
almost everywhere.

**How comment completions get their candidates.** Completing files and symbols in a reply
needs the same index as where-used, which is the codeintel extraction. Until then,
completion is limited to files in the diff.

**Whether the context pane and side-by-side can coexist.** Both want the right half of
the screen. Current thinking is that they are alternates rather than simultaneous, with
signature detail arriving as an on-request popup the way an LSP hover does.
