"""Chạy tiếp một lượt B/B′ đã kết thúc vì lỗi ngoại cảnh, với ngân sách thêm (giây). Ghi tiếp vào run.json (events, end).

python cont.py <run> <thêm_giây> <lý do>
"""
import json, os, sys, time
sys.path.insert(0, os.path.dirname(__file__))
import run as R

name, extra, why = sys.argv[1], float(sys.argv[2]), sys.argv[3]
arm, k = name.split("-")
r = R.Run(arm, int(k), 180, 0)
r.rec = json.load(open(os.path.join(r.dir, "run.json"), encoding="utf-8"))
prev_wall = r.rec["wall_s"]
r.t0 = time.monotonic() - prev_wall
r.deadline = time.monotonic() + extra
r.rec.setdefault("continuations", []).append({"at_wall_s": prev_wall, "extra_s": extra, "why": why, "prev_end": r.rec.get("end")})
r.rec.pop("end", None)
tid = r.rec["task_id"]
r.rec["user_actions"].append({"t": r.now(), "action": "resume_after_external_error", "reason": why})
res, how = r.bint("resume", ["run", "resume", tid, "--json"])
# Tái dùng vòng quyết định của run_b từ kết quả này.
orig = r.bint
first = [(res, how)]


def once(label, args):
    if first:
        return first.pop()
    return orig(label, args)


r.bint = once
try:
    r.run_b()
except Exception as e:
    r.rec["end"] = f"driver_error:{e!r}"
r.rec["wall_s"] = r.now()
r.rec["tokens"] = r.tokens()
r.save()
print(name, "END", r.rec["end"], r.rec["wall_s"], r.rec["tokens"], flush=True)
