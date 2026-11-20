"""Tóm tắt tiến độ từng lượt (một khối mỗi lượt) để báo người dùng."""
import glob, json, os, re, subprocess, sys, time

W = r"D:\bench\bench-v7-20261120"
STRIP = ("CLAUDE", "AI_AGENT", "BINT_", "CODEX_", "GEMINI_CLI")


def tok(home):
    inp = out = 0
    for f in glob.glob(os.path.join(home, "sessions", "**", "*.jsonl"), recursive=True):
        last = None
        for line in open(f, encoding="utf-8", errors="replace"):
            if '"total_token_usage"' in line:
                try:
                    last = json.loads(line)["payload"]["info"]["total_token_usage"]
                except Exception:
                    pass
        if last:
            inp += last.get("input_tokens", 0)
            out += last.get("output_tokens", 0)
    return f"{inp/1e6:.1f}M vào/{out/1e3:.0f}k ra"


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    print(time.strftime("[%H:%M] tiến độ đợt 7"))
    for rd in sorted(glob.glob(os.path.join(W, "runs", "*"))):
        name = os.path.basename(rd)
        try:
            r = json.load(open(os.path.join(rd, "run.json"), encoding="utf-8"))
        except Exception:
            r = {}
        head = f"{name}: {tok(os.path.join(rd, 'codex-home'))}"
        if r.get("end"):
            g = {}
            try:
                g = json.load(open(os.path.join(rd, "grade.json"), encoding="utf-8"))
            except Exception:
                pass
            sc = f", test ẩn {g.get('hidden_clean')}/{g.get('hidden_total')}" if g else ", đang chấm"
            print(f"{head} | XONG ({r['end']}, {r.get('wall_s', 0)/60:.0f} phút{sc})")
            continue
        if name.startswith(("A-", "Ap-")):
            logs = sorted(glob.glob(os.path.join(rd, "log", "*.out")))
            n_cmd, last_msg = 0, ""
            if logs:
                for line in open(logs[-1], encoding="utf-8", errors="replace"):
                    if '"command_execution"' in line and '"item.completed"' in line:
                        n_cmd += 1
                    if '"agent_message"' in line and '"item.completed"' in line:
                        try:
                            last_msg = json.loads(line)["item"]["text"]
                        except Exception:
                            pass
            last_msg = re.sub(r"\s+", " ", last_msg)[:160]
            print(f"{head} | phiên {len(logs)}, {n_cmd} lệnh đã chạy | gần nhất: {last_msg}")
            continue
        tid = r.get("task_id") or next((re.search(r"01[0-9A-Z]{24}", e.get("reason", "") or "").group(0)
                                        for e in r.get("events", []) if re.search(r"01[0-9A-Z]{24}", e.get("reason", "") or "")), None)
        if not tid:
            for ev in r.get("events", []):
                m = re.search(r"tasks\\(01[0-9A-Z]{24})", ev.get("tail", "") or "")
                if m:
                    tid = m.group(1)
        if not tid:
            print(f"{head} | chưa có phiếu (b-req đang chạy)")
            continue
        env = {k: v for k, v in os.environ.items() if not k.startswith(STRIP)}
        env.update(BINT_STATE_DIR=os.path.join(rd, "state"), BINT_CONFIG_HOME=os.path.join(rd, "cfg"))
        s = subprocess.run([os.path.join(W, "bin", "bint.exe"), "run", "status", tid], cwd=os.path.join(rd, "repo"), env=env,
                           capture_output=True, text=True, encoding="utf-8", errors="replace").stdout
        lines = s.splitlines()
        rows = [l for l in lines if re.match(r"^01[0-9A-Z]{24}\s", l)]
        steps = []
        for l in rows:
            c = l.split()
            # RUN ID, WORKFLOW, CHA, TRẠNG THÁI, ngày, giờ, THỜI LƯỢNG ...
            if c[1] != "b-do":
                steps.append(f"{c[1]} {c[6] if c[3] == 'completed' else c[3]}")
        print(f"{head} | {lines[0].split(': ', 1)[-1] if lines else '?'} | các bước: {', '.join(steps[-6:])}")


if __name__ == "__main__":
    main()
