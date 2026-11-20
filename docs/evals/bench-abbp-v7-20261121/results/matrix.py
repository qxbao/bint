"""Chấm ma trận loại bí mật × nơi lưu: ô = cặp lộ trên binary gốc; điểm = ô sạch / tổng ô."""
import json, sys
sys.stdout.reconfigure(encoding="utf-8")
base = json.load(open(sys.argv[1], encoding="utf-8"))
cells = {g: sorted(v) for g, v in base["leaks"].items() if v}
out = {"cells": cells, "total": sum(len(v) for v in cells.values())}
for path in sys.argv[2:]:
    r = json.load(open(path, encoding="utf-8"))
    leak = r["leaks"]
    per_g, clean = {}, 0
    for g, ts in cells.items():
        c = [t for t in ts if t not in leak.get(g, {})]
        per_g[g] = f"{len(c)}/{len(ts)}"
        clean += len(c)
    per_t = {}
    for g, ts in cells.items():
        for t in ts:
            a = per_t.setdefault(t, [0, 0]); a[1] += 1
            if t not in leak.get(g, {}): a[0] += 1
    extra = {g: sorted(set(leak.get(g, {})) - set(cells.get(g, []))) for g in leak if set(leak.get(g, {})) - set(cells.get(g, []))}
    res = {"clean": clean, "score_pct": round(100 * clean / out["total"], 1), "per_group": per_g,
           "per_type": {t: f"{a}/{b}" for t, (a, b) in per_t.items()}, "leaks_outside_cells": extra,
           "N1": r.get("N1", {}).get("pass"), "fixture": r.get("fixture"), "fixture_error": r.get("fixture_error")}
    out[path] = res
    print(path, f"{clean}/{out['total']} ({res['score_pct']}%)", per_g, "N1:", res["N1"])
json.dump(out, sys.stdout if len(sys.argv) < 3 else open(sys.argv[2] + ".matrix.json", "w", encoding="utf-8"), ensure_ascii=False, indent=1)
