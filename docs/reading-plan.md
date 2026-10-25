# Reading plan

How second-look orders a large change for reading. The research behind it is in
[huge-pr-review-2026-10.md](research/huge-pr-review-2026-10.md), and the review screen
that draws it is described in [DESIGN.md](../DESIGN.md).

## One plan, several views

A plan holds the change's claim, its features, the skeleton stops, and the chapters. Each
chapter has a mode: read, skim, shape, table, or fold. Every hunk lands in exactly one
chapter, which is the invariant `internal/order` already keeps for its groups, and the plan
grows out of those groups. Each placement records its source, drawn as a glyph: `§` a rule,
`⇢` the code graph, `⊙` a commit, `◆` the agent, and `✎` you.

The layers apply cheapest first, and each may move what an earlier one placed:

1. What the change says about itself: the pull request body, the AI Summary's links into
   the code, and its commits. Commits that each touch their own hunks become the chapters,
   in commit order and named by subject. Fixups, "address review" commits, and one commit
   touching everything send the plan back to the symbol and directory groups. A pull
   request's commits are listed with `gh` behind the first frame, so the screen opens on the
   symbol and directory groups and replans when they arrive. They are blamed in the checkout
   and never fetched, and where it lacks any of them the plan's basis says so
2. Rules you keep per repository in your config (below)
3. The code graph from the language server the checker pass already runs: who calls what,
   and which test covers which file
4. The agent, through plan verbs on the CLI the way comments are staged. It names the
   stops, writes what each one does and what follows from it, and places what the first
   three could not

Layers 1 through 3 are recomputed on every open. What the agent and you change is kept in
a local `[plan]` table in the prepared review, anchored by path and line range so it
survives a head that moved, and it never posts. Without an agent pass there are no stops,
and the tour opens on the chapters. Each chapter is drawn under a heading with its glyph,
mode, and reason, and a file in a fold chapter starts folded. `i` says what the chapters
were cut from and lists every placement the plan could not honor, which the header counts.
`O` puts the diff's own order back.

### Rules

A `[[plan]]` table in `~/.config/second-look/config.toml` names one repository as
`owner/name` and takes these keys. A glob matches the path from the repository root, `**`
spans directories, and a glob with no slash matches a file name at any depth. A rule set
with a bad glob or mode is refused, and the error names every problem in it.

- `fold` lists globs read as one line each
- `never_fold` lists globs that win over `fold`, a fold mode, and the folding of generated
  files
- `[[plan.mode]]` takes a `glob` and a `mode` and reads matching hunks in that mode. It is
  checked before `fold`, and the first match wins
- `[[plan.after]]` takes `read` and `after` globs and moves every chapter touching `read`
  behind the last chapter touching `after`

### Repeated edits

Between the commits and the rules, an edit made in many files is gathered into a shape
chapter with no agent. Each changed line is matched with its whitespace, its numbers, and
any identifier spelling the file's own name taken out, and a line with fewer than four
letters and digits, such as a brace, is ignored. A line made in five or more files seeds a
shape. The shape is every line at least 80% of those files make, and a file is in it when
it makes at least 80% of the shape. A commit's hunks are searched on their own and every
other chapter's together, and a hunk a rule reads, or a chapter the agent or you named, is
left alone. The agent turns a shape off, or turns any chapter into one, with `plan mode`,
which finds a shape among as few as two files.

The head is searched three ways for files the edit missed: the longest line every member
removed (or kept around the edit, where they removed none in common), the longest name the
shape stops calling, and file names like the members' in their directory at their depth.
The searches run in the checkout behind the first frame and list at most twenty files.

## Views

- Tour is the default up to 60 files, and `p` switches it on and off. It lists the stops
  and then the chapters. At 160 columns a rail of stops and chapters sits left and a margin
  of notes sits right, and below that the rail becomes a strip and the notes move inline
- Outline is the default above 60 files, and `I` moves between it and the diff. Each row
  carries source glyph, name, mode, files, and changed lines, and a fold chapter counts its
  lines as folded. The selected chapter's why, files, and stops sit beside it from 160
  columns and under it below that. A file another chapter also reads names that chapter.
  `enter` opens the chapter at its heading and `ctrl-o` walks back
- Pattern is a chapter mode and not a screen. The edit is drawn once from a file that makes
  it exactly, then a row per file saying what it does beyond it, then the files at head
  that still carry the line the edited files carried before. `enter` on a row opens that
  file and `space` marks its hunks read. Files making the edit exactly are not drawn again,
  and the ones doing more are, in full, under the table
- Features is `tab` in the outline, one row per feature, for a change too big to read as
  one. A row counts its chapters, files, and lines, and the files another feature also
  touches. The selected feature lists its chapters, then its files with the shared ones
  first, each marked `◇` with the features that share it. It is for understanding the
  change, so nothing is staged from it and it proposes no split

## Local changes

`second-look local` reads a range as well as a branch: `--range A..B` for git and
`-r <revset>` for jj. The header names the repository and the range in place of a pull
request, and each commit or revision in the range is a chapter.

## Reading the code itself

Comments, threads, peeks, and questions are all cards drawn under the row they belong to.

- `gd` and `gr` label each name on the line with a letter, and the letter picks one. A line
  with one name skips the labels. The answer is a peek card holding the definition's lines
  or the list of references
- `gc` draws the call graph of the function the line is in, with callers on the left and
  callees on the right. `✎` marks a function the change edits and `◆` one with a finding to
  judge. `za` excerpts the selected function's call sites, `enter` opens it, and `gc` on it
  centers the graph there
- `enter` on a peek opens the whole file at head with the change marked in the gutter.
  `ctrl-o` and `ctrl-i` walk the jump list, and `gf` opens the same place in `$EDITOR`
- `A` on a `V` range, or `A` then `l` on one line, asks about it, in the diff or the whole
  file. The question is kept in `[plan]` as yours, drawn as a card under the range, and
  listed in the outline under its chapter. `]a` and `[a` walk the asks. A live agent
  session gets the question first, with its pane open so a permission it asks for can be
  answered, and the configured one-shot agent next. With neither, the card says nobody was
  asked, and an agent answers later by staging the same id with `second-look plan ask`. An
  ask about a line the diff does not carry is marked `outside`, is placed by path and line
  alone, and draws only in the whole file
