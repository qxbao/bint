# Bint cleanup verification

Passed: cleanup regression tests in internal/app, internal/eval, internal/config and CLI end-to-end tests; targeted race tests; go vet ./...; Windows go vet on changed packages; gofmt and git diff --check.

The full suite did not pass. The sandbox rejects local listening sockets; evaluation fixtures also reached the minimum free-space guard (999 MB free versus 1024 MB required). A bounded full run used -timeout 120s and several packages exceeded that limit. Initial affected-package VCS stamping failures were resolved in test invocation with GOFLAGS=-buildvcs=false.

Full command: GOCACHE=<task-temp>/go-cache TMPDIR=<task-temp>/testtmp GOFLAGS=-buildvcs=false go test ./... -timeout 120s

Failures and tests running at timeout:

```text
		TestVerifyApplyMovedRealCase (14s)
FAIL	bint/internal/app	120.507s
--- FAIL: TestEvalEndToEnd (25.53s)
		TestEvalMineFromTasksEndToEnd (2s)
FAIL	bint/internal/cli	126.801s
--- FAIL: TestRestrictedRPCProfileCleanupAndRejectsUnknownTools (0.18s)
--- FAIL: TestRunQuotaStopsAndResumes (27.72s)
		TestRunCancelDuringPrepareMarksCancelled (7s)
FAIL	bint/internal/eval	120.437s
		TestGuardReadOnly (33s)
		TestGuardReadOnly/sửa_tệp_theo_dõi_sạch (4s)
FAIL	bint/internal/evolve	120.525s
--- FAIL: TestHTTPJSONAndSSE (0.00s)
FAIL	bint/internal/mcpcat/probe	2.329s
--- FAIL: TestSecondDaemonRejected (0.00s)
--- FAIL: TestInfoFile (0.00s)
--- FAIL: TestAuthTable (0.00s)
--- FAIL: TestAdminStopShutsDown (0.00s)
--- FAIL: TestUpstreamEnv (0.00s)
--- FAIL: TestUpstreamEnvDefaultsAndMissing (0.00s)
--- FAIL: TestSecretsScrubbedFromStatus (0.00s)
--- FAIL: TestReviewerDoesNotSeeWriteTools (0.00s)
--- FAIL: TestDeniedCallsNeverReachUpstream (0.00s)
--- FAIL: TestRoleResolutionFailsClosed (0.00s)
--- FAIL: TestInteractiveIsProjectScoped (0.00s)
--- FAIL: TestToolsCacheSurvivesDaemonRestart (0.00s)
--- FAIL: TestExclusiveGroup (0.00s)
--- FAIL: TestGroupMaxConcurrent (0.00s)
--- FAIL: TestHTTPUpstreamSharedHandshake (0.00s)
FAIL	bint/internal/mcpgw	0.015s
		TestInstallFlowsNameTakenByUnpublishedSkillKeepsEntryForRemoval (11s)
		TestInstallFlowsNameTakenByUnpublishedSkillKeepsEntryForRemoval/tiến_hoá_không_active (7s)
FAIL	bint/internal/skills	120.341s
		TestSystemConfigUpgradeActionsWithRealServices (15s)
FAIL	bint/internal/tui/actions	120.156s
		TestEvalRunEscCancelsBatchAndShowsResumeCommand (19s)
FAIL	bint/internal/tui/app	120.059s
TestWorktreeCodegraphAddsExcludeWhenOriginIndexed (workflow package timeout)
```

