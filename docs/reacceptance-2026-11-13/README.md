# Reacceptance report — 2026-11-13

This is a technical recheck of Bint source and existing tests, not a user approval. The audit baseline is HEAD `d788669b8f9754c21a94fb8324a65731b2edcc38` (tree `74735317002625692ca9f6a51d74f53073af88d6`), captured on 2026-11-13 in `Asia/Ho_Chi_Minh` (`+07:00`). The source tree was clean at capture. The task-specific diff includes `.bint/config.yml`: its captured task base tree `1a15e30d85994cad8085e524422cc2bdae1792e4` has reviewer/evolver set to `gpt-6.1-sol`, while current HEAD and the worktree have both set to `gpt-6-astra`. After that capture, the user explicitly confirmed «Đúng, thay đổi của tôi; giữ lại» for these two model changes. The hunk remains in the task diff and is user-owned, separate from the audit's report artifacts; it is not an agent scope violation and does not make AC-3 unsatisfied. The current working-tree diff against HEAD is empty for `.bint/config.yml` and `local.yml`. This audit did not edit either config file, Git metadata, snapshots, task state, or original evidence logs. The confirmation is recorded as a later addendum; the original capture remains intact. No product code, historical acceptance report, or eval data was edited.

## Overall result

The repository has broad implemented functionality with automated coverage, but the whole-suite command is not green in this managed sandbox, several current P6 safety paths remain exposed in source, empirical gates remain unaccepted, Claude-only runtime acceptance was intentionally deferred, and the proposed MCP gateway is not implemented at this HEAD.

| Plan group | Current conclusion |
|---|---|
| Core P0 | **đạt chức năng trên phạm vi đã kiểm** — init/state/spool and shared worktree fixtures. |
| Core P1 | Function contracts **đạt chức năng trên phạm vi đã kiểm**; real evaluation acceptance **tạm hoãn/thiếu bằng chứng**. |
| Core P2 | Bootstrap/knowledge contracts **đạt chức năng trên phạm vi đã kiểm**; gate-1 **tạm hoãn/thiếu bằng chứng**. |
| Core P3 | Context retrieval/MCP/path-hook contracts **đạt chức năng trên phạm vi đã kiểm**; gate-2 **tạm hoãn/thiếu bằng chứng**. |
| Core P4 | **đạt chức năng trên phạm vi đã kiểm** for fixture workflow and task-artifact flow. |
| Core P5 | **đạt chức năng trên phạm vi đã kiểm** for current Codex/Antigravity adapter contracts; Claude runtime parity **tạm hoãn/thiếu bằng chứng**. |
| Core P6 | **chưa đạt** — source inspection confirms unresolved P6-01..05 hazards in lock recovery, dirty-file detection, partial rollback, update provenance and merge content preservation (DEF-002, DEF-014, DEF-015); independent-set benefit is not demonstrated. |
| Core P7 | Skill lifecycle contracts **đạt chức năng trên phạm vi đã kiểm**; independent-set usefulness **tạm hoãn/thiếu bằng chứng**. |
| Core P8 | Cycle/lesson/harness deterministic contracts **đạt chức năng trên phạm vi đã kiểm**; measured effectiveness **tạm hoãn/thiếu bằng chứng**. |
| Evolution E1–E5, E6 tests, H1–H4 | **đạt chức năng trên phạm vi đã kiểm** for deterministic fixtures and contracts. |
| Evolution E6 measurement, H5 | **tạm hoãn/thiếu bằng chứng**; no live/paid evaluation was run. |
| TUI 0–6b and 12.11 | **đạt chức năng trên phạm vi đã kiểm** for code, fixture flows, and golden render tests; owner approval, real timings, users, terminal/editor/restart/Windows remain **tạm hoãn/thiếu bằng chứng** (DEF-004). |
| Isolation G1/G2/G3a/G3b/G4 | G2 and fixture contracts in G1/G3/G4 **đạt chức năng trên phạm vi đã kiểm**; full G1 acceptance (50-run race repetition/static inventory/staticcheck) and Claude runtime proof for G3/G4 **tạm hoãn/thiếu bằng chứng** (DEF-003/005/012/013). |
| Isolation spikes S1–S3 | Prior write-up catalogued; fresh runtime confirmation **tạm hoãn/thiếu bằng chứng** (DEF-003). |
| MCP M0–M4 | **chưa triển khai** as defined by the gateway plan (DEF-007). Existing Context Engine MCP server is a separate feature. |
| MCP M5, gates, A/B, P6–P8 experiments | **tạm hoãn/thiếu bằng chứng** (DEF-001, DEF-006, DEF-008); A's snapshot import prerequisite is **chưa triển khai** (DEF-011). |

## Reports and inventory

- [Core P0–P8](core.md) — each phase's acceptance row, source/test references, functional versus experimental conclusion.
- [Evolution E1–E6 / H1–H5](evolution.md) — lessons and harness-component work.
- [TUI](tui.md) — 0, 1, 2, 3, 4, 5a, 5b, 6a, 6b and supplemental 12.11.
- [Isolation/subagents](isolation.md) — G1, G2, G3a, G3b, G4 and S1–S3.
- [MCP gateway](mcp.md) — M0–M5, distinct from existing `mcpserver` Context Engine.
- [Experiments](experiments.md) — gate-1/2, A/B, E6/H5/M5 and all eval PLAN/task-plan artifacts found.
- [Deferred register](deferred.md) — every observed failure, incomplete implementation, missing evidence, skipped Claude requirement and rerun condition.

The inventory search enumerated the 49 Markdown documents present under `docs/` at the baseline. It identifies six canonical project plans, gate-1/2, two Codex flow `PLAN.md` files, three embedded mixed-eval task plans, historical acceptance/evaluation reports, and one task-evidence directory with a review but no plan. See [`evidence/inventory-search.txt`](evidence/inventory-search.txt). Historical reports are discovery context, not new evidence.

Baseline warning: the MCP M0–M4 conclusion above is for `d788669`; the supplemental audit of delta `d788669..04214d8` is in [update-04214d8](update-04214d8/README.md). Its source, fixture, live and measurement conclusions are separate; it does not rewrite this report's history.

Stage counts represented: P0–P8 (9); E1–E6/H1–H5 (11); A/B (2); TUI 0–6b including split 5a/5b and 6a/6b, plus 12.11 (10); G1/G2/G3a/G3b/G4 and S1–S3 (8); M0–M5 (6); gate-1/2 (2). All rows link to source/test evidence where present; absent or human/empirical evidence is registered as deferred rather than filled with an assumed result.

## Evidence and command logs

Environment versions, baseline caveats, role/model selection, exact command timestamps/cwd/exit codes and raw output files are indexed in [`evidence/commands.md`](evidence/commands.md). Baseline material: `evidence/baseline.txt`, `baseline-caveats.txt`, `index-hash.txt`; final HEAD/status/index hash: `evidence/final-state.txt`. The tracked documentation diff is preserved in `evidence/final-tracked-diff.patch`; SHA-256 entries for report artifacts are in `evidence/artifact-hashes.txt`. The initial Git status was clean; `.git/index` SHA-256 is recorded. `git write-tree` could not create `.git/index.lock` in the read-only Git metadata directory, so a tree object for the index could not be generated (DEF-009). The task diff's `.bint/config.yml` hunk, its base/current comparison, and exact values remain documented in `evidence/task-diff-config.txt`; the user's later ownership confirmation is recorded in `baseline-caveats.txt`, `commands.md`, and DEF-017. The captured diff remains historically accurate; no resnapshot or Git metadata change was made.

Report navigation check after the review-3 updates: 50 local Markdown links and anchors checked against their actual destinations, 0 errors (`evidence/report-links-r3.txt`). Source line citations appear as text beside links and are checked separately: 141 recognized file/line references with no missing or out-of-range locations (`evidence/source-refs-r3.txt`). The checker validates existence/range only; direct package-name references were also searched and the cited `internal/hcomp`/`internal/ctxengine` files were read. The three embedded toy-task plans are itemized criterion-by-criterion; their referenced `calc.go`/`calc_test.go` artifacts are absent here (DEF-016).

| Check | Command | Result |
|---|---|---|
| T-TEST | `go test -count=1 -json -timeout 20m ./...` | **Exit 1**. 1,565 test pass actions, 5 failed test actions plus one package fail action, and 1 skipped test plus 11 packages without test files. The restricted RPC fixture was denied loopback bind in this environment; see DEF-010. No live model was invoked. |
| T-VET-LINUX | `go vet ./...` | Exit 0. |
| T-VET-WINDOWS | `GOOS=windows go vet ./...` | Exit 0; this is not Windows runtime testing. |
| T-SC | staticcheck v0.7.0 installation and Linux/Windows analysis | **Not run**. No binary existed and module download was blocked by DNS/network restrictions; DEF-005. |
| T-RACE | `go test -race -count=1 -json -timeout 20m ./...` | **Exit 1**. Same restricted RPC fixture loopback-bind failure under race; no race report was emitted before failure. See `evidence/go-test-race.meta` and DEF-010. |

No test result is treated as proof of human usability, cross-platform runtime, effect size, model quality, or generalization. The task's confirmed `go test ./...` is owned by Bint Core; this b-feat run does not call `bint task verify`, change task criteria, or change task state. Core verify-3 passed `go test ./...` as recorded separately in the original task log `logs/verify-3.log`; that result is distinct from this audit's uncached sandbox run, which exited 1, and the original log was not changed. Review-3 documentation checks and `git diff --check` all exited 0; see `evidence/commands.md` and the `*-r3.txt` outputs.

## Scope and safety

No Claude process ran. Claude-required criteria are identified as `deferred-claude`; neither Codex nor mocks are substituted for a PASS. No paid benchmark, live evaluation, actual user approval, evolution apply, knowledge approval, or real eval-data mutation was performed. The working tree edits are documentation only and fit the authorized scope.
