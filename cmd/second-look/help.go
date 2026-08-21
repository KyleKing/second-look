package main

const shortHelp = `second-look — prepare a code review locally, then post it in one call.

  second-look comment add <pr>     stage comments from a JSON batch on stdin
  second-look show <pr>            print the prepared review
  second-look show <pr> --payload  print exactly what would be sent
  second-look post <pr>            post the review
  second-look post <pr> --dry-run  print the request without sending it

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

  second-look comment add <pr>
      Read a JSON batch on stdin and stage it. A comment carrying an id that is
      already present replaces it; anything else is appended. The whole review is
      validated before anything is written, so a rejected batch changes nothing.

  second-look show <pr>
      Print the prepared review as JSON, local fields included.

  second-look show <pr> --payload
      Print exactly what would be sent. Use this to confirm what stays local.

  second-look post <pr> [--dry-run]
      Post the review in one request, then post any replies. Refuses while any
      comment is still a draft.

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
  comment:  id, note, severity, status, skip_reason

  A skipped comment is never posted and stays in the file, so a finding that was
  considered and declined reads as considered rather than forgotten.

WHAT IS REFUSED

  An unknown field, in the batch or in the file. A misspelled key is a hand-edit
  that will not do what its author meant, and a field the schema does not know is
  one the split cannot classify.

  A draft at post time. Mark it ready or skip it; second-look will not guess.

  A comment with no path, no positive line, or a side that is not RIGHT or LEFT,
  unless it is a reply. Every problem is reported at once, not just the first.

ANCHORS

  line is in the file's post-image when side is RIGHT and its pre-image when LEFT,
  which is GitHub's own convention. For a multi-line comment, start_line and
  start_side mark the first line and line marks the last.
`
