# Key usage audit (2026-11-14)

This audit reads the key log from 2026-10-14 through 2026-11-13. That covers about 26k presses, and the last session was on 2026-11-13. `second-look keys` reproduces it, and running it again later shows whether the changes below worked.

Lines from before 2026-11-14 came from the old journal, which recorded only `review` or `queue`. Keys pressed in the help legend, the outline, or on a thread were all counted under `review`, so for that period the overlay contexts show as unused and a few misses are really overlay keys. Line movement (`j`, `k`, `up`, `down`) is not counted.

## How a review goes

A typical session runs `tab` to the next item, `e` to open nvim, `m x` to skip, then `tab` again, and ends with `S a`. `m x` was pressed 41 times and `S a` 40 times. Almost everything else on the review screen is rare or never used.

## Keys pressed with nothing bound

These are the clearest signals, because each one is a key you reached for that did nothing.

- [ ] `left`/`right` on review, pressed 939 and 198 times. They are bound only inside a gc card (callers and callees). Decide what they should do on the diff: scroll sideways, switch sides in split view, or jump through the jump list
- [ ] `a q`, pressed 19 times. The question severity is `?`, and `q` cancels the chord. Bind `q` to question, and fix the stale comment near `write.go:32` that already says `q`
- [ ] `s` on the queue, pressed 20 times. `inboxHelp` lists it as "next row order" but nothing binds it. Bind it or drop it from the help
- [ ] Bare `x` on review, pressed 14 times. It only shows the "m first" hint. Because `m x` is the most used chord, consider letting bare `x` skip
- [ ] `:` on review, pressed 13 times (a vim reflex). Consider opening aragonite's `commandbar` here
- [ ] `u b` and `u p`, 4 each, and `S e`, `S f`, `S k`, `S m`, and `S q`, 1 or 2 each. Find out what you expected each one to do

## Never used

Each of these keys is either a candidate for removal or a sign that it is hard to find. On review:

- Writing: `F` fixme, `E` edit note, `V` range, `P` post one, `!` shell, `+`/`-` context
- Views: `O` order, `U` only new, `H` since a round, `X` trouble
- Chords: every `A` ask object, `m d`, `m t`, `a b`, `a m`, `a ?`, most `]`/`[` objects (`s`, `h`, `u`, `p`, `a`), `u g`, `u i`, `u v`, `z t`, `z b`, `z i`, and every `,` reaction except `t`
- Other: `.` repeat, `ctrl+r` restage, and `ctrl+i` forward

On the queue: `m`, `A`, `M`, `w`, `f`/`F`, `g c`, and `g s`.

Each one of these needs a decision: keep it and let the legend brighten it until you learn it, move it behind `:`, or delete it.

## Gone cold

These were used once or twice in the first days and not since: `g f`, `g r`, `N`, `M` merge, `u s`, `u w`, and `ctrl+y`. Each was tried and then dropped, so ask what replaced it before deciding.

## One key, different meanings

`keys` lists 24 overloads, and most are noise: synonyms such as "half page" and "page down" for `ctrl+d`. The ones that matter are `r` (reply on the queue, ready verdict on a thread), `u`/`U` (look chord on review, unread on the queue), `A` (ask chord on review, approve on the queue), and `m` (state chord on review, comment on the queue). These fire constantly in one place and are dangerous in the other. `A` on the queue approves, and it asks for a second press only as confirmation.

## Revisit

Run `second-look keys` again around 2026-12-14. The brightening levels start at 7 days, so a month of new data, logged with full context, will show which never-used keys got picked up.
