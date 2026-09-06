# Diff ordering and wayfinding, September 2026

**Kyle's bot did stuff and wants it available for future bots.** Findings from an agent
research session on 2026-09-04 with minimal human review. Validate every claim before
building on it. Activity dates and version numbers come from docs and pages read at that
time and will go stale.

second-look's diff view groups hunks by symbol relationship (a hunk that declares a
symbol, gathered with the hunks that call it) rather than showing them in git's
alphabetical file order, falling back to per-directory grouping when nothing links a
hunk to another (`internal/order/order.go`). Two questions this was gathering evidence
for: once a file's hunks are split across non-adjacent groups, how does a reader keep
track of where they are in that file and how much is left, and is there real evidence
that a deterministic reordering of a diff improves review over the file order everyone
already uses.

Short answer: every wayfinding mechanism found in an existing tool assumes a file's
hunks are shown contiguously, so none of them transfers cleanly to second-look's actual
situation. The ordering question has better news: file-ordering has real, recent
(2023-2026) empirical study behind it, reviewers dislike alphabetical order, and simple
heuristics beat it. Nothing in that literature is at hunk or symbol granularity, so
second-look's specific mechanism is an extrapolation from adjacent evidence, not a
validated method.

## Wayfinding

### File lists and progress tracking

GitHub tracks a per-file "Viewed" checkbox that collapses a file once checked and clears
itself the moment the file changes again, together with a progress count of viewed files
against the total, described in its own docs on
[reviewing proposed changes in a pull request](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/reviewing-changes-in-pull-requests/reviewing-proposed-changes-in-a-pull-request).
Sticky file headers, keeping the filename visible while scrolling a long file, shipped
in 2019 per the
[GitHub changelog](https://github.blog/changelog/2019-03-18-sticky-file-headers-in-pull-requests/),
and the "Files changed" page kept being revised through
[2026-01-22](https://github.blog/changelog/2026-01-22-improved-pull-request-files-changed-page-on-by-default/).
All of it assumes the review unit is one whole file, read start to end, once.

Gerrit's file table lists every modified file with per-file status and change counts,
expandable inline, per its
[Review UI docs](https://gerrit-review.googlesource.com/Documentation/user-review-ui.html).
Position within a file is a URL line-number fragment, not a visual progress indicator;
navigation is Vim-like keybindings covered by the in-app `?` overlay
([Gerrit navigation](https://www.mediawiki.org/wiki/Gerrit/Navigation),
[vogella's tutorial](https://www.vogella.com/tutorials/Gerrit/article.html)). No
progress bar, no "percent reviewed" concept documented anywhere found.

Phabricator/Differential has been defunct since Phacility wound down on 2021-06-01. Its
own docs
([Differential User Guide](https://secure.phabricator.com/book/phabricator/article/differential/),
[Phacility product page](https://www.phacility.com/phabricator/differential/),
[LLVM's Phabricator guide](https://releases.llvm.org/14.0.0/docs/Phabricator.html))
describe side-by-side review and inline comments but say nothing concrete about
navigation or a progress UI. A claim that it had "a file browser in the left pane that
highlights the current file being viewed" turned up in search results with no traceable
primary source, so treat that specific claim as unverified.

Reviewable.io is the strongest progress model found. Its file matrix shows, per file,
per revision, per reviewer, a review-state grid, and "Show Diffs to Review" sets diff
bounds to the next range a given reviewer hasn't seen, per
[Reviewable's file docs](https://docs.reviewable.io/files.html). A shift-click marks a
file reviewed while jumping to the prior unreviewed one
([GitHub issue on that shortcut](https://github.com/Reviewable/Reviewable/issues/800)).
Even this tracks completion at file granularity: one file is one reviewable unit, shown
once.

### Diff engines and terminal tools

difftastic has no orientation aid beyond piping through a pager; a
[pager-mode request](https://github.com/Wilfred/difftastic/issues/551) and a
[pagination issue](https://github.com/Wilfred/difftastic/issues/917) are both still
open, and its own [manual](https://difftastic.wilfred.me.uk/git.html) just recommends
configuring `less`.

delta has real cross-file navigation: `n`/`N` jump between file headers or between
successive diffs in `log -p` (enabled via `--navigate`), and it draws box/line
decorations marking commit, file, and hunk boundaries, per the
[delta README](https://github.com/dandavison/delta/blob/main/README.md). It moves
between units, it doesn't track how much of one is left.

tig has chunk-level navigation (`<Enter>` searches forward to the next `^@@` chunk
header) and chunk/line/file-scoped staging, per its
[keybindings test fixture](https://github.com/jonas/tig/blob/master/test/help/all-keybindings-test.expected)
and [tigrc(5)](https://jonas.github.io/tig/doc/tigrc.5.html). No position-within-file
indicator found.

magit is the strongest prior art for structural folding as an orientation aid. Diff
hunks and files are sections in a hierarchical outline; `TAB` toggles a section, `n`/`p`
move between siblings, `M-n`/`M-p` move at the same depth, and `M-1` through `M-4` set
global fold depth, per
[Magit's Sections manual](https://docs.magit.vc/magit/Sections.html). Collapsed sections
read as "unread," expanded as "read," so the fold state itself is a progress signal, and
it doesn't require a file's hunks to sit contiguously in the underlying section tree.
That's the one design here that doesn't structurally assume contiguity, though magit's
own sections are still built from git's file order.

lazygit has hunk navigation (`h`/`l` or arrow keys) and conflict navigation within the
diff panel, per its
[keybindings doc](https://github.com/jesseduffield/lazygit/blob/master/docs/keybindings/Keybindings_en.md).
No progress indicator, minimap, or cross-file jump documented, and hunk-jump
keybindings across files are still an
[open feature request](https://github.com/jesseduffield/lazygit/issues/3558).

VS Code's minimap renders green/blue/red decorations for added/modified/deleted lines
(`scm.diffDecorationsMinimap`), with Next/Previous Change buttons and a "Collapse
Unchanged Regions" control, per its
[staging docs](https://code.visualstudio.com/docs/sourcecontrol/staging-commits).
IntelliJ's diff viewer marks changes on the scrollbar as a clickable "mini-map of the
document," with F7/Shift+F7 stepping through changes and rolling into the next file once
the current one is exhausted, per its
[diff viewer docs](https://www.jetbrains.com/help/idea/differences-viewer.html) and
[scrollbar plugin SDK page](https://plugins.jetbrains.com/docs/intellij/scrollbar.html).
Neither documents a real multi-file table of contents: both operate on one file's diff
at a time, then advance in sequence.

Across all of these, orientation rests on one of three primitives: a file list with a
viewed flag and count (GitHub, Gerrit, Reviewable), a linear next/previous stepper that
continues file to file in file order (VS Code, IntelliJ, lazygit, tig), or a foldable
outline whose fold state doubles as progress (magit). All three assume a file's hunks
are always one contiguous run, because in every one of these tools they are. None of
them was built to answer "how much of file X is left" when file X's hunks are scattered
non-adjacently through the screen, which is second-look's actual case.

### Minimaps and scrollbar annotation in a fixed-width terminal

Block-character minimaps exist for terminal editors: `code-minimap`
([wfxr/code-minimap](https://github.com/wfxr/code-minimap)) compresses a buffer to a
block-glyph overview and is the render engine behind
[minimap.vim](https://github.com/wfxr/minimap.vim). Neovim has several native
reimplementations:
[mini.map](https://github.com/nvim-mini/mini.map) shows a floating overview column with
"integrations" layered on top as colored marks (diagnostics, git diff, search matches),
which is the closest existing analog to "show where the changes are across the whole
buffer."
[codewindow.nvim](https://neovimcraft.com/plugin/gorbit99/codewindow.nvim) and
[neominimap.nvim](https://dotfyle.com/plugins/Isrothy/neominimap.nvim) do similar work,
the latter naming a diagnostics/git-changes overlay as a first-class feature.

Cheaper than a full minimap: Ratatui's built-in
[`Scrollbar` widget](https://docs.rs/ratatui/latest/ratatui/widgets/struct.Scrollbar.html)
draws a thumb over a track, and the third-party
[`tui-scrollbar`](https://crates.io/crates/tui-scrollbar) crate adds 1/8-cell fractional
thumb positioning for finer feedback than one glyph gives. Windows Terminal's "Scrollbar
Marks" feature
([tracking issue](https://github.com/microsoft/terminal/issues/11000)) is the closest
precedent to a right-margin density column: it overlays colored pips for
shell-integration prompt/command-status marks or manual bookmarks on the scrollbar
track, driven by VT sequences. It's still experimental rather than shipped, and it
marks shell prompts, not diff hunks, but the mechanism (buffer metadata rendered as
scrollbar pips) is the one that would transfer.

Bubble Tea's `bubbles` library has no annotated-scrollbar widget; its `viewport`
component tracks offsets and has a left-margin "Gutter" hook for line numbers, nothing
for a right-margin density column
([charmbracelet/bubbles](https://github.com/charmbracelet/bubbles)). This would be a
build-it-yourself widget. Textual's scrollbar is stylable via `ScrollBarRender` but no
minimap or match-density feature is documented
([textual.textualize.io/api/scrollbar](https://textual.textualize.io/api/scrollbar/)).
fzf's preview window supports a scrollbar character
([man page](https://man.archlinux.org/man/fzf.1.en)) and a match/total count, but that's
a single position indicator, not a density map. No evidence found of `bat` or
ripgrep-based pagers doing match-density gutters.

### Sticky headers and breadcrumbs

VS Code's sticky scroll pins the current scope's header lines to the top as you scroll
deeper into it, triggered by nesting (each unterminated block's opening line stays
pinned)
([explainer](https://leonardomontini.dev/sticky-scroll-vscode/),
[write-up](https://www.roboleary.net/vscode/2023/11/19/vscode-sticky)). JetBrains'
"Sticky lines" does the same, capped at 5 lines by default, clickable to jump to the
declaration
([docs](https://www.jetbrains.com/help/idea/sticky-lines.html)).
`nvim-treesitter-context` computes the same thing from Treesitter node queries rather
than indentation, memoized across cursor/scroll events
([nvim-treesitter/nvim-treesitter-context](https://github.com/nvim-treesitter/nvim-treesitter-context)).

All three are strictly nesting-triggered: the pinned header is always "what block is
the cursor lexically inside of," derived from the buffer's own contiguous structure.
None addresses a reordered view, because the trigger condition (unterminated enclosing
scope) assumes what's above the fold is the real ancestor of what's below it. In a
symbol-grouped diff the reader isn't inside a lexical scope of the file at all, they're
inside a synthetic section boundary the tool built. Generalizing this would need a
different trigger: not "what scope contains the cursor" but "which of the tool's own
groups is on screen, and what file and line range did it come from." No shipped tool or
design doing this was found. The nearest documented precedent for the underlying
tension is StickyLand, built for Jupyter notebooks, whose paper names "a mismatch
between the linear presentation of code and the non-linear process of exploratory data
analysis" directly and answers it by letting a user manually pin arbitrary cells rather
than inferring structure ([arxiv.org/abs/2202.11086](https://arxiv.org/abs/2202.11086)).
That's a different mechanism (user-pinned, not auto-computed) but it's the closest
match for "linear scroll widget applied to non-linear content." NN/g's own breadcrumb
guidance states plainly that breadcrumbs are built for hierarchical or sequential
navigation and are known to fail at representing non-linear flows
([nngroup.com/articles/breadcrumbs](https://www.nngroup.com/articles/breadcrumbs/)); no
research proposing a solved pattern for a breadcrumb in a reordered view was found, only
this acknowledged gap. Separately, sticky scroll draws real usability pushback even in
its native linear use, some users reporting it costs vertical space without earning it
([medium write-up](https://tomaszs2.medium.com/vscode-sticky-scroll-didnt-stick-with-programmers-fddf162565ce),
[VS Code issue #206915](https://github.com/microsoft/vscode/issues/206915)), a cost that
would compound in a terminal's tighter vertical budget.

### Short stable jump labels

vim-easymotion overlays a label on every candidate motion target for a one-keypress
jump. The tradeoff against it, argued in a
[Hacker News thread on Leap](https://news.ycombinator.com/item?id=33134113), is that its
many distinct motion commands force a decision about which motion is closest to the goal
before typing anything, "buying speed for cognitive load, a questionable bargain," and
require typing target keys blind while the eyes stay on the target. leap.nvim
([ggandor/leap.nvim](https://github.com/ggandor/leap.nvim)) collapses this to one
2-character search motion instead of many pre-built ones, and auto-jumps immediately
when the remaining matches fit inside a "safe" label set (keys unlikely to be needed
right after the jump), falling back to a wider, less comfortable label set only once
candidates exceed it. This is the collision-avoidance mechanism the research was after,
but the specific home-row-bias claim couldn't be verified against primary source text
(the README fetch returned only a migration stub), so treat it as reported secondhand.

flash.nvim documents an explicit "safe labels" algorithm: labels are chosen so they can
never be confused with a continuation of the search pattern itself
([flash.nvim labeler docs](https://deepwiki.com/folke/flash.nvim/6.1-custom-matchers-and-labelers)).
tmux's `display-panes` assigns short numeric labels to panes on demand, a much smaller
namespace than easymotion's since pane counts are usually single digits
([configuring pane-base-index](https://zhauniarovich.com/post/2021/2021-03-tmux/)).

No evidence was found of gh CLI, lazygit, or tig assigning short jump labels to files or
hunks; lazygit's own hunk-navigation request is still open
([lazygit#3558](https://github.com/jesseduffield/lazygit/issues/3558)), which suggests
this class of tool hasn't converged on a labeling scheme to borrow. harpoon.nvim assigns
direct-jump labels to a small, user-curated file set
(`<leader>1`..`<leader>N`, per
[miniharp.nvim's docs](https://github.com/vieitesss/miniharp.nvim)) rather than a
dynamic unbounded set, sidestepping the collision problem by capping candidate count
structurally instead of computing collision-safe labels. No documented discussion of
"labels shift position between renders" as a named critique of easymotion, hop.nvim, or
leap.nvim was found; this is a plausible real problem in a fast-scrolling or
live-updating UI, but it's unverified rather than sourced.

## Ordering that tells a story

### Change untangling and commit decomposition

This is a studied problem, but it answers which lines belong together, not what order to
read them in. Herzig and Zeller's foundational work established "tangled changes," and
later studies converge on roughly 11-40% of commits mixing unrelated concerns
([arxiv.org/html/2601.21298v1](https://arxiv.org/html/2601.21298v1),
[arxiv.org/pdf/2011.06244](https://arxiv.org/pdf/2011.06244)). The tool lineage,
EpiceaUntangler, SmartCommit, Flexeme, UTANGO, and recent LLM-based systems like
Atomizer and ColaUntangle, all optimize for clustering hunks into coherent groups,
evaluated by how well a cluster matches a ground-truth concern
([Flexeme paper](https://discovery.ucl.ac.uk/id/eprint/10107163/2/Barr_Flexeme-%20Untangling%20Commits%20Using%20Lexical%20Flows_AAM.pdf),
[SmartCommit](https://dl.acm.org/doi/abs/10.1145/3540250.3549171)). ChangeBeadsThreader
lets a human interactively split, merge, and arrange untangled clusters
([arxiv.org/pdf/2003.14086](https://arxiv.org/pdf/2003.14086)), which is the closest
thing to a reading-order artifact in this literature, but it's for tailoring the
clustering, not a claim about optimal sequence. None of the untangling papers found
propose or evaluate a within- or across-cluster reading order.

### Review order effects on defect detection

Rigby and Bird's "Convergent Contemporary Software Peer Review Practices" establishes
review-interval and reviewer-count norms across Microsoft, Google, AMD, and open source
projects, but doesn't study ordering
([Microsoft Research PDF](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/rigby2013convergent.pdf)).
Bacchelli and Bird's CodeFlow survey of 873 programmers found that change understanding
is the key aspect of reviewing, and that developers reach for ad hoc external tools to
get that understanding because the review tool doesn't support it
([ICSE 2013 PDF](https://www.microsoft.com/en-us/research/wp-content/uploads/2016/02/ICSE202013-codereview.pdf)),
a motivating finding for ordering tools generally, not a specific order study.

The directly relevant work is newer and specific to file ordering, not hunk or symbol
ordering:

- ["Assessing the Impact of File Ordering Strategies on Code Review Process"](https://arxiv.org/abs/2306.06956)
  found reviewers leave more comments on files shown earlier in the review, and that
  ordering by diff size (rather than the alphabetical default) moves files needing more
  scrutiny higher.
- ["Not One to Rule Them All: Mining Meaningful Code Review Orders From GitHub"](https://dl.acm.org/doi/10.1145/3756681.3756961)
  (23,241 PRs across 100 repos) found 44.6% of PRs are actually reviewed
  non-alphabetically, with largest-diff-first (20.6%), title/content-similarity ordering
  (17.6%), and test-before-or-after-production ordering as the recurring strategies.
- ["Breaking the Alphabet: Rethinking File Ordering in Code Review"](https://conf.researchr.org/details/icse-2026/icse-2026-research-track/310/Breaking-the-Alphabet-Rethinking-File-Ordering-in-Code-Review)
  (ICSE 2026, 1,355 developers, 182 projects) found only 10.2% think alphabetical order
  is actually best, despite it being the default almost everywhere.
- A 29-expert user study reordering files found +23% more review comments and improved
  precision (53%, +13pp) and recall (28%, +8pp) for finding the files that needed
  attention, against alphanumeric order
  ([arxiv.org/html/2404.10703v1](https://arxiv.org/html/2404.10703v1)).

The one controlled (not survey or observational) experiment closest to second-look's
actual mechanism is Baum et al., "The effects of change decomposition on code review, a
controlled experiment" (28 subjects). It found decomposition reduces wrongly reported
issues and changes how reviewers seek context, but explicitly found no effect on defect
count or on understanding the change's rationale
([PeerJ CS](https://pmc.ncbi.nlm.nih.gov/articles/PMC7924728/),
[arxiv.org/abs/1805.10978](https://arxiv.org/abs/1805.10978)). State this caveat
plainly: the strongest causal study found real but narrower effects than "more defects
found."

File-ordering research is young (2023-2026), operates at file granularity, and the
biggest causal effect (the 29-expert study) is encouraging but small-N. No paper tests
hunk- or symbol-level ordering, which is the gap second-look's mechanism sits in.

### Top-down vs bottom-up comprehension

This evidence is old (1980s-90s), thin against modern claims, and studies whole-program
comprehension by one reader over time, not diff review by someone already oriented in
the surrounding codebase. Soloway and Ehrlich's top-down "beacons and plans" model says
experts read top-down when code matches a recognized programming plan and degrades
otherwise. Pennington's competing model is bottom-up: build a program model from control
flow before building a situation model of what the code does
([summary PDF](https://www.cs.kent.edu/~jmaletic/cs69995-PC/papers/von_mayrhauser95.pdf)).
Letovsky characterizes real programmers as "opportunistic processors" mixing top-down
and bottom-up as cues appear, which suggests the dichotomy itself may be a modeling
artifact rather than how people actually read. Von Mayrhauser and Vans' integrated
model, built from protocol analysis of professional maintainers, formalizes this:
understanding is built at all abstraction levels simultaneously, with frequent
switching, not level by level
([same PDF](https://www.cs.kent.edu/~jmaletic/cs69995-PC/papers/von_mayrhauser95.pdf),
[IET summary](https://digital-library.theiet.org/doi/10.1049/sej.1995.0023)). State this
plainly: none of this was run on diff review, the studies are small, decades old, and
predate modern IDEs and review tooling entirely. "Experts read top-down from the API"
is a plausible extrapolation, not a transferred finding.

### Existing tool ordering mechanisms

Git's `-O`/`diff.orderFile` is real and documented: one shell-glob pattern per line,
matched in order, first match wins, unmatched files sorted last as if an implicit
catch-all pattern existed
([git-scm.com/docs/diff-config](https://git-scm.com/docs/diff-config),
[git-scm.com/docs/git-diff](https://git-scm.com/docs/git-diff)). Without an orderfile,
git's default is tree order, effectively a lexicographic path sort. `git diff --stat`
follows that same order, not a magnitude sort. Public gists exist purely to hack a
magnitude-sorted `--stat`, because git doesn't provide one natively
([example gist](https://gist.github.com/jakub-g/7599177)). Gerrit's own docs don't
surface a configurable review-file order in what was found; the file-ordering paper
above states as background that "popular modern code review tools like Gerrit and
GitHub sort files in alphabetical order," a claim carried secondhand through that paper
rather than sourced from Gerrit's own documentation.

### Topological or call-graph ordering, and what breaks

One real working tool does this today:
[pr-review-order](https://github.com/Goncalo-Chambel/pr-review-order) parses Python
ASTs to find import, symbol-usage, and test relationships, builds a dependency graph,
and topologically sorts it with file-type priority as a tiebreak. Its README lists
"cycle detection warnings" as a future enhancement, implying today's cycle handling is
thin or absent, and it says nothing about generated files. Two GitHub feature requests
proposed the same idea without shipping it:
[community discussion #185943](https://github.com/orgs/community/discussions/185943)
("function called in file1, defined in file2, tested in file3") and
[isaacs/github#1248](https://github.com/isaacs/github/issues/1248) ("public API first,
then supporting functions"). Neither has a substantive engineering response.
CodeRabbit's "Change Stack"
([docs](https://docs.coderabbit.ai/pr-reviews/change-stack),
[blog](https://www.coderabbit.ai/blog/introducing-change-stack-the-first-ai-native-code-review-interface))
orders "layers" so foundational changes (data shapes, contracts) come before what
depends on them, but its docs describe the outcome, not the mechanism, so whether it's a
literal topological sort of a static graph or an LLM's semantic judgment is unconfirmed,
and it says nothing about cycles or generated files either.

The strongest academic grounding is Baum, Schneider, and Bacchelli, "On the Optimal
Order of Reading Source Code Changes for Review" (ICSME 2017,
[PDF](https://sback.it/publications/icsme2017.pdf)). They formalize a "part graph" (
change parts as vertices, relations like call flow and data flow as labeled edges) and a
partial order over "tours" through it, but this is graph-based grouping via pattern
matching, not a topological sort, and it explicitly does not commit to a single global
direction for call-flow edges. Their paper states the cycle problem directly: "the
caller of a method provides information on why the callee exists and how it is used,
and the callee provides information about its pre- and postconditions," a genuinely
cyclic relationship. Their formal model handles this by fiat, demanding the graph have
no loops, which is an assumption imposed on the model rather than a property of real
call graphs. Their survey data also shows no consensus direction even where a DAG order
exists: 111 of 130 respondents preferred bottom-up in the abstract, but interview
subjects split, and a separate study (Geffen and Maoz) found real code more often
satisfies a top-down caller-before-callee order.

Real cycle detectors don't force an order through a cycle, they report it as a distinct
condition. Go rejects import cycles at compile time rather than resolving them
([Go import cycle strategies](https://www.dolthub.com/blog/2025-03-14-go-import-cycle-strategies/)).
madge (JS) uses DFS and reports the specific cycle path, and is known to miss some
cycles ([issue #447](https://github.com/pahen/madge/issues/447)). The Rocha-Thatte
algorithm for enumerating all cycles in a graph exists because detecting a cycle and
producing a total order are different problems; a real tool has to fall back to
something else (grouping, breaking the cycle at an arbitrary edge, or flagging it)
rather than silently producing a bogus order.

No prior art treats files or hunks with no recognized symbols as a distinct ordering
problem with an agreed convention. Bazel places a no-dependency target at the graph's
roots; pr-review-order's README calls a no-dependency file a "root file" reviewed
first. Neither addresses config, markdown, or data files specifically; they'd have zero
edges and end up wherever the tiebreak (alphabetical, file-type) puts them.

There's a clear convention for identifying generated code that has nothing to do with
ordering: GitHub Linguist's `linguist-generated` `.gitattributes` attribute suppresses
generated files from diffs entirely
([github-linguist/linguist](https://github.com/github-linguist/linguist)), and Go's
`// Code generated ... DO NOT EDIT` comment is machine-checked to exclude such files from
lint output. No source proposes ordering generated files first or last in a
dependency-based review order; the established pattern is to exclude them from review
outright, which is what second-look's own `Made` field and trailing `Generated` group
already does.

Whether a topological order restricted to just the changed nodes of a call graph is even
well-defined is the least-addressed question found. No tool or paper names it directly.
Baum et al.'s "part graph," built only from changed parts, is the closest analog, and
their own discussion implicitly concedes the asymmetry: a changed leaf function called
from many unchanged sites has no natural place, because its callers aren't in the graph
at all. No source quantifies this pitfall.

Bottom line: no production tool ships a call-graph-topological-sort as its default
review order today. The one attempt (pr-review-order) is small and undocumented on
cycles and generated files. The file-ordering literature studies heuristics empirically
and finds none dominates, treating dependency relations as one grouping signal among
several rather than a sortable total order, precisely because of the cycle problem.

## Gap analysis

| Question | Best coverage found | Verdict |
| --- | --- | --- |
| Track "how much of file X is left" when its hunks aren't contiguous | Reviewable's per-file revision matrix (file granularity, not hunk) | **None at hunk granularity** |
| Structural fold/outline that survives non-adjacency | magit's section tree | Partial, tree still built from git's own file order |
| Sticky header keyed to a synthetic group rather than lexical scope | None found; StickyLand's manual pinning is the nearest analog | **None** |
| Right-margin density column / minimap in a TUI | mini.map (Neovim), Windows Terminal scrollbar marks (experimental) | Exists as a pattern, not shipped for diffs |
| Short jump labels with documented collision handling | leap.nvim / flash.nvim "safe labels" | Solved for editor motions, unapplied to diff review |
| Label instability between renders as a named problem | Not found | **Unverified, plausible** |
| Hunk- or symbol-level ordering studied empirically | None; file-ordering studies (2023-2026) are the nearest evidence | **Gap**, evidence one level too coarse |
| Topological sort of a call graph as default review order | pr-review-order (small, third-party, cycle handling incomplete) | **Effectively none in production** |
| Cycle handling in dependency-ordered review | Not addressed by any review tool; general graph tools report rather than resolve | **None specific to review** |

## Recommendations for second-look

Ranked by how directly the research supports them, with a build-cost estimate.

1. **Per-file progress counter, independent of screen position.** Every wayfinding
   mechanism found (GitHub, Gerrit, Reviewable) tracks completion per file; none needs
   a file's hunks to render together to do it. second-look already knows which hunks
   belong to which file and which the reader has seen. A small always-visible line like
   "internal/order/order.go: 2 of 5 hunks seen" attached to the current group, refreshed
   as groups are visited, reuses data the tool already has. Low cost: state that already
   exists, a new render line, no parsing change.

2. **A synthetic-group sticky header naming the source file and line range.** No tool
   solves "which of the tool's own groups is this," which is squarely second-look's
   situation, but the closest analog (VS Code sticky scroll, treesitter-context) is a
   proven UI pattern once the trigger is redefined from "lexical scope" to "group
   boundary." Medium cost: needs a header row, computing which group and file/line range
   is at the top of the viewport, and a decision on whether it costs a line of vertical
   space (documented complaint against VS Code's version, worse in a terminal).

3. **A right-margin density column showing where a file's remaining hunks sit, across
   groups.** This is the one gap with a real, if unshipped, terminal precedent (Windows
   Terminal's scrollbar marks, mini.map's overlay integrations). Nothing needs building
   from scratch conceptually, but nothing in Bubble Tea's `bubbles` provides it either, so
   this is a genuine custom widget: computing per-hunk positions across the whole
   ordered plan, not just the current group, and rendering a fixed-width glyph column.
   Medium-high cost, and it only pays off if groups are visited enough that a global map
   is worth showing over the simpler per-file counter in (1).

4. **Do not build short jump labels for hunks or files.** The one clear finding here is
   a real, documented cost: easymotion-style labeling trades speed for a decision cost
   before every jump, and the one tool ecosystem that tried collapsing it (leap.nvim,
   flash.nvim) still needed a "safe label" algorithm to avoid collisions, tuned for a
   candidate set (motion targets in a buffer) much larger and more dynamic than the
   number of files or groups in one diff. No diff or review tool has adopted this
   pattern at all. Given no existing tool validates it for this exact use, and given the
   dynamic-group-count problem (second-look's groups differ per diff, so labels can't be
   memorized across sessions the way `<leader>1`-`<leader>N` can), this isn't worth
   building until the density column or progress counter prove insufficient.

5. **Leave the symbol-relationship ordering as heuristic, not a strict topological
   sort.** The one production attempt at call-graph-topological review order
   (pr-review-order) is small and admits its cycle handling is incomplete; the one
   academic model with a similar graph (Baum et al.) explicitly forbids cycles by
   assumption rather than solving them, and finds no consensus direction (bottom-up vs
   top-down) even where a DAG exists. second-look's actual mechanism, gather-and-fall-
   back-to-directory rather than a global sort, sidesteps the cycle problem entirely by
   not attempting one. That's the right call given the evidence: nothing found shows a
   strict topological sort surviving contact with mutual recursion, ungrouped
   config/data files, or a changed leaf called from forty unchanged sites. No action
   item here, this is confirmation the current design already avoided a well-documented
   trap.

6. **If pursuing hunk-level ordering evidence further, run or commission a small user
   study rather than trusting the file-ordering literature to transfer.** Every empirical
   result on ordering improving review (the 2023-2026 papers, the 29-expert study) is at
   file granularity. None of it has been tested at hunk or symbol granularity, so
   second-look's core mechanism is currently unvalidated by any published experiment.
   This is a real gap, not a small one, and the honest framing for any future writing
   about second-look's ordering is "informed by file-ordering evidence, not proven at
   this granularity."
