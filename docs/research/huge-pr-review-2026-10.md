# Reviewing huge pull requests, October 2026

**Kyle's bot did stuff and wants it available for future bots.** Findings from an agent
research session on 2026-10-23 with minimal human review. Validate every claim before
building on it. "(unverified)" marks claims taken from search snippets or secondary pages
that could not be confirmed on the primary page.

This picks up where [diff-ordering-2026-09.md](diff-ordering-2026-09.md) stopped: what
2025-2026 tools and papers do to put a big change in a sensible reading order, and what a
terminal could borrow. The second half applies it to six real pull requests, 5 to 394
files, described by shape only because the repositories are private.

## 1. Tools that reorder or regroup a diff

- CodeRabbit Change Stack (2026-05-13) groups a PR into a few independent cohorts and splits each into ordered layers in "natural reading order". Each layer has a summary anchored to line ranges. UI: navigation left, diff center, per-range context right. `J`/`K` advance, `Z` is focus mode, files can be marked viewed, and a snapshot selector warns when the PR moved. The heuristic is undisclosed. https://www.coderabbit.ai/blog/introducing-change-stack-the-first-ai-native-code-review-interface
- Amp (2026-09-01) orders the Changes pane by importance and mutes tests, fixtures, and generated code below the core files. A button toggles against alphabetical order. A dot appears when the diff changed, and the user chooses when to re-apply the order. Criteria are undisclosed. https://ampcode.com/news/intelligently-ordered-diffs. Its review agent (2025-12-18) pre-scans and recommends a file order with per-file summaries. https://ampcode.com/news/agentic-code-review
- Sourcegraph Diff Tour (beta, 2026-08-17) puts important files first, groups the rest into sections, explains each section and file, tracks viewed files, and caches the tour for other reviewers. https://sourcegraph.com/changelog/guided-diff-tour-beta
- Linear Diffs (2026-05-28), guided reviews in beta, put the core change first and separate glue code. Each explanation says what the change is, then its consequences. The Reviews tab ranks by closeness to shipping. https://linear.app/changelog/2026-05-27-linear-diffs
- Baz Topics (undated, unverified) groups changes into 1 to 5 cross-file topics from the diff plus PR description, each scored 1 to 5 for importance. The docs page now redirects and 404s. https://docs.baz.co/changes/code-review/topics
- No reordering found for Copilot code review, which on 2026-08-27 dropped its 300-file / 20,000-line cap (https://github.blog/changelog/2026-08-27-copilot-code-review-resolution-reasons-and-expanded-capabilities). Same for Cursor Bugbot, which on 2026-06-10 added review of only what changed since the last review (https://cursor.com/changelog/bugbot-updates-june-2026). Same for Graphite Agent (2025-10-07, https://graphite.com/blog/introducing-graphite-agent-and-pricing).
- Greptile gives a 1 to 5 confidence score, a sequence diagram, and an "Important Files Changed" table with per-file scores. This rests on one Nov 2025 example PR and a marketing page (unverified). https://www.greptile.com/sequence-diagrams
- No ordering documentation was found for Gerrit, Critique, Phabricator, Reviewable, Jane Street Iron, Reviewpad, Ellipsis, or CodeSee.

## 2. Research on ordering and cognitive load

- Rahman, Codabux, and Roy, "Breaking the Alphabet" (ICSE 2026, presented 2026-04-16), surveyed 1,355 developers. Only 10.2% called alphabetical order optimal, and 66% asked for customizable ordering or dependency-aware grouping. https://conf.researchr.org/details/icse-2026/icse-2026-research-track/310/Breaking-the-Alphabet-Rethinking-File-Ordering-in-Code-Review (arXiv 2609.04207, whose listed date conflicts with its ID, so trust the ICSE page)
- Rahman et al., "Does Order Matter?" (arXiv 2026-09) mined 330,343 multi-file PRs. The latent-bug rate rises from 56.7% at file position 1 to 61.5% at position 30. Each extra file cuts the odds of any review comment by 8.7%. PRs of about 10 files had the lowest risk. https://arxiv.org/abs/2609.22610
- Bouraffa et al. (2025-06-12) examined 23,241 PRs. 44.6% were reviewed in non-alphabetical order. Of those, 20.6% followed largest-diff-first, 17.6% followed match to the PR description, and 29% of PRs touching production and test code were reviewed test-first. https://arxiv.org/abs/2506.10654
- Fregnan et al. (FSE 2022) found 64% lower odds of catching one defect type when its file came last. https://arxiv.org/abs/2208.04259. An undated talk transcript from the same group reports a test-first group 250% more likely to find the seeded test bug. https://neverworkintheory.org/static/2022-04/bacchelli-alberto-en.txt
- Baum et al. (ICSME 2017, ESE 2019): the 2017 theory says the best order mostly groups change parts by relatedness. The 2019 experiment (50 participants) found a moderate link between working memory and finding delocalized defects only. Both from abstracts and secondary sources. https://www.zora.uzh.ch/entities/publication/245eb643-3ebd-476c-a613-8097ff6d9582
- di Biase et al. (2018, revised 2020): decomposing a change gave fewer wrongly reported issues but no change in defects found. https://arxiv.org/abs/1805.10978. Gonçalves et al. (ESE 2022): a guided checklist helped on a simple review and a plain checklist on a complex one, with no strong overall link. https://link.springer.com/10.1007/s10664-022-10123-8
- LLM intent grouping: Atomizer (arXiv 2026-01-03, ICSE 2026) loops a grouper agent and a reviewer agent until each cluster shares one intent. It beats graph baselines by over 6.0% (C#) and 5.5% (Java), and by over 16% on complex commits. https://arxiv.org/abs/2601.01233
- A Technical Disclosure Commons proposal (2026-08-24) presents a PR as a "story" with diff ordering and tagging. The abstract gives no heuristics. https://www.tdcommons.org/dpubs_series/11460. No arXiv "review guide generation" paper was found.

## 3. Terminal and editor review UIs

- diffnav (Bubble Tea, wraps delta; v0.9.0 2026-02-07 per pkg.go.dev) shows the file tree by default at width 26. `e` toggles the tree, `s` toggles split and unified, `t` goes to a file, `n`/`p` step files. https://github.com/dlvhdr/diffnav
- Hunk (undated, 2026) has a sidebar and `auto_split_min_width = 160` diff columns before auto mode goes split. Agent annotations stay hidden until `a`, and agents drive a live session through a skill. https://github.com/modem-dev/hunk
- critique switches split and unified automatically by width with no published cutoff, plus a fuzzy file switcher (`p`) and `--watch`. https://github.com/remorses/critique (undated)
- tuicr 0.21.0 (2026-08-05) streams all files in one scroll. It has line, range, file, and review-level comments, with comment types configurable by id, color, and meaning. `{`/`}` jump files, `[`/`]` jump hunks, and `:submit` posts to GitHub, GitLab, or Bitbucket. https://docs.rs/crate/tuicr/0.21.0
- diffview.nvim (undated) has a 35-column file panel in tree style, two-pane horizontal diff by default, and `g<C-x>` to cycle layouts. https://github.com/sindrets/diffview.nvim
- Zed's Review Changes multibuffer shows every agent edit with keep or reject per hunk or for the whole set. A crosshair toggle makes the editor follow the agent. https://zed.dev/docs/ai/agent-panel (undated)
- Warp collects inline comments in a list, submits them as one batch, and returns an updated diff. It works with Claude Code and Codex CLIs. https://docs.warp.dev/agent-platform/warp-agents/interactive-code-review (undated)
- Claude Code desktop has a file list left, per-file diff right, and batched line comments (Cmd+Enter). Its "Review code" action flags only compile errors, definite logic errors, security issues, and obvious bugs, and skips style. https://code.claude.com/docs/en/desktop (undated, search snippet)
- Cursor 3's Agents Window uses file-level diffs with auto-apply. Per-change keep/undo needs the Inline Diffs setting (forum threads only, unverified). https://forum.cursor.com/t/when-using-agents-window-should-approve-undo-diffs-be-visible-and-actionable-in-the-files-tab-and-other-questions/160526
- Not researched: delta, gh-dash, lazygit, tig, octo.nvim, neogit, and jj's tools. difftastic 0.69.0 (year unconfirmed) treats trailing-punctuation-only changes as no syntactic change for several languages. https://github.com/Wilfred/difftastic/releases

## 4. ACP and reviewing agent-written changes

- ACP tool calls carry `content` of type content, diff (`path`, `oldText`, `newText`), or terminal. They carry `locations` (`path`, optional `line`) that clients use for follow-along, a `kind` (read, edit, delete, move, search, execute, think, fetch, and more), and a status lifecycle. https://agentclientprotocol.com/protocol/tool-calls
- ACP plan entries have `content`, `priority` (high, medium, low), and `status`. Each update fully replaces the plan. https://agentclientprotocol.com/protocol/agent-plan
- Zed launched ACP in August 2025 with follow-along plus multibuffer review. https://zed.dev/blog/bring-your-own-agent-to-zed
- "Habituation at the Gate" (arXiv 2026-06-21, KDD 2026 workshop) studied 11,429 reviews by 400 reviewers. Approval rose from 30.1% to 36.8%, latency rose about 3.5x, and inline comments fell 22%. https://arxiv.org/abs/2606.22721
- "Early-Stage Prediction of Review Effort in AI-Generated PRs" (arXiv 2026-01-02, MSR '26) covers 33,707 agent PRs. File types and patch size give AUC 0.96, and a 20% review budget captures 69% of the high-effort PRs. https://arxiv.org/abs/2601.00753
- No product was found that clusters an agent diff by inferred intent. The closest are open-source skills that split by concern into stacks, one of which checks that the top of the stack is byte-identical to the original diff. https://tessl.io/registry/skills/github/RoleModel/rolemodel-skills/split-stack/review

## 5. Triaging hunks with rules, classes, and risk

- Meta's Diff Risk Score (2025-08-06) is a fine-tuned Llama model that outputs a risk score and highlights risky snippets. https://engineering.fb.com/2025/08/06/developer-tools/diff-risk-score-drs-ai-risk-aware-software-development-meta/
- Meta's attention-based explainer (arXiv 2026-07-02, AIware '26) lifts attention to lines, hunks, and files. The top 2 hunks cover 53.85% of outage-causing lines while reviewers read 26.28% of changed lines. Hunk level beat line and file level. Early feedback was mostly negative (48 of 55), citing highlights on tests, generated code, and boilerplate. https://www.alphaxiv.org/abs/2607.02782.md
- Qodo's review-depth router (2026-07-30) first judged effort from the diff and scored about 4% below baseline because it treated size as difficulty. Hunk count (dispersion) beat line count, and its Deep gate needs both minimum lines and minimum hunks. Generated files are skipped. https://www.qodo.ai/blog/building-an-adaptive-router-for-code-review-depth/
- Refactoring-aware review is older. RAID cut the median lines to read for move refactorings from 14.5 to 2. https://arxiv.org/abs/2103.11453v1. RefactorInsight auto-folds detected refactorings. https://arxiv.org/pdf/2108.11202
- Rule files: `linguist-generated` in `.gitattributes` keeps matching files out of GitHub diffs by default (https://docs.github.com/en/enterprise-server@3.15/repositories/working-with-files/managing-files/customizing-how-changed-files-appear-on-github). CodeRabbit `path_filters` and `path_instructions` (https://docs.coderabbit.ai/configuration/path-instructions, snippet only). Greptile's `.greptile/` config cascades per directory with strictness 1 to 3 (https://www.greptile.com/docs/code-review/greptile-config, snippet only).
- `pr-review-order` (undated, Python only) topologically sorts files by import and symbol dependencies and puts tests after the code they cover. https://github.com/Goncalo-Chambel/pr-review-order. The test-first evidence in section 2 contradicts its tests-last rule, so that position wants to be a setting.

## 6. What six real reviews showed

One agent per pull request worked out the walk a reviewer would want: a walking skeleton of 3 to 8 hunks that shows the change end to end, then chapters, and for each chapter what decided it. The six were a 5-file bug fix, a 9-file behavior narrowing, a 27-file feature with a generated client, a 91-file feature stacked on a schema PR, a 189-file performance sweep, and a 394-file bundle of 13 customer follow-ups.

- The skeleton was always the same five or six roles in some order: data shape, entry point, core decision, side effect, boundary, and the test that proves it. It covered 6% of added lines on the 91-file PR and an estimated 35 to 40 minutes against 3 to 4 hours for the whole
- The cheapest skeleton source was the PR's own description. On the 27-file PR, the `blob/<sha>/path#Lnn` anchors in the author's AI Summary comment matched 4 of 5 skeleton stops. On the 189-file PR the summary ranked 30 fixes, which the agent called "the author's own walk order, which a tool could ingest but not derive". A linked design doc with a mermaid diagram was the skeleton on the 91-file PR
- Rules reproduced most chapters: generated clients and locale copies (28% and 11% of added lines on the 91-file PR, 54% generated on the 27-file PR), models before migrations before logic, a layer order of routes, accessors, processors, then connectors, tests placed under their subject by stem and imports, a new parameter threaded through many hunks as one exemplar plus a list, and import-only or docstring-only hunks folded
- A rule could not catch the exception inside a pattern. On the 27-file PR a field threaded through 20 hunks also reached a public API schema whose handler never reads it. On the 5-file PR one of the identical forwards passes `None`. Both were the most important findings in their chapter
- Shape clustering is a chapter mode, not a layout. On the 189-file sweep only about 40 files shared an edit shape and the rest were bespoke
- Commits rarely helped. The 394-file PR had one commit of 211 files spanning six items, and the 27-file PR's commits were mostly merges and stack parents
- Past a few hundred files the useful output was a split. The 394-file PR's features share files in clusters, and the clusters are a seven-PR split
- `gh pr diff` returns HTTP 406 past 300 files ("the diff exceeded the maximum number of files"), so a tool that reads the diff that way cannot open the largest PRs at all. The paged `pulls/<n>/files` endpoint still works, and omits `patch` for very large files

## Ideas a TUI could steal

1. Two-level navigation of cohort, then ordered layers, each anchored to line ranges, with `J`/`K` to advance (CodeRabbit Change Stack)
2. An order toggle (alphabetical, intelligent, or a rule-based third) and a "diff changed, re-apply?" marker so the order never shifts silently (Amp)
3. Mute tests, fixtures, and generated files instead of hiding them, and make tests-first or tests-last a setting (Amp, Bouraffa et al.)
4. Core change first, glue separated, and a one-line "what, then consequences" per section (Linear Diffs)
5. At most 5 topic groups, each with a 1 to 5 importance (Baz Topics)
6. A review-cost chip based on hunk count and dispersion, not line count (Qodo router)
7. A viewed gutter and per-file coverage indicator, so attention decay on later files shows (Sourcegraph Diff Tour, "Does Order Matter?")
8. Risk shown at hunk granularity only, suppressed for tests, generated code, and boilerplate (Meta attention paper)
9. Split above about 160 diff columns, otherwise unified, tree about 26 columns, `e` toggles the tree, `s` toggles split (Hunk, diffnav, critique)
10. Hidden-by-default annotations revealed with a key, plus typed comment categories (Hunk, tuicr)
11. Batch comments into a list and submit as one unit (Warp, Claude Code desktop, tuicr `:submit`)
12. Rule globs feeding a "mechanical" bucket with a visible "N files folded by rule" row, refactor-move folding, and a check that every hunk lands in exactly one group (`linguist-generated`, RefactorInsight, split-stack's byte-identical invariant)
