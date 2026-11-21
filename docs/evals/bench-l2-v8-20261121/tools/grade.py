"""Snapshot a bench v8 tree and grade it after all agents have stopped."""
import argparse
from contextlib import contextmanager
import json
import msvcrt
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import time


ROOT = Path(__file__).resolve().parents[1]
SHORT = Path("D:/bt")
BASE = ROOT / "bin" / "bint-base.exe"
HIDDEN = ROOT / "hidden" / "hidden_l2.py"
STRIP = ("CLAUDE", "AI_AGENT", "BINT_", "CODEX_", "GEMINI_CLI", "ANTHROPIC_", "GIT_")


def env_for(tmp):
    env = {k: v for k, v in os.environ.items() if not k.upper().startswith(STRIP)}
    env.update(TMP=str(tmp), TEMP=str(tmp), TMPDIR=str(tmp), GIT_CONFIG_NOSYSTEM="1")
    return env


def write_json(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_suffix(path.suffix + ".tmp")
    temp.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    for _ in range(5):
        try:
            temp.replace(path)
            return
        except PermissionError:
            time.sleep(0.1)
    temp.replace(path)


@contextmanager
def machine_lock():
    SHORT.mkdir(exist_ok=True)
    start = time.monotonic()
    with (SHORT / "bint-bench-grade.lock").open("a+b") as lock:
        while True:
            try:
                lock.seek(0)
                msvcrt.locking(lock.fileno(), msvcrt.LK_NBLCK, 1)
                break
            except OSError:
                time.sleep(5)
        try:
            yield round(time.monotonic() - start, 3)
        finally:
            lock.seek(0)
            msvcrt.locking(lock.fileno(), msvcrt.LK_UNLCK, 1)


def capture(cmd, cwd, env, check=True, timeout=300):
    return subprocess.run(cmd, cwd=cwd, env=env, capture_output=True, encoding="utf-8",
                          errors="replace", check=check, timeout=timeout)


def git(repo, env, *args):
    return capture(["git", *args], repo, env).stdout.strip()


def snapshot(repo, label):
    SHORT.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="v8-index-", dir=SHORT) as directory:
        tmp = Path(directory)
        env = env_for(tmp)
        isolated_index = dict(env, GIT_INDEX_FILE=str(tmp / "index"))
        git(repo, isolated_index, "read-tree", "bench")
        git(repo, isolated_index, "add", "-A")
        tree = git(repo, isolated_index, "write-tree")
        commit = git(repo, env, "-c", "user.name=bench", "-c", "user.email=bench@local", "commit-tree",
                     tree, "-p", "bench", "-m", f"bench v8 {label}")
        git(repo, env, "update-ref", f"refs/bench/{label}", commit)
        return commit


def export(repo, commit, source, env):
    source.mkdir()
    archive = source.parent / "source.tar"
    with archive.open("wb") as output:
        subprocess.run(["git", "archive", "--format=tar", commit], cwd=repo, env=env, stdout=output,
                       check=True, timeout=120)
    subprocess.run(["tar", "-xf", str(archive)], cwd=source, env=env, check=True, timeout=120)
    archive.unlink()


def l2(exe, tmp, dest, res, env):
    hidden_path = dest / "hidden.json"
    args = [sys.executable, "-I", str(HIDDEN), "--bint", str(exe)]
    hidden = capture([*args, "--work", str(tmp / "l2"), "--out", str(hidden_path)],
                     ROOT, env, check=False, timeout=7200)
    if hidden.returncode:
        res["hidden_exit"] = hidden.returncode
        res["hidden_error"] = hidden.stderr[-600:]
        if hidden_path.exists():
            res.update(json.loads(hidden_path.read_text(encoding="utf-8")))
        return
    result = json.loads(hidden_path.read_text(encoding="utf-8"))
    res.update(hidden_clean=result["green"], hidden_total=result["total"],
               hidden_pct=round(100 * result["green"] / result["total"], 3), N1=result["N1"])
    stamp = time.strftime("%Y%m%dT%H%M%S") + f"-{time.time_ns() % 1000000000:09}"
    n2_path = dest / f"n2-{stamp}.json"
    run = capture([*args, "--base-bint", str(BASE), "--work", str(tmp / "n2"), "--out", str(n2_path),
                   "--decoys", str(ROOT / "controls" / "n2-decoys"), "--n2-only"],
                  ROOT, env, check=False, timeout=3600)
    if run.returncode:
        res["n2_error"] = run.stderr[-600:]
        if n2_path.exists():
            res["n2_fixture_error"] = json.loads(n2_path.read_text(encoding="utf-8")).get("fixture_error")
        return
    n2 = json.loads(n2_path.read_text(encoding="utf-8"))["N2"]
    res.update(N2=n2, N2_file=n2_path.name)
    shutil.copyfile(n2_path, dest / "n2.json")
    with (dest / "n2-history.jsonl").open("a", encoding="utf-8") as history:
        history.write(json.dumps({"file": n2_path.name, "commit": res["commit"], "N2": n2}) + "\n")


def go_tests(source, dest, env, baseline_path, baseline, res):
    log = dest / "gotest.txt"
    with log.open("wb") as out:
        result = subprocess.run(["go", "test", "-count=1", "-timeout", "40m", "./..."], cwd=source, env=env,
                                stdout=out, stderr=subprocess.STDOUT, timeout=4 * 3600)
    content = log.read_text(encoding="utf-8", errors="replace")
    failed = sorted(set(re.findall(r"^\s*--- FAIL: (\S+)", content, re.M)))
    packages = sorted(set(re.findall(r"^FAIL[ \t]+(\S+)[ \t]", content, re.M)))
    res.update(gotest_exit=result.returncode, gotest_fail_tests=failed, gotest_fail_pkgs=packages)
    if baseline:
        write_json(baseline_path, {"commit": res["commit"], "fail_tests": failed, "fail_pkgs": packages,
                                   "exit": result.returncode})
        return
    control = json.loads(baseline_path.read_text(encoding="utf-8"))
    res.update(gotest_baseline_fail_tests=sorted(set(failed) & set(control["fail_tests"])),
               gotest_new_fail_tests=sorted(set(failed) - set(control["fail_tests"])),
               gotest_new_fail_pkgs=sorted(set(packages) - set(control["fail_pkgs"])))


def refresh_baseline(repo, source, dest, env, baseline_path, inherited, res):
    previous = json.loads(inherited.read_text(encoding="utf-8"))
    diff = git(repo, env, "diff", "--name-only", previous["commit"], res["commit"]).splitlines()
    if set(diff) != {"internal/cli/bdo_lean_goal_test.go", "internal/cli/breq_baseline_goal_test.go"}:
        raise RuntimeError(f"the skill-integrity refresh may only change the two test fixtures: {diff}")
    pattern = "^(TestGoalBDoLeanSlicesAndGates|TestGoalBReqBaselineAndSkillGates)$/^(skills_package_tests_synced|skill_published_copies|skill_size)$"
    helm = ["go", "test", "-count=1", "-timeout", "5m", "./internal/cli", "-run", pattern]
    with (dest / "gotest-refresh.txt").open("wb") as output:
        run = subprocess.run(helm, cwd=source, env=env, stdout=output, stderr=subprocess.STDOUT, timeout=600)
    if run.returncode:
        raise RuntimeError("the refreshed installed-skill assertions failed")
    fixed = {"TestGoalBDoLeanSlicesAndGates", "TestGoalBDoLeanSlicesAndGates/skills_package_tests_synced",
             "TestGoalBReqBaselineAndSkillGates", "TestGoalBReqBaselineAndSkillGates/skill_published_copies"}
    tests = sorted(set(previous["fail_tests"]) - fixed)
    packages = sorted(set(previous["fail_pkgs"]) - {"bint/internal/cli"})
    res.update(gotest_exit=1 if tests or packages else 0, gotest_fail_tests=tests, gotest_fail_pkgs=packages,
               baseline_full_suite_at=previous["commit"], baseline_refreshed_test_pattern=pattern)
    write_json(baseline_path, {"commit": res["commit"], "fail_tests": tests, "fail_pkgs": packages,
                               "exit": res["gotest_exit"], "full_suite_at": previous["commit"],
                               "focused_log": f"{dest.name}/gotest-refresh.txt"})


def grade(path, baseline=False, label="final", repo_tests=True, refresh=None):
    repo = path if baseline else path / "repo"
    dest = ROOT / "controls" / path.name if baseline else path if label == "final" else path / "checkpoints" / label
    dest.mkdir(parents=True, exist_ok=True)
    with machine_lock() as waited:
        tmp = Path(tempfile.mkdtemp(prefix="v8g-", dir=SHORT))
        env = env_for(tmp)
        base = git(repo, env, "rev-parse", "bench")
        if baseline:
            commit = base
        else:
            commit = git(repo, env, "rev-parse", f"refs/bench/{label}")
        baseline_path = ROOT / "controls" / f"gotest-baseline-{base[:12]}.json"
        source = tmp / "src"
        export(repo, commit, source, env)
        exe = tmp / "bint.exe"
        built = capture(["go", "build", "-o", str(exe), "./cmd/bint"], source, env, check=False, timeout=1800)
        diffstat = git(repo, env, "diff", "--shortstat", base, commit, "--", ".", ":!.bint", ":!.agents",
                       ":!.claude", ":!.codex")
        res = {"run": path.name, "label": label, "commit": commit, "baseline": base,
               "build_ok": built.returncode == 0, "build_err": built.stderr[-2000:], "diffstat": diffstat,
               "graded_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"), "waited_for_lock_s": waited}
        write_json(dest / "grade.json", res)
        if built.returncode == 0 and not baseline:
            l2(exe, tmp, dest, res, env)
            write_json(dest / "grade.json", res)
        if refresh and built.returncode == 0:
            refresh_baseline(repo, source, dest, env, baseline_path, refresh, res)
        elif (repo_tests or baseline) and built.returncode == 0:
            go_tests(source, dest, env, baseline_path, baseline, res)
        write_json(dest / "grade.json", res)
        summary = ("run", "label", "commit", "build_ok", "hidden_clean", "hidden_total", "gotest_exit")
        print(json.dumps({k: res.get(k) for k in summary}, ensure_ascii=False), flush=True)
        return res


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    parser = argparse.ArgumentParser()
    parser.add_argument("path", type=Path)
    parser.add_argument("--baseline", action="store_true")
    parser.add_argument("--label", default="final")
    parser.add_argument("--no-repo-tests", action="store_true")
    parser.add_argument("--refresh-baseline", type=Path)
    args = parser.parse_args()
    if args.refresh_baseline and not args.baseline:
        parser.error("--refresh-baseline requires --baseline")
    grade(args.path.resolve(), args.baseline, args.label, not args.no_repo_tests, args.refresh_baseline)
