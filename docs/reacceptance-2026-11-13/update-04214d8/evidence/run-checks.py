#!/usr/bin/env python3
"""Run the narrow, fixture-only checks for this report and retain raw outputs."""
import datetime
import argparse
import hashlib
import json
import os
import pathlib
import subprocess
import time

ROOT = pathlib.Path(__file__).resolve().parents[4]
EVIDENCE = pathlib.Path(__file__).resolve().parent
CHECKS = [
    ("C0a", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/workflow", "-run", "^(TestRunStep.*MCP.*|TestRunStepWritesBindingAndTools|TestWriteStepMCPConfig|TestStepMCPTools.*|TestPruneStaleJSON)$"]),
    ("C0b", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/eval", "-run", "^(TestCodexExclusiveMCP.*|TestCodexEvalArgsUnchangedByUserMCP|TestClaudeArgsForMCPTools|TestCodexArgsForMCPTools|TestCodexEvalCitationApproval)$"]),
    ("C0c", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/harness/...", "-run", ".*MCP.*"]),
    ("C0d", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/cli", "-run", "^TestKnowledgeCiteEndToEnd$"]),
    ("C1a", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/mcpcat/..."]),
    ("C1b", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/config", "-run", "^Test(.*MCP.*|RenderMatchesDefaults|RenderRoundTrip)$"]),
    ("C1c", ["go", "test", "-count=1", "-json", "-timeout", "10m", "./internal/app", "-run", "^Test(.*MCP.*|EnsureGitExcludeVariants)$"]),
    ("C1d", ["go", "test", "-count=1", "-json", "-timeout", "10m", "./internal/cli", "-run", "^TestMCP"]),
    ("C23", ["go", "test", "-count=1", "-json", "-timeout", "15m", "./internal/mcpgw"]),
    ("C4a", ["go", "test", "-count=1", "-json", "-timeout", "10m", "./internal/tui/app", "-run", "^Test(MCP|Golden|SystemScreenHasNoMCPTab|ReadOnlyAndAgentNeverReachDo)"]),
    ("C4b", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/tui/actions", "-run", "^(TestMCP.*|TestNoReferenceToSystemMCPTab)$"]),
    ("C4c", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/cli", "-run", "^Test(CommandAndFlagCoverage|CoverageCheckerCatchesGaps|ServiceMessageCLIInventory.*)$"]),
    ("C4d", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/tui/screens/mcp", "./internal/tui/components/tabs"]),
    ("C-RPC", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/mcpserver"]),
    ("C-README-CURRENT", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/cli", "-run", "^TestUsageDocumentsTUIGlobalKeys$"]),
    ("C1c-INDEX", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/app", "-run", "^TestMCPIndex$"]),
    ("C1c-LINKED-WORKTREE", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/app", "-run", "^TestMCPIndexLinkedWorktree$"]),
    ("C1c-DEGRADED", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/app", "-run", "^TestMCPCheckCodegraphDegraded$"]),
    ("C1c-SECRET-NAMES", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/app", "-run", "^TestMCPCheckNeverLeaksSecret$"]),
    ("C1c-DOCTOR", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/app", "-run", "^TestDoctorMCP$"]),
    ("C1c-INIT-PERMISSION", ["go", "test", "-count=1", "-json", "-timeout", "5m", "./internal/app", "-run", "^TestInitAddsMCPPermissionRule$"]),
]

def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def stamp():
    return datetime.datetime.now().astimezone().isoformat(timespec="seconds")

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--check", action="append", dest="check_ids", help="run only this check ID; repeat to select multiple")
parser.add_argument("--list", action="store_true", help="list check IDs and exit")
args = parser.parse_args()
if args.list:
    print("\n".join(check_id for check_id, _ in CHECKS))
    raise SystemExit(0)
selected = set(args.check_ids or [check_id for check_id, _ in CHECKS])
known = {check_id for check_id, _ in CHECKS}
unknown = selected - known
if unknown:
    parser.error("unknown check ID(s): " + ", ".join(sorted(unknown)))

RUNS = EVIDENCE / "runs"
RUNS.mkdir(parents=True, exist_ok=True)
run_id = datetime.datetime.now().astimezone().strftime("%Y%m%dT%H%M%S%z") + f"-{os.getpid()}"
RUN_DIR = RUNS / run_id
try:
    RUN_DIR.mkdir()
except FileExistsError:
    parser.error(f"run directory already exists; refusing to overwrite: {RUN_DIR}")

env = os.environ.copy()
try:
    head = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, env=env, capture_output=True, text=True, check=True).stdout.strip()
except (OSError, subprocess.CalledProcessError) as exc:
    parser.error(f"cannot record baseline HEAD: {exc}")
tracked_sources = [ROOT / "internal/app/mcp_svc_test.go", ROOT / "internal/app/init_mcp_perm_test.go", pathlib.Path(__file__).resolve()]
source_paths = set(tracked_sources)
for check_id, argv in CHECKS:
    if check_id not in selected:
        continue
    for arg in argv:
        if not arg.startswith("./internal/"):
            continue
        package = ROOT / arg[2:].removesuffix("...")
        if package.is_dir():
            source_paths.update(package.rglob("*.go"))
baseline = {
    "run_id": run_id,
    "head": head,
    "cwd": str(ROOT),
    "script_sha256": sha256(pathlib.Path(__file__).resolve()),
    "source_sha256": {str(path.relative_to(ROOT)): sha256(path) for path in sorted(source_paths) if path.is_file() and path.is_relative_to(ROOT)},
    "env_relevant": {k: env.get(k) for k in ["GOOS", "GOARCH", "GOTOOLCHAIN", "TMPDIR", "BINT_TASK_ID", "BINT_RUN_ID", "BINT_AGENT"]},
    "selected_checks": [check_id for check_id, _ in CHECKS if check_id in selected],
    "started_local": stamp(),
}
(RUN_DIR / "baseline.json").write_text(json.dumps(baseline, ensure_ascii=False, indent=2) + "\n")

summary = []
for check_id, argv in CHECKS:
    if check_id not in selected:
        continue
    started, t0 = stamp(), time.monotonic()
    p = subprocess.run(argv, cwd=ROOT, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    elapsed = round(time.monotonic() - t0, 3)
    ended = stamp()
    (RUN_DIR / f"{check_id}.stdout").write_text(p.stdout)
    (RUN_DIR / f"{check_id}.stderr").write_text(p.stderr)
    meta = {
        "id": check_id,
        "argv": argv,
        "cwd": str(ROOT),
        "started_local": started,
        "ended_local": ended,
        "elapsed_seconds": elapsed,
        "exit_code": p.returncode,
        "env_relevant": {k: env.get(k) for k in ["GOOS", "GOARCH", "GOTOOLCHAIN", "TMPDIR", "BINT_TASK_ID", "BINT_RUN_ID", "BINT_AGENT"]},
        "kind": "repository test fixture; no live MCP service or Claude",
        "head": head,
        "run_id": run_id,
        "stdout_file": f"{check_id}.stdout",
        "stderr_file": f"{check_id}.stderr",
    }
    (RUN_DIR / f"{check_id}.json").write_text(json.dumps(meta, ensure_ascii=False, indent=2) + "\n")
    summary.append({"id": check_id, "exit_code": p.returncode, "elapsed_seconds": elapsed})
    print(json.dumps(summary[-1], ensure_ascii=False), flush=True)

(RUN_DIR / "checks-summary.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n")
print("RUN_DIR=" + str(RUN_DIR), flush=True)
print("SUMMARY=" + str(RUN_DIR / "checks-summary.json"), flush=True)
