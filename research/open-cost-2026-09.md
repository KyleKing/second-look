# What opening a review costs

**Kyle's bot did stuff and wants it available for future bots.** Findings from an agent session with minimal human review. Validate every claim before building on it.

## Method

Benchmarks live in [`internal/diff/bench_test.go`](../internal/diff/bench_test.go) and
[`internal/tui/open_bench_test.go`](../internal/tui/open_bench_test.go), plus two new
exports in [`internal/tui/export_test.go`](../internal/tui/export_test.go)
(`Relayout` calls `rebuild`, `Restructure` runs and applies the real structural pass).
They follow the shape of the existing `BenchmarkRichFrame` family in
[`internal/tui/bench_test.go`](../internal/tui/bench_test.go): build a synthetic patch,
construct a `*tui.Model` with `tui.New` and `m.Init()`, then measure. All use `b.Loop()`
(Go 1.25 here) and `b.ReportAllocs()`.

Commands run:

```sh
go test ./internal/diff/... -bench=. -benchmem -run=^$
go test ./internal/tui/... -bench='BenchmarkOpen' -benchmem -run=^$
mise run bench
go test ./internal/tui/... -bench='BenchmarkOpenRebuild/large' -benchmem -run=^$ \
  -memprofile=/tmp/rebuild.mprof -benchtime=20x
go tool pprof -top -alloc_space -nodecount=15 internal/tui/tui.test /tmp/rebuild.mprof
```

Machine: Apple M4 Pro, `GOMAXPROCS`-equivalent of 12.

## Sizes, and where they came from

Three sizes, each a patch of N files with L added-only lines apiece (no context lines,
so every file is one hunk, the worst case for hunk-splitting):

| name   | files | lines/file | total lines | anchor |
| ------ | ----- | ---------- | ----------- | ------ |
| small  | 5     | 40         | 200         | this repository's median commit is 170 changed lines (computed from `git log --numstat` over all 420 commits) |
| medium | 32    | 70         | 2,240       | this repository's own largest historical commit: 32 files, 2,198 insertions |
| large  | 100   | 200        | 20,000      | a lockfile-scale change. Two `package-lock.json` files on disk elsewhere on this machine run 1,113 and 2,933 lines; a full regeneration rewrites nearly every line of one. A dependency bump touching several lockfiles in a monorepo, or a sweeping rename, spreads that across many files rather than one, which is what 100 files targets |

The small and medium sizes are observed data, not guesses. Large is a deliberate
stretch beyond anything in this repository's own history, chosen to see whether the
open path stays fast well past realistic bounds before concluding it does.

## Results

### Parsing and refining (`internal/diff`)

| stage  | size   | time      | memory   | allocs |
| ------ | ------ | --------- | -------- | ------ |
| Parse  | small  | 9.4 µs    | 51.7 KB  | 56     |
| Parse  | medium | 76.0 µs   | 380 KB   | 303    |
| Parse  | large  | 0.77-0.80 ms | 4.32 MB | 1,119 |
| Refine | small  | 4.9 µs    | 37.6 KB  | 36     |
| Refine | medium | 34-36 µs  | 240 KB   | 225    |
| Refine | large  | 420-441 µs | 3.21 MB | 901    |

### Opening the screen (`internal/tui`)

`New` + `Init` (model construction, `rebuild`, and command setup, structural pass
excluded since it runs as an async `tea.Cmd` and isn't awaited here):

| size   | time       | memory   | allocs |
| ------ | ---------- | -------- | ------ |
| small  | 0.38-0.40 ms | 563 KB   | 1,404  |
| medium | 1.54-1.61 ms | 4.77 MB  | 3,095  |
| large  | 7.5-8.5 ms   | 47.45 MB | 7,837  |

`rebuild` alone (the row-layout pass that a fold, toggle, or order change also runs,
isolated from `New`'s own setup):

| size   | time       | memory   |
| ------ | ---------- | -------- |
| small  | 0.29-0.31 ms | 469 KB   |
| medium | 1.29-1.45 ms | 4.48 MB  |
| large  | 6.1-6.8 ms   | 44.18 MB |

One rendered frame at 80 and 120 columns, off the plain renderer (matches
`BenchmarkFrame`'s renderer, not `BenchmarkRichFrame`'s):

| size   | width | time       | memory  |
| ------ | ----- | ---------- | ------- |
| small  | 80    | 1.22-1.25 ms | 368 KB  |
| small  | 120   | 1.24-1.25 ms | 381 KB  |
| medium | 80    | 1.27-1.30 ms | 369 KB  |
| medium | 120   | 1.27-1.28 ms | 382 KB  |
| large  | 80    | 1.42-1.46 ms | 374 KB  |
| large  | 120   | 1.41-1.46 ms | 387 KB  |

Frame time barely moves across a 100x range in total diff lines (200 to 20,000) and
memory per frame is flat. `rowLines` in `internal/tui/view.go` walks from `m.offset`
for `m.viewHeight()` rows and stops, so a frame costs what the visible window costs,
not what the diff holds. This is already windowed. It needs no help.

The structural pass (one `ast-grep` subprocess per hunk side, capped at 8 concurrent
in `internal/structure/batch.go`, followed by the `rebuild` it triggers on landing):

| size   | time         | memory  | allocs |
| ------ | ------------ | ------- | ------ |
| small  | 6.3-8.8 ms   | 874 KB  | 1,962  |
| medium | 36.5-39.5 ms | 7.31 MB | 8,900  |
| large  | 155-187 ms   | 57.1 MB | 39,183 |

This one varies run to run (subprocess scheduling noise), unlike everything else
above, which is stable to within a few percent across repeated runs.

### Where the memory goes

A `-memprofile` on `BenchmarkOpenRebuild/large` (20 runs, `-alloc_space`):

```
      flat  flat%   sum%        cum   cum%
  521.22MB 57.89% 57.89%   884.87MB 98.28%  internal/tui.build
  350.63MB 38.94% 96.84%   358.64MB 39.83%  internal/tui.(*pairer).emit
    4.51MB  0.50% 97.34%     4.51MB  0.50%  runtime.mallocgc
    4.01MB  0.45% 97.78%     7.01MB  0.78%  internal/tui.commentRows
    2.50MB  0.28% 98.06%   362.64MB 40.28%  internal/tui.screen.fileRows
```

`tui.build` (row construction) and `pairer.emit` (side-by-side line pairing) account
for 96.8% of what `rebuild` allocates. The parsed diff itself, per `BenchmarkParse/large`
above, is 4.32 MB. `rebuild`'s row slice is 44.18 MB, about ten times that. The
diff structure is not what a large review holds in memory. The rows built from it are,
and per-line highlight spans (measured by the existing `BenchmarkRichFrame`, which
notes its own cost is a one-time lex kept in `m.lexed` and paid once per hunk drawn,
not per frame) are a smaller, on-demand cost layered on top.

## When does the first frame cross 100ms?

Time to first paint is Parse + (`New`+`Init`) + one Frame:

| size   | total       |
| ------ | ----------- |
| small  | ~1.6 ms     |
| medium | ~3.0 ms     |
| large  | ~10 ms      |

`New`+`Init` (dominated by `rebuild`) is the only stage of the three that scales with
diff size, at roughly 75-85 µs per file in the large case (8.0 ms / 100 files). Frame
draw is flat (windowed) and Parse is already under 1 ms at 20,000 lines. Holding that
per-file rate, reaching a 100ms first frame needs around 1,100-1,200 files at 200
lines apiece, on the order of 220,000-240,000 changed lines in one review. That is
past what a single pull request review is for. No diff this repository or the two
sampled lockfiles would produce comes close.

The structural pass is a different story. Its per-hunk cost is roughly flat across
hunk sizes too (small hunks of 40 lines cost about as much per hunk as large hunks of
200 lines: 1.1-1.9 ms/hunk across all three sizes), which says the cost is dominated
by spawning the `ast-grep` subprocess, not by what it has to read once it starts.
At that rate, 100ms crosses at roughly 65-70 hunks, a size several real PRs reach and
one this repository's own largest commit (32 hunks, at one hunk per file) is well
under. The large case here (100 hunks) is already 155-187ms.

That pass runs as an async `tea.Cmd` behind the first frame (`Init` in
`internal/tui/model.go` batches it alongside the head check and the file watcher), so
it does not block the first paint. What it does cost is a felt hitch when it lands
and triggers `applyStructure` → `rebuild`: for a diff with hundreds of hunks, at the
measured 1.2-1.9ms/hunk rate, that lands 300ms to 1s or more after the screen opens,
as everything reflows with symbols and moves it didn't have a moment before.

## Is a lazy-load or size limit warranted?

Not for parsing, model construction, row layout, or rendering. All four stay under
10ms combined at 20,000 changed lines and the extrapolated crossover for a 100ms first
frame is over 200,000 lines, a size no real diff sampled here approaches. Capping any
of these against diff size would trade correctness for headroom nothing is asking for.

The structural pass is the one place the numbers cross a threshold a reviewer would
notice at sizes real reviews reach. But "only structurally read what's visible" is
not available as a fix, because every feature that consumes the pass needs the whole
diff read first:

- `order.Plan` sorts the whole reading order by cost, so it needs every hunk's
  reading before it can rank any one of them
- move detection (`readShape` in `internal/tui/structural.go`) pairs a deletion in
  one hunk with an insertion in another by declaration key, so a symbol windowed out
  of view is a symbol that reads as deleted rather than moved
- cosmetic-hunk folding marks a hunk by what its own two sides parse to, across the
  whole diff, so folding is exactly the feature a partial read would break for the
  hunks not yet read
- search, `]f` navigation, and the per-hunk read counts all count or search over
  hunks the reader has not scrolled to yet

A window over the diff breaks all of these the moment the reader looks past what was
initially parsed. None of them tolerate a partial answer.

The cheaper intervention the data actually points at: the per-hunk cost floor of
1.2-1.9ms looks like subprocess-spawn overhead, not parse time, since it barely
changes between a 40-line hunk and a 200-line one. `internal/structure/read.go` spawns
one `ast-grep` process per hunk side. Batching several hunks into fewer invocations
(one process reading many fragments, rather than one process per fragment) would cut
that per-hunk floor without touching whole-diff correctness at all, since batching
changes how many processes run, not what gets read. Raising `workers` past 8 in
`internal/structure/batch.go` is the other lever in the same direction, bounded by
however many concurrent subprocesses is reasonable on a laptop.

If a size limit belongs anywhere, it belongs on the structural pass specifically
(skip it, or require an explicit key press, past some hunk count), sized around where
the async pass crosses 300-500ms of felt hitch, which by the measured rate is roughly
200-300 hunks. That is larger than this repository's own largest commit and smaller
than the synthetic large case here, so it is a real number, not a hypothetical one.
Nothing in the render or layout path needs a limit at any size seen here.

## Raising the worker count was tried and does not pay

The cheaper of the two levers above was measured rather than assumed. `workers` in
`internal/structure/batch.go` was raised from a fixed 8 to `runtime.NumCPU()`, which is
12 on the machine these numbers come from, and `BenchmarkOpenStructural/large`
(100 files, 100 hunks) was run three times at each setting:

| workers | ns/op, three runs | mean |
| --- | --- | --- |
| 8 | 139.6ms, 141.5ms, 155.2ms | 145ms |
| 12 (`runtime.NumCPU()`) | 149.5ms, 131.5ms, 147.7ms | 143ms |

Two milliseconds inside a 24ms spread is nothing. The change was reverted. Eight
concurrent `ast-grep` processes already saturate what the machine will give, so the
per-hunk floor is the process itself rather than the queue in front of it, and batching
several hunks into one invocation is the only one of the two levers left worth building.
