# Next steps

What alpha needs, in the order it wants doing. Scope and priorities live in
[requirements.md](requirements.md), screens in [DESIGN.md](DESIGN.md).

## What alpha means

I can review one of my own pull requests end to end and post it, reading the diff in the
TUI rather than in `$EDITOR`. No seen-state, no inbox, no rating.

The TUI is in scope because reviewing a diff through a text editor on a TOML file is the
experience this tool exists to replace. A CLI-only alpha would prove the plumbing and
none of the premise.

Concretely, this works and nothing in it is faked:

```sh
sl get 42                 # fetch the PR, check it out, build the artifact
# claude drafts comments through the change-review skill
sl 42                     # read the diff, edit comments, submit with a key
```

## Decided since

The artifact is deleted on a successful post, and GitHub becomes the source of truth from
that moment. The inbox reads submitted reviews back from the API for its reviewed-and-open
and reviewed-and-merged buckets, so no comment ids are written back, nothing local
outlives the post, and the schema loses a field rather than gaining one.

`models.PRInfo` is now `forge.PullRequest` in aragonite, moved outright with no alias.

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

- Read the PR through `gh`, which means the forge client (step 4)
- Check the PR out. A checkout has to move the working tree, so it needs a clean one and
  errors otherwise, in git and jj alike. Being already on the PR head with uncommitted
  changes is fine and never blocks, because refusing to review a branch you already have
  because you have unstaged edits would be wrong
- Show the mismatch and the resolution as a keybinding when HEAD is not the PR head, and
  say how many in-progress comments survive the move
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

## 5. `sl skill`

`go:embed` the skill file and print it to stdout. The same build produces the binary and
its documentation, so the two cannot disagree about the schema.

```sh
sl skill                                  # read it
sl skill > ~/.claude/skills/change-review/SKILL.md
```

hunk does this as `hunk skill path`, which returns a path into its installed npm package
and lets a thin global skill point at the file. A Go binary has no package directory, so
printing the content is the equivalent and it is simpler. It is also better here: the
global skill can say "run `sl skill` for the current contract" and be read fresh every
time rather than copied once and left to rot.

## 6. Publish aragonite, and keep it published

`go mod tidy` fails in both consumers today because aragonite has no published version,
and CI has no workspace to fall back on. So this blocks step 1's `mise run ci` in GitHub
Actions even though it is listed last.

The standing workflow after that:

- `go.mod` always pins a released aragonite version. The gitignored `go.work` overrides
  it while working across the two repos
- `GOWORK=off` is what proves the consumer builds against the published version rather
  than the checkout on disk. Without it, local green says nothing about CI
- hk runs the workspace-free check on **pre-push**, not pre-commit. Committing mid-change
  against a local aragonite is the normal state; pushing a consumer that needs an
  unpublished library is the mistake worth catching

```sh
# .config/mise/conf.d/*.toml
[tasks.verify-released]
run = "go test ./..."
env = { GOWORK = "off" }
```

Releasing aragonite is `cz bump` plus a pushed tag, since the Go module proxy resolves a
library from its tag and no goreleaser is involved. Consumers then `go get` the new
version and commit the `go.mod` change.

## Open questions

**How `sl get` handles a PR against a repo not cloned locally.** There is no branch to be
wrong about and nothing to check out. Whether that is in scope or an error is undecided.

**Which of the moved display helpers belong in `forge`.** `ReviewGlyph` and
`StatusDisplay` emit specific glyphs, which is a rendering choice sitting in a data
package. They moved with their types because splitting them would have doubled the churn,
and both tools do render pull requests. Revisit when `tui/` exists.

**Whether the review-cost rating needs its own package.** It reads the diff, the symbol
graph, and the changed symbols, so it may want to live in aragonite next to `codeintel`
rather than in second-look. Undecided until it is written.
