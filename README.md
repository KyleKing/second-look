# second-look

Prepare a code review locally, with Claude Code drafting comments you edit by hand, then
post it to GitHub in one deterministic call.

Fetching a pull request, staging comments, reading the diff on screen, and posting all
work.

![Reading a prepared review, ruling on a comment, editing it in place, and the same
review as the code that results](demo/second-look.gif)

```bash
second-look get 42        # fetch the PR, cache the diff, check it out
# an agent drafts comments through the skill the binary prints
second-look 42            # read the diff, triage the comments, submit with a key
second-look post 42       # or post from the shell in one call
```

A pull request is named three ways: `42` for this checkout's repository,
`owner/repo#42`, or its URL. The last two need no clone of it. Reading the diff, triaging,
answering a conversation, and posting all come off the API, so a review is prepared and
finished from an empty directory, with its state under your config directory rather than
in a working copy. `second-look reviews` lists those beside the checkout's own.

`second-look inbox`, `second-look threads`, and `second-look reviews` are three tabs of
one screen rather than three programs. Each command opens on its own tab, `1`, `2`, and
`3` pick one, `]` and `[` step through them, and each keeps its own cursor and filter, so
going from a pull request waiting on you to the conversation on it costs a keystroke. A
tab loads when it is first looked at, so opening on one pays for that one alone.

The inbox runs its sections at once and draws each as it lands, with the headings saying
which searches are still out. Each bucket is ordered for triage rather than by recency:
what you have already started here first, then the smallest of what an earlier read
rated, then what has waited longest, with drafts under all of it.

A bucket you cannot see all of at once is rated in the background, four diffs at a time,
and re-sorts as the answers land while the header counts what is still out. The reader
keeps their place: the cursor stays on the row it was on. What was rated is kept under the
state directory against the update time it was read at, so a second open orders itself off
disk and only a pull request pushed to since is read again. A diff no grammar answers for
(a lockfile, a directory of YAML) is recorded as read all the same, and one that could not
be fetched at all is not, so a rate limit costs the order rather than the cache. A bucket
short enough to read at a glance is left alone, because a read per row buys nothing there.

Before any of it runs, the queue asks GitHub what is left of the hour's allowance, which is
a read GitHub does not charge for. It spends at most half of what remains: ordering a queue
is what leads to opening the reviews in it, and those are reads too, so a queue sorted
perfectly by an exhausted allowance has spent the budget on the index and none on the book.
Where it stops short the header says so and when more arrives.
The other thing a reviewer would want and cannot have for free is whether they are the
only human asked: `gh search prs` does not say. `/` narrows any of the queues to the rows carrying a word, matching the repository, the
author, the title, and the line last said, with the header saying how many are held back.
`esc` puts them back before it leaves the screen.

`C` in the review screen is what gets a working copy when you want one: it moves the
checkout onto the pull request, asks before it stashes anything, and draws the screen
again. Cloning stays manual, so `C` moves a clone that is already on this laptop and says
so when there is none. `!` refuses in that case too, rather than opening a shell against
whatever the working directory happens to be.

`second-look skill` prints the instructions an agent needs to drive it, ready to write
into a skills directory.

Every comment is anchored to the diff line it points at. A comment on a line the diff
does not carry is refused while staging, and one whose line has moved since is refused
before anything is sent.

Conversations already open on the pull request are shown where they anchor, so a second
pass answers what was said last time. `e` on one writes the reply in `$EDITOR`.

`second-look inbox` is your review queue. `enter` on a row opens the review, which needs no
clone of that repository, so getting to one costs an API read rather than a clone and a
branch switch. `C` moves a checkout onto it, `m` comments on the pull request itself, `A`
approves it (`A` again to confirm), and `o` opens it on GitHub. A pipe or `--json` gets the
text instead of the screen.

The sections are yours. Without a config it shows three buckets (pending your review,
reviewed and still open, then reviewed and merged); with one it shows what you asked for:

```toml
# ~/.config/second-look/config.toml
limit = 25

[[section]]
name = "needs my review"
query = "review-requested:@me is:open archived:false sort:updated-desc"

[[section]]
name = "my work"
query = "author:@me org:acme is:open archived:false sort:updated-desc"
```

A query is `gh search prs` terms, which is what GitHub's search box takes. A `sort:`
qualifier becomes gh's own flags and a query naming no subject is scoped to what involves
you, so a query written for [gh-dash](https://github.com/dlvhdr/gh-dash) answers the same
way here.

Merging is not on a list row. It is `M` in the review screen, `M` again to confirm, and it
refuses while anything is still staged.

Submitting is `S` and then what the review is: `a` approves, `r` requests changes, and `c`
comments. What the second key says is written back to the artifact, so a review posted as
an approval does not read afterwards as a comment. There is no key for sending it as
whatever the file already says, because that is the decision the confirmation exists to
take. `o` opens the pull request in a browser, which is where to go the moment it posts.

`second-look threads` is the queue of conversations across every open pull request you
are involved in, in three buckets: what moved since you last looked, what is still
waiting on you, then what is waiting on somebody else. A conversation is yours when the
pull request is yours, when you have commented in it, or when a comment names you, and it
covers inline review threads, the pull request's own comments, and the bodies submitted
reviews carry.

"New" means new to you. What you have read is kept per conversation under your config
directory rather than in a repository, because the queue spans repositories, so a reply
that arrived while you were away shows up whether or not a notification did. `enter`
reads a conversation and marks it, `R` marks one dealt with, and `r` opens the review
screen to stage an answer, in whichever clone of that repository this laptop has.

A thumbs-up is what `R` always leaves, because that is the marker a person recognizes and
the only one a pull request comment or a review body can carry. A thread gets the resolve
as well. So anything you have already thumbs-upped is gone from the queue whether or not
GitHub let you resolve it.

A bot reaches that queue only through an inline review thread, which is the one surface
where what it says is anchored to code and can be resolved. Its pull request comments are
coverage tables and linkbacks nobody ever resolves, and admitting them filled the queue
with 77 rows where 13 were real.

`second-look reviews` lists what is staged under `.second-look/`, newest first: this
checkout's, then the ones staged with no checkout of their repository at all. `enter`
opens one. Everything it lists is unfinished, because the artifact is deleted the moment a
review posts. `d`, twice, throws one away along with the diff, threads, rating, and read
marks kept for it.

A pull request based on another one staged here is a stack, and the two are grouped
together with the bottom first, which is the order they read in: a diff against changes
you have not seen yet is a diff you cannot review. The branches are read when the review
is prepared, so a stack is visible where both of its pull requests have a review staged
on this laptop.

Answering a conversation on a repository you are not standing in works too. second-look
asks `gh repo-dashboard --cli` (from its cache, so no network) which clones of that
repository are on this laptop, uses the one that answers, and offers the choice best first
when several do: already on the branch, then clean, then one that would need a stash. A
repository with no clone here is reviewed from the API instead. It needs
[gh-repo-dashboard](https://github.com/kyleking/gh-repo-dashboard) new enough to print a
`remote` field, and says so when it is not.

Uncommitted work is the case the move asks about: it names how many files are dirty and
offers to park them with `git stash`, and `git stash pop` brings them back. Nothing is
popped for you, and declining leaves the tree as it was. Only a terminal is asked, so a
piped run never has its working tree moved.

`P` posts one comment on its own for the thing that should not wait, and
`second-look post 42 --only <id>` does the same from the shell.

`w` hides hunks that change nothing but whitespace, and says how many it hid. `t`
hides every hunk a parser says changed no code, so a re-wrap across lines and a
reworded comment go too. That needs [ast-grep](https://ast-grep.github.io) on the
path, because every tree-sitter binding for Go needs cgo and the release builds
ten platforms without it; `t` says so when it is missing and `w` never needs it.

The same pass rates the change, which is the `cost` in the title bar. A signature
change outweighs anything a body does, because every caller of the symbol is in
scope whether or not the diff shows one; a capability the change reaches and the
base did not counts next; and size is only the tiebreaker. The number is
advisory, deterministic, and decides nothing.

Files are grouped by directory with file and hunk counts on each heading, and `]d` walks
the groups. The title names the file the cursor is in once that file's own heading has
scrolled off the top, beside where the frame sits and which comment the cursor is on. A
heading you can still see is not worth the width.

`c` walks three views: both, the code, the comments. Both is the diff with what is being
said about it inline. The code is the file as it reads after the change, where a removal
stands as one line saying how much came out and a comment stands as one row, both of which
`za` opens where they are, because a +/- pair leaves working out what the code now says to
the reader and four comments on one hunk bury it. The comments are what will post, by file. The cursor keeps
its comment across the change.

`v` walks the renderers, which change how a line of the diff is drawn rather than which
lines are drawn. `plain` is a whole-line color per side and one number per line, which is
what a terminal diff has always looked like. `rich` colors the code by its grammar and
says which side a line is on with a band behind it instead, marks the runs that actually
differ from the line the change paired it with, and carries both line numbers in the
gutter, so a one-word edit reads as one word.

`rich` is a spike and ships with its caveat named in the footer when you switch to it: a
hunk is a fragment of a file, so a grammar's state above the hunk is lost and a hunk
opening inside a block comment is colored as code. Highlighting is
[chroma](https://github.com/alecthomas/chroma), which is pure Go and keeps the release
matrix building with `CGO_ENABLED=0`, unlike every tree-sitter binding. The bands are
drawn deeper on a terminal that cannot mix its own colors, because the 256-color cube
carries almost no dark tints and both sides otherwise quantize into the grey ramp.

`a` then `b`, `m`, `n`, `t`, or `?` writes a comment on the line under the cursor, ranked
blocker to question (`q` is every chord's cancel, so a question is `?`). The editor opens under the line, and `ctrl+s` stages the comment
ready with the line it anchors to already quoted, so nothing has to be restamped and the
post guard has what it needs.

`e` opens an editor in the frame, in place of the block it is writing, so the line being
answered stays on screen. `ctrl+s` saves, `esc` leaves it, and `ctrl+e` hands what is typed
to `$EDITOR` for the edits a text box is the wrong shape for. It writes a comment, an
answer to an open thread, and the review's own body and note. Saving an edit marks a draft
ready, since writing the comment out is the ruling a draft was waiting for; a skip keeps
its status, because a skip is a decision with a reason against it.

An edit left unfinished is kept beside the review rather than thrown away, so a terminal
that dies mid-sentence loses nothing. The next `e` on the same thing opens on what was
left, says how long ago, and `ctrl+r` puts back what the field says now, which is the
other half of the decision.

`S` then `a`, `r`, or `c` submits, approving, requesting changes, or commenting. The second
key both confirms the post and says what kind of review it is: there is no key for "send it
as whatever the file already says", because that is the decision the confirmation exists to
take.

`m` then `r`, `d`, or `x` marks a comment ready, draft, or skipped. It is a chord because
each of those restamps whatever the cursor is on, irreversibly, and three unmodified
letters next to the motion keys made that one keystroke away.

`z` folds what the cursor is on: a whole file from its name, one hunk from anywhere inside
it, a comment's note from the comment, and in the code view the run of removed lines from
the row standing for it. `za` inverts it, `zi` inverts the whole review, `zR` opens
everything, and `zM` folds to the file names, which is the outline a long review is read
from. Everything is drawn until you fold it: a note is the evidence for the comment above
it, and one folded by default is one nobody reads.

`/` searches, and `tab` in the prompt restricts it to hunks you have not read yet. The
pattern becomes a motion, so `n` walks the matches the same way it walks hunks.

`ctrl+e` and `ctrl+y` scroll without moving the cursor, so a glance at what is above or
below costs nothing to come back from: the next motion pulls the frame back to where you
were.

`space` marks a hunk read and `]u` goes to the next unread one, so a long review is
finished when nothing answers `]u`. What is read is keyed by the hunk's content, so a
force-push that leaves a hunk alone leaves it read.

`!` drops to your shell in the repository and attaches what the session printed to the
comment under the cursor, so a comment carries the output that proves it. That note is
local and never posted. It refuses while the checkout is on another branch or missing,
since a shell there would run against something other than the diff.

- [Next steps](NEXT_STEPS.md) — what alpha needs, in order, and what is still open
- [Requirements](requirements.md) — scope, decisions made, and what is still open
- [Prior art, August 2026](research/prior-art-2026-08.md) — what already exists in this
  space and the three capabilities nothing implements

## Installation

Homebrew, once a release exists:

```bash
brew install --cask kyleking/tap/second-look
```

Or from source:

```bash
go install github.com/kyleking/second-look/cmd/second-look@latest
```

The binary is `second-look`. Every command below is short enough to type through an
alias, which is what I use:

```bash
alias sl=second-look
```

## Development

```bash
mise install && hk install --mise
mise run ci
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full workflow.
