# Next steps

What alpha needs, in the order it wants doing. Scope and priorities live in
[requirements.md](requirements.md), screens in [DESIGN.md](DESIGN.md).

## What alpha means

I can review one of my own pull requests end to end and post it, reading the diff in the
TUI rather than in `$EDITOR`. No seen-state, no inbox, no rating.

The TUI is in scope because reviewing a diff through a text editor on a TOML file is the
experience this tool exists to replace. A CLI-only alpha would prove the plumbing and
none of the premise.

Concretely, this works and nothing in it is faked:

```sh
gh pr checkout 42         # or second-look get 42, which also caches the diff
# claude drafts comments through the change-review skill
second-look 42            # read the diff, triage comments, submit with a key
second-look               # the same, for whatever this branch belongs to
```

Reached. The submit from inside the screen is driven on a real pty against the recorded
gh, and the requests it makes are the ones that posted the review on
[#2](https://github.com/KyleKing/second-look/pull/2) for real; it is the one step nobody
has run against live GitHub from inside the screen rather than from the shell. Seen-state,
the inbox, the rating, and writing a comment without an agent all landed, so nothing alpha
named is outstanding.

## The next goal: twenty-five reviews, locally

Read every one of my twenty-five open pull requests in the terminal, stage a review on
each, then work with Claude Code to implement the notes and answer the threads that need
a human, without opening a browser. The steps below are in the order they want doing and
each names what it depends on. The scratch list of gripes I was keeping is folded into
them and deleted.

The order comes from where the friction is rather than from what is architecturally
tidy. Reading the diff comes first, in two halves (how a change is drawn, then what
order the changes arrive in), because it is the part I dislike today and every other step
is spent looking at it. The store move follows because it is small and three later steps need it.
The agent loop is next because it is the second half of the goal, and the session after
that, because it is what turns twenty-five reviews into one sitting.

### 1. The diff is unpleasant to read

A changed line is whole-line green or red with no syntax highlighting and no word-level
marking (`internal/tui/styles.go`), so a one-word edit reads as two entirely changed
lines and nothing tells Go from YAML. [delta](https://github.com/dandavison/delta) and
[hunk](https://www.hunk.dev/) both treat highlighting and intra-line marking as the
floor, and this is below it.

Three renderers landed behind `v`, which cycles `plain` (what shipped before), `rich`,
`split`, and `structural`. They are experiments: rough edges are fine, each caveat is
written into the help and the README and named in the footer on the keystroke that
switches to it, all of them get lived with on real reviews, and the losers are deleted
rather than kept for symmetry.

**rich** is built: highlighting per grammar, word-level intra-line marking, a background
band instead of colored text, and a gutter carrying the old and new line numbers together.
Highlighting is [chroma](https://github.com/alecthomas/chroma), which is pure Go and so
keeps `CGO_ENABLED=0` across the ten-platform matrix, unlike every tree-sitter binding.

Two things it turned up. The pairing decides what to mark by how much of the two lines is
shared, and measuring that against the longer line left a line that gained a clause
unmarked, so it is measured against their mean instead. And a band mixed from the accent
and the background lands in the grey ramp, where a 256-color terminal quantizes added and
removed to the same slot and the band stops saying anything: it is the accent's hue at the
background's depth now, drawn deeper where the terminal cannot mix its own colors. tmux
reports 256 colors on a laptop that has millions, so that is the common case rather than
the odd one.

**split** is built: side by side, each removal beside the addition that replaced it, with
a unified fallback below 120 columns rather than two columns of code wrapped into
illegibility. Each half numbers the file it is showing, which for a context line means two
different numbers the moment anything above it was added or removed.

Where the context pane goes is settled: the split wins the frame and context opens over it
as a modal, closing on escape, so the two are never both asking for the right half. It is
the only renderer that changes which rows exist rather than only how they are drawn, which
is why it applies to the diff and not to the code and comment views, and why `v` lays the
screen out again rather than merely redrawing it. Pairing a whole review costs the resize
path about 20% (1.5ms to 1.8ms on a forty-file diff); the per-keystroke frame is
unchanged.

**structural** is built, and reuses the ast-grep pass the rating already runs, so it costs
no extra work: the symbols each hunk touched on its heading, a per-file list of the same
under the file name, and a symbol that left one hunk and arrived in another drawn as a
move rather than as a delete beside an unrelated insert. A symbol whose declaration was
rewritten on the way is not a move, since it is not the same code arriving somewhere else.

A hunk is a fragment, so the symbol a body edit sits inside is not knowable, which is the
caveat this one ships with. What the pass now hands back that it did not before is the
symbol names themselves rather than only the strongest kind, which is what step 2 needs to
co-locate hunks.

The faces are nightfox's, derived into aragonite's palette rather than exposed as
configuration, so second-look, gh-repo-dashboard, and gh-sweep stay one visual family.
`dim_inactive` was the idea worth taking from that config, and half of it is done: a hunk
already marked read recedes in both renderers, keeping its band and losing its grammar,
since what the code says has been read and what is left to read is the question. The
folded region and the file the cursor is not in are the other half, and both want living
with before they are built: dimming every file but one is most of a long diff most of the
time, which is a different thing from dimming a window nobody is typing in.

Folded in here because every one of them is a rendering problem, and both are waiting on
a decision rather than on code. `demo/scene.sh comment-run` and `demo/scene.sh skipped`
open each as a live review off a TOML file to edit, which is how the shape gets argued
with:

- Several comments on one line do not break the layout, and the three things about a run
  of them that read badly are all fixed. A run opens on a heading naming how many
  comments it holds and which line they answer, and each one is numbered inside it, so
  two blocks separated by a blank row no longer read as comments on consecutive lines.
  The line the run answers is pinned to the top of the frame while the run is read past
  it, at the contrast of a hunk already read, which is the first pinned row this screen
  has and costs a row only while it is needed. And a skip inside a run gathers below the
  live comments as one row saying how many, opened with `za`
- A run is the case that breaks the settled decision about a skipped comment, which is
  recorded below: it draws as an ordinary comment carrying its body and note, marked
  skipped, everywhere except the comment view. The decision stands wherever a line
  carries fewer comments than read as a run, since a lone skip drawn as a count would
  hide the only thing said about the line

One keymap fix stood alone rather than folding in, and is done: `dd` on the third staged
review armed the first, because `moved` set `touched` on every keypress and cleared it
again for anything that was not a motion, so the rebuild every action runs sent the cursor
back to the top. Getting back to where you were was `djjd`. It cost every row verb the
same way, not only the discard.

### 2. What order the diff arrives in — done

Files group by directory today and the diff's own order is kept inside a group, which is
the right default and is not the whole of it. What a review actually wants is the change
read in the order it makes sense in, and three things stop that.

Hunks are gathered by symbol now, across the whole diff rather than within a file: a
group is a symbol some hunk declares together with every hunk that calls it, so the callee
whose signature moved sits next to the caller that has to change with it whatever
directories the two came from. Everything no symbol gathered keeps the directory grouping
it always had, and `O` puts the diff's own order back, which is both the way out when the
gathering guesses wrong and the way to consult what the forge thought the order should be.

Two things the pass had to start answering. A call is spelled without the keyword that
introduced the declaration it reaches, so a symbol carries the bare identifier beside its
name. And a hunk whose body changed inside an untouched declaration reports no symbol at
all, which is right for saying what a hunk did and useless for saying what it is inside,
so the pass reports the declarations a fragment shows as well as the ones it changed.

The guard on all of it is that a name is matched rather than resolved. A name more than
one hunk declares is `New` or `Error`, and gathering on it would put half a diff under a
heading that lies, so a symbol worth gathering around is one the diff declares in one
place.

A file split across two groups says so on its heading, with `]f` walking to the rest,
because a reader who does not know they are seeing half a file draws the wrong conclusion
from it.

Generated and machine-authored files no longer sit in the reading order as if they were
code: they are collected into one group at the end however many directories they came
from, folded, and counted, with the heading saying how many files and hunks it holds.
`generated` in `config.toml` adds to the built-in patterns rather than replacing them,
because a monorepo naming its own generated tree does not stop a lockfile being one. They
are the one thing on the screen that starts folded, so `za` opens rather than closes.

The card that replaces the diff, rather than only the count, is the lockfile section
below.

The rating earns its second use here: the gathered groups are ordered by what their
declaring hunk costs on the same weights the whole change is scored on, so a signature
change is read before a renamed local, and equal costs keep the order the diff declared
them in so two opens of one review agree. Every gathered group comes before every
directory group, which is the part worth arguing with: a group exists because something
structural was found in it, and that is the claim being made about it.

An unparsed hunk costs nothing and sorts as cheap. That is the honest answer rather than a
convenient one: it is a hunk nobody can rank, and putting it first on a guess would be
worse than leaving it where the diff had it. The per-hunk cost carries no ceiling, since
what matters is which of two hunks is dearer and the ceiling exists to keep a number
comparable between pull requests.

### 3. The score that always says 99 — done

The weights were never the problem. A signature change at 40 plus two capability classes
at 25 each already passed the 99 ceiling, and the ceiling was a `min`, so every change
carrying that much or more rated exactly 99 and the number ranked nothing above itself.

The sum is bent onto the scale now rather than clipped: the same weights, mapped through
a curve that approaches 99 without reaching it. It is strictly increasing, so two changes
that differ at all still rate differently, and the order `internal/rate`'s test pins is
untouched because a monotonic map cannot change an order. What it changes is that the top
of the range is usable: one hunk of a body rates 2, thirty rate 39, a symbol added 19, one
new capability 35, a signature 49, a signature with two capabilities 78, and the same
again across forty hunks 88.

The weights themselves still want a real week of the queue behind them, which is what
requirements.md has been waiting for. That is a different pass and this one no longer
blocks it: the numbers now spread far enough apart to tell whether a weight is wrong.

The cache carries the scale it was written on, and a file from another one is dropped
rather than read. A queue ordering a mix of two scales orders wrongly and says nothing
about it.

### 4. One store, one identity — done

Everything second-look keeps (the review, the read marks, the thread cache, the diff
cache, the ratings) lives under the state directory keyed by host, owner, and repository,
with the number in the filename it always had. Reading #42 from `../irm-0-null` and later
from `../irm-5-five` is one review, where it used to be two, which is what made an
incremental re-review across directories impossible.

The seam was already in the right place: every path in the codebase is built from a root
plus `.second-look/`, and a target already carried a store beside its working copy, so
what changed is which root a checkout answers with. What a checkout still holds is moved
into the store the first time that review is opened, from `Here`, from `Staged`, and from
`reviews`, and a `WHERE.md` is left saying where it went. The migration is idempotent, and
a file staged on both sides stops the whole move rather than picking one: choosing would
drop work nobody has posted.

A bare number outside a checkout now resolves too, because the store holds every review
and only one usually answers to a number. Two repositories with the same number open is
refused rather than guessed, since guessing reads one pull request while saying the
other's.

Two things fell out of it. A review that no longer parses names no repository, so nothing
can move it: it lists as a leftover under a heading of its own rather than being hidden.
And `post` says what it removed by naming the review rather than the path, since an
absolute path into the state directory is neither short nor something anyone types.

### 5. The agent loop, both directions — done, bar the threads tab

Four parts, and the point of all of them is that a review is a conversation with Claude
Code rather than a file handoff.

**Reload rather than clobber** is built. The screen stats the artifact once a second and
takes what an agent wrote, saying what arrived rather than redrawing silently. Its own
writes are told apart by the stamp it records after each save, so a save of its own never
reads as a change. The collision is the case it exists for: where the comment being typed
in was rewritten underneath, the buffer keeps the screen and the other version is held on
`ctrl+t`, which swaps the two and swaps them back. Resolving that without asking would
throw one of the two away.

**What the agent reads** is built, as `show <pr> --diff` and `context <pr> <id>`. The
first prints the cached diff with every staged comment marked on the line it anchors to,
the second prints one comment with its hunk, its private note, its turns, and the
conversation it answers. Both are text rather than JSON, because they are read rather
than parsed, and both read the commit the review was written against rather than the
working tree.

**A fifth state and a thread** is built. `todo` sits beside ready, draft, and skipped,
means an agent owes work here, and blocks the post the way a draft does. Turns are the
exchange about one comment, they append rather than replace so an answer cannot lose the
half already there, and they render collapsed: the last turn trimmed to two lines, one
line of the turn before it, a count of everything older, and `za` for the lot.

**Batch dispatch** is `T`. It writes every todo comment out with its context and runs
whatever `dispatch` in `config.toml` names, with the command's own output going to a log
beside the set, since the screen owns the terminal while it runs. Unset, it writes the
file and names it: starting an agent is not something to do on a keystroke nobody
configured. `second-look todo <pr>` prints the same set, so the loop works from the shell
as well as from the screen.

**The threads view** is built, as `t` in the review screen: every open conversation on the
pull request, each under the line it answers, with the rest of the diff left out. GitHub's
own list interleaves the diff, so what is still being asked spreads through a page of code
nobody is rereading; here the code is the one line the thread hangs from, drawn as a line
of the diff so it keeps its number and its grammar.

It is off the `c` cycle rather than a fourth stop on it, because a conversation is what the
forge already holds rather than what this pass is staging, and putting it on the cycle
would have cost the settled one-press return from the comment view to the diff. `t` was the
syntax-aware whitespace filter, which moved to `W` beside `w`, since `T` is batch dispatch.

### 6. The session for twenty-five reviews — half done

Prefetch is built. Once every search has answered, the queue stages the next few rows in
the order it is read, with no checkout: a detached target moves no working copy, so a
prefetch cannot be noticed in a clone. A row this laptop already holds a review for is
skipped, since what is on disk is what a re-review reads and refetching it would throw
away the read marks it is pinned to. `prefetch` in `config.toml` sets how many to keep
ahead and zero turns it off.

It holds back on a thin allowance, using the same reasoning the rating burst already used:
staging one review costs three reads, and spending the last of the hourly allowance on
rows nobody asked for is the wrong half to spend it on. The header says how many are
staging and how many are staged.

Leaving a review comes back to the queue rather than ending the session, which is the
cheap half of the navigation the step asks for: quitting the program to reach the next row
makes the queue a list to consult rather than one to work through.

Pruning is finished. Posting and merging already discarded what they staged, and a
prefetched review the queue no longer holds is now discarded once every search has
answered: a row that merged, or that somebody else reviewed, used to leave one on disk
forever, invisible until the staged list was opened weeks later. Only what this session
staged and nobody has written into goes. A review carrying a comment, a body, or a note is
work however it got there, so it is kept however stale it is, and losing one would be far
worse than keeping one.

Still outstanding, and each wants living with before it is built, because each encodes a
decision rather than a gap: the session cutoff line (what defines a session), reordering
rows by hand and holding the reordered ones out of the sort with a count on the tab (what
a hand-placed row means when the rating re-sorts under it), notification at a boundary
(which boundary), the recently-opened list, the checkout indicator (gh-repo-dashboard owns
disk, so what second-look may say about a clone without duplicating it), and the review
screen as a view inside the tabbed shell rather than a program the shell hands off to.
`demo/scene.sh` now opens each queue on seed data, which is where those get argued with.

### 7. What changed since I read it — the filter and the detection, not the picker

The filter is `U`, and it turned out to be presentation over state that already existed:
every hunk already marked read is hidden, and the marks are keyed by what a hunk says
rather than by the commit it sat on, so a hunk that survived a force-push unchanged stays
hidden and one that was touched comes back. It is a separate axis from `w` and `t` rather
than a rung on that ladder, because what a parser calls cosmetic and what this pass has
already read are different questions and a second pass wants to ask both.

Live detection is the watcher asking the head again once a minute rather than only when
the screen opens. A review read over twenty minutes outlives the answer given when it
started, and finding out at submit time that the head moved is finding out too late.

**Restaging in place** is `ctrl+r`, and it is only offered once the watcher has found a
head that has moved. It prepares the review again against that head, then swaps in the
diff, the conversations, and the read marks without the screen being reopened, and says
what the move cost: which staged comments no longer anchor in the diff that resulted.

The seam this was waiting on is `Target.Restage()`, which is the same target with no
working copy. A restage moves no tree, because a tree moved as a side effect of somebody
else's push is a tree moved without being asked, and where the checkout stands is already
`C`'s question. What `get` wrote is read back off disk rather than handed over, so the
screen ends up holding exactly what a reopen would have given it. The read marks survive on
their own, since they are keyed by what a hunk says rather than by the commit it sat on.

Still not built: the picker for comparing against any earlier round, which needs the caches
of earlier heads kept rather than swept, a change to what `get` cleans up and a decision
about how much disk a year of reviews is allowed.

A pull request already reviewed says so wherever it is opened from, which step 4 gave for
free: the read marks and the rating live with the review rather than with the clone.

### 8. Suggestions and drift — done, bar the markers

`s` takes the line under the cursor, opens it on the line's own text, and stages what
comes back as a GitHub suggestion. That is the whole of why nobody writes one from a
terminal: three fences and the line's own leading whitespace, typed correctly, to say
what an edit of the line says by being it.

What GitHub would refuse is refused at staging time. A suggestion replaces lines of the
file that results, so it can only hang from the right side, and every line it covers has
to be a line that exists there. A range crossing a removed line is the case that reads
as fine and fails on posting: the numbers are contiguous in the post-image and the diff
shows a gap between them. The check runs in `artifact`, so a suggestion an agent writes
into the TOML is held to it too, and `Resolve` runs it on every staged batch.

Drift is the generalization, and it is the post-time anchor guard asked at read time: a
comment whose line no longer reads the way it did says so where it is drawn. It costs
nothing against a diff already in memory, and finding out at submit that four comments
moved under a force-push is finding out too late.

Ranges are selected as well as staged. `V` opens one on the line under the cursor and
closes it wherever the cursor has reached, `a` writes a single comment covering all of it,
and `s` opens on every line the suggestion will replace. The range is answered by the key
that uses it and by a second `V`, so the next comment is never written against the last
one's lines, and a rebuild drops it because the two row numbers it holds are about to mean
something else. The bar is thinner than the cursor's rather than paler, since a terminal
that quantizes the two colors into one would leave the range invisible.

`[TODO:` markers are not built. They are requirements.md's second review target rather
than a part of this one: local changes with no posting endpoint, where a comment lands in
the source as a marker, which is a second mode rather than a key.

### 9. Writing the comment — completion built, the editor still owed to aragonite

`ctrl+n` completes the word under the cursor from what the review already holds: the files
the diff touches by full path and by base name, the symbols the structural pass named, and
the logins of everyone who has said something on the pull request, with `@` switching to
that last list. Pressing again takes the next match, so the key walks the list rather than
sticking on the first. It needs no index, which is exactly why it is the half that got
built.

Every symbol in the repository rather than the ones in the diff still needs codeintel, and
link completion needs somewhere for the candidates to come from, so both wait.

Extracting the inline editor to aragonite as `tui/editor` is not done, and it is a
cross-repository change rather than a feature: the editor here works, and moving it means
its own check ladder, a release, and a version bump on this side. It wants doing when a
second tool needs it, which is what would prove the shape.

Images are still an open question rather than an assumption. Nothing here verified whether
`gh` can upload one, and requirements.md's note that it cannot is what still stands: the
right next move is to test it against a real repository, not to build the fallback on the
strength of a remembered limitation.

### 10. Reading around the diff — the spike landed

`+` and `-` grow and shrink the file's own lines around the hunk under the cursor, three
at a press. The file is read once and kept, and the press that started the read is the
press that gets the lines.

The checkout-less path is answered, and it is the same seam either way: `git show
<sha>:<path>` where a checkout holds the commit, which is free and works with no network
and reads the commit rather than the working tree, so a tree left on another branch still
answers correctly; and `gh api repos/<repo>/contents/<path>?ref=<sha>` where none does.
Neither is a fallback for the other going wrong, since a checkout that does not hold the
commit is the ordinary detached case rather than a failure.

An expanded line carries no pre-image number, because a line outside the hunk has none in
the diff and inventing one would be a number that lies.

Definitions and usages still wait on codeintel, and opening the whole file rather than a
window around the hunk wants living with first: three at a press is enough for the case
this was built for, and a whole-file view is closer to an editor than to a review.

### Later, in the order I would reach for them

A history view of a comment's turns, once the thread exists and I know what I go back for.
Linear and other context on a queue row, which is the thing gh-dash cannot do at all.
Lockfile cards, which have their own section below. Issues beside pull requests, still
waiting for a real gap rather than parity.

## jj colocated repositories — fixed upstream

`second-look <pr>` used to fail in any repository with a `.jj` directory, including this
one, because `vcs.HeadSHA` asked `jj log -r 'git_head()'` and jj 0.44 answers "Function
`git_head` doesn't exist".

[aragonite](https://github.com/kyleking/aragonite) v0.11.0 reads a colocated repository
through its own `.git` instead, which answers the same commit with nothing left to rename
and does not snapshot the working copy the way every jj command does. A jj repository with
no `.git` has no commit a code host knows about, so the working copy's first parent stands
in, which is what jj names as `git_head()`'s replacement. Verified here: `second-look
reviews` and `second-look show` both read this checkout.

## Beyond alpha: replace gh-dash

The bar is [gh-dash](https://github.com/dlvhdr/gh-dash). It is the tool I would otherwise
open, and everything second-look does better is wasted if getting to a pull request costs
a clone and a branch switch. So the goal is one screen I can live in: read the queue,
open any pull request in it, review it properly, answer the conversations, post, and move
to the next one, without touching the working tree unless I mean to.

All four are built (below). Sections come from `~/.config/second-look/config.toml`, one gh
search query each, and the row verbs are `C` check out, `m` comment, `A` approve, with the
merge deliberately elsewhere: `M` in the review screen, after the diff has been read.

What gh-dash still has that this does not: issues beside pull requests, which waits for a
real gap rather than parity for its own sake, and its preview pane, which the review screen
replaces with something better. The queue's order past a screenful is the rating's, which
now rates a list of pull requests rather than only the one being read.

The division of labour with gh-repo-dashboard is worth stating, because two tools reading
the same data is the failure mode to avoid. gh-repo-dashboard owns disk: clones,
worktrees, branches, dirty state. second-look owns the review and the conversations.
aragonite owns the data both read and the views both draw, so neither is the other's
server. `filter/` and `tui/table` are already named for extraction there, and the pull
request cache is the next thing that wants to move, since both tools now ask GitHub the
same questions.

## The screens are hard to read — done

All of it came out of driving the built binary rather than reading the code,
which is why none of it showed up in a test.

### Done

**Landing on a comment centers it.** A jump anchored the block one row down the frame, so
the code that explains a finding, which is above the line it hangs from, was entirely off
the top. `]c`, `tab`, and `n` now put the block in the middle of the frame where it fits,
and a block taller than the frame still anchors near the top rather than losing its head.

**A range says what it covers.** A comment renders under its end line, so one anchored to
lines 12 through 15 read as a comment on line 15 and left the three above it looking
untouched. The heading names the span, and names both sides where they differ, which is
the case where the range crosses a change rather than sitting on one side of it. Drawing
the covered lines themselves belongs to the rich renderer, which is where a band is.

**The rating is a column.** The inbox spliced `cost 38  ` onto the front of the title,
so a rated row's title started nine columns right of an unrated one's and every title
slid sideways as the background pass answered and the queue re-sorted under it. It has a
right-aligned column of its own between the age and the title now, sized to the widest
number in the list and drawn at zero width where nothing is rated, which is what the
conversations and staged queues get.

**A staged review can be discarded.** `d` on `second-look reviews`, twice, removes the
artifact along with the read marks and every cache keyed to its head. Until then the only
way to be rid of one was `rm -r` on the directory it lives in, which is how an empty
artifact from a verification run turned up in an unrelated repository weeks later: a
review prepared outside a checkout lives under the user config directory and lists from
every working directory on this laptop.

**Nothing collected the caches keyed by head commit.** The diff, the threads, and the
rating are keyed by the commit their line numbers belong to, and a pull request pushed to
a dozen times kept a dozen of each forever, because posting removed the artifact alone and
merging removed nothing. `second-look get` now sweeps every one no staged review is
pinned to, and posting, discarding, and merging take that review's own with them. A file
that will not parse stops the sweep, since its head is unknown and the diff a hand repair
needs could be any of them.

**An edit left unfinished is offered back.** The editor threw its buffer away on escape,
so a long comment half written was gone with the keystroke that left it. It is written
through on every key now, kept under `.second-look/drafts/` beside the review, and offered
back the next time the same thing is opened, with how long ago it was left. `ctrl+r` puts
back what the field says instead, so the two are one keystroke apart rather than a guess.
Age does not expire one: a buffer is offered however old it is, and saying when it was
left is what makes that safe.

**A removal opens where it stands.** In the code view a run of removed lines is one row
saying how much came out, and `za` now opens it in place, comments on those lines
included. A bottom split, a right split, and a modal were the three shapes considered; the
fold is the fourth and the only one the screen already had a grammar for.

**A comment can be written without an agent.** `a` then a severity letter opens the editor
under the line the cursor is on, and `ctrl+s` stages it ready with the anchor quoted. It
was the last thing on the review screen that needed something else to do it, and it closes
the gap alpha was still carrying.

**Notes are drawn until you fold them.** A note over two lines used to start folded, which
hid the evidence for the comment it sits under. `zi` inverts every fold the way `za`
inverts one, which is the toggle vim spells `zi`.

**Saving an edit makes a draft ready.** Writing the comment out is the ruling a draft is
waiting for, and reaching for `m r` afterwards was a second decision nobody was making. A
skip keeps its status: it is a decision with a reason against it.

**The title carries the file only once its heading has gone.** It was there whichever row
the cursor was on, truncated from the right, so the name went first and the directory it
was in survived. The list screens already carried their section heading this way.

**The three queues are one screen with three tabs.** `inbox`, `threads`, and `reviews`
each open on their own tab, digits and `]`/`[` switch, and each tab keeps its own cursor,
filter, and scroll. A tab's loader runs when it is first looked at, so a tab nobody
switched to makes no request. Going from a pull request waiting on you to the conversation
on it used to mean quitting and starting a second program.

**A stack reads bottom first.** The branches a pull request joins are recorded when its
review is prepared, so the staged list can see that one review's base is another's head
and group the chain in the order the diffs make sense in.

**Where the frame sits, and which comment the cursor is on.** A scrollbar down the right
edge of every list and the review screen, and `comment 3/12` in the title where the cursor
is inside one. The column is only spent where the content overflows.

**Submitting names what it posts.** `S` then `a`, `r`, or `c`, written back to the
artifact. `S` then `S` used to send the review as whatever the file already said it was,
which is the one decision the confirmation exists to take. `o` opens the pull request in a
browser, which is where to go the moment it posts.

**The inbox fills in as it answers, and orders itself for triage.** Its sections are
independent searches, so they run at once and each is drawn as it lands rather than
the terminal staying empty until the slowest returns. Each bucket is then ordered by
what this laptop already knows: a review started here first, then the smallest of what
an earlier read rated, then the oldest, with drafts under all of it. Nothing there
costs an API call. Two things a reviewer would also want — how large an unrated diff
is, and whether they are the only human asked — would cost one per row, because
`gh search prs` returns neither.

**`/` narrows a queue.** It matches the repository, the author, the title, and the
line last said, narrows as it is typed, and says how many rows it is holding back,
because a queue that is quiet for the wrong reason is the worst thing a filter can
do. `esc` puts the rows back before it leaves the screen.

**A third view: the code alone.** `c` walks both, the code, the comments. The code
view is the file as it reads after the change, where a removal stands as one line
and each conversation stands as one row that `za` opens. A +/- pair leaves working
out what the code now says to the reader, and four comments on one hunk bury the
lines they are about.

**The cursor is a bar in the margin.** A reversed row meant reading the content
through a band of inverted text, and on a wide terminal it was the loudest thing on
the screen. The glyph carries the position, so it still survives NO_COLOR.

**`ctrl+e` and `ctrl+y` peek.** They scroll and leave the cursor, so a glance at what
is above or below costs nothing: the next motion pulls the frame back. Both screens.

**Everything an agent stages is a draft.** `comment add` writes a comment as a draft
whatever status it arrived with, and says how many it held; a skip is left alone. The
skill said to use status honestly and an agent could always disagree, so the rule is
in the binary and the contract in `--help` says so.

**The README has a recording.** `mise run demo` drives a prepared review in a
throwaway checkout behind a stand-in `gh`, so it reaches no network and touches no
real review.

**A comment has a shape of its own.** The body used to render in the note style, so
the prose was dimmer than the code it was about, and a wrapped note repeated its
bullet per line and read as a list. A comment now opens on a blank row under a
heavy rail, names its severity in caps, keeps the body at the contrast of the code,
and caps prose at 88 columns with a margin off the right edge. Only the note stays
dim, under one capital label: it is the evidence, not the finding.

**Editing happens in the frame.** `e` opens a box in place of the block, so the
line being answered stays on screen: `ctrl+s` saves, `esc` abandons, `ctrl+e` hands
what is typed to `$EDITOR` for what a text box is the wrong shape for. It writes a
comment, an answer to a thread, and the review's own body and note. Modal editing
is deliberately not here: it belongs in aragonite as `tui/editor`, shared by every
tool that writes prose in a terminal, and is recorded there.

**The review's body and note are rows.** They carried no comment index, so `tab`
walked past them and `e` said there was no comment here, which left the review's own
prose editable only in the TOML. Both are drawn whether or not anything is written
in them, since a field that appears once it is filled in is one nobody knows to fill
in, and the screen opens on the body.

**`m` then `r`, `d`, or `x`.** Three unmodified letters next to the motion keys each
restamped whatever the cursor was on. A bare press now names the chord rather than
doing nothing, because the hand that learned them keeps reaching for them.

**`z` folds.** What the cursor is on: a whole file from its name, one hunk from
anywhere inside it, a note from its comment. `za` inverts, `zR` opens everything,
`zM` folds to the file names, which turns an eight-file review into one screen with
the comment count on each file. It also found a bug the whitespace fold had all
along: a hidden hunk's comments were reported as no longer in the diff.

**The title bar says where the checkout is.** `C`, `!`, and `M` each behave
differently for on head, off head, and no clone, and the way to find out was to
press a key and read the refusal.

**Hints are drawn one way.** The key bracketed inside the word it does
(`[c]omments`, `[S]ubmit`) and bracketed in front where the word does not carry it
(`[tab] switch`), which is enough of a legend that a footer needs none. The same
shape captions the second key of a chord while it waits. It is
`aragonite/tui/keyhint`, shared with every other tool here.

**A row keeps its identifying end.** Truncating from the right dropped a pull
request number and a thread's `:LINE`, so two threads on one file rendered as the
same row twice.

**The section a row is in.** The heading scrolls away exactly when the list is long
enough to need it, so the header carries the one the cursor sits under.

**A bot's preview line was its own labels.** `_🎯 Functional Correctness_ | _🟡
Minor_ | _⚡ Quick win_` rather than what it said, which was four of eight rows in
one bucket on a real queue. A leading line whose every bar-separated part is
emphasis is passed over.

**`(s)`.** One `humanize.Plural` now, which the three packages that count things
share.

## Reviewing with no checkout — done

A pull request is named three ways now: `42` for this checkout's repository,
`owner/repo#42`, or a pull request URL, which is what a browser and a comment both hand
over. Anything but a bare number reviews from the API alone, with the artifact, the diff
cache, the thread cache, and the read marks under the user config directory, one directory
per repository. `second-look reviews` lists those beside the checkout's own, because a
review nobody can find again is a review lost.

The addressing is the part that had to change upstream. aragonite's `GetPR` and `PRDiff`
inferred the repository from a working directory, which is the assumption a tool holding
pull requests from many repositories at once breaks. They take the repository explicitly
now and pass `-R`. Inside a checkout second-look still passes nothing, because gh's own
resolution reads the remotes and picks a fork's upstream correctly where a name derived
from one remote would not. **That needs an aragonite release before this can be pushed.**

Verified against live GitHub rather than only the cassette: prepared from an empty
directory, read back from a third one, listed by `reviews` from a fourth, and the screen
drawn on a real terminal with the three open threads on
[#2](https://github.com/KyleKing/second-look/pull/2) rendered where they anchor.

## The lazy checkout — done

`C` in the review screen moves the working copy onto the pull request, asks before it
stashes anything, and draws the screen again. The screen closes first because the stash
question needs stdin and two Bubble Tea programs cannot own the terminal at once.

Which means the checkout stopped being a precondition. `get.Open` reports where the tree
is standing rather than refusing, so `ErrNotOnHead` is gone and choosing a row off either
list opens the review instead of moving the tree, which answers the open question that was
here about whether it should. `!` is the other half: a shell against another branch, or
against a directory that is not the repository at all, is refused by name rather than run.

Cloning stays manual. `C` moves a clone that is already here, and a repository with none
says so.

## Sections and row verbs — done

The inbox reads `[[section]]` blocks out of `~/.config/second-look/config.toml`: a name and
a gh search query each, in the order the file names them, replacing the three built-in
buckets outright. aragonite's `FleetSearchArgs` turns the query into gh's arguments, so a
`sort:` qualifier becomes `--sort` and `--order` and a query naming no subject is scoped to
what involves me. A query written for gh-dash works unchanged, which is the whole point.

The file lives under XDG rather than `os.UserConfigDir`, which on macOS answers
`~/Library/Application Support`. A file written by hand belongs beside gh's and gh-dash's
in a dotfiles repository; what second-look writes for itself stays in the platform
directory. A config that will not parse is reported and the built-in buckets are used, so
a typo leaves a working queue rather than none.

Row verbs are `C` check out, `m` comment in `$EDITOR`, and `A` approve, which arms on the
first press and sends on the second. Reviewing, checking out, and commenting all need the
terminal the screen owns, so each closes the screen, runs, and comes back to the queue;
approving is a single gh call and happens in place. Merging is `M` in the review screen
instead, where the key is only reachable after the diff has been read, and it refuses while
anything is still staged.

One thing here has never reached GitHub: the merge. Recording it would merge a pull
request, and [#2](https://github.com/KyleKing/second-look/pull/2) exists precisely because
it never merges, so the request shape is covered by a fake and by nothing else. Proving it
needs a throwaway pull request opened for the purpose.

`internal/ghrun` is the seam all of these share, lifted out of `internal/resolve` because
resolving, reacting, browsing, approving, commenting, and merging are one shape: run a gh
call, report its own stderr in the error. Writing that stderr to the terminal, which is
what it did before, drew over the frame of whichever screen was up.

## A lockfile is not a diff worth reading

A `uv.lock` or a `package-lock.json` change is hundreds of lines that say almost nothing,
and the four things I actually want to know are not in them: what moved, when the new
version shipped, whether anything newer exists, and whether any of it is a known
vulnerability. GitHub shows the diff and a dependency review tab that is thin, and I read
neither. So: fold every lockfile hunk by default and draw a card where it was.

The card is one row per package, with the direction of the move (`1.4.2 → 1.6.0`, major
minor or patch), how old the new version is, what the latest is if the move does not reach
it, and any advisory against the range being adopted. A package that is new to the file
gets more, because adopting one is the decision worth a second look: stars, downloads,
open issues and pull requests, how often it releases, and when it last did.

Folding the hunk is also what makes a comment on a lockfile placeable. Today the anchor is
whichever of four hundred lines the cursor happens to be on, which is a line nobody will
find again. With the card in the hunk's place, the comment lands on the package.

What has to be answered before any of it is built:

- Where the metadata comes from, one resolver per ecosystem: PyPI, the npm registry,
  crates.io, and the Go module proxy all answer versions and release dates without auth
- Where advisories come from. [OSV](https://osv.dev) covers every ecosystem here, takes a
  batch query, and needs no key, which makes it the one to try first
- What it costs. A hundred-package bump is a hundred lookups, so the answers are cached by
  package and version under the state directory, the batch endpoints are used where they
  exist, and the card fills in as it lands the way the inbox's sections do
- What happens offline, on a private registry, or when a lookup fails. The hunk is still
  there, so the fallback is the diff and a line saying which packages could not be
  resolved. A card that quietly omits a package is worse than no card
- Which files count as lockfiles, and whether that list is configurable
- What "popular alternatives" means. I have no definition that is not somebody's ranking,
  so it stays out until one exists that I would trust in a review

This is the first thing second-look would fetch from anywhere but GitHub, which is a real
change to what the tool is: every request is a package name leaving the laptop. It reaches
nothing without being asked, so the fetch is opt-in per repository and says what it will
query before it does.

## Decided since

The artifact is deleted on a successful post, and GitHub becomes the source of truth from
that moment. The inbox reads submitted reviews back from the API for its reviewed-and-open
and reviewed-and-merged buckets, so no comment ids are written back, nothing local
outlives the post, and the schema loses a field rather than gaining one.

`models.PRInfo` is now `forge.PullRequest` in aragonite, moved outright with no alias.

`forge` holds data and predicates only. Everything that emits a glyph, a placeholder, or
a human-readable duration moved to gh-repo-dashboard's `internal/ui`, which becomes
`aragonite/ui` once second-look has a TUI and gh-sweep is being cut over. gh-sweep is the
third consumer: it carries its own `internal/tui/theme` with terminal detection, while
gh-repo-dashboard has `styles`, `table`, and `markdown`.

`my_go_template` now ships a `verify-released` task (`GOWORK=off`, build and test) and
runs it as an hk **pre-push** step. Committing against a local sibling checkout is the
normal state; pushing a module that only builds against an unpublished one is the
mistake. gh-repo-dashboard has it, and it currently fails there by design, which is what
blocks pushing it until aragonite is released.

## Built

`internal/artifact` holds the schema, the TOML store, the payload builder, and the
anchor guard, with the posted and local split enforced by the builder rather than by a
list. `internal/diff` parses the unified diff both halves of the guard read.
`second-look get`, `comment add`, `show`, `show --payload`, `post`, and `post --dry-run`
all work, smoke-tested end to end against a real pull request. `post` removes the
prepared review once GitHub has it, so re-running it cannot publish a second copy.
Posting lives in `internal/post` behind a `Poster` interface, so the success path,
the reply-failed-after-the-review-posted path, and the draft refusal are all tested
against a fake rather than against GitHub.

`internal/tui` is the review screen: the diff with each comment rendered under the line
it anchors to, navigation by line, hunk, file, and comment, `r`/`d`/`x` to mark a
comment ready, draft, or skipped, `e` to edit one in `$EDITOR`, and `S` to submit.
Every keystroke that changes a comment writes the artifact, so quitting loses nothing.
A comment whose path is no longer in the diff is listed at the end under "not in this
diff" rather than dropped, since a comment nobody can see is a comment nobody can
retract. Bubble Tea v2 through `charm.land/bubbletea/v2`, colored from
`aragonite/tui/theme`, with every state carrying a glyph as well as a color.

`second-look <pr>` opens that screen, and `second-look` with no argument opens it for
the pull request the current branch belongs to. Neither moves the working copy:
checking a pull request out is `gh pr checkout`, and a screen that moved the tree as a
side effect of being opened would move a tree nobody asked it to. Standing somewhere
else is refused by name — no pull request for this branch, or the checkout is not on
the head and here is the command that fixes it. The artifact and the cached diff are
written only when they are missing, and an existing review keeps the head it was staged
against rather than being restamped on every open.

The `change-review` skill drafts through `second-look` and no longer writes a markdown
staging file. The original is backed up at `~/.claude/change-review-pre-sl.bak/`.

`internal/ghcassette` records and replays the gh subprocess through a `PATH` shim, so the
tests in `cmd/second-look` drive the built binary against the bytes GitHub actually sent.
The review and the reply on
[KyleKing/second-look#2](https://github.com/KyleKing/second-look/pull/2) were posted for
real and recorded; the head that moved, the draft refusal, the reply that failed after the
review posted, and the unanchored comment are all derived from those two recordings rather
than checked in again. The review screen is driven on a real pty through submit and every
quit path, and `internal/tui/testdata/TestFrames/` pins the review, comment, help, and
confirm frames at 80 and 120 columns. [AGENTS.local.md](AGENTS.local.md) carries the
recording procedure.

Five more the pty found, each proved before it was fixed. A post that failed
showed one truncated line, took the reason with it when the alternate screen
closed, and exited 0, so nothing recorded that GitHub had refused; the footer
wraps a failure now, the whole error reaches the scrollback, and the exit code
says so. Posting is asynchronous and `posted` was only set when the result
arrived, so six fast `S` presses armed and confirmed a second post before the
first answered. A review with no body and every comment skipped posted an empty
review and reported success; a COMMENT carrying nothing is refused, and an
APPROVE, which says something on its own, is not. Running `second-look <pr>`
without a terminal, which is exactly what an agent that ignores the skill does,
answered with Bubble Tea's "could not open TTY" and now names `second-look show
<pr>` instead. And the frame measured itself in runes: a comment in Japanese ran
211 cells into a 120-column frame, and a sentence in a script that puts no
spaces between its words was truncated after one line rather than wrapped.
Everything is measured in terminal cells now, and an over-wide word is broken
rather than dropped.

Two things the critique of the review screen found and fixed. A keystroke after a
successful post called `save`, which wrote back the prepared review that `post` had
deleted, so `second-look post` would have published the same review a second time. And the
cursor was a background color with no glyph or attribute, so under `NO_COLOR` the one
thing a reader needs most, where they are, was the one thing that vanished; it carries
`Reverse` now. The footer gained `q quit` and `j/k line` by dropping the hunk and file
keys while the cursor is inside a comment, since both sets do not fit 80 columns.

## The conversation queue — done

`second-look threads` is the queue of open discussions across every pull request I am
involved in, in three buckets: what moved since I last looked, what is still waiting on
me, then what is waiting on somebody else. One GraphQL call reads the viewer, the pull
requests, and all three conversation surfaces (inline review threads, the pull request's
own comments, and the bodies submitted reviews carry). A terminal gets the screen and a
pipe or `--json` gets the text.

Four rules keep it readable, each measured against my own 82 open pull requests rather
than guessed. A machine account reaches the queue only through an inline review thread,
which is the one surface where what a bot says is anchored to code and can be resolved;
without that rule the queue held 77 rows and 13 were real. My own comment with nothing
under it is something I said rather than a discussion. A resolved thread is gone, and so
is anything I have thumbs-upped. An outdated thread stays, because a reply to it is still
owed.

`R` resolves a thread and thumbs-ups it as well, since the thumbs-up is the marker I use
everywhere and GitHub gives a pull request comment and a review body no resolve at all.
What I have read is kept per conversation under the user config directory rather than in a
repository, because the queue spans repositories. Which bucket a row is in is fixed while
the screen is open: recomputing it after a mark moved the row out from under the cursor
the moment I opened it.

## Staged reviews, and the stash question — done

`second-look reviews` lists what is on disk under `.second-look/` in this checkout,
newest first, with what each review holds and whether a draft is blocking its submit. A
file that no longer parses is listed with the reason rather than skipped. Both it and the
conversation queue are the same `tui.List`, because two list screens would drift apart.

Choosing a row off either list moves the checkout onto that pull request when standing
somewhere else is what stops it opening. `get` asks before it moves a dirty tree now
rather than sending me away to stash by hand: it names how many files are uncommitted,
parks them with `git stash push --include-untracked` on a yes, and says that `git stash
pop` brings them back. Nothing is popped for me, because the work rarely belongs on the
head I just landed on. Only a terminal is asked, so an agent's run never has its tree
moved. The move that fails after the stash still prints the hint, which is the case the
pty test pins.

## The inbox — done

`second-look inbox` prints the review queue in three buckets, in the order they want
doing: pending your review, reviewed and still open, then reviewed and merged. Each line
is where it is, who wrote it, how stale it is, and the title, which is enough to triage
without opening anything. `--json` carries the same with the fields a script sorts on.

It is three `gh search prs` calls and reads nothing local, so it works from anywhere gh is
logged in rather than only inside a checkout. Each bucket's search fails on its own: the
first real run hit GitHub's secondary rate limit and printed three reasons instead of one
stack trace, which is the behaviour that was designed for and got tested by accident.
GitHub answers a rate limit with four hundred characters of terms of service, so the
human view prints the first sentence and points at `--json` for the rest.

The screen is built too, as the same `tui.List` the other two lists are, so all three
share their keys and their help. `enter` opens the review, which needs no clone of the
repository, and `o` opens it on GitHub. A bucket whose search failed carries one row saying
so and leaves the others alone.

Searching and sorting are still left to the caller, since the JSON output is a better sort
key than anything a flag would give. Ranking a long queue by review cost is the open
question below.

**Its cassette is written rather than recorded**, alone among the four. A real recording
carries the private repository names, usernames, and pull request titles of whatever is in
the queue, and this repository is public. [AGENTS.local.md](AGENTS.local.md) says so where
the recording procedure lives.

## Posting one comment on its own — done

`second-look post <pr> --only <id>` and `P` in the review screen post a single staged
comment through the standalone comment endpoint, outside any review. It is for the finding
that should not wait for the rest: a build broken for everyone, a secret in a diff.

The anchor guard runs first, the same as for a whole review, and the comment is removed
from the prepared review afterwards for the same reason a posted review's artifact is
deleted: GitHub owns it from that moment, and a copy left staged would go out a second
time. A reply goes to the replies endpoint instead, since that endpoint already names the
comment it answers. A skipped or draft comment is refused however directly it is named.

`P` asks nothing first, where `S` asks twice. A single comment is small enough to take
back by deleting it on GitHub; a whole review is not.

## Hiding whitespace — done

`w` hides every hunk that changes nothing but whitespace, says how many it hid on the
file it hid them from, and takes them out of the read count as well as the frame, since a
hunk nobody is being asked to read should not hold the count short of its total.

The test is what a reader would do: strip every space and tab from each added and removed
line and see whether the two sides say the same things. A re-indent, a tabs-to-spaces
pass, a reordering that changes no text, and a trailing-whitespace strip all answer true;
a line that gained a character does not. It lives in `internal/diff` because it is a fact
about a diff rather than about a screen.

The syntax-aware half is `t`, which hides more: every hunk a parser says changed no code,
so a re-wrap across line boundaries and a reworded comment go too. It shares its pass with
the review-cost rating, which is why the two arrived together. The section below has the
whole of it.

## The review-cost rating — in the review screen and in the queue

The number is in the title bar as `cost`, and `t` hides every hunk a parser says changed
no code. Both come from one pass, which is why they arrived together: they read the same
tree and neither was worth a dependency on its own.

The dependency is `ast-grep` on the path, not a linked library. Every tree-sitter binding
for Go needs cgo, `.goreleaser.yml` builds ten platforms with `CGO_ENABLED=0`, and its own
comment says a cgo-only dependency "makes this matrix build cleanly and emit nothing
usable". Shelling out is what the tool already does with gh, git, `$EDITOR`, and `$SHELL`,
so it keeps the release matrix and loses nothing but the feature where the binary is
missing, which `t` says out loud. It is pinned in `.config/mise/conf.d/user.toml` so CI
runs `internal/structure`'s tests rather than skipping them.

difftastic was the other candidate and was dropped after measuring it. `difft --exit-code`
answers 0 for a pure reformat and 1 for a reworded comment, which is correct for a diff
viewer and wrong here: telling a comment change from a code change is the distinction the
whole pass exists to make. What replaced it costs nothing: comparing every non-whitespace
byte of a hunk's two sides settles layout exactly, in process, and only what survives that
needs a grammar.

Both sides of a hunk come from the patch, context lines included, so nothing here reads a
working copy or fetches a blob. That is what makes it work on a review prepared with no
checkout, and it is also the limit: a hunk is a fragment, so the enclosing symbol of a
change deep inside a body is not always knowable. What is knowable from the fragment is
the four classes the spec asks for, because a declaration that changed is in the changed
lines by definition.

What the score reads, in weight order: a signature changed (40, once however many hunks
carry one, since the second is the same risk again rather than more of it), a capability
class the after side reaches and the before side did not (25 each), a symbol added (12), a
symbol deleted (8), and the hunk count as a tiebreaker (1 each), capped at 99. A hunk the
parser could not read counts toward size and nothing else, and `Score.Rated` reports that,
so the title shows no number rather than a number meaning less than it looks.

### Ordering the queue by it

Built. A bucket holding more rows than a screen shows is rated in the background, four
diffs at a time, and the queue re-sorts as the answers land. What was rated is kept in one
file under the state home against the update time the search reported, so a second open
orders itself off disk and only a row pushed to since is read again. A diff no grammar
answers for is recorded as read too, or a queue of lockfile bumps would fetch the same
unratable diffs on every open; a diff that could not be fetched is not recorded, so a rate
limit costs the order and not the cache.

The threshold is the reason the rating exists: recency orders a screenful well enough, so
a bucket a reader can see all of at once is left in the order its search answered rather
than costing an API read per row.

That threshold leaves rows unrated, and so does the allowance, so a row nothing has rated
is read once the cursor has stopped on it. Every move restarts the wait, which is what
makes it lazy: running the cursor down a queue asks for nothing, and stopping on a row
pays for that row alone. It is the same read the burst makes and writes into the same
cache, so a row rated this way is rated for the next open too.

The allowance guard is the other half, and it lives in aragonite as `github.Budgets`
because gh-sweep and gh-repo-dashboard burst the same way. It reads what is left of each
pool (core, GraphQL, and search are separate allowances) through `gh api rate_limit`, which
GitHub does not charge against the limit, so a burst can ask whether it is affordable
rather than firing and reporting the wreckage. The queue spends at most half of what is
left, since opening the reviews it just ordered costs reads too.

GraphQL is worth naming as the pool that stays untouched while core empties, and it cannot
take this work: `pullRequest.files` answers path, additions, deletions, and changeType, and
no patch text, which the structural rating needs. It would carry a size-only order, which
is worse than the rating and better than age, if the core pool ever turns out to be the
binding constraint in practice.

Capability classes are read off the callee's name rather than resolved, so a local named
`exec` counts and an aliased call does not. Its honest meaning is "a new capability visible
to syntax", which requirements.md already states and which still separates the changes
carrying one from the ones carrying none.

### What is left

**Blast radius.** requirements.md already calls it "a later addition rather than a
first-cut input": import graphs overcount, dynamic imports undercount, and it needs a
whole-repo scan, which the checkout-less path cannot promise. Cache it by base SHA if it
lands.

**The weights are guesses.** They order the cases I could think of, and `internal/rate`'s
test pins the order rather than the numbers for that reason. They want a pass over a real
week of the queue before anyone trusts the gap between 38 and 51.

## Files grouped by directory — done

Files render under the directory they sit in, which in a Go tree is one package, with the
file and hunk counts on the heading so a directory can be taken or left as a unit. `]d`
walks the groups and `n` repeats it.

The diff's own order is kept inside a group rather than re-sorted, because that order
carries the forge's judgment about what to show first. What changes is that a directory
the diff interleaves is now shown as one block, so a reader never holds two places in one
package at once, and the boundaries are visible instead of being inferred from the paths.

## The comment view — done

`c` shows the comments alone, grouped by the file they sit on, with each heading carrying
its own ready, draft, and skipped counts. A skipped comment is counted rather than listed:
a finding considered and declined is worth recording and not worth re-reading, and the
diff view still shows it where it sits.

It is a filter over the same rows rather than a second screen, so every motion, the
search, and `r`/`d`/`x`/`e` all work in it with no extra code. `c` again returns to the
same comment in the diff, and the one case that cannot round-trip, a cursor sitting on a
skipped comment, says so rather than landing silently at the top.

## Search — done

`/` opens the one prompt the screen has, and a committed pattern becomes the motion `n`
repeats, so a search and a jump between hunks are walked with the same key rather than
two. Matching is case-insensitive until the pattern carries an uppercase letter.

`tab` inside the prompt flips the scope between the whole diff and hunks nobody has read,
which is the part [requirements.md](requirements.md) says no tool anywhere does. The
prompt names the scope rather than leaving it to be remembered, because a search that
silently skipped most of the diff would be the worst kind of wrong.

The prompt is `bubbles/textinput`, which handles unicode and paste properly. Its cursor
schedules a blink half a second out and reschedules forever, so `press` in the model
tests drops a command that has not answered in 100ms; waiting on each blink cost the
package thirteen seconds a run.

## Seen-state — done

`space` marks the hunk under the cursor read, or every hunk of a file from its file line.
`]u` goes to the next unread hunk and `n` repeats it, so a review is finished when nothing
answers `]u`. The title carries `n/m read`, and a read hunk shows a `✓` on its heading,
which is a glyph rather than a color so the number that says how much is left survives a
monochrome terminal. Every mark is written through immediately, the way every other change
is, so quitting loses nothing.

A hunk is identified by what it says: the file plus every line of the hunk, kinds
included, line numbers left out. That is what makes read-state survive a force-push
without a carry-over step, since a hunk that slides down the file answers the same
identity and one whose text changed does not. `.second-look/seen/pr-<n>.toml` holds the
hashes, pruned on every `get` to the hunks the current diff still carries, and `get`
reports how many were already read.

**range-diff was built and measured out.** The plan was to delegate to `git range-diff`
first and fall back to the hash, and it turns out to answer nothing the hash does not.
Rebase onto a commit touching an unrelated file: range-diff says `=`, and the two
cumulative diffs are byte-identical, so the hash already carries the mark. Rebase onto a
commit touching the hunk's own context: range-diff says `!`, and the hunk's text differs,
so the hash correctly leaves it unread. `=` and "the hash matches" are one condition.
Getting more out of it would mean attributing a cumulative-diff hunk to a single commit,
which is blame-level work. [requirements.md](requirements.md) carries the same finding
where the decision was made.

Still unbuilt from the same Must: `jj interdiff`, which was named beside range-diff and
loses to the hash for the same reason, and per-commit browsing, which seen-state is
supposed to replace and which was never built to begin with.

## The keymap — rebuilt

Moving is a grammar now rather than a key per destination. `]` or `[` plus an object
letter names a motion (`h` hunk, `f` file, `c` comment, `t` thread), `n` repeats it and
`N` reverses it, and `.` repeats the last change. Triaging a review is `]c` then
`n . n . n .`.

Three things drove it. `n`/`p` meant "next hunk", which collides with the one vim
convention every reader already has, and `n` now means what it means everywhere else.
Every new destination used to cost another key pair, and seen-state alone adds two more;
an object costs no keyspace under the grammar. And chording is a dead end here: ctrl+c,
ctrl+d, ctrl+s, and ctrl+z belong to the terminal, and Meta chords do not survive tmux
and ssh intact, so a chord is the one binding that cannot be relied on. The two page
keys are the only chords left.

`.` records only the changes that need no further input, which is `r`, `d`, and `x`.
Replaying an editor blind is not a repeat of anything. An unfinished `]` cancels on
escape and refuses an unknown letter rather than swallowing the next keystroke, since a
prefix nobody meant is the easiest key to mistype.

`E` took the note, freeing `N`. `tab` still walks whatever wants a decision, so the
single-key path a first-time reader finds is still there.

## Evidence on a comment — done

The schema always had `note`, local and never posted. The screen could not reach it, so
the evidence a comment rests on could only be written by an agent through `comment add`.
`E` edits the note in `$EDITOR` now, and `!` hands the terminal to `$SHELL` in the
repository and appends what the session printed to the note under the cursor. Run the
code under review, come back, and the comment carries the output rather than a claim
about it.

`internal/shellrun` is the capture. It runs the shell under `script(1)`, which is what
allocates the pty an interactive shell needs while its output is being recorded, and
there is no fallback: a shell writing to a pipe would not be interactive, and one on the
real terminal would leave nothing to attach. util-linux and BSD `script` take their
arguments in opposite orders and only the first has `--version`, which is how they are
told apart. A transcript is stripped of escape sequences and capped at its tail, since a
long build ends in the part worth quoting.

The transcript is left as the shell wrote it otherwise, trailing `exit` included.
Trimming that would mean guessing at a prompt, and a heuristic that eats real output is
worse than two lines of ceremony.

## Existing review threads — done

`second-look get` reads the pull request's unresolved review threads through the GraphQL
`reviewThreads` query and caches them under `.second-look/threads/`, keyed by head commit
the way the diff is. The review screen shows each one under the line it anchors to, above
the comments this pass is adding, so a comment reads as an answer to the conversation
above it. `e` on a thread opens `$EDITOR` and stages the answer as a reply with
`in_reply_to` already filled in, and `second-look show <pr> --threads` prints the same
threads with the comment id a reply addresses, which is how an agent answers one without
copying an id by hand.

A resolved or outdated thread is dropped rather than shown. A second pass is about what
is still open, and an outdated thread anchors to a line the diff no longer carries, so it
has nowhere to render.

Nothing here is posted from the thread cache. A reply is an ordinary comment in the
prepared review and goes out through the same path as every other one, which is what
keeps the "nothing local outlives the post" rule intact.

The GraphQL read is the one recording that lives beside the code that makes it, in
`internal/threads/testdata/cassettes/threads.golden`, because no scratch repository can
record the rest of a `get`. It reads and posts nothing, so re-recording it is safe:
`GHCASSETTE_RECORD=1 go test ./internal/threads/`.

## 1. Scaffold from my_go_template — done

Scaffolded from template v0.11.4 with `project_name=second-look`. The binary is
`second-look`, aliased to `sl` in the README, because copier keys the entrypoint
directory, the goreleaser build, and the gitignored binary path off `project_name`, and
a repo whose binary is named something else gets two entrypoints. `golangci-lint` had
never run on this code and found 75 issues; all of them are fixed.

Two things [my_go_template](https://github.com/KyleKing/my_go_template) needed:

- `verify-released` and its hk pre-push step exist at the template's HEAD but not at the
  v0.11.4 tag, so they were copied in by hand. A `copier update` after the next release
  should be a no-op
- tombi keeps a TOML array multi-line only when it has a trailing comma, and none of the
  template's arrays had one, so `hk check` failed on a fresh scaffold and the fix
  collapsed `.golangci.toml` into 300-character lines. Fixed in the template and in
  gh-repo-dashboard, which had the same failure, and unreleased there too

Two settings this repo owns rather than inherits. `fieldalignment` is off, because
go-toml writes keys in declaration order and packing `Review` and `Comment` would
scramble the file a person hand-edits. `_skip_if_exists` kept `DESIGN.md`, `README.md`,
and `go.mod`.

## 2. `second-look get` — done

It reads the pull request through `aragonite/forge/github`, moves the working copy onto
its head, writes the artifact, and caches the diff under `.second-look/diff/` keyed by
head commit. It never clones.

Moving the working tree needs a clean one, and being already on the pull request head
never blocks however dirty the tree is. Already on the branch but behind, it tries
`git pull --ff-only` and stops with git's own reason when that refuses. **Never
`--autostash`**: on git 2.x `--ff-only --autostash` against a dirty file the pull also
touches exits 0 while leaving `UU` conflict markers in the tree and the stash still on
the stack, so a tool reading the exit code walks into a review of a conflicted tree.

jj needs none of the guard, since its working copy is a commit and a fetch has nothing
uncommitted to clobber. The jj paths are written and untested: I have no colocated jj
checkout with an open pull request to run them against.

Re-running `get` after the head moves says how many staged comments came with it, and
the anchor guard re-checks each one on post. The keybinding that resolves the mismatch
waits for the TUI.

One thing the plan missed. `.second-look/` is untracked, so second-look's own state
counted against the clean-tree guard and `get` refused to run a second time. The
artifact tree now carries a `.gitignore` of its own, which is also what keeps the
prepared review out of the user's commits.

## 3. The anchor guard — done

Two checks rather than one, because the failure has two shapes.

Staging resolves each comment against the cached diff and quotes the line it anchors to
into the comment's `anchor` field. A comment on a line the diff does not carry is
refused with nothing written, which is where a bot citing line 993 of a 137-line file
now gets caught. GitHub refuses that comment anyway, so this only moves the refusal
somewhere a person can read it.

Posting re-reads the live diff and compares those quotes byte for byte, and refuses
outright if the pull request has new commits.

Both halves read the pull request's cumulative diff against its merge base, which is what
GitHub numbers a review comment against. A diff carrying a file twice is a per-commit
patch series, whose line numbers belong to an intermediate commit, and the guard refuses
it rather than quoting an anchor from it. A multi-line comment has to keep both ends
inside one hunk, since only its end line carries a quote and GitHub refuses the rest. `internal/diff` is the parser both sides
share: it reads only what an anchor needs, so a rename or a binary payload is skipped
rather than modeled.

Step 0 is cut from the skill, replaced by `sl get <pr>` and a note that anchors are
checked from there.

## 4. Extract `forge` and `vcs` into aragonite — done

`aragonite/vcs` holds git and jj behind one interface, and `aragonite/forge/github`
holds the `gh` wrapper. gh-repo-dashboard reads both and no longer carries
`internal/vcs`, `internal/github`, or `internal/cache`, the last of which had nothing
left of its own once the caches typed on forge and vcs values moved with them.

`RepoSummary` split rather than moved whole. `vcs.RepoSummary` is what a checkout says
about itself, and gh-repo-dashboard's `models.RepoSummary` embeds it and keeps `PRInfo`,
`WorkflowInfo`, `TemplateInfo`, `NotesFiles`, `Loading`, and `Error`. The glyph and
duration methods became `ui` functions, which is the same rule the pull request move
settled. `models.VCSType` is `vcs.Type`, since `vcs.VCSType` stutters.

The gh implementation went to `forge/github` rather than into `forge`, so `forge` keeps
the host-neutral model and GitLab is a sibling directory rather than a rename. The
interface itself waits for a second implementation to shape it.

gh-repo-dashboard's suite passes and it now lints clean, down from 121 issues, because
the packages carrying most of them left.

## 5. `second-look skill` — done

`go:embed` the skill file and print it to stdout. The same build produces the binary and
its documentation, so the two cannot disagree about the schema.

```sh
second-look skill        # read it
second-look skill > ~/.claude/skills/change-review/SKILL.md
```

It ships the contract second-look owns and nothing else: the commands, the anchor rules,
the local fields, and the opening line telling an agent not to open the review screen. The
schema itself stays in `--help`, which lives beside the code that enforces it, so the
skill has nothing to drift from. The voice rules stay in the personal `change-review`
skill, which is where they belong and where a public repository should not carry them.

hunk does this as `hunk skill path`, which prints a path to a skill file bundled in its
install tree and tells you to load or symlink it. That path is version-pinned
(`/opt/homebrew/Cellar/hunk/0.18.1/libexec/skills/hunk-review/SKILL.md`), which is why
the command exists at all: a symlink to it breaks on the next upgrade, so the agent has
to ask each time.

Printing the content sidesteps that. The global skill says "run `second-look skill` for
the current contract" and reads it fresh, so there is no path to go stale and no copy to
rot.

Two things to copy from hunk's skill, which is 184 lines:

- YAML frontmatter with `name` and `description`, so what `second-look skill` prints is
  a complete skill file that needs no assembly
- An opening line telling the agent **not** to launch the TUI. hunk's says the TUI is for
  the user and the agent drives `hunk session *` instead. Once `second-look <pr>` opens
  a review, an agent that runs it will hang on a terminal nothing is attached to

## 6. Publish aragonite, and keep it published — done

aragonite v0.1.0 read the diff with `gh pr diff --patch`, which returns a patch series
rather than the pull request's diff, so every anchor a released build quoted was quoted
from the wrong line. v0.2.1 carries the fix, second-look depends on it, and
`mise run verify-released` passes with no workspace. The `go.work` that made the
unreleased sibling usable is gone; recreating it is three lines when aragonite next
needs work ahead of a release.

aragonite still has no tooling of its own: no `.golangci.toml`, no mise config, no hk
setup. It was linted here with a copy of gh-repo-dashboard's config, which is not a
thing anything enforces. Scaffold it before the first release rather than after.

The standing workflow after that:

- `go.mod` always pins a released aragonite version. The gitignored `go.work` overrides
  it while working across the two repos
- `GOWORK=off` is what proves the consumer builds against the published version rather
  than the checkout on disk. Without it, local green says nothing about CI
- hk runs the workspace-free check on **pre-push**, not pre-commit. Committing mid-change
  against a local aragonite is the normal state; pushing a consumer that needs an
  unpublished library is the mistake worth catching

```sh
# .config/mise/conf.d/*.toml
[tasks.verify-released]
run = "go test ./..."
env = { GOWORK = "off" }
```

Releasing aragonite is `cz bump` plus a pushed tag, since the Go module proxy resolves a
library from its tag and no goreleaser is involved. Consumers then `go get` the new
version and commit the `go.mod` change.

## Settled

**The coverage floor is enforced and passes at 76.0%.** `internal/get` did not need
aragonite's stubs widened after all: the cassette replaces gh at the process boundary, so
the pty and CLI tests exercise it for real. What was missing was counting them. `go test
-cover` instruments the test binary and everything those tests run happens in a child, so
the binary is built with `go build -cover` and both halves are merged as covdata. CI runs
it through the template's `ci:project` hook, which needs no workflow edit.

## Deferred

The coverage-gate backport is done and pushed:
[`8816862`](https://github.com/KyleKing/my_go_template/commit/8816862) anchors the read
to `grep '^total:'`, since the loose form also matches every covered function whose name
contains it and hands `awk` two numbers. Once the template releases and second-look takes
a `copier update`, the `test:coverage-min` override in `.config/mise/conf.d/user.toml`
loses that half of its reason to exist.

The other half is the subprocess coverage this project needs and the template does not
render: `COVERDIR_SUBPROCESS` is still absent upstream, so the override stays until it
lands there. `ci:project` stays either way, since it is what runs the floor in CI.

One thing is owed to [gh-sweep](https://github.com/KyleKing/gh-sweep): its `comments`
view reads unresolved review threads across a repo list via GraphQL, which second-look
only does for one PR at a time (`## Existing review threads — done`). A cross-repo
unresolved-thread queue is the natural next tab once second-look wants one; gh-sweep's
implementation is the reference. Not moving until second-look actually wants that scope.

One thing is owed to [aragonite](https://github.com/KyleKing/aragonite): `tui/editor`,
recorded in its README and not written. Modal editing over a text box, or a pane handing
the buffer to the user's own nvim, shared by every tool here that writes prose in a
terminal. Everything else it owed has shipped: `tui/keyhint` (including the help legend),
`tui/skin`, and the out-of-order cassette replay a queue running its searches at once
needs, all in v0.9.0.

## Open questions

**Whether the queue should pay for what it cannot know for free.** The lazy half is
built: the cursor stopping on a row rates that row and nothing else, which is what bounds
an unrated queue's cost to what is actually read. Two signals a reviewer would want are
still not fetched. How large a diff is could be counted off the patch the rating already
pulls, so it costs nothing more. Whether they are the only human asked is a second read
per row, and it wants living with the lazy rating before anyone spends it.

**Whether the review-cost rating moves to aragonite.** It reads the diff, the symbol
graph, and the changed symbols, so it may belong next to `codeintel` rather than here.
Not a question worth answering before it is written: extract it if a second tool wants
it, and leave it here otherwise.

**Where the tests for moved code should live.** Settled by the vcs and forge moves:
tests go with the code. The `RepoSummary` predicate tests are in `aragonite/vcs`, the
disk and registry wiring tests are in `aragonite/forge/github`, and the display tests
are in gh-repo-dashboard's `internal/ui`. The pull request tests still sitting in
`internal/app` from the earlier display split are the remaining strays.
