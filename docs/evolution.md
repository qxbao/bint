# Tiến hoá tri thức (P6), skill (P7) và vòng tự động (P8)

Evolution Engine phát hiện cơ hội từ lịch sử lượt chạy, soạn đề xuất có bằng chứng (L1 cho
tri thức và bài học từ thất bại, L2 cho skill), đối chiếu bằng khung đánh giá rồi áp dụng
hoặc chờ người duyệt (mục 10–11 của [đặc tả thiết kế](bint-plan.md); cấu hình tại
[docs/configuration.md](configuration.md)).

## Tiến hoá tri thức — L1 (P6)

Ở chế độ mặc định (`propose`), Bint phát hiện cơ hội, soạn đề xuất và đối chiếu trên tập
`validation`/`test`; **người duyệt** quyết định áp dụng. Ở vòng tự động `bint evolve cycle` (P8),
khi bật `evolution.adoption.auto_low_risk: true`, Bint chỉ tự áp dụng tối đa một đề xuất rủi ro
thấp tạo mục `candidate` mới mỗi vòng (đặt `evolution.mode: auto` riêng lẻ không bật tự áp dụng).

```sh
bint evolve scan                         # cơ hội + điều kiện tiên quyết (không gọi mô hình)
bint evolve propose --no-agent           # đề xuất xác định: loại bỏ, gộp mục trùng
bint evolve propose op-1a2b3c4d --budget 1   # cơ hội tạo/sửa tri thức: agent evolver soạn
bint evolve propose op-… --budget 1 --reflector-model <m>   # cơ hội failure_lesson: agent reflector rút bài học
bint experience digest <đợt|lượt>        # bản tóm tắt thất bại xác định (~1.500 token) và nhóm thất bại
bint experience show                     # thư viện bài học: chung trước, riêng theo vùng; số lần chèn, đạt/trượt
bint evolve show ep_…                    # thay đổi, phiên bản gốc, căn cứ, kế hoạch, kết quả
bint evolve evaluate ep_… --split validation --budget 5 --yes
bint evolve evaluate ep_… --split test --budget 10 --yes
bint evolve apply ep_…                   # cần kết quả supported, độc lập trên test
bint evolve apply ep_… --unverified      # hoặc: chấp nhận rõ là chưa kiểm chứng (được ghi lại)
bint evolve rollback ep_…
bint evolve history                      # kiểm chứng, đối chiếu mô hình, theo dõi sau áp dụng, chi phí
```

**Dữ liệu** (chỉ đọc, đã lọc): lượt chạy đã kết thúc trong cửa sổ `memory.retention_days`, các
bước workflow gộp vào lượt gốc; nhãn kết quả tính từ tín hiệu (mục 9.6) sau khi quét Git để cập
nhật commit/hoàn tác đến muộn — **lượt `unlabeled` không bao giờ là bằng chứng**. Hook Claude Code
ghi thêm `file.read` (tệp agent đọc) để đo khám phá lặp lại; Codex/Antigravity không có hook
trước công cụ đọc nên chỉ có tệp được sửa.

**Nhóm tác vụ** (MVP, mục 10.3): theo khu vực mã chính của lượt; mặc định hai cấp thư mục của tệp
được sửa, nhóm chiếm quá nửa số lượt được tách sâu thêm (có thể đặt `evolution.trigger.group_depth`).
**Cơ hội** (mục 10.2):

| Loại | Điều kiện | Hành động | Tạo bởi |
|---|---|---|---|
| `exploration` | tệp (hoặc ≥ 2 tệp cùng mẫu tên trong một thư mục: `handlers/*_handler.py`, `test_*.py`, `*_test.go`) được chạm lại ở ≥ `min_similar_runs` lượt có nhãn, không mục nào có phạm vi khớp | `create` | agent |
| `failure_hotspot` | tệp bị sửa ở ≥ `min_new_evidence` lượt thất bại, chưa có `warning`/`rule` | `create` | agent |
| `stale_used` | mục `stale` vẫn được truy xuất, hoặc `revalidate` nhận định `outdated` | `update` | agent |
| `stale_expired` | `stale` quá `knowledge.stale_deprecate_days` | `deprecate` | xác định |
| `unused` | truy xuất ở ≥ `min_similar_runs` lượt có nhãn, chưa từng được trích dẫn | `deprecate` | xác định |
| `duplicate` | cùng loại, phạm vi chồng, tiêu đề gần trùng | `merge` | xác định |

Cơ hội dựa trên lượt chạy cần đủ lượt có nhãn **và** ≥ `min_new_evidence` lượt chưa dùng cho đề
xuất trước cùng đối tượng; cơ hội đã có đề xuất đang chờ thì không đề xuất lại.

**Điều kiện tiên quyết** (mục 10.6) được kiểm tra ở mỗi lần quét: lượt có nhãn, tác vụ đánh giá đã
kiểm chứng ở tập `validation`/`test` đủ cỡ mẫu cho `evolution.evaluation.min_effect` (mặc định 0,15: +10 điểm %
không phát hiện được với tác vụ của một repository; cỡ mẫu theo mô hình phương sai bên dưới), ước tính chi phí một lần đối chiếu nằm trong `evolution.evaluation.budget_usd`. Không
đạt → đề xuất vẫn được tạo nhưng ghi rõ **chưa được kiểm chứng định lượng**.

**Đề xuất** (mục 10.4) lưu trong cơ sở dữ liệu trạng thái cục bộ (bảng `evolution_proposals`, schema
có phiên bản) và bất biến: hành động, **phiên bản gốc** của mỗi mục bị sửa (phiên bản + băm tệp), lượt
làm căn cứ kèm nhãn lúc đó, thay đổi (chỉ nội dung: loại, tiêu đề, thân, phạm vi, bằng chứng — không
đặt được trạng thái, người duyệt, nguồn gốc hay cờ bên thứ ba), **rủi ro do Bint tính** (sửa/loại bỏ
`rule`/`warning` đã duyệt = cao), **kế hoạch đánh giá chốt trước** (`superiority` trên
`evolution.evaluation.metric`, hoặc `non_inferiority` với `non_inferiority_margin` cho loại bỏ/gộp).

**Bài học từ thất bại** (kế hoạch E). Nguồn: lượt thử trượt của đợt đánh giá trên tập **validation** (tập
`test` không bao giờ là đầu vào), và lượt thật có nhãn thất bại. `bint experience digest` dựng bản tóm tắt xác
định (đề bài, chuỗi lệnh gộp lặp, tệp đọc/sửa, test trượt và đuôi log, dấu hiệu: không chạy test, sửa ngoài
vùng, lặp lệnh, hết lượt/thời gian, bỏ qua tri thức đã chèn), giới hạn ~1.500 token; log cũ không có vết
được ghi là "không đo được", không đoán. Gom nhóm theo **vùng mã** (bài học riêng) và theo **dấu hiệu** (bài
học chung); một nhóm sinh cơ hội `failure_lesson` khi có ≥ `evolution.trigger.min_similar_runs` tác vụ phân
biệt (lượt của cùng một tác vụ chỉ tính một). Agent reflector (`roles.reflector`, chỉ `claude`, khoá hết công cụ
bằng `--tools ""`, chạy trong thư mục rỗng) trả khuôn `{"lessons":[{scope, trigger, mistake, instead, evidence}]}`, tối đa một ứng viên
chung (`global`) và một riêng (`domain:<glob>`). Mỗi ứng viên qua kiểm tra xác định trước khi thành đề xuất
L1 `create lesson` (`candidate`, rủi ro thấp): rò rỉ (định danh dạng mã của phần thêm mới lời giải, rà bằng
`eval.CheckLeak` và `SolutionIdentifiers`; không kiểm được thì loại), tổng quát (chung ≥ 2 vùng mã, riêng ≥ 2
lượt trong vùng), trùng (gộp vào mục đã có bằng `update`, không đánh giá định lượng), hình thức (đủ `trigger`,
`mistake`, `instead`; ≤ 80 từ), nguồn gốc (lượt thuộc `validation`). Tác vụ nguồn của bài học không được chấm lại
trên chính nó; delivery `minimal` không chở bài học nên đề xuất không đo được. Bài học đi qua
`bint evolve cycle` như đề xuất khác.

Agent evolver (`roles.evolver`, chỉ Read/Grep/Glob, dữ liệu đóng khung là dữ liệu không đáng tin) trả
JSON; mỗi ứng viên phải qua kiểm tra xác định được như `bint knowledge add` (bằng chứng tồn tại, không
trùng, độ dài, cờ bên thứ ba tự gắn) — không qua thì bị loại kèm lý do.

**Đối chiếu** (`evaluate`, mục 11.2) dùng khung đánh giá với hai nhánh mới `base` và `candidate`: cùng
tập tri thức của tác vụ (lọc chống rò rỉ theo `evolution.evaluation.leakage_mode`), cùng cách đưa tới
agent (`delivery: bint | minimal`); `candidate` = sau khi áp đề xuất. Đề xuất `deprecate` chính là
**đánh giá loại bỏ** (mục 9.7, 12.4). Chỉ chạy tác vụ đã duyệt, đã kiểm chứng fail→pass, mà đề xuất thực
sự đổi tập tri thức và mục đổi có phạm vi liên quan tới tệp của tác vụ.

Nội dung mới của đề xuất luôn được viết *sau* các tác vụ trong lịch sử, nên không qua được điều kiện nguồn
gốc của economy (A3: mục tạo sau commit gốc bị loại). Thay vào đó, đối chiếu **hồi cứu** dùng ba lớp chặn:
(1) bằng chứng của nội dung phải đã khớp tại commit gốc; (2) nội dung không chứa mã của lời giải;
(3) loại tác vụ mà lời giải có thể đến từ chính các lượt làm căn cứ của đề xuất — commit lời giải được quét
Git gắn với lượt đó, hoặc lượt đó sửa tệp của lời giải và commit nằm trong cửa sổ 24 giờ sau lượt. Rủi ro rò
rỉ gián tiếp còn lại được ghi trong kết luận. `--prospective` chỉ dùng tác vụ sinh sau đề xuất (**tiến cứu**)
để loại hẳn rủi ro này. `show`, `apply` và `history` ghi rõ lần đối chiếu làm căn cứ là tiến cứu hay hồi cứu
(đề xuất chỉ loại bỏ tri thức không có nội dung mới nên không phân loại). Kết luận
`supported | not_supported | inconclusive` theo kế hoạch đã chốt (cảnh báo đo lường chặn → `inconclusive`).
Trên tập **test**, nếu tác vụ đã dùng để đối chiếu một đề xuất khác cùng đối tượng thì kết quả bị đánh
dấu **không độc lập** (mục 12.2).

**Probe tuần tự** (ít tác vụ, nhiều lần lặp, dừng sớm). Mỗi đề xuất chỉ có vài chục tác vụ đủ điều kiện,
nên cỡ mẫu được tính theo phương sai của chênh lệch theo tác vụ khi mỗi nhánh lặp R lần:
`σ²(R) = σ_b² + W/R`, với `W` = trung bình theo tác vụ của `2·p(1−p)` (ước lượng không chệch `k(m−k)/(m(m−1))`
từ các tác vụ có ≥ 2 lượt cùng nhánh `none` trong lịch sử; chưa có dữ liệu thì p = 0,5, W = 0,5) và `σ_b` là độ
lệch giữa các tác vụ về hiệu ứng thật (`evolution.evaluation.effect_sd`, mặc định 0,15; chỉ ước lượng từ lịch
sử khi có đủ chênh lệch ghép cặp kèm lặp). Mỗi con số in kèm nguồn. Trước khi chạy, `evolve evaluate` in kế hoạch:
số tác vụ `n(R) = ⌈((z₀.₉₇₅+z₀.₈)·σ(R)/δ)²⌉` (δ = `min_effect`, hoặc `non_inferiority_margin` cho loại bỏ),
số lần lặp R, số lượt và chi phí tối đa/kỳ vọng. `evolution.evaluation.repeats: 0` để bộ lập kế hoạch chọn R
trong 1..`max_repeats` (mặc định 8) sao cho `n·2·R` thấp nhất với `n` ≤ số tác vụ sẵn có; > 0 thì cố định R.
Không R nào thoả → từ chối kèm mức chênh lệch nhỏ nhất phát hiện được, trừ khi có `--force`.

Các lượt chạy theo từng lần lặp (mọi tác vụ và nhánh của lần lặp 1, xáo trộn, rồi lần lặp 2, ...) nên mỗi ranh
giới lần lặp đều cân bằng. Tối đa 4 điểm nhìn (K = min(R, 4)); tại điểm nhìn j dùng biên kiểu O'Brien–Fleming
`z_j = c_K·√(K/j)` (c_K = 1,96 / 1,977 / 2,004 / 2,024 cho K = 1..4, đã hiệu chỉnh để tổng sai số loại I ≈ 5 %)
trên CI chuẩn của trung bình chênh lệch ghép cặp: **superiority** dừng và `supported` nếu cận dưới > 0;
**non-inferiority** dừng và `supported` nếu cận dưới > −margin; **futility** dừng và `not_supported` nếu cận trên
CI95 < `min_effect` (hoặc < −margin với non-inferiority). Điểm nhìn cuối luôn kết luận. Số điểm nhìn, điểm dừng
và lý do được ghi vào kết luận; đợt dừng sớm theo kế hoạch không bị coi là "incomplete". `--resume` chạy tiếp theo
đúng thứ tự cũ (cùng hạt giống) và nhìn lại các lần lặp đã xong trước khi chạy lượt mới.

**Áp dụng** (`apply`, chỉ người; khoá tiến hoá trong `<state>/runtime/locks/evolve.lock`):
- Phiên bản gốc phải còn khớp — mục bị sửa sau khi đề xuất (người khác áp dụng, `git pull`, sửa tay)
  làm đề xuất **không hợp lệ** (`list` hiện `outdated`); bác bỏ và đề xuất lại.
- Cần kết quả `supported`, độc lập, trên `test`, với đúng phiên bản gốc hiện tại — hoặc `--unverified`.
- Rủi ro cao cần `--confirm-risk`. Mọi mục được kiểm tra xác định được lại trước khi ghi.
- Mục mới do agent soạn vào `candidate` (duyệt bằng `bint knowledge review`); mục được sửa nhận người
  áp dụng làm người duyệt; loại bỏ = `deprecated` (`delete` chỉ cho mục `candidate`/`deprecated`).
- Bản ghi kiểm toán (`evolution_adoptions`) giữ nội dung trước/sau từng tệp: `rollback` khôi phục, từ
  chối nếu tệp đã bị sửa sau khi áp dụng (trừ `--force`).

`history` gắn mỗi lần áp dụng với harness/mô hình của lần đối chiếu làm căn cứ và báo **chưa xác thực
với mô hình hiện tại** khi mô hình đổi (`evolution.adoption.bind_to_model_version`, mục 12.6); theo dõi
số lần truy xuất/trích dẫn sau áp dụng (chỉ là gợi ý, không phải tác động); chi phí tạo ứng viên và
chi phí đối chiếu tính riêng (mục 12.8). `show` báo khi nhãn của lượt làm căn cứ đã đổi do tín hiệu đến
muộn. `bint status` nhắc khi có đề xuất chờ duyệt.

Giới hạn đã biết của P6:

- Chế độ Manual và Deferred (xem dưới); chưa có Managed (tiến trình nền, mục 13.4).
- Nhóm tác vụ theo khu vực mã, chưa dùng mô tả tác vụ hay biểu diễn ngữ nghĩa.
- `evaluate` cho đề xuất đang chờ; với đề xuất đã áp dụng (tri thức hoặc skill), đổi mô hình được báo ở
  `bint evolve history` / `bint skill regress` và kiểm tra lại bằng `evaluate --regression`.
- Ở chế độ `economy`, tri thức hiện có tạo sau commit gốc của tác vụ bị loại, nên đánh giá loại bỏ cần
  tác vụ có commit gốc sau khi mục đã tồn tại; tác vụ không chịu ảnh hưởng được báo kèm lý do.
- Đã chạy [đợt đo thực nghiệm trên Claude Code](evals/p6-p7-claude-2026-11-10/README.md) nhưng chưa có
  đề xuất đạt trên tập `test` độc lập; năm phát hiện nghiệm thu `P6-01..P6-05` (khoá tiến hoá, hậu kiểm
  `git status` của evolver, rollback giữa chừng, gộp `Inputs` và merge nội dung) vẫn còn mở tại
  [docs/status.md](status.md).

## Tiến hoá skill — L2 (P7)

Cùng vòng đề xuất → đối chiếu → áp dụng → quay lui của L1, đối tượng là **skill chuyên biệt** trong
`.bint/skills/evolved/<tên>/`. Skill gốc (`b-*`) bất biến: không bao giờ là đối tượng của đề xuất,
tuỳ biến chúng chỉ qua lớp phủ do người viết (mục 6.6).

```sh
bint evolve scan                         # thêm cơ hội L2: skill_candidate, skill_merge, skill_stale
bint evolve propose op-… --budget 1      # tạo skill: agent evolver soạn; gộp/loại bỏ: xác định, không gọi mô hình
bint skill check ep_…                    # kiểm tra điều kiện kích hoạt TRƯỚC khi đối chiếu
bint evolve show ep_…                    # đọc SKILL.md / bản thay đổi: đây là chỉ dẫn sẽ nằm trong ngữ cảnh agent
bint evolve evaluate ep_… --split validation --budget 5 --yes   # base = tập skill hiện có, candidate = sau đề xuất
bint evolve evaluate ep_… --split test --budget 10 --yes
bint evolve apply ep_…                   # xuất bản cho harness (bint sync) ngay sau khi ghi
bint evolve rollback ep_…                # khôi phục tệp và gỡ bản đã xuất bản
bint skill list
bint skill show <tên>
bint skill versions <tên>
bint skill diff <tên>
bint skill regress                       # skill nào chưa xác thực với mô hình hiện dùng
bint evolve evaluate ep_… --regression --model <mô hình> --yes  # chạy lại tập nhỏ đại diện
```

**Kích hoạt thực tế**: hook Claude Code ghi `skill.activated` khi công cụ Skill chạy; bảng `run_skill_usages`
(migration 0007) liên kết skill với lượt chạy. `bint skill check` báo số lượt đủ điều kiện mà skill thực sự
được kích hoạt; `skill_ignored` (action `update`, agent viết lại mô tả, giữ tên) khi < 20% với ≥ `min_similar_runs`
lượt đủ điều kiện. Hook cũ cần `bint init --hooks` để thêm `Skill` vào matcher.

**Cơ hội** (từ lượt chạy có nhãn, mục 10.2): `skill_candidate` — khu vực mã có quy trình được xác nhận
hiệu quả ở ≥ `evolution.trigger.min_similar_runs` lượt thành công mà chưa có skill bao phủ (cần
`min_new_evidence` lượt mới so với đề xuất trước); `skill_merge` — hai skill mô tả tương đồng ≥ 50% và
chồng đường dẫn kích hoạt; `skill_stale` — mọi mẫu `triggers.paths` không còn khớp tệp nào;
`skill_ignored` — kích hoạt dưới 20% lượt đủ điều kiện (chỉ tính trên các lượt của Claude Code).

**Kiểm tra xác định được** (không gọi mô hình) trước khi đề xuất thành hình, và lặp lại khi áp dụng /
`bint sync` / `bint doctor`: tên hợp lệ (không `b-*`, không trùng skill gốc); mô tả một dòng 20–400 ký
tự (harness chọn skill theo mô tả); frontmatter chỉ `name` và `description` (không tự xin quyền hay hook);
không ghi đè quy tắc/tri thức/skill gốc, không nhắc `.bint/evals`, `.bint/config.yml` hay dữ liệu Bint
quản lý (đề xuất không tự sửa tiêu chí chấm điểm); bằng chứng, ký hiệu, ví dụ mã, đường dẫn khớp mã thật
(cùng bộ kiểm tra của tri thức); điều kiện kích hoạt khớp tệp thật; không trùng chức năng với skill khác;
chứa lệnh thực thi → rủi ro cao (`--confirm-risk`). Đề xuất tạo/sửa skill tối thiểu rủi ro trung bình.

**Điều kiện kích hoạt** (`bint skill check`): mẫu đường dẫn còn khớp tệp; kích hoạt được ở ≥ 50% lượt
thành công làm căn cứ (không quá hẹp); không kích hoạt ở > 35% lượt có nhãn không liên quan (không quá
rộng, cần ≥ 3 lượt để kết luận); mô tả không trùng skill khác.

**Đối chiếu**: tác vụ chịu ảnh hưởng là tác vụ thoả điều kiện kích hoạt của skill bị thay đổi (tệp hoặc
từ khoá trong đề bài). Skill được đặt vào thư mục làm việc của cả hai nhánh (`.claude/skills` và
`.agents/skills`, đúng định dạng từng nơi), cùng một tập tri thức. Mọi skill đặt vào phải áp dụng được tại
commit gốc: bằng chứng khớp và nội dung không chứa mã lời giải (skill của đề xuất không thoả → tác vụ bị
loại; skill khác không thoả → bỏ khỏi cả hai nhánh). Tạo/sửa: tốt hơn trên thước đo chọn trước;
gộp/loại bỏ: không kém về tỷ lệ đạt. Nguồn ngữ cảnh ghi `skills[tên@băm]` để kiểm toán.

**Hợp nhất** giữ skill có phiên bản cao hơn, hợp điều kiện kích hoạt và bằng chứng, thêm vào cuối mọi
dòng của skill kia chưa có — không mất kiến thức; skill bị gộp thành `deprecated` và ghi `merged_from`.

**Phiên bản gắn mô hình** (mục 12.6): `skill.yaml` ghi từng phiên bản kèm băm, đề xuất, harness, mô
hình, kiểm chứng hay chưa, lần đối chiếu và người duyệt; `versions/N.md` giữ ảnh chụp để `bint skill
diff` xem bản thay đổi. Khi mô hình hiện dùng khác mô hình đã kiểm chứng, `bint skill regress` đánh dấu
"chưa xác thực với mô hình hiện tại" (kết quả cũ không là mốc cho mô hình mới) và
`evolve evaluate --regression` chạy lại tối đa 5 tác vụ đại diện (chính các tác vụ đã làm căn cứ, `base` =
tập skill trước đề xuất dựng lại, `candidate` = hiện tại). Lần chạy này ghi `kind = regression`, không bao
giờ dùng làm căn cứ áp dụng; kết quả: `revalid` (vẫn có lợi), `regressed` (không còn bằng chứng có lợi),
`inconclusive`. Bint chỉ báo cáo; không tự gỡ skill.

**Làm việc nhóm** (mục 8.7): skill và `skill.yaml` được commit; đề xuất ghi băm `SKILL.md` + `skill.yaml` làm
phiên bản gốc và mất hiệu lực nếu đổi. Hai thay đổi cùng sửa một skill rồi merge (nội dung không khớp
phiên bản ghi trong `skill.yaml`, dấu xung đột, thiếu ảnh chụp) → `bint doctor` báo lỗi để xử lý thủ
công; Bint không tự hợp nhất. `bint sync` xuất bản skill còn hiệu lực cho harness và gỡ bản của skill bị
loại bỏ, xoá hoặc lỗi thời (bản đã bị sửa tay không bao giờ bị xoá; ghi vào `core.lock`, mục `evolved`).

Giới hạn đã biết của P7:

- Skill mới/sửa có hiệu lực ngay khi người áp dụng (người áp dụng là người duyệt): hãy đọc `SKILL.md` ở
  `evolve show` — skill là chỉ dẫn hành vi do agent soạn từ dữ liệu không đáng tin.
- Sự kiện `skill.activated` chỉ có ở Claude Code (hook `PostToolUse` của công cụ Skill). Codex và
  Antigravity chọn skill ngầm, không có công cụ gọi skill đi qua hook: không đo được, và Bint không tổng
  hợp giả (khả năng `skill_events` trong bảng khả năng ghi rõ). Cơ hội `skill_ignored` chỉ dựa vào lượt
  của Claude Code.
- Vòng đời đã kiểm thử đầu-cuối với agent giả (`TestEvaluateSkillProposal`) và chạy thực nghiệm tại
  [docs/evals/p6-p7-claude-2026-11-10/README.md](evals/p6-p7-claude-2026-11-10/README.md), nhưng chưa có
  skill được chứng minh có lợi trên tập `test` độc lập.

## Chế độ thực thi tiến hoá: Deferred (mục 13.3–13.4)

`evolution.execution: deferred` — khi đủ bằng chứng mới Bint **đánh dấu yêu cầu tiến hoá** (tệp trong
`<state>/runtime/evolve/requests`) và xử lý ở **lần gọi CLI phù hợp tiếp theo** (`bint status` khi có sự kiện
mới hoặc còn yêu cầu chờ; hoặc `bint evolve process`). Hook không bao giờ chạy vòng gọi mô hình.

```sh
bint evolve queue --all                  # yêu cầu đã đánh dấu: pending | done | skipped
bint evolve process --budget 2 --max 2   # đánh dấu + xử lý ngay (người dùng)
```

Mỗi lần xử lý: kiểm tra khoá tiến hoá (đang bị giữ → không làm gì, yêu cầu vẫn chờ) và ngân sách
(`evolution.deferred.budget_usd`; `null` = chỉ yêu cầu không cần mô hình: loại bỏ, gộp, skill lỗi thời; yêu cầu cần
agent evolver chờ ngân sách), tối đa `evolution.deferred.max_per_run` yêu cầu, rồi tạo **đề xuất** qua cùng đường
với `bint evolve propose`. Không áp dụng gì (chế độ `propose`). Cơ hội không còn sẵn sàng lúc xử lý (đã có đề xuất,
điều kiện đổi) → `skipped` kèm lý do. Không xử lý trong phiên agent (đề xuất tiến hoá chỉ dành cho người dùng).
Managed (tiến trình nền được cài đặt riêng) chưa có.

## Vòng tự động: `evolve cycle` (P8)

`bint evolve cycle` chạy trọn một chu trình tiến hoá có ngân sách (không phải tiến trình nền):

```sh
bint evolve cycle --budget 20 --max 2 --parallel 3   # quét → đề xuất → đối chiếu → áp dụng rủi ro thấp
bint evolve cycle --budget 10 --no-propose --json    # chỉ đối chiếu các đề xuất đang chờ
```

Thứ tự thực hiện trong một vòng:
1. **Kiểm tra hồi quy (`regress`):** khi mô hình hiện dùng khác mô hình đã kiểm chứng, chạy lại tập đại
   diện; đề xuất từng do Bint tự áp dụng bị hồi quy sẽ được tự động `rollback`, còn đề xuất do người
   duyệt chỉ được báo để người dùng quyết định.
2. **Tạo đề xuất (`propose`):** dành tối đa `--propose-budget` (mặc định $1) để soạn đề xuất mới từ các
   cơ hội đủ điều kiện (bỏ qua khi có `--no-propose`).
3. **Đối chiếu (`evaluate`):** chạy Probe tuần tự trên `validation` rồi `test` (song song tối đa
   `--parallel` lượt). Kết quả phải mới hơn lần áp dụng/quay lui gần nhất; đề xuất `not_supported` bị tự
   bác bỏ (`--keep-failed` để giữ). Khi ngân sách còn lại dưới $1 thì không mở Probe mới.
4. **Áp dụng (`apply`):** xếp hạng các đề xuất `supported` độc lập trên `test` theo mức tăng tỷ lệ đạt.
   Nếu `evolution.adoption.auto_low_risk: true`, tự áp dụng tối đa **một** đề xuất rủi ro thấp chỉ tạo
   mục tri thức mới (mục vẫn ở trạng thái `candidate`); đề xuất skill và sửa/gộp/loại bỏ mục có sẵn luôn
   chờ người duyệt. Các đề xuất còn lại phải đối chiếu lại trên nền mới ở vòng sau.

`--model` trong `evolve cycle` áp cho cả evolver lẫn các lượt đối chiếu; reflector rút bài học dùng mô
hình riêng tại `harnesses.claude.models.reflector` (hoặc tách bước bằng `evolve propose --reflector-model`
rồi `evolve evaluate --model`). Báo cáo từng vòng được lưu tại `<state>/runtime/evolve/cycles/<id>.json`
(tách chi phí tạo đề xuất và chi phí Probe, danh sách tự áp dụng, tự quay lui, chờ duyệt và lý do dừng).

### Đề xuất thành phần quy trình (kế hoạch H3–H4)

Ngoài tri thức, bài học và skill, `bint evolve` còn đề xuất thay đổi **thành phần quy trình** (chỉ dẫn
công cụ, guardrail, script hỗ trợ, chiến lược ngữ cảnh) lưu ở `.bint/harness/`; cách dùng và định dạng
tệp xem [harness-components.md](harness-components.md).

- **Cơ hội `harness`:** nhóm lượt thất bại validation theo dấu hiệu hành vi (`no_test`/`edit_outside` →
  guardrail, `repeat_cmd`/`timeout` → chỉ dẫn, `ignored_ctx` → chiến lược; thêm `harness:mcp_unused`).
- **Đề xuất `target_type: harness`** (create/update/deprecate) do agent theo `roles.evolver` viết, không
  công cụ, thư mục làm việc rỗng, chỉ claude. Rủi ro do Bint tính: guidance/`remind` thấp; `ask`/strategy
  trung bình; `deny`/script cao.
- **Chung và riêng:** mỗi cơ hội cho tối đa hai đề xuất anh em; `bint evolve cycle` giữ một nửa theo
  `non_inferiority_margin` và không gộp hai bản. `bint evolve evaluate` buộc `--delivery bint` cho đề xuất
  quy trình.
- **Áp dụng:** chỉ tự áp dụng `create` rủi ro thấp (khi bật `auto_low_risk`), thành phần vẫn vào ở
  `candidate` chờ người duyệt (`bint harness component approve`).

Mã và kiểm thử xác định đã xong; phần đo (H5, tốn tiền) **chưa chạy**. Xem
[Kế hoạch H](p8-harness-evolution-plan.md).
