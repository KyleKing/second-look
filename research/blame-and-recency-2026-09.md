# Blame and recency, September 2026

**Kyle's bot did stuff and wants it available for future bots.** Findings from an agent
research session on 2026-09-06 with minimal human review. Validate every claim before
building on it. Version numbers and doc text come from pages read at that time and will
go stale.

The question this was scoping: what would "VCS blame, a recency heat map, and similar
views" mean for second-look, given that
[requirements.md's "Reviewing needs no checkout"](../requirements.md) section makes
having a working tree optional and lazy. Blame needs history, and history lives in a
checkout, not in a pull request's diff from the API. So the real question is narrower:
what can the screen show in each of the three states it already models, and what does it
say honestly when it cannot answer.

## The three states, and what blame needs

`internal/tui/tui.go` defines `Tree` as `TreeOnHead`, `TreeElsewhere`, and `TreeNone`
(`internal/tui/tui.go:26`). `TreeOnHead` is a checkout standing on the pull request's own
head. `TreeElsewhere` is a checkout of the same repository standing on something else.
`TreeNone` is no checkout of the repository on this laptop at all. `m.tree` already gates
`C` (checkout), `!` (shell), and `M` (merge) in `internal/tui/model.go` and
`internal/tui/view.go`, each refusing with a spoken reason rather than doing nothing
(`internal/tui/model.go:1567-1601`).

Blame reads history: which commit introduced a line, walking backward through parents.
The pull request's diff from the API carries the two sides of each hunk and nothing
before them, so blame is unavailable by construction in `TreeNone` and in `TreeElsewhere`
if the something-else the checkout stands on does not have the pull request's commits
reachable (a fork, an unfetched branch, a shallow clone). It is available only where a
real object database with the relevant history is on disk. That is a stronger condition
than `TreeOnHead` and weaker than "always requires TreeOnHead": a `TreeElsewhere`
checkout that has fetched the pull request's branch (its objects are there even though
the working copy stands on another branch) can blame without moving anything, because
blame reads the object database, not the working tree.

So the state that actually gates blame is not `Tree` as currently defined, it is history
reachability: does this checkout's object database contain the commits the diff's old
side belongs to. `TreeOnHead` always has it. `TreeElsewhere` sometimes has it (same
repository, branch fetched, no unreachable rewrite). `TreeNone` never has it. The screen's
existing pattern of a spoken refusal (`m.noTree()` in `internal/tui/model.go:1583-1590`)
is the right shape to reuse: a blame key that does nothing where the object is
unreachable should say "no history for this line here" rather than draw nothing, the same
way `!` says "the checkout is on another branch" rather than silently running in the
wrong tree.

What that means for the base commit specifically: even a `TreeOnHead` checkout blaming
the diff's *old* side needs the base commit's ancestry, which is present in any normal
clone (the base is an ancestor of or close to `HEAD`), but a checkout that fetched only
the pull request branch with `--depth 1` will have the new side's blob and none of its
history. `git blame` on a shallow clone attributes unreachable lines to the shallow
boundary commit rather than failing, so the honest posture is degraded output, not a hard
refusal, once at least one relevant commit is reachable.

## What aragonite's `vcs` package already has

Checked both the pinned module (`github.com/kyleking/aragonite v0.13.0`, resolved from
this repository's `go.mod`) and the local checkout at `~/Developer/kyleking/aragonite`
(git HEAD `ba611dc` on the `vcs/` path, dated 2026-09-01, ahead of what an unrelated
consumer would see only in files outside `vcs/`). Neither has blame, log-of-a-path, or
per-line history in any form. Grepping the whole aragonite tree for `blame` and
`annotate` returns nothing.

What `vcs.Operations` (`vcs/operations.go:88`) exposes today, all repository-summary and
branch-management shaped, none of it per-line or per-path:

- `GetRepoSummary`, `GetCurrentBranch`, `GetUpstream`, `GetAheadBehind`,
  `CompareBranches`: branch state and divergence counts
- `GetStagedCount`, `GetUnstagedCount`, `GetUntrackedCount`, `GetConflictedCount`:
  working-tree status counts
- `GetBranchList`, `GetStashList`, `GetWorktreeList`: enumeration
- `GetCommitLog(ctx, repoPath, count)`: the *repository's* most recent commits, not
  scoped to a path or a line range. This is the nearest existing thing to "log," and it
  answers a different question (what happened recently, repo-wide) than blame does (who
  touched this specific line)
- `GetNewestModifiedFile`, `GetLastModified`: mtime-based, not commit-based
- `StashDiff`, `StashDiffstat`, `UncommittedDiff`, `UncommittedDiffstat`,
  `StashDiffExternal`, `UncommittedDiffExternal`: diff text for uncommitted or stashed
  work, not history
- `FetchAll`, `PruneRemote`, `PushBranch`, `SwitchBranch`, `DeleteBranch`,
  `ApplyStash`, `DropStash`, `CleanupMergedBranches`: mutators
- `RemoteBranches`, `ResolveDefaultBranch`, `DefaultBranchHead` (`vcs/refs.go`,
  `vcs/git.go:972`): ref resolution
- `Stamp` (`vcs/stamp.go:24`): a fast, non-shelling-out fingerprint of HEAD, upstream,
  and tracking state read directly from `.git`, used for change detection rather than
  content
- `CheckoutIdentity`, `RemoteIdentity`, `RemoteIdentityFor` (`vcs/identity.go`): which
  repository a directory or remote URL names
- `cachedBranchList`, `cachedCommitLog`, `stamped` (`vcs/cached.go`): the TTL-cache
  wrapping pattern the package already uses for its own expensive calls, which is the
  pattern a blame cache should follow rather than inventing a new one

Both `GitOperations` and `JJOperations` implement the same `Operations` interface
(`vcs/git.go`, `vcs/jj.go`), so a blame method added to the interface has to be
implemented for both backends, matching the pattern every other method in the interface
already follows.

### Where a blame call belongs

Both gh-repo-dashboard and second-look would want blame (gh-repo-dashboard already shows
per-file recency in its dashboard rows via `GetNewestModifiedFile`/`GetLastModified`, and
a heat map is the same shape of question at finer grain), so this is an `aragonite/vcs`
addition, not a second-look-only one, matching `requirements.md`'s own rule for what
moves to aragonite ("the second consumer needs them"). Concretely:

- Add `Blame(ctx, repoPath, path string, ranges []LineRange) ([]BlameLine, error)` to
  `Operations`, implemented in `git.go` over `git blame --porcelain -L a,b -L c,d ...`
  (one call, multiple `-L` ranges, see below) and in `jj.go` over `jj file annotate`.
  Line-range scoping is what keeps this cheap for a review, covered in the cost section.
- `BlameLine` is a new type in `vcs/types.go`: commit id (or jj change id), author name
  and email, author time, and the line's number in the file being annotated. Kept small
  and forge-agnostic, the same way `CommitInfo` already is.
- A cached wrapper following `vcs/cached.go`'s existing `stamped`/`cachedCommitLog`
  pattern, keyed by (repo path, file path, blob or content hash, ranges) rather than by
  wall-clock TTL, because blame of an unchanged blob at an unchanged history never goes
  stale until the tree moves.
- Second-look's own `internal/rate` or a new `internal/blame` package (second-look side)
  converts `[]BlameLine` into the per-hunk summary the screen renders (age of the newest
  line, distinct author count, distinct commit count within the hunk) and owns the
  caching under `.second-look/`, the way `internal/artifact/contextcache.go` already
  caches other head-keyed derived data. The raw git/jj call stays in aragonite because
  it is a VCS primitive. The review-specific rollup and its cache key stay in
  second-look because nothing else needs that shape.

## The concrete commands

### git

`git blame --porcelain` and `--line-porcelain` are both documented in
[git-blame(1)](https://git-scm.com/docs/git-blame). `--porcelain` emits, per group of
contiguous lines from one commit, a header line of `<sha> <source-line> <result-line>
<num-lines-in-group>`, then (the first time that commit appears) `author`, `author-mail`,
`author-time`, `author-tz`, equivalent `committer-*` fields, `summary`, and `filename`,
then the line text itself prefixed by a tab. `--line-porcelain` repeats the full commit
block on every line instead of only the first occurrence of a commit, trading output size
for a parser that never has to remember state across lines. For second-look's per-hunk
rollup (author, age, distinct-commit count), plain `--porcelain` is enough and cheaper to
parse, since the rollup only needs to see a commit's metadata once per hunk.

`--incremental`, also in the same man page, streams entries as they are computed rather
than waiting for the whole file, with more-recent commits typically appearing first, which
is meant for interactive viewers watching a blame come in. Second-look's use case is
committing to reading a bounded set of hunk line ranges and using the *result*, not
displaying a spinner while a big file blames, so plain (non-incremental) `--porcelain` is
the right mode. Incremental mode only pays off if the tool ever blames a whole file
on demand (the "who wrote this function" popup a context pane might offer later), where
showing partial results while a large file finishes is a real UX improvement.

`-L <start>,<end>` restricts blame to a line range and can be given multiple times, with
overlapping ranges allowed (same man page). This is the mechanism that keeps blame
proportional to review size rather than file size: `git blame -L 14,22 -L 88,95 -- path`
answers only the lines a diff's hunks (plus whatever context padding is wanted) actually
touch, in one process, rather than blaming the whole file and discarding the rest.

`-M[<num>]` detects lines moved or copied within the same file, defaulting to a 20
alphanumeric-character floor before Git considers a block a move. `-C[<num>]` extends this
across files modified in the same commit (default 40-character floor), with `-C` given
twice or three times widening the search to the file's creating commit or to any commit
respectively. These matter for a heat map or blame overlay because without them, a
function relocated by a refactor blames to the commit that moved it rather than to
whoever last touched its logic, which is exactly the kind of attribution error
`git diff --color-moved`'s move markers already work around elsewhere in second-look's
own diff view (`design.md`'s `▸ moved from util.go:88` marker). `-M`/`-C` cost real time
(they run extra inspection passes per the man page's own wording) so they belong behind
a flag or a size cutoff rather than being on by default for every blame call.

`--since=<time>` or a revision range (`git blame v2.6.18.. -- foo`) stops attribution at a
boundary, blaming everything not touched since then to the boundary commit itself, which
is one way to answer "how old is this" cheaply if the rollup only needs an age bucket
rather than an exact commit.

A file `.git-blame-ignore-revs` at the repository root (one unabbreviated SHA per line,
`#`-comments allowed) combined with `git blame --ignore-revs-file <path>` or the
`blame.ignoreRevsFile` config skips listed commits, which is how projects keep a mass
reformat or a mechanical rename from attributing every line to that commit
([madewithlove's write-up](https://madewithlove.com/blog/ignoring-revisions-when-using-git-blame/),
confirmed against
[git-blame(1)](https://git-scm.com/docs/git-blame)). Second-look already treats
whitespace-only changes as a separate category in its review-cost rating
(`requirements.md`'s "Review cost" section excludes them before rating), so respecting an
existing `.git-blame-ignore-revs` file the repository already maintains, rather than
inventing a second reformat-detection mechanism, is the consistent choice if this ever
becomes a real gap.

### jj

[`jj file annotate`](http://docs.jj-vcs.dev/latest/cli-reference/) (`jj file annotate
<PATH>`, `-r`/`--revision` to pick a starting revision, `-T`/`--template` to control the
rendered line) "annotates a revision line by line" and reports "the source change that
introduced the associated line" per line. It tracks jj's change id, not a git commit
hash, which fits jj's model where a change survives being amended and rebased under a
stable id while its underlying commit changes. The documentation found does not mention
a `-L`-equivalent line-range flag or explicit rename/copy-following behavior, unlike
git's `-M`/`-C`. That is a real capability gap relative to git blame: scoping to just a
diff's touched lines and getting move-aware attribution both look like they would need
either an upstream jj feature or client-side post-filtering (annotate the whole file, then
keep only the lines the hunk touches), which loses the cost benefit `-L` gives git.
Confirm both absences against `jj file annotate --help` on the version actually pinned,
since the docs site content could not be checked as thoroughly as `git-blame`'s own man
page. That is exactly the kind of gap description that goes stale as jj adds features.

Because `vcs.Operations` already treats git and jj as one interface with different
implementations (`git.go` and `jj.go` both implementing the same methods,
`vcs/factory.go:37`'s `GetOperations` picking the right one by `DetectVCSType`), a
`Blame` method's git-side line-range scoping and jj-side lack of one is a real asymmetry
the interface has to document rather than hide: the jj implementation either blames the
whole file and filters client-side (correct, slower) or the interface admits it cannot
promise the same cost bound on both backends.

## Cost

For git, the cost that matters is process count and per-line work inside one process, not
which specific flags are set. `-L` scoping is what keeps both bounded to hunk size:

- **Per file**: one `git blame --porcelain -L a,b -L c,d ...` process, all of a file's
  touched ranges in one call (the man page confirms `-L` may be given multiple times with
  overlapping ranges allowed), rather than one process per hunk. Process startup
  (fork/exec, opening the object database) dominates for small ranges, so batching
  ranges into one call per file is the real saving, not the blame algorithm itself, which
  is near-linear in the number of lines actually requested once `-L` limits the walk.
- **Per review**: one process per changed, non-generated file that has at least one
  surviving (non-whitespace-only) hunk, which is the same file set second-look's review
  rating already restricts itself to (`requirements.md`'s "Review cost" section excludes
  whitespace-only changes and reads patch text, not a working copy, for the same reason).
  A typical reviewed PR in this project's own history touches single-digit files, so this
  is single-digit processes, not proportional to repository size.
- **What's expensive and avoidable**: blaming a whole file when only a few lines changed,
  and turning on `-M`/`-C` unconditionally, since both cost real extra passes per the man
  page. Both are avoidable by construction if line ranges always come from the diff's own
  hunks and move detection is opt-in (a keybinding or a size cutoff) rather than default.
- **What to cache and by what key**: per (file path, blob SHA of the *old* side, ranges
  requested), following the existing pattern in `internal/artifact/diffcache.go` and
  `contextcache.go`, both keyed by head SHA under `.second-look/`. Blame of an unchanged
  blob never changes as long as the history behind it doesn't move, so the cache key is
  the blob content, not the head commit: a rebase that doesn't touch a file's blob should
  still hit the cache, whereas keying by head SHA the way diff and threads are keyed would
  invalidate on every push even where nothing blame-relevant changed. This is a real
  divergence from the existing cache shape and worth calling out rather than copying
  blindly: `DiffPath`/`ThreadsPath` are keyed by head because the diff and threads
  genuinely are functions of the head. Blame of an already-committed line is a function of
  the blob, which is a coarser and more stable key.

## The heat map: what "recency" should mean

Four candidate signals, and what evidence supports each:

1. **Age of last change to a line.** The most literal reading of "recency," directly what
   `git blame`'s `author-time` gives per line at zero extra cost once blame runs at all.
   Hassan and Holt's "top ten list" work
   ([ResearchGate](https://www.researchgate.net/publication/4175861_The_top_ten_list_Dynamic_fault_prediction),
   [primary PDF](https://research.cs.queensu.ca/home/ahmed/home/pubs/icsm2005.pdf))
   validated the heuristic "recently changed files tend to be buggy" and "recently
   bug-fixed files tend to be buggy" against six open-source systems (FreeBSD, KDE,
   KOffice, NetBSD, OpenBSD, PostgreSQL) and found recently changed and recently fixed
   files the most fault-prone, though a later re-evaluation found the heuristics'
   predictive performance declined or stayed flat over time as the tracked subsystems
   aged out of active development, which is a caveat about heuristic staleness rather
   than about the signal being wrong. This evidence is at file granularity, not line
   granularity: nothing found tests whether "this specific line was touched two days
   ago" predicts a defect in that line any better than file-level recency does.

2. **Number of distinct commits touching a line/region (churn).** Nagappan and Ball's
   "Use of relative code churn measures to predict system defect density"
   ([ICSE 2005, ACM record](https://doi.org/10.1145/1062455.1062514),
   [Microsoft Research copy](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/icse05churn.pdf))
   is the foundational citation here: absolute churn (raw lines added/deleted/changed) was
   a poor predictor of defect density, while *relative* churn (churn normalized against
   file size or total lines) was highly predictive, with a churn-metric suite reported to
   discriminate fault-prone from non-fault-prone binaries at roughly 89% accuracy in that
   study. The PDF's own text could not be parsed directly in this session (it returned as
   compressed binary to a fetch tool), so this is carried from search-result
   summaries and the paper's public abstract rather than a direct read of the full text.
   Flag the exact 89% figure and the specific metric list as needing a direct read before
   quoting them anywhere more binding than this note. This evidence is at the
   file/module level, same caveat as above: nothing found tests line- or hunk-level
   churn specifically.

3. **Number of distinct authors.** Bird, Nagappan, and colleagues' "Don't Touch My Code!
   Examining the Effects of Ownership on Software Quality"
   ([Microsoft Research PDF](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/bird2011dtm.pdf))
   studied Windows Vista and Windows 7 and found that the number of low-expertise
   ("minor") contributors to a component correlated with pre- and post-release failures
   more strongly than any other ownership metric Microsoft tracked at the time, and that
   defect-prediction models built only from minor contributions outperformed models built
   from major contributions, to a statistically significant degree. This is the strongest
   single piece of evidence found for any of the four candidates, but it is
   component/binary granularity (a Windows sense of "component," closer to a package than
   a file), not line or hunk granularity, and it is ownership-of-a-component rather than
   authors-of-a-specific-line.

4. **Churn over a window** (e.g., commits touching a region in the last N months,
   decayed). No source found studies this specifically as distinct from plain churn
   count. It reads as a plausible refinement of (2) rather than a separately validated
   signal.

**Where the evidence is thin, stated plainly:** every citation above operates at file,
module, or component granularity. None of the four sources tests line-level or
hunk-level recency, churn, or authorship against defect outcomes. A heat map painted
per-line is therefore an extrapolation from adjacent, real evidence, not a validated
method, in exactly the same shape as `diff-ordering-2026-09.md`'s finding that
file-ordering evidence doesn't transfer cleanly to hunk-level ordering. Say so if this
ships: "informed by file- and component-level defect research, not validated at line
granularity" is the honest framing, not "recency predicts defects here."

Given the evidence available, if second-look builds one heat-map signal rather than
several, distinct-author count (candidate 3) has the single strongest causal-flavored
result behind it (Bird et al.'s minor-contributor finding), and age of last change
(candidate 1) is the cheapest to compute and explain, coming for free out of the same
blame call. Recommend leading with age as the always-on signal (cheap, literal, matches
what "recency" means in plain language) and treating distinct-author count as a second,
optional overlay rather than trying to fuse both into one score, because Bird et al.'s
signal is about component ownership over a project's life, not about a single line's
edit history, and collapsing it to "how many authors touched this line" is a bigger
extrapolation than either signal is on its own.

## UI proposal

The existing gutter (`internal/tui/render.go:278`, `gutterWidth() = numWidth*2 +
signRuleAndSpaces` in unified view, `numWidth + signAndSpaces` per side in split view,
`render.go:635`) already carries two line numbers, a sign, and a rule. `internal/tui/
keymap.go` already has the `u` chord as the place independent draw toggles live: `u`
then `g` (grammar), `s` (side by side), `p` (parser), listed at
`internal/tui/keymap.go:223`. Blame and recency belong there as more of the same
grammar, not as a new pane:

- **`u` then `b`: toggle a blame column.** One more glyph column at the gutter's right
  edge, between the existing gutter and the code, showing the age of the line's last
  change as a single character on a fixed ramp (say, five buckets: today, this week, this
  month, this year, older), the same idea as the existing band/mark two-depth ramp
  already used for add/remove color (`render.go`'s `depths`/`blend`). One column, not a
  name: a name needs enough width to be legible (`kyleking` is 8 columns) and repeats on
  every line of a run, which is exactly the "band that says nothing extra past the first
  line" problem the code already solves for add/remove kind by only bothering to draw a
  full band, not per-line text. Author identity, if ever wanted, is better placed at the
  hunk header (next to the existing `▸ moved from util.go:88` marker slot in
  `design.md`'s mock) as one line naming the newest author and the number of distinct
  authors in the hunk, rather than repeated per line.
- **Cost in width**: one column, one cell wide, using the same glyph-plus-color pattern
  the rest of the screen already commits to for `NO_COLOR` and 16-color survival (see
  degradation below). At 80 columns with a typical `numWidth` of 3-4 and the existing
  gutter already at roughly 9-11 columns in unified view, one more column is a five to
  ten percent tax on the frame width, which is the same order of magnitude the context
  pane and side-by-side view already accept as a real cost per `DESIGN.md`'s
  degradation rules.
- **Where the per-hunk rollup goes**: the existing hunk-header right margin, which
  `design.md`'s mock already reserves for move provenance (`▸ moved from util.go:88`).
  Add "N authors, oldest change Xd ago" there rather than opening a new pane, matching
  the instruction to prefer the gutter or a margin over a new full-screen view: a hunk
  header is read once per hunk already, which is the right frequency for a summary
  fact, whereas a per-line blame column is read continuously while scanning code, which
  is the right frequency for the cheap single-glyph signal.
- **Degradation at 80 columns**: the blame column is the first thing to drop, before the
  gutter or the code, the same priority order `DESIGN.md`'s degradation section already
  gives the context pane and side-by-side (both collapse below their minimum width,
  leaving the diff as the one thing that must always render). A screen at exactly 80
  columns with side-by-side or the context pane already open has no room for a third
  optional column, so `u` then `b` should refuse or auto-collapse rather than
  squeezing the diff.
- **`NO_COLOR`**: the age ramp needs a glyph per bucket independent of color, the same
  rule seen-state and resolved-state and move markers already follow (`DESIGN.md`'s
  degradation section: "nothing depends on color alone"). Five buckets map cleanly to
  five ramp characters (existing prior art: block-density glyphs like `░▒▓█` plus a
  blank for "today"), reusing exactly the kind of glyph-ramp already discussed as a
  precedent for a right-margin density column in `diff-ordering-2026-09.md`'s
  recommendation 3, which is unshipped there for the same reason it would be new work
  here: nothing in Bubble Tea's `bubbles` library provides a ramp or density-column
  widget (confirmed in that research), so this is custom rendering code either way.
- **Why not a new full-screen view**: the existing context pane is already the
  alternate-right-half slot (`DESIGN.md`'s "Context pane" section, occupying the same
  half side-by-side would use), and it is explicitly reserved for where-used, blast
  radius, and symbol-graph diagrams once `codeintel` exists. Blame is a per-line fact
  about code already on screen, which is what a gutter is for. Recency is a per-hunk
  summary, which is what the hunk header margin is for. Neither needs the screen space
  a whole-file history view (a `git log -p` equivalent) would need, and building that
  view would duplicate the "commit list" DESIGN.md already ruled out ("Seen-state fully
  replaces per-commit browsing. No commit list until it proves necessary").

## Staged build plan

1. **`vcs.Blame` in aragonite, git only, no cache, no UI.** Add the method to
   `Operations`, implement it in `git.go` over `git blame --porcelain -L ...`
   (no `-M`/`-C` yet), return `[]BlameLine`. `JJOperations.Blame` can return a documented
   "not yet supported" error rather than blocking on `jj file annotate`'s missing
   line-range flag. Testable with no network: `vcs`'s existing tests already build real
   git repositories in a temp directory and run real commands
   (`vcs/git_integration_test.go`, `vcs/git_exec_test.go`), so a blame test adds commits
   to a fixture repo and asserts on the parsed output, the same shape as the package's
   existing tests.
2. **A second-look-side rollup package** (`internal/blame`, following the one-package-
   one-purpose rule) that takes a prepared review's diff, extracts the hunk line ranges
   per file (this data already exists wherever `internal/diff` builds the `Hunk` type
   the renderer consumes), calls `vcs.Blame` once per file with all its ranges batched,
   and produces the per-hunk age/author-count summary. Cache it under `.second-look/`
   keyed by (file path, old-side blob SHA), following `contextcache.go`'s pattern but
   with the blob-keyed cache key argued above rather than head-SHA keying. Testable with
   no network, same fixture-repo pattern as (1), plus a fixture review artifact.
3. **The `u` then `b` toggle and the gutter column**, reading (2)'s cached rollup when
   `m.tree` can reach the relevant history (`TreeOnHead` always, `TreeElsewhere` only if
   the blame call itself succeeds, which is the honest way to answer "is history
   reachable" without duplicating that logic) and showing the spoken refusal pattern
   (`m.say(...)`, matching `m.noTree()`'s existing wording style) when it cannot.
   Testable with `teatest` golden frames at 80 and wider columns, `NO_COLOR` on and off,
   the same harness `internal/tui/testdata/TestFrames/` already uses.
4. **The hunk-header summary line** ("N authors, oldest Xd ago"), once (2) and (3) prove
   the per-line column is worth the width in practice. This is additive to the same data,
   not a new fetch.
5. **`-M`/`-C` move detection and `.git-blame-ignore-revs` respect**, gated behind a flag
   or a file-size cutoff given their documented extra cost, once the plain version has
   been used enough to know whether misattributed moved code is a real complaint or a
   theoretical one.
6. **jj support for `Blame`**, once `jj file annotate`'s actual flags (checked against the
   pinned jj version's own `--help`, not just the docs site) are confirmed to lack or
   carry line-range scoping. If it truly lacks `-L`, the implementation blames the whole
   file and filters client-side, which is slower but correct, and the interface's doc
   comment says so rather than pretending both backends cost the same.

Not staged, because nothing above argues for it: a whole-file or whole-repository
history browser, an interactive "walk back through blame" feature, or any signal beyond
age and author count for the initial heat map. Each would be new evidence to gather and
new screen space to justify, not a natural extension of steps 1-4.
