# second-look specifics

What [AGENTS.md](AGENTS.md) does not cover because it is template-owned and this is not.

## Recorded gh interactions

Everything that reaches GitHub goes through the `gh` binary: aragonite's pull request
reads and second-look's own `gh api` posts alike. So the recording seam is `PATH`, not an
HTTP transport. `aragonite/ghcassette` builds a stand-in `gh`, puts it ahead of the real
one, and either records what the real binary did or answers from a cassette. The tests in
`cmd/second-look` drive the built binary against it, which is the same code path a person
runs, subprocess included.

It lives in aragonite because gh-sweep and gh-repo-dashboard shell out to `gh` the same
way. `GHCASSETTE_RECORD=1` is its variable, not this repository's, and its `Apply` replays
gh for an in-process test that drives the packages rather than the built binary.

A cassette is TOML named `.golden`, because `hk.pkl` excludes that suffix from the
whitespace fixers and nothing else. A recorded diff whose trailing spaces were stripped on
commit no longer matches the anchors it was recorded with.

There is one recording per outward-facing shape, and every other case is derived from it
in test code rather than checked in again. `cmd/second-look/testdata/cassettes/` holds
`post-review` and `post-reply`; the head that moved, the run that stops before it posts,
and the reply that failed are edits to those, made by `derive` in `e2e_test.go`. Three
copies of the same 60-line diff would drift.

### Re-recording

```sh
GHCASSETTE_RECORD=1 go test ./cmd/second-look/ -run 'TestPostReview$'
go test ./cmd/second-look/ -update       # then regenerate the goldens
```

**This posts to GitHub for real.** The target is
[KyleKing/second-look#2](https://github.com/KyleKing/second-look/pull/2), a draft pull
request that exists to be reviewed and is never merged. Nothing is pushed to
`fixture/review-target` again, so `fixtureHeadSHA` in `e2e_test.go` and the anchors in
`testdata/review/*.toml` stay valid. Record one test at a time: the tests run in parallel
and a recording session writes one cassette.

`post-reply` answers a comment created by the `post-review` recording, so its
`in_reply_to` is a real comment id. Re-recording the review without re-recording the reply
leaves that id pointing at a thread that still exists, which is fine; deleting the thread
on GitHub is what would break it.

`internal/threads` carries the third recording, of the GraphQL query for a pull request's
open review threads. It lives there rather than under `cmd/second-look/testdata/` because
the scratch repository cannot record a whole `get`: the recorded head and the scratch head
differ, so `get` tries to pull from an unreachable origin. Re-record it with
`GHCASSETTE_RECORD=1 go test ./internal/threads/`, which reads and posts nothing.
`cmd/second-look` splices that interaction onto the `post-review` reads in `getCassette`.

`cmd/second-look/testdata/cassettes/inbox.golden` is the exception to all of this: it is
written, not recorded. A real recording of `second-look inbox` carries the private
repository names, usernames, and pull request titles of whatever is in the reviewer's
queue, and this repository is public. The arguments in it are the ones a real run made and
the answers are gh's own shape with invented content. `GHCASSETTE_RECORD=1` would
overwrite it with real data, so the test uses `Replay` rather than `Start`.

The conversation queue's cassette is not checked in at all. `queueCassette` in
`threads_e2e_test.go` builds it from `conversations.Args` and
`internal/conversations/testdata/queue.json`, so the call it answers is the call the
fetcher makes. A hand-written copy of that 60-line GraphQL query would stop matching on
the first edit to it, and there is only one fixture of the queue's shape to keep current.

`internal/conversations/testdata/queue.json` is written for the same reason
`inbox.golden` is. A real reply carries the private repository names, logins, and titles
of whatever is open, and this repository is public, so the shapes in it are GitHub's own
with invented content. Every admit-or-drop rule is exercised against that one reply,
because the rules interact: a bot's inline thread stays while the same bot's pull request
comment goes, and that only reads as a rule when both are present.

### `--repo` is in the recording, and stripped for the tests that stand in a checkout

`post-review` and `post-reply` were recorded from a directory holding nothing but a
prepared review, which is what `post` reads, so their `pr view` and `pr diff` calls carry
`--repo KyleKing/second-look`: with no checkout to read a repository off, second-look names
it. A test that stands in `scratchRepo` gets the other shape, because there gh resolves the
repository itself, and `inCheckout` in `e2e_test.go` drops the flag for those. Re-recording
produces the flag again, so the file stays canonical and `inCheckout` stays the derivation.

### The suite reaches nothing

Only `gh` is replayed. `git` runs for real, so a code path that shells out to the network
would pass on a laptop that has credentials and hang in CI. `scratchRepo` points `origin`
at `https://127.0.0.1:1/KyleKing/second-look.git`, which parses as the right repository
and answers nothing. A test that starts failing with a connection error has found a new
network call, not a broken fixture.

`childEnv` also gives every child its own `HOME` and `XDG_CONFIG_HOME`. The queue's read
marks and every review staged with no checkout live under the user config directory, so a
child inheriting a real one would read the state of whoever runs the suite and write into
it. A test that inspects that state appends its own `HOME`, which wins because `os/exec`
keeps the last of a repeated variable.

## Coverage counts the subprocess

`go test -cover` instruments the test binary, and everything `cmd/second-look` tests
happens in a child process, so the plain profile reads `internal/get` as 0% while the pty
tests exercise it end to end. `TestMain` builds the binary with `go build -cover` and
passes `GOCOVERDIR` to the child explicitly, since `go test -test.gocoverdir` overwrites
that variable in the test process. `mise run test:coverage-min` collects both halves as
covdata directories and merges them, which is the only format `go tool covdata merge`
takes. It runs in CI through `ci:project`.

## Driving the review screen

`tmux` for looking at it, the pty tests in `cmd/second-look/tui_e2e_test.go` for pinning
it, and `internal/tui/testdata/TestFrames/` for the frames themselves.

A bare pty answers no terminal capability queries, so the program waits out a two-second
timeout on each of OSC 11 and DA1 before drawing anything, and the first frame used to
take about 4.5 seconds there against 0.5 under tmux. The harness answers those queries
now, which brings a pty test back under 1.5 seconds and is what stopped them timing out
when two suites run at once (`hk check --all` runs `ci` and `verify-released`
concurrently, each running the whole suite). A new capability query that nothing answers
will put the 2-second stall back: add it to `answers` in that file rather than widening
the deadline.
