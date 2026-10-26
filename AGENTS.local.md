# second-look specifics

What [AGENTS.md](AGENTS.md) does not cover, because it is template-owned and this is not.

## Recorded gh interactions

Everything that reaches GitHub goes through the `gh` binary, so the recording seam is
`PATH`. `aragonite/ghcassette` builds a stand-in `gh`, puts it ahead of the real one, and
either records what the real binary did or answers from a cassette. The tests in
`cmd/second-look` drive the built binary against it. `GHCASSETTE_RECORD=1` is aragonite's
variable, and its `Apply` replays gh for in-process tests.

A cassette is TOML named `.golden`, because `hk.pkl` excludes that suffix from the
whitespace fixers and nothing else. A diff with stripped trailing spaces no longer matches
its anchors.

Each outward-facing shape has one recording, and the other cases are derived from it in test
code (`derive` in `e2e_test.go`), because three copies of one 60-line diff would drift.
`cmd/second-look/testdata/cassettes/` holds `post-review` and `post-reply`.

### Re-recording

```sh
GHCASSETTE_RECORD=1 go test ./cmd/second-look/ -run 'TestPostReview$'
go test ./cmd/second-look/ -update       # then regenerate the goldens
```

**This posts to GitHub for real.** The target is
[KyleKing/second-look#2](https://github.com/KyleKing/second-look/pull/2), a draft pull
request that is never merged. Nothing is pushed to `fixture/review-target` again, so
`fixtureHeadSHA` in `e2e_test.go` and the anchors in `testdata/review/*.toml` stay valid.
Record one test at a time, because the tests run in parallel and a session writes one
cassette.

`post-reply` answers a comment the `post-review` recording created, so its `in_reply_to` is
a real comment id. Deleting that thread on GitHub would break it.

- `internal/resolve` records the resolve and thumbs-up `R` sends. One thread on #2 is now
  resolved with a thumbs-up, and `addReaction` refuses a duplicate, so re-recording needs a
  new thread. The ids are consts atop `live_test.go`. It replays through `Session.Apply`, so
  it cannot be parallel
- `internal/threads` records the GraphQL query for open review threads. It lives there
  because the scratch repository cannot record a whole `get` (its head differs from the
  recorded one, so `get` would pull from an unreachable origin). Re-record with
  `GHCASSETTE_RECORD=1 go test ./internal/threads/`, which posts nothing.
  `cmd/second-look` splices it onto the `post-review` reads in `getCassette`
- `inbox.golden` and `internal/conversations/testdata/queue.json` are written by hand with
  invented content, because a real recording would carry private repository names, logins,
  and titles in a public repository. The inbox test uses `Replay` because
  `GHCASSETTE_RECORD=1` would overwrite the file with real data. Every conversation
  admit-or-drop rule runs against that one `queue.json`, since the rules interact
- The conversation queue's cassette is built by `queueCassette` in `threads_e2e_test.go`
  from `conversations.Args` and `queue.json`, so it cannot drift from the query

`post-review` and `post-reply` were recorded from a directory with no checkout, so their
`pr view` and `pr diff` calls carry `--repo KyleKing/second-look`. Tests standing in
`scratchRepo` get the other shape, and `inCheckout` drops the flag. Re-recording produces the
flag again, so the file stays canonical and `inCheckout` stays the derivation.

### The suite reaches nothing

Only `gh` is replayed and `git` runs for real, so a path that shells out to the network would
pass on a laptop with credentials and hang in CI. `scratchRepo` points `origin` at
`https://127.0.0.1:1/KyleKing/second-look.git`, which parses as the right repository and
answers nothing. A connection error in a test means a new network call, not a broken fixture.

`childEnv` gives each child its own `HOME` and `XDG_CONFIG_HOME`, because read marks and
checkout-less reviews live under the user config directory and an inherited one would read
and write the runner's state. A test that inspects that state appends its own `HOME`, which
wins because `os/exec` keeps the last of a repeated variable.

## Coverage counts the subprocess

`go test -cover` instruments only the test binary, and `cmd/second-look` tests run in a child
process, so the plain profile reads `internal/get` as 0%. `TestMain` builds the binary with
`go build -cover` and passes `GOCOVERDIR` to the child explicitly, since
`go test -test.gocoverdir` overwrites that variable in the test process.
`mise run test:coverage-min` collects both halves as covdata directories and merges them,
which is the only format `go tool covdata merge` takes.

## The README recording

`mise run demo` builds the binary and runs `demo/demo.tape` through VHS. The tape drives
`demo/run.sh`, which copies `demo/fixture/.second-look` into a throwaway checkout and puts
`demo/bin/gh` ahead of the real one. That stand-in answers the one `pr view` the screen makes
and refuses everything else, so a recording reaches no network and touches no real review.
The gif is committed, so regenerate it when the screen changes shape, and keep the tape's
sleeps so each frame is readable.

## `internal/ghmd`

It segments a GitHub comment body and imports nothing from this repository, so it can move to
aragonite when a second tool wants it. It answers in blocks and leaves width, folding, and
color to `internal/tui/notes.go`. It is not a markdown implementation. It answers four
questions: where a fence starts and ends, which lines are a table, what a `<details>` holds,
and what an HTML comment is. Everything else is prose, and a rule needing a real parser is not
worth having here.

## Scenes

`demo/scene.sh <name>` opens `demo/scenes/<name>/` as a live screen against seed data, and
with no argument lists them. A scene names its argv in `scene.conf` and brings only what it
changes about the shared fixture: `review.toml`, `threads.json`, extra `gh` routes and their
seeds, and `diff/<sha>.patch` when it compares against an earlier round (`rounds`).

`demo/bin/gh` is the forge every scene runs against. Its `routes` table maps the tokens a call
must carry to the seed file that answers it. A token must start an argument, because
substring matching anywhere in the call read `--review-requested` as "view" and routed
the inbox's searches to the pull request. A seed named `.err` goes to stderr with a non-zero
exit, which is how a scene draws a failure. An unrouted call fails loudly, so a missing seed
cannot pass as an empty screen.

`SEED` lists directories searched in order, the scene's own first, so overriding an answer
means dropping a same-named file into the scene. The seed under `demo/fixture/` is written
rather than recorded, for the same privacy reason as `inbox.golden`. The checkout is rebuilt
on every run and kept afterwards, so what the screen wrote back is at
`demo/.work/<name>/.second-look/`. Edit `demo/scenes/`, since the work directory is discarded.

## The checker pass

`internal/diag` places notes against the diff, `internal/diag/lsp` speaks the protocol to the
installed language server, and `internal/diag/scan` runs the commands `[[check]]` names. A
file opens as an unsaved buffer holding the commit under review, so nothing writes to the
working tree, and its imports still come from disk.

A server starts in the file's project and not at the checkout root, and
`internal/diag/project` answers where that is for a server and a `[[check]]` alike. This is a
correctness rule. Rooted at a TypeScript monorepo, tsserver reads a package's imports as
unresolvable and reports errors the change did not cause, and where the root has no
`typescript` it refuses to start. The root goes in both `rootUri` and `rootPath`. The screen
passes a checkout named `.`, so the session makes the root absolute before walking, and every
project is asked at once so the floor below is paid once.

`Root` ranks marker groups before walking directories, as `vim.fs.root` does, because a
`go.work` above a file's `go.mod` resolves its sibling imports and depth is the wrong
tiebreak. A directory with a marker and none of the server's `Needs` is walked past.

A server that pulls its settings must be answered. `workspace/configuration` is answered per
section out of `Settings`, and `workspace/didChangeConfiguration` carries all of it once at
the handshake. Answering null leaves the server on its defaults. A `[[check]]` resolves its
command out of the project first (`bin`), because a linter from `PATH` is a different version
under different settings. Its output is re-anchored from the project to the checkout.

Two invariants in `lsp` each cost a session and each have a failing test without them:

- A server publishes an empty list while loading the project and corrects itself seconds
  later, so the pass waits out a floor before a quiet line means anything
- A server publishes for every file it loaded on the way, which never stops in a monorepo, so
  only a file the pass asked about restarts the quiet line. Without this every pass ran to its
  30-second deadline

The suite and demo turn the pass off like a laptop without a server: a `[[server]]` override
claims the extensions and names a command nothing has (`settle` in `e2e_test.go`, and a
written config in `demo/run.sh` and `demo/scene.sh`). A configured server that is not
installed disables its extensions instead of falling back to another. Coverage is a stub
server in `internal/diag/lsp` and a real gopls in `cmd/second-look/probe_test.go`, which skips
without it.

## `internal/advisory`

`L` on a lockfile asks osv.dev, which the lockfile never named. It names what it will send and
only a second `L` sends it, because every package name leaves the laptop. A review is given a
client unconditionally, which is why there is no config key.

OSV's batch call returns advisory ids only, so each hit is read again from `/v1/vulns/{id}`.
Severity is the record's own word and never computed from its CVSS vector. The batch answer
is positional, so a short answer is refused instead of lined up. Three limits shape the
screen's wording:

- A package OSV has never heard of answers the same as a clean one, so "nothing known against
  it" cannot become "every name resolved"
- Two databases publish one finding under their own ids and name each other as aliases, so a
  Go module otherwise reads twice
- Offline, rate limited, and a private registry arrive as one refusal, which the lockfile's
  own row says

## `internal/versions`

It asks each ecosystem's registry what a lockfile's new version shipped, what is current, and
where the source lives. It runs behind the first frame with no consent gate, because a
module's index is where its name already goes, and the `L` gate is for osv.dev alone. Answers
are cached through `aragonite/cache`, memory then disk, keyed by ecosystem and package with a
day's TTL.

Gotchas by fetcher:

- The Go proxy escapes uppercase as `!lower` and dates one version at a time, so a cached
  package at a new version still re-asks
- PyPI's `releases` carries both a zoned and a naive-UTC upload field
- npm needs the full packument, because the abbreviated form drops the `time` object
- RubyGems latest is the newest non-prerelease on the ruby platform
- crates.io refuses the HTTP client's default user agent, so requests send `second-look`

A 404 is an answer (`Missing`, drawn "not in <eco>"), and a failed ask is neither an answer
nor cached.

## Driving the review screen

Use `tmux` to look at it, the pty tests in `cmd/second-look/tui_e2e_test.go` to pin it, and
`internal/tui/testdata/TestFrames/` for frames.

A bare pty answers no terminal capability queries, so the program waits out a two-second
timeout on each of OSC 11 and DA1 before drawing, which made the first frame take about 4.5
seconds against 0.5 under tmux. The harness answers them now, which keeps pty tests under 1.5
seconds when two suites run at once (`hk check --all` runs `ci` and `verify-released`
concurrently). Add any new capability query to `answers` in that file and leave the deadline
alone, since an unanswered one brings the stall back.

`screen.frame()` is what a terminal would be showing, from a `charmbracelet/x/vt` emulator
fed everything the program writes. A test of a resize, a column that should be gone, or a
reversed row reads the frame and not the stream that drew it. `screen.resize` changes the
pty and replaces the emulator with one of the new size, because the library's own resize
leaves the old scroll region behind and panics on the next scroll. After a resize wait with
`awaitFrame` on something only the new size draws, such as the footer on the last row, since
the old frame stays on screen until the redraw lands.

Three harness rules each cost a session:

- `wait` waits for the reader as well as the exit, because an exited process may still have
  bytes in the pty and an assertion on its last write fails under load without that
- `await` searches the whole buffer, so across a screen that closes and reopens it answers
  from the first draw and the next keystroke lands in a program that has not started. Take a
  `mark()` and use `awaitFrom` across any hand-off that returns to a screen
- Queue rows tail-cut their `Left` cell, so `kyleking/aragonite#100` can render as
  `aragonite#100` at a narrow width. Await the substring that survives the narrowest frame
  the test drives
- A status that changes in place is never in the stream whole. A redraw writes only
  the cells that differ, so `showing 0 of 11` becoming `showing 1 of 15` writes
  `1 of 15`. Wait on a row, which is written whole where the cell was blank, and
  use `awaitPlayed` before quitting a screen that is still loading

## jj in this repository

`jj split` opens the configured diff editor (nvim) even with filesets, so it has no headless
path. The non-interactive route is `jj new` for an empty change on top, then
`jj squash --from @- --into @ <files>`.

`jj new <rev>` on an ancestor does not reset the working tree, so the new change forks
whatever the tree holds and silently duplicates a change if edits are present.
