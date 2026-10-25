# Roadmap

What is next, in the order I would take it, then the questions that wait on use. The
reasons behind the current shape are in [DESIGN.md](DESIGN.md), and what shipped is in
[CHANGELOG.md](CHANGELOG.md).

The goal is twenty-five reviews in one sitting with no browser: read every pull request
waiting on me, judge the bot findings, hand fixes to an agent, and post.

## Next

1. Use the agent pane and the verdict loop on real queues. Both agent adapters (`devin acp`
   and the Claude shim) ran full turns, and what is left is a week of daily use to find
   what the loop gets wrong
2. Move to the next repository without leaving the queue. Focus holds one repository, but
   ending one and starting the next takes three keys (clear, move, focus). The motion
   needs a key of its own
3. Draw a stack as a stack in the inbox. The `reviews` tab already orders staged
   reviews bottom first, and the inbox lists a stack as unrelated rows, so the top gets
   read before the bottom
4. Extract the definitions-and-usages index. `gd`, `gr`, and `gc` ask a language server
   today. If real reviews show the shape is right, the index moves to a shared
   `codeintel` package in aragonite, and comment completion can then offer every symbol in
   the repository rather than those in the diff
5. Add blast radius to the rating as a third input. Import graphs overcount and dynamic
   imports undercount, and the scan needs a checkout, so it is cached by base SHA and
   skipped where there is none
6. Put images in inline review comments once `gh` allows it. `gh --attach` covers pull
   request comments only, and the upload endpoint behind it is undocumented. Rendering an
   image in the terminal is separate and has no blocker

## Not scheduled

- GitLab. The forge layer is an interface that GitHub alone has exercised
- Checkout under jj, since aragonite moves a clone with `gh pr checkout`, which is git only
- Issues beside pull requests, which is what gh-dash has that this does not. It waits for a
  gap I hit rather than parity
- Context from TLR and GitHub Issues, fetched when asked for
- Opt-in checkers that run when asked: semgrep, a traceback resolved against the code
  under review, a check generated from the ticket. What is missing is the rule for when
  one is worth running
- Which files count as lockfiles, as config. Eight formats are read today, and "popular
  alternatives" has no definition I trust
- An nvim front end over the same CLI

## Waiting on use

Each of these is a decision I would rather make from `second-look journal` after a week of
the queue than from memory now.

- Which of the `A` questions earn a key. `p` (reading path), `c` (a hunk decided), and `d`
  (the diff against what it claims) share one chord on purpose. A card that never shortens
  a hunk's reading and a drift check that only ever agrees are the delete candidates
- Whether the head check that gates the first frame is felt. A staged review draws only
  after GitHub confirms the head, and offline costs one round trip
- Whether `$EDITOR` on the whole file (`gf`) beats growing context with `+` and `-`
- The rating's weights. `internal/rate` pins the order of cases and not the numbers, so a
  week of queues should say whether the gap between 38 and 51 means anything
- Whether `inbox --json` should rate a queue of under twenty rows. It answers
  `rated: false` there today to avoid a read per row
- Whether the session features earn building: a cutoff line, hand-placed rows, a
  notification at a boundary, a recently opened list, and a checkout indicator. Each
  encodes a decision the shell alone does not settle. `demo/scene.sh` opens each one on
  seed data
- Whether a review with no comments needs its own state, so "an agent looked and found
  nothing" reads differently from "nobody opened it"
- Whether the participating-only notification filter is the right cut
- Whether the rich renderer's changed-line band is too light on a truecolor terminal
- Whether a release should mark a version or a day's work, because the Bump Version
  workflow cuts one per push to main
- Whether the rating moves to aragonite. It does if a second tool wants it
- Which of the two agent-guidance homes points at the other. Queue guidance lives in both
  `second-look skill` and the private `change-review` skill
