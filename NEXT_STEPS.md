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
second-look get 42        # fetch the PR, check it out, build the artifact
# claude drafts comments through the change-review skill
second-look 42            # read the diff, edit comments, submit with a key
```

## Decided since

The artifact is deleted on a successful post, and GitHub becomes the source of truth from
that moment. The inbox reads submitted reviews back from the API for its reviewed-and-open
and reviewed-and-merged buckets, so no comment ids are written back, nothing local
outlives the post, and the schema loses a field rather than gaining one.

`models.PRInfo` is now `forge.PullRequest` in aragonite, moved outright with no alias.

`forge` holds data and predicates only. Everything that emits a glyph, a placeholder, or
a human-readable duration moved to gh-repo-dashboard's `internal/ui`, which becomes
`aragonite/ui` once second-look has a TUI and gh-sweep is being cut over. gh-sweep is the
third consumer: it carries its own `internal/tui/theme` with terminal detection, while
gh-repo-dashboard has `styles`, `table`, and `markdown`.

`my_go_template` now ships a `verify-released` task (`GOWORK=off`, build and test) and
runs it as an hk **pre-push** step. Committing against a local sibling checkout is the
normal state; pushing a module that only builds against an unpublished one is the
mistake. gh-repo-dashboard has it, and it currently fails there by design, which is what
blocks pushing it until aragonite is released.

## Built

`internal/artifact` holds the schema, the TOML store, the payload builder, and the
anchor guard, with the posted and local split enforced by the builder rather than by a
list. `internal/diff` parses the unified diff both halves of the guard read.
`second-look get`, `comment add`, `show`, `show --payload`, `post`, and `post --dry-run`
all work, smoke-tested end to end against a real pull request.

The `change-review` skill drafts through `second-look` and no longer writes a markdown
staging file. The original is backed up at `~/.claude/change-review-pre-sl.bak/`.

## 1. Scaffold from my_go_template — done

Scaffolded from template v0.11.4 with `project_name=second-look`. The binary is
`second-look`, aliased to `sl` in the README, because copier keys the entrypoint
directory, the goreleaser build, and the gitignored binary path off `project_name`, and
a repo whose binary is named something else gets two entrypoints. `golangci-lint` had
never run on this code and found 75 issues; all of them are fixed.

Two things [my_go_template](https://github.com/KyleKing/my_go_template) needed:

- `verify-released` and its hk pre-push step exist at the template's HEAD but not at the
  v0.11.4 tag, so they were copied in by hand. A `copier update` after the next release
  should be a no-op
- tombi keeps a TOML array multi-line only when it has a trailing comma, and none of the
  template's arrays had one, so `hk check` failed on a fresh scaffold and the fix
  collapsed `.golangci.toml` into 300-character lines. Fixed in the template and in
  gh-repo-dashboard, which had the same failure, and unreleased there too

Two settings this repo owns rather than inherits. `fieldalignment` is off, because
go-toml writes keys in declaration order and packing `Review` and `Comment` would
scramble the file a person hand-edits. `_skip_if_exists` kept `DESIGN.md`, `README.md`,
and `go.mod`.

## 2. `second-look get` — done

It reads the pull request through `aragonite/forge/github`, moves the working copy onto
its head, writes the artifact, and caches the diff under `.second-look/diff/` keyed by
head commit. It never clones.

Moving the working tree needs a clean one, and being already on the pull request head
never blocks however dirty the tree is. Already on the branch but behind, it tries
`git pull --ff-only` and stops with git's own reason when that refuses. **Never
`--autostash`**: on git 2.x `--ff-only --autostash` against a dirty file the pull also
touches exits 0 while leaving `UU` conflict markers in the tree and the stash still on
the stack, so a tool reading the exit code walks into a review of a conflicted tree.

jj needs none of the guard, since its working copy is a commit and a fetch has nothing
uncommitted to clobber. The jj paths are written and untested: I have no colocated jj
checkout with an open pull request to run them against.

Re-running `get` after the head moves says how many staged comments came with it, and
the anchor guard re-checks each one on post. The keybinding that resolves the mismatch
waits for the TUI.

One thing the plan missed. `.second-look/` is untracked, so second-look's own state
counted against the clean-tree guard and `get` refused to run a second time. The
artifact tree now carries a `.gitignore` of its own, which is also what keeps the
prepared review out of the user's commits.

## 3. The anchor guard — done

Two checks rather than one, because the failure has two shapes.

Staging resolves each comment against the cached diff and quotes the line it anchors to
into the comment's `anchor` field. A comment on a line the diff does not carry is
refused with nothing written, which is where a bot citing line 993 of a 137-line file
now gets caught. GitHub refuses that comment anyway, so this only moves the refusal
somewhere a person can read it.

Posting re-reads the live diff and compares those quotes byte for byte, and refuses
outright if the pull request has new commits. `internal/diff` is the parser both sides
share: it reads only what an anchor needs, so a rename or a binary payload is skipped
rather than modeled.

Step 0 is cut from the skill, replaced by `sl get <pr>` and a note that anchors are
checked from there.

## 4. Extract `forge` and `vcs` into aragonite — done

`aragonite/vcs` holds git and jj behind one interface, and `aragonite/forge/github`
holds the `gh` wrapper. gh-repo-dashboard reads both and no longer carries
`internal/vcs`, `internal/github`, or `internal/cache`, the last of which had nothing
left of its own once the caches typed on forge and vcs values moved with them.

`RepoSummary` split rather than moved whole. `vcs.RepoSummary` is what a checkout says
about itself, and gh-repo-dashboard's `models.RepoSummary` embeds it and keeps `PRInfo`,
`WorkflowInfo`, `TemplateInfo`, `NotesFiles`, `Loading`, and `Error`. The glyph and
duration methods became `ui` functions, which is the same rule the pull request move
settled. `models.VCSType` is `vcs.Type`, since `vcs.VCSType` stutters.

The gh implementation went to `forge/github` rather than into `forge`, so `forge` keeps
the host-neutral model and GitLab is a sibling directory rather than a rename. The
interface itself waits for a second implementation to shape it.

gh-repo-dashboard's suite passes and it now lints clean, down from 121 issues, because
the packages carrying most of them left.

## 5. `second-look skill`

`go:embed` the skill file and print it to stdout. The same build produces the binary and
its documentation, so the two cannot disagree about the schema.

```sh
second-look skill        # read it
second-look skill > ~/.claude/skills/change-review/SKILL.md
```

hunk does this as `hunk skill path`, which prints a path to a skill file bundled in its
install tree and tells you to load or symlink it. That path is version-pinned
(`/opt/homebrew/Cellar/hunk/0.18.1/libexec/skills/hunk-review/SKILL.md`), which is why
the command exists at all: a symlink to it breaks on the next upgrade, so the agent has
to ask each time.

Printing the content sidesteps that. The global skill says "run `second-look skill` for
the current contract" and reads it fresh, so there is no path to go stale and no copy to
rot.

Two things to copy from hunk's skill, which is 184 lines:

- YAML frontmatter with `name` and `description`, so what `second-look skill` prints is
  a complete skill file that needs no assembly
- An opening line telling the agent **not** to launch the TUI. hunk's says the TUI is for
  the user and the agent drives `hunk session *` instead. Once `second-look <pr>` opens
  a review, an agent that runs it will hang on a terminal nothing is attached to

## 6. Publish aragonite, and keep it published

`go mod tidy` fails in both consumers today because aragonite has no published version,
and CI has no workspace to fall back on. So this blocks step 1's `mise run ci` in GitHub
Actions even though it is listed last. `mise run verify-released` now fails in
second-look too, for the same reason and by the same design.

aragonite still has no tooling of its own: no `.golangci.toml`, no mise config, no hk
setup. It was linted here with a copy of gh-repo-dashboard's config, which is not a
thing anything enforces. Scaffold it before the first release rather than after.

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

**The 70% coverage floor is not enforced anywhere.** `mise run test:coverage-min`
exists, CI does not run it, and it has never passed: 33.5% before this work and 39.9%
after. `internal/get` is the largest gap, and it is untestable as written because
aragonite's command stubs are exported only to aragonite's own tests. Either widen those
or drop the task.

**Whether the review-cost rating moves to aragonite.** It reads the diff, the symbol
graph, and the changed symbols, so it may belong next to `codeintel` rather than here.
Not a question worth answering before it is written: extract it if a second tool wants
it, and leave it here otherwise.

**Where the tests for moved code should live.** Settled by the vcs and forge moves:
tests go with the code. The `RepoSummary` predicate tests are in `aragonite/vcs`, the
disk and registry wiring tests are in `aragonite/forge/github`, and the display tests
are in gh-repo-dashboard's `internal/ui`. The pull request tests still sitting in
`internal/app` from the earlier display split are the remaining strays.
