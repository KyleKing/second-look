# second-look specifics

What [AGENTS.md](AGENTS.md) does not cover because it is template-owned and this is not.

## Recorded gh interactions

Everything that reaches GitHub goes through the `gh` binary: aragonite's pull request
reads and second-look's own `gh api` posts alike. So the recording seam is `PATH`, not an
HTTP transport. `internal/ghcassette` builds a stand-in `gh`, puts it ahead of the real
one, and either records what the real binary did or answers from a cassette. The tests in
`cmd/second-look` drive the built binary against it, which is the same code path a person
runs, subprocess included.

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
SECOND_LOOK_RECORD=1 go test ./cmd/second-look/ -run 'TestPostReview$'
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
`SECOND_LOOK_RECORD=1 go test ./internal/threads/`, which reads and posts nothing.
`cmd/second-look` splices that interaction onto the `post-review` reads in `getCassette`.

`cmd/second-look/testdata/cassettes/inbox.golden` is the exception to all of this: it is
written, not recorded. A real recording of `second-look inbox` carries the private
repository names, usernames, and pull request titles of whatever is in the reviewer's
queue, and this repository is public. The arguments in it are the ones a real run made and
the answers are gh's own shape with invented content. `SECOND_LOOK_RECORD=1` would
overwrite it with real data, so the test uses `Replay` rather than `Start`.

### The suite reaches nothing

Only `gh` is replayed. `git` runs for real, so a code path that shells out to the network
would pass on a laptop that has credentials and hang in CI. `scratchRepo` points `origin`
at `https://127.0.0.1:1/KyleKing/second-look.git`, which parses as the right repository
and answers nothing. A test that starts failing with a connection error has found a new
network call, not a broken fixture.

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
