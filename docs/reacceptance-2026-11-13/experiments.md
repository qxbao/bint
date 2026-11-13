# Experimental plans and gates

This inventory separates implementation tests from claims about agent effectiveness. Historical evaluations below were inspected as leads only; none is presented as new evidence. The audit ran no paid benchmark, live model experiment, approval flow, `evolve apply`, or mutation of real eval/knowledge data.

## Frozen gate protocols

| Plan / criterion | Plan and archived result | Current evidence and conclusion |
|---|---|---|
| Gate 1 / P2 | [`gate-1.md` (lines 24)](../gate-1.md) fixes the paired pass-rate noninferiority floor (−5 points) plus ≥10% cost or turns reduction with confidence interval. It requires ≥2 repositories, ≥20 verified test cases each, 3 repeats and reliability guards (`:37-48`). | The result section says the existing pilot is one repository/two validation cases and does not meet the official protocol (`:87-103`). No new runs were made. **tạm hoãn/thiếu bằng chứng** — DEF-001. |
| Gate 2 / P3 | [`gate-2.md` (lines 26)](../gate-2.md) requires paired `bint − minimal` pass-rate CI lower bound >0, cost increase ≤25%, same cross-repository/sample requirements and ≥80% of Bint runs with retrieval (`:42-49`). | The result section says there is no official gate result; historical pilot lacks repository/case/cost criteria (`:89-111`). No new runs were made. **tạm hoãn/thiếu bằng chứng** — DEF-001. |

## P8 follow-up experiments

| Plan stage | Frozen question/acceptance | Current evidence | Conclusion |
|---|---|---|---|
| A-impl.1 | Import human-authored snapshot at a base commit. `p8-next-experiments.md:31`. | Fresh source search found no `eval snapshot import` implementation or `knowledge_origin` field (`evidence/experiment-feature-search.txt`). | **chưa triển khai** — DEF-011. |
| A-impl.2 | Group snapshot import, per-task evidence filtering, and `knowledge_origin: human` reporting. `:32-33`. | Same fresh source search as above; no new snapshot code was found. | **chưa triển khai** — DEF-011. |
| A-prep.1 | Choose a code area with ≥20 test/validation tasks whose bases are close. `:25`. | No external corpus was selected or changed. | **tạm hoãn/thiếu bằng chứng** — DEF-006. |
| A-prep.2 | Write at B0 in a separate worktree without seeing cases/fix commits; require evidence at B0 and validate against task bases. `:26-28`. | No human-authored knowledge experiment was run; real knowledge mutations were prohibited. | **tạm hoãn/thiếu bằng chứng** — DEF-006. |
| A1 | Run `none` on ~20 tasks twice. `:49`. | No fresh runs or historical log reuse. | **tạm hoãn/thiếu bằng chứng** — DEF-006/008; no Claude model is specified for this `none` arm. |
| A2 | Run `bint` with human-authored items on same cases twice; budget ~$8. `:50`. | No experiment or approval. | **tạm hoãn/thiếu bằng chứng** — deferred-claude DEF-003 and DEF-006/008. |
| A3 | Optional compare generated knowledge on same cases; budget ~$8. `:51`. | Not run. | **tạm hoãn/thiếu bằng chứng** — DEF-006/008; the plan does not specify a mandatory Claude model for this generated-knowledge arm. |
| A-gate | Δ pass rate ≥15 points with CI excluding zero; ambiguous results require approved expansion; Δ≤0 is a negative result. `:57-59`. | No sample means no gate decision. | **tạm hoãn/thiếu bằng chứng** — DEF-006/008. |
| B-guard | Validation-only input, exclude each lesson's source task from scoring, and reject answer leakage. `:80-82`. | Deterministic `CheckLesson` tests are covered under E3; no live experiment was run. | **đạt chức năng trên phạm vi đã kiểm** for code guard contracts; experiment remains deferred. |
| B-infra | Digest/scan discovers validation failures; reflector and cheaper executor use separate configured models; fixture E2E excludes test data. `:86-88`. | Source/contracts are covered under E1–E4. No model invocation or data mutation occurred. | **đạt chức năng trên phạm vi đã kiểm** for deterministic plumbing; live use remains deferred. |
| B0 | Measure none/Haiku on 70 validation cases; require baseline 15–60%. `:94-95`. | Not run. | **tạm hoãn/thiếu bằng chứng** — DEF-003/006/008; Claude Haiku arm not run. |
| B1 | Measure Bint/Haiku on same validation cases for failure logs. `:96`. | Not run. | **tạm hoãn/thiếu bằng chứng** — DEF-003/006/008; Claude Haiku arm not run. |
| B2 | Opus reflector plus Haiku executor; at most 3 proposals and validation→test, within $25. `:97`. | No live reflector/evolver calls. | **tạm hoãn/thiếu bằng chứng** — deferred-claude DEF-003 and DEF-006. |
| B3 | If a proposal passes test, repeat test twice to confirm. `:98`. | No proposal passed a new independent test because no experiment was run. | **tạm hoãn/thiếu bằng chứng** — DEF-003/006/008. |
| B-gate | Require independent test success and B3 confirmation; after 3 failed proposals record a negative result. `:104-106`. | No new batch or gate result. | **tạm hoãn/thiếu bằng chứng** — DEF-006/008. |
| Ordering and common constraints | Frozen order, cost/data guardrails and prerequisites `:116-132`. | Followed: no live benchmark, paid inference, approval, or real-data apply. | Constraint compliance observed for this audit; experiment not executed. |

## Evolution experiments

| Plan stage | Acceptance | Current evidence | Conclusion |
|---|---|---|---|
| E6-T | Digest, grouping, leakage and fake-agent E2E requirements (`p8-harness-evolution-plan.md:112-116`). | Deterministic suite evidence is in `evidence/go-test.json`. | Test contract is checked within the limits in [evolution.md](evolution.md). |
| E6-M1 | Run validation batch to collect failures (reuse old results only if logs still exist). `:118-120`. | No batch. | **tạm hoãn/thiếu bằng chứng** — DEF-003/006; live model batch not run. |
| E6-M2 | Reflector extracts up to four groups within ~$2–4. `p8-harness-evolution-plan.md:123-125`. | No inference. | **tạm hoãn/thiếu bằng chứng** — deferred-claude DEF-003 and DEF-006. |
| E6-M3 | Cycle evaluates up to three lessons on validation then independent test within $20, total cap $30. `:123-134`. | No paid/live evaluation. | **tạm hoãn/thiếu bằng chứng** — DEF-003/006/008. |
| E6-gate | At least one lesson passes test, or after three proposals record a negative result. `:128-132`. | No new proposal batch. | **tạm hoãn/thiếu bằng chứng** — DEF-006/008. |
| H5-M1 | Compare Bint with/without author reminder guardrail on 30 tasks (~$13). `:289-298`. | No live eval. | **tạm hoãn/thiếu bằng chứng** — DEF-003/006; Claude-backed comparison was not run. |
| H5-M2 | Run one process-evolution cycle, up to two opportunities × two candidates, within $25. `p8-harness-evolution-plan.md:294-298`. | No live proposal/cycle. | **tạm hoãn/thiếu bằng chứng** — deferred-claude DEF-003 and DEF-006. |
| H5-gate | If baseline agents already run tests on nearly all cases, switch to another signal before step 2. `:299-302`. | No baseline measurement exists. | **tạm hoãn/thiếu bằng chứng** — DEF-006. |

## MCP measurement

| Plan stage | Acceptance | Current evidence | Conclusion |
|---|---|---|---|
| M5 | Measure quality and operational savings before defaulting service grants; follows M3 (`mcp-gateway-plan.md:341-342,354-358`). | M0–M4 gateway implementation was not found at HEAD, so no planned gateway effect can be measured. No PSS/quality benchmark was run. | **tạm hoãn/thiếu bằng chứng**, dependent on M3 implementation; DEF-006/007. |

## Evaluation plan inventory

| Source | Classification | Reacceptance use |
|---|---|---|
| [`docs/evals/codex-flows-2026-11-10/PLAN.md` (lines 1)](../evals/codex-flows-2026-11-10/PLAN.md) | Historical execution plan | Inventory/search lead only; no rerun. |
| [`docs/evals/codex-flows-2026-11-11/PLAN.md` (lines 1)](../evals/codex-flows-2026-11-11/PLAN.md) | Historical execution plan | Inventory/search lead only; no rerun. |
| [`docs/evals/claude-codex-mixed-2026-11-11/evidence/tasks/01M74AEVHRH7M0HEK0G2396WE1/plan.md` (lines 1)](../evals/claude-codex-mixed-2026-11-11/evidence/tasks/01M74AEVHRH7M0HEK0G2396WE1/plan.md) | Embedded task plan | Small implementation task; historical only. |
| [`01M74AT2NVMZBMRB69MH92H6TH/plan.md` (lines 1)](../evals/claude-codex-mixed-2026-11-11/evidence/tasks/01M74AT2NVMZBMRB69MH92H6TH/plan.md) | Embedded task plan | Small implementation task; historical only. |
| [`01M74B6HCGKJC5DJNMD0Y39X76/plan.md` (lines 1)](../evals/claude-codex-mixed-2026-11-11/evidence/tasks/01M74B6HCGKJC5DJNMD0Y39X76/plan.md) | Embedded task plan | Small implementation task; historical only. |
| `docs/evals/claude-codex-mixed-2026-11-11/evidence/tasks/01M74BFD9EM8XEXRVQRW4EQ3XZ/` | Embedded task evidence with `review.md`, but no `plan.md` in inventory | Recorded as a missing plan artifact, not treated as evidence of acceptance. |
| `docs/evals/pilot-codex-2026-11-10/README.md`, `docs/evals/p6-p7-claude-2026-11-10/README.md`, `docs/evals/codex-flow-acceptance-2026-11-12/**` | Historical reports/evidence; not plan files | Read as context only. Their model outputs and pass counts are not reacceptance evidence at this HEAD. |

## PLAN criteria reacceptance

The two Codex `PLAN.md` files and embedded task plans are plans in the inventory, so their checklist items are shown individually here. Their checked boxes record what those historical runs intended or reported; they do not establish this audit's acceptance. Current source/test evidence is limited to `T-TEST`, `T-VET-LINUX`, `T-VET-WINDOWS`, and `T-RACE` in `evidence/commands.md`. Live flows, model results, and old logs were not rerun or promoted to new evidence. The embedded plans describe separate toy repositories; no corresponding `calc.go`/`calc_test.go` artifact is present in this repository, so their code/test criteria cannot be checked against current source and are individually deferred as DEF-016.

| Plan / criterion | Plan line | Current source/test and new evidence | Conclusion / deferred |
|---|---|---|---|
| Codex flow 2026-11-10 F1: init/local/project lifecycle, idempotence, doctor-before-trust | `evals/codex-flows-2026-11-10/PLAN.md:6` | Existing harness/init tests are included in `T-TEST`; no fresh Codex CLI profile runtime was run. | **tạm hoãn/thiếu bằng chứng** for the plan's runtime criterion — DEF-008. |
| F2: real Codex JSONL tools, shell/patch/MCP and deny behavior | `:7` | Adapter contract tests are in `T-TEST`; historical runtime artifacts are not new evidence. | **tạm hoãn/thiếu bằng chứng** for real provider behavior — DEF-008. |
| F3: citation policy and CLI confirmation | `:9` | Current code/tests are represented by `internal/eval/agent_harness_test.go` in `T-TEST`; no model session. | **tạm hoãn/thiếu bằng chứng** for fresh citation runtime — DEF-008. |
| F4/F5: b-do real role flow, protected paths, approve/resume | `:11` | Workflow/CLI fixture tests ran in `T-TEST`; no live model or human approval was performed. | Fixture contracts only; runtime acceptance **tạm hoãn/thiếu bằng chứng** — DEF-008. |
| F6: strict none/minimal/bint evaluation arms and citation | `:12` | No new eval arm was run; audit did not invoke a model. | **tạm hoãn/thiếu bằng chứng** — DEF-006/008. |
| F7: AGENTS.md context size and skill context | `:13` | No plan-specific fresh context measurement was retained. | **tạm hoãn/thiếu bằng chứng** — DEF-008. |
| Codex flow 2026-11-11 F1: real hooks/list/config state and isolated user profile | `evals/codex-flows-2026-11-11/PLAN.md:5` | Unit/CLI tests in `T-TEST`; no fresh Codex profile runtime. | **tạm hoãn/thiếu bằng chứng** — DEF-008. |
| F2: malformed/fail-open hook payloads and timeout | `:6` | Deterministic hook tests are included in `T-TEST`; no provider event delivery was observed. | Function contract checked; live integration **tạm hoãn/thiếu bằng chứng** — DEF-008. |
| F3/F4: skill discovery, explicit/implicit invocation, MCP and events | `:7` | No new Codex session or event capture. | **tạm hoãn/thiếu bằng chứng** — DEF-008. |
| F5: readonly violation, timeout, interrupt/resume, mixed role execution | `:8` | Workflow fixtures ran; mixed Claude/Codex runtime was not run. | Fixture contracts only; mixed live flow **tạm hoãn/thiếu bằng chứng** — deferred-claude DEF-003 and DEF-008. |
| F6: isolated profiles and strict evaluation modes | `:9` | No new evaluation run. | **tạm hoãn/thiếu bằng chứng** — DEF-006/008. |
| Embedded `01M74AEVHRH7M0HEK0G2396WE1`: `Abs(int)` uses the specified integer branch without `math.Abs` | `docs/evals/claude-codex-mixed-2026-11-11/evidence/tasks/01M74AEVHRH7M0HEK0G2396WE1/plan.md:4-7,10` | No target `calc.go` is present in this checkout; exact artifact search is recorded in `evidence/embedded-calc-search.txt`. | **tạm hoãn/thiếu bằng chứng** — DEF-016. |
| Embedded `01M74AEVHRH7M0HEK0G2396WE1`: zero, negative, positive test cases | `docs/evals/claude-codex-mixed-2026-11-11/evidence/tasks/01M74AEVHRH7M0HEK0G2396WE1/plan.md:11-12,20-21` | No target `calc_test.go` or fresh test log is present. | **tạm hoãn/thiếu bằng chứng** — DEF-016. |
| Embedded `01M74AEVHRH7M0HEK0G2396WE1`: append-only changes; gofmt and `go test ./...` | `docs/evals/claude-codex-mixed-2026-11-11/evidence/tasks/01M74AEVHRH7M0HEK0G2396WE1/plan.md:17-22` | Separate task diff/repository and command log are absent from this checkout. | **tạm hoãn/thiếu bằng chứng** — DEF-016. |
| Embedded `01M74AT2NVMZBMRB69MH92H6TH`: independent `Abs(int)` branch implementation | `docs/evals/claude-codex-mixed-2026-11-11/evidence/tasks/01M74AT2NVMZBMRB69MH92H6TH/plan.md:4-6,9` | No target `calc.go` is present in this checkout; exact artifact search is recorded in `evidence/embedded-calc-search.txt`. | **tạm hoãn/thiếu bằng chứng** — DEF-016. |
| Embedded `01M74AT2NVMZBMRB69MH92H6TH`: zero, negative, positive tests; preserve Add/Negate and existing tests | `docs/evals/claude-codex-mixed-2026-11-11/evidence/tasks/01M74AT2NVMZBMRB69MH92H6TH/plan.md:10-11,19-20` | No target `calc_test.go` or fresh test log is present. | **tạm hoãn/thiếu bằng chứng** — DEF-016. |
| Embedded `01M74AT2NVMZBMRB69MH92H6TH`: append-only diff, gofmt, and `go test ./...` | `docs/evals/claude-codex-mixed-2026-11-11/evidence/tasks/01M74AT2NVMZBMRB69MH92H6TH/plan.md:16-21` | Separate task diff/repository and command log are absent from this checkout. | **tạm hoãn/thiếu bằng chứng** — DEF-016. |
| Embedded `01M74B6HCGKJC5DJNMD0Y39X76`: add symmetric `Max(int,int)` and keep existing functions | `docs/evals/claude-codex-mixed-2026-11-11/evidence/tasks/01M74B6HCGKJC5DJNMD0Y39X76/plan.md:3,6` | No target `calc.go` is present in this checkout; exact artifact search is recorded in `evidence/embedded-calc-search.txt`. | **tạm hoãn/thiếu bằng chứng** — DEF-016. |
| Embedded `01M74B6HCGKJC5DJNMD0Y39X76`: test (2,9), (9,2), and (4,4); preserve existing Abs | `docs/evals/claude-codex-mixed-2026-11-11/evidence/tasks/01M74B6HCGKJC5DJNMD0Y39X76/plan.md:7,10` | No target `calc_test.go` or fresh test log is present. | **tạm hoãn/thiếu bằng chứng** — DEF-016. |
| Embedded `01M74B6HCGKJC5DJNMD0Y39X76`: run `go test ./...` | `docs/evals/claude-codex-mixed-2026-11-11/evidence/tasks/01M74B6HCGKJC5DJNMD0Y39X76/plan.md:12-13` | Separate task repository and command log are absent from this checkout. | **tạm hoãn/thiếu bằng chứng** — DEF-016. |
| Mixed Claude/Codex historical task plans and reports | `evals/claude-codex-mixed-2026-11-11/README.md:1-20`; plan rows above | No Claude or Codex replacement run in this audit; no simulated event is counted as runtime acceptance. | Every Claude-required flow is **tạm hoãn/thiếu bằng chứng** — deferred-claude DEF-003. |

This section's `DEF-008` rows are intentionally not marked PASS from source tests alone. Every requirement for a live Claude harness, Claude-generated event, or Claude/Codex mixed session uses the single `deferred-claude` registry entry DEF-003, including the mixed-evaluation plan above. The recheck command and required raw artifacts are specified in `deferred.md#def-003`.

The full Markdown document inventory is in [`evidence/inventory-search.txt`](evidence/inventory-search.txt); canonical plans, historical acceptance reports and eval artifacts are distinguished there and in the linked group reports. The only implementation verification newly run here is the deterministic Go suite and static analysis listed in [README](README.md#evidence-and-command-logs).
