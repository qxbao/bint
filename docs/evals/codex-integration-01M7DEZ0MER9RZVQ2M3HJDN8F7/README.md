# Codex integration — implementation evidence

Task `01M7DEZ0MER9RZVQ2M3HJDN8F7`, new runs on 2026-11-14 UTC and
Asia/Ho_Chi_Minh. Base `25c26000b9b1fc48b2715f64fcb613d780f17a1f`, directly after
`fc27998` merged main into duc. This is an uncommitted implementation handoff.
Core owns independent b-review, confirmed `go test ./...`, task state and land;
the orchestrator owns commit/merge/push/PR after the workflow gates pass.

Environment: macOS 26.6.2 arm64, Apple M4, Go 1.25.1, Codex CLI 0.169.0,
staticcheck 2026.1 (v0.7.0). See [environment.json](evidence/environment.json).
No model inference, nested workflow or external agent was invoked. No Astra
model was selected. Fixture model names are strings used by fake subprocesses.
Personal configuration and the old draft worktree/artifacts were only read.

## Review round 2 correction — current handoff

Finding `01M7DEZ0MER9RZVQ2M3HJDN8F7.3` reproduced on both linked
separate-git-dir and linked submodule repositories. `MainWorktree()` cannot
infer a main root from their CommonDir, which does not end in `.git`. Bint
therefore checked each layer against an empty inherited root and discovered
only the explicitly trusted worktree root. Codex CLI 0.169.0 instead uses the
current toplevel as its trust root in this case.

`repoRoots` now falls back to the current toplevel whenever no main root can
be inferred. Existing ordinary linked-worktree inheritance remains covered;
each layer still honors explicit untrusted entries, and configs outside the
current worktree are not loaded. The production change is one condition plus
comments in `internal/codexconfig/config.go`. The two existing MCP test files
now include linked Git-file fixtures; no other internal file changed since
the round 1 handoff. Parser source and its recorded measurements are unchanged.

New red evidence: [unit](evidence/review2-unit-red.txt) and
[CLI](evidence/review2-cli-red.txt). The CLI run confirms the actual failure:
`enabled=[bint child_only deep_only]`, or `[bint deep_only]` with the child
explicitly untrusted. After normalizing the new unit fixture's macOS path,
an [overlay red run](evidence/review2-unit-overlay-red.txt) again fails only
the two linked root-trust cases. Its [patch](evidence/review2-red-overlay.patch)
removes only the fallback from the green source; the neighboring JSON records
the overlaid product hash separately from the on-disk fingerprint.

Current fingerprint is
`7d220dbb92e357e9891550e809e9bf264f199c7e51ed71cf312de462f840b97b`.
[review2-source-manifest.json](evidence/review2-source-manifest.json) defines
the fingerprint as a hash of the per-file hash map and identifies all three
files changed since round 1. This fingerprint scheme differs from the earlier
diff-based fingerprints. Commands, environment overrides, timestamps and exits
are retained in neighboring JSON files; versions were captured again in
[review2-environment.json](evidence/review2-environment.json).

| Check on the current fingerprint | Result |
|---|---|
| MCP/trust regressions, including env precedence, override order and normal linked inheritance | PASS, [review2-unit-green](evidence/review2-unit-green.txt) |
| Installed CLI config resolution and ExclusiveMCP | PASS, [review2-cli-final](evidence/review2-cli-final.txt): 25 cases across regular, separate-git-dir, submodule and both linked variants; only `bint` remains enabled in each case |
| Related eval/Codex trust/MCP tests with `-race` | PASS, [review2-race](evidence/review2-race.txt) |
| Host and GOOS=windows `go vet ./...` | PASS, [host](evidence/review2-vet-host.txt), [Windows](evidence/review2-vet-windows.txt) |
| Host and GOOS=windows installed `staticcheck ./...` | PASS, [host](evidence/review2-staticcheck-host.txt), [Windows](evidence/review2-staticcheck-windows.txt) |
| `go build ./cmd/bint` | PASS, [review2-build](evidence/review2-build.txt) |
| `git diff --check` | PASS, [review2-diff-check](evidence/review2-diff-check.txt) |

Binary `/tmp/bint-codex-01M7DEZ-review2` SHA-256:
`6c0526f898347839425ae536cec057053a26a8c2bf34c53ec4d6f26194c3bbca`.
The CLI checks resolve isolated fixture configuration; they do not start a
model or MCP service and do not establish tools/list behavior. Windows checks
are static analysis. No sandbox, trust or approval gate was relaxed.

The first unit attempt used the sandbox-restricted default Go cache
([setup log](evidence/review2-cache-setup.txt)); subsequent runs explicitly use
the writable temporary cache. A subsequent unit failure exposed a fixture
path mismatch (`/var` versus `/private/var`) for explicit child-denial keys
([log](evidence/review2-unit-fixture-path-failure.txt)); the fixture now resolves
its root before forming keys. Both limitations/corrections were recorded with
`bint task error`; no further production change was needed.

AC-1: correction and audit evidence supplied. AC-2: current targeted checks and
all four analyzers pass, but Core must rerun the confirmed full suite after
this production change. Core's successful verify-2 predates it. AC-3: the
author supplies a correction for the high finding; independent closure and
review of this final diff remain Core's next step. No Git delivery performed.
The sections below retain earlier snapshots and their results as history.

## Review round 1 corrections

The sections below this revision record describe the initial implementation
snapshot `7b45127d…`. The round 1 code fingerprint was
`18e7e51840f62e7534a7e4c40247b7bb166005fc59f39f04f16650920ad32b04`;
[review1-source-manifest.json](evidence/review1-source-manifest.json) records its
file hashes. Parser code and its five-sample measurements are unchanged.

| Finding | Correction and new evidence |
|---|---|
| `01M7DEZ0MER9RZVQ2M3HJDN8F7.1` high: child MCP config missed for separate-git-dir/submodule | `internal/codexconfig/config.go` now uses the discovered toplevel as the trust root when GitDir equals CommonDir. Those repositories have no linked-worktree indirection, and their CommonDir need not end in `.git`. Linked-worktree handling retains its existing main-root behavior. New self-contained Git fixtures fail before the fix with `[root_only]` instead of `[child_only root_only]`: [review1-red](evidence/review1-red.txt). Explicit child denial and unrelated parent trust remain covered. |
| `01M7DEZ0MER9RZVQ2M3HJDN8F7.2` medium: stale matcher assertion fails full verification | `internal/app/multiharness_test.go` parses installed hooks and checks that PreToolUse and PostToolUse match all five supported tool names, including local_shell. Init installation and second-init hash equality checks remain. The old assertion fails in [review1-red](evidence/review1-red.txt); the corrected integration test passes in [review1-green](evidence/review1-green.txt). No production matcher or verification gate was weakened. |

The real CLI comparison also reproduced the discovery mismatch before the fix:
[review1-cli-red](evidence/review1-cli-red.txt). After the fix,
[review1-cli-green](evidence/review1-cli-green.txt) passes 15 cases: regular,
separate-git-dir and submodule, each with root trust, child-only trust, explicit
child denial, no trust, and trust of an outside ancestor. Each case compares
CLI-resolved names to Bint discovery and then checks that only Bint remains
enabled after applying generated disable arguments. This uses installed
Codex CLI 0.169.0 and isolated config fixtures, without a model request or MCP
service. It establishes config resolution, not tools/list or inference.

All checks below ran on the round 1 fingerprint; `.json` records retain full
commands, overrides, exits and timestamps:

| Check | Result |
|---|---|
| Focused eval/app/Codex regressions, including both findings | PASS, [review1-green](evidence/review1-green.txt) |
| Related eval/app/Codex tests with `-race` | PASS, [review1-race](evidence/review1-race.txt) |
| Host and GOOS=windows `go vet ./...` | PASS, [host](evidence/review1-vet-host.txt), [Windows](evidence/review1-vet-windows.txt) |
| Host and GOOS=windows installed `staticcheck ./...` | PASS, [host](evidence/review1-staticcheck-host.txt), [Windows](evidence/review1-staticcheck-windows.txt) |
| `go build ./cmd/bint` | PASS, [review1-build](evidence/review1-build.txt) |
| `git diff --check` | PASS, [review1-diff-check](evidence/review1-diff-check.txt) |

Round 1 binary `/tmp/bint-codex-01M7DEZ-review1` SHA-256:
`c65d78ab35c5d38f7059a1dca5aa110b882239aba6a4f600df40c168cdd8abf5`.
Windows results remain static analysis, not Windows runtime execution.

Core's earlier `logs/verify-1.log` ran the full suite outside this agent's
socket restriction and failed specifically at `TestMultiHarnessInit`; all
other packages, including eval, mcpgw and workflow, passed that earlier run.
That result predates these corrections and does not verify the current diff.
The selected passing checks here do not replace a successful full-suite run.
Core must rerun its confirmed `go test ./...` and send this revision to the
independent reviewer. The author supplies fixes for both findings but does not
close the independent review on the reviewer's behalf.

Round 1 self-assessment: AC-1 met with audit and correction evidence; AC-2
unmet pending Core's full verification; AC-3 unmet pending independent review
of the final diff and successful full verification. No Git delivery performed.
The Git-directory pitfall was submitted through `bint knowledge add` as
candidate `01M7DGMGC810QA0C2G90312RCS`. That CLI generated a file outside task
scope, so this agent moved only its newly generated candidate to
[review1-knowledge-candidate.md](evidence/review1-knowledge-candidate.md) under
the allowed docs tree. It is retained for later human import/review, not active
project knowledge; no `.bint` edit is included in the handoff.

## Draft adoption and preservation of main

The old draft task `01M7DD79YVSWW7QJ5TSYDSTRMY` used base `c7311ec`. Its report
was read as historical context; none of its old test outputs is acceptance
evidence for this tree. New logs below include UTC start, command, overrides,
exit code, elapsed time and a fingerprint of the internal diff, including new
source files. [source-manifest.json](evidence/source-manifest.json) records the
draft/final hashes and checks that the relevant main production files remain
unchanged.

| Draft file/hunk | Decision and reason |
|---|---|
| `internal/eval/agent_harness.go` | Keep parser and process-error priority hunks; these reproduce on the new base. No main changes in this file were replaced. |
| `internal/eval/agent_harness_test.go` | Keep terminal/failure/large-item regressions and benchmark, run on base before product patch. |
| `internal/codexconfig/config.go` (new) | Keep shared TOML reader and bounded Git root discovery; existing dependency and gitrepo helper suffice. |
| `internal/eval/codex_mcp.go` | Keep per-project-layer discovery through the common trust helper. Inline override generation/order remains unchanged. |
| `internal/eval/codex_mcp_test.go` | Keep physical-path fixture normalization and project-layer cases. Existing env/dotted-name/worktree cases remain. |
| `internal/eval/codex_mcp_live_test.go` (new) | Keep opt-in CLI configuration check; executed afresh against 0.169.0, without starting a model/service. |
| `internal/harness/codex/codex.go` | Keep TOML trust, cmd fallback and local_shell Pre/Post dispatch hunks. |
| `internal/harness/codex/codex_test.go` | Keep installed matcher regression; add direct command/cmd precedence and string/argv coverage, observed red then green. |
| `internal/harness/codex/trust_test.go` | Keep opt-in hooks/list metadata check; executed afresh. No hook approval. |
| `internal/harness/codex/project_trust_test.go` (new) | Keep TOML syntax, false-positive trust, worktree inheritance and explicit-denial tests. |
| `internal/harness/codex/outcome_test.go` (new) | Keep ID/exit/large-transcript evidence checks; no outcome is invented when evidence is missing. |
| `internal/app/guardrail_hook_test.go` | Keep exec_command/cmd and local_shell/argv inputs; require four durable child-run deny events. |
| `internal/eval/codex_profile_test.go` | Adjust: manually merge successful fixture terminal and error/cancel cleanup test. Keep main's complete alias/cycle/relative-target tests. Initial apply-check rejected the old end-of-file hunk before mutation; recorded with task error. |
| Old docs/evidence | Do not import as new evidence. This directory contains new measurements/results only. |

Main's `codex_profile.go`, pathutil/state, hookcore path handling, VerifyEnv,
workflow run/ops/worktree/land and finalize/resume remain intact. The source
manifest lists the exact unchanged files. No new syscall, dependency,
configuration key or protected-path edit was needed.

## Reproduced defects

[baseline-regressions.txt](evidence/baseline-regressions.txt) is the new red run
with test changes but original product code. [baseline-tests.patch](evidence/baseline-tests.patch)
and [baseline-product-sha256.txt](evidence/baseline-product-sha256.txt) identify
that setup; the new test files are also included in the runner fingerprint.

| Trigger | Baseline | Fixed behavior and new evidence |
|---|---|---|
| Empty stream, answer without terminal, truncated/malformed JSON, second turn interrupted, detail-free failure/error | Reported success; a secondary JSONL error could also overwrite cancellation | Require final started turn completion; reject malformed event JSON and terminal failures; preserve process errors. Seven invalid-stream cases plus error-priority checks go red→green in `TestCodexJSONLRequiresCompletedTurn`. [parser-green](evidence/parser-green.txt). |
| Tool-result line above 64 MiB followed by failure | Scanner stopped silently and retained an earlier completed turn | Iterate slices of buffered stdout without the Scanner ceiling/copy. `TestCodexJSONLReadsFailureAfterLargeItem` goes red→green and sees the late failure. |
| Single-quoted/inline trust TOML, indented unrelated table, invalid trusted suffix | Valid trust missed; unrelated/invalid trust accepted | Parse TOML through common helper. `TestProjectTrustParsesTOML` goes red→green on all four formerly wrong cases. |
| Linked worktree with trusted main repo | Harness reported untrusted | Use Git roots, honor explicit untrusted before inherited main trust. Worktree regression goes red→green. |
| Cwd below repo root, with MCP servers in root/child config | Exclusive MCP saw only cwd config | Enumerate each independently trusted layer to the current worktree root. Root/child/explicitly-denied-child cases go red→green; unrelated parent does not confer trust. |
| exec_command uses cmd; local_shell dispatched through installed hooks | Empty command bypassed command guardrail; local_shell missed Pre/Post matcher | command takes priority, cmd falls back; local_shell included in both matchers. Four durable denial events verified. [trust-hooks-green](evidence/trust-hooks-green.txt), [shell precedence red](evidence/shell-precedence-red.txt). |

Warnings represented by nonterminal item errors and the historical 0.167
warning/citation fixture remain valid. Cost and turn counts remain unknown
when not reported; usage is accumulated only from completed turns. The process
exit/cancellation diagnostics retain priority over parser diagnostics.

## Five-area audit and evidence levels

**Fixture** means controlled input or a fake subprocess exercising Bint code.
**CLI metadata/config** means installed CLI 0.169.0 processing isolated fixture
configuration, with no model request. **Endpoint fixture** would mean actual
CLI request construction captured at a local Responses endpoint. **Inference**
would mean a real provider/model turn. **Historical** captures are regression
inputs only. There is no successful new endpoint-fixture or inference evidence.

| Area and call path | Checked behavior | Limits |
|---|---|---|
| Harness: HarnessAgentsWithChoices → CodexAgent.ArgsFor/Run; InstallHooks/ProjectTrusted | Saved empty model and explicit model remain stable; workspace-write arguments and no automatic hook-trust bypass; shared TOML trust. Parser/model/args fixtures and new CLI config check. | CLI config processing does not establish live model behavior. Codex read-only process steps use workspace-write plus Bint's post-step snapshot enforcement. |
| Hooks: codex.HandleHook → hookcore.Handle; Classify/desiredGroups/postToolOutcome | String/argv shell, command priority/cmd fallback, four installed shell aliases, patch paths, durable child-run denials, ID/exit matching and large transcript tail. Main symlink name/target handling retained. | cmd/local_shell payloads are adapter fixtures. hooks/list confirms metadata only; no hook was approved or exercised during inference. Matcher changes may require ordinary re-trust after sync. |
| Workflow: runStep/endStep → stepMCP; VerifyWith/Next/finalize | Parser Error reaches the failure branch (`run.go`); saved empty models, VerifyEnv, finalize fingerprint/resume, read-only snapshots, run/task IDs in MCP argv, binding cleanup and role selection. | Process/session tests use fixtures, not a nested real bint run or human approval. Broad result and focused/race evidence below delimit coverage. |
| Eval/profile: CodexAgent.Run → codexEvalProfile; CodexRestrictedAgent.Run → probe/runRestrictedThread; bounded readers | Success/error/cancel/timeout profile cleanup and unchanged source auth; all main skill aliases/cycles/relative link rules retained; readers reject traversal/symlink/FIFO/state paths and check opened handles. Restricted catalog keeps selected model/refuses fallback; surface checks cover tools and input.additional_tools. | Restricted RPC success/unknown-tool/unsafe-surface/timeout fixture cannot bind localhost here and does not reach its probe. Keep this unverified; no probe was removed. |
| MCP/gateway: codexDisableMCPArgs → ArgsFor; stepMCP/WriteRunBinding → NewMCPShim/ResolveIdentity | Env precedence, user/project trust layers, explicit denial, linked worktree, dotted/spaced names, inline-table-before-bint order, nonexistent untrusted project names; binding round-trip/traversal and role filtering fixtures. | `codex mcp list` describes configured servers, not service tools/list or gateway authorization runtime. Socket-dependent gateway tests need Core's verification environment. |

Applied knowledge: hook nil-exit/error handling with intentional deny preserved
[k:01M746NPTW1B304MRQQPBCAFAM]; Windows analysis [k:01M746NASVKV9XMRHA4QNHJ3R8];
MCP override order [k:01M77JFNA33PQ99TVS9A386HFG]; real restricted tool-surface
probe [k:01M77BS1E11BFC8C6JSS20Z5VF]; reader handle identity
[k:01M77FRYBCKRMTENPFE1XRFHVW]; race fixture startup budget
[k:01M78968T95MDQSK472NC5XQT9]. Citations were recorded through MCP.

## Parser measurements on the new base

Both sides use Go 1.25.1/darwin-arm64, Apple M4, the same 728-byte codexSample,
and the same temporary Go cache. Command, five samples each:

```sh
GOCACHE=/tmp/bint-codex-01M7DEZ-cache go test ./internal/eval \
  -run '^$' -bench '^BenchmarkParseCodexJSONL$' -benchmem -count=5
```

| Metric | Before | After |
|---|---|---|
| ns/op samples | 93172, 89673, 84021, 89186, 89008 | 5174, 4799, 4741, 4696, 4722 |
| ns/op median (range) | 89186 (84021–93172) | 4741 (4696–5174) |
| B/op median (range) | 1051224 (1051223–1051224) | 2592 (2592–2592) |
| allocs/op median (range) | 67 (67–67) | 65 (65–65) |

Median parsing time is 18.8× lower, bytes allocated decrease 99.75%. Raw outputs:
[before](evidence/baseline-benchmark.txt), [after](evidence/final-benchmark.txt),
[summary](evidence/benchmark-summary.json). Baseline ran before the parser patch;
its test-only fingerprint is `802ca50b…`; final source fingerprint is `7b45127d…`.
The baseline invocation shared a short build/regression window; results are a
local microbenchmark, not a controlled end-to-end latency study. No token,
cost or model-latency reduction is claimed. Run still buffers all stdout; this
change does not bound peak memory for an entire large session. There is no
global trust/catalog cache and no skipped safety check.

## Verification and acceptance

Each `.txt` log has a neighboring `.json` record for command/exit/fingerprint.
Both analyzer caches use temporary writable directories, since the sandbox
restricts the user's normal caches. Windows analysis uses the host-installed
staticcheck binary with GOOS=windows; it is not Windows runtime testing.

| Check | Result and log |
|---|---|
| Parser/profile/args narrow regression | PASS, [parser-green](evidence/parser-green.txt) |
| Trust/MCP/hook narrow regression | PASS, [trust-hooks-green](evidence/trust-hooks-green.txt) |
| Actual CLI config and hooks metadata | PASS, [local-cli](evidence/local-cli.txt), opt-in BINT_CODEX_CONFIG_LIVE=1 |
| Host go vet ./... | PASS, [vet-host](evidence/vet-host.txt) |
| GOOS=windows go vet ./... | PASS, [vet-windows](evidence/vet-windows.txt) |
| Host staticcheck ./... | PASS, [staticcheck-host](evidence/staticcheck-host.txt) |
| GOOS=windows staticcheck ./... | PASS, [staticcheck-windows](evidence/staticcheck-windows.txt) |
| Build ./cmd/bint | PASS, [build](evidence/build.txt) |
| Broader eight-package tests | FAIL (exit 1), [broad](evidence/broad.txt). Full harness/codex, hookcore, pathutil, state and workflow pass; eval/app/mcpgw fail at socket-dependent fixtures. |
| Race, including restricted RPC | See [race](evidence/race.txt); fails on localhost bind before RPC fixture starts. Other selected packages pass; state selector matched no tests. |
| Focused eval/workflow/app/MCP interactions | PASS, [focused](evidence/focused.txt); includes VerifyEnv, process/session finalize-resume and main profile aliases. |
| Local eval race without socket-dependent RPC | PASS, [race-eval-local](evidence/race-eval-local.txt); parser, trust/MCP, reader races, profile cleanup, catalog and tool-surface fixtures. This selection does not replace the failed RPC-inclusive check. |
| git diff --check | PASS, [diff-check](evidence/diff-check.txt) |
| Core confirmed go test ./... | Pending Core; not invoked through task verify/status/approve by this agent. |

Built binary `/tmp/bint-codex-01M7DEZ` SHA-256:
`b77628171df4dc3986610165d06d52e0dec45d6ef2eb1dbe5ca9e4cf258d7cc1`.
This hash identifies the implementation build, not a release.

The broad command completed in 213.679 seconds (workflow package 206.366s).
Its failures are restricted RPC's four subcases;
`app.TestMCPGatewayLogWithDaemon` and `TestMCPGatewayLogOldDaemon404`;
gateway daemon/auth/env/grants/groups fixtures and
`mcpgw.TestHTTPUpstreamSharedHandshake`. Errors show either IPv4
`listen tcp 127.0.0.1:0: bind: operation not permitted` or the IPv6 equivalent.
The app/gateway httptest listener panics stop those packages, so tests after
the panic are not claimed as run. The race-inclusive command also fails before
the restricted RPC fixture starts. No test was skipped or weakened to turn
these failures green. Both broader and selected results are retained, and the
restriction was recorded with `bint task error`.

Remaining limits: no inference, no Windows runtime, no service tools/list or
successful Responses-endpoint probe in this sandbox. The shared trust resolver
covers fixture-verified user/project layers, not enterprise managed config,
arbitrary injected CLI overrides/profiles or future discovery semantics. When
Git discovery fails it falls back to explicit cwd only and cannot enumerate
ancestor configs; restricted safety checks remain in place. No personal trust
or authentication was changed. Socket failures require a new successful Core
verification result; historical or selected passing tests do not replace it.

- AC-1: implementation/audit evidence supplied across all five areas.
- AC-2: regressions, measurements, build and host/Windows analysis supplied;
  full-suite acceptance remains unmet pending Core's successful verification.
- AC-3: pending Core's independent b-review and final verification. The author
  does not assert that independent review is complete or no critical/high
  findings remain.

Rollback may revert the parser, trust/MCP or hook hunks independently with
their corresponding tests. Profile tests must retain main's alias coverage.
No migration/config schema rollback is required. Git delivery remains with the
orchestrator after these gates pass.
