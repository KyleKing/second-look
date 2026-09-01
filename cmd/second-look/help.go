package main

const shortHelp = `second-look — prepare a code review locally, then post it in one call.

  <pr> is 42 for this checkout's repository, or owner/repo#42, or a PR URL.

  second-look get <pr>             fetch the PR, prepare the review, check it out
  second-look comment add <pr>     stage comments from a JSON batch on stdin
  second-look show <pr>            print the prepared review
  second-look show <pr> --payload  print exactly what would be sent
  second-look show <pr> --threads  print the open review threads and their ids
  second-look post <pr>            post the review
  second-look post <pr> --dry-run  print the request without sending it
  second-look post <pr> --only <id>  post one comment on its own, now
  second-look inbox                the review queue, in your own sections
  second-look threads              conversations that moved since you looked
  second-look reviews              what is staged locally under .second-look/
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

NAMING A PULL REQUEST

  Every command below takes <pr> in three shapes. 42 is this checkout's own
  repository. owner/repo#42 and a pull request URL name any other, which is what
  makes reviewing one you have no clone of possible.

  A review of a repository this directory is not a checkout of keeps its state
  under the user config directory instead of in a working copy, one directory per
  repository, and second-look reviews lists both.

  second-look <pr>
      Open the review screen: the diff with the prepared review's comments
      inline, where they anchor. m then r, d, or x marks a comment ready,
      draft, or skipped, e writes one where it sits, and S submits the review.
      Press ? for every key.

      It needs no checkout. The diff, the open threads, and the comment id a
      reply carries all come off the API, so a pull request is read, triaged,
      answered, and posted with no working copy at all. What a tree adds is
      reading around the change and running it, which is what C and ! are for.

      C moves the working copy onto the pull request, asking before it stashes
      anything, and draws the screen again. It is offered while standing in a
      checkout of the same repository on another branch. Cloning is never done
      for you: a repository with no clone here is reviewed from the API, and C
      says so.

      Moving is a grammar rather than a key per destination: ] or [ followed by
      h, f, c, t, or u goes to the next or previous hunk, file, comment, thread,
      or unread hunk, n repeats that motion and N reverses it, and . repeats the
      last change. So triaging a review reads "]c" then "n . n . n .", and
      reading one through reads "]u" then "n" until nothing answers.

      M squash-merges the pull request and deletes its head branch, M again to
      confirm. It refuses while anything is still staged, since a review left
      behind by a merge is work nobody will post.

      P posts the comment under the cursor on its own, now, and takes it off
      the review. It asks nothing first: a single comment is small enough to
      take back by deleting it on GitHub, where a whole review is not.

      w hides hunks that change nothing but whitespace and says how many it
      hid, and takes them out of the read count too, since a hunk nobody is
      being asked to read should not hold the count short of its total. A
      re-indent, a tabs-to-spaces pass, and a trailing-whitespace strip all
      count as whitespace; a line that gained a character does not.

      t hides more: every hunk a parser says changed no code, which adds a
      re-wrap across line boundaries and a comment nobody has to re-read. The
      grammars come from ast-grep, so t says so where that is not installed and
      w keeps working either way. The same pass rates the change, which is the
      cost in the title bar: a signature change outweighs anything a body does,
      a new capability the change reaches counts, and size is the tiebreaker.

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

      e opens an editor in the frame, over the block it is writing, so the line
      being answered stays on screen: ctrl+s saves, esc abandons, and ctrl+e
      hands what is typed to $EDITOR for the edits a text box is the wrong
      shape for. It writes a comment, an answer to an open thread, and the
      review's own body and note, which are rows the cursor lands on.

      z folds what the cursor is on: a whole file from its name, one hunk from
      anywhere inside it, and a comment's note from the comment. za inverts it,
      zR opens everything, and zM folds to the file names, which is the outline
      a long review is read from. A note over two lines starts folded, since it
      carries the evidence for a comment rather than the comment.

      E edits a comment's local note, and ! hands the terminal to $SHELL in the
      repository and appends what the session printed to that note. Running the
      code under review and then writing the comment is the flow that exists
      for; the note never posts, so a transcript stays on this machine. It
      refuses while the checkout is on another branch or missing, since a shell
      there would run against something other than the diff.

      Conversations already open on the pull request are shown where they
      anchor, and e on one writes the answer in $EDITOR, staged as a reply.

      It creates the prepared review and caches the diff if they are missing,
      and moves the working copy only when C asks. A review staged against an
      older head is refused rather than shown beside a diff it was not written
      against.

  second-look
      The same, for the pull request the current branch belongs to. A branch
      with no pull request is an error, not a guess.

  second-look get <pr>
      Read the pull request, write the prepared review, and cache the diff under
      .second-look/diff/. Inside a checkout of the repository it also moves the
      working copy onto the pull request head. Run this first: every later
      command reads the head commit and the diff it leaves.

      It also caches the pull request's unresolved review threads under
      .second-look/threads/, which is what the review screen shows and answers.
      A resolved or outdated thread is skipped: an outdated one anchors to a
      line the diff no longer carries.

      It never clones, and it needs no checkout: a repository with no clone here
      has its review, diff, and threads written under the user config directory
      instead. A dirty working tree stops a move, and on a terminal it
      asks first: answer yes and the work is parked with git stash, which
      "git stash pop" brings back. A run nobody is watching is never asked and
      never moved. Already being on the pull request head is fine however dirty
      the tree, since refusing to review a branch you already have would be
      wrong.

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
      The review queue, as the sections your config names or the three built-in
      buckets when it names none: pending your review, reviewed and still open,
      then reviewed and merged. Each line carries the repository, the author,
      how stale it is, and the title, which is enough to triage without opening
      anything. A terminal gets the screen and a pipe or --json gets the text.

      enter reviews the pull request under the cursor, C moves a checkout onto
      it, m comments on the pull request itself in $EDITOR, A approves it (A
      again to confirm), and o opens it on GitHub. Opening one needs no clone of
      its repository, which is the point: getting to a review costs an API read
      rather than a clone and a branch switch.

      Merging is not here. It is M in the review screen, where the diff has been
      read by the time the key is reachable.

      It reads GitHub and nothing local, so it works from anywhere gh is logged
      in rather than only inside a checkout. One bucket failing prints its
      reason and leaves the others: a rate limit on the merged list is no reason
      to stop showing what is waiting.

  second-look threads [--json]
      The conversations across your open pull requests that are yours to answer,
      in three buckets: what moved since you last looked, what is still waiting
      on you, then what is waiting on somebody else. A terminal gets the screen
      and a pipe or --json gets the text, so an agent and a person run the same
      command.

      A conversation is yours when the pull request is yours, when you have
      commented in it, or when a comment names you. Three surfaces count: inline
      review threads, the pull request's own comments, and the bodies submitted
      reviews carry.

      R marks a conversation dealt with, which always means a thumbs-up and,
      on a thread, the resolve as well. The reaction is the marker a person
      recognizes and the only one a pull request comment or a review body can
      carry, since GitHub gives neither a resolve. It goes on the comment that
      opened the conversation rather than on the last reply to it.

      So a resolved thread is gone from the queue, and so is anything you have
      already thumbs-upped.

      A machine account reaches the queue only through an inline review thread.
      That is the one surface where what a bot says is anchored to code and can
      be resolved; its pull request comments are status posts nobody ever
      resolves, so admitting them would fill the queue with rows that never
      leave.

      "New" means new to you, not new to GitHub. What you have read is kept
      per conversation in the user config directory rather than in a repository,
      because the queue spans repositories. enter reads a conversation and marks
      it; space marks one without opening it. Which bucket a row is in is fixed
      while the screen is open, so marking one read does not move it out from
      under the cursor.

      r stages a reply. The answer is written in the review screen, which is
      where a threaded reply already lives, so r leaves this screen and opens
      that one. Standing somewhere else is fine, whichever repository the
      conversation is on: the checkout moves onto the pull request on the way,
      asking before it stashes anything.

      A pull request in another repository is found by asking gh-repo-dashboard
      which clones of it are on this laptop ("gh repo-dashboard --cli", read
      from its cache, so no network). One answer is used and several are offered
      best first: already on the branch, then clean, then one that would need a
      stash. None, or a dashboard too old to answer, means the review opens from
      the API with its state under the user config directory.

  second-look reviews [--json]
      List the reviews staged under .second-look/ in this checkout, newest
      first. A terminal gets a screen where enter opens one, and a pipe or
      --json gets the text. Opening one you are not standing on moves the
      checkout onto it, asking before it stashes anything.

      Everything it prints is unfinished by definition: the artifact is deleted
      the moment a review posts. Each row says what the review holds and what
      state it is in -- blocked when a comment is still a draft, which stops the
      submit -- and a file that no longer parses is listed with the reason
      rather than skipped.

  second-look skill
      Print the instructions for an agent driving this binary, as a skill file
      ready to write to a skills directory. It says what this help does not:
      which commands an agent should never run, and how to use the local fields.
      Read it, or install it with

        second-look skill > ~/.claude/skills/second-look/SKILL.md

CONFIG

  ~/.config/second-look/config.toml, or $XDG_CONFIG_HOME/second-look/config.toml.
  It sits beside gh's and gh-dash's rather than in the platform's own config
  directory, because a person writes it by hand and keeps it in their dotfiles.
  Everything second-look writes for itself stays in the platform directory.

  Optional. Without it the inbox shows the three built-in buckets.

    limit = 25                        # rows per section, 30 by default

    [[section]]
    name = "needs my review"
    query = "review-requested:@me is:open archived:false sort:updated-desc"

    [[section]]
    name = "my work"
    query = "author:@me org:acme is:open archived:false sort:updated-desc"

  A query is gh search prs terms, which is what GitHub's own search box takes.
  A sort: qualifier becomes gh's --sort and --order, and a query naming no
  subject is scoped to what involves you, so a query written for gh-dash answers
  the same way here. Sections replace the built-in buckets outright: merging the
  two would put rows in front of you that no query asked for.

  A file that exists and says something wrong is reported and the built-in
  buckets are used, so a typo leaves a working queue. An unknown key is refused
  for the same reason it is in a prepared review: a misspelled key does not do
  what its author meant.

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
