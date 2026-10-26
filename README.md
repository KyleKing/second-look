# second-look

A terminal code review tool optimized for the modern rate of code generation, is local-first and AI-friendly, and encourages us to actually read and understand the code as everything else changes. While this is currently personalized for my own use cases and workflows, it should be straightforward to generalize and I'm open to input and making changes because I would be happy to see others find value in this. Feel free to open a GitHub issue and let me know what you think!

![Working the queue: open a pull request, judge a bot's findings, label it, and hand the fixes off on submit](demo/triage.gif)

## How a review moves

```mermaid
flowchart LR
    Q[Queue<br/>second-look inbox] -->|enter| R[Review screen]
    B[Review bot<br/>threads on GitHub] --> R
    A[Your agent<br/>stages comments] --> R
    R -->|judge, edit| R
    R -->|S| P[One review posted]
    P -->|todo replies| F[Fixer<br/>local agent, or a bot you label or @-mention]
    F -->|pushes| Q
    Q -->|back to you, H| R
```

The queue has three tabs, inbox, conversations, and staged, which `1`, `2`, `3`, `[`, and `]` switch between. Every row is one line on a shared grid, so titles start in the same column on all three, and the row under the cursor is a single tinted band. A line under the list says what the row had no room for, such as the full title, the commit prefix, and who it is waiting on. `esc` on a review opens the queue again, and `B` on a review opens its checkout in [gh-repo-dashboard](https://github.com/KyleKing/gh-repo-dashboard) and returns when you quit it.

The inbox is ordered to keep the amount of work in progress small. Your own pull requests that are approved and ready to merge come first, then anything waiting on a reply from you, then new review requests (priority labels and pull requests where you are the only human reviewer go to the top). Work waiting on somebody else and merged pull requests come last and recede, so the rows that need you are the brightest, and `z` shows the merged lane. Each finding from a review bot or teammate gets one of four verdicts:

| Key | Verdict | What posts |
| --- | ------- | ---------- |
| `r` | discuss | your reply, thread stays open |
| `t` | todo | your reply, and the finding goes to your local agent |
| `R` | won't fix | your reply, then the thread is resolved |
| `x` | skip | nothing |

If you don't write a reply, your agent drafts one that fits the verdict, and you can edit it before anything is posted. A bot that fixes on a label or a mention needs no setting here: `l` labels the pull request from the repository's own labels, and `@` in a reply completes the bots and people seen on its recent pull requests. Once the fixes are pushed, the pull request moves back into your inbox and `H` shows which of the todos the new commits addressed.

## Reading a big change

Large diffs open on an outline that splits the change into chapters, with how much of each you've read and how many findings are still open. Hunks are grouped by the symbol they change, generated files and lockfiles are folded into a summary, and the language server you already use powers `gd`, `gr`, and `gc` (the callers and callees of a function) against the commit under review.

![Reading a large change from its outline, then walking the call graph](demo/reading.gif)

<p>
<img src="docs/images/outline.png" width="49%" alt="The outline of a large second-look commit, with read strips and findings to judge per chapter">
<img src="docs/images/calls.png" width="49%" alt="gc on a function in second-look's own code, with its callers and callees">
</p>

The keybindings follow vim, so `]c` goes to the next comment, `]u` to the next unread hunk, `za` toggles a fold, and `n` repeats the last motion. The footer shows the keys that apply at the cursor and `?` lists the rest.

## Install

```bash
brew install --cask kyleking/tap/second-look
# or
go install github.com/kyleking/second-look/cmd/second-look@latest
```

sl requires [gh](https://cli.github.com), while language servers and
[ast-grep](https://ast-grep.github.io) are optional

## Use

```bash
second-look inbox                    # the queue of pull requests waiting on you
second-look 10                       # review #10 in this repository
second-look kyleking/second-look#10  # or any pull request, no clone needed
second-look local                    # review your own unpushed change before it goes up
```

A click puts the cursor on a row or switches to a tab, and the wheel moves the cursor. The screen holds the mouse to do that, so hold shift (option in some terminals) to select text.

Agents like Claude Code use second-look through a skill that the binary prints, which you can install with:

```bash
second-look skill > ~/.claude/skills/second-look/SKILL.md
```

Re-run it after upgrading so the skill matches the binary. `second-look --help` covers the remaining commands, and [config.example.toml](config.example.toml) documents the settings you can put in `~/.config/second-look/config.toml`.

## More

- [DESIGN.md](DESIGN.md) covers the problem, the main design decisions, and the architecture
- [ROADMAP.md](ROADMAP.md) lists what I plan to work on next
- [CONTRIBUTING.md](CONTRIBUTING.md) explains how to build and test it

## Inspiration

second-look borrows ideas from a lot of great tools, and I'd recommend checking them out:

- [gh-dash](https://github.com/dlvhdr/gh-dash) for a configurable dashboard of pull requests in the terminal (second-look reads gh-dash style queries for its inbox sections)
- [Hunk](https://github.com/modem-dev/hunk) for a TypeScript review TUI where agents annotate a live review session through a skill
- [prr](https://github.com/danobi/prr) for preparing a review locally as a file and posting it in one request
- [tuicr](https://github.com/agavra/tuicr) for a vim-style review TUI over git, jj, and Mercurial that submits real GitHub reviews
- [diffnav](https://github.com/dlvhdr/diffnav) and [delta](https://github.com/dandavison/delta) for reading diffs in the terminal
- [difftastic](https://github.com/Wilfred/difftastic) for structural, syntax-aware diffs
- [Reviewable](https://reviewable.io) for tracking what you've reviewed across force-pushes
- [lazygit](https://github.com/jesseduffield/lazygit) for showing that a keyboard-driven TUI can be discoverable
