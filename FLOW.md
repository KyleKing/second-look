# The flow

One sitting, twenty-five reviews, one repository at a time. This file is the whole
motion from opening the queue to posting the last review, what each step already does,
and what it needs that nobody has built. Why the tool is shaped this way lives in
[requirements.md](requirements.md), the screens and the keymap in [DESIGN.md](DESIGN.md),
and the ordered work list in [NEXT_STEPS.md](NEXT_STEPS.md), which points here for
anything the inbox owes.

The queue works today and reading it feels like eighty unrelated errands. Every row is
a different repository, so every row is a different set of conventions to remember, a
different clone to find, and a different question to ask an agent that knows none of it.
The fix is to make the repository the unit of a sitting rather than the pull request:
one repository in focus, one checkout leased for it, and one agent conversation per pull
request inside it.

## Where it stands

| Step | Built | Missing |
| --- | --- | --- |
| Open on what is owed | three buckets or configured sections, drawn as each search lands | nothing |
| Narrow to one repository | `f` focuses the cursor row's repository across all three tabs, `F` clears it | the clone it would use in the header, and a motion to the next repository |
| Take the ordering advice | `inbox.Rank`, and `inbox --json` carries it | why a row sits where it does, `s` to sort another way, and the stack drawn as a stack |
| Get a checkout | `C` where the cwd is a clone of that repository | the `internal/checkouts` ranking, a lease, and a column saying which clone is free |
| Stage the batch | `get` with no clone, prefetch ahead of the cursor, `reviews --json` | nothing |
| Ask Claude Code | `T` writes the todo set and runs `dispatch` one-shot | a conversation that outlives one question, and a key that asks one |
| Read and answer | the review screen, the conversation queue, notes, threads | the narrative pass, which is its own problem |
| Post and move on | `S`, and leaving a review returns to the queue | nothing |

Five of eight steps are done. The two left in the middle, the lease and the agent, are
what a sitting actually spends its time on.

## 1. Open on what is owed

`second-look` opens the inbox tab and sends every search off at once. Nothing to add.

## 2. Focus one repository

`/` narrows by a word and it is the wrong shape for this. It is per-list, it is lost on a
refresh, and it says nothing to the rest of the program: the checkout `C` takes and the
agent `T` starts have no idea a filter is on.

So focus is session state rather than a filter. `f` on a row focuses its repository and
`F` clears it. While it is on, all three tabs show only that repository, switching tabs
and refreshing keep it, the handoff that closes the screen to open a review brings it
back, and the header carries it beside how much of the queue is left. A row standing for
a search that failed names no repository, so it survives the narrowing: the reason a
section is short is the one thing this must not hide.

Two things it still wants. The header should say which clone it would use and what state
that clone is in, which is step 4's to add. And a motion to the next repository in the
queue is how a sitting ends one and starts the next, where today that is `F`, a move, and
`f` again; `]` and `[` already switch tabs, so the motion needs a key of its own rather
than the repository object.

Focus is what makes the lease and the agent single-valued. Without it both are per-row
and the laptop cannot honour either: of six clones of one repository here, one is clean.

## 3. Take the ordering advice, and see why

`Rank` puts what was started first, then the cheapest rated, then the oldest, with
drafts under everything. The order is right and the screen never says why, so it reads
as arbitrary and gets second-guessed. Two additions:

- the cursor row's placement, in the footer: `started · rated 38 · 4d old`. One line,
  drawn for the cursor only, so eighty rows cost nothing
- `s` cycles triage, cost, age, and size. The mock promised it and the screen never had
  it. Size sorts only where it is asked for, because sorting on it by default is the
  line count the rating exists to replace

Stacks are the third thing the mock promised. `reviews` already knows the chain from the
branches `get` records, so the inbox can draw a stack once both ends are staged, indented
bottom first. Until then a stack reads as unrelated rows and the top one gets reviewed
against changes nobody has seen.

## 4. Lease a checkout

`C` today calls `get.Resolve` against the cwd and refuses when the cwd is a checkout of
something else, so the key works for one repository per terminal. `internal/checkouts`
already ranks every clone and worktree of a remote by on-branch, then clean, then
needs-a-stash, and it is wired only into the threads reply path.

What the step needs:

- `C` asks `checkouts.Find` for the focused repository rather than reading the cwd, and
  moves the best-ranked clone, asking before it stashes, which is the question
  `get.Prepare` already knows how to ask
- a clone column in the header while focus is on, saying which clone it would use and
  what state it is in, so `C` is not a surprise
- a lease, held for the focused repository and released when focus moves, written where
  a second second-look and a dispatched agent can both see it. Two agents claiming the
  one clean clone is the failure this exists to stop
- the answer for a repository with no clone at all, which is the common case and is
  already handled everywhere else: read it from the API and say in the review's note
  that nothing was checked against the code

Only two things actually need a working tree: checking a finding that cites code outside
the diff, and running something to prove a claim about behaviour. Everything else reads
the API, which is why the lease is one per repository and not a queue-wide bottleneck.

## 5. Stage the batch

`get` per row with no clone, prefetch ahead of the cursor, and `reviews --json` for the
stack order. Built, and focus makes it cheaper: staging the focused repository's rows is
a bounded burst against one repository rather than eighty reads across forty.

## 6. Ask Claude Code

This is the step with the real open question, so the options are worth writing down.

`T` today writes the todo set to a file and runs `dispatch = ["claude", "-p"]` over it.
One-shot, no memory, one direction. Every question pays for the repository's context
again, an answer cannot be followed up, and there is nowhere for "you already read this
diff, now check the other call site" to go.

Four session models:

- **One-shot per question.** What exists. Cheapest to build and it cannot hold a
  conversation, which is the whole ask
- **One session per repository.** The context worth caching would be how the repository
  builds. Except that is what CLAUDE.md is, and every session reads it anyway, so the
  saving is small and the cost is real: a follow-up about #118 lands in a conversation
  about #91
- **One session per pull request, resumed.** The unit of the work is the pull request,
  and Claude Code models it the same way: `claude --from-pr` resumes the session linked
  to a pull request, `--bg` runs it detached and prints an id, `claude agents --json`
  lists what is live with a `state` on each, `claude attach <id>` hands a terminal to
  one, and `claude logs <id>` prints what it has said
- **The interactive session already open.** Cannot be driven from outside. The most this
  can be is second-look writing a file and the person pasting a path into their own
  session, which is where `T` started

So: the pull request is the session, it is resumed, and it runs in the background.

How it goes:

- first dispatch or first question on a row starts `claude --bg --name "review
  kyleking/tlr#118"` in the leased clone, and second-look records the session id in the
  prepared review, beside the note. The review artifact is already the per-pull-request
  file that outlives the screen
- every later `T` or question on that row resumes that id, so the agent knows what it
  already read and a second question is a follow-up rather than a restart
- `claude agents --json` is 0.65s and carries `state`, so the inbox draws an agent column
  from it: running, blocked, or done. A blocked agent is waiting on a permission prompt,
  which is the notification boundary [NEXT_STEPS.md](NEXT_STEPS.md) wants and never had a
  source for
- `a` attaches: the screen closes, `claude attach <id>` takes the terminal, and the queue
  comes back when it exits. This is the same handoff `C` and `m` already use, so it costs
  no new machinery
- posting or discarding a review takes the session with it, the way it already takes the
  diff, the threads, and the read marks

A question is a second key rather than a mode of `T`, for the same reason `T` is not a
mode of `S`. `T` hands work back and expects a change. A question expects prose and
changes nothing, so it runs the session with the tools that cannot write:
`--tools "Read,Grep,Glob,Bash"` where a claim needs proving, and `--restricted` where it
does not. Asking about a row with no review staged stages one first, because the answer
has to land somewhere and the review's note is where evidence already lives.

Where the config goes. `dispatch` is opaque today, a command with the file appended, and
that is the right instinct: second-look should not carry Claude Code's flag list. So the
session id becomes a placeholder rather than a hardcoded flag:

```toml
[agent]
start  = ["claude", "--bg", "--name", "{title}"]   # prints the id
resume = ["claude", "-p", "--resume", "{session}"]
ask    = ["claude", "-p", "--resume", "{session}", "--restricted"]
attach = ["claude", "attach", "{session}"]
list   = ["claude", "agents", "--json"]
```

Unset, `T` writes the file and names it, which is what it does now. That keeps the
default honest: starting an agent is not something to do on a keystroke nobody asked for.

The alternative is a `kind = "claude-code"` key with second-look owning every flag, which
reads better in the file and pins this repository to another tool's CLI. Placeholders win
because the only thing second-look actually needs back is one id.

## 7. Read the notes and answer the threads

Built, and the narrative problem is its own item in
[NEXT_STEPS.md](NEXT_STEPS.md). Focus adds one thing here: with a repository in focus,
the conversation queue is the same repository's threads, so answering and reviewing stop
being separate sittings.

## 8. Post and move on

`S` posts, and leaving a review returns to the queue on the row it came from. Built. The
end of a repository is `]` on the repository object, which releases the lease, and the
next repository's rows are already staged if prefetch reached them.

## What I would not decide here

- Whether focus is one repository or a set. A set is the honest shape for a monorepo org
  and it makes the lease ambiguous again, so start with one
- What a lease does when a review is left with drafts in it. Releasing loses the tree the
  drafts were written against, and holding blocks the next repository
- Whether the agent column costs a `claude agents --json` per refresh or a poll. It is
  local and it is 0.65s, so per refresh until that hurts
- Whether a question with no clone is worth asking at all. An agent with no working tree
  can read the diff and nothing else, which is a weaker answer that still beats none
