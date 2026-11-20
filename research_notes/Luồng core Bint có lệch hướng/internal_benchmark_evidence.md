# Internal benchmark evidence: Bint (`bint run b-do`) vs plain prompt, rounds 1–6

Legend for every bullet under "Cited Findings":
- **[R]** = stated in a report/plan file (not re-checked by me).
- **[V]** = I verified it myself in a raw artefact (file named in the bullet).
- Inferences are only in the "Inferences" subsections.

Path shorthands (all local):
- `EV` = `D:\Projects\bint\docs\evals`
- `SUM` = `EV\bench-bint-vs-plain-tong-hop.md`; `CHG` = `EV\bench-bint-vs-plain-thay-doi.md`
- `R1` = `EV\bench-bint-vs-plain-20261116\README.md`, `R2` = `EV\bench-bint-vs-plain-v2-20261116\README.md`, `R3` = `EV\bench-bint-vs-plain-v3-20261117\README.md`, `R4` = `EV\bench-bint-vs-plain-v4-20261117\README.md`, `R5` = `EV\bench-bint-vs-plain-v5-20261117\README.md`, `R6` = `EV\bench-bint-vs-plain-v6-20261120\README.md`
- `P6` = `D:\Projects\bint\docs\benchmark-bint-vs-plain-v6-plan.md`
- `WS` = `D:\bench\bench-bint-v6-20261120` (round-6 raw workspace)
- `TASK` = `WS\state-B\B-L1-3f58690cab85\runtime\tasks\01M7TTED76VJBDNNWDZJ5N5AXD`
- `GOAL` = `WS\B-L1\internal\cli\secret_persistence_e2e_test.go` (the frozen goal test, 1128 lines)

Times in round-6 raw files: `state.json` is UTC (`Z`); `log\*.segments.jsonl` is local time (UTC+7). "Budget minute" = cumulative agent time over segments (cap 4 × 30 = 120 min).

---

## 1. Per-round scoreboard (tasks, model, verdict, quality A vs B, token ratio, wall time, how B ended)

### Takeaway
Across six rounds B (Bint) never beat A on the pre-registered rule except round 2 (by one critical, single run); verdicts were: no signal, benefit-with-trade-off, disadvantage ×3, tie. B always cost more input tokens (1.5× to 11.3×) and in rounds 3–6 the hard task's B run never reached a finished/green state inside the cap.

### Cited Findings
- [R] Summary table of rounds 1–5 (verdict, C/M counts, token ratio, time) — `SUM` §1 lines 21–27. Thresholds: "costly" = 2× (round 2), 2.5× (rounds 3–5); from round 3 B must have fewer criticals, or equal criticals and ≥ 2 fewer mediums — `SUM` line 29.
- **Round 1 (2026-11-16, Linux, `gpt-6.1-sol`, technical user, two tasks "mid" GĐ3 and "big" GĐ2a+2b).** [R] Verdict "Không có tín hiệu". Hidden tests A/B: mid 12/12 vs 12/12; big 11/12 vs 10/12. Blind grade (of 20): mid A 14 / B 17; big A 15 / B 13. Input tokens B vs A: +51 % / +48 %; output +123 % / +52 %; wall +90 % / +55 %; interventions A/B 1/2 and 0/2 — `R1` lines 8–14. Raw tokens: A-mid 5,636,649; B-mid 8,505,144; A-big 11,322,887; B-big 16,762,557 — `R1` lines 58–63. Wall: A-mid 35m46, A-big 38m54, B-mid 67m52, B-big 60m26; both B runs ended `done` — `R1` lines 70–75.
- **Round 2 (2026-11-16, Linux, `gpt-6.1-sol`, vague low-tech user; T1 "dọn dữ liệu cũ", T2 "không lưu bí mật").** [R] Verdict "Có lợi, đánh đổi". T1 A 0C/3M vs B 0C/2M (tie); T2 A 6C/3M vs B 5C/5M (B better by one critical). Input tokens ×2.07 (T1), ×2.46 (T2); output ×2.4 / ×3.2; wall ×3.5 / ×4.7 — `R2` lines 8–25, 115–126. Runs: A-T1 7.57M / 19 min; A-T2 6.91M / 18 min; B-T1 15.64M / 68 min; B-T2 16.97M / 85.5 min, ended **blocked** ("còn 1 phát hiện nghiêm trọng… cần người xem review.md") with no exit command for the user — `R2` lines 117–120, 147–154.
- **Round 3 (2026-11-17, Linux, `gpt-6.1-sol`, same T1/T2, base `8757340`).** [R] Verdict "Bint bất lợi". T1 A 0C/1M vs B 0C/3M; T2 A 5C/2M vs B 6C/7M. Input tokens A total 14.70M vs B 165.69M = **11.3×** (B-T1 95.70M in 10 sessions; B-T2 69.99M in 8 sessions). A wall ≈ 22 / 19 min; both B runs hit the 90-minute cap (timeout) in the middle of fix round 3, tickets `interrupted` — `R3` lines 9–22, 32–41. Post-hoc correction: B's red `go test` was caused by the cap cutting mid-edit; without it T1 becomes a tie (0C/2M vs 0C/1M) — `R3` line 134.
- **Round 4 (2026-11-17, B only re-run on Bint `72aa43c`; A = round-3 diff re-graded).** [R] Verdict still "Bint bất lợi" (near threshold). T1 A 0C/2M vs B 0C/4M; T2 A 7C/1M vs B 8C/2M. B 36.95M vs A 14.70M = **2.51×** (B-T1 23.52M, B-T2 13.43M). B-T1 finished after 88.5 min (review round 4: 0 findings, `go test` green); B-T2 self-stopped on "không hội tụ" (4 high → 4 high), user-sim sent `--fix-round`, then hit the 90-min cap (exit 124) — `R4` lines 10–17, 28–41, 46–58.
- **Round 5 (2026-11-17, B only re-run on Bint `9095646`).** [R] Verdict "Bint bất lợi" (T1 tie, T2 A better). T1 A 0C/3M vs B 0C/2M; T2 A 7C/1M vs B 10C/6M. B 72.60M vs A 14.70M = **4.94×** (B-T1 25.50M / 5 sessions; B-T2 47.10M / 2 sessions). B-T1 ≈ 77 min, done with 0 findings at review round 4; B-T2 hit the 90-min cap in its first fix round, `last_green = None`, never verified green — `R5` lines 10–19, 29–46.
- **Round 6 (2026-11-20, Windows 11, `gpt-6.2-sol`, one long task L1 = T2 reworded, max 4 sessions × 30 min with one hard kill at minute 15 of session 2).** [R] Verdict "Hoà". Matrix (secret type × storage location, hidden test): A **22/36 (61.1 %)**, B **20/36 (55.6 %)**; N2 (20 MB verify log, limit 3×): A fails 20.1× (17.9 s vs 0.89 s), B fails 5.5× (4.7 s vs 0.86 s); verified criticals outside "still leaks": A 0 (3 medium, 1 low), B 3 (Y1–Y3) + 6 medium; input tokens A 37.4M vs B 90.0M = **2.41×**; A declared done after 3 sessions (≈ 84 agent-minutes); B used all 4 sessions (120 min), stopped mid fix round with 10 open high findings and never had a green checkpoint — `R6` lines 8–21.
- [V] Round-6 numbers confirmed in raw files: `WS\results\tokens.json` (A input 37,368,790, cached 36,533,504, 5 sessions; B input 89,992,139, cached 88,472,960, 5 sessions; ratio 2.41); `WS\results\A-L1\seg3.hidden.json.matrix.json` (clean 22, per_group S1 7/7, S2 7/7, S3 0/7, S4 7/7, S5 1/1, S6 0/7); `WS\results\B-L1\seg4.hidden.json.matrix.json` (clean 20, per_group S1–S4 and S6 4/7 each, S5 0/1; per_type aws_key_id 0/5, aws_secret 0/5, private_key 0/6, bearer/github_token/password/url_creds 5/5); `WS\results\A-L1\n2.json` (cand 18.24/17.89/17.07 s, base 0.92/0.89/0.82 s, ratio 20.11, pass false); `WS\results\B-L1\n2.json` (cand 4.73/4.61/4.82 s, ratio 5.5, pass false).
- [V] Segment durations: A = 1800.7 s (soft timeout) + 901.7 s (hard cut) + 901.0 s (soft timeout) + 1427.3 s (exit 0) = 5030.7 s ≈ 83.8 min — `WS\log\A-L1.segments.jsonl`. B = 330.1 + 1472.1 + 900.7 (hard cut) + 902.6 + 1105.2 + 697.2 + 1801.5 = 7209.4 s ≈ 120.2 min — `WS\log\B-L1.segments.jsonl`.
- [V] Final diff size round 6: A "65 files changed, 2120 insertions(+), 228 deletions(-)" (`WS\results\A-L1\seg3.json`); B "63 files changed, 3232 insertions(+), 209 deletions(-)" (`WS\results\B-L1\seg4.json`).

### Inferences
- The token ratio tracks the size of the b-req plan rather than any quality outcome: small plan → 2.1–2.5× (rounds 2, 4, 6-with-b-goal), sprawling plan → 4.9–11.3× (rounds 5, 3). `SUM` line 35 states the same.
- Only two of the ten B runs on the vague-user tasks (rounds 2–6) ended in a clean "done with 0 open findings" state: B-T1 in rounds 4 and 5 (both the easier task, both after the user-sim pressed `--fix-round`).

### Gaps
- Round 2 README gives no per-step timing for B (only one `start/end` line in `EV\bench-bint-vs-plain-v2-20261116\log\timeline-B-T2.txt`), and reports token ratio per task only.
- Rounds 4–5 give no A re-run; A numbers are the round-3 diff re-graded by a different reviewer each time.

---

## 2. Time and token breakdown per Bint step; share of budget before first product code and before first verification

### Takeaway
In round 6, 21 % of B's 120-minute budget elapsed before the first line of product code (b-req 5.5 min + b-goal 19.2 min) and 78 % before Bint's first verification started; the first verification result arrived at ≈ 90 % and the first review result at ≈ 97 % of budget. b-feat itself consumed ≈ 68 min (my count from raw logs; the README says ≈ 55).

### Cited Findings
Round 6 (raw):
- [V] Step durations printed by Bint: b-req 5m28s (`WS\log\B-L1.seg1.1.out`); b-goal 19m11s, then b-feat 4m24s interrupted (`B-L1.seg1.2.out`); b-feat hard-cut after ≈ 15 min (`B-L1.seg2.1.out`, segment elapsed 900.7 s); b-feat 15m0s interrupted (`B-L1.seg2.2.out`); b-feat 18m13s ending in a question to the user (`B-L1.seg3.1.out`); b-feat 11m28s interrupted (`B-L1.seg3.2.out`); b-feat 3m46s completed, verification, goal gate, b-review 8m23s (12 findings, 10 serious), fix b-feat 2m37s interrupted (`B-L1.seg4.1.out`).
- [V] State transitions (UTC) in `TASK\state.json` → `history`: created 21:12:18; b-req done 21:17:47; blocked on large-plan gate 21:17:47 ("55 tệp sẽ sửa, vượt ngưỡng workflow.small_plan_max_files = 5"); resumed 21:17:57; **b-feat begins 21:38:03**; `in_review` 23:04:26; fix round begins 23:12:51 ("sửa theo review vòng 1 (10 phát hiện nghiêm trọng; vòng sửa 1/2)"); interrupted 23:15:30.
- [V] `TASK\state.json` → `goal_test.at` = 21:37:10Z (frozen file hash recorded), `goal_checks[0]` 21:38:02Z failed ("mốc sau b-goal"), `verifications[0].at` = 23:03:56Z exit 1, `goal_checks[1]` 23:04:26Z passed.
- [V] Token usage per step from `turn.completed` events in `TASK\logs\*.log`: b-req input 945,789 (cached 850,048); b-goal input 11,940,671 (cached 11,731,072), output 36,584; b-review input 2,633,370; b-feat session `01a1f5d1…` cumulative 36,108,179 at end of the seg3.1 run and 56,557,996 at end of the last pre-review b-feat; `state.json` → `last_step.usage` for that same session = 60,196,346 input (cache read 59,450,112) at the end.
- [R] README's own breakdown: "b-req 5,5 phút · cổng kế hoạch lớn · b-goal 19 phút · b-feat cộng dồn ≈ 55 phút (5 lần, 2 lần bị cắt) · kiểm chứng `go test ./...` ≈ 15 phút · b-review 8,5 phút · sửa 2,6 phút. Hơn 1/3 thời gian của B đi vào b-goal và kiểm chứng toàn repo trước khi có vòng sửa đầu" — `R6` §5 lines 71–73.

Earlier rounds (reports only):
- [R] Round 1: B-mid = b-req 6m06 → wait for human (Q-1) → b-req 1m30 → b-feat 14m56 → verify → b-review 4m47 (0 findings) → full verify red (environment) + artifact-contract block → wait 7m22 → b-feat 2m24 → b-review 4m14 (0 findings) → full verify green → done (67m52 total). B-big = b-req 8m37 → b-feat 32m41 → full verify red (environment) → b-review 7m38 (3 findings, 0 serious) → blocked "tiêu chí 8 chưa đạt" → b-review 5m17 → done (60m26) — `R1` lines 74–75.
- [R] Round 3: b-req ≈ 2.3M / 1.7M tokens (6m43s each run); b-feat main session 44.1M / 25.6M plus 3 self-spawned parallel child sessions 27.3M / 32.0M ≈ 75 % / 82 % of total; each b-review ≈ 2–5M; first b-feat 42 min (T1) / 34 min (T2), fix round 1 12 / 14 min — `R3` lines 42–44. T1 timeline: "b-req 7 phút, b-feat 47, review 9, sửa 1 16, review 8, sửa 2 bị cắt ở phút 90" — `R3` line 140.
- [R] Round 4: first b-feat ≈ 30 / 28 min; plans 11.4 KB (5 AC, 5 TSK) and 10.9 KB (4 AC, 4 TSK); Codex sessions dropped 10→6 and 8→4 — `R4` lines 37–41, 71–73.
- [R] Round 5: B-T1 = b-req 1m32s → 2 open questions → b-req 3m03s → large-plan gate (15 files, 8 surface groups) → b-feat 20m02s → fix 10m36s → fix 7m41s → `--fix-round` → b-feat 2m24s → review round 4: 0 findings. B-T2 = b-req 1m59s + 3m46s + 1m27s (three question rounds) → large-plan gate (34 files, 8 groups) → **b-feat 47m06s** → question → b-feat 11m25s → verification red → review round 1: 6 high → fix 13m10s → cut at 90 min — `R5` lines 41–45.

### Inferences
- Round-6 budget arithmetic (from the [V] numbers above): first product-code edit possible at budget minute 330.1 s + (21:38:03 − 21:17:57 = 1206 s) = 1536 s ≈ **25.6 min = 21 %** of 120. First Bint verification starts after segments 1–3 (5408 s) + 3m46s ≈ **93.9 min = 78 %**; its result lands ≈ 108.6 min = 90 %; review result ≈ 117 min = 97 %; only ≈ 2.6 min of fixing followed.
- Summing the per-run b-feat durations gives ≈ 4.4 + 15.0 + 15.0 + 18.2 + 11.5 + 3.8 = **≈ 68 min (57 % of budget)** over 6 pre-review runs, plus the 2.6 min fix run. This reconciles with the 120.2 min total (5.5 + 19.2 + 68 + 14.7 verify + ≈ 1.4 goal checks + 8.4 + 2.6 ≈ 119.8). The README's "≈ 55 phút (5 lần)" understates it by ≈ 13 min.
- Token shares in round 6: b-goal ≈ 13 % (11.94M / 89.99M); b-review ≈ 3 %; b-req ≈ 1 %; the b-feat session `01a1f5d1` alone ≈ 67 % (60.2M). The remaining ≈ 15.2M is most likely the first session `01a1f5a7` (b-req + the first two b-feat runs, which resumed the b-req session) — not measured directly.
- Round 5 B-T2: first verification came after ≈ 7.2 min b-req + 47.1 + 11.4 min b-feat ≈ 65.7 min ≈ 73 % of the 90-minute cap (computed from `R5` line 43–44).

### Gaps
- No per-step token split exists for rounds 2, 4, 5 (the token tool takes the max cumulative value per session; `R3` line 44 explains fix rounds are folded into the b-feat session).
- Round 3's first-b-feat duration is reported inconsistently inside the reports: 42 / 34 min (`R3` line 44, H4 line 95), 47 min for T1 (`R3` line 140), and "lần 3: 47 / 40" (`R4` line 38).

---

## 3. Round 6 raw-artefact checks (the six specific questions)

### Takeaway
The frozen goal test planted one synthetic canary shape under label-style contexts (password=/token=/secret=/API key/Bearer/URL userinfo) across a very wide set of storage and copy surfaces, but contained no AWS key ID, no `aws_secret_access_key`, and no PEM private key; B's final hidden-test result is exactly "the label shapes the goal test planted are clean at every location, the three shapes it did not plant leak at every location". b-feat's transcript shows it iterating against that frozen test from its first message; the test went green at about budget minute 47 and the hidden score did not move from 20/36 between minute 45 and minute 120.

### Cited Findings

**(1) Frozen goal test coverage vs the hidden 7 types × 6 locations**
- [V] Hidden test types and locations: `TYPES = ["github_token", "aws_key_id", "aws_secret", "bearer", "url_creds", "password", "private_key"]`; groups S1 verify-output, S2 agent-output, S3 task-description, S4 review, S5 hook-events, S6 task-docs; full fixture strings are `GITHUB_TOKEN=<gh-prefixed token>`, `AWS_ACCESS_KEY_ID=<AKIA-prefixed id>`, `aws_secret_access_key=<40 chars>`, `Authorization: Bearer <JWT-like>`, `https://benchuser:<pw>@git.example.com/repo.git`, `password=<pw>`, `-----BEGIN PRIVATE KEY-----…` — `WS\hidden\hidden_l1.py` (definitions near `TYPES`, `GROUP_NAME`, `secrets_for`).
- [R] Matrix = 36 cells (S1–S4, S6: 7 types each; S5: `private_key` only, because the baseline hook already masks the other six) — `R6` line 83; `WS\log\prep-summary.md`.
- [V] The goal test uses a single canary generator: `persistenceCanary(group)` returns `"synthetic-bint-" + group + "-Zq8W3mX9<>&"` (`GOAL` lines 39–41), plus one "short" pattern for `password … 1` (`GOAL` line 43). Case-insensitive grep counts in `GOAL`: `AKIA` 0, `PRIVATE KEY` 0, `aws` 0, `ghp_` 0, `github_pat` 0, `sk-` 0; the only `BEGIN` is `execSQL("BEGIN IMMEDIATE")` at line 1073.
- [V] Contexts in which the canary is planted (one each): `password=`, `password:`, `--password=`, `passwd=`, `token=`, `secret=`, `--secret=`, `API key:`, `API_KEY=`, `FIXTURE_MCP_TOKEN=`, `Authorization: Bearer`, URL userinfo via `url.UserPassword("fixture", canary)` — `GOAL` lines 274, 315–316, 358, 459, 482, 534–545, 564–572, 592, 645, 662, 670, 684–688, 703, 717, 993–1011, 1072–1082.
- [V] Storage surfaces the goal test scans go far beyond the six measured ones: four subtests `runtime`, `metadata`, `copies`, `legacy` (`GOAL` lines 256–261) covering hook trace, spool, agent/verify logs, `prompt.txt`, eval cases, MCP shim output and tools cache, `gateway.json`, tickets/criteria/requirement docs/review, knowledge items, generated backups, MCP machine registration, imported iso profile (`auth.json`, `config.toml`), worktree copy of a git-ignored file, legacy files (hook-errors.log, spool pending/rejected, backups, legacy profile, gateway log/cache, ledger), SQLite rows and raw DB/WAL bytes (`GOAL` lines 263–330, 561–606, 653–728, 878–990, 1054–1107; first failing run listing in `TASK\logs\goal-1.log`).
- [V] B's redactor has no pattern for the three missed types: `WS\B-L1\internal\redact\redact.go` lines 23–27 define only `assignment` (key list + `:`/`=`), `flagValue`, `bearer`, `urlAuth`, `reference`; `CredentialName` (lines 32–40) accepts names equal to or ending in `_password/_passwd/_token/_secret/_api_key/_apikey/_credential…` — so `AWS_ACCESS_KEY_ID` and `aws_secret_access_key` (ending `_id` / `_key`) do not match. `WS\B-L1\internal\redact\redact_test.go` has 0 lines matching `aws|akia|private key|ghp_`.
- [V] By contrast A's redactor contains explicit shape patterns: `knownToken` (gh*_ / github_pat_ / sk- / npm_ / hf_ / glpat- …) and `privateKey` PEM regexes — `WS\A-L1\internal\redact\redact.go` lines 43–46; A is 7/7 on every location it covered (`WS\results\A-L1\seg3.hidden.json.matrix.json`).
- [V] B final leaks: `aws_key_id`, `aws_secret`, `private_key` at S1, S2, S3, S4, S6 and `private_key` at S5 — `hidden_tail` in `WS\results\B-L1\seg4.json`.
- [V] No command or command output in the b-req, b-goal or b-feat logs mentions `AKIA`, `BEGIN … PRIVATE KEY`, `aws_secret`, `ghp_`, `github_pat` or `private key`, except one incidental `aws_secret` string hit while b-feat inspected the Codex binary for auth options (`TASK\logs\b-feat-01M7TZ32F9AZMFP0SHEB74B92V.log`) — my scan of all `TASK\logs\b-*.log`.
- [V] The requirement written by b-req itself scopes detection to labelled fields: "Nhận dạng ít nhất trường password/passwd/token/API key/secret trong văn bản có nhãn, đối số, biến môi trường, JSON/YAML, Authorization Bearer, URL có thông tin xác thực… Chuỗi tùy ý không nhãn và chưa biết giá trị không thể tự nhận diện đáng tin cậy." — `TASK\requirement.md` line 9; same in plan "Giả định thay người dùng" (`TASK\plan.md` line 78).
- [V] The frozen file was not modified: sha256 of `GOAL` on disk = `a476df91735a34633bf85c041f6f00d3da3af190a1e32ffbb0e0d45a624beac0` = `state.json` → `goal_test.files[0].sha256`; the file is listed under `scope.deny` in `TASK\task.yml` line 93.

**(2) Size of requirement / plan / scope.allow vs the literal prompt**
- [V] Prompt: "Tôi sợ mật khẩu với token bị lưu lại trong log của bint, xử lý giúp tôi. Mọi chỗ bint lưu đều phải sạch, việc này lớn nên cứ làm từ từ qua nhiều lần cũng được." — `WS\log\prompt-L1.txt`.
- [V] Requirement goal expands this to "mọi nơi Bint quản lý. Bao gồm dữ liệu ghi mới, dữ liệu đã lưu và các bản Bint sao chép, đồng bộ hay xuất", with FR-1 "trước mọi đường lưu/hiển thị/phát hành", FR-2 passing credentials via external sources/references, FR-3 a new `bint scrub --dry-run/--apply` command; 5 acceptance criteria — `TASK\requirement.md` lines 3, 9–11, 14–18.
- [V] Plan "Cỡ kế hoạch: Kết luận: lớn" lists **55 files** (`TASK\plan.md` lines 14–68), including new gateway token transport over Unix socket / Windows named pipe (`internal/mcpgw/auth_unix.go`, `auth_windows.go`), profile credential sourcing (`internal/iso/*`), worktree/codegraph copy filtering, SQLite maintenance, a new CLI command. The gate message confirms "55 tệp sẽ sửa, vượt ngưỡng workflow.small_plan_max_files = 5" (`WS\log\B-L1.seg1.1.out`).
- [V] `scope.allow` in `TASK\task.yml` lines 16–89 has **74 entries** (55 from the plan + 19 added during b-feat; 4 of them via the scope-expansion question answered "tôi không rành, bạn tự quyết cho an toàn." — `state.json` → `questions[0]`; `WS\log\user-actions.md`). `state.json` → `touched` lists 51 files.
- [V] tasks.md has 5 work items (TSK-1…TSK-5), each "done when" a named subtest of `TestBintSecretPersistence` is green — `TASK\tasks.md` lines 3–7.
- [V] Plan execution strategy: "Chia lát: không — cấu hình hiện hành có slice_timeout_minutes bằng 0; b-feat có thể làm và kiểm từng nhóm theo thứ tự dưới đây trên cùng phiếu." — `TASK\plan.md` line 9.

**(3) Where the verified criticals Y1–Y3 came from, and whether those surfaces are in the measured matrix**
- [V] Y1: `workflow.worktree.copy` redacts file content copied into a worktree (`.env` → `DB_PASSWORD=REDACTED`, YAML `auth: false` → `auth: REDACTED` as string) — `WS\review\Y-verified.json` (Y1, citing `workflow/worktree.go:320-343`). The frozen goal test plants `password: <canary>` in a git-ignored file listed in `workflow.worktree.copy`, runs a worktree task, requires the control marker to exist under `state/worktrees` and then `g.scanAll()` requires no canary there — `GOAL` lines 695–726. Acceptance criterion AC-3 names "bản sao profile/worktree" (`TASK\requirement.md` line 16) and the plan lists `internal/workflow/worktree.go — không tạo bản sao thô của dữ liệu bí mật` (`TASK\plan.md` line 57).
- [V] Y2: verify commands with valid references (`Bearer ${API_TOKEN}`, `$(gh auth token)`, `${X:-d}`) are treated as secrets; new ticket rejected, existing ticket silently loses `verify.command` and approval — `WS\review\Y-verified.json` (Y2, `task/task.go:173-178, 902, 974-978, 1068-1079`). Source in the plan/goal test: FR-2 "nếu gặp lệnh nhúng bí mật mà không thể chuyển an toàn, hướng dẫn dùng tham chiếu" (`TASK\requirement.md` line 10); plan line 4 "`Verify.Command` lưu tham chiếu"; goal test `persistenceCommandApproval` plants an inline `--password=<canary>` verify command and accepts rejection if the message explains references (`GOAL` lines 645–651).
- [V] Y3: `bint scrub --apply` overwrites the repo's `.mcp.json`, iso profiles and machine `mcp.yml` with no backup; `"Bearer ${GITHUB_PAT}"` becomes `"Bearer REDACTED}"` — `WS\review\Y-verified.json` (Y3, `app/scrub.go:73-78, 178-222`). Source: FR-3 / AC-4 (`TASK\requirement.md` lines 11, 17) and the goal test's `legacy` subtest, which seeds legacy profile `config.toml`, backups, gateway log/cache etc. and requires `scrub --apply` to leave 0 canaries and a second apply to report 0 repairs (`GOAL` lines 878–990; failing lines 976, 987 in `TASK\logs\goal-1.log`).
- [R] The measured matrix is only S1–S6 (verify log, agent log, task description, review, hook events/SQLite, task docs) — `P6` §6.1 lines 78–86. Review-blind scope excluded "still leaks" findings and looked only for blocking defects — `WS\review\risk-spec.md`.
- [V] N1 (over-redaction probe of the hidden test: `TOKEN=$CI_TOKEN sh ok.sh`) passed for B (`hidden_tail` in `WS\results\B-L1\seg4.json`: "N1: True … verify_out: 'TOKEN=$CI_TOKEN sh ok.sh → qua (mã 0)'"); Y-verified notes `$CI_TOKEN` and `${CI_TOKEN}` are not altered, only `${VAR` inside Bearer, `$(…)` and `${X:-d}` forms.

**(4) What the verification command actually ran and how long it took**
- [V] Configured command: `go test -timeout 30m ./...` (`TASK\task.yml` line 7). What Bint ran: `scope: "package"`, i.e. `go test -timeout 30m` over 42 listed packages (`./internal/affected … ./internal/workflow`) — `state.json` → `verifications[0].ran`; header of `TASK\logs\verify-1.log`.
- [V] Result: exit 1. Failures: `TestMCPShimIdentityToSpool` (5 subtests, "shim tool missing … tools:[]"), `TestRepoWritesGoThroughRepoWriter` ("scrub.go:scrubTextFile gọi fsafe.WriteAtomic thẳng"), `TestEvalRunJobPlanBeforeConfirmAndDeclineCreatesNoBatch` (git "Filename too long" in a deep TEMP path), `TestNoVietnameseLiteralsInTUICode` (`actions\cliref.go:38`), `TestWorktreeBaseHasDirtyStateAndCopies` (".env trong worktree = \"TOKEN=REDACTED\\n\", mong \"TOKEN=1\\n\""). Slowest packages: `internal/workflow` 851.7 s, `internal/cli` 813.0 s, `internal/tui/app` 491.2 s, `internal/eval` 430.0 s, `internal/tui/actions` 412.0 s, `internal/evolve` 396.1 s, `internal/app` 317.1 s, `internal/config` 300.8 s — `TASK\logs\verify-1.log`.
- [V] Wall time ≈ 14 min 40 s: the last b-feat (3m46s) started with segment 4 at 05:45:29 local (`WS\log\B-L1.segments.jsonl`), i.e. ended ≈ 22:49:15Z; verification recorded 23:03:56Z (`state.json`). `tasks.md` artifact timestamp 22:49:06Z is consistent.
- [V] The goal gate afterwards ran `go test -run "^(TestBintSecretPersistence|TestSecretPersistenceFakeUpstream)$" -timeout 30m ./internal/cli` → `ok bint/internal/cli 26.794s` — `TASK\logs\goal-2.log`; `state.json` → `goal_checks[1]` passed at 23:04:26Z on worktree hash `5bbf20a7…`, the same hash as the red verification.
- [V] b-feat never ran the full-repo test itself: 0 `go test … ./...` invocations in any of the seven b-feat logs (my scan of `TASK\logs\b-feat-*.log`); it ran narrow `-run` selections only.
- [R] On Windows `go test ./...` takes ≈ 10–15 min, mostly git spawn and Defender; plan flagged in advance that this could eat most of a 30-min B session — `R6` line 89; `P6` §8 line 127.

**(5) What b-feat did between minute 45 and the end, while the score stayed 20/36**
- [V] Score path for B: 8/36 after session 1; 20/36 at the hard-cut snapshot (budget minute ≈ 45); 20/36 after sessions 2, 3 and 4 — `WS\results\B-L1\seg1/seg2cut/seg2/seg3/seg4.hidden.json.matrix.json`. Diff at the hard cut was 33 files / +2409 lines (`seg2cut.json`), at the end 63 files / +3232 lines (`seg4.json`): 30 more files, 0 more clean cells.
- [V] Session after the hard cut (`b-feat-01M7TX3N7G59K0FXKQMS2VYDJZ.log`, 15m0s): first test command is the full frozen goal test, exit 0; agent message: "Kiểm tra mục tiêu đóng băng đã qua cả bốn nhóm trên bản tôi nhận từ lượt trước. Kiểm thử hẹp lại phát hiện `TestImportClaude` vẫn yêu cầu sao chép tệp xác thực dạng rõ. Tôi đang xử lý cách cấp credential từ nguồn ngoài…". Files edited: `eval/db.go`, `evolve/evaluate.go`, `evolve/proposal.go`, `workflow/run.go`, `eval/shell.go`, `eval/codex_profile.go`, `eval/codex_restricted.go`, `iso/import.go`, `iso/session.go`, `redact/redact.go`, `mcpgw/env.go`, `mcpgw/stdio.go`, `store/store.go`, `task/ledger.go`, `workflow/writes.go`.
- [V] Next run (`b-feat-01M7TY0H9FHE17Z49KMHRPAS0E.log`, 18m13s): SQLite scrub marker for interrupted re-runs, CodeGraph index filtering before the worktree copy, gateway tests, git pathspec fix for ledger snapshot (`TestWorktreeSnapshotLedgerIgnored`); 10 `go test` commands, the goal test green 4 times; ends by asking the user to extend `scope.allow` by 4 files (TUI legacy-document read, Codex OAuth, two profile tests). It reported criteria 4–5 met and 1–3 "còn mở".
- [V] Next run (`b-feat-01M7TZ32F9AZMFP0SHEB74B92V.log`, 11m28s): filter on the TUI legacy-document read path and spool ingest; a live probe of Codex 0.170 to decide how to pass OAuth credentials ("`CODEX_ACCESS_TOKEN` dành cho agent identity JWT… dùng cấu hình provider chỉ chứa tên biến môi trường"); edits to `iso/session.go`, `eval/agent_harness.go`, `eval/codex_profile*.go`, `iso/import_test.go`, `runlog/*`.
- [V] Last pre-review run (`b-feat-01M7TZS1BENBAYV0700C1YZMA6.log`, 3m46s): regression re-runs, two test-file edits, then declares all 5 criteria met ("Bint đã ghi nhận đủ 5 tiêu chí đạt"). `state.json` → `criteria` entries at 22:48:19Z each cite a `TestBintSecretPersistence/<subtest>` pass as evidence.
- [V] Then ≈ 14.7 min verification (red), goal gate (green), b-review 8m23s (10 high, 2 medium), fix run 2m37s editing `mcpgw/partition.go`, `mcpgw/stdio.go`, `redact/redact.go`, `tui/actions/cliref.go`, `tui/actions/tasks.go`, cut by the cap (`b-feat-01M7V1B5H2PA3DQE06QRQDEQ5J.log`; `WS\log\B-L1.seg4.1.out`).
- [V] `state.json` → `errors` records what blocked b-feat along the way: Go build cache permission in sandbox (21:53:35Z), `TestImportClaude` expecting a credential copy (22:06:19Z), pathspec/ledger test and staticcheck cache denied by sandbox (22:29:25Z), Codex 0.170 OAuth probe (22:42:03Z), legacy SQLite read path (22:44:49Z).

**(6) Does b-feat's transcript show it coding to satisfy the frozen goal test?**
- [V] First b-feat message: "Tôi sẽ dùng skill `b-feat` trong phiên này, đọc kiểm tra mục tiêu đã đóng băng rồi triển khai và chạy lại từng nhóm kiểm thử." — `TASK\logs\b-feat-01M7TVXJBGV1Z2X0TX0SDKWMFE.log`.
- [V] Second run: "Tôi sẽ kiểm diff và dấu vân tay của test đóng băng trước…"; then "Hai nhóm `metadata` và `legacy` đã qua test đóng băng… Nhóm `runtime` còn phát hiện dạng nhãn `API key:` trong thông điệp commit được đưa vào ca đánh giá. Tôi sẽ bổ sung dạng này vào bộ lọc chung rồi chạy lại cả bốn nhóm." — `b-feat-01M7TW709T0MJ3PH419FMWGHZQ.log`. The same event is in `state.json` → `errors` 21:53:35Z ("Test runtime sau sửa còn lọt nhãn API key có dấu cách trong ca eval; đã bổ sung nhận dạng").
- [V] Share of b-feat commands that reference the goal test (`TestBintSecretPersistence` / `secret_persistence_e2e` / `TestSecretPersistenceFakeUpstream`): 5 of 14, 7 of 31, 5 of 34, 9 of 38, 4 of 26, 6 of 14, 1 of 8 in the seven b-feat logs (my scan).
- [V] b-review's own summary treats goal-green as established and finds no type gap: "Lượt goal riêng xanh và tệp test khớp hash đóng băng. Verify rộng vẫn đỏ." Finding 12 (medium) criticises the goal fixture only for not exercising the imported profile's auth path — `TASK\review.md` lines 5, 106–113. None of the 12 findings mentions AWS keys, PEM keys or unlabelled token shapes.

### Inferences
- The proximate cause of B's 16 leaking cells is the frozen goal test's secret-shape coverage (4 of the hidden test's 7 shapes, reached only through generic label rules), inherited from b-req's requirement that explicitly limited detection to labelled fields. B's clean set equals {password, bearer, url_creds, github_token (as `GITHUB_TOKEN=` label)} × {all locations}; its leaking set equals {aws_key_id, aws_secret, private_key} × {all locations}. Location coverage, which is what the plan/goal test invested in, was not B's problem.
- Because the goal test was green from ≈ minute 47 and criteria evidence was defined as subtest passes, b-feat had no signal that anything measured was still wrong; the remaining ≈ 47 minutes of b-feat went to surfaces outside the measured matrix (profile/OAuth credential transport, legacy SQLite, worktree/codegraph copies, TUI read paths).
- Y1 is traceable line-by-line to the frozen goal test (worktree-copy canary must not survive in `state/worktrees`), so the goal test did not merely fail to catch a defect — it required the behaviour that the blind review then classified as critical. Y2 and Y3 are traceable to requirement FR-2/FR-3 and the `legacy`/command-approval parts of the same test. All three surfaces (worktree copies, verify-command validation, legacy scrub of config/profile files) are outside the S1–S6 matrix, i.e. they could only cost B points, never earn any.
- The README's statement for H5 ("chưa có lần kiểm chứng xanh, nên cổng mục tiêu chưa chạy trên cây cuối", `R6` line 66) is narrower than the raw record: the goal gate did run and pass at 23:04:26Z on the tree that the red verification ran on; the hidden test scored 20/36 on the snapshots before and after it. So the evidence does show the frozen goal test passing on a tree that the independent hidden test scores at 55.6 %.
- Full-repo verification ran once, at 78 % of budget, and was the first time B's own regressions (worktree `.env`, repoWriter, MCP shim) became visible to the flow; b-feat had only run narrow tests.

### Gaps
- I did not diff the tree at 23:04:26Z against the final tree; the only changes after the goal-gate pass are the 5 files of the 2m37s fix run, and no hidden-test snapshot exists for exactly that instant (seg3 snapshot is at 22:45Z, seg4 at 23:15Z, both 20/36).
- I did not read the b-goal agent's skill prompt, so I cannot say whether b-goal was instructed to enumerate secret formats; I only observed that its log contains no AWS/PEM strings.
- Why b-req chose label-only detection (training prior vs something in the repo) is not determinable from the logs.

---

## 4. For each loss by B, which step is the proximate cause

### Takeaway
Reported causes shift round by round, but they cluster in two steps: b-req (plan too broad, unreviewed assumptions, or — once — too narrow) and the verification/review loop arriving too late or not converging before the cap. Round 6 adds b-goal (wrong oracle) as a distinct cause.

### Cited Findings
- [R] Round 1, big task: B lost hidden item B2a because its offline analysis only works when review rounds are paired with Runner-driven fix runs; tickets built via CLI yield `unknown`; blind grader called it a deviation from the GĐ2a spec — `R1` lines 114–116. Extra cost came from Bint friction: verification outside the sandbox sharing `TMPDIR` (false red → extra b-feat + b-review round in B-mid), b-feat rewriting `tasks.md` and breaking the artifact contract, agent self-grading "go test toàn bộ đạt" inside the sandbox, resume re-running a fix round after a green re-verify, planner asking an already-answered question — `R1` lines 151–163.
- [R] Round 2: three flow defects with a vague user — Classify labelled the short vague request "rõ" and **skipped b-req**; the done gate passed an empty diff ("✓ Hoàn thành") although review said "tính năng chưa được triển khai"; the blocked state "cần người xem review.md" had no exit — `R2` lines 142–161. B-T1 medium findings: scope too narrow (frees 0 bytes on the fixture), no preview — `R2` line 82.
- [R] Round 3 cause analysis (ranked by the report): (1) b-req's new instruction "mỗi bề mặt một dòng" doubled the plan (6.6 KB/6 AC/5 TSK in round 2 → 14.2/12.2 KB, 8/7 AC, 7/8 TSK) and B's diff was ≈ 6× A's; (2) assumptions made on the user's behalf were never reviewed and contradicted the persona (T1: "Không thêm lệnh CLI", permanent delete → D8; T2: "thay nguyên giá trị dài nhất của mọi env nhạy cảm" → Y1 corrupting `state.json`, "từ chối lệnh chứa credential" → Y7, heavy escaping → N2 15× slower); (3) many tool calls × large context × self-spawned sub-agents; (4) review–fix loop not converging (T1 3 high → 2 high; T2 5 high → 5 high, mostly new defects in freshly added code); (5) no green state to fall back to when cut; (6) unexplained: both sides red on S1–S5 although B's plan listed those surfaces — `R3` lines 136–142.
- [R] Round 4: B-T2 only implemented phase G1 of a plan that b-req itself split into G1–G4 (diff touched only `detect/redact/eval`, skipped package `task`); regression in `detect.Redact` (flag patterns dropped) and Z10 (output after `'Bearer …'` swallowed, including `--- FAIL` lines); B-T1 extra mediums Z1 (backup identified by name → deletes the only copy of `~/.claude/settings.json`), Z2 (one binary file makes the whole clean a no-op), Z3 (quadratic: 20 MB SQLite → 56.6 s preview, 88 s apply) — `R4` lines 50–53, 60–64, 89.
- [R] Round 5: with the plan now covering all 8 surface groups (34 files, no deferred phase), one 47-minute b-feat, red verification, 6 high findings at review round 1, then the cap; B-T2 still leaks at 7 locations due to regex bugs and misses PEM, `aws_secret_access_key`, `SECRET_KEY`, `private_key`, and adds 3 data/flow-corrupting defects (knowledge item loses `---` and body; `TOKEN=$CI_TOKEN` stored as `TOKEN=***`; iso-profile credentials overwritten) — `R5` lines 43–45, 64–69; `SUM` lines 58–59. The large-plan gate "chỉ báo, không giảm công việc" — `R5` line 79.
- [R] Summary hypotheses: same model writes the code; wider scope → more places to be wrong; goal check written by the agent shares the code's blind spots ("PEM, `aws_secret_access_key`, `private_key` vẫn lọt. Chưa kiểm trực tiếp trên transcript"); review finds defects but no time to fix; large-plan gate does not split work; the measurement only scores the end state — `SUM` §4 lines 64–69.
- [V] Round 6: see section 3 — b-req (label-only detection, 55-file plan, no slicing because it read `slice_timeout_minutes: 0` from `.bint/config.yml`), b-goal (19m11s, frozen oracle with 4 of 7 shapes and with requirements that produced Y1), full-repo verification (≈ 14.7 min, first run at 78 % of budget), b-review (8m23s, 10 high) leaving 2m37s for fixes.
- [V] Round-6 config behind "no slicing": `WS\B-L1\.bint\config.yml` line 171 `slice_timeout_minutes: 0`; `WS\B-L1\.bint\local.yml` line 18 `slice_timeout_minutes: 30`; plan text cites the 0 value (`TASK\plan.md` line 9).

### Inferences
- Round 5's summary hypothesis 3 (agent-written goal check shares the code's blind spot; "not yet checked on the transcript") is now checked for round 6 and holds there: the same three shapes (PEM, AWS secret, AWS key id) leak in rounds 5 and 6, and in round 6 the frozen test demonstrably never planted them.
- The b-req step shows an oscillation the fixes did not damp: skipped (round 2) → sprawling (round 3) → self-truncated to G1 (round 4) → full 34 files (round 5) → 55 files (round 6). Each correction traded one failure mode for the other.
- In rounds 3, 5 and 6 the review step did find real defects but they were predominantly defects B itself had just introduced; the loop's net effect inside the cap was negative or zero on the measured outcome.

### Gaps
- `R3` cause (6) — why listing S1–S5 in the plan did not result in redaction there — was never investigated in any later report.
- No transcript-level analysis exists for rounds 2–5 in the files I read (raw `*.jsonl` were deliberately removed from the repo copies, `SUM` §6).

---

## 5. Concrete instances where a Bint step added value (including defects that side A shipped)

### Takeaway
There are a handful of documented gains — mostly safer defaults on T1, one redacted field in round 2, better tests in round 1, and b-review independently finding B's own regressions — but only two or three cases where Bint's side avoided a defect that A actually shipped, and no round where that changed the verdict in B's favour beyond round 2's single critical.

### Cited Findings
- [R] Round 1 mid task: blind grade B 17 vs A 14 "vì test kỹ hơn, xử lý lỗi chặt hơn và ít sửa ngoài phạm vi"; B steps cited project knowledge 18/17 times (four-place config-key rule, `%%` in Render template, `GOOS=windows` vet/staticcheck, `cliRefs`) — `R1` lines 48–51, 130–131.
- [R] Round 2 T2: "Review vòng 1 tìm ra 5 lỗi rò bí mật thật, trong đó 2 high, và vòng sửa đã chữa một phần. Đây là điểm cộng rõ nhất của Bint trong lần đo này." — `R2` line 150. B redacted the agent's `result_summary` going into spool/`runs`; A did not (A's critical Y1) — `R2` line 100.
- [R] Round 2 T1: A shipped a staticcheck failure (SA4006 at `internal/app/cleanup.go:233`, counted as C4+C5) and auto-deletes `backups/` after 30 days with no prompt before a preview could run; B passed staticcheck — `R2` lines 82–83.
- [R] Rounds 4 and 5 T1: B's `bint clean` defaults to preview, `--apply` to delete, 90 days, does not touch open tickets/worktrees/SQLite; A's diff auto-cleans silently with a 30-day default and can delete the only backup (W1, critical downgraded to medium). "Điểm B hơn A: xem trước mặc định, không tự dọn ngầm." — `R4` lines 46–53; `R5` lines 51–58; `SUM` line 60.
- [R] Round 4: N2 slowdown gone (1.1–1.5 s vs A 3.4 s); last-green ref pinned and the copy at the green point gave identical hidden-test results; `reviewStalled` stopped a non-converging loop once — `R4` lines 56–58, 71–75.
- [R] Round 3 H5: b-review recorded "bỏ sót" findings (e.g. T2 round 2 "TUI vẫn đọc nguyên tài liệu và log kiểm chứng cũ ngoài lớp che"), not individually verified — `R3` line 96.
- [R] Round 6: "b-review vòng 1 tìm 10 high, trùng phần lớn với lỗi chặn của review mù (worktree `.env`, repoWriter, YAML). **Bint bắt được lỗi nhưng hết ngân sách trước khi sửa.**" — `R6` line 67. Resume after the hard kill did not redo b-req/b-goal; no work lost on either side — `R6` lines 39–41, 64.
- [V] Round 6 overlap, checked item by item: b-review finding 9 ("Worktree thay giá trị vận hành trong .env mà vẫn báo đã chép", `internal/workflow/worktree.go:223-226`, `TASK\review.md` lines 79–86) = blind Y1 (`WS\review\Y-verified.json`). Finding 8 (scrub writes project files outside `repoWriter`, `TASK\review.md` lines 70–77) touches the same code as Y3 but not Y3's substance (no backup; `${VAR}` references destroyed). Finding 7 (MCP stdio filter renames tools before the gateway permission check) is in the same filter as blind Y4 (downgraded to medium) with a different symptom. **Y2 (verify command with references rejected / silently dropped) has no counterpart among the 12 b-review findings.**
- [V] Round 6: Bint's verification gate caught four real regressions before review (`TASK\logs\verify-1.log`); three of the same tests are still red on the final tree (`WS\results\B-L1\repo-tests.txt`: `TestMCPShimIdentityToSpool`, `TestRepoWritesGoThroughRepoWriter`, `TestWorktreeBaseHasDirtyStateAndCopies`; `TestNoVietnameseLiteralsInTUICode` no longer listed).
- [V] Round 6, defects A shipped that B's flow avoided: A leaves S3 (task description) and S6 (task docs) at 0/7 each while B, whose requirement AC-2 names tickets/documents/review, is 4/7 on both (`WS\results\*\seg*.hidden.json.matrix.json`; `TASK\requirement.md` line 15); B's N2 ratio 5.5× vs A's 20.11× (`n2.json` files) — both still fail the 3× limit.

### Inferences
- Counting strictly "a Bint step caught a real defect that A shipped": round 2 (`result_summary` leak; staticcheck SA4006), rounds 4–5 T1 (silent destructive default), round 6 (two whole storage locations skipped). In round 6 the gain came from b-req's breadth, not from a checking step, and it was more than offset by the type gap and Y1–Y3.
- b-review's hit rate against the independent blind review in round 6 is 1 exact of 3 criticals (Y1), with 2 adjacent; it missed the one critical (Y2) that sits in ticket/verify-command logic rather than in a failing repo test. Four of its ten highs (7–10) restate tests already red in `verify-1.log`.
- No Bint step in any round checked performance (N2) or measured the user's actual goal on T1 (space reclaimed); both sides share D6/D8i misses in rounds 3–5 (`SUM` line 34).

### Gaps
- Round 2's "5 real leaks found by review" and round 3's H5 findings were not cross-checked against the blind findings in the reports.
- The value of plan/requirement artefacts, resume, and acceptance criteria as process outputs was never scored (`SUM` line 69).

---

## 6. What side A got wrong in each round (what a checker could have caught)

### Takeaway
A's failures are consistently omissions and unverified "done" claims: red build/tests at hand-off, storage locations never touched, destructive defaults, and performance regressions — all detectable by mechanical checks that A did not run or ignored.

### Cited Findings
- [R] Round 1: A-mid handed off with build/tests red (`TestNoUnusedKeys`, 6 unused i18n keys); fixed only after the user said "build/test đỏ, sửa đi" — `R1` lines 72, 81. A-big: both sides reject `stop` (B5a) — `R1` lines 103, 117.
- [R] Round 2: A-T1 told the user to run `bint clean --dry-run` without rebuilding/installing the binary ("lệnh lạ"); auto-deletes `backups/` after 30 days; staticcheck red. A-T2: 6 critical — S1–S5 plus `result_summary`; missed PEM and AKIA in the very logs it redacts; missed bare `ghp_`/`github_pat_`/`sk-`, token-in-URL, `key = value` with spaces, `curl -u`, `xoxb-`, `npm_`; N2 passed narrowly (4.82 s vs 4.88 s limit); agent "báo xong, nói tính năng tự bật, không có lệnh nào để thử" — `R2` lines 82–109, 134–138.
- [R] Round 3 (this diff is reused as A in rounds 4 and 5): T1 — cleaning reclaims 144 bytes of a 685 KB fixture (D6 ≡ X2), preview does not list old tickets (D8i), default 30 days, cleans on event ingest. T2 — S1–S5 critical, S6 medium; X4: sensitive env vars with short values (`1`/`true`) masked everywhere, corrupting numbers/JSON in diagnostics; N2 3.4–3.5 s vs 0.9 s baseline (pass under that round's limit) — `R3` lines 64, 67, 71, 78–83.
- [R] Same A diff as seen by later reviewers: round 4 — W1 (silent auto-clean, deletes the only backup), W5 (does not clean worktrees/tickets/logs), T2 7C/1M with secrets raw in `task.yml`, plan/requirement/tasks, `review.*`, `state.json`, `logs/prompt.txt`, `state.sqlite`, W10 over-redaction — `R4` lines 50, 60–64; round 5 — W1, W2 (deletes eval logs SQLite still points to), W7 (no cleaning of finished-ticket logs) — `R5` line 55.
- [R] Round 6: A "Bỏ trọn 2 nơi lưu: mô tả phiếu (S3), tài liệu phiếu (S6), mọi loại"; N2 fails at 20.1×; declared done after 3 sessions; mediums X1 (env-value substring replacement corrupts event keys/paths when the value is weak, e.g. `DB_PASSWORD=test`), X2 (`runs scrub` without locks or backup), X5 (an unterminated JSON line/quote swallows all following log output including `--- FAIL`); low X10 (old data is only cleaned when the user runs `bint runs scrub`) — `R6` lines 13–19, 42, 52–54.
- [V] Round 6 raw: A's matrix was 15/36 after session 1 (S3, S4, S6 at 0/7), 22/36 after session 2 (+S4), unchanged after session 3 — `WS\results\A-L1\seg*.hidden.json.matrix.json`. In session 3 A ran full-repo `go test` three times, all exit 1, and its final message says "Bộ kiểm thử toàn repo vẫn chưa qua hết trên Windows" while reporting the work complete; it also says "Token cũ xuất hiện riêng lẻ có thể còn sót…" — `WS\log\A-L1.seg3.1.out` (last `agent_message`). `WS\results\A-L1\repo-tests.txt`: `go test` exit 1 with `TestCleanupWorktreeReleasesGatewayBounded` and `TestGateEvidenceAndBudget` failing on the first run. [R] README counts these as Windows flakiness that passed on rerun (`R6` line 17).
- [V] A's try-out by the user-sim was vacuous: `bint runs scrub` printed "Tệp đã lọc 0, tệp đã xoá 0…" and "repository chưa được khởi tạo Bint" because side A's repo has Bint removed — `WS\log\A-L1.tryout1.txt`; `WS\log\user-actions.md`.
- [V] X-verified details: X1, X2, X5 confirmed medium; X4 downgraded to low — `WS\review\X-verified.json`.

### Inferences
- A black-box "plant each secret type at each storage location, then scan" check (the hidden test itself) would have flagged A's S3/S6 omission after session 1 and B's three-type omission equally; a timing check on a 20 MB log would have flagged both N2 failures. Neither side had such a check in its loop; B had a check, but of the wrong shape.
- A's stopping rule was self-declared completion; in round 6 it stopped with 14 of 36 cells leaking, a 20× slowdown and a non-green full test run it acknowledged.

### Gaps
- I did not see the rerun evidence for A's two flaky tests in round 6 (only the first-run failure file is in `WS\results\A-L1`).
- Round 1's A-big defects beyond B5a are not itemised in the README.

---

## 7. Bint mechanisms never actually exercised in any benchmark

### Takeaway
Thin slicing, slice checkpoints, `restore-green --apply`, the opt-in fast path, and cross-model session continuation have no benchmark measurement at all; round 6 was designed to measure slicing and did not, because b-req read the un-merged repo config.

### Cited Findings
- [R] Slicing: "**Không chia lát:** b-req đọc `.bint/config.yml` của repo (`slice_timeout_minutes: 0`), không thấy giá trị 30 ở `.bint/local.yml`, nên ghi 'Chia lát: không'. Cơ chế lát mỏng **không được đo**." — `R6` line 62; H2 (one-way slice commits) "Không áp dụng" — `R6` line 63. Round 6 was meant to be the first measurement of "b-goal, lát mỏng chốt một chiều, điểm xanh lát" — `P6` lines 14–20.
- [V] Confirmed in `TASK\plan.md` line 9 and the two config files (`WS\B-L1\.bint\config.yml` line 171, `.bint\local.yml` line 18).
- [R] `last_green`: `None` for the whole round-6 run ("kiểm chứng duy nhất … đỏ") — `R6` line 63; `None` in round 5 B-T2 — `R5` line 45. Round 4: the green ref was pinned and a copy at that point was measured, but "`bint task restore-green` chưa chạy `--apply` thật" — `R4` lines 57, 74, 86.
- [R] Fast path: default off since `a8f2f95`; "Đường nhanh opt-in **chưa đo**" — `CHG` lines 30, 39; `SUM` line 72; `R5` line 75. It was observed being auto-selected for both prompts before round 5 (the reason it was turned off), not benchmarked — `CHG` line 29.
- [R] Cross-model continuation (`a0b8581`): hand-tested only; every benchmark step used the same model — `CHG` lines 33–34; `SUM` line 73; `R5` lines 38, 90.
- [R] Exercised only once or twice: `reviewStalled` ("không hội tụ") fired once (round 4 B-T2) — `R4` line 56; fix-round exit used via `--fix-round` in rounds 4–5 — `R4` line 47, `R5` lines 42, 46; b-goal ran once (round 6); the agent-written "Kiểm tra mục tiêu" section existed in round 5 — `R5` line 77. Round 3's exit-from-fix-rounds path (H3) "Không đo được" — `R3` line 94.
- [V] Session continuation across the hard kill in round 6 did not happen at the Codex-session level: the first b-feat run after the kill has no `resume` argument and a new thread id `01a1f5d1-d879-70c3-8a05-75ae79b38a73` (previous: `01a1f5a7-3856-7d51-870c-6d5630ce22e0`), and its console output lacks the "↻ nối tiếp phiên" line — `TASK\logs\b-feat-01M7TX3N7G59K0FXKQMS2VYDJZ.log` line 1 and `thread.started`; `WS\log\B-L1.seg2.2.out`. Runs before the kill resumed `01a1f5a7`; all runs after resumed `01a1f5d1`; `state.json` → `last_step.origin_run_id` = `01M7TX3N7G59K0FXKQMS2VYDJZ`, `continuations: 2`.
- [R] README's wording for the same event: "`bint run resume` nối tiếp đúng phiên b-feat ('lớp dừng external, nối tiếp được, đã nối tiếp 2 lần'). Không chạy lại b-req/b-goal." and the killed run stayed `running` in `bint run status` — `R6` lines 40, 64.

### Inferences
- What survived the hard kill in round 6 was ticket-level state (plan, frozen test, working tree), not model context; the post-kill b-feat re-read the plan in a fresh session, as A did with its fixed continuation prompt. The README's H3/H4 "yes" should be read as ticket resume, not session resume, for that specific cut.
- `--accept` (second fix-round exhaustion → accept) does not appear as used in any report I read; only `--fix-round` is recorded.

### Gaps
- No benchmark shows what slicing would have done to the round-6 timeline; the only data point is the un-sliced run.

---

## 8. Methodology limits

### Takeaway
Every cell is a single run, reviewer variance on an identical diff (±2 criticals) exceeds most A–B gaps, B's hard-task runs were all truncated by time caps, and rounds differ in platform, model, scoring scheme and hidden test, so absolute numbers do not compare across rounds; round 6 additionally had a contaminated first attempt and a second aborted attempt.

### Cited Findings
- [R] One run per cell in every round — `R1` line 4; `R2` lines 16, 177; `R3` line 5; `R4` line 19; `R5` line 21; `R6` line 27; `SUM` line 39.
- [R] Reviewer variance: the same A diff was graded T1 0C/1M (round 3), 0C/2M (round 4), 0C/3M (round 5); T2 5C (round 3), 7C (round 4), 7C (round 5); counting granularity changed from round 4 (one finding per leaking location) — `SUM` lines 40–41; `R4` lines 19–21 ("dao động của người chấm cỡ ±2 critical").
- [R] Time caps: 90 min per B run in rounds 3–5, B-T2 cut mid fix round in all three, "Chưa từng đo B với thời gian dài hơn" — `SUM` line 42; round 6 cap 4 × 30 min with one hard kill at minute 15 of session 2 — `P6` §5. User-sim waiting time is excluded from the cap — `CHG` line 49.
- [R] A was not re-run in rounds 4 and 5 — `R4` lines 3–5; `R5` lines 3–5.
- [R] Scoring changed across rounds: hidden pass counts + blind 20-point grade (round 1); deduplicated critical/medium from hidden test + blind adversarial review + blind verification (rounds 2–5); type × location matrix with review used only for blocking defects (round 6) — `R1` lines 11–14; `R2` lines 59–70; `P6` §2, §6. Round-6 tie rule: ≥ 15 percentage points and no blocking defect the other side lacks — `P6` §7.
- [R] Fixture/protocol changes after seeing data: round 1 fixture v1→v2 after A's results, before B's — `R1` lines 109–113; round 2 two mid-run protocol decisions (let the user ask again on B-T1; try-out on a fake "user machine") — `R2` lines 179–181; round 3 D5 fixture change for the new "no file changed" gate — `R3` lines 52–53.
- [R] The "remaining defects" metric does not reward work done: B-T1's first pass in round 2 did nothing and had 0 critical — `R2` line 178. Per-location counting makes B's critical count partly a function of covering more locations — `R5` line 87.
- [R] Blind reviewers knew diff sizes (B ≈ 6× A in round 3); coordinator knew which T1 variant had `gc` — `R3` line 119. B has a legitimate project-knowledge advantage — `R1` lines 48–51; `R3` line 121.
- [R] Environment incidents affecting B's cost: `/tmp/.git` created by the Codex sandbox causing false-red verification and an extra fix/review round (round 1) — `R1` lines 137–147; disk full causing a red `go test` and ≈ 17 extra minutes (round 2) — `R2` lines 167–171; orphan `codex exec` after timeout, token-tool cutoff bug (round 3) — `R3` lines 108–113; disk nearly full (rounds 4, 5) — `R4` line 84, `R5` line 89.
- [R] Round 6 contamination: attempt 1 cancelled because the repo at `17812a0` still contained `docs/evals/bench-bint-vs-plain-*` and old plans (risk spec, round-5 findings, hidden-fixture description); A's rollout read them > 2000 times, B's b-req/b-goal also read them; A reached 36/36 after one session; B after session 1 (b-req 6m10s + b-goal 23m48s) was 0/36. Main run on `48dbfad` (docs removed, both sides); rollouts checked: not read — `R6` lines 77–79; `EV\bench-bint-vs-plain-v6-20261120\log\aborted-1.md`.
- [V] Main-run base confirmed: `state.json` → `base` = `48dbfaded1feebf9f6f56e268c9f911422b3b716`; `git log` in `WS\B-L1` shows `48dbfad bench: gỡ tài liệu benchmark trước (đáp án của bài thi), áp cho cả hai bên` on top of `17812a0`.
- [R] Round 6 attempt 2 cancelled after 1 minute: two `CODEX_HOME`s reset the Windows sandbox account password (`CreateProcessWithLogonW 1909`, 10-minute lockout) — `R6` lines 80–81; `WS\log\prep-summary.md`.
- [R] Platform/model differences: rounds 1–5 Linux + `gpt-6.1-sol`; round 6 Windows 11 + `gpt-6.2-sol`; hidden test rewritten for Windows (`hidden_l1.py`), not comparable in absolute numbers with `hidden2.py`; Windows `go test ./...` ≈ 10–15 min penalises B's full-repo verification more than A — `R6` lines 82–83, 89; `P6` §2.
- [R] Round 6 did not stress context: window 872k, "L1 với `gpt-6.2-sol` chưa đủ dài để A tràn ngữ cảnh: A tự kết thúc sau 84 phút" — `R6` line 95; `WS\log\prep-summary.md`.
- [R] The b-req skill text (present in both repos) contains a generic example "không lưu bí mật — cài bí mật giả có dấu dễ grep vào mọi đường nhập"; kept as is — `R6` line 86.
- [R] B's `.bint/config.yml` in the repo sets `slice_timeout_minutes: 0`; override only in `local.yml` — `R6` lines 84–85.
- [R] Matrix weights all cells equally (a leaked private key = a leaked URL credential) — `P6` §8 line 124.

### Inferences
- The only results robust to grader variance are the deterministic ones: hidden-test equality of A and B in rounds 4–5 (same D6/D8i, same S1–S5/S6), the round-6 matrix on a fixed tree, N2 timings, repo test status, token counts and wall time.
- Because the round-6 matrix has six locations and B's flow spent most of its effort on locations outside it, the round-6 score under-rewards B's breadth and simultaneously exposes B to blocking defects from that breadth; the instrument and B's self-chosen scope were misaligned. This is a property of the design (stated in `P6` §6.1–6.2), not an error in execution.
- Round 6's contaminated attempt is itself informative as an anecdote, not a measurement: with the answer key readable, A hit 36/36 in one 30-minute session while B's first 30 minutes produced a plan and a goal test and 0 cells.

### Gaps
- No estimate of run-to-run variance for the agent exists in any round (never more than one run per cell).
- No uncapped B run exists, so "would B have converged" is unanswered for T2/L1 in rounds 3–6.
- The removed raw material for rounds 1–5 (`hidden/`, `tools/`, `*.jsonl`, files > 400 KB; `SUM` §6) lives on a Linux machine (`/home/baodq/…`) not accessible from this workspace, so rounds 1–5 could only be read through their reports.
