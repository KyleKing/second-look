# Queue loading plan

Goal: opening `second-look inbox` draws the last known queue at once, shows counts
for all three tabs, fetches only what changed, and makes any reorder easy to follow.

This plan covers data and motion. The row layout lives in `queue-ui.md`. The two
plans meet at the interface in "What the UI needs from this plan".

## Measured baseline

One laptop, my real queue, a `gh` shim that logged argv and duration (a shell
script that execs the real `gh` and appends start, end, exit code, and the first
two arguments to a log). Counts and timings only, no repository names. One
sitting, ratings already cached on disk.

| Moment | Calls | Wall time | Notes |
| --- | --- | --- | --- |
| Open, tab 1 | `api rate_limit` plus 4 `search prs`, all parallel | 1.6 to 3.0 s | Rows first draw when a search lands. The allowance read (0.6 to 1.8 s) only gates the rating burst |
| Open, tab 1, lanes final | 1 `api graphql` facts call for 63 candidate rows | 4.3 to 9.5 s, typically 6.8 s | Starts only after the slowest search, so lanes settle near 9.6 s. Reply is 88 KB |
| After lanes | 3 to 8 `pr diff` ratings, 4 at a time | about 2.4 s each | `ratings.toml` skips rated rows. A cold file rates every row |
| First look at tab 2 | `api notifications` plus 1 conversations `api graphql` | 5.0 s | Notifications answered HTTP 404 in 0.5 s and the tab prints that error on every open |
| First look at tab 3 | 45 `api graphql`, one per staged review, started together | about 2.4 s wall, 0.9 to 2.4 s each | 45 `gh` processes in the same instant |
| Cursor down 8 rows | 0 calls | none | Ratings were cached. An unrated row costs one `pr diff` |
| Whole sitting | 56 calls | | |

Two direct measurements against the API, same 63 ids:

- The facts query with `files(first:100)` took 9.5 s and 6.8 s. Without `files` it
  took 6.3 s and 4.3 s. Changed-path areas cost about a third of the call, and the
  rest scales with node count (about 0.1 s per pull request). The lever is asking
  for fewer nodes, not a lighter query.
- A conditional `gh api -i -H 'If-None-Match: <etag>'` on a REST endpoint returns
  `304 Not Modified`, and `gh` exits 1 on it with the status line on stdout. In
  one request each, the REST search endpoint and the GraphQL endpoint carried no
  `ETag` header, so neither can be conditioned.

Also seen: on first draw the strip shows "[2] conversations" and "[3] staged · 14
blocked" only because staged is local. Conversations has no count until visited.

Unmeasured: the return from a review to the queue, and a cold cache run. Reading
`queueOnce` in `cmd/second-look/queuescreen.go`, every return rebuilds all three
screens and each tab's `Start()` runs again, so each return should repay the open
cost. Step 2 measures it before and after.

### What is cached today

- `~/Library/Caches/second-look/`: an aragonite `DiskCache` (0700 directory, 0600
  files), installed by `installCache` in `cmd/second-look/run.go`. Only the
  version cards (`internal/versions`) use it.
- `~/.config/second-look/`: `ratings.toml` (diff cost keyed by `Updated`, the one
  existing change-detection pattern), `conversations.toml` (read marks), the area
  and status files. None holds a queue.
- Nothing holds the searches, the facts, the conversations reply, or the staged
  reviews' remote states. Every open fetches all of it again.

### Where the time goes

1. A serial chain on tab 1: searches (about 3 s), then facts (about 7 s). Nothing
   is in final order until the chain ends.
2. Facts are fetched for every row on every open, though most rows did not change.
3. Tabs 2 and 3 wait for a first look, so their counts are blank and then cost 5 s
   and 2.4 s after the switch.
4. Tab 3 spawns one process per review.
5. Every return from a review pays all of it again.

## Design: snapshot, then diff

### Snapshot, stale-while-revalidate

One snapshot per tab, through aragonite's existing disk cache. No new mechanism.

- Store: the process-wide store `installCache` already sets. Write with
  `cache.Persist(mem, "queue", key, cache.NoStamp, value)` and read with
  `cache.PersistedStale`. `PersistedStale` returns the value past its TTL (up to the
  7 day `staleRetention`) plus its age, which is the stale-while-revalidate read.
  `Persisted` refuses a stale entry, so it is the wrong call here.
- Upstream `queue`, keys `inbox`, `conversations`, `staged`. One entry per tab
  because `DiskCache.write` rewrites the whole upstream file on each call, so 63
  per-PR entries would mean 63 rewrites.
- Memory TTL 24 h on the backing `TTLCache`. It only decides what counts as recent
  for the age label. Disk retention stays 7 days.
- Inbox value: the buckets' items (as a record type that keeps `ID`, which
  `PullRequest` hides behind `json:"-"`), the facts map keyed by node id with each
  record's `updatedAt`, `headRefOid`, and fetch time, the viewer login, and the
  saved time. `headRefOid` is added to the facts query (a few bytes per node).
- Conversations value: the decoded `conversations.Queue` plus notices. Staged
  value: review key to `prstate.State`.
- Account switch: the snapshot carries the viewer login. If the first refresh
  answers with another login, drop the snapshot, replace without animation, and
  flash nothing.
- Privacy: the snapshot holds private repository names, titles, and review text.
  It lives in the user cache directory, which aragonite creates 0700 with 0600
  files, written atomically. The repository is public, so no fixture or cassette
  may be recorded from it (the existing rule behind `inbox.golden`). A test
  asserts the modes of the written file. `second-look gc` should offer to clear it,
  since `cache.ClearAll` already does.
- Size: `diskMaxBytes` is 4 MB and eviction is by file. Step 1 measures the
  conversations snapshot (up to 50 threads of 50 comments per PR) against that. If
  it does not fit, add a `MaxBytes` option to `DiskCache` in aragonite, tag it, and
  bump `go.mod`. Otherwise aragonite is untouched.

On open, each loader's `Start()` first paints its snapshot synchronously, so the
first frame already has rows and counts, marks every row `Stale`, then returns the
refresh commands. When the refresh lands, the diff against what is on screen drives
the animation.

### Change detection

Each source gets the cheapest honest test.

- Inbox: the four searches stay (1.6 to 3 s, parallel, and the search resource has
  its own limit). Each result carries `updatedAt`. Compare it to the snapshot per
  node id and fetch facts only for ids that are new, have a different `updatedAt`,
  or fall under a rule below. Steady state is a handful of rows, so about 1 s at
  0.1 s per node instead of 6.8 s.
  - Rule A: a row whose last known checks were pending is refetched on every
    refresh.
  - Rule B: any record older than 10 minutes is refetched.
  - Unverified: whether `updatedAt` moves when a check suite finishes. I expect it
    does not, which is why Rule A exists. Step 3 proves it against a PR with a
    running check and drops Rule A if `updatedAt` does move.
  - Rows keep their snapshot facts while the changed ones are in flight, so the
    lanes do not wait.
- Conversations: the one query costs 5.0 s and returns everything. Split it into a
  light pass (the same search returning only `id`, `updatedAt`, `headRefOid`, plus
  `viewer`) and a heavy pass (`nodes(ids:)` with threads and comments) for changed
  ids only. Light pass latency is unmeasured, so step 5 times it first. If it is
  not under about 1.5 s, drop the split and run the whole query in the background
  behind the snapshot.
- Staged: states cost one process per review. Replace with one aliased GraphQL
  query (`r0: repository(...) { pullRequest(...) { ... } }`), chunked at 50
  aliases. One call instead of 45.
- Notifications: the one endpoint with real HTTP caching (`Last-Modified`, `ETag`,
  `X-Poll-Interval`, and a 304 on an authorized request does not count against the
  primary limit, per
  [GitHub's best practices](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api)).
  `gh` supports it through `-i -H 'If-None-Match: ...'`, where exit 1 plus a `304`
  status line means unchanged. It is optional (step 10) because it 404s with my
  token. A 404 is remembered for a day in the snapshot and drawn as one quiet line,
  not an error on every open.
- Not relied on: GraphQL conditional requests. My request returned no `ETag` and I
  found no GitHub documentation promising one. A web search did not settle it
  either way, so this is "not used", not "proven absent".

### Eager counts for all three tabs

`tui.Tabs` starts a loader on first look. Add `StartAll()` and call it once at open:
inbox first, then conversations, then staged, in one `tea.Batch`. With a snapshot
the strip counts are instant. Cold, the three run in parallel, so open costs the
slowest chain (about 9.6 s, the same as today) but tabs 2 and 3 are ready when
reached.

A count read from a snapshot draws dim until its refresh lands, so a stale "15" is
not read as fact. The queue hands the strip `(n, fresh)` in place of `n`.

Process cap: step 4 removes the 45 process burst. Keep the inbox's `slots` pool for
diffs and add one shared `gh` semaphore (width 6) in the process-wide exec helper,
so three tabs starting together cannot swamp the machine.

### Invalidation after the user acts

Approve, merge, comment, and checkout change a PR, and GitHub search is eventually
consistent, so waiting for the next search would show the old lane.

- After a successful action, delete that PR's facts record and fetch only it by node
  id (one `nodes(ids:)` call, about 1 s), then run the normal diff.
- A post or resolve evicts that PR from the conversations snapshot and refetches it
  alone.
- The existing refresh key bypasses Rule B and refetches everything.

## Design: reorder animation

Constraint from the code: `List.rebuild` already keeps the cursor on the same row
key once the user has moved (`touched`), so the cursor staying on one PR is done for
that case and stays a hard requirement. With single-line rows (queue-ui) a row moves
by whole lines, and a terminal is a poor fit for sub-line motion.

Options:

1. Multi-frame slide. Rows step toward their new index over 150 to 250 ms at about
   30 fps through `tea.Tick`. This is the FLIP technique (record the first position,
   apply the final layout, play from the old offset, named by Paul Lewis, described
   in [this overview](https://afasterweb.com/2015/12/31/flipping-animations)). It
   needs a layout model with offsets and a redraw per frame. On a one-line grid a row
   is on line N or it is not, so the motion is a few discrete hops that read as
   flicker. Costliest and least legible, so not the default.
2. Hold the reorder behind a hint ("3 updates, press r"). Stable and honest, but the
   screen is knowingly out of date, and the complaint is that opening is unclear.
3. Apply the new order at once and tint the rows that moved lane, moved rank, or are
   new for about 600 ms, then settle. One tick, four steps, no layout model.
4. Spring easing with [harmonica](https://github.com/charmbracelet/harmonica) on a
   per-row offset. The right tool only if option 1 were chosen.

Recommendation: option 3, plus an idle hold so it never fights the keyboard.

- Idle hold: if a key was pressed in the last 400 ms, queue the refreshed order and
  apply it at the next idle tick. Rows never shift in the middle of a burst of `j`.
  Only incoming data waits, never a key, so input is never delayed.
- Cursor: pinned by row key on every apply. An untouched cursor stays on the top
  row, which is the wanted behavior on open.
- Flash: each changed row gets `Flash = 4`. A `tea.Tick` every 150 ms decrements all
  flashing rows and returns nil when none are left. A generation number on the tick
  message drops ticks from an earlier apply, so overlapping refreshes never stack
  timers.
- Cancel: a keypress does not need to cancel the tick, because `Update` is constant
  time and the tick only changes a counter. Quitting or switching tab bumps the
  generation.
- No-op: an identical order flashes nothing. A cold open with no snapshot flashes
  nothing, since there is nothing to compare against.
- Reduced motion: under `NO_COLOR` the tint is off and a plain marker glyph shows for
  the same 600 ms.

I did not verify how lazygit or gh-dash behave on refresh, so I cite no prior art for
"keep position, mark change". Treat option 3 as my judgment.

## What the UI needs from this plan

The interface with `queue-ui.md` is three row fields and one strip change:

- `Row.Flash uint8`: 0 is settled, 1 to 4 is the decay step. The renderer maps it to
  an accent on a leading cell that does not depend on the selection band. This plan
  owns when it is set and the UI plan owns what it looks like.
- `Row.Stale bool`: the row came from a snapshot and has not been refreshed. Draw it
  normally, no spinner per row. It may dim the facts cells only.
- A row whose facts are not yet known draws a fixed width `··` placeholder in each
  facts cell, so the column grid never changes width when the facts land. Its lane is
  provisional, where the search put it.
- The tab strip takes `(count int, fresh bool)`.

## Testing

- Snapshot and cache: `cache.NewDiskCache(t.TempDir())`. Assert the round trip, a
  stale read past the TTL, schema drop, account switch, and 0600/0700 modes. No
  network and no sleeps.
- Incremental facts: a pure `Plan(snapshot, search) (reuse, fetch)` tested as a table
  with fabricated `updatedAt`, pending checks, and ages. On the `gh` side use the
  existing `ghcassette` seam and assert the facts call carries only the changed ids,
  built from `inbox.FactsArgs`, not a checked-in copy.
- Batched staged states: a hand-written cassette like `inbox.golden`, since a
  recording would hold real names. Assert one call for 45 rows.
- Scenes: add `demo/scenes/inbox-reorder` with a seeded snapshot and `routes` that
  answer the refresh with a moved row. It is how to look at the animation by hand,
  and `demo/bin/gh` already refuses anything unrouted, so it proves no extra calls.
- Animation: send the model its own `flashTickMsg{gen}` and assert the frame at each
  step with golden frames (`internal/tui/testdata/TestFrames/` style, `teatest` where
  it fits). One test for a stale generation, one for a key during a hold, one for no
  flash on a cold open. No `time.Sleep`.
- pty: one test in `cmd/second-look/tui_e2e_test.go` that opens against a seeded
  snapshot and asserts rows and all three counts are on screen before any `gh`
  answers (the stand-in stalls on purpose).
- Each bug found by hand becomes a test named for its trigger, per `AGENTS.md`.
- "The suite reaches nothing": `childEnv` already gives each child its own `HOME`,
  so the cache directory is per test and my real snapshot is never read.

## Steps

Each is one commit and one subagent, finished with `mise exec -- hk check --all`
plus the golangci-lint and bench commands from `AGENTS.md`. All are second-look
changes. No aragonite release is planned, and the one conditional one is named in
step 1.

1. Snapshot package. New `internal/snapshot/` with typed `Inbox`, `Conversations`,
   and `Staged` records over `cache.Persist` and `cache.PersistedStale` (upstream
   `queue`, schema version, viewer login). Measure the conversations snapshot
   against `diskMaxBytes`. Tests: temp dir round trip, stale read, modes, account
   mismatch. Risk: low. Aragonite only if the 4 MB cap is too small, as a
   `MaxBytes` option and tag in its own commit before this one.
2. Inbox paints from the snapshot. `inboxScreen.Start` loads it, fills buckets and
   facts, calls `regroup`, sets `Stale`, then runs the existing commands. Searches
   and facts write a fresh snapshot when they land. Files:
   `cmd/second-look/inboxscreen.go`, `queuescreen.go`. Measure the return from a
   review before and after. Tests: pty first-frame test and a seeded scene. Risk:
   medium (two sources of rows must not duplicate lanes). The first fresh answer
   replaces the rows wholesale.
3. Incremental facts. `internal/inbox/plan.go` with `Plan`, `Fetch` for the `fetch`
   set only, merge into the snapshot's facts map, add `headRefOid` to the query.
   Probe first: poll one PR with a running check and record whether `updatedAt`
   moves, then keep or drop Rule A. Tests: `Plan` table, cassette asserting changed
   ids only. Risk: medium. A wrong reuse shows a stale lane, which Rule B bounds at
   10 minutes.
4. Batched staged states. `internal/prstate`: one aliased query chunked at 50,
   called from `reviewsscreen.askAll`. Tests: hand-written cassette, one call
   asserted. Risk: low. The alias limit is unmeasured, so the step runs a real
   query of 63 aliases first.
5. Conversations incremental. Time the light query. Under 1.5 s, split into light
   plus heavy-by-id and merge into the snapshot. Otherwise keep one query and add
   only the snapshot paint. Both paths add the notifications 404 memo. Files:
   `internal/conversations/`, `cmd/second-look/threadsscreen.go`. Tests:
   `queueCassette` extended and a light reply decode. Risk: medium.

   Built as the split. The light query answered in 1.0 to 1.1 s for 40 pull
   requests (5 KB). The heavy read by node id is cheapest in parallel chunks:
   40 ids took 5.4 s in one call and 2.7 to 3.0 s as four calls of ten, so the
   heavy read is always chunked by ten, cold included. One id took 0.9 to 1.0 s.
   With a snapshot and nothing moved the tab confirms in the light query's time
   (1.2 s against 5.1 s before), and a record older than ten minutes is fetched
   again, the same bound the inbox uses for what `updatedAt` does not carry. The
   notification stream's 404 is remembered for a day in the snapshot. Unverified:
   whether a thread resolved on another device moves the pull request's
   `updatedAt`, which is what the ten minute bound covers.
6. Eager start and counts. `tui.Tabs.StartAll`, `(count, fresh)` for the strip, the
   shared `gh` semaphore. Files: `internal/tui/tabs.go`, `queuescreen.go`, the exec
   helper. Tests: strip golden frames for fresh and stale, and no tab starts twice.
   Risk: medium (`Loader.Start` assumes first look). Coordinate the strip rendering
   with `queue-ui.md`.
7. Reorder diff and idle hold. A pure `Moves(old, new []rowKey)` in `internal/tui`
   and the 400 ms hold in `List`, with no visuals yet. Tests: a table for moves
   (lane change, rank change, new, removed), a hold test with an injected clock,
   cursor pinned by key. Risk: low.
8. Flash. `Row.Flash`, the generation-numbered `flashTickMsg`, the four step decay,
   and the reduced-motion path. The final look is `queue-ui.md`'s, and this step
   ships a minimal default so it works alone. Tests: golden frames per step, stale
   generation, cold open flashes nothing, the `inbox-reorder` scene. Risk: low.
9. Post-action invalidation. After approve, merge, comment, and resolve, evict and
   refetch that one PR. Files: `inboxscreen.go`, `threadsscreen.go`, `perform`.
   Tests: cassette for a single-id refetch after a merge. Risk: medium (touches
   every action path).
10. Optional: notifications conditional requests. Store `ETag` and `Last-Modified`
    in the conversations snapshot, send them with `gh api -i -H`, treat `304` as
    unchanged. My token gets a 404, so this needs a working token or a hand-written
    cassette, and ships only if one exists.

Why this order: 1 to 3 give the instant open and the biggest cut, since the first
frame comes from disk and confirmation drops from about 9.6 s toward the search
floor of about 3 s plus about 1 s of facts. 4 and 6 fix tabs 2 and 3. 7 and 8 make
the reorder legible. 9 keeps actions honest. Those after-numbers are estimates until
each step reruns the shim.
