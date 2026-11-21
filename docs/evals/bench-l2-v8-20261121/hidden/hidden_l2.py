"""Bench v8. Exercise the public Bint CLI against disposable project moves."""
import argparse
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import statistics
import subprocess
import sys
import tempfile
import time
import uuid


DATA = tuple(f"D{i}" for i in range(1, 10))
MOVES = ("M1", "M2", "M3", "M4")
STRIP = ("BINT_", "AI_AGENT", "CLAUDE", "CODEX_", "GEMINI_CLI", "GIT_", "ANTHROPIC_")


@dataclass
class Output:
    code: int | None
    text: str
    error: str
    seconds: float

    def result(self, condition=True, reason="missing"):
        if self.code is None:
            return "timeout"
        if self.code != 0:
            return f"exit:{self.code}"
        return "ok" if condition else reason

    def json(self):
        try:
            return json.loads(self.text)
        except ValueError:
            return None


class Fixture:
    def __init__(self, bint, work, label, state=None, initial=None):
        self.bint = bint.resolve()
        self.work = work
        self.case = label
        self.id = uuid.uuid4().hex[:10]
        self.mark = {d: f"L2-{label}-{d}-{self.id}" for d in DATA}
        self.session = str(uuid.uuid4())
        self.mark["D5"] = self.session
        self.mark["K"] = f"L2-{label}-K-{self.id}"
        self.run_id = ""
        self.original = initial or work / "w" / (("Proj" if label == "M3" else "proj-") + self.id)
        self.repo = self.original
        self.state = state or work / "state"
        self.cfg = work / "cfg"
        self.tmp = work / "tmp"
        for path in (self.state, self.cfg, self.tmp):
            path.mkdir(parents=True, exist_ok=True)
        self.env = {k: v for k, v in os.environ.items() if not k.upper().startswith(STRIP)}
        self.env.update(BINT_STATE_DIR=str(self.state), BINT_CONFIG_HOME=str(self.cfg),
                        TMP=str(self.tmp), TEMP=str(self.tmp), TMPDIR=str(self.tmp),
                        GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull,
                        PATH=str(self.bint.parent) + os.pathsep + self.env["PATH"])
        self.tasks = {}
        self.green = ""

    def command(self, executable, args, stdin=None, extra=None):
        start = time.monotonic()
        with tempfile.TemporaryFile(dir=self.tmp) as inp, tempfile.TemporaryFile(dir=self.tmp) as out, \
                tempfile.TemporaryFile(dir=self.tmp) as err:
            inp.write((stdin or "").encode("utf-8"))
            inp.seek(0)
            with subprocess.Popen([str(executable), *args], cwd=self.repo, env=self.env | (extra or {}),
                                  stdin=inp, stdout=out, stderr=err) as proc:
                try:
                    code = proc.wait(timeout=120)
                except subprocess.TimeoutExpired:
                    try:
                        subprocess.run(["taskkill", "/T", "/F", "/PID", str(proc.pid)],
                                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
                    finally:
                        if proc.poll() is None:
                            proc.kill()
                    return Output(None, "", "", time.monotonic() - start)
            out.seek(0)
            err.seek(0)
            return Output(code, out.read().decode("utf-8", errors="replace"),
                          err.read().decode("utf-8", errors="replace"), time.monotonic() - start)

    def redact(self, text):
        for marker in (*self.mark.values(), self.session):
            text = text.replace(marker, "[marker]")
        return text[-700:]

    def require(self, kind, out):
        if out.code != 0:
            raise RuntimeError(f"{kind} ({out.result()}): {self.redact(out.error or out.text)}")
        return out

    def run(self, *args, stdin=None, extra=None, required=False):
        out = self.command(self.bint, args, stdin, extra)
        return self.require("bint " + " ".join(args[:2]), out) if required else out

    def git(self, *args):
        out = self.command("git", args)
        return self.require("git " + args[0], out).text.strip()

    def create(self, marker=None, empty=False):
        self.repo.mkdir(parents=True)
        self.git("init", "-q", "-b", "main")
        self.git("config", "user.email", "bench@local")
        self.git("config", "user.name", "bench")
        self.git("config", "core.autocrlf", "false")
        if not empty:
            (self.repo / "go.mod").write_text("module l2fixture\n\ngo 1.22\n", encoding="utf-8")
            (self.repo / "fixture.go").write_text(f"package fixture\n\nconst Fixture = {json.dumps(marker or self.id)}\n",
                                                 encoding="utf-8")
            (self.repo / "fixture_test.go").write_text(
                'package fixture\n\nimport "testing"\n\nfunc TestFixture(t *testing.T) {}\n', encoding="utf-8")
        self.git("add", "-A")
        self.git("commit", "--allow-empty", "-qm", "fixture")
        self.init()

    def init(self):
        self.run("init", "--no-hooks", "--harness", "codex", required=True)

    def new_task(self, marker):
        result = self.run("task", "new", marker, "--no-plan", "--shared", "--json", required=True)
        doc = result.json()
        if isinstance(doc, dict) and doc.get("id"):
            return doc["id"]
        match = re.search(r"\b01[0-9A-HJKMNP-TV-Z]{24}\b", result.text)
        if not match:
            raise RuntimeError("task new did not report the task ID")
        return match.group()

    def populate(self):
        self.create()
        task = self.tasks["D1"] = self.new_task(self.mark["D1"])
        self.git("add", "-A", "--", ".bint")
        self.git("commit", "-qm", "commit first task")
        self.tasks["D2"] = self.new_task(self.mark["D2"])
        # The normal workflow records BaseTree, which a bare status transition does not do.
        # Use a deterministic local step so no live model or network is part of grading.
        step = self.run("run", "b-feat", "--task", task, "--agent-command",
                        r"printf '\n// fixture tracked change\n' >> fixture.go")
        if step.code not in (0, 1, 2, 3, 4, 5):
            self.require("local b-feat step", step)
        self.run("knowledge", "add", "--type", "warning", "--paths", "fixture.go",
                 "--evidence", "fixture.go:1", self.mark["K"], extra={"BINT_AGENT": "1"}, required=True)
        self.run("task", "set", task, "--verify-command", f'echo {self.mark["D3"]}', required=True)
        self.run("task", "approve", task, required=True)
        self.run("task", "verify", task, required=True)
        hook = {"session_id": self.session, "turn_id": str(uuid.uuid4()), "cwd": str(self.repo),
                "model": "gpt-6.2-sol", "permission_mode": "bypassPermissions", "transcript_path": ""}
        events = [dict(hook, hook_event_name="SessionStart", source="startup"),
                  dict(hook, hook_event_name="UserPromptSubmit", prompt=self.mark["D5"]),
                  dict(hook, hook_event_name="PostToolUse", tool_name="Bash", tool_use_id="fixture-test",
                       tool_input={"command": "go test ./... # " + self.mark["D5"]},
                       tool_response="ok   l2fixture\n")]
        for event in events:
            self.run("hook", "codex", stdin=json.dumps(event), required=True)
        self.run("events", "ingest", required=True)
        runs = self.run("runs", "list", "--json", required=True).json()
        if not isinstance(runs, list):
            raise RuntimeError("runs list did not return a list")
        self.run_id = next((row["run_id"] for row in runs if self.session in strings(row) and row.get("run_id")), "")
        if not self.run_id:
            raise RuntimeError("hook fixture run did not appear in runs list")
        self.run("runs", "rate", self.run_id, "good", "--note", self.mark["D8"], required=True)
        review = {"summary": "fixture review", "findings": [
            {"kind": "bug", "severity": "high", "title": self.mark["D6"], "path": "fixture.go",
             "lines": "1", "evidence": "package fixture", "body": "fixture finding"}]}
        self.run("task", "review", task, "-", stdin=json.dumps(review), required=True)
        self.run("task", "error", task, "--step", "b-feat", "--question", self.mark["D7"], required=True)
        self.run("task", "status", task, "blocked", required=True)
        shown = self.run("task", "show", task, "--json", required=True).json()
        self.green = ((shown or {}).get("last_green") or {}).get("tree", "")
        preview = self.run("task", "restore-green", task, "--json", required=True)
        if not self.green or self.green not in preview.text:
            raise RuntimeError("restore-green did not report the initial tree")

    def move(self, target):
        target.parent.mkdir(parents=True, exist_ok=True)
        try:
            self.repo.rename(target)
        except OSError as err:
            if err.winerror != 17 or self.case != "M2":
                raise
            shutil.move(self.repo, target)
        self.repo = target

    def destinations(self):
        if self.case == "M2":
            return [self.work / "khác" / f"Dự án của tôi (bản mới) {self.id}"]
        if self.case == "M3":
            return [self.repo.with_name(self.repo.name.lower())]
        if self.case == "M4":
            return [self.repo.with_name(self.repo.name + "-once"), self.work / "twice" / self.repo.name]
        return [self.repo.with_name(self.repo.name + "-renamed")]


def strings(value):
    if isinstance(value, str):
        yield value
    elif isinstance(value, list):
        for item in value:
            yield from strings(item)
    elif isinstance(value, dict):
        for item in value.values():
            yield from strings(item)


def log_paths(out):
    return sorted({x for x in strings(out.json()) if x.lower().endswith(".log") and ("/" in x or "\\" in x)})


def log_has(path, marker):
    try:
        return marker in path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return False


def presence(fx):
    task = fx.tasks["D1"]
    listing = fx.run("task", "list", "--all", "--json")
    shown = fx.run("task", "show", task, "--json")
    runs = fx.run("runs", "list", "--json")
    rating = fx.run("runs", "show", fx.run_id, "--json") if fx.run_id in runs.text else runs
    findings = fx.run("findings", "list", "--open", "--json")
    knowledge = fx.run("knowledge", "list", "--all", "--json")
    logs = [fx.repo / p for p in log_paths(shown)]
    return {
        "D1": task_presence(listing, task, fx.mark["D1"]),
        "D2": task_presence(listing, fx.tasks["D2"], fx.mark["D2"]),
        "D3": shown.result(bool(logs) and all(p.is_file() for p in logs) and
                           any(log_has(p, fx.mark["D3"]) for p in logs), "log_missing"),
        "D5": runs.result(fx.mark["D5"] in runs.text),
        "D6": findings.result(fx.mark["D6"] in findings.text),
        "D7": shown.result(fx.mark["D7"] in shown.text),
        "D8": rating.result(fx.mark["D8"] in rating.text),
        "D9": shown.result(fx.green in shown.text),
        "K": knowledge.result(fx.mark["K"] in knowledge.text),
    }


def task_presence(out, task, marker):
    def matches(row):
        if isinstance(row, dict):
            values = list(strings(row))
            return (task in values and any(marker in v for v in values)) or any(matches(x) for x in row.values())
        return isinstance(row, list) and any(matches(x) for x in row)
    return out.result(matches(out.json()))


def d4(fx, task, marker):
    verify = fx.run("task", "verify", task, "--json")
    verified = marker in verify.text and any(log_has(fx.repo / path, marker) for path in log_paths(verify))
    return verify.result(verified, "verify_did_not_run")


def check(fx, pending=False):
    data = presence(fx)
    # Preview before rerunning verify so verification cannot recreate a missing green point.
    if data["D9"] == "ok":
        restore = fx.run("task", "restore-green", fx.tasks["D1"], "--json")
        data["D9"] = restore.result(fx.green in restore.text, "wrong_green_tree")
    data["D4"] = d4(fx, fx.tasks["D1"], fx.mark["D3"])
    return {d: data[d] for d in ((*DATA, "K") if pending else DATA)}


def isolated(fx, committed_task):
    details = presence(fx)
    if committed_task:
        allowed = {"D1": "ok", "D2": "missing", "D3": "log_missing", "D5": "missing",
                   "D6": "missing", "D7": "missing", "D8": "missing", "D9": "missing", "K": "missing"}
        preview = fx.run("task", "restore-green", fx.tasks["D1"], "--json")
        approve_status = fx.run("task", "show", fx.tasks["D1"], "--json")
        # Reject even an approved command whose verification data is absent.
        shown = approve_status.json()
        details["D4"] = approve_status.result(
            isinstance(shown, dict) and shown.get("verify_confirmed") is not True, "unexpected_approval")
        return (all(details.get(d) == value for d, value in allowed.items()) and details["D4"] == "ok"
                and preview.code is not None and fx.green not in preview.text), details
    for data in ("D3", "D7", "D9"):
        # There is no such task in this project, so its show command may reject the ID.
        if details[data] in ("exit:1", "exit:4") and details["D1"] == "missing":
            details[data] = "task_absent"
    allowed = {"D1": {"missing"}, "D2": {"missing"}, "D3": {"task_absent", "log_missing"},
               "D5": {"missing"}, "D6": {"missing"}, "D7": {"task_absent", "missing"},
               "D8": {"missing"}, "D9": {"task_absent", "missing"}, "K": {"missing"}}
    return all(details[d] in allowed[d] for d in allowed), details


def n1(bint, work):
    results = {}

    fx = Fixture(bint, work / "clone", "N1-clone")
    fx.populate()
    origin = fx.repo
    clone = origin.with_name("clone")
    fx.git("clone", "-q", "--", str(origin), str(clone))
    fx.repo = clone
    fx.init()
    marker = f"L2-N1-clone-new-{fx.id}"
    clone_task = fx.new_task(marker)
    no_leak, details = isolated(fx, committed_task=True)
    fx.repo = origin
    origin_list = fx.run("task", "list", "--all", "--json")
    origin_isolated = origin_list.code == 0 and clone_task not in origin_list.text and marker not in origin_list.text
    results["clone"] = {"pass": no_leak and origin_isolated, "checks": details, "origin_isolated": origin_isolated}

    a = Fixture(bint, work / "same", "N1-same-a", initial=work / "same" / "a" / "app")
    b = Fixture(bint, work / "same-b", "N1-same-b", state=a.state, initial=work / "same" / "b" / "app")
    a.create()
    b.create()
    a_task = a.new_task(a.mark["D1"])
    b_task = b.new_task(b.mark["D1"])
    a_list = a.run("task", "list", "--all", "--json")
    b_list = b.run("task", "list", "--all", "--json")
    results["same_name"] = {"pass": task_presence(a_list, a_task, a.mark["D1"]) == "ok"
                            and task_presence(b_list, b_task, b.mark["D1"]) == "ok"
                            and b_task not in a_list.text and a_task not in b_list.text}

    fx = Fixture(bint, work / "round-trip", "N1-round-trip")
    fx.populate()
    fx.move(fx.destinations()[0])
    fx.move(fx.original)
    fx.run("status")
    checks = check(fx, pending=True)
    results["round_trip"] = {"pass": all(x == "ok" for x in checks.values()), "checks": checks}

    fx = Fixture(bint, work / "stationary", "N1-stationary")
    fx.populate()
    new = fx.new_task(fx.mark["D9"])
    stationary = check(fx, pending=True)
    listed = task_presence(fx.run("task", "list", "--all", "--json"), new, fx.mark["D9"])
    results["normal_use"] = {"pass": listed == "ok" and all(x == "ok" for x in stationary.values()),
                             "checks": stationary, "new_task": listed}

    fx = Fixture(bint, work / "old-path", "N1-old-path")
    fx.populate()
    fx.move(fx.destinations()[0])
    moved = fx.repo
    first = fx.run("status")
    fx.repo = fx.original
    fx.create(marker="unrelated repository")
    no_leak, checks_new = isolated(fx, committed_task=False)
    fx.repo = moved
    checks_moved = check(fx, pending=True)
    results["old_path_reused"] = {"pass": no_leak and all(x == "ok" for x in checks_moved.values()),
                                  "status_after_move_exit": first.code,
                                  "new_repo_isolated": no_leak, "new_repo": checks_new, "moved_repo": checks_moved}
    return {"pass": all(c["pass"] for c in results.values()), "cases": results}


def matrix(bint, work, oracle=None):
    results = {}
    controls = {}
    for move in MOVES:
        fx = Fixture(bint, work / move, move)
        try:
            fx.populate()
            if oracle:
                controls[move] = check(fx)
            actions = []
            for i, target in enumerate(fx.destinations()):
                old = fx.repo
                fx.move(target)
                if oracle:
                    oracle(fx, old)
                actions.append(fx.run("status").code)
                if move == "M4" and i == 0:
                    actions.append(fx.run("task", "list").code)
            results[move] = {"discovery_exits": actions, "cells": check(fx)}
        except (RuntimeError, OSError) as err:
            reason = "fixture_timeout" if "(timeout)" in str(err) else "fixture_error"
            results[move] = {"fixture_error": fx.redact(str(err)), "cells": dict.fromkeys(DATA, reason)}
        print(move, sum(result == "ok" for result in results[move]["cells"].values()), file=sys.stderr, flush=True)
    score = sum(cell == "ok" for column in results.values() for cell in column["cells"].values())
    return {"green": score, "total": len(DATA) * len(MOVES), "matrix": results,
            **({"fixture_before_move": controls} if oracle else {})}


def prepare_decoys(base, decoys):
    manifest = decoys / "complete.json"
    if manifest.exists():
        return
    state = decoys / "state"
    decoys.mkdir(parents=True, exist_ok=False)

    def create(i):
        fx = Fixture(base, decoys / "repo" / f"{i:03d}", "N2-decoy", state=state)
        fx.create(empty=True)

    with ThreadPoolExecutor(max_workers=4) as pool:
        for _ in pool.map(create, range(200)):
            pass
    if len(list(state.iterdir())) != 200:
        raise RuntimeError("expected 200 independently initialized Bint state directories")
    write_json(manifest, {"count": 200, "base": str(base)})


def time_lists(bint, base, work, decoys):
    if json.loads((decoys / "complete.json").read_text(encoding="utf-8"))["count"] != 200:
        raise RuntimeError("N2 decoy library is incomplete")
    fixtures = {}
    for label, exe in (("base_stationary", base), ("stationary", bint), ("moved", bint)):
        workdir = work / label
        state = workdir / "state"
        shutil.copytree(decoys / "state", state)
        fx = Fixture(exe, workdir, label, state=state)
        fx.create()
        fx.new_task(fx.mark["D1"])
        fixtures[label] = fx
    moved = fixtures["moved"]
    moved.move(moved.destinations()[0])
    first = moved.run("task", "list")
    samples = {k: [] for k in fixtures}
    exits = {k: [] for k in fixtures}
    for _ in range(5):
        for label, fx in fixtures.items():
            result = fx.run("task", "list")
            samples[label].append(round(result.seconds, 6))
            exits[label].append(result.code)
    medians = {label: statistics.median(values) for label, values in samples.items()}
    ratio = medians["moved"] / medians["base_stationary"]
    return {"pass": all(code == 0 for code in exits["base_stationary"]) and
            all(code is not None for code in exits["moved"]) and ratio <= 3,
            "ratio": ratio, "moved_usable": all(code == 0 for code in exits["moved"]),
            "seconds": samples, "exit": exits, "medians_s": medians,
            "first_after_move": {"seconds": first.seconds, "exit": first.code}, "decoys": 200}


def write_json(out, data):
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def load_oracle(path):
    spec = importlib.util.spec_from_file_location("bench_v8_baseline_oracle", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.migrate


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--bint", type=Path, required=True)
    parser.add_argument("--base-bint", type=Path)
    parser.add_argument("--work", type=Path)
    parser.add_argument("--out", type=Path)
    parser.add_argument("--decoys", type=Path)
    parser.add_argument("--prepare-decoys", action="store_true")
    parser.add_argument("--n2-only", action="store_true")
    parser.add_argument("--matrix-only", action="store_true")
    parser.add_argument("--oracle", type=Path)
    args = parser.parse_args()
    if args.prepare_decoys:
        prepare_decoys(args.bint.resolve(), args.decoys.resolve())
        return
    work = args.work.resolve()
    work.mkdir(parents=True, exist_ok=False)
    try:
        if args.n2_only:
            result = {"N2": time_lists(args.bint, args.base_bint, work, args.decoys.resolve())}
        else:
            result = matrix(args.bint, work, load_oracle(args.oracle) if args.oracle else None)
            if not args.oracle and not args.matrix_only:
                try:
                    result["N1"] = n1(args.bint, work / "N1")
                except (RuntimeError, OSError) as err:
                    result["N1"] = {"pass": False, "fixture_error": str(err)}
    except (RuntimeError, OSError) as err:
        write_json(args.out, {"fixture_error": str(err)})
        raise SystemExit(2) from None
    write_json(args.out, result)


if __name__ == "__main__":
    main()
