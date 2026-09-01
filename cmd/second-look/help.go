package main

const shortHelp = `second-look — prepare a code review locally, then post it in one call.

  second-look get <pr>             fetch the PR, check it out, prepare the review
  second-look comment add <pr>     stage comments from a JSON batch on stdin
  second-look show <pr>            print the prepared review
  second-look show <pr> --payload  print exactly what would be sent
  second-look show <pr> --threads  print the open review threads and their ids
  second-look post <pr>            post the review
  second-look post <pr> --dry-run  print the request without sending it
  second-look post <pr> --only <id>  post one comment on its own, now
  second-look inbox                the review queue, in three buckets
  second-look skill                print the agent instructions this binary carries

  --help  the full contract, including every JSON field
`

// longHelp is written for an agent with no other documentation. It states the
// fields, which of them post, and what is refused, because a tool an agent drives
// blind has to answer those three questions itself.
const longHelp = `second-look — prepare a code review locally, then post it in one call.

The prepared review lives at .second-look/pr-<number>.toml. TOML because a person
edits it; JSON at both edges because an agent writes it and gh posts it.

Every field is either posted or local. Local fields are shown while reviewing and
never sent. The split is enforced by the payload builder, which reads only the
posted fields, so a local field cannot leak by being forgotten.

COMMANDS

  second-look <pr>
      Open the review screen: the diff with the prepared review's comments
      inline, where they anchor. Mark a comment ready, draft, or skipped, edit
      one in $EDITOR, and submit the review with S. Press ? for every key.

      Moving is a grammar rather than a key per destination: ] or [ followed by
      h, f, c, t, or u goes to the next or previous hunk, file, comment, thread,
      or unread hunk, n repeats that motion and N reverses it, and . repeats the
      last change. So triaging a review reads "]c" then "n . n . n .", and
      reading one through reads "]u" then "n" until nothing answers.

      P posts the comment under the cursor on its own, now, and takes it off
      the review. It asks nothing first: a single comment is small enough to
      take back by deleting it on GitHub, where a whole review is not.

      w hides hunks that change nothing but whitespace and says how many it
      hid, and takes them out of the read count too, since a hunk nobody is
      being asked to read should not hold the count short of its total. A
      re-indent, a tabs-to-spaces pass, and a trailing-whitespace strip all
      count as whitespace; a line that gained a character does not.

      Files are grouped by the directory they sit in, which in a Go tree is one
      package, with the file and hunk counts on each heading so a directory can
      be taken or left as a unit. ]d walks the groups.

      c shows the comments alone, grouped by file with the counts on each
      heading and skipped ones counted rather than listed, and c again goes
      back to the same comment in the diff. It is the same rows either way, so
      every key works in both.

      / searches, and tab inside the prompt restricts the search to hunks not
      yet read, which is the question a second pass asks and no other reviewer
      answers. A committed pattern becomes the motion n repeats, so a search and
      a jump between hunks are walked with the same key.

      space marks the hunk under the cursor read, or the whole file from a file
      line. What is read is kept in .second-look/seen/ against the hunk's
      content rather than its position, so a force-push that does not touch a
      hunk leaves it read.

      E edits a comment's local note, and ! hands the terminal to $SHELL in the
      repository and appends what the session printed to that note. Running the
      code under review and then writing the comment is the flow that exists
      for; the note never posts, so a transcript stays on this machine.

      Conversations already open on the pull request are shown where they
      anchor, and e on one writes the answer in $EDITOR, staged as a reply.

      It creates the prepared review and caches the diff if they are missing,
      and never moves the working copy: the checkout has to already be on the
      pull request head, which "gh pr checkout <pr>" or "second-look get <pr>"
      does. A review staged against an older head is refused rather than shown
      beside a diff it was not written against.

  second-look
      The same, for the pull request the current branch belongs to. A branch
      with no pull request is an error, not a guess.

  second-look get <pr>
      Read the pull request, move the working copy onto its head, write the
      prepared review, and cache the diff under .second-look/diff/. Run this
      first: every later command reads the head commit and the diff it leaves.

      It also caches the pull request's unresolved review threads under
      .second-look/threads/, which is what the review screen shows and answers.
      A resolved or outdated thread is skipped: an outdated one anchors to a
      line the diff no longer carries.

      It never clones, and it never moves a dirty working tree. Already being on
      the pull request head is fine however dirty the tree, since refusing to
      review a branch you already have would be wrong.

  second-look comment add <pr>
      Read a JSON batch on stdin and stage it. A comment carrying an id that is
      already present replaces it; anything else is appended. The whole review is
      validated before anything is written, so a rejected batch changes nothing.

  second-look show <pr>
      Print the prepared review as JSON, local fields included.

  second-look show <pr> --payload
      Print exactly what would be sent. Use this to confirm what stays local.

  second-look show <pr> --threads
      Print the pull request's unresolved review threads as second-look get last
      read them, each with the comment id a reply addresses. Put that id in a
      comment's in_reply_to to answer the thread.

  second-look post <pr> [--dry-run]
      Post the review in one request, then post any replies. Refuses while any
      comment is still a draft. On success the prepared review is removed:
      GitHub is the source of truth from that point and re-running post would
      publish a second copy.

  second-look post <pr> --only <id>
      Post one comment on its own, outside any review, for the finding worth
      saying now rather than at the end. The anchor guard runs first, the rest
      of the review stays staged, and the comment is taken out of the file
      because GitHub owns it from that moment.

  second-look inbox [--json]
      Print the review queue in three buckets, in the order they want doing:
      pending your review, reviewed and still open, then reviewed and merged.
      Each line carries the repository, the author, how stale it is, and the
      title, which is enough to triage without opening anything.

      It reads GitHub and nothing local, so it works from anywhere gh is logged
      in rather than only inside a checkout. One bucket failing prints its
      reason and leaves the others: a rate limit on the merged list is no reason
      to stop showing what is waiting.

  second-look skill
      Print the instructions for an agent driving this binary, as a skill file
      ready to write to a skills directory. It says what this help does not:
      which commands an agent should never run, and how to use the local fields.
      Read it, or install it with

        second-look skill > ~/.claude/skills/second-look/SKILL.md

BATCH SHAPE (stdin to second-look comment add)

  {
    "note":  "review-level private note, replaces the existing one",
    "body":  "review-level comment, posted",
    "event": "COMMENT | APPROVE | REQUEST_CHANGES",
    "comments": [
      {
        "id":       "stable across edits, yours to choose",
        "path":     "internal/vcs/diff.go",
        "line":     16,
        "side":     "RIGHT | LEFT",
        "start_line": 0,
        "start_side": "",
        "body":     "the exact text to post",
        "in_reply_to": 0,
        "note":     "why this comment exists: evidence, the command that proved it",
        "severity": "blocker | major | minor | nit | question",
        "status":   "ready | draft | skip",
        "skip_reason": "required when status is skip"
      }
    ]
  }

WHAT POSTS

  review:   body, event, head_sha (as commit_id)
  comment:  path, line, side, start_line, start_side, body
  reply:    body, to the comment named by in_reply_to

WHAT STAYS LOCAL

  review:   note
  comment:  id, anchor, note, severity, status, skip_reason

  A skipped comment is never posted and stays in the file, so a finding that was
  considered and declined reads as considered rather than forgotten.

WHAT IS REFUSED

  An unknown field, in the batch or in the file. A misspelled key is a hand-edit
  that will not do what its author meant, and a field the schema does not know is
  one the split cannot classify.

  A comment on a line the diff does not carry, start_line included. A line
  number invented out of nothing is the most common failure in this workflow,
  and GitHub refuses the comment anyway, so it is caught while staging rather
  than on the wire. A multi-line comment whose two ends fall in different
  hunks is refused for the same reason.

  A diff that carries a file more than once, which is a per-commit patch
  series rather than the pull request's diff. Its line numbers belong to an
  intermediate commit, so no anchor into it can be trusted.

  A comment whose diff line has changed since it was staged, and a post against
  a pull request that has new commits. Re-run second-look get and re-read them.

  A draft at post time. Mark it ready or skip it; second-look will not guess.

  A review with no body and no comments left to post, which is what skipping
  every comment leaves behind. An APPROVE says something on its own and is
  allowed to be empty; a COMMENT carrying nothing is a keystroke nobody meant.

  A comment with no path, no positive line, or a side that is not RIGHT or LEFT,
  unless it is a reply. Every problem is reported at once, not just the first.

ANCHORS

  line is in the file's post-image when side is RIGHT and its pre-image when LEFT,
  which is GitHub's own convention. For a multi-line comment, start_line and
  start_side mark the first line and line marks the last.

  Staging quotes the diff line an anchor points at into the comment's anchor
  field, and posting compares that text against the live diff byte for byte.
  The quote is second-look's, not yours: it is overwritten on every stage.
`
