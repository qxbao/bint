# Gateway group concurrency fixture correction

## Root cause

`internal/mcpgw/partition.acquireSem` and `toolsCall` keep the contract group's permit until `up.call` has received the upstream response. The failure was in the test upstream fixture: `fake.call` incremented `cur`, wrote a terminal response, and only decremented `cur` in a deferred function after `Flush` returned. A reader can receive the complete response and unblock the gateway before that sender unwinds. The next serialized contract call then starts while the previous fixture goroutine is still counted, making `max_concurrent` report 2 even though the gateway serialized the calls.

The deterministic regression uses an in-memory writer. It exposes a complete terminal frame to the test and blocks the writer before returning. Before the fixture change, success, RPC error, and cancellation each exposed `cur == 1` at that point. Progress notifications and the server-to-client `roots/list` request expose `cur == 1` while active, as expected. After the change, terminal frames expose `cur == 0` before the blocked write returns, and unwind leaves `cur == 0`.

## Change

In `fake.call` (test-only fixture), a local `sync.Once` completion function decrements `cur` immediately before each terminal success, RPC error, or cancellation response. The deferred call remains as a fallback and the once guard prevents a second decrement. Progress and server-to-client requests do not complete the active count. A test-only cancellation signal makes the cancellation case deterministic.

Production gateway code and the original `TestGroupMaxConcurrent` assertions/timing were not changed. The contract serialize case and compat overlap control remain in `groups_test.go`.

## Verification

Detailed commands, UTC timestamps, cwd, exit codes, and observed results are in [run log](evidence/runs/gateway-concurrency-fix/validation.txt).

The in-memory lifecycle regression passed normally and with `-race`; Linux and Windows `go vet ./...` passed. Loopback-dependent checks could not run in this sandbox because binding `127.0.0.1:0` and `[::1]:0` is denied. Core must run the repeated contract test, package tests, and the authorized `go test ./...` verification on the host. `staticcheck` was not installed in this environment.

## Review handoff

Manual diff review: only `internal/mcpgw/fake_test.go` and the new `fake_concurrency_test.go` change implementation/test code; no production gateway behavior, assertion relaxation, sleep increase, retry, config, or historical evidence changed. `git diff --check` passed. Formal b-review and Core verification remain for the orchestrator before PR update.
