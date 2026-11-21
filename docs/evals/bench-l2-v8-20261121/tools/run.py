"""Đợt 8: một lượt A, B hoặc B′ trên bài L2.

python run.py <arm> <k> [--cap-min 180]
Mỗi lượt có repo, CODEX_HOME, thư mục trạng thái Bint và TMP riêng: runs/<arm>-<k>/.
Ghi runs/<arm>-<k>/run.json (dòng thời gian, thao tác người dùng giả, kết thúc thế nào).
"""
import argparse, glob, json, os, re, shutil, signal, subprocess, sys, time
from pathlib import Path

from hook_trust import enable_bint

W = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BINT = os.path.join(W, "bin", "bint.exe")
MODEL = "gpt-6.2-sol"
PROMPT = ("Tôi hay đổi tên hoặc chuyển thư mục dự án sang chỗ khác, mà mỗi lần như vậy bint quên sạch: tác vụ, lịch sử, mọi thứ. Sửa giúp tôi để chuyển\n"
          "thư mục thoải mái mà không mất gì. Việc này chắc lớn nên cứ làm từ từ qua nhiều lần cũng được.")
# Người dùng giả low-tech: một câu trả lời cố định lấy từ thẻ người dùng (log/persona-card.md), dùng cho mọi câu hỏi.
ANSWER = ("Mục tiêu của tôi: đổi tên hay chuyển thư mục dự án đi đâu (kể cả chỗ có dấu cách, tiếng Việt, hay chỉ đổi chữ hoa/thường) thì mọi thứ bint đã có vẫn\n"
          "còn nguyên. Không cần chạy lệnh tay, bint tự nhận ra là được. Nếu có hai bản sao dự án cùng lúc thì đó là hai dự án riêng, đừng trộn. Tôi không muốn\n"
          "bint chạy hỏng hay chậm hẳn đi. Còn câu kỹ thuật khác thì tôi không rành, bạn tự quyết cho an toàn.")
SELF_CHECK = ("Tự kiểm lại toàn bộ việc bạn đã làm so với yêu cầu ban đầu của tôi: «" + PROMPT + "» "
              "Rà mọi loại dữ liệu bint lưu cho một dự án xem loại nào còn mất khi\n"
              "đổi tên hay chuyển thư mục, chạy test liên quan, và sửa mọi chỗ còn thiếu hoặc sai. Đừng làm lại phần đã đúng.")
AGENT_VARS = ("CLAUDE", "AI_AGENT", "BINT_", "CODEX_", "GEMINI_CLI", "ANTHROPIC_", "GIT_")
MAX_USER_ACTIONS = 6
BPRIME_LOCAL = """  variant: bprime
  budget_minutes: 170
  probe_confirm: false
  verify:
    scope: affected
    affected_command: "go test -count=1 {test_dirs}"
"""


class Run:
    def __init__(self, arm, k, cap_min):
        self.arm, self.k = arm, k
        self.name = f"{arm}-{k}"
        self.dir = os.path.join(W, os.environ.get("BENCH_RUNS", "runs"), self.name)
        self.repo = os.path.join(self.dir, "repo")
        self.home = os.path.join(self.dir, "codex-home")
        self.state = os.path.join(self.dir, "state")
        self.cfg = os.path.join(self.dir, "cfg")
        self.tmp = os.path.join(self.dir, "tmp")
        self.logdir = os.path.join(self.dir, "log")
        self.t0 = None
        self.deadline = None
        self.cap = cap_min * 60
        self.rec = {"run": self.name, "arm": arm, "k": k, "cap_s": self.cap,
                    "events": [], "user_actions": []}

    # ---------- dựng môi trường ----------
    def env(self):
        env = {k: v for k, v in os.environ.items() if not k.upper().startswith(AGENT_VARS)}
        env.update(TMP=self.tmp, TEMP=self.tmp, TMPDIR=self.tmp, CODEX_HOME=self.home, GIT_CONFIG_NOSYSTEM="1")
        if self.arm in ("B", "Bp"):
            env["BINT_STATE_DIR"] = self.state
            env["BINT_CONFIG_HOME"] = self.cfg
            env["PATH"] = os.path.join(W, "bin") + os.pathsep + env["PATH"]
        return env

    def setup(self):
        if os.path.exists(self.dir):
            raise SystemExit(f"{self.dir} đã tồn tại")
        tmpl = os.path.join(W, "tmpl-A" if self.arm == "A" else "tmpl-B")
        shutil.copytree(tmpl, self.repo, symlinks=True)
        shutil.copytree(os.path.join(W, "codex-home-tmpl"), self.home)
        for d in (self.state, self.cfg, self.tmp, self.logdir):
            os.makedirs(d, exist_ok=True)
        with open(os.path.join(self.home, "config.toml"), "a", encoding="utf-8") as f:
            for p in (self.repo, self.state, self.tmp):
                f.write(f"\n[projects.'{p.lower()}']\ntrust_level = \"trusted\"\n")
        if self.arm in ("B", "Bp"):
            local = os.path.join(self.repo, ".bint", "local.yml")
            with open(local, encoding="utf-8") as f:
                settings = f.read()
            if self.arm == "Bp":
                if not re.search(r"^workflow:\s*$", settings, re.M):
                    raise RuntimeError("missing template workflow section")
                settings = settings.replace("workflow:\n", "workflow:\n" + BPRIME_LOCAL, 1)
                with open(local, "w", encoding="utf-8", newline="\n") as f:
                    f.write(settings)
            with open(os.path.join(self.logdir, "bint-local.yml"), "w", encoding="utf-8", newline="\n") as f:
                f.write(settings)
            r = subprocess.run([BINT, "init", "--hooks", "--git", "track"], cwd=self.repo, env=self.env(),
                               capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=120)
            with open(os.path.join(self.logdir, "init.txt"), "w", encoding="utf-8") as f:
                f.write(r.stdout + r.stderr)
            self.event("init", exit=r.returncode)
            if r.returncode and not os.path.isfile(os.path.join(self.repo, ".codex", "hooks.json")):
                raise RuntimeError("Bint init did not install Codex hooks")
            hooks = enable_bint(Path(self.repo), self.env(), Path(BINT))
            self.event("codex_hooks_trusted", count=hooks)

    # ---------- tiện ích ----------
    def now(self):
        return round(time.monotonic() - self.t0, 1)

    def left(self):
        return self.deadline - time.monotonic()

    def event(self, kind, **kw):
        e = {"t": self.now() if self.t0 else 0, "kind": kind, **kw}
        self.rec["events"].append(e)
        self.save()
        print(self.name, json.dumps(e, ensure_ascii=False)[:400], flush=True)

    def save(self):
        path = os.path.join(self.dir, "run.json")
        with open(path + ".tmp", "w", encoding="utf-8") as f:
            json.dump(self.rec, f, ensure_ascii=False, indent=1)
        for _ in range(5):
            try:
                os.replace(path + ".tmp", path)
                return
            except PermissionError:
                time.sleep(0.1)
        os.replace(path + ".tmp", path)

    def exec(self, label, cmd, stdin_text=None):
        """Chạy cmd tới khi xong hoặc hết trần; trả (exit, stdout, how)."""
        n = len([e for e in self.rec["events"] if e["kind"] == "cmd"]) + 1
        out_p = os.path.join(self.logdir, f"{n:02d}-{label}.out")
        err_p = os.path.join(self.logdir, f"{n:02d}-{label}.err")
        t = self.now()
        with open(out_p, "wb") as out, open(err_p, "wb") as err:
            p = subprocess.Popen(cmd, cwd=self.repo, env=self.env(), stdin=subprocess.PIPE if stdin_text else subprocess.DEVNULL,
                                 stdout=out, stderr=err, creationflags=subprocess.CREATE_NEW_PROCESS_GROUP)
            if stdin_text:
                p.stdin.write(stdin_text.encode("utf-8"))
                p.stdin.close()
            how = "exit"
            while p.poll() is None:
                if self.left() <= 0:
                    how = "cap"
                    try:
                        p.send_signal(signal.CTRL_BREAK_EVENT)
                    except Exception:
                        pass
                    try:
                        p.wait(45)
                    except subprocess.TimeoutExpired:
                        how = "cap_kill"
                        subprocess.run(["taskkill", "/T", "/F", "/PID", str(p.pid)], capture_output=True)
                    break
                time.sleep(2)
            try:
                p.wait(60)
            except subprocess.TimeoutExpired:
                subprocess.run(["taskkill", "/T", "/F", "/PID", str(p.pid)], capture_output=True)
        self.event("cmd", label=label, cmd=cmd if not stdin_text else cmd + ["<stdin>"], start=t, dur=round(self.now() - t, 1),
                   exit=p.returncode, how=how, out=out_p)
        with open(out_p, encoding="utf-8", errors="replace") as f:
            return p.returncode, f.read(), how

    def tokens(self):
        tot = {"sessions": 0, "input": 0, "cached": 0, "output": 0}
        for f in glob.glob(os.path.join(self.home, "sessions", "**", "*.jsonl"), recursive=True):
            last = None
            for line in open(f, encoding="utf-8", errors="replace"):
                if '"total_token_usage"' in line:
                    try:
                        last = json.loads(line)["payload"]["info"]["total_token_usage"]
                    except Exception:
                        pass
            if last:
                tot["sessions"] += 1
                tot["input"] += last.get("input_tokens", 0)
                tot["cached"] += last.get("cached_input_tokens", 0)
                tot["output"] += last.get("output_tokens", 0)
        return tot

    # ---------- nhánh A ----------
    def codex_first(self):
        return self.exec("codex", ["codex", "exec", "--json", "--sandbox", "workspace-write", "--skip-git-repo-check",
                                   "--cd", self.repo, "--model", MODEL, "-"], PROMPT)

    def codex_resume(self, text):
        return self.exec("codex-resume", ["codex", "exec", "resume", "--last", "--json", "--skip-git-repo-check",
                                          "-c", 'sandbox_mode="workspace-write"', "--model", MODEL, "-"], text)

    def run_a(self):
        code, out, how = self.codex_first()
        while how == "exit" and code == 0 and self.asks_question(out):
            if len(self.rec["user_actions"]) >= MAX_USER_ACTIONS:
                self.rec["end"] = "user_actions_exhausted"
                return
            self.rec["user_actions"].append({"t": self.now(), "action": "answer_direct"})
            code, out, how = self.codex_resume(ANSWER)
        self.rec["end"] = "cap" if how != "exit" else "agent_exit" if code == 0 else "agent_failed"

    @staticmethod
    def asks_question(out):
        messages = []
        for line in out.splitlines():
            try:
                event = json.loads(line)
            except ValueError:
                continue
            item = event.get("item", {})
            if event.get("type") == "item.completed" and item.get("type") == "agent_message":
                messages.append(item.get("text", ""))
        return bool(messages and messages[-1].rstrip().endswith("?"))

    # ---------- nhánh B / B′ ----------
    def bint(self, label, args):
        _, out, how = self.exec(label, [BINT] + args)
        # Bint có thể in lời hỏi (vd "[y/N] ") ngay trước JSON trên cùng dòng: giải mã từ mỗi "{" có "task_id" theo sau.
        res = None
        dec = json.JSONDecoder()
        for m in re.finditer(r"\{\s*\"task_id\"", out):
            try:
                res, _ = dec.raw_decode(out, m.start())
            except Exception:
                pass
        return res, how

    def requirement_path(self, tid):
        hits = glob.glob(os.path.join(self.state, "**", "tasks", tid, "requirement.md"), recursive=True)
        return hits[0] if hits else None

    def answer_open_questions(self, tid):
        p = self.requirement_path(tid)
        if not p:
            return False
        text = open(p, encoding="utf-8").read()
        new, n = re.subn(r"^([ \t]*)- \[ \] (Q-\d+:.*?)[ \t]*$",
                         lambda m: f"{m.group(1)}- [x] {m.group(2)} — Trả lời: {' '.join(ANSWER.splitlines())}",
                         text, flags=re.M)
        if not n:
            return False
        r = subprocess.run([BINT, "task", "write", tid, "requirement", "-"], cwd=self.repo, env=self.env(), input=new,
                           capture_output=True, text=True, encoding="utf-8", errors="replace")
        self.event("answer_questions", n=n, exit=r.returncode, tail=(r.stdout + r.stderr)[-300:])
        return r.returncode == 0

    def run_b(self):
        variant = "bprime" if self.arm == "Bp" else "classic"
        res, how = self.bint("b-do", ["run", "b-do", "--variant", variant, "--harness", "codex", "--model", MODEL,
                                      "--autonomy", "auto", "--shared", "--json", PROMPT])
        tid = (res or {}).get("task_id")
        self.rec["task_id"] = tid
        last_reason = None
        while True:
            st = (res or {}).get("task_status")
            reason = (res or {}).get("reason", "")
            self.event("status", status=st, reason=reason[:500], how=how,
                       choices=[c.get("command") for c in (res or {}).get("choices") or []])
            if how != "exit":
                self.rec["end"] = "cap"
                return
            if not tid:
                self.rec["end"] = "no_task"
                return
            if st == "done":
                self.rec["end"] = "done"
                return
            if st == "failed":
                self.rec["end"] = "failed"
                return
            if len(self.rec["user_actions"]) >= MAX_USER_ACTIONS:
                self.rec["end"] = "user_actions_exhausted"
                return
            cmds = " ".join(c.get("command", "") for c in (res or {}).get("choices") or [])
            args = ["run", "resume", tid, "--json"]
            act = None
            if reason.startswith("kế hoạch lớn"):
                act = "approve_large_plan"
            elif "--slice" in cmds:
                act, args = "slice_more", args + ["--slice", "more"]
            elif "--accept" in cmds:
                # hết vòng sửa hoặc kiểm chứng cuối đỏ: người dùng low-tech không quyết được, lượt kết thúc với cây hiện tại
                self.rec["end"] = "review_exhausted"
                return
            elif "--answer" in cmds or reason.startswith("b-feat hỏi") or "không đổi tệp nào" in reason:
                act, args = "answer_bfeat", args + ["--answer", ANSWER]
            elif (res or {}).get("questions") and self.answer_open_questions(tid):
                act = "answer_open_questions"
            elif "approve" in reason or "duyệt" in reason:
                r = subprocess.run([BINT, "task", "approve", tid], cwd=self.repo, env=self.env(), capture_output=True,
                                   text=True, encoding="utf-8", errors="replace")
                self.event("approve", exit=r.returncode, tail=(r.stdout + r.stderr)[-300:])
                act = "approve"
            elif reason == last_reason:
                self.rec["end"] = f"stuck:{st}"
                return
            else:
                act = f"resume_{st}"
            last_reason = reason
            self.rec["user_actions"].append({"t": self.now(), "action": act, "reason": reason[:300]})
            res, how = self.bint("resume", args)

    def main(self):
        try:
            self.setup()
            self.t0 = time.monotonic()
            self.deadline = self.t0 + self.cap
            self.rec["start"] = time.strftime("%Y-%m-%dT%H:%M:%S%z")
            self.event("started")
            {"A": self.run_a, "B": self.run_b, "Bp": self.run_b}[self.arm]()
        except Exception as e:
            self.rec["end"] = f"driver_error:{e!r}"
        self.rec["wall_s"] = self.now() if self.t0 else 0
        self.rec["finish"] = time.strftime("%Y-%m-%dT%H:%M:%S%z")
        self.rec["tokens"] = self.tokens()
        self.rec["cost_usd"] = None
        self.save()
        print(self.name, "END", self.rec["end"], self.rec["wall_s"], self.rec["tokens"], flush=True)


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser()
    ap.add_argument("arm", choices=["A", "B", "Bp"])
    ap.add_argument("k", type=int)
    ap.add_argument("--cap-min", type=float, default=180)
    a = ap.parse_args()
    Run(a.arm, a.k, a.cap_min).main()
