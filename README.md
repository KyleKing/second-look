# second-look

Prepare a code review locally, with Claude Code drafting comments you edit by hand, then
post it to GitHub in one deterministic call.

Fetching a pull request, staging comments, reading the diff on screen, and posting all
work.

```bash
second-look get 42        # fetch the PR, check it out, cache the diff
# an agent drafts comments through the skill the binary prints
second-look 42            # read the diff, triage the comments, submit with a key
second-look post 42       # or post from the shell in one call
```

`second-look skill` prints the instructions an agent needs to drive it, ready to write
into a skills directory.

Every comment is anchored to the diff line it points at. A comment on a line the diff
does not carry is refused while staging, and one whose line has moved since is refused
before anything is sent.

Conversations already open on the pull request are shown where they anchor, so a second
pass answers what was said last time. `e` on one writes the reply in `$EDITOR`.

`second-look inbox` prints your review queue in three buckets: pending your review,
reviewed and still open, then reviewed and merged.

`second-look threads` is the queue of conversations across every open pull request you
are involved in, in three buckets: what moved since you last looked, what is still
waiting on you, then what is waiting on somebody else. A conversation is yours when the
pull request is yours, when you have commented in it, or when a comment names you, and it
covers inline review threads, the pull request's own comments, and the bodies submitted
reviews carry.

"New" means new to you. What you have read is kept per conversation under your config
directory rather than in a repository, because the queue spans repositories, so a reply
that arrived while you were away shows up whether or not a notification did. `enter`
reads a conversation and marks it, `R` resolves the thread (or thumbs-ups what GitHub
gives no resolve, which means the same thing), and `r` opens the review screen to stage
an answer.

A bot reaches that queue only through an inline review thread, which is the one surface
where what it says is anchored to code and can be resolved. Its pull request comments are
coverage tables and linkbacks nobody ever resolves, and admitting them filled the queue
with 77 rows where 13 were real.

`second-look reviews` lists what is staged under `.second-look/` in this checkout,
newest first, and `enter` opens one. Everything it lists is unfinished, because the
artifact is deleted the moment a review posts.

`P` posts one comment on its own for the thing that should not wait, and
`second-look post 42 --only <id>` does the same from the shell.

`w` hides hunks that change nothing but whitespace, and says how many it hid.

Files are grouped by directory with file and hunk counts on each heading, and `]d` walks
the groups.

`c` shows the comments alone, by file, and `c` again goes back to the same comment in the
diff.

`/` searches, and `tab` in the prompt restricts it to hunks you have not read yet. The
pattern becomes a motion, so `n` walks the matches the same way it walks hunks.

`space` marks a hunk read and `]u` goes to the next unread one, so a long review is
finished when nothing answers `]u`. What is read is keyed by the hunk's content, so a
force-push that leaves a hunk alone leaves it read.

`!` drops to your shell in the repository and attaches what the session printed to the
comment under the cursor, so a comment carries the output that proves it. That note is
local and never posted.

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
