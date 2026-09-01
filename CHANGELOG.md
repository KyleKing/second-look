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
