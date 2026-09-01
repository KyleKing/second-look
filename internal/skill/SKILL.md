---
name: second-look
description: Stage pull request review comments through the second-look CLI, where they are anchored to the diff and held for the user to proofread before anything is posted. Use when drafting review comments, replying to a review thread, or preparing a review for the user to post.
---

# second-look

Do not run `second-look <pr>` or `second-look` with no argument. Both open the review
screen, which belongs to the person at the keyboard, and an agent that runs one waits
forever on a terminal nothing is attached to. Everything below is a command that reads
stdin and writes stdout.

Run `second-look --help` before the first batch of a session. It is the contract: every
field, which of them post, what stays local, and what is refused. It ships in the same
binary as the code that enforces it, so it cannot be out of date. Do not work from a
remembered shape of the schema.

## The shape of the work

```sh
second-look get <pr>                       # check out the head, cache the diff
second-look show <pr>                      # what is already staged
second-look comment add <pr> < batch.json  # stage a batch
second-look show <pr> --payload            # exactly what would leave the laptop
```

Then the user reads the prepared review and posts it. Do not post on their behalf unless
they said so in this session.

`get` refuses to move a dirty working tree, so commit or stash when it says so. Already
being on the pull request head never blocks, however dirty the tree is.

## Anchors are checked, not trusted

Staging quotes the diff line each comment points at, and a comment on a line the diff
does not carry is refused with nothing written. This is what catches a finding that
cites line 993 of a 137-line file, whoever wrote it. Posting compares those quotes
against the live diff again and refuses if any moved.

That covers anchors inside the diff. Anything a finding cites outside the diff is still
yours to check against the checked-out code before you write a comment about it.

## Every comment gets a note

`note` is local and never posted. It is where the evidence goes: the command that proved
the finding and what it printed, the file that contradicts the claim, the reason for the
doubt. `body` carries only what the author reads, so reasoning that would clutter a
review comment goes in the note rather than being cut.

The user attaches evidence the same way from the review screen: `!` hands the terminal
to their shell and appends the transcript to the note under the cursor. So a note you
write is the start of that record, not the whole of it, and it should say what you ran
rather than reading as a finished argument.

The review's own `note` is the run log: what was run and what it returned, suites that
could not run and why, whether a bot already reviewed the pull request. It shows how much
of the review is proven rather than read.

## Everything you stage is a draft

Write `"status": "draft"` on every comment. `comment add` holds one anyway: a comment
staged as `ready` is written as a draft and the run says how many it held. Nothing you
write posts until the author has read it and marked it ready in the review screen, and
`post` refuses while any draft remains.

That is the point of staging through a file rather than posting. What you write is a
proposal about someone else's code, and the author rules on each one.

`skip` with a `skip_reason` is left as you wrote it, because it records a finding you
considered and declined, which is worth more than deleting it: it reads as considered
rather than missed.

`severity` is blocker, major, minor, nit, or question. It orders what the user reads
first.

## Replies

`second-look get <pr>` caches the pull request's unresolved review threads, and
`second-look show <pr> --threads` prints them with the comment id each one answers. Set
`in_reply_to` to that id and second-look sends the reply to its own endpoint.

Before replying to a bot thread, check whether a later commit already resolved it; close
a stale thread with a one-line pointer to the fixing commit instead of raising it again.

## Saying one thing now

`second-look post <pr> --only <id>` posts a single staged comment on its own, outside any
review, and takes it out of the file. It is for the finding that should not wait for the
rest of the review: a build broken for everyone, a secret in a diff. The anchor guard runs
first and the rest of the review stays staged. Ask before using it, the same as posting.

## Editing on a later pass

Reuse a comment's `id` and the edit replaces it; a new id appends a duplicate. Once the
user has hand-edited a comment, treat it as settled. A changed anchor or a factual
correction is fine. Rewriting their sentence is not.
