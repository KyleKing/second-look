# Queue screen redesign

Plan for a visual rewrite of the queue screen (inbox, conversations, staged). Nothing here is built yet. A sibling plan, [queue-loading.md](queue-loading.md), covers caching, fetching, and reorder animation. This one only names the interfaces it needs from that plan (see "Interfaces with queue-loading").

## Round 2 revisions (these override the design below)

After review, the user asked for a quieter frame with stronger titles, and chose the tint over a slide for reorders.

- Lane headers are `  label  count`, with no rule and no dot. The label is bold Text for action lanes and Overlay2 for waiting and merged. The count is Subtext0 or Overlay1. Merged reads `merged  5 · z shows`. Lanes are separated by one blank line at 30 rows and up, and by none below that.
- Rows lead with the ref (`repo#n`) in a fixed dim column, then one mark cell (`!`, `╭`, `╰`, or `●` for unread), then the title. All titles start on the same column.
- A conventional-commit prefix (`feat(inbox):`) draws in Overlay1, and the subject draws in Text. The inspector shows the subject as its bold first line, with the prefix and ref after it.
- Only the status glyph carries the tone. The word draws in Subtext0 (Overlay2 on waiting rows). `↻ new commits` uses Sapphire, and comments use Yellow.
- The inspector and footer share one Mantle panel pinned to the bottom of the frame. Key hints drop their brackets: the key draws in Lavender and the word in Overlay1.
- Kept from round 1: tab chips with the underline, the selection band with lit faces, the one-line grid, `·` placeholders, Overlay2 waiting rows, and breakpoints.

## What is wrong now

Frames captured from `demo/scene.sh inbox` under tmux at 80x24, 120x34, and 160x45.

- Each pull request takes two lines, with the columns hanging 10 cells under the title. A 9-row inbox fills 27 lines, so 80x24 shows 11 rows before it scrolls.
- The size column (`46  +57  -0`) draws only when the facts have loaded, so columns appear and shift as ratings land. The reviewers column mixes `only you`, `+ 2`, and `maria +1`.
- The status text is the last weighted column, yet it truncates (`approved, checks passi…`) while an empty gap sits to its left. At 80 columns the table collapse hides five columns and shows `…+5`, so the status words never appear at all.
- The cursor is a bright lavender `▌` chip in column 0 plus a faint band, and `!` for priority sits in the same column. Two marks compete for one cell.
- Rows waiting on others use `Overlay0` (`#69717f`). Measured against the real backgrounds:

| Foreground | on Base `#1e2128` | on today's band `#1a2037` | on proposed band `#31374a` |
| ---------- | ----------------- | ------------------------- | -------------------------- |
| Overlay0   | 3.3:1             | 3.3:1                     | 2.4:1                      |
| Overlay1   | 4.8:1             | 4.8:1                     | 3.5:1                      |
| Overlay2   | 5.5:1             | 5.5:1                     | 4.1:1                      |
| Subtext0   | 7.0:1             | 7.0:1                     | 5.1:1                      |

  A waiting row fails 4.5:1 on every background today. Today's band (the `Tint` of Lavender onto Base) is also darker than Base itself, so it adds no lift and the lavender `▌` chip carries the whole selection. That matches "hard to read before and after selection" and "AI sloppy" cursor. A band light enough to see (the proposed one) costs contrast, so selected text must step up a rung.
- The tab strip counts (`· 9 waiting`) are muted text inside a chip, and the right-hand header count repeats the same number in `Subtext0`. Neither is bold or distinct from the label next to it.
- The footer cuts mid-word at 80 columns (`[o] GitH…`).
- `demo/scenes/inbox` has no route for `gh api notifications`, so its conversations tab shows an error instead of rows.

Contrast numbers come from the WCAG luminance formula applied to the Ink hex values in `aragonite/tui/theme/theme.go`. The band values are approximate (computed from the Ink hex values and a linear RGB mix, where aragonite mixes in Lab), so step S2 asserts the ratios in a test instead of trusting this table.

## Inspiration

The searches found thin material, so only claims I could read are listed.

- [gh-dash docs](https://www.gh-dash.dev/) group pull requests into sections that each carry a count, and let each column be hidden, given a width, or told to grow. That matches the Column model aragonite already has, so the gap is the breakpoints and the cell rendering, not the idea.
- [Linear's UI redesign write-up](https://linear.app/now/how-we-redesigned-the-linear-ui) stresses alignment you feel after a few minutes, a neutral base with a limited accent, a clear elevation scale, and testing several row densities before settling on one. This plan follows it: one accent (Lavender), chrome rows one step darker than the body, one row height.
- [Drew DeVault on GitHub notifications](https://drewdevault.com/2020/03/13/GitHub-notifications.html) and the refined-github thread behind GitHub's "group by repository" setting both favor grouped lists where the group name is read once and rows drop the repeated part. Rows here drop the owner for the same reason.
- [k9s skins](https://k9scli.io/topics/skins/) define one cursor color for a table plus a header with a sort indicator. A single, whole-row cursor treatment is the convention in table TUIs.
- [Nielsen Norman Group on data tables](https://www.nngroup.com/articles/data-tables/) says to order columns by importance, keep a readable identifier in the first column, and show a clear indicator when columns are hidden. Hence the title leads, and the `…+N` marker stays, now backed by an inspector line that shows what was dropped.
- Numbers right-aligned and a tinted full-row band for selection are general table practice (see the [uxcel selected-row lesson](https://app.uxcel.com/lessons/tables-152/selected-row-4575)). I found no source for lazygit, Graphite, or Superhuman layouts, so nothing here is attributed to them.

## Design

### Rules

1. One line per row. The row shows the title and one state. Everything else is one keypress away or in the inspector (below).
2. Columns sit on a fixed grid. A column that exists keeps its width on every row, and a value not yet known draws a dim `·`, so nothing shifts when facts land.
3. Hierarchy comes from weight and color: rows that need action are `Text`, rows waiting on others recede to `Overlay2` (still 5.5:1), merged is folded.
4. Selection is a full-width band. There is no glyph, so `!` priority, the unread `●`, and the stack connector each own their cell.
5. Lanes are one surface separated by hairline rules, not boxes.
6. Breakpoints are explicit (`Column.From`), so the design at 80, 100, 112, and 140 is a table in code, not an accident of minimum widths.

### Column grid (inbox)

Margin 1 cell, mark cell group 3 cells (`!`, stack connector, space), then:

| Column | Min | Max | Weight | From | Align | Content |
| ------ | --- | --- | ------ | ---- | ----- | ------- |
| title  | 36  | -   | 3      | 0    | left  | PR title, ellipsis tail |
| ref    | 15  | 15  | 0      | 0    | left  | `repo#n`, owner shown as dim prefix at 140+ (and always when two owners share a repo name in the lane) |
| author | 10  | 10  | 0      | 112  | left  | login |
| who    | 8   | 8   | 0      | 140  | left  | requested reviewers, one shape: `you`, `maria`, `maria +1` |
| age    | 4   | 4   | 0      | 0    | right | `55d` |
| size   | 10  | 10  | 0      | 100  | right | `+57` and `-0`, signs in fixed cells |
| status | 16  | 26  | 1      | 0    | left  | glyph and word, short form below 26 cells |

Surplus goes to title (weight 3) and status (weight 1, capped at 26). At 120 columns that gives the title about 49 cells, at 160 about 72. The `cost 46` rating and labels leave the row and live in the inspector, which is also where `Why` text longer than the status cell is read in full.

Status vocabulary, with the glyph that survives NO_COLOR. The long form shows when the cell is 24 or wider:

| Meaning | Short (16) | Long | Tone |
| ------- | ---------- | ---- | ---- |
| approved, checks pass | `✓ approved` | `✓ approved, checks passing` | good |
| pushed since your review | `↻ new commits` | `↻ pushed since your review` | warn |
| new comments | `◆ 2 comments` | `◆ 2 new comments` | warn |
| you asked for changes | `✗ changes asked` | `✗ you asked for changes` | bad |
| awaiting review | `○ awaiting` | `○ awaiting review` | muted |
| you approved | `✓ you approved` | same | muted |
| draft | `◌ draft` | same | muted |
| priority | `priority` | `priority label` | warn |

This needs `inbox.Judgement` to carry a glyph, a short word, and a tone next to `Why`, so the TUI does not parse prose. `render.go` (the `--json` and plain output) keeps using `Why`.

### Stacks

A stack member draws a connector in the mark group: `╭` for the bottom (read first), `├` for middles, `╰` for the top. The `stack 1/2` and `└ 2/2` text goes away. Members stay adjacent as today. The inspector says `stack 2 of 2 · read #104 first`. A member whose lane is later than the lane it is listed in draws as a waiting row (the existing `Dim` rule).

### Selected row

- Color: the whole row, from the left margin to the right edge, gets a band `Mix(Base, Lavender, 0.16)` (about `#31374a`). Every foreground steps up one rung while selected (Overlay2 to Subtext0, Subtext0 to Text, title to bold Text). That keeps waiting rows at 5.5:1 or better under the band, which the contrast test enforces.
- NO_COLOR or an ascii profile: no background exists, so the row is drawn reverse-video across its full width. A test renders with the ascii profile and requires exactly one reverse row.
- The `▌` bar, `cursorBar`, `styles.cursor`, and `lightLine`'s reset-patching go away. aragonite's cell-level styling (below) replaces the hack that re-applies the background after every reset.

### Lane headers

One line, no blank line above on frames under 30 rows (compact), one blank otherwise:

```
 ▪ back to you ───────────────────────────────────── 4
```

Label bold `Text` for action lanes and `Overlay2` plain for waiting and merged. Rule in `Surface2`. Count right-aligned, bold `Subtext1` (action) or `Overlay1`. The lane dot is `▪` in the lane tone (ready Green, back Yellow, new Sapphire) and hollow `▫` in `Overlay1` for waiting and merged, so the distinction survives NO_COLOR. A folded lane reads `▫ merged ──── z shows  5`. An empty lane keeps its header with count `0`. A loading lane puts `searching…` where the count goes (`Section.Note`, already there).

The unread mark in the conversations tab stays `●` (accent) so it is not confused with the lane dot.

### Header and tab strip

Two lines replace today's title line plus chip strip. The program name and tabs share line one, a rule carries the active-tab underline on line two:

```
 second-look   1 inbox 9   2 conversations 7   3 staged 1                          sorted by triage
───────────────━━━━━━━━━──────────────────────────────────────────────────────────────────────────
```

- Tab label: digit in `Overlay1`, name in `Subtext0` (active: bold `Text`).
- Count is a chip: bold `Text` on `Surface1` when inactive, bold `Base` on `Lavender` when active. It is the same number the old right-hand header repeated, so that duplicate is deleted. Staged shows `1` in `Red` when something is blocked.
- Active tab is also marked by `━` under it, which survives NO_COLOR. The inactive chip degrades to plain padded digits.
- Right side shows only what changes how to read the list: the sort (`sorted by triage`), or a loading state in `Yellow` (`reading reviews…`). The existing `counts()` states ("3 searches still out", "lanes provisional") move there unchanged. A narrowing filter or focus repo replaces it, as it does now.

### Inspector

Two lines above the footer on a `Mantle` band, for the row under the cursor, so the frame never changes height:

1. Full title, bold `Text`.
2. `owner/repo#n · author · age ago · reviewers · stack note · cost · labels`, in `Subtext0` with `Overlay0` separators, and the long status word.

Below 16 rows tall it collapses to line 2 only. It is the "one expand" for everything a column dropped, so 80 columns loses nothing it cannot recover with one `j`. The existing `Detail` expansion (enter) is untouched.

### Footer

`keyhint.Fit` with priorities, replacing hand-cut text: enter review, A approve, M merge, o GitHub, and `?` carry the highest priority and never shed. `C`, `m`, `l`, `s`, `/` shed first. Off keys (`inert`) shed before any on key, which `Fit` already does.

### Color spec (Ink only)

| Element | Face |
| ------- | ---- |
| Body background | Base `#1e2128` |
| Inspector and footer rows | Mantle `#1a1d23` |
| Selection band | `Mix(Base, Lavender, 0.16)` |
| Hairlines | Surface2 `#343b48` |
| Tab count chip, inactive | Text on Surface1 `#2d3340`, bold |
| Tab count chip, active, and underline | Base on Lavender `#94a8ff`, bold |
| Lane label, action | Text `#c8ced8`, bold |
| Lane label, waiting or merged | Overlay2 `#8f98a7` |
| Lane count | Subtext1 `#b6bdc8` bold (action), Overlay1 `#848d9c` |
| Lane dot | Green `#5fd497`, Yellow `#e8c35a`, Sapphire `#5fb8e0`, hollow in Overlay1 |
| Title | Text (action), Overlay2 (waiting), bold Text (selected) |
| ref, author | Subtext0 `#a3abb8` (owner prefix Overlay1) |
| age | Overlay1 |
| size `+` and `-` | Green and Red, Overlay1 on waiting rows |
| Status glyph and word | tone: Green, Yellow, Red, Overlay1, Mauve for merged |
| Priority `!` | Peach `#ffa45c` bold |
| Unread `●` | Lavender bold |
| Stack connector | Overlay1 |
| Placeholder `·` | Overlay0 (decoration only, allowed under 4.5:1) |
| Inspector title, meta | Text bold, Subtext0, separators Overlay0 |

## Mockups

`░` is the selection band's background (the real screen paints it, no glyph). The selected row is the one drawn with `░`.

### Inbox, 120x34

```
 second-look   1 inbox 9   2 conversations 7   3 staged 1                                              sorted by triage
───────────────━━━━━━━━━────────────────────────────────────────────────────────────────────────────────────────────────
 ▪ ready to merge ─────────────────────────────────────────────────────────────────────────────────────────────────── 1
░░░░feat(inbox): order the queue by lane░░░░░░░░░░░░░░aragonite#130░░░░kyleking░░░░░55d░░░░+57░░░-0░░✓ approved░░░░░░░░░

 ▪ back to you ────────────────────────────────────────────────────────────────────────────────────────────────────── 4
  ╭ refactor(vcs): split reading the head from movi…  aragonite#104    maria        55d           ·  ◆ 2 comments
  ╰ feat(vcs): read the head through the operations…  aragonite#105    maria        55d           ·  ↻ new commits
    fix(cache): key the parsed diff by head commit    second-look#110  hubot        56d           ·  ↻ new commits
    fix(post): anchor a reply to the right line       second-look#131  kyleking     55d           ·  ◆ 2 comments

 ▪ new for you ────────────────────────────────────────────────────────────────────────────────────────────────────── 4
 !  refactor(forge): move the pull request type out…  hello-world#103  maria        98d    +57   -0  priority
    chore(deps): bump the charm packages              gh-sweep#102     dependabot   62d           ·
    feat(vcs): read the head through the operations…  aragonite#100    octocat      55d           ·
    fix(cache): key the parsed diff by head commit    second-look#101  hubot        56d           ·  ◌ draft

 ▫ waiting on others ──────────────────────────────────────────────────────────────────────────────────────────────── 3
    refactor(forge): move the pull request type out…  hello-world#112  octocat      98d    +57   -0  ✗ changes asked
    docs: record why the anchor guard has two halves  gh-sweep#132     kyleking     59d           ·  ○ awaiting
    chore(deps): bump the charm packages              gh-sweep#111     dependabot   62d           ·  ✓ you approved

 ▫ merged ────────────────────────────────────────────────────────────────────────────────────────────────── z shows  5
⋮
 feat(inbox): order the queue by lane
 KyleKing/aragonite#130 · kyleking · 55d ago · approved, checks passing · cost 46 · [needs-review]
 [enter] review  [A] approve  [M] merge  [o] GitHub  [C]heck out  [s]ort  [/] filter  [?] help
```

### Inbox, 80x24 (compact: no blank rows, author and size hidden, second row selected)

```
 second-look   1 inbox 9   2 conversations 7   3 staged 1                triage
───────────────━━━━━━━━━────────────────────────────────────────────────────────
 ▪ ready to merge ─────────────────────────────────────────────────────────── 1
    feat(inbox): order the queue by lane  aragonite#130     55d  ✓ approved
 ▪ back to you ────────────────────────────────────────────────────────────── 4
  ╭ refactor(vcs): split reading the he…  aragonite#104     55d  ◆ 2 comments
░░╰ feat(vcs): read the head through th…░░aragonite#105░░░░░55d░░↻ new commits░░
    fix(cache): key the parsed diff by …  second-look#110   56d  ↻ new commits
    fix(post): anchor a reply to the ri…  second-look#131   55d  ◆ 2 comments
 ▪ new for you ────────────────────────────────────────────────────────────── 4
 !  refactor(forge): move the pull requ…  hello-world#103   98d  priority
    chore(deps): bump the charm packages  gh-sweep#102      62d
    feat(vcs): read the head through th…  aragonite#100     55d
    fix(cache): key the parsed diff by …  second-look#101   56d  ◌ draft
 ▫ waiting on others ──────────────────────────────────────────────────────── 3
    refactor(forge): move the pull requ…  hello-world#112   98d  ✗ changes asked
    docs: record why the anchor guard h…  gh-sweep#132      59d  ○ awaiting
    chore(deps): bump the charm packages  gh-sweep#111      62d  ✓ you approved
 ▫ merged ────────────────────────────────────────────────────────── z shows  5
⋮
 feat(vcs): read the head through the operations interface
 KyleKing/aragonite#105 · maria · 55d ago · only you · stack 2 of 2 · read #104
 [enter] review  [A]pprove  [M]erge  [o] GitHub  [?] help
```

### Waiting row selected (band present, text one rung brighter than unselected)

At 120 columns the row for `hello-world#112` reads as `Overlay2` text when the cursor is elsewhere (the unselected row in the 120 frame above), and as `Subtext0` title and cells on the band when it is selected. The status glyph keeps its tone color in both states.

### Conversations, 120x34 and 80x24

```
 second-look   1 inbox 9   2 conversations 7   3 staged 1                                                      7 unread
───────────────────────────━━━━━━━━━━━━━━━━━────────────────────────────────────────────────────────────────────────────
 ▪ new since you looked ───────────────────────────────────────────────────────────────────────────────────────────── 6
 ●  two questions inline                               mine#1      review body    alice          55d
 ●  can you split this?                                mine#1      conversation   alice          55d
 ●░░this leaks░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░mine#1░░░░░░a.go:12░░░░░░░░coderabbitai░░░55d░░░░░░░░░░░░░░░░░░░░
 ●  cc @me for the diff parser                         theirs#2    conversation   alice          56d
 ●  @ME does this match your parser?                   theirs#2    b.go:60        alice          56d
 ●  because of the lease                               theirs#2    b.go:5         bob            56d  2 replies · old
 ▪ waiting on you ─────────────────────────────────────────────────────────────────────────────────────────────────── 0
 ▫ awaiting others ────────────────────────────────────────────────────────────────────────────────────────────────── 1
    no, that one is per worker                         theirs#2    b.go:80        me             56d  2 replies
⋮
 this leaks
 o/mine#1 · a.go:12 · coderabbitai · 55d ago · unread · your pull request
 [enter] read  [space] done  [r]eply  [R]esolve  [o] GitHub  [?] help
```

```
 second-look   1 inbox 9   2 conversations 7   3 staged 1              7 unread
───────────────────────────━━━━━━━━━━━━━━━━━────────────────────────────────────
 ▪ new since you looked ───────────────────────────────────────────────────── 6
 ●  two questions inline                       mine#1       55d
 ●  can you split this?                        mine#1       55d
 ●░░this leaks░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░mine#1░░░░░░░55d░░░░░░░░░░░░░░░░░
 ●  cc @me for the diff parser                 theirs#2     56d
 ●  @ME does this match your parser?           theirs#2     56d
 ●  because of the lease                       theirs#2     56d  2 replies · old
 ▪ waiting on you ─────────────────────────────────────────────────────────── 0
 ▫ awaiting others ────────────────────────────────────────────────────────── 1
    no, that one is per worker                 theirs#2     56d  2 replies
⋮
 this leaks
 o/mine#1 · a.go:12 · coderabbitai · 55d ago · unread · your pull request
 [enter] read  [space] done  [r]eply  [R]esolve  [o] GitHub  [?] help
```

Columns: title is the last comment's first line, ref is `repo#n`, anchor (`review body`, `conversation`, `a.go:12`) shows from 100, author from 112, age, status (`2 replies · old`). The unread `●` replaces the stack slot, and an empty lane keeps its `0` header.

### Staged, 120x34 and 80x24

```
 second-look   1 inbox 9   2 conversations 7   3 staged 1                                                     1 blocked
───────────────────────────────────────────────━━━━━━━━━━───────────────────────────────────────────────────────────────
 ▪ staged ─────────────────────────────────────────────────────────────────────────────────────────────────────────── 1
░░░░fix(cache): key the parsed diff by head rather than…░░KyleKing/second-look#2░░░░░2 drafts░░░░0m░░✗ blocked░░░░░░░░░░
⋮
 fix(cache): key the parsed diff by head rather than by number
 KyleKing/second-look#2 · 2 drafts · you commented · blocked: head moved
 [enter] open  [C]heckout  [d]iscard  [ctrl+r] refresh  [?] help
```

```
 second-look   1 inbox 9   2 conversations 7   3 staged 1             1 blocked
───────────────────────────────────────────────━━━━━━━━━━───────────────────────
 ▪ staged ─────────────────────────────────────────────────────────────────── 1
░░░░fix(cache): key the parsed diff by h…░░second-look#2░░░░░░0m░░✗ blocked░░░░░
⋮
 fix(cache): key the parsed diff by head rather than by number
 KyleKing/second-look#2 · 2 drafts · you commented · blocked: head moved
 [enter] open  [C]heckout  [d]iscard  [ctrl+r] refresh  [?] help
```

Columns: title, ref (full `owner/repo#n` at 100 and up, since local checkouts span owners), drafts count right-aligned, age, status (`✗ blocked`). The forge's word (`you commented`) and the agent's word move to the inspector below 140.

## aragonite changes

Read `tui/table/table.go`. It already has `Column{Min, Max, Weight, Priority, Align, Trim}`, `Fit`, `Pad`, `Row(layout, cells, face)`, and `Header`. What is missing, and second-look currently works around in `list.go`:

1. Breakpoints. Priority collapse is arithmetic on minimums, so which column goes at 120 depends on every other column's `Min`. Add a field:

```go
// From hides the column when the table is narrower than From cells,
// regardless of Priority. Zero means no floor.
From int
```

   `collapse` drops columns with `From > width` first and counts them in `Hidden`.

2. Say what was hidden, so the inspector can show it:

```go
// Dropped lists the keys of the columns collapse hid, in column order.
Dropped []string
```

3. Styled cells inside a band. `Row` takes `[]string` and calls `PadTrim`, which cuts with `uniseg` on raw text, so a cell that carries ANSI cannot be truncated safely, and `listview.go`'s `lightLine` patches resets by string replacement. Add:

```go
// Cell is one cell's text and the face it draws in, layered over the row's.
type Cell struct {
	Text  string
	Style lipgloss.Style
}

// RowCells is Row for cells that carry their own face. The row's face is the
// base for every cell, gutter, and the marker slot, so a band behind the row
// is unbroken and no reset can end it early.
func RowCells(l Layout, cells []Cell, face lipgloss.Style) string
```

   The implementation pads and truncates `Text` first, then renders `face.Inherit(cell.Style)`.

4. A group rule, the one header shape all three tabs use:

```go
// RuleStyles are the faces a Rule draws with.
type RuleStyles struct{ Line, Label, Count lipgloss.Style }

// Rule draws "label ──── count" to exactly width cells. A count wider than
// width/2 is cut, and a label wider than the room is truncated with an ellipsis.
func Rule(s RuleStyles, label, count string, width int) string
```

5. Natural widths without a struct per screen. `list.go`'s `natural` has a field per column. Add:

```go
// Measure returns the widest cell per key across rows, in display cells.
func Measure(rows [][]Cell, keys []string) map[string]int
```

   It is optional for the first cut (second-look can keep `natural` until step S8), but it is the piece that lets three tabs share one path.

6. `skin` additions (not changes, because other tools use `Selection` and `Tab`): `Badge` and `BadgeActive` (chip faces for counts), `Hairline`, and `Band` (the selection mix above, with a doc line giving the contrast it guarantees). aragonite tests assert the ratios through a small helper in `theme`.

Tests in `table_test.go` follow the existing table-driven style: `From` at widths 99, 100, 111, 112; `Dropped` order; `RowCells` with an ANSI-bearing cell shows no break in the band; `Rule` at widths 20, 40, and a count wider than room; wide glyph cells (`⬆️`, CJK) through all of it.

Release: aragonite has no release script. `CONTRIBUTING.md` says `gh release create vX.Y.Z --generate-notes` and the latest tag is `v0.22.0`. So step A2 ends with the user pushing aragonite main and running `gh release create v0.23.0 --generate-notes`. Until then second-look develops against a local `replace github.com/kyleking/aragonite => ../aragonite` in `go.mod`, removed in the step that bumps to `v0.23.0` (S1b). Nothing in this plan pushes or tags.

## Interfaces with queue-loading

- Each `Section` may hold rows whose facts are not in yet. A `tui.Cell` for such a row carries `Pending: true` and draws `·` in Overlay0 at the column's full width, so landing facts changes no column width.
- The lane's right-hand text is `Section.Note` when set (`searching…`), else the count. Reorder animation moves whole rows, so the band and the connector cells must travel with the row. They do, because both come from the row's own cells.
- A row is identified by `Row.Key` only. Nothing in this plan stores positions by index.

## Implementation steps

Each step is one commit, small enough for one subagent. Run `go test ./...` then `go test ./... -update` and read every golden diff. Steps A1 and A2 are in aragonite, S-steps in second-look. Order matters: A1, A2, then S0 onwards.

| # | Repo | Step | Files | Tests and goldens | Risk |
| - | ---- | ---- | ----- | ----------------- | ---- |
| A1 | aragonite | `Column.From`, `Layout.Dropped`, `Cell` and `RowCells`, `Rule`, `Measure` | `tui/table/table.go`, `table_test.go` | new table tests, no goldens | Low. Additive, `Row` unchanged |
| A2 | aragonite | `skin.Badge`, `BadgeActive`, `Hairline`, `Band`, contrast helper in `theme`, then user tags `v0.23.0` | `tui/skin/skin.go`, `tui/theme/*.go`, tests | contrast test over Ink | Low. Additive |
| S0 | second-look | Seed `gh api notifications` for the inbox scene | `demo/fixture/notifications.json` (invented content), `demo/fixture/routes` (add `api notifications  notifications.json` above the `api graphql` rows) | none. Check with `demo/scene.sh inbox` then `2` | Low |
| S1 | second-look | Bump aragonite to `v0.23.0`, drop the local `replace` | `go.mod`, `go.sum` | `go build ./...` | Low |
| S2 | second-look | Faces: band, rung ladder (dim and lit), chips, hairline. Test asserts every dim and lit pair on Base and band is 4.5:1 or better | `internal/tui/styles.go`, new `styles_test.go` | no frame changes yet | Low |
| S3 | second-look | Header: one tab line plus underline rule, count chips, right side shows sort or loading only. Delete the title line and `Subtitle` duplication | `internal/tui/listview.go` (`header`, `modes`), `tabs.go` (`tabStrip`) | `TestListFrames`, `TestLaneFrames` goldens; pty tests that await `second-look inbox` | Medium. Many awaits |
| S4 | second-look | Lane header rule via `table.Rule`, dot glyphs, compact blank policy | `listview.go` (`line`), `list.go` (`heading`) | list goldens | Low |
| S5 | second-look | Status model: `inbox.Judgement` gains glyph, short word, tone. Plain `--json` output unchanged | `internal/inbox/lanes.go`, `lanes_test.go` | lane table tests, `TestInbox*` e2e golden stays | Medium. Touches judgement |
| S6 | second-look | One-line row on a column grid. Add `Tab.Columns` and per-row `Cell`s next to the old fields, switch the inbox tab to them, apply `From` breakpoints, stack connectors, placeholder `·`, owner-prefix rule | `internal/tui/list.go`, `listview.go`, `cmd/second-look/inboxscreen.go` (`rows`) | `TestListFrames` (80, 100, 120, 160), `TestLaneFrames`, pty inbox tests | High. Largest step. Keep old path alive for the other two tabs |
| S7 | second-look | Selection band and no-glyph cursor. Remove `▌`, `cursor` face, `lightLine`. ascii profile draws reverse video | `listview.go`, `styles.go` | frame goldens, new ascii-profile test (one reverse row), contrast test on band | Medium |
| S8 | second-look | Inspector lines, labels and cost move there, `Dropped` columns listed in it | `listview.go`, `list.go` (height math), `inboxscreen.go` | goldens at 24 and 34 rows, `TestKeyPresses...` that count visible rows | Medium. Height change moves scroll tests |
| S9 | second-look | Footer through `keyhint.Fit` with priorities | `listview.go` (`footer`), `hints.go` | footer goldens, 80-col truncation test | Low |
| S10 | second-look | Conversations tab onto the grid (anchor, author, status, unread slot) | `cmd/second-look/threadsscreen.go`, `internal/tui` | `threads_e2e_test.go` goldens, list goldens | Medium |
| S11 | second-look | Staged tab onto the grid (drafts, status, forge and agent words into the inspector) | `cmd/second-look/reviewsscreen.go` | reviews e2e goldens | Medium |
| S12 | second-look | Delete the old `Row` fields (`Left`, `Mid`, `Cost`, `Added`, `Removed`, `Who`, `Why`, `Tail`, `Under`, `Branch`), `natural`, `metaLayout`, `metaChrome`. Update AGENTS.local.md and regenerate the README gif with `mise run demo` | `list.go`, `listview.go`, docs, `demo/demo.tape` | whole suite | Medium. Mechanical but wide |
| S13 | second-look | Pty tests: resize at 80x24 and 160x45, NO_COLOR frame, quit paths, one test per bug found in tmux, named for its trigger | `cmd/second-look/tui_e2e_test.go`, `internal/tui/list_test.go` | new tests only | Low |

Verification for every S step before commit follows AGENTS.md: `mise exec -- hk check --all`, `actionlint`, `golangci-lint run`, and `mise run bench`, then look at the screen with `demo/scene.sh inbox` under tmux at 80x24, 120x34, and 160x45, and with `NO_COLOR=1`.

## Decisions worth a second look

- Dropping author and size below 112 and 100 columns trades information for title width. The inspector recovers both with one keypress. If you would rather keep the author on the row at 80, it costs 14 cells of title.
- The status words get short forms in `inbox.Judgement`, which adds a second spelling of each reason. The alternative is truncating prose, which is what produced `approved, checks passi…`.
- Selection brightens text one rung instead of keeping colors fixed, which means each face has two variants. The alternative is a fixed band light enough for Overlay0, which would wash out the band against Base.
