# Next steps

What is open, in the order it wants doing. Why the tool is shaped the way it is lives in
[requirements.md](requirements.md), the screens and the keymap in [DESIGN.md](DESIGN.md),
the motion a whole sitting follows in [FLOW.md](FLOW.md), and what shipped in which
release in [CHANGELOG.md](CHANGELOG.md). This file holds only
what nobody has built yet, plus the decisions that are waiting on a week of use rather
than on code.

## Where it stands

Alpha is reached: a pull request is read in the terminal, comments are drafted with Claude
Code or written by hand, and the review is submitted from inside the screen against the
real API. Seen-state, the inbox, the conversation queue, the rating, the three-tab shell,
and reviewing with no checkout all landed after it.

The current goal is twenty-five reviews in one sitting: read every open pull request
waiting on me, stage a review on each, then work through the notes and the threads with
Claude Code, without opening a browser. Batching the pass over a whole queue is built, and
so is the session shell: the review screen is a view inside the tabbed program rather than
a separate one, so reading a review no longer tears the queue down and rebuilds it. What is
left of that goal is the five things that had nowhere to live until the shell did, in the
open item below.

## Open, in the order I would take them

### 1. Four renderers is three too many

The renderers are three independent axes now rather than four modes: `v` walks four named
presets and `u` toggles the grammar (`ug`), the columns (`us`), and the structural pass
(`up`) one at a time, which reaches combinations the cycle cannot. Rich is the default.

The deletion this step was written for has not happened, and splitting the cycle into axes
made it easier to defer rather than easier to decide. It still needs a stretch of real
reviews rather than a decision made here, and the questions are the same: which one I
actually reach for, whether the split view earns being the only one that changes which
rows exist rather than only how they are drawn, and whether the structural pass says
enough to be a view rather than a heading on the other three.

One open question needs eyes on a truecolor terminal rather than a decision here. The rich
renderer bands a changed line at a 0.12 lightness lift and 0.60 saturation, with deeper
values on a 256-color terminal because the cube carries almost no dark tints. Whether that
band should be darker, less saturated, or left alone is a judgment nothing here can make.

`dim_inactive` closed at two thirds on purpose and wants no more: a read hunk recedes and
a folded heading recedes, and dimming every file but one turned out to be a highlight
wearing a dim's clothing.

### 2. The session shell's five things

The review screen used to be a separate program the queue handed off to; it is now a view
inside the same `tui.Shell`, so opening a row switches modes in place and leaving it
switches back to the same `*List`, filter and cursor untouched. Coming back no longer
re-runs the three searches, which is what let the queue's own tests drop the second round
they used to pay for.

Five things had nowhere to live until a resident process existed, and none of them are
built yet. [FLOW.md](FLOW.md) is what the shell is for: the whole motion from opening the
queue to posting the last review, one repository at a time. `f` focuses a repository across
all three tabs, which is what makes the two steps after it single-valued. One of those is
built: an agent records its own session with `second-look session` and `T` resumes it, so a
second hand-over reaches the agent that already read the diff. Leasing a checkout for the
focused repository is what is left, and the notification boundary still wants a source,
which `claude agents --json` has: it says which session is blocked on a prompt. The session
cutoff line and the recently-opened list are the other two, both waiting on a week of use
rather than on code (see below).

`C` (checkout) and a posted review asking for the next one staged still quit the shell's
program the way the old handoff did, since both need the terminal released or a fresh
target picked; only the read-a-row-and-come-back path moved in-process.

### 3. A lockfile is not a diff worth reading

A `uv.lock` or a `package-lock.json` change is hundreds of lines that say almost nothing,
and the four things worth knowing are not in them: what moved, when the new version
shipped, whether anything newer exists, and whether any of it is a known vulnerability.

The first of those four is built. `internal/meta` reads go.sum, go.mod, Cargo.lock,
uv.lock, package-lock.json, pnpm-lock.yaml, yarn.lock, and Gemfile.lock, so a folded
lockfile draws a table of what moved rather than a hunk count, and `za` still opens the
hashes. A format nothing here reads keeps counting hunks, because a guessed table is worse
than an honest count. Folding is also what makes a comment on a lockfile placeable, since
the anchor was otherwise whichever of four hundred lines the cursor happened to be on.

The other three all need the network. The next card is advisories and nothing else, which
is the half that changes a review decision and the half that costs one request.
[OSV](https://osv.dev) answers a whole lockfile in one POST to `/v1/querybatch`, with no
key and no auth, in about 700ms.

Version age, what the latest is, and the detail block a package new to the file deserves
are deferred on measurement: the Go module proxy is 194 bytes for a version and a release
date, PyPI is 193KB for a package, npm is 248KB and the abbreviated form that drops to
69KB also drops the `time` object, and crates.io is 441KB for `serde`. A hundred-package
bump would be a hundred requests of that size, which makes the cache by package and
version mandatory rather than an optimization and leaves the first cold card slow however
it is written.

Three things still have to be answered before this is buildable:

- What happens offline, on a private registry, or when a lookup fails. The hunk is still
  there, so the fallback is the diff and a line naming which packages could not be
  resolved, because a card that quietly omits a package is worse than no card
- Which files count as lockfiles, and whether that list is configurable
- What "popular alternatives" means. No definition exists that is not somebody's ranking,
  so it stays out until there is one I would trust in a review

This is also the first thing second-look would fetch from anywhere but GitHub, which is a
real change to what the tool is, because every request is a package name leaving the
laptop. So the fetch is opt-in per repository and says what it will query before it does.

### 4. Definitions and usages, which wait on codeintel

`+` and `-` grow the file's own lines around a hunk, three at a press, through
`git show <sha>:<path>` in a checkout and the contents API without one. Going from there
to where a symbol is defined and where else it is used needs an index, which is wavez's
`codeintel` and its own extraction.

Opening the whole file rather than a window around the hunk wants living with first. Three
at a press is enough for the case the expansion was built for, and a whole-file view is
closer to an editor than to a review.

### 5. Writing a comment, the half that is left

`ctrl+n` completes from what the review already holds: the files the diff touches, the
symbols the structural pass named, and the logins of everyone who has said something.
Completing every symbol in the repository rather than the ones in the diff needs the same
index step 4 does, and link completion needs somewhere for candidates to come from.

Extracting the inline editor to aragonite as `tui/editor` is a cross-repository change
rather than a feature: the editor here works, and moving it means its own check ladder, a
release, and a version bump on this side. It wants doing when a second tool needs it,
which is what would prove the shape.

Images are answered and the answer is half a yes. gh v2.99.0 carries `--attach` on
`pr comment` and `issue comment` and nowhere else, and a review goes through
`gh api .../reviews`, so there is no supported way to put an image in an inline review
comment. requirements.md carries the whole finding, the undocumented upload endpoint
included and why building on it would be a dependency that breaks silently.

### 6. The second review target: local changes and `[TODO:` markers

Scope item 2 in requirements.md, and nothing of it is built. Local uncommitted or branch
changes, with no posting endpoint, where a comment either stays local or lands in the
source as a `[TODO:` marker. It is a second mode rather than a key, which is why it has
not leaked into the pull request path.

### 7. Beyond alpha: replace gh-dash

[gh-dash](https://github.com/dlvhdr/gh-dash) is the bar, because it is the tool I would
otherwise open, and everything second-look does better is wasted if getting to a pull
request costs a clone and a branch switch. One screen to live in: read the queue, open any
pull request in it, review it properly, answer the conversations, post, and move on
without touching the working tree unless I mean to.

Most of that is built. What gh-dash still has that this does not is issues beside pull
requests, which waits for a real gap rather than parity for its own sake, and a preview
pane the review screen replaces with something better.

The division of labour with gh-repo-dashboard is worth restating, because two tools
reading the same data is the failure mode to avoid. gh-repo-dashboard owns disk (clones,
worktrees, branches, dirty state), second-look owns the review and the conversations, and
aragonite owns the data both read and the views both draw, so neither is the other's
server. The pull request cache already lives there and second-look already reads it;
`tui/table` is extracted too, waiting on a consumer. `filter` joined them: a generic
predicate and scoped-query package, redesigned against second-look's own free-text queue
filter rather than lifted from gh-repo-dashboard's enum-mode filters as-is, since the two
were different enough shapes that extracting one unchanged would have meant redoing the
work later. second-look's `internal/tui/filter.go` now composes it instead of carrying its
own token-splitting and case-fold matching. gh-repo-dashboard is still on its own
`internal/filters` and on aragonite v0.10.0, four minors behind: bumping it and migrating
it onto `aragonite/filter` is deliberately not done, so it stays a decision for whenever
that repository is next touched rather than a mid-air rewrite here.

### 8. The structural pass is the only thing that gets slow

[research/open-cost-2026-09.md](research/open-cost-2026-09.md) measured every stage of
opening a review, and the answer is that nothing about the diff itself needs a limit.
Parsing, building the model, laying out rows, and drawing a frame together stay under
10ms at 20,000 lines, and the frame is windowed, so it costs the same on a hundred files
as on five. The first frame would not cross 100ms until roughly 1,100 files.

The structural pass is the exception. It costs 1.2-1.9ms a hunk almost regardless of how
long the hunk is, which is the process rather than the reading, so it crosses 100ms at
around 65 hunks and reaches 155ms at 100. Raising the worker count was tried against the
benchmark and bought nothing, so the lever left is batching several hunks into one
`ast-grep` invocation.

Windowing the pass is not on the table. The reading order, move detection, cosmetic
folding, search, and the read counts all need the whole diff read before any of them can
answer, so a pass over what is on screen would give four features a different answer
depending on where the cursor was.

### 9. Blame, and a heat map of recency

Who last touched a line and how long ago is the context a diff cannot carry, and
[research/blame-and-recency-2026-09.md](research/blame-and-recency-2026-09.md) has the
evidence, the commands, and the staged plan. The signal worth drawing is age of last
change plus how many distinct authors a hunk carries. Churn is the better-supported
predictor in the literature and it is file-level evidence, so it stays out of a per-line
column until something argues for it.

It starts in aragonite because gh-repo-dashboard wants the same primitive: a `Blame`
method on `vcs.Operations`, git over `git blame --porcelain` with one `-L` per hunk range
so the cost is proportional to the review rather than to the file, and jj over
`jj file annotate`.

Two things about the jj side are settled by looking rather than by the docs. On jj 0.44.0
`file annotate` carries no line-range flag, so it annotates the whole file and the
filtering happens in Go, which the interface has to say out loud rather than pretend both
backends cost the same. It does carry `-T`, so the output is a template this side writes
and there is no format to guess at, which is better than git's porcelain. And it snapshots
the working copy unless `--ignore-working-copy` is passed, so a read-only blame call that
forgets that flag mutates the repository it was only meant to read.

What comes after the primitive: the per-hunk rollup and its cache, keyed by the old side's
blob rather than by head, because blame of an unchanged blob does not change when a push
lands elsewhere. Then `ub` for a one-column age ramp in the gutter, with a glyph per
bucket so it survives `NO_COLOR`, dropping first when the frame is too narrow. Then the
hunk header summary. Move-aware blame (`-M`/`-C`) and `.git-blame-ignore-revs` are last
and opt-in, because both cost real extra passes.

### 10. An agent session is invisible while it is working

`T` hands the todo set over and resumes the session recorded on the review, and that is
the whole of what the screen knows. It cannot say whether a session is running right now,
whether it is blocked waiting to be answered, or how to get into the conversation, so the
one thing a reviewer wants to know mid-review (is it still working, and what is it asking
me) is only answerable by leaving the screen.

`claude agents --json` answers it: it lists every session with its id, its working
directory, and a state, `blocked` among them, so matching the review's recorded
`Agent.Session` against that list is enough for an indicator and for the notification
boundary [FLOW.md](FLOW.md) wants. Two things have to be decided first.

The dispatcher is deliberately tool-agnostic, configured as argv in `config.toml`, so
probing with a Claude Code command would be the first thing here that knows which agent it
is talking to. The consistent shape is a third configured command whose output carries the
session ids, next to `dispatch` and `resume`.

Getting into the chat is the other half and it is a different act from dispatching. `T`
runs a headless command and reports one line, whereas attaching means handing the terminal
over the way the shell key already does, and coming back to a review whose comments the
agent may have rewritten underneath. `ctrl+t` already reloads rather than clobbers, so the
machinery for the return is there. What is missing is the key and the decision that
attaching is worth the handoff.

## Waiting on use rather than on code

Each of these is a decision I would rather make after a week of the queue than now, and
building any of them early encodes a guess.

**Rating a queue from the CLI.** The cost only exists where something rated that head, and
the burst needs twenty rows, so a queue of eighteen answers `rated: false` on every row and
a driver falls back to started-then-oldest. Rating on demand would be a read per row, which
is exactly what the burst threshold exists to refuse.

**Which clone an agent gets.** `internal/checkouts` already ranks the clones of a
repository by on-branch, then clean, then needs-a-stash, and it is wired only into the
threads reply path. A batch wants that ranking plus a lease so two agents do not both claim
the one clean clone. Of six clones of one repository on this laptop, one is clean, so the
real parallelism with a working tree is two rather than six.

**Whether "an agent looked and found nothing" needs a state.** A prepared review with no
comments reads the same whether it was read carefully or never opened, which is `skip`
semantics one level up. The guidance asks for a run log in the review's note instead, and
whether that is enough is worth finding out before adding a fifth status.

**The five session features.** The cutoff line that defines a session, reordering rows by
hand and holding them out of the sort with a count on the tab, a notification at a
boundary, the recently-opened list, and the checkout indicator. Each encodes a decision
(what a hand-placed row means when the rating re-sorts under it, which boundary, and what
second-look may say about a clone without duplicating gh-repo-dashboard) that the shell
existing does not settle on its own. `demo/scene.sh` opens each queue on seed data, which is
where they get argued with.

**Whether the head check should gate the first frame.** It does now: a review opened out
of the cache draws nothing but a line saying what it is waiting on until the head check
answers, because a diff staged against an older head has been read by the time the screen
admits it is the older one. The cost is the property that opening a staged review asked
the network nothing, which was the whole point of caching it. A failed check releases the
diff, so being offline costs one round trip rather than the review. Worth a week of the
queue to see whether the wait is felt.

**A release per push.** The Bump Version workflow fires on every push to main, so a
session of ten commits pushed in five batches cuts five releases. Batching the pushes is
one answer and gating the workflow is the other, and which is right depends on whether a
release is meant to mark a version or a day's work.

**The rating's weights.** They order the cases I could think of, and `internal/rate`'s
test pins the order rather than the numbers for that reason. The curve that replaced the
ceiling spread them far enough apart to tell whether one is wrong, so what is left is a
pass over a real week of the queue before anyone trusts the gap between 38 and 51.

**Blast radius as a third rating input.** requirements.md already calls it a later
addition: import graphs overcount, dynamic imports undercount, and it needs a whole-repo
scan the checkout-less path cannot promise. Cache it by base SHA if it lands.

**Whether the rating moves to aragonite.** It reads the diff, the symbol graph, and the
changed symbols, so it may belong next to `codeintel`. Extract it if a second tool wants
it and leave it here otherwise.

## Ideas with no scope yet

Each of these has been named and none has been argued through far enough to sit in the
ordered list above.

**A gh extension.** Installing with `gh extension install` needs the repository renamed to
`gh-second-look`, which is the only shape gh recognizes, and the rename takes the binary
name, the brew tap, and every link with it. Deferred deliberately rather than rejected.

**GitLab, and jj.** The forge interface exists and has been exercised once. jj already
works through aragonite's `vcs`, with one hole: `checkout` shells out to `gh pr checkout`,
which is git-only.

**Diagrams and higher-level views.** Call-graph changes, change frequency, data
structures, and what production says about the code under review. It waits on `codeintel`
for the graph and on a decision about what a review is allowed to fetch.

**Review-specific tooling, opt in and not always run.** Similarity detection, a traceback
resolved against the version of the code under review through gh-lazydispatch, semgrep, or
a check generated from what the issue tracker says the change is for. Each is a subprocess
the review screen already knows how to run, and what is missing is the rule for when one
is worth running.

**Context from TLR, and from GitHub Issues.** Lazily fetched, on the pull request under
review. It is the same shape as the checkout question: something the reader asks for
rather than something a screen fetches to open.

**Images and video.** requirements.md carries the finding: `gh --attach` covers a pull
request comment and not an inline review comment, so half of this is buildable now.
Rendering an image in the terminal (WezTerm carries the protocol) and handing a video to
the default viewer are separate and unblocked.

## Owed in both directions

**The merge has never reached GitHub.** `M` in the review screen is covered by a fake and
by nothing else, because recording it would merge a pull request and
[#2](https://github.com/KyleKing/second-look/pull/2) exists precisely because it never
merges. Proving it needs a throwaway pull request opened for the purpose.

**The queue guidance has two homes.** `second-look skill` ships the contract this
repository owns and the private `change-review` skill carries the voice rules, which is
the right split. Working a queue is now described in both, so a change to the order or the
fields wants making twice. One of them should point at the other.

**To [aragonite](https://github.com/KyleKing/aragonite):** `tui/editor`, recorded in its
README and not written. Modal editing over a text box, or a pane handing the buffer to the
user's own nvim, shared by every tool here that writes prose in a terminal. Everything
else it was owed shipped in v0.9.0.

**To [gh-sweep](https://github.com/KyleKing/gh-sweep):** nothing yet, but its `comments`
view reads unresolved review threads across a repo list through GraphQL, which second-look
does for one pull request at a time. A cross-repository unresolved-thread queue is the
natural next tab once second-look wants that scope, and gh-sweep's implementation is the
reference. `internal/ghmd` is the other candidate, written to import nothing from this
repository so it can be lifted the moment a second tool wants to segment a comment body.

**From [my_go_template](https://github.com/KyleKing/my_go_template):** `COVERDIR_SUBPROCESS`
is still absent upstream, so the `test:coverage-min` override in
`.config/mise/conf.d/user.toml` stays until it lands there. The coverage-gate anchor fix is
already backported and pushed.

## Built

Enough to answer "is that in there already", newest first. The reasoning behind any of it
is in [requirements.md](requirements.md) if it still constrains something, and in the
commit if it does not.

- The session shell: the review screen is a view inside one `tui.Shell` rather than a
  program the queue hands off to, so opening a row switches modes in place and leaving it
  returns to the same queue, filter and cursor intact, with no re-search
- `internal/tui/filter.go` composes `aragonite/filter`'s predicates and scoped-query
  tokens rather than carrying its own, and directory groups the diff falls back to sort by
  summed hunk cost the same way symbol groups already did
- Wayfinding: a piece of a split file counts the whole file, the title carries the group
  and the line once their headings scroll off, and the scrollbar track marks where the
  file's unread hunks sit off screen
- A cached review waits on the head check before drawing anything, so the diff on screen
  is one the forge has agreed still stands
- Batching a queue: `get` records the branches a pull request joins, `reviews` and its
  `--json` read a stack bottom first, and `inbox --json` carries the triage order with
  `reviewed`, `cost`, `rated`, `added`, and `removed` on every row
- The session: prefetch ahead of the cursor with no checkout, pruning of what nobody wrote
  into, and leaving a review returning to the queue
- The agent loop: `show --diff`, `context`, `todo`, a `todo` status that blocks a post,
  turns that append, batch dispatch on `T`, and reload rather than clobber with `ctrl+t`
  holding the other version
- Three tabs (`inbox`, `threads`, `reviews`) with their own cursor, filter, and scroll, and
  the conversation queue's four admission rules
- Reading with no checkout, one store per repository, and `C` moving a clone that is
  already here
- The diff: four renderers on `v`, hunks gathered by symbol across the whole diff, files
  grouped by directory, generated files collected and folded, whitespace and
  syntax-cosmetic filters on `w` and `W`, seen-state on `space` with `U` hiding what is
  read, `H` comparing against an earlier round, and `+`/`-` expanding context
- Writing: `a` with a severity, `s` for a suggestion, `V` for a range, `E` for the note,
  `!` appending a shell transcript, `ctrl+n` completion, drafts kept through an escape, and
  editing in the frame
- Posting: `S` naming what it sends, the two-part anchor guard, drift drawn at read time,
  and the artifact deleted once GitHub has it
- The rating on the review screen and ordering the queue, with the allowance guard in
  aragonite's `github.Budgets`
- `second-look skill`, the keymap grammar (`]`/`[` plus an object, `n`, `N`, `.`), folds on
  `z`, the README recording, and `demo/scene.sh` for arguing with a screen on seed data
