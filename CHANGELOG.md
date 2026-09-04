## v0.8.0 (2026-09-04)

### Feat

- **tui**: group the legend and place the frame with zz, zt, zb
- **reviews**: mark the rows this checkout can reach and move it onto one
- **cli**: open the queue where a branch names no review
- **tui**: grow the truncating columns into a wide frame

### Fix

- **tui**: report a boundary when tab finds nothing further
- **queue**: come back to the row and filter a review was opened from
- **artifact**: leave a twice-staged review rather than refusing every other one

## v0.7.0 (2026-09-04)

### Feat

- **inbox**: carry the triage order and the rating into the piped queue
- **reviews**: carry the stack order into the piped list
- **review**: cut the frame down to what has to be acted on
- **review**: let a folded heading recede
- **queue**: say how large a change is beside what it is rated
- **review**: compare against an earlier round the review was read at
- **queue**: rate the row the cursor stops on
- **review**: pin the line a run answers and gather its skips
- **review**: select a range of lines to comment on or suggest over
- **queue**: discard a prefetched review the queue no longer holds
- **review**: take a head that moved without reopening the screen
- **review**: read every open conversation with the line it answers
- **review**: say when comments on one line are a run
- **demo**: drive every screen from seed data, not just the review
- **review**: read a bot comment as blocks instead of as raw markdown
- **review**: make a change carry on four channels, not one band
- **review**: grow a hunk with the file's own lines
- **review**: complete a word from the files, symbols, and people in it
- **review**: suggest a replacement, and say when an anchor drifted
- **review**: narrow to what is new, and keep asking what the head is
- **queue**: stage the next few reviews while you read this one
- **review**: take what an agent wrote without losing what you typed
- **review**: hand a comment back to an agent and keep the exchange
- **cli**: give an agent the diff and one comment's context
- **store**: keep every review in one place, keyed by repository
- **demo**: open a layout case from an editable review file
- **tui**: gather a caller with its callee across the diff
- **tui**: group what a machine wrote last, folded and counted
- **tui**: say what the parser saw on every heading
- **tui**: pair each edit onto one row side by side
- **tui**: let a hunk already read recede
- **tui**: draw the diff by its grammar behind v
- **tui**: center a comment when a motion lands on it
- **tui**: name the lines a ranged comment covers
- **inbox**: give the review-cost rating a column of its own

### Fix

- **get**: record the branches a pull request joins
- **rate**: bend the score onto the scale instead of clipping it
- **list**: keep the cursor on the row a verb acted on

## v0.6.0 (2026-09-02)

### Feat

- **inbox**: ask what the hourly allowance covers before rating a queue

### Fix

- **ci**: run the coverage task under bash and pin the queue test's config home

## v0.5.0 (2026-09-02)

### Feat

- **inbox**: order a long queue by what each diff costs to read
- **tui**: wrap a list under its own text and set the review's note apart
- **tui**: repeat a chord with .
- **reviews**: discard a staged review and collect what nothing needs
- **tui**: keep an edit that was left unfinished
- **tui**: keep the file name in the title once its heading scrolls off
- **tui**: write a comment on the line under the cursor with a
- **tui**: make a draft ready when its edit is saved
- **tui**: open notes by default, and fold a removal open where it stands
- **tui**: put the three queues in one screen as tabs
- **reviews**: group a stack of pull requests in the order it reads
- **tui**: show where the frame sits and which comment the cursor is on
- **tui**: open the pull request with o, and name what a submit posts as
- **inbox**: order a bucket for triage rather than by recency
- submit as an approval, and fill the queue in as it answers
- **tui**: narrow a queue with /
- **tui**: peek in the queues too, and record what the pass landed
- hold staged comments as drafts, mark the cursor with a bar, and peek
- **tui**: show the code alone as a third view
- **tui**: fold a file, a hunk, or a note, and bracket the keys
- **tui**: write a comment where it sits
- **tui**: give a comment a shape, and put its state behind a chord
- **rate**: rate a change by what its symbols and capabilities did
- **tui**: hide hunks that change no code, not just no whitespace
- **inbox**: run the sections a config names, and act on a row
- **inbox**: open a review from the queue on screen
- **review**: read, answer, and post a review with no checkout
- **threads**: find the clone that can answer a cross-repo conversation
- **threads**: thumbs-up everything R marks dealt with
- **get**: offer to stash before moving the checkout onto a pull request
- **tui**: read and answer conversations from a queue screen
- **reviews**: list what is staged under .second-look
- **threads**: queue the conversations that moved since you looked

### Fix

- **tui**: key a question comment to ?, since q cancels every chord
- **tui**: count the comment under the cursor in the order it is drawn
- open the browser on o instead of printing the URL into the alt screen
- **tui**: keep the identifying end of a row, and say which section it is in
- **get**: cache the open threads when a review is opened without a get
- **inbox**: report a broken config on stderr so --json stays parsable
- **threads**: review from the API when no dashboard can name a clone
- **inbox**: drop the unread dot that only restated the bucket

### Refactor

- **rate**: make the structural pass over a diff in one place
- **tui**: take keyhint from aragonite v0.8.0
- **test**: replay gh through aragonite ghcassette

### Perf

- **review**: draw a staged review from the cache and check the head behind it

## v0.4.0 (2026-09-01)

### Feat

- **inbox**: print the review queue in three buckets
- **post**: send one comment on its own, from the shell or the screen
- **tui**: hide hunks that change nothing but whitespace
- **tui**: group files by directory with their counts
- **tui**: show the comments alone, by file, with the counts
- **tui**: search the diff, scoped to what has not been read
- **get**: carry read hunks across a head move by content
- **tui**: mark hunks read and walk what is left with ]u
- **tui**: replace the key-per-destination map with a motion grammar
- **tui**: attach a shell transcript to the comment it is evidence for
- read the pull request's open review threads and answer them
- **tui**: show the renames, binaries, and mode changes a diff carries
- print the agent instructions the binary carries

### Fix

- **tui**: say when a jump ran out and when an edit changed nothing
- **tui**: measure the frame in terminal cells instead of runes
- name the command to run when the review screen has no terminal
- **post**: refuse a comment review that would carry nothing
- **tui**: refuse a second submit while the first is still in flight
- **tui**: report a submit that failed instead of losing it with the frame
- **tui**: keep the cursor and the way out visible without color
- **tui**: refuse to write the review back once it has posted

### Refactor

- name the subprocess coverage directory for the template, not the project

## v0.3.0 (2026-09-01)

### Feat

- **tui**: anchor a jump near the top of the frame and confirm before posting
- **tui**: title the frame with the path and how far the cursor has read

### Fix

- hold the post summary back until the review screen exits
- **post**: address replies to the pull request's own comment route

## v0.2.0 (2026-08-31)

### Feat

- **cli**: open the review screen for a pull request, or for this branch
- **tui**: read the diff and triage the prepared review on screen
- **diff**: keep each hunk header for a caller that renders the diff
- **post**: remove the prepared review once GitHub has it

### Fix

- **anchor**: hold a multi-line comment to one hunk of the diff
- **cli**: refuse an unknown argument and print the review in the schema's own names
- **get**: refuse to replace a prepared review that will not parse
- **diff**: keep hunk content out of the file-header cases and refuse a patch series

### Refactor

- **post**: move posting out of cmd behind a Poster seam

## v0.1.1 (2026-08-31)

### Fix

- **scripts**: generate the tap deploy key inside 1Password
- **deps**: upgrade aragonite to v0.2.1

## v0.1.0 (2026-08-27)

### Feat

- guard every comment against the diff it anchors to
- add second-look get
- **artifact**: stage a review as TOML and post the JSON subset

### Fix

- satisfy the linter across the review artifact
