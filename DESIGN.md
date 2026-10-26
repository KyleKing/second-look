# Design

This file explains why second-look is shaped the way it is and how the pieces fit together. What to build next is in [ROADMAP.md](ROADMAP.md). I left the screens and keys out because the footer and `?` already describe them, and they change faster than this file does.

## The problem

Most of the code I ship is now written by an agent and reviewed first by a bot. An agent opens a pull request, a review bot reviews it, another agent drafts replies, and a third pushes fixes. Every step in that loop is automated except the one that matters most, which is a person reading the diff and deciding whether the change is right. On github.com that read is shallow, because the viewed checkbox is per file and resets on every push, a moved function shows up as a delete and an unrelated insert, and files arrive in alphabetical order. I ended up skimming, and the review I posted said LGTM whether or not I had really looked.

Two smaller problems sit under that one. Working through the queue of waiting pull requests meant opening a browser tab per repository, each with its own conventions. The comments I staged with an agent also lived in a markdown file with no schema, so nothing checked that a comment still pointed at the line it cited, and posting meant a series of `gh` calls that I had to supervise.

second-look puts the diff, the open threads, and my staged comments on one screen, remembers what I have read, and posts the review in a single call. It is also the interface the agents use, so the person and the bots work on the same artifact.

## Who it is for

It is for one person (me) reviewing many pull requests a day across many repositories, where the authors are often agents, and I have published it in case anyone else has the same loop. It assumes `gh` is signed in and that an agent such as Claude Code drafts comments through the skill the binary prints. It does not try to serve a team, a web UI, or any forge other than GitHub.

## Decisions

### The review is a local artifact

A review lives in a TOML file per pull request, written by the tool and by agents through the CLI, and nothing reaches GitHub until I press `S`. The alternative was to post comments as they are written, which is how `gh pr review` works. That loses the property I care about most, which is that a comment an agent drafted at 2am can be read, edited, and deleted before anyone else sees it.

TOML is the on-disk format because a person edits it. The agent and `gh` speak JSON, so JSON is the input and output of the CLI and is never something a person edits. Every field is either posted or local, and the payload builder reads only the posted fields. Local fields carry the command that proved a finding, the doubt, and the reason a finding was declined, so a private note cannot leak because someone forgot to remove it. An unknown field is refused, because a misspelled key is a hand edit that will not do what its author meant.

The store is one directory per repository under the user config directory, keyed by host, owner, and name, and a checkout that held reviews keeps a pointer to it. My first version kept the review in the checkout, and that turned `kyleking/second-look#2` read from two clones into two unrelated reviews.

### Posting is one call

`S` sends the review body and every line comment to GitHub's reviews endpoint in one request, so a failure leaves none of them posted. Replies to existing threads cannot ride in a review, so they follow as one request each. Before anything is sent, each comment's anchor is checked against the live diff and the pull request's head is compared with the one the review was written against. A comment that no longer anchors, or a head that moved, stops the post and says which. A draft present at submit time blocks the post instead of being posted or dropped. The artifact is deleted once GitHub has the review, and GitHub is the source of truth from then on.

### Seen-state is a content hash

A hunk is identified by its file plus every line of the hunk with the line numbers left out. A hunk that slides down the file stays read, and a hunk whose text changed comes back unread, so a force-push keeps the reading that still applies. I first planned to delegate this to `git range-diff`, and I built it and measured it. It answers nothing the hash does not, because it reports per commit and calls a pair identical only when the patch matches byte for byte, which is exactly when the hunk hashes match too.

### The queue is ordered in lanes

A flat list of pull requests says nothing about what to do next, so the queue has five lanes, drawn in this order: yours that are ready to merge, anything back in your court, new requests, pull requests waiting on others, and merged. The order keeps the work in flight small by finishing things before starting new ones. Inside a lane, drafts sink to the bottom. Above them a review already started comes first, then a priority label, then a request where I am the only human asked, then one in the area of the pull request I opened last, and then the oldest. The cost rating and the line count order nothing. They are drawn beside the row for me to weigh, because a rating that decides is a rating I would stop checking.

### The queue is one line per row on a shared grid

The first queue gave each pull request two lines, with the columns hanging under the title, so a nine-row inbox filled 27 lines and an 80x24 terminal showed eleven rows. Each row is now one line on a column grid that the inbox, conversations, and staged tabs share. The ref column is a fixed 15 cells, so titles start in the same cell on every tab and switching tabs leaves the eye where it was. A column that exists keeps its width on every row, and a fact that has not arrived draws a dim dot, so landing it moves nothing.

Columns leave at explicit breakpoints (size at 100 cells, author at 112) instead of by arithmetic on minimum widths, so what 80, 100, and 120 columns show is a table in code. The title column is capped near 72 cells, because past that the facts drift far from the title they describe. Below 40 cells the title drops its commit prefix, since the subject is what a reader scans for.

What a row leaves out, the inspector says. It is two lines on one panel with the footer, pinned to the bottom, and it shows the full title, the prefix, the ref, and every fact a column dropped. It gives way in a fixed order when the frame is narrow: the ref goes first, then the prefix, and the subject is cut last, because the row above already shows the other two. The status column comes from `inbox.Judgement`, which carries a glyph, a short word, and a tone beside the prose reason. The alternative was truncating the prose, which produced `approved, checks passi…`. A glyph keeps its meaning across tabs and under `NO_COLOR` (`✗` is always the red failure, and `⊘` marks a staged review that cannot post). The tab strip shows each name with a count chip and no digit, because a bare numeral beside a count read as two numbers. The digits still switch tabs and the help lists them.

### Lanes recede and the cursor is a tint

Hierarchy comes from weight and color. Rows that ask something of me draw in the brightest face, and waiting and merged rows recede to a dimmer rung that still passes 4.5:1 on the background. Merged is folded behind `z`. A stacked row whose own lane is later than the lane it is listed in recedes as well, and it names its own lane in its status so a grey row in an action lane never goes unexplained. Without color the colors that carry this are gone, so a receding row and a quiet lane label are drawn faint, which is the one attribute that survives.

The cursor is a single tint across the full row and has no glyph, so the priority mark, the stack connector, and the unread mark each keep a cell of their own. The earlier cursor was a bright chip plus a band darker than the page, which added no lift. A band light enough to see costs contrast, so every face has a dim and a lit variant and steps up one rung under the band. A test measures every pair against 4.5:1. With no color the row under the cursor is reversed across its width, and it is the only reversed row.

### The queue opens from a snapshot

Opening used to draw nothing for about three seconds and settled the lanes near ten, because the facts read chained behind the slowest of four searches, and every return from a review repaid all of it. Each tab now paints its last queue from a snapshot before any call is made, marks those rows stale, and refreshes behind it. A stale count draws dim so an old number is not read as a fact. One snapshot per tab goes through aragonite's disk cache, because that cache rewrites a whole upstream file on every write and per-pull-request entries would mean sixty rewrites. It holds private repository names and titles, so it lives in the user cache directory with owner-only modes, is never used as a fixture, and carries the login it was read as. A refresh that answers as another account drops it.

The refresh asks for what changed. The four searches stay, since each result carries `updatedAt`, and facts are read only for pull requests that are new, moved, had checks still running, or were read more than ten minutes ago. The age bound exists because `updatedAt` does not cover everything a facts read holds, and whether a finished check suite moves it is unverified, so the running-checks rule stays until a probe shows it does. Conversations read a light query first and fetch full threads only for what moved, in chunks of ten, which measured about twice as fast as one large call. Staged states come from one aliased query instead of one process per review, and a shared limit of six keeps three tabs starting together from swamping the machine.

Acting on a pull request evicts it from the snapshots, so the next read brings it back in the lane it now belongs in. GitHub search is eventually consistent, and waiting for it would show the old lane. An approve also takes its row off the screen at once.

A refreshed order is held until the keyboard has been quiet for 400 ms, so rows never shift in the middle of a burst of `j`. Only incoming data waits and a key is never delayed. Rows that moved lane or rank, or are new, are then tinted for about 600 ms in steps, and the tint stays below the cursor's band so it never reads as a second cursor. The cursor stays on its row by key. The other options were a slide, which on a one-line grid is a few discrete hops that read as flicker, and a "3 updates" hint, which leaves the screen knowingly out of date. An identical order and a cold open with no snapshot flash nothing, and under `NO_COLOR` a marker glyph replaces the tint.

### Verdicts are reply states

Judging a bot finding is the most common action, so it gets one key. The verdict is the state of the staged reply to that finding's thread, and there are four. Discuss posts a reply and leaves the thread open. Todo posts a reply and sends the finding to a fixer. Won't fix posts a reply and then resolves the thread. Skip posts nothing. A blank reply is drafted by the agent to fit the verdict. Keeping the verdict and the reply as one object means there is no second list to drift out of step with the first.

On submit, todo replies go to a local agent run over ACP, or to nobody. A bot that fixes on a label or a mention is reached with the general keys: `l` sets a label from the repository's own list, and `@` completes a login. When the fixer pushes, the pull request returns to the back-to-you lane, and `H` shows which todos the new commits answered.

### Reviewing needs no checkout

The diff comes from the API, and a threaded reply carries a comment id instead of a line, so reading, judging, answering, and posting need no working tree. A checkout is something the reader asks for when they want to read around the change or run it. `C` moves a clone that is already on the laptop and never makes one, because a tool that cloned gigabytes to answer a keystroke would be the wrong tool. When several clones exist, they are ranked by what using one costs: already on the head, then clean, then one that needs a stash. gh-repo-dashboard already scans the clones, so second-look asks it instead of growing a second directory scanner. A lease file names the process holding a clone, so two sessions do not move one tree out from under each other.

### Chroma, ast-grep, and no cgo

The release builds every platform with `CGO_ENABLED=0`, and every tree-sitter binding for Go needs cgo. So highlighting uses [chroma](https://github.com/alecthomas/chroma), and the structural pass shells out to `ast-grep` when it is on the path. The tool already shells out to `gh`, `git`, `$EDITOR`, and `$SHELL`, and a missing binary costs one feature, which the screen says is off. I measured difftastic and ruled it out, because it calls a reworded comment a change and a pure reformat none, and telling comment changes from code changes is the distinction this pass exists to make.

### The language-server pass

Go to definition, references, the call graph, and checker notes come from whatever language server is installed, so second-look carries no index of its own yet. A file is opened as an unsaved buffer holding the commit under review, so nothing writes to the working tree. The server starts in the project the file belongs to instead of at the checkout root, because a TypeScript server rooted at a monorepo reads a package's imports as unresolvable and reports errors the change did not cause. A configured server that is not installed disables its extensions, since silently running a different server than the one named is worse. The cost is that nothing answers without a checkout, which is why these features run only when asked for and never to open a screen.

### The conversation queue

GitHub notifications fire on everything and say nothing about whose turn it is. The conversations tab lists the open discussions across every pull request I am in, bucketed by whose turn it is. A bot reaches it only through an inline review thread, because that is the one place its words anchor to code and can be resolved. Its pull request comments are coverage tables and nudges that nobody resolves, and without this rule the queue held 77 rows of which 13 were real. A machine account is told apart by GitHub's `__typename`, since a list of logins goes stale and `coderabbitai` carries no `[bot]` suffix.

## Architecture

```mermaid
flowchart LR
    cmd["cmd/second-look"] --> tui["internal/tui"]
    cmd --> post["internal/post"]
    tui --> inbox["internal/inbox"]
    tui --> artifact["internal/artifact"]
    tui --> diag["internal/diag"]
    post --> artifact
    inbox --> gh[("gh CLI")]
    post --> gh
    artifact --> store[("store on disk")]
    diag --> ls["language servers"]
    tui & inbox & post --> aragonite["aragonite"]
```

| Package | Responsibility |
| --- | --- |
| `cmd/second-look` | Flags, target resolution, and the CLI an agent drives (`--help` is its contract) |
| `internal/tui` | The tabbed screen: queues, review, outline, tour, and the agent pane |
| `internal/inbox` | The review searches, lanes, and the order inside a lane |
| `internal/artifact` | The prepared review: schema, anchors, the store, and the caches keyed by head |
| `internal/post` | The anchor guard and the single review request |
| `internal/diag` | Language-server and `[[check]]` notes, placed against the diff |
| `aragonite` | Shared Go layer: `forge`, `vcs`, `cache`, `filter`, and `tui/` widgets |

About forty smaller packages sit under `internal/`, each with one job described in its package comment. `internal/get` prepares a review, `internal/plan` and `internal/order` decide the reading order, `internal/conversations` is the second queue, and `internal/acp` speaks to a local agent.

State lives under the user config directory, one directory per host, owner, and name, because the queue spans repositories and a review filed into whichever clone was open would be lost to every other one. A checkout that held reviews gets them moved there and a `WHERE.md` left behind. `second-look local` is the exception and keeps `local.toml` in the checkout. The diff, threads, and rating are cached by head commit and cleared when the review posts or is discarded, and `second-look gc` drops reviews for merged or closed pull requests and sweeps the caches.

Opening a staged review reads those files and makes one network call, which checks that the head still stands. The first frame waits for that check, because a diff staged against an older head has already been read by the time the screen admits it. Being offline costs that one round trip and leaves the review intact.

### Shared code

aragonite holds what gh-repo-dashboard and second-look both need: the GitHub client, git and jj behind one interface, a TTL cache with a disk layer, the filter engine, and the table and pane widgets. The two tools keep separate inboxes because they answer different questions. gh-repo-dashboard asks what state my repositories are in, and second-look asks what review I owe. `go.mod` pins a released aragonite version, and `mise run verify-released` builds and tests with `GOWORK=off` to prove the consumer works against the published version and not a sibling checkout.

## What it does not do

- Call a language model. Drafting is an agent's job, driven through the CLI and the skill, so no API key or prompt lives in this tool
- Run a daemon. The process starts fast and reads `.second-look/` and the cache, so there is no resident index to go stale
- Clone a repository, or move a working tree without asking
- Post to a forge other than GitHub. The forge layer is an interface so another could be added, and none has been
- Rate a pull request to decide anything, because the rating is advisory
- Attach an image to an inline review comment. `gh --attach` covers pull request comments only, and the endpoint it uses for reviews is undocumented
