# Reading plan

How second-look orders a large change for reading. The research behind it is in
[huge-pr-review-2026-10.md](../research/huge-pr-review-2026-10.md). The review screen in
[DESIGN.md](../DESIGN.md#review) draws every view below.

## One plan, several views

A plan holds the change's claim, its features, the skeleton stops, and the chapters. Each
chapter has a mode: read, skim, shape, table, or fold. Every hunk lands in exactly one
chapter, which is the invariant `internal/order` already keeps for its groups, and the plan
grows out of those groups rather than beside them. Each placement records where it came
from, drawn as a glyph: `§` a rule, `⇢` the code graph, `⊙` a commit, `◆` the agent, and
`✎` you.

The layers apply cheapest first, and each may move what an earlier one placed:

1. What the change says about itself: the PR body, the AI Summary's links into the code,
   and its commits. Commits that each touch their own hunks become the chapters, in commit
   order and named by subject. Fixups, "address review" commits, and one commit touching
   everything send the plan back to the symbol and directory groups
2. Rules you keep per repository in your own config: what to fold, what reads after what,
   and what never folds
3. The code graph from the language server already running for the checker pass: who calls
   what, and which test covers which file
4. The agent, through plan verbs on the CLI the way comments are staged: it names the stops,
   writes what each one does and what follows from it, and places what the first three
   could not

Between the first layer and the rules, an edit made in many files is gathered into a shape
chapter, with no agent. Each changed line is matched with its whitespace, its numbers, and
any identifier spelling the file's own name taken out, and a line with fewer than four
letters and digits, such as a brace, is ignored. A line made in five or more files seeds a
shape, the shape is every line at least 80% of those files make, and a file is in it when
it makes at least 80% of the shape. A commit's hunks are searched on their own and every
other chapter's together, and a hunk a rule reads or a chapter the agent or you named is
left alone. A shape takes only the files making the edit, and the rest of the change stays
where it was. The agent turns one off, or turns any chapter into one, with `plan mode`,
which finds a shape among as few as two files. The search of the head is for the longest
line every member removed, or kept around the edit where they removed none in common, runs
in the checkout behind the first frame, and lists at most twenty files.

Layers 1 through 3 are recomputed on every open. What the agent and you change is kept in a
local `[plan]` table in the prepared review, anchored by path and line range so it survives
a head that moved, and it never posts. Without an agent pass there are no stops, and the
tour opens on the chapters.

The review screen draws each chapter under a heading carrying its glyph, its mode, and its
reason, and a file a fold chapter holds starts folded. `i` says what the chapters were cut
from (the commits, or why they were passed over) and lists every placement the plan could
not honor, which the header counts. `O` still puts the diff's own order back.

## Views

- **Tour.** The default up to 60 files, and `p` switches it off and on. The stops first,
  then the chapters. At 160 columns a rail of stops and chapters sits left and a margin of
  notes sits right. Below that the rail becomes a strip across the top and the notes move
  inline
- **Outline.** The default above 60 files, and `I` opens it from the diff and goes back.
  A list of chapters, each row carrying its source glyph, name, mode, files, and changed
  lines, with a fold chapter counting its lines as folded and the list's head totalling
  them. The selected chapter's why, files, and stops sit to its right from 160 columns and
  under it below that, where the list keeps at most half the frame so the detail has room.
  A file another chapter also reads names that chapter. `enter` opens the chapter at its
  heading in the diff, and `ctrl-o` walks back to the row the diff was on
- **Pattern.** A chapter mode, not a screen. The edit is drawn once from one file that
  makes it exactly, then a row per file saying what it does beyond it, then the files at
  head that still carry the line the edited files carried before and were not changed.
  `enter` on either kind of row opens that file, and `space` on a file's row marks its
  hunks read. The files making the edit exactly are not drawn again, and the ones doing
  more are, in full, under the table
- **Features.** `tab` in the outline, one row per feature, for a change too big to read as
  one. A row counts its chapters, files, and lines, and the files another feature also
  touches. The selected feature lists its chapters, then its files with the shared ones
  first, each marked `◇` and naming the features that share it. It is for understanding the
  change, so nothing is staged from it and it proposes no split. The same structure is what
  splitting your own change into a stack would start from

In the tour the strip under the header names the stops by number and the chapters by name.
The plain screen's strip names each file by its shortest unique tail.

## Local changes

`second-look local` reads a range as well as a branch: `--range A..B` for git and
`-r <revset>` for jj. The header names the repository and the range in place of a pull
request, and each commit or jj revision in the range is a chapter.

## Reading the code itself

Comments, threads, peeks, and questions are all cards drawn inline under the row they
belong to, so there is one thing to learn:

- `gd` and `gr` label each name on the line with a letter, and the letter picks one. A
  line with one name skips the labels. The answer opens as a peek card under the row:
  the definition's lines, or the list of references, walked with `j` and `k`
- `enter` on a peek opens the whole file at head with the change marked in the gutter.
  `ctrl-o` and `ctrl-i` walk the jump list back to the diff row and forward again, and
  `gf` opens the same place in `$EDITOR`
- `A` on an open `V` range, or `A` then `l` on one line, of the diff or of the whole file,
  asks about it. The question is kept in `[plan]` as yours and drawn as a card under the
  range's last line, and the outline lists it under its chapter as asked. `]a` and `[a`
  walk the asks. A live agent session gets the question first, with its pane open so a
  permission it asks for can be answered, and the configured one-shot agent next. The
  answer is written into the ask. With neither, the card says nobody was asked, and an
  agent answers later by staging the same id with `second-look plan ask`. A line the diff
  does not carry cannot be asked about, since the ask anchors the way a comment does
