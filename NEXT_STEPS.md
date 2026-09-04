# Next steps

What is open, in the order it wants doing. Why the tool is shaped the way it is lives in
[requirements.md](requirements.md), the screens and the keymap in [DESIGN.md](DESIGN.md),
and what shipped in which release in [CHANGELOG.md](CHANGELOG.md). This file holds only
what nobody has built yet, plus the decisions that are waiting on a week of use rather
than on code.

## Where it stands

Alpha is reached: a pull request is read in the terminal, comments are drafted with Claude
Code or written by hand, and the review is submitted from inside the screen against the
real API. Seen-state, the inbox, the conversation queue, the rating, the three-tab shell,
and reviewing with no checkout all landed after it.

The current goal is twenty-five reviews in one sitting: read every open pull request
waiting on me, stage a review on each, then work through the notes and the threads with
Claude Code, without opening a browser. Batching the pass over a whole queue is built.
What is left of that goal is the session shell, which is step 3 below, and the five things
that have nowhere to live until it exists.

## Open, in the order I would take them

### 1. The screen has a narrative to tell and no way to draw it

The largest thing outstanding, and it is a complaint rather than a feature: reading a
review on this screen means being a little lost in it. Every step that landed added
something true to the frame and none of them asked what the frame was becoming.

One subtraction pass is done and it bought five rows, so code starts on row 6 of the frame
where it started on row 14. That was the cheap half. What the screen should be doing
instead is telling a narrative about the change: what was done, in what order it makes
sense to read, and how the pieces relate to each other. Hunks gathered by symbol is one
relation out of several and the only one drawn.

Two things are open and neither is a design yet. What algorithm decides the narrative, and
what the visual helper for how a change maps across the filesystem looks like, since that
relation is the one nobody can see and another line of prose on a heading will not carry
it. Both want arguing with on a real review through `demo/scene.sh review` rather than
being reasoned about here.

### 2. Four renderers is three too many

`v` cycles `plain`, `rich`, `split`, and `structural`. They were built as experiments on
the promise that they get lived with on real reviews and the losers are deleted rather
than kept for symmetry. Nothing has been deleted, so every frame now has four code paths
through it and each carries a caveat in the help.

This step is the deletion. It needs a stretch of real reviews rather than a decision made
here, and the questions it answers are which one I actually reach for, whether `split`
earns being the only renderer that changes which rows exist rather than only how they are
drawn, and whether `structural` says enough to be a view rather than a heading on the
other three.

`dim_inactive` closed at two thirds on purpose and wants no more: a read hunk recedes and
a folded heading recedes, and dimming every file but one turned out to be a highlight
wearing a dim's clothing.

### 3. The session shell

The review screen is a separate program the queue hands off to, and five wanted things
have nowhere to live until it is a view inside the tabbed shell instead.

Without a resident process there is nothing for a session cutoff to be measured against,
nothing for a recently-opened list to outlive, and no boundary for a notification to fire
at. The handoff also costs what it already cost once: coming back re-runs three searches
and loses the filter, which is what made the queue's own test wait on a heading an empty
bucket draws before its search answers.

This is the largest refactor on the list and it takes away the thing every pty test and
every scene relies on, which is that the review screen runs on its own. Take it first
anyway, because the alternative is building five features against a handoff that is about
to go.

### 4. A lockfile is not a diff worth reading

A `uv.lock` or a `package-lock.json` change is hundreds of lines that say almost nothing,
and the four things worth knowing are not in them: what moved, when the new version
shipped, whether anything newer exists, and whether any of it is a known vulnerability.
So fold every lockfile hunk by default and draw a card where it was. Folding the hunk is
also what makes a comment on a lockfile placeable, since today the anchor is whichever of
four hundred lines the cursor happens to be on.

The first card is advisories and nothing else, which is the half that changes a review
decision and the half that costs one request. [OSV](https://osv.dev) answers a whole
lockfile in one POST to `/v1/querybatch`, with no key and no auth, in about 700ms.

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

### 5. Definitions and usages, which wait on codeintel

`+` and `-` grow the file's own lines around a hunk, three at a press, through
`git show <sha>:<path>` in a checkout and the contents API without one. Going from there
to where a symbol is defined and where else it is used needs an index, which is wavez's
`codeintel` and its own extraction.

Opening the whole file rather than a window around the hunk wants living with first. Three
at a press is enough for the case the expansion was built for, and a whole-file view is
closer to an editor than to a review.

### 6. Writing a comment, the half that is left

`ctrl+n` completes from what the review already holds: the files the diff touches, the
symbols the structural pass named, and the logins of everyone who has said something.
Completing every symbol in the repository rather than the ones in the diff needs the same
index step 5 does, and link completion needs somewhere for candidates to come from.

Extracting the inline editor to aragonite as `tui/editor` is a cross-repository change
rather than a feature: the editor here works, and moving it means its own check ladder, a
release, and a version bump on this side. It wants doing when a second tool needs it,
which is what would prove the shape.

Images are answered and the answer is half a yes. gh v2.99.0 carries `--attach` on
`pr comment` and `issue comment` and nowhere else, and a review goes through
`gh api .../reviews`, so there is no supported way to put an image in an inline review
comment. requirements.md carries the whole finding, the undocumented upload endpoint
included and why building on it would be a dependency that breaks silently.

### 7. The second review target: local changes and `[TODO:` markers

Scope item 2 in requirements.md, and nothing of it is built. Local uncommitted or branch
changes, with no posting endpoint, where a comment either stays local or lands in the
source as a `[TODO:` marker. It is a second mode rather than a key, which is why it has
not leaked into the pull request path.

### 8. Beyond alpha: replace gh-dash

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
server. `filter/` and `tui/table` are already named for extraction there, and the pull
request cache is the next thing that wants to move, since both tools now ask GitHub the
same questions.

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
second-look may say about a clone without duplicating gh-repo-dashboard) and each needs
step 3 first. `demo/scene.sh` opens each queue on seed data, which is where they get
argued with.

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
