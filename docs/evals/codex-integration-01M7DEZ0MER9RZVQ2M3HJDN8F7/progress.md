# Implementation ledger — task 01M7DEZ0MER9RZVQ2M3HJDN8F7

Plan: runtime/tasks/01M7DEZ0MER9RZVQ2M3HJDN8F7/plan.md (Core owned).
Base: 25c26000b9b1fc48b2715f64fcb613d780f17a1f, after fc27998 merge main. Old base c7311ec.
TSK-1: old draft read only; tests applied as hunks with git apply --check, profile fixture+cleanup manually merged to preserve all main alias tests. Initial profile hunk rejection recorded via task error; no partial mutation.
Ruling: use this allowed docs directory for ledger/evidence instead of .superpowers scratch; task scope restricts edits to internal/docs; Core owns review/Git delivery. No commits or state file edits.
Pending: baseline benchmark, red regression, product patches, final checks and report. TSK-9 belongs to Core b-review.

TSK-2: baseline-regressions.txt red in all seven invalid streams, process-error priority and >64 MiB late failure; parser-green.txt exit 0 after patch. No scanner ceiling, stdout remains buffered. Baseline benchmark five runs saved before product patch.
TSK-3/4: baseline red confirms TOML literal/inline/false-positive trust, worktree inheritance, MCP root/child layers, cmd and local_shell. trust-hooks-green.txt exit 0. Extra shell-precedence-red.txt proves cmd string/argv and empty command fallback; command wins. Final green covers these cases.
TSK-5: profile fixture terminal + cleanup tests manually merged, all main alias tests retained. Parser/profile selector passes. Other main interaction/race checks pending.
TSK-8 partial: local-cli.txt exit 0 on installed CLI 0.169.0, config resolution and hooks metadata only. No endpoint, model inference, service tools/list or hook approval.

TSK-5: focused.txt exit 0 includes VerifyEnv, saved empty model, process/session finalize-resume, binding/role; race.txt passes workflow/hook/path/app/binding selections but blocked at restricted RPC. Full workflow/harness/codex/hookcore/pathutil/state pass in broad.txt. Main production files in source-manifest.json unchanged.
TSK-6: baseline median 89186 ns/op, range 84021–93172; final median 4741, range 4696–5174; 1051224→2592 median B/op, 67→65 allocations. Same 728-byte fixture/Go/host/cache, count=5 both. Raw samples saved; no runtime model/cost claim.
TSK-7: host+Windows vet/staticcheck and build exit 0, diff-check exit 0. broad.txt exit 1: eval RPC and app/gateway localhost bind restrictions (httptest panics stop remaining app/gateway tests); workflow full suite passes. race.txt exit 1 only at RPC bind; race-eval-local.txt exit 0 explicitly excludes socket-dependent RPC, no test implementation changed. Core go test ./... remains pending, AC-2 unmet.
TSK-8: report README.md complete with adoption inventory, five-area call-path matrix, measurement ranges, metadata/config versus fixture/inference limits, code/build fingerprints and command records.
TSK-9: pending Core-owned independent b-review; no review-complete assertion, no Git delivery by implementer. AC-3 unmet.
Ruling: follow the accepted Bint plan's Core-owned review/verification and separate delivery boundaries; do not start an additional reviewer or nested workflow from b-feat. Cost if missed: independent review and full-suite gate remain required before delivery.

Criteria recorded through CLI: AC-1 met with evidence; AC-2/AC-3 unmet pending Core. No task status/approve/verify calls. Final code hashes/gofmt/diff whitespace/report links checked; no source changes since analysis/test fingerprints.

Review round 2: reproduced finding .3 in fresh unit and actual CLI config runs before changing production. Linked separate-git-dir/submodule CommonDir has no inferred MainWorktree; Codex uses current toplevel. Added that fallback and linked fixtures in the two existing MCP test files. New unit fixture initially missed canonical macOS child-denial keys; corrected fixture only, recorded task error and retained failed log. Overlay removing only the fallback reproduces both failures with the corrected fixture. Current fingerprint 7d220dbb92e357e9891550e809e9bf264f199c7e51ed71cf312de462f840b97b (per-file hash map scheme, specified in review2-source-manifest.json). Only three internal files changed since review 1; parser/benchmark and other previous corrections remain unchanged.
New passing evidence: review2-unit-green, review2-cli-final (25 installed CLI 0.169.0 config cases, only Bint enabled), review2-race, host/Windows vet and staticcheck, and build. Full suite and independent b-review remain Core-owned next gates; previous verify-2 passes the prior snapshot, not this production change. No model/service inference, nested agent, Git delivery, state-file or protected-path edit.
