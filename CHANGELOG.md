## v0.37.0 (2026-10-26)

### Feat

- **tui**: draw the queue's chrome through aragonite's frame and answer clicks on rows, tabs, and the legend
- **tui**: widen the help legend with the frame so its rows stop wrapping at 120 columns
- **tui**: esc on a review opens the queue
- **snapshot**: evict a pull request from the snapshot once the reader acts on it
- **conversations**: paint the last queue and fetch only the pull requests that moved
- **tui**: draw staged reviews as one line each on the column grid
- **tui**: draw the conversation queue as one line per thread on the column grid
- **tui**: open the checkout in gh-repo-dashboard and come back
- **tui**: tint rows a refresh moved, then let them settle
- **tui**: hold a refreshed order until the keyboard is idle
- **tui**: start every tab at open and draw stale counts dim
- **reviews**: read every staged review's state in one query and gate gh at six
- **inbox**: read facts only for rows the snapshot cannot vouch for
- **inbox**: paint the last queue on the first frame and refresh behind it
- **snapshot**: keep the last queue each tab drew on the disk cache
- **tui**: fit the footer's keys to the width by priority
- **tui**: show the row under the cursor in an inspector panel
- **tui**: mark the cursor row with a full-width band
- **tui**: draw the inbox as one line per pull request on a column grid
- **inbox**: give each judgement a glyph, a short word, and a tone
- **tui**: lane headers read name and count, with a blank between lanes only in tall frames
- **tui**: one header line of tabs with count chips and an underline rule
- **tui**: add the queue's faces: band, rung ladder, count chips, hairline

### Fix

- **tui**: draw the outline's selected row facts in the lit muted rung
- **tui**: spell the review header ref as repo#n like the queue
- **inbox**: name the lane of a stack-dimmed row in its status
- **reviews**: mark blocked staged reviews with their own glyph
- **tui**: size the queue's help legend with the review screen's width
- **tui**: cap the queue title column and widen the author column
- **tui**: drop the hotkey digit from the tab strip
- **tui**: drop the commit prefix from a title column under forty cells
- **tui**: keep the inspector subject whole before dropping its ref
- **inbox**: recede merged rows like waiting rows
- **tui**: dim waiting rows and quiet lane labels when color is off
- **tui**: keep the file strip's cursor mark on the opening row and in one-cell files
- **tui**: keep the ref column 15 cells wide so titles start in one column on every tab
- **tui**: filter grid rows by what their cells and inspector say

### Refactor

- **tui**: move the color profile switch out of list update
- **tui**: draw the cursor and range as row tints on aragonite v1.0.0
- **tui**: drop the two-line row fields and the layout that drew them
- **tui**: fit the review screen's keys with keyhint

### Perf

- **reviews**: read staged states in parallel chunks

## v0.36.0 (2026-10-25)

### Feat

- **inbox**: draw stacked pull requests as one unit, bottom first

## v0.35.0 (2026-10-25)

### Feat

- **tui**: draw in Ink always
- **tui**: complete an @ from the people on the pull request and the repository's recent ones
- **tui**: pick a pull request's labels from the repository's own on a row or a review
- **tui**: scroll a call graph side past five nodes

### Fix

- **tui**: draw a bracket key bare in the chord footer
- **tui**: tint one cell of a changed line's indent instead of all of it
- **tui**: tag headings instead of narrating them, and show how to see a summary's hunks
- **post**: hand todo replies off from P and post, as S does
- **tui**: count only threads someone else opened as to judge
- **tui**: draw the call graph as two contiguous trees that meet the center row

### Refactor

- **tui**: hand todo replies to the local agent or nobody, and drop the Watch Doggo label settings

## v0.34.0 (2026-10-25)

### Feat

- **tui**: draw the call graph under the cursor's line on gc
- **lsp**: ask a language server for the call graph around a function
- **tui**: show read and to-judge progress per chapter in the outline
- **tui**: say which todos a round answered on the thread and in the H box
- **post**: hand todo replies to Watch Doggo or a local agent when submitting
- **demo**: a scene for judging a bot's findings
- **tui**: rule on a finding with r t x R and draft the reply for it
- **post**: resolve the thread a won't-fix reply answers
- **inbox**: order the queue by lanes that keep work in flight small

### Fix

- **tui**: align the H box in columns and size it to its rows

## v0.33.0 (2026-10-24)

### Feat

- **tui**: mark a run of lines the change moved, name its other end, and jump there on enter
- **tui**: badge a file the change creates or deletes and drop the sign from its lines
- **tui**: pair a line pushed to a new indent with itself, mark it, and tint the indent of an unpaired change
- **tui**: ask about a line outside the diff from the whole file
- **tui**: find a shape's look-alikes at head by name and file name, and read its exact files at once
- **tui**: cap an unresolved thread at three rows until za opens it
- **get**: read a pull request's commits into its plan, as local reviews do
- **tui**: ask about any selection and keep the answers in the plan
- **plan**: read a repeated edit once, then what each file does beyond it
- **tui**: show which features share files
- **tui**: open a big change on an outline of its chapters
- **tui**: walk the plan as a tour
- **tui**: read the change in its plan's chapters
- **local**: review a git range or a jj revset
- **artifact**: keep the agent's and your plan edits in the prepared review
- **plan**: build a reading plan from the change's commits, your rules, and its symbols
- **tui**: open a peek as the whole file and walk back with a jump list
- **tui**: pick a name with a letter and peek at it in a card
- **tui**: light the queues the way the review screen is lit
- **tui**: name each file in a directory strip by its shortest unique tail
- **tui**: split the title into a two-line header
- **tui**: draw comments and threads as bordered cards
- **tui**: hold the prompt cursor still
- **tui**: wrap comments and threads to the card measure
- **artifact**: record who last wrote each comment
- **tui**: light the cursor row, through aragonite's tint

### Fix

- **get**: say a pull request lists no commits rather than counting one missing
- **tui**: leave the cursor's file and line off the strip row, which the strip's mark already says
- **tui**: leave a blank row between a line's cards and the next line of code
- **tui**: read esc then a letter typed quickly as the two keys, outside a prompt
- **tui**: return the cursor from an off-cycle view, wheel through the outline, and keep a plan problem at 80 columns
- **tui**: hold the strip still as the cursor moves, and rule the tour's panels off the diff
- **tui**: keep the cursor on its hunk when the view changes
- **tui**: band only a line paired with its replacement, as wide as its hunk's code
- **tui**: one line number in the unified gutter
- **tui**: tint changed lines instead of washing them, and keep strings off the add color
- **get**: say "reading pull request #N" once
- **demo**: keep the stand-in gh out of the ignored bin/ pattern

### Refactor

- **tui**: let the cursor pass over a card's foot

## v0.32.3 (2026-10-21)

### Refactor

- **tui**: the queue's meta line is a declared table

## v0.32.2 (2026-10-21)

### Fix

- **tui**: ctrl+d and ctrl+u scroll the legend half a page
- **tui**: a peek outside the checkout reads the file the server resolved

## v0.32.1 (2026-10-15)

### Fix

- **tui**: the tab strip is a row of filled segments, not bracketed text

## v0.32.0 (2026-10-15)

### Feat

- **tui**: f narrows the diff by file, and uw folds a long line

## v0.31.1 (2026-10-15)

### Fix

- **tui**: y and Y scroll a line, and an unmoved frame says so
- **tui**: a queue row's title is its own line

## v0.31.0 (2026-10-15)

### Feat

- **local**: F writes a [TODO: marker into the source it guards
- **tui**: an asked-for path is a motion n walks
- second-look journal reads the sitting back

### Refactor

- **tui**: the pty pane lives in aragonite now

## v0.30.0 (2026-10-15)

### Feat

- **tui**: the looks are rich and split, and A asks the agent
- second-look local reviews the checkout's own diff
- **tui**: gf opens the whole file in $EDITOR at the line it carries
- fold the notification stream into the conversations tab
- **tui**: draw a jump's source under its name, not only its address
- journal the keys and waits a sitting makes

## v0.29.0 (2026-10-10)

### Feat

- **tui**: draw help and a waiting chord as a floating box
- **tui**: page the key legend through its chords

### Fix

- **e2e**: keep the agent config under the child's own XDG root

## v0.28.2 (2026-10-09)

### Fix

- **tui**: draw the cursor shape the pane's child asks for
- **tui**: keep the cursor on its line and tab on comments
- **diag**: tell pyright the project's own python interpreter

## v0.28.1 (2026-10-08)

### Fix

- **tui**: take a shell transcript from the pane's screen, not its bytes

## v0.28.0 (2026-10-08)

### Feat

- **tui**: run an ACP agent in a pane beside the diff
- **acp**: open an agent session over ACP, transcript and asks in-band

## v0.27.0 (2026-10-08)

### Feat

- **tui**: keep ctrl+\ for the screen to end a pane's program

## v0.26.1 (2026-10-08)

### Fix

- **lsp**: let a line's other names answer past a refused one

## v0.26.0 (2026-10-08)

### Feat

- **tui**: run ! in the pane and tee its transcript off the pty
- **tui**: run $EDITOR in a pane beside the diff, and ask gd/gr of the server

## v0.25.0 (2026-10-08)

### Feat

- **blame**: keep each file's answer under its old-side blob

## v0.24.0 (2026-10-08)

### Feat

- **blame**: gutter each line's age, blamed at the merge base

### Perf

- **structure**: scan every hunk's fragments in one ast-grep invocation

## v0.23.0 (2026-10-08)

### Feat

- **lockfile**: card each moved version's age and the latest, asked of its own registry

## v0.22.0 (2026-10-08)

### Feat

- **second-look**: lease the checkout C claims, handed back when focus leaves
- **agent**: say what a recorded session is doing, on the row, the title, and status

## v0.21.0 (2026-10-07)

### Feat

- **second-look**: the staged tab as a queue, with gc and status commands
- **queue**: merge read marks across shells and keep a status snapshot
- **review**: fetch an earlier round's diff on demand
- **tui**: delta-style diff rendering and shared list plumbing

### Fix

- **prepared**: todos block, approvals are postable, broken rows name the file

## v0.20.0 (2026-09-17)

### Feat

- **tui**: ask osv.dev what is known against the versions a lockfile moved to
- **diag**: run a check in the project its files belong to, with that project's tool
- **diag**: answer a server asking what it is configured with

### Fix

- **diag**: root a server where it can answer, ranking markers before depth

## v0.19.0 (2026-09-16)

### Feat

- **tui**: keep a long note and a hover answer inside the frame
- **diag**: show what a language server and your own checks make of the change

### Fix

- **diag**: start a language server in the project the file belongs to

## v0.18.0 (2026-09-11)

### Feat

- **tui**: mention D in the post-submit status line

## v0.17.0 (2026-09-11)

### Feat

- **tui**: add D to check out the base branch and delete this one

## v0.16.0 (2026-09-11)

### Feat

- **artifact**: relocate a comment whose anchor shifted but did not change

### Fix

- **get**: recover from a rebased or force-pushed upstream
- **tui**: make tab skip a resolved thread
- **tui**: draw the cursor as a chip instead of bold accent text
- **tui**: lex a thread's fenced code once instead of on every rebuild
- **tui**: center a thread the same way landing on a comment does

## v0.15.1 (2026-09-07)

### Fix

- **tui**: detect the palette once, before a program owns stdin

## v0.15.0 (2026-09-07)

### Feat

- **tui**: fold the review screen into the queue's own program
- **order**: sort directory groups by summed hunk cost

### Refactor

- **tui**: filter through aragonite's shared predicate package

## v0.14.0 (2026-09-06)

### Feat

- **tui**: check the head before drawing, and mark the hunks still to read

## v0.13.0 (2026-09-06)

### Feat

- **inbox**: walk the queue orders on demand

### Fix

- **meta**: keep a diff fixture the whitespace hook would rewrite

## v0.12.0 (2026-09-06)

### Feat

- **tui**: carry the group and the line in the title
- **meta**: read pnpm, yarn, and Gemfile lockfiles too

## v0.11.0 (2026-09-06)

### Feat

- **tui**: toggle the grammar, the columns, and the parser on their own

## v0.10.0 (2026-09-06)

### Feat

- **tui**: open a review on the rich renderer

## v0.9.0 (2026-09-06)

### Feat

- **tui**: count the whole file on each piece of a split one
- **tui**: read a lockfile as the dependencies it changed
- **tui**: read a symbol that moved between files as one move
- **threads**: keep resolved and outdated conversations, tagged
- **tui**: put the note under the body and keep room below both
- **tui**: dim the keys that do nothing where the cursor is
- **tui**: draw the review body in full rather than folded
- **review**: post, then open the next staged review checked out
- **agent**: resume the session an agent records on the review
- **queue**: filter conversations by kind and mark started work
- **reviews**: name the change and say what the forge thinks of it
- **tui**: react with an emoji and fold a whole conversation
- **tui**: show what the pull request says about itself with i
- **inbox**: say what a staged review holds and which clone is free
- **queue**: focus one repository across all three tabs with f

### Fix

- **typos**: let Ratatui through as a library name
- **tui**: keep the thread on screen while replying to it

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
