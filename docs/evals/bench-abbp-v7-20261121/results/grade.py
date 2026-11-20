"""Chấm một lượt đợt 7: chụp cây (không đụng index), build bint, test ẩn L1 + ma trận, go test ./... của repo.

python grade.py <thư mục lượt> [--no-repo-tests]  →  <thư mục lượt>/grade.json
"""
import json, os, re, shutil, subprocess, sys, time

W = r"D:\bench\bench-v7-20261120"
STRIP = ("CLAUDE", "AI_AGENT", "BINT_", "CODEX_", "GEMINI_CLI", "ANTHROPIC_")


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    rd = os.path.abspath(sys.argv[1])
    repo_tests = "--no-repo-tests" not in sys.argv
    name = os.path.basename(rd)
    repo = os.path.join(rd, "repo")
    # đường dẫn ngắn (tránh "Filename too long" của test lồng repo trên Windows), mỗi lần chấm một thư mục mới
    gtmp = os.path.join("D:/bt", f"{name}-{int(time.time())}")
    os.makedirs(gtmp)
    env = {k: v for k, v in os.environ.items() if not k.startswith(STRIP)}
    env.update(TMP=gtmp, TEMP=gtmp, GIT_CONFIG_NOSYSTEM="1")
    idx = os.path.join(gtmp, "snapidx")
    genv = dict(env, GIT_INDEX_FILE=idx)
    subprocess.run(["git", "read-tree", "bench"], cwd=repo, env=genv, check=True, capture_output=True)
    subprocess.run(["git", "add", "-A"], cwd=repo, env=genv, check=True, capture_output=True)
    tree = subprocess.run(["git", "write-tree"], cwd=repo, env=genv, check=True, capture_output=True, text=True).stdout.strip()
    commit = subprocess.run(["git", "-c", "user.name=bench", "-c", "user.email=bench@local", "commit-tree", tree,
                             "-p", "bench", "-m", "bench v7 final"], cwd=repo, env=genv, check=True,
                            capture_output=True, text=True).stdout.strip()
    subprocess.run(["git", "update-ref", "refs/bench/final", commit], cwd=repo, env=env, check=True)
    exp = os.path.join(gtmp, "src")
    os.makedirs(exp)
    ar = subprocess.run(["git", "archive", "--format=tar", commit], cwd=repo, env=env, check=True, capture_output=True).stdout
    subprocess.run(["tar", "-xf", "-"], cwd=exp, input=ar, check=True)
    diffstat = subprocess.run(["git", "diff", "--shortstat", "bench", commit, "--", ".", ":!.bint", ":!.agents", ":!.claude", ":!.codex"],
                              cwd=repo, env=env, capture_output=True, text=True).stdout.strip()
    files = subprocess.run(["git", "diff", "--name-only", "bench", commit, "--", ".", ":!.bint", ":!.agents", ":!.claude", ":!.codex"],
                           cwd=repo, env=env, capture_output=True, text=True).stdout.split()
    exe = os.path.join(gtmp, "bint.exe")
    b = subprocess.run(["go", "build", "-o", exe, "./cmd/bint"], cwd=exp, env=env, capture_output=True, text=True)
    res = {"run": name, "commit": commit, "build_ok": b.returncode == 0, "build_err": (b.stdout + b.stderr)[-1500:],
           "diffstat": diffstat, "files_changed": len(files)}
    if b.returncode == 0:
        hj = os.path.join(rd, "hidden.json")
        h = subprocess.run([sys.executable, "-I", os.path.join(W, "hidden", "hidden_l1.py"), "--bint", exe,
                            "--work", os.path.join(gtmp, "hidden"), "--out", hj],
                           env=env, capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=3600)
        res["hidden_exit"] = h.returncode
        res["hidden_tail"] = (h.stdout[-1200:] + h.stderr[-600:])
        if os.path.exists(hj):
            m = subprocess.run([sys.executable, os.path.join(W, "tools", "matrix.py"),
                                os.path.join(W, "controls", "l1-base.json"), hj],
                               env=env, capture_output=True, text=True, encoding="utf-8")
            mj = hj + ".matrix.json"
            if os.path.exists(mj):
                mm = json.load(open(mj, encoding="utf-8"))
                r = mm.get(hj, {})
                res.update(hidden_clean=r.get("clean"), hidden_total=mm.get("total"), hidden_pct=r.get("score_pct"),
                           hidden_per_group=r.get("per_group"), hidden_per_type=r.get("per_type"), N1=r.get("N1"),
                           fixture_error=r.get("fixture_error"))
            else:
                res["matrix_err"] = m.stdout[-500:] + m.stderr[-500:]
        nj = os.path.join(rd, "n2.json")
        subprocess.run([sys.executable, "-I", os.path.join(W, "hidden", "hidden_l1.py"), "--bint", exe,
                        "--base-bint", os.path.join(W, "bin", "bint-base.exe"), "--work", os.path.join(gtmp, "n2"),
                        "--out", nj, "--n2-only"], env=env, capture_output=True, text=True, encoding="utf-8",
                       errors="replace", timeout=7200)
        if os.path.exists(nj):
            res["N2"] = json.load(open(nj, encoding="utf-8")).get("N2")
    if repo_tests and b.returncode == 0:
        log = os.path.join(rd, "gotest.txt")
        with open(log, "w", encoding="utf-8") as f:
            t = subprocess.run(["go", "test", "-count=1", "-timeout", "40m", "./..."], cwd=exp, env=env, stdout=f,
                               stderr=subprocess.STDOUT, timeout=4 * 3600)
        txt = open(log, encoding="utf-8", errors="replace").read()
        res["gotest_exit"] = t.returncode
        res["gotest_fail_pkgs"] = re.findall(r"^FAIL\s+(\S+)", txt, re.M)
        res["gotest_fail_tests"] = sorted(set(re.findall(r"^\s*--- FAIL: (\S+)", txt, re.M)))[:60]
    json.dump(res, open(os.path.join(rd, "grade.json"), "w", encoding="utf-8"), ensure_ascii=False, indent=1)
    print(json.dumps({k: res.get(k) for k in ("run", "build_ok", "diffstat", "hidden_clean", "hidden_total", "N1",
                                              "gotest_exit", "gotest_fail_pkgs")}, ensure_ascii=False))


if __name__ == "__main__":
    main()
