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
has run against live GitHub from inside the screen rather than from the shell. What alpha
still leaves out: seen-state, the inbox, the rating, and writing a new comment (rather
than a reply) without an agent.

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

## Hiding whitespace — done

`w` hides every hunk that changes nothing but whitespace, says how many it hid on the
file it hid them from, and takes them out of the read count as well as the frame, since a
hunk nobody is being asked to read should not hold the count short of its total.

The test is what a reader would do: strip every space and tab from each added and removed
line and see whether the two sides say the same things. A re-indent, a tabs-to-spaces
pass, a reordering that changes no text, and a trailing-whitespace strip all answer true;
a line that gained a character does not. It lives in `internal/diff` because it is a fact
about a diff rather than about a screen.

The syntax-aware half of that requirement is not built. It needs tree-sitter, which is the
same dependency the review-cost rating waits on, and neither should arrive on its own.

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
`SECOND_LOOK_RECORD=1 go test ./internal/threads/`.

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

Both backports to [my_go_template](https://github.com/KyleKing/my_go_template) are done
and sit unpushed there:
[`e665527`](https://github.com/KyleKing/my_go_template/commit/e665527) counts subprocess
tests toward the coverage floor, and
[`82fcd6d`](https://github.com/KyleKing/my_go_template/commit/82fcd6d) teaches typos to
read an abbreviated commit hash as a hash. Once the template releases and second-look
takes a `copier update`, the `test:coverage-min` override in
`.config/mise/conf.d/user.toml` can be deleted: the two now agree on
`COVERDIR_SUBPROCESS`, so the rendered task drives this project's `TestMain` unchanged.
`ci:project` stays, since it is what runs the floor in CI.

## Open questions

**Whether the review-cost rating moves to aragonite.** It reads the diff, the symbol
graph, and the changed symbols, so it may belong next to `codeintel` rather than here.
Not a question worth answering before it is written: extract it if a second tool wants
it, and leave it here otherwise.

**Where the tests for moved code should live.** Settled by the vcs and forge moves:
tests go with the code. The `RepoSummary` predicate tests are in `aragonite/vcs`, the
disk and registry wiring tests are in `aragonite/forge/github`, and the display tests
are in gh-repo-dashboard's `internal/ui`. The pull request tests still sitting in
`internal/app` from the earlier display split are the remaining strays.
