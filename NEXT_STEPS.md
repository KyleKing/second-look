# Next steps

What alpha needs, in the order it wants doing. Scope and priorities live in
[requirements.md](requirements.md), screens in [DESIGN.md](DESIGN.md).

## What alpha means

I can review one of my own pull requests end to end without hand-writing TOML, and post
it. That is the bar. No TUI, no seen-state, no inbox, no rating.

Concretely, this works and nothing in it is faked:

```sh
sl get 42                 # fetch the PR, build the artifact
# claude drafts comments through the change-review skill
sl show 42 --payload      # confirm what leaves the laptop
$EDITOR .second-look/pr-42.toml
sl post 42
```

## Built

`internal/artifact` holds the schema, the TOML store, and the payload builder, with the
posted and local split enforced by the builder rather than by a list. `sl comment add`,
`sl show`, `sl show --payload`, `sl post`, and `sl post --dry-run` work. Seven tests
cover the split, the draft refusal, replies, the round trip, unknown keys, and the
all-problems-at-once validator.

The `change-review` skill drafts through `sl` and no longer writes a markdown staging
file. The original is backed up at `~/.claude/change-review-pre-sl.bak/`.

## 1. Scaffold from my_go_template

Nothing else is safe to build on until the repo has the tooling every other Go project
here has. `second-look` is currently a bare `go.mod` with no linter, no hooks, no CI, and
no release path.

```sh
copier copy gh:KyleKing/my_go_template . \
  --data project_name=second-look --data project_type=cli --data use_goreleaser=true
mise install && hk install --mise && mise run ci
```

`_skip_if_exists` covers `DESIGN.md`, `README.md`, and `go.mod`, so the docs and the
module path survive. It runs `git init` as a task, which is a no-op here.

What arrives: `hk.pkl`, `.golangci.toml`, `.config/mise.toml`, `.goreleaser.yml`,
`.cz.toml`, `.typos.toml`, `.ls-lint.yml`, `.editorconfig`, `AGENTS.md`,
`CONTRIBUTING.md`, `CHANGELOG.md`, `LICENSE`, and `.github/`.

Then fix what the linter finds. `golangci-lint` has never run on this code.

## 2. `sl get`

The one piece of the pipeline still done by hand. Fetch the pull request, resolve the
head SHA, write the artifact, and cache the diff.

- Read the PR through `gh`, which means `aragonite/forge` (extraction 2, below) or a
  temporary local copy
- Refuse when HEAD does not match the PR head, showing the mismatch and the resolution
  as described in requirements. A dirty tree never blocks
- Cache the diff under `.second-look/`, keyed by head SHA, since the anchor guard and
  every later feature reads it

## 3. The anchor guard

Currently `sl` validates shape and not truth, and the skill carries the gap as the
agent's job (Step 0). That is the right stopgap and the wrong resting place, because a
bot citing line 993 of a 137-line file is the single most common failure in this
workflow.

prr is the reference: quote the diff line each comment anchors to, and compare it byte
for byte against the live diff before posting. Refuse on a mismatch and say which comment
moved. Once this lands, cut Step 0 from the skill.

## 4. Extract `forge` and `vcs` into aragonite

`sl get` and `sl post` both want the `gh` wrapper that gh-repo-dashboard already has, and
the extraction pattern is fresh from the cache move. `docs/extraction.md` in aragonite
records what that one taught.

The open question below about `models.PRInfo` decides how large this is.

## 5. Ship the skill with the tool

`sl --help` already documents the full contract, so the skill mostly points at it. The
global `change-review` skill and the tool can now drift, and the tool is the thing that
knows its own schema.

hunkdiff's pattern is to ship the skill inside the installed package and have a thin
global skill point at it. Worth copying, and it needs a decision about where a Homebrew
or `go install` build puts the file.

## 6. Publish aragonite

`go mod tidy` fails in both consumers today because aragonite has no published version.
`go build` and `go test` work through the gitignored `go.work`, so this is friction
rather than breakage, and it blocks CI, which has no workspace.

This has to happen before step 1's `mise run ci` can pass in GitHub Actions.

## Open questions

**Does `models.PRInfo` move into `forge`.** Both the filter atoms and the cache payloads
are typed on it, so sharing either means moving the type and having gh-repo-dashboard
alias it back. Same seam the cache extraction used, much larger blast radius, since
`models` is imported almost everywhere. This gates step 4.

**Where a shipped skill lives on disk.** A Homebrew cask and `go install` put the binary
in different places and neither has an obvious home for a skill file. Options are
embedding it in the binary behind `sl skill --print`, installing it to
`~/.claude/skills/` on first run, or keeping the global skill hand-maintained and
accepting the drift.

**Whether alpha needs the TUI at all.** The pipeline works without it and the CLI is what
the agent drives. The counterargument is that reviewing through `$EDITOR` on a TOML file
is exactly the experience this tool exists to replace, so a CLI-only alpha proves the
plumbing and none of the premise.

**How `sl get` handles a PR with no local checkout.** Fetching to `FETCH_HEAD` works for
reading, and the requirements say a wrong branch is refused. A PR against a repo not
cloned locally has no branch to be wrong about, and it is unclear whether that is in
scope or an error.

**Whether the artifact should record what was posted.** After `sl post`, GitHub assigns
ids the artifact does not know. Without them a later `sl` cannot tell an already-posted
comment from a new one, which matters the moment a review gets a second round. Probably a
`posted_id` local field written back on success.
