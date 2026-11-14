# Khung đánh giá (P1)

Đo xem một thay đổi (tri thức, skill, quy trình) có thực sự giúp agent trên **chính
repository này** hay không (mục 12 của [đặc tả thiết kế](bint-plan.md)). Người dùng cuối chỉ cần phần này khi
bật mức 3 (tiến hoá có kiểm chứng).

```sh
# 0. Xác nhận lệnh kiểm thử (Bint không bao giờ chạy lệnh đề xuất khi chưa được xác nhận)
#    .bint/config.yml hoặc .bint/local.yml:
#      eval:
#        test_command: "go test {test_dirs}"   # {test_dirs}/{test_files} = kiểm thử của tác vụ
#        setup_command: "go mod download"      # tuỳ chọn, chạy trước agent ở mỗi lượt
bint eval mine --verify          # khai thác commit có kiểm thử fail→pass làm ứng viên
#   (hoặc `bint eval mine --from-tasks --verify`: dựng ứng viên từ phiếu b-do đã done)
bint eval show <id>              # đọc đề bài; sửa `prompt` trong tệp ứng viên nếu cần
bint eval approve <id>           # duyệt vào .bint/evals/cases/ (nhận một hoặc nhiều ID, commit được)
bint eval bootstrap --budget 5   # nhánh minimal (strict): dựng tri thức tại commit gốc từng tác vụ
bint eval estimate --effect 0.1  # số lượt, chi phí, cỡ mẫu cần thiết
bint eval run --arms none --budget 20     # chạy; Ctrl-C an toàn, --resume <batch> để chạy tiếp
bint eval report --cases         # so sánh theo cặp + khoảng tin cậy 95% (thêm --json nếu cần)
```

**Khai thác (`mine`).** Chọn commit có một commit cha, thêm/sửa kiểm thử *và* sửa mã nguồn,
quy mô trong `eval.mine.*`, mô tả đủ dài. Đề bài được viết lại tự động từ thông điệp commit
(bỏ trailer, khối mã, diff, tham chiếu commit lời giải) rồi đối chiếu với lời giải:
`fail` nếu chứa nguyên dòng mã hoặc SHA của lời giải; `warn` nếu chứa chú thích của lời giải
hay định danh mới mà kiểm thử không dùng tới. `--verify` (hoặc `bint eval verify`) xác nhận
kiểm thử của commit **thất bại** ở commit cha và **qua** (2 lần, bắt kiểm thử chập chờn) với
lời giải thật; ứng viên không đạt bị loại. Ứng viên chứa thông điệp gốc nên chỉ nằm trong thư
mục trạng thái; `approve` từ chối đề bài `fail` (trừ `--force`) và tác vụ chưa kiểm chứng (trừ
`--unverified`), ghi người duyệt và gán tập `build/validation/test` (50/25/25, tất định theo ID).

**Khai thác từ phiếu (`mine --from-tasks`).** Thay vì `git log`, quét phiếu workflow `b-do` ở trạng thái
`done` và dựng ứng viên từ yêu cầu, tiêu chí chấp nhận và lời giải của phiếu. Lời giải là cây lúc đưa về cây
gốc (`landed.tree`) hoặc cây của lần kiểm chứng qua cuối cùng, giới hạn trong tập tệp của phiếu; Bint đóng
băng nó thành hai commit tổng hợp (gốc = cây trước `b-feat` đầu tiên, lời giải = gốc + tệp của phiếu), dựng
tất định và giữ sống bằng ref `refs/bint/eval/<id>/base` và `.../commit` (chỉ có trong kho cục bộ: đề này
không chia sẻ được giữa các máy cho tới khi phiếu xuất được vào Git). Từ đó ứng viên đi qua đúng đường ống của
đề từ Git:

- ID là chữ `t` nối ID phiếu viết thường (ổn định, chạy lại không tạo trùng); `source.task` và
  `source.task_runs` ghi nguồn. Phiếu không dựng lại được lời giải (cây đã bị `git gc` dọn, không có tệp)
  hoặc không qua bộ lọc `eval.mine.*` (không có kiểm thử, không có mã nguồn, quá nhỏ/lớn) được đếm trong
  `Bỏ qua` kèm lý do.
- Đề bài là yêu cầu (hoặc mục tiêu) cộng tiêu chí chấp nhận, làm sạch như thông điệp commit rồi qua `spec` và
  kiểm tra rò rỉ; `original_message` (chỉ trong ứng viên) chứa yêu cầu, tiêu chí và `requirement.md` cho người
  duyệt và bị xoá khi `approve`. Tiêu chí do `b-req` viết có thể nêu tên ký hiệu mới: xem `leak_check`.
- `test_command` là `verify.command` của phiếu khi đã được người xác nhận (hoặc trùng `eval.test_command`);
  chưa xác nhận thì để trống để dùng `eval.test_command`. Lệnh của phiếu thường rộng hơn `{test_dirs}`;
  người duyệt sửa được trong tệp ứng viên (đổi fingerprint).
- `verify`/`approve` như đề từ Git; tập con mặc định là `AssignSplit(id)` (`build` là "train" của tài liệu
  hướng đi).
- Chống rò rỉ (mục 12.3): khi chấm đề có `source.task`, mục tri thức có `origin.run_id` thuộc lượt của phiếu
  hoặc nêu ID phiếu trong tiêu đề/nội dung bị loại ở cả chế độ `economy` và `strict`.
- Không có hai đề cùng thay đổi kiểm thử: `--from-tasks` bỏ phiếu trùng đề từ Git đã có, và `mine` từ Git
  bỏ commit trùng đề từ phiếu đã có (đếm trong `Bỏ qua`).

**Đặc tả đề bài (`mine`, `bint eval spec`).** Đề bài từ tiêu đề commit thường không nói tên
hàm/lớp/hằng mới mà kiểm thử ẩn gọi tới; agent không đoán được nên tác vụ không giải được ở mọi
nhánh. `mine` nối vào cuối đề bài khối *"Interface the hidden tests will use"*: đường dẫn và **chữ
ký** (không thân hàm, không giá trị hằng) của định danh vừa mới trong lời giải vừa được kiểm thử
dùng tới; kiểm tra rò rỉ bỏ qua đúng khối này. Cảnh báo thiếu đặc tả (cột ĐẶC TẢ, `show`,
`approve`): `interface_gap` — định danh mới với cả repository mà không trích được chữ ký (khoá
chuỗi, thuộc tính…); `exact_text` — kiểm thử so khớp nguyên văn chuỗi mới mà đề bài không nêu
(cụm có khoảng trắng ≥ 12 ký tự, hoặc chuỗi ngắn "đặc thù" ≥ 3 ký tự: chữ hoa liên tiếp, ký hiệu
hay không phải ASCII, mà tệp lời giải đụng tới chưa có ở commit gốc; nội dung chuỗi chỉ được cảnh
báo, không bao giờ vào đề bài); `short_prompt` — đề bài < 100 ký tự. Khối giao diện còn ghi
**tên** (không giá trị) của hợp đồng ẩn khác: `CLI flag: --from` (cờ test kiểm mà lời giải thêm),
`Config key (path): qa_skills` (khoá trong tệp yml/yaml/json/toml lời giải thêm), `Data key: __v`
(khoá dict/chỉ số trong mã lời giải thêm), và thuộc tính mô-đun bị `monkeypatch.setattr` /
`mock.patch` / `patch.object` thay thế (đi qua cơ chế chữ ký, không trích được thì `interface_gap`).
Mỗi gợi ý có `kind` (`signature` mặc định/`flag`/`config_key`/`data_key`). `bint eval spec [id…|--all]` áp lại cho ứng viên cũ
(`--approved` cho tác vụ đã duyệt: đổi fingerprint).

**Cô lập (`run`).** Mỗi lượt là một repository Git **mới** chỉ chứa lịch sử tới commit gốc
(`git fetch <repo> <sha>` vào kho đệm, rồi clone cục bộ, gỡ remote) — không dùng
`git worktree` vì worktree dùng chung kho đối tượng và agent có thể `git show` commit lời giải.
Mọi tệp ngữ cảnh agent (`AGENTS.md`, `CLAUDE.md`, `.claude/`, `.bint/`, `.cursor/`…) bị gỡ và
commit. Agent làm xong → tệp kiểm thử của commit mới được áp vào → `test_command` quyết định
đạt/không. Thư mục làm việc bị xoá sau mỗi lượt (`--keep-workdirs` để giữ); log ở
`<state>/eval-work/logs/<batch>/`.

**Nhánh.** `none`: chỉ harness. `minimal`: thêm `AGENTS.md` dựng từ tri thức
(`eval.minimal_source: knowledge`, mặc định từ P2) hoặc từ tệp viết tay `eval.minimal_context`
(`minimal_source: file`). `bint` (P3): cùng `AGENTS.md` và **cùng tập tri thức** với `minimal`, thêm
MCP server `bint` và hook chèn ngữ cảnh theo khả năng của harness, chạy
ngoài thư mục làm việc trên bản sao tri thức; truy xuất/trích dẫn ghi vào `eval_context_usage`.
`ideal` (chỉ để chẩn đoán, mục 12.9): `AGENTS.md` + mọi mục có phạm vi chồng lên tệp của lời giải
thật — cận trên, không phải kết quả của Bint. `bint`/`ideal` cần `minimal_source: knowledge`.

**Biến thể ngữ cảnh (`bint@…`).** Nhánh `bint` nhận thêm tham số sau dấu `@` (nối bằng `+` vì
`--arms` phân cách nhánh bằng dấu phẩy) để bật/tắt từng kênh ngữ cảnh (`task=on|off`, `path=on|off`,
`mcp=on|off`), chọn tập thành phần quy trình (`harness=off|on|all`) hoặc giới hạn ngân sách
(`tokens=<N>`, `items=<N>`):

```sh
bint eval run --arms 'none,minimal,bint,bint@path=off+tokens=3000+items=3' --budget 20
bint eval report --baseline minimal --cases
```

`task` điều khiển chèn theo đề bài (`UserPromptSubmit`), `path` điều khiển chèn theo đường dẫn
(`PreToolUse`), `mcp` điều khiển MCP server. `harness=off` chỉ dùng guidance dựng sẵn, `on` (mặc định)
dùng thành phần `verified` trong `.bint/harness`, `all` thêm cả `candidate` (ví dụ
`bint eval run --arms bint,bint@harness=all`); báo cáo có thêm bảng số liệu từng guardrail theo nhánh
(bảng `eval_guardrail_usage`, migration 0009). Xem [harness-components.md](harness-components.md). Hai nhánh nội bộ `base` và `candidate` chỉ dùng trong
`bint evolve evaluate` ([docs/evolution.md](evolution.md)).

**Chống rò rỉ.** `strict`: nhánh `minimal` chỉ dùng tri thức dựng **tại commit gốc** — ảnh chụp do
`bint eval bootstrap` sinh (chạy `b-bootstrap` bằng agent trong repository cô lập tại commit gốc,
lưu ở `<state>/eval-work/knowledge/<sha>/`), hoặc `.bint/knowledge/items` đã commit tại commit
gốc (với `file`: phiên bản tệp tại commit gốc); không có thì lượt bị `skipped`. `economy`: tri thức
hiện tại, chỉ giữ mục có mọi bằng chứng tồn tại và khớp fingerprint tại commit gốc, commit bằng
chứng là tổ tiên của commit gốc, không rút ra từ chính tác vụ, và nội dung không chứa mã của lời
giải; báo cáo đánh dấu kết quả chỉ để sàng lọc.

**Kho đệm commit gốc.** Mỗi base được nạp vào một kho trần riêng (`<state>/eval-work/bases/<sha>.git`,
chỉ chứa lịch sử tới commit gốc, không có alternates) rồi clone cho từng lượt. Kho được tạo lười,
dọn ngay khi lượt cuối dùng base xong (`eval.keep_bases: true` để giữ, kèm `eval.base_cache_max_mb`
để giới hạn theo LRU), và `bint eval clean` xoá phần còn sót. Trước khi nạp, Bint kiểm tra đĩa còn
ít nhất `eval.min_free_mb` (mặc định 1024) và dừng có thông báo nếu thiếu. `eval.base_depth: N` nạp
nông; mặc định 0 = đầy đủ lịch sử, để số liệu so sánh được với các đợt cũ.

**Ngân sách.** Bắt buộc với Claude Code (`--budget` hoặc `eval.budget_usd`). Ước tính =
tác vụ × nhánh × lần lặp × chi phí trung bình mỗi lượt từ lịch sử (hoặc `--cost-per-run`);
vượt ngân sách thì từ chối chạy. Khi chạy, lượt mới không bắt đầu nếu đã tiêu + chi phí dự
kiến vượt ngân sách; khi chưa lượt nào đo được chi phí thì dùng giá trị tiên nghiệm
`eval.prior_run_usd` (mặc định $0,50; 0 = tắt), từ lượt thật đầu tiên dùng chi phí đo được
và thông báo nêu rõ lý do dừng (trạng thái `budget_exhausted`, chạy tiếp bằng `--resume <batch> --budget
<tổng mới>`). `limits.per_run.max_cost_usd` → `claude --max-budget-usd` cho từng lượt.

**Báo cáo.** Tỷ lệ đạt mỗi nhánh (trung bình theo tác vụ, CI95 bootstrap 10 000 lần, hạt giống
cố định); so sánh theo cặp với mốc `none` (hoặc `--baseline`) trên cùng tác vụ cho tỷ lệ đạt, chi phí, thời gian,
số lượt agent; độ dao động giữa các lần lặp (độ lệch chuẩn trong tác vụ, số tác vụ không nhất
quán). Phần "độ tin cậy" chẩn đoán nhóm đầu của mục 12.9: CI quá rộng, mô hình/harness đổi
giữa chừng, lỗi hạ tầng > 10%, tác vụ bị sửa sau khi chạy, tác vụ chưa kiểm chứng, economy,
trần/sàn tỷ lệ đạt. `estimate` tính cỡ mẫu theo cặp (alpha 0.05, power 0.8) với độ lệch chuẩn
đo từ các đợt trước, hoặc 0.5 khi chưa có dữ liệu.

**Harness.** `eval.harness` / `bint eval run --harness` nhận `claude | codex | antigravity`:
- **Claude Code:** chạy `claude -p --output-format json --no-session-persistence --strict-mcp-config
  --permission-mode acceptEdits --allowedTools … --setting-sources project,local` (cấu hình ở
  `eval.agent`), đọc chi phí, token, số lượt và mô hình từ JSON.
- **Codex CLI:** chạy `codex exec --json` trong profile tạm chỉ chứa runtime/provider cần thiết và
  bản sao `auth.json` (`0600`), tắt toàn bộ skill người dùng/admin/bundled và không mang `AGENTS.md`,
  plugin hay hook cá nhân sang (xoá profile cả khi timeout/huỷ; thiếu `auth.json` dạng tệp thì từ
  chối thay vì quay về thư mục người dùng thật). Nhánh `bint` gồm MCP + `AGENTS.md` + hook trước
  `apply_patch` (cần duyệt trust).
- **Antigravity CLI:** nhánh `bint` bị bỏ qua (`skipped` kèm lý do) vì chưa có MCP cấp dự án. Với
  Codex và Antigravity (không báo chi phí USD), ngân sách USD chỉ là chỉ dẫn.
- **Tác nhân tuỳ chỉnh:** `--agent-command "<lệnh>"` truyền đề bài qua stdin và
  `$BINT_EVAL_PROMPT_FILE`; nếu stdout là JSON cùng định dạng thì số liệu được đọc.

Giới hạn đã biết của P1:

- Agent chạy với quyền của người dùng; mạng **không** bị chặn ở tầng hệ điều hành
  (`security.allow_network_in_evals` chỉ là chỉ dẫn). Mỗi đợt ghi lưu ý này vào báo cáo.
- Không có `--bare`, Claude Code vẫn có thể nạp `~/.claude/CLAUDE.md` của người dùng ở mọi
  nhánh (như nhau giữa các nhánh, nhưng không phải "không ngữ cảnh" tuyệt đối).
  `--bare` cô lập nguồn ngữ cảnh cấp người dùng nhưng cần `ANTHROPIC_API_KEY`.
- Agent mất lịch sử Git sau commit gốc (đúng ý) nhưng cũng không có remote/tag.
- Đề bài nháp lấy từ thông điệp commit; commit có mô tả sơ sài cho đề bài sơ sài — người
  duyệt nên viết lại trước khi `approve`.

Kết quả lưu trong SQLite (`eval_batches`, `evaluation_runs`, migration 0002); tác vụ đã
duyệt là nguồn chuẩn trong `.bint/evals/cases/*.yml` (fingerprint của đề bài + lệnh chấm
được ghi vào mỗi lượt để phát hiện tác vụ bị sửa). Giao thức đã chốt của hai cổng ở
[docs/gate-1.md](gate-1.md) và [docs/gate-2.md](gate-2.md); kế hoạch các vòng nghiệm thu ở
[docs/acceptance.md](acceptance.md); các đợt đo thực nghiệm đã lưu tại [docs/evals/README.md](evals/README.md).

## Kiểm thử skill (`bint skill test`) khác `bint eval run`

`bint skill test <tên>` (GĐ6 của `docs/custom-flow-plan.md`) không dùng bộ tác vụ, nhánh Bint hay worktree của `eval run`: nó chạy
kịch bản áp lực `.bint/skills/<tên>/tests/*.yml` của một skill tuỳ biến qua `eval.Agent` chỉ đọc (role `skill-tester`, mặc định theo
`planner`), ở nhánh không và có skill trong đề bài, chấm bằng chuỗi/regex trên câu trả lời cuối rồi ghi tỉ lệ tuân thủ mỗi nhánh vào
`<thư mục trạng thái>/runtime/skill-tests/<tên>/<id>.json`. Cùng quy tắc chi phí như `eval run`: in ước tính trước, cần `--yes` khi
không tương tác. Định dạng kịch bản và giới hạn: `docs/workflows.md` mục "Kiểm thử skill".
