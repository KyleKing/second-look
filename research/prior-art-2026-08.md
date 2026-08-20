# Prior art, August 2026

**Kyle's bot did stuff and wants it available for future bots.** Findings from an agent
research session on 2026-08-19 with minimal human review. Validate every claim before
building on it. Activity dates come from release pages and commit logs read at that
time and will go stale.

The question this was gathering evidence for: does a tool already exist that prepares a
code review locally with LLM assistance, persists it as an editable structure, and posts
it deterministically, while making the diff itself navigable (hunk seen-state, move
detection, jump-to-definition, relation-based grouping)?

Short answer: the pieces exist in separate tools. Nothing combines them, and three
capabilities have no implementation anywhere.

## AI-assisted review

[hunk.dev](https://www.hunk.dev) ([modem-dev/hunk](https://github.com/modem-dev/hunk)) is
TypeScript on OpenTUI and the Pierre diff library, very active (v0.19.0 on 2026-08-16).
A local daemon holds a live review session over a diff, driven by `hunk session`
subcommands. `hunk session comment apply` reads a validated JSON batch from stdin with
`filePath` plus one of `hunk`, `hunkNumber`, `oldLine`, or `newLine`, and optional
`rationale`, `author`, and `markup`. That is the strongest agent-facing comment
interface found, and it is explicitly built for agents to drive. It does not post to
GitHub at all, by design.

[tuicr](https://tuicr.dev) ([agavra/tuicr](https://github.com/agavra/tuicr)) is Rust and
the closest single tool to this whole idea (v0.23.0 on 2026-08-18). Vim-keybinding TUI
over git, Jujutsu, and Mercurial diffs, a Rust library API (`ReviewStore`,
`AddCommentRequest`) for programmatic access, and `:submit` posts a real review with
inline comments to GitHub, GitLab, Bitbucket, or Azure DevOps. Persisted comment format
looks like TOML config plus markdown-anchored export rather than a documented JSON
schema, which is unverified from public docs.

[prr](https://github.com/danobi/prr) (Rust, commits into 2026-04) is the clean reference
for prepare-locally-then-post. `prr get <pr>` writes a plain-text review file with every
diff line quoted `> `, validated byte for byte against the live diff at submit time,
with `[..]` snip markers collapsing unchanged hunks. Anything else typed becomes a
comment on the nearest quoted line. `prr submit` builds one JSON payload and POSTs it to
`/reviews` atomically. Plain text rather than JSON, and the diff-guard is the idea worth
stealing.

The hosted engines all run review-then-optionally-post in one step, with no standing
hand-editable artifact between the two:

- [CodeRabbit CLI](https://docs.coderabbit.ai/cli) runs the same engine as the GitHub bot
  against local diffs (`--uncommitted`, `--include-untracked`), with `--agent` JSON
  (schema undocumented), an `--interactive` apply TUI, and `cr review findings` to replay
  cached results. No evidence it posts to GitHub itself
- [Greptile CLI](https://www.greptile.com/docs/code-review/greptile-cli), Node 22+, same
  engine as its apps, `--json`/`--agent`/`--diff` output. No confirmed CLI-side posting
- Graphite Diamond, folded into "Graphite Agent" branding. Cloud-only GitHub App, no CLI
  or local mode found
- Codex CLI `/review` does a local read-only pass over a diff, commit, or uncommitted
  changes, configurable through `AGENTS.md`, with a separate cloud path via
  `@codex review`. No documented schema or persisted artifact
- [Claude Code `/code-review`](https://code.claude.com/docs/en/code-review) reviews the
  current diff, branch, PR, or ref-range locally on any plan, applies findings with
  `--fix`, posts inline PR comments with `--comment`, and escalates to a billed cloud
  pass with `ultra`. Findings flow through a structured `ReportFindings` tool in-session
  and are tracked fixed or skipped, so there is no file you edit between review and post
- [Cursor BugBot](https://cursor.com/docs/bugbot) is cloud-only. Its CLI is "coming soon"

## Diff engines

- [difftastic](https://github.com/Wilfred/difftastic), Rust on tree-sitter, active
  (2026-08-17). Structural diff for 30+ languages, works as a git external diff driver.
  No cross-file move detection, no patching, no review state
- [delta](https://github.com/dandavison/delta), Rust, v0.19.2 (2026-03-28) after a long
  gap. A pager over git's own diff output with syntax highlighting, word-level intra-line
  diff, and side-by-side. Move detection is whatever `git diff --color-moved` gives it
- [diffnav](https://github.com/dlvhdr/diffnav), Go and Bubble Tea on top of delta,
  v0.12.0 (2026-07-24, which added search). Collapsible file tree, in-viewport regex and
  fuzzy search, watch mode, reads PR diffs through `gh`. Read-only
- [mergiraf](https://codeberg.org/mergiraf/mergiraf), Rust tree-sitter merge driver,
  v0.19.0 (2026-08-16). The one genuinely move-aware tool: resolves conflicts where one
  branch moves a block and another edits it, by reconciling ASTs. Merge time only, no
  viewer
- `git diff --color-moved` matches on line text similarity with a 20-alphanumeric-char
  floor, and `--find-copies` works at whole-file granularity. Display heuristics rather
  than stored structural facts
- [semanticdiff.com](https://semanticdiff.com), commercial, hides formatting noise and
  detects moves within a file. Augments the GitHub and VS Code comment workflows
- GitHub's rebuilt PR diff (public preview 2025-06-26, default 2026-01-22, docked side
  panel 2026-03-19) fixed rendering, split/unified, and comment search. Move detection is
  still absent, tracked in
  [community discussion 8573](https://github.com/orgs/community/discussions/8573)

## Neovim

- `diffview.nvim` is dead: last commit 2024-06-13, 26 unmerged PRs, maintainer
  unresponsive (see [neogit#1921](https://github.com/NeogitOrg/neogit/issues/1921)). The
  live fork is `diffview-plus.nvim` (dlyongemallo, pushed 2026-08-19), which adds
  subword inline diffs, tree-sitter highlighting on deleted lines, Jujutsu and Perforce
  support, and JSON-persisted panel state. Still no move detection, and LSP inside a
  historical diff remains an open request
  ([diffview.nvim#487](https://github.com/sindrets/diffview.nvim/issues/487))
- `octo.nvim` (2026-07-30) is the closest on seen-state, with `toggle_viewed` and
  `select_next_unviewed_entry`. That state is GitHub's own per-file API state, so it does
  not survive a force-push
- `gitsigns.nvim` (2026-08-11) has `nav_hunk next/prev` and hunk staging, with no
  seen-state concept
- `codediff.nvim` (esmuellert) ports VS Code's diff algorithm through LuaJIT FFI, with
  opt-in move-detection rendering and real editable buffers, so LSP and tree-sitter work
  inside the diff. Neogit's maintainers point users here
- Smaller AI-review plugins exist and none were verified: `pr.nvim`, `gh-review.nvim`,
  `hunk-review.nvim`, `meow.review.nvim`, `quickfix-review-nvim`, `reviewthem.nvim`,
  `review.nvim`

## TUI and CLI

- [gh-dash](https://github.com/dlvhdr/gh-dash), Go and Bubble Tea, v4.25.2 (2026-07-10).
  Groups PRs by configurable query sections, which is grouping at the PR level rather
  than files within a diff. Diff view passes through to your pager
- lazygit v0.64.1 (2026-08-12) has line, range, and hunk staging plus a custom-patch
  builder. Hunk-jump keybindings are still an open request
  ([lazygit#3558](https://github.com/jesseduffield/lazygit/issues/3558))
- tig has had no release since 2.6.1 (2024-06-13) though commits continue into 2026-07.
  Chunk-level staging, `@` jumps to the next chunk
- jj v0.44.0 (2026-08-06). `jj interdiff --from A --to B` rebases `--from` onto `--to`'s
  parent before diffing, which isolates what the author actually changed independent of
  how the base moved. `jj evolog -p` shows a change's full amend and rebase history with
  patches. Keyed to stable change IDs
- `git range-diff` (built in since 2.19) matches corresponding commits across a rebase,
  reorder, or squash and diffs their patches
- [difit](https://github.com/yoshiko-pg/difit), TypeScript, formerly named ReviewIt (same
  project, [renamed by the author](https://zenn.dev/yoshiko/articles/difit-from-reviewit)).
  Local web server rendering a GitHub-style diff over any commit-ish including
  `--pr <url>`. Its differentiator is copying comments out as an AI prompt
- Unverified leads: `MFAshby/prtool` (README says it does not work yet), `darccio/diffty`,
  `agynio/gh-pr-review`

## Seen-state across force-push

[Reviewable.io](https://docs.reviewable.io/files.html) is the only tool that solves this.
It tracks review state per file, per revision, and per reviewer, and diffs any revision
against any other. On force-push the affected revision is marked
[obsolete rather than deleted](https://www.reviewable.io/blog/completion-conditions-and-obsolete-revisions/),
so prior marks survive and render struck through.

Gerrit scopes its reviewed flag to one immutable patch set
(`PUT .../revisions/{revision-id}/files/{file-id}/reviewed`). A new patch set gets a new
revision id, so marks do not carry forward, and Code-Review votes reset too unless
`copyAllScoresOnTrivialRebase` is set. Diffing any two patch sets is supported, it is
only the propagation that is absent.

GitHub's viewed checkbox is binary per file with no line or hunk granularity, and the
docs state plainly that changed content unmarks it. There is no interdiff feature.

Phabricator has been deprecated since Phacility wound down on 2021-06-01 and its docs
site returned 503 throughout this research. Graphite has no primary doc describing an
independent mechanism, and its own docs imply it inherits GitHub's per-file behavior.
Flagged unverified rather than absent.

## Gap analysis

| Capability | Best coverage found | Verdict |
| --- | --- | --- |
| Hunk jump, mark seen | gitsigns (jump), octo.nvim (viewed, per file) | Partial, nothing does both at hunk granularity |
| Search filtered by seen state | diffnav searches, nothing filters | **None** |
| Jump to definition from inside a diff | codediff.nvim (real buffers) | Thin, breaks on historical revisions |
| Whitespace and syntax-aware toggle | difftastic (not a toggle), delta (delegates to `diffopt`) | Solved as a mode, not as an in-session toggle |
| Move detection with a linked side-by-side | mergiraf (merge only), semanticdiff (in-file) | **None** |
| Seen-state across force-push | Reviewable.io | One implementation, nothing git-native |
| LLM draft, hand-edit, persist, post later | prr (text, no LLM), hunk (JSON, no post), tuicr (posts, format unclear) | Partial, no tool has all three |
| Grouping files by relation | gh-dash groups PRs, not files | **None** |

Three capabilities have no implementation at all: seen and unseen search, relation-based
file grouping, and side-by-side move detection with a linked view of the origin. A
third-party survey of 27 diff and review tools reached the same conclusion
independently.

The seventh row is the one nearly built. prr's quote-guarded file plus hunk's validated
JSON comment batch are working halves of the same idea, and nobody ships the pair.
