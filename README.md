# second-look

Prepare a code review locally, with Claude Code drafting comments you edit by hand, then
post it to GitHub in one deterministic call.

Fetching a pull request, staging comments, and posting them all work. The TUI that
replaces reading the diff in `$EDITOR` is not built yet.

```bash
second-look get 42        # fetch the PR, check it out, cache the diff
# an agent drafts comments through the change-review skill
second-look post 42       # post the review in one call
```

Every comment is anchored to the diff line it points at. A comment on a line the diff
does not carry is refused while staging, and one whose line has moved since is refused
before anything is sent.

- [Next steps](NEXT_STEPS.md) — what alpha needs, in order, and what is still open
- [Requirements](requirements.md) — scope, decisions made, and what is still open
- [Prior art, August 2026](research/prior-art-2026-08.md) — what already exists in this
  space and the three capabilities nothing implements

## Installation

Homebrew, once a release exists:

```bash
brew install --cask kyleking/tap/second-look
```

Or from source:

```bash
go install github.com/kyleking/second-look/cmd/second-look@latest
```

The binary is `second-look`. Every command below is short enough to type through an
alias, which is what I use:

```bash
alias sl=second-look
```

## Development

```bash
mise install && hk install --mise
mise run ci
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full workflow.
