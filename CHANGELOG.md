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
