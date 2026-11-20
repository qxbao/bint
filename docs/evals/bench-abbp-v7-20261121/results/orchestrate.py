"""Chạy cả đợt 7 không dừng: pha 1 (B, B′), tính ngân sách A+, pha 2 (A, A+), chấm test ẩn ngay sau mỗi lượt,
cuối cùng chạy go test ./... cho mọi lượt. Ghi log/orchestrate.log và log/progress.json."""
import json, os, statistics, subprocess, sys, threading, time
from concurrent.futures import ThreadPoolExecutor

W = r"D:\bench\bench-v7-20261120"
PY = sys.executable
N = int(os.environ.get("BENCH_N", "5"))
POOL = int(os.environ.get("BENCH_POOL", "5"))
lock = threading.Lock()


def log(msg):
    line = time.strftime("%Y-%m-%dT%H:%M:%S ") + msg
    with lock:
        with open(os.path.join(W, "log", "orchestrate.log"), "a", encoding="utf-8") as f:
            f.write(line + "\n")
    print(line, flush=True)


def one(arm, k, target=0, delay=0):
    time.sleep(delay)
    name = f"{arm}-{k}"
    rd = os.path.join(W, "runs", name)
    if not os.path.exists(os.path.join(rd, "run.json")) or "end" not in json.load(open(os.path.join(rd, "run.json"), encoding="utf-8")):
        if os.path.exists(rd):
            log(f"{name}: đang chạy từ lần trước, chờ xong")
            while True:
                try:
                    if "end" in json.load(open(os.path.join(rd, "run.json"), encoding="utf-8")):
                        break
                except Exception:
                    pass
                time.sleep(30)
        else:
          log(f"{name}: bắt đầu")
          with open(os.path.join(W, "log", f"{name}.driver.txt"), "w", encoding="utf-8") as f:
            subprocess.run([PY, "-u", os.path.join(W, "tools", "run.py"), arm, str(k), "--target-tokens", str(target)],
                           stdout=f, stderr=subprocess.STDOUT)
    r = json.load(open(os.path.join(rd, "run.json"), encoding="utf-8"))
    log(f"{name}: xong end={r.get('end')} wall={r.get('wall_s')} tokens={r.get('tokens')}")
    if not os.path.exists(os.path.join(rd, "grade.json")):
        g = subprocess.run([PY, "-u", os.path.join(W, "tools", "grade.py"), rd, "--no-repo-tests"],
                           capture_output=True, text=True, encoding="utf-8", errors="replace")
        log(f"{name}: chấm {g.stdout.strip()[-400:]} {g.stderr.strip()[-300:]}")


def phase(jobs):
    with ThreadPoolExecutor(POOL) as ex:
        futs = [ex.submit(one, *j) for j in jobs]
        for f in futs:
            try:
                f.result()
            except Exception as e:
                log(f"lỗi luồng: {e!r}")


def interleave(a, b, target=0):
    jobs = []
    for k in range(1, N + 1):
        jobs += [(a, k, target), (b, k, target)]
    # giãn khởi động 90 giây trong nhóm đầu (tránh đụng mật khẩu sandbox Windows)
    return [(arm, k, t, 90 * i if i < POOL else 0) for i, (arm, k, t) in enumerate(jobs)]


def main():
    log(f"bắt đầu đợt 7: N={N} POOL={POOL}")
    phase(interleave("B", "Bp") + [("A", k, 0, 0) for k in range(1, N + 1)])
    toks = []
    for k in range(1, N + 1):
        p = os.path.join(W, "runs", f"B-{k}", "run.json")
        if os.path.exists(p):
            toks.append(json.load(open(p, encoding="utf-8"))["tokens"]["input"])
    target = int(statistics.median(toks)) if toks else 0
    json.dump({"B_input_tokens": toks, "target": target}, open(os.path.join(W, "log", "ap-target.json"), "w"), indent=1)
    log(f"ngân sách A+ = trung vị token vào của B = {target} (từ {toks})")
    if not os.environ.get("BENCH_SKIP_AP"):
        phase([("Ap", k, target, 0) for k in range(1, N + 1)])
    log("chạy go test ./... cho mọi lượt")
    runs = sorted(d for d in os.listdir(os.path.join(W, "runs")))

    def repo_test(name):
        rd = os.path.join(W, "runs", name)
        g = json.load(open(os.path.join(rd, "grade.json"), encoding="utf-8")) if os.path.exists(os.path.join(rd, "grade.json")) else {}
        if "gotest_exit" in g:
            return
        r = subprocess.run([PY, "-u", os.path.join(W, "tools", "grade.py"), rd], capture_output=True, text=True,
                           encoding="utf-8", errors="replace")
        log(f"{name}: chấm đủ {r.stdout.strip()[-400:]}")

    with ThreadPoolExecutor(3) as ex:
        list(ex.map(repo_test, runs))
    log("XONG đợt 7")


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    main()
