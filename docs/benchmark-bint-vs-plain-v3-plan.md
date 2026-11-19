# Kế hoạch: benchmark Bint so với không dùng Bint, lần 3 (đo lại sau khi sửa luồng b-do cho người dùng mơ hồ)

Trạng thái: **đã duyệt 2026-11-16** (mục 11), sẵn sàng chạy. Agent chạy kế hoạch này không phải agent đã viết nó: mọi đường dẫn, lệnh và
bẫy đã biết đều ghi ở đây; đọc hết trước khi làm.

Tài liệu gốc (đọc trước):
- Plan lần 2: [benchmark-bint-vs-plain-v2-plan.md](benchmark-bint-vs-plain-v2-plan.md). Lần 3 giữ nguyên task, prompt, thẻ
  người dùng, kịch bản test ẩn và giao thức của lần 2, trừ các điểm ghi ở mục 2.
- Biên bản lần 2: [evals/bench-bint-vs-plain-v2-20261116/README.md](evals/bench-bint-vs-plain-v2-20261116/README.md).
- Plan lần 1 (cách gỡ Bint cho bên A, mục 3): [benchmark-bint-vs-plain-plan.md](benchmark-bint-vs-plain-plan.md).
- Workspace lần 2 (script, test ẩn, đối chứng dùng lại được): `/home/baodq/bench-bint-v2-20261116/`.

## 1. Vì sao có lần 3

Lần 2 ra *Có lợi, đánh đổi*: T1 hoà, T2 B hơn đúng 1 critical; B tốn 2,1–2,5× token vào và 3,5–4,7× thời gian. Phân tích sau đó
cho thấy B không hơn vì Bint bỏ chính bước nó mạnh nhất:

1. Classify xếp hai câu mơ hồ ngắn là "rõ" nên **bỏ b-req**; không ai liệt kê các nơi bị ảnh hưởng (với T2: mô tả phiếu,
   review.md, `.bint/tasks`, SQLite — đúng những nơi cả hai bên bỏ sót).
2. b-review chỉ soi diff, không tìm chỗ **bỏ sót**.
3. Cổng done cho qua khi **diff rỗng** (B-T1 lần đầu); câu hỏi của b-feat qua `bint task error` bị bỏ qua.
4. Hết vòng sửa thì blocked "cần người xem review.md", **không có lối ra** cho người low-tech (B-T2 kẹt).
5. Vòng sửa không nối tiếp phiên b-feat, review tự dò lại mã: tốn token.

Các điểm này đã sửa trên `main` của repo chính `/mnt/01DCE9A4D6522BD0/Project/Go/bint`:

| Commit | Nội dung |
|---|---|
| `aba67fb`, `dd6541d` | Merge `fix/bench-frictions` (4 điểm vướng của lần 1, đã dùng ở lần 2) + sinh lại golden |
| `9e1296d` | Classify mỏ neo cụ thể; skill b-req "Bề mặt bị ảnh hưởng" + "Giả định thay người dùng"; b-review tìm chỗ bỏ sót, đề bài có sẵn diff + plan; cổng không đổi tệp; `task error --question` + `run resume --answer`; hết vòng sửa in `--accept` / `--fix-round` / `task cancel`; vòng sửa nối tiếp phiên b-feat |
| `f816de5`, `8757340` (sync skill dự án + mẫu config) | Thu hẹp mỏ neo Classify; câu hỏi thiếu `--step`; trạng thái chờ người vào `.bint/tasks`; dấu vân tay nối tiếp; rào ``` trong đề bài review |

**Câu hỏi của lần 3:** với cùng người dùng mơ hồ và cùng task rủi ro, Bint đã sửa có để lại **ít lỗi critical/medium hơn rõ
rệt** so với Codex trực tiếp không, và chi phí thêm là bao nhiêu?

## 2. Khác lần 2 ở đâu

| | Lần 2 | Lần 3 |
|---|---|---|
| Commit gốc | `ff262a2` (+ commit `bench:` sửa baseline) | `8757340` trên `main` (đã duyệt: có bộ skill mới). Kiểm baseline xanh, xem mục 4 |
| Số lượt | 1 lượt mỗi task mỗi bên (4 lượt) | Như lần 2: 1 lượt mỗi task mỗi bên (đã duyệt) |
| Song song | 4 lượt cùng lúc | Như lần 2, nhưng cần ≥ 30 GB trống trước khi chạy (mục 4) |
| Lựa chọn khi hết vòng | Không có (kẹt) | Người low-tech chạy lệnh Bint in ra theo quy tắc mục 6.2 |
| Câu hỏi của b-feat | Không có cơ chế | Người trả lời bằng lệnh `--answer` Bint in ra, tính là tin nhắn người dùng |
| Bảng kết luận | Theo 1 lượt | Như lần 2, chỉ nới ngưỡng tốn kém (mục 8) |

Giữ nguyên: Codex `gpt-6.1-sol` (qua codex-lb) cho mọi bước của cả hai bên; trần 90 phút mỗi lượt; Opus điều phối; prompt,
thẻ người dùng, test ẩn, review đối kháng mù + xác minh mù; cách đo token.

## 3. Hai task (y như lần 2)

- **T1** prompt: "Thư mục dữ liệu của bint trên máy tôi cứ phình to mãi, làm cho nó tự dọn bớt mấy thứ cũ đi."
- **T2** prompt: "Tôi sợ mật khẩu với token bị lưu lại trong log của bint, xử lý giúp tôi."
- Thẻ người dùng: chép nguyên `evals/bench-bint-vs-plain-v2-20261116/log/persona-cards.md`.
- Đặc tả rủi ro cho review: chép nguyên `evals/bench-bint-vs-plain-v2-20261116/review/risk-spec.md`.

Kiểm trước khi chạy: ở commit gốc mới, T1 và T2 **vẫn chưa được làm** (không có lệnh/cơ chế dọn dữ liệu; `detect.Redact` chưa
áp cho log kiểm chứng, log lượt chạy, review, mô tả phiếu). Nếu đã có thì dừng, báo chủ dự án.

## 4. Chuẩn bị

Workspace mới: `/home/baodq/bench-bint-v3-<YYYYMMDD>/`, cấu trúc như lần 2 (`bin/`, `tools/`, `hidden/`, `log/`, `results/`,
`review/`, `blind/`, `tmp/`, mỗi lượt một repo `A-T1`, `A-T2`, `B-T1`, `B-T2`).

1. **Đĩa.** Lần 2 đầy ổ `/` giữa chừng làm kiểm chứng của B-T2 đỏ giả. Cần **≥ 30 GB trống** trước khi chạy (4 repo + build
   cache + worktree). Không đủ thì dừng, báo chủ dự án; **không tự xoá** dữ liệu hay cache của người dùng.
2. **Repo.** Clone 4 bản từ repo chính ở commit gốc (`git clone --no-local` rồi `checkout <sha>`).
   - Bên A: gỡ Bint như plan lần 1 mục 3 (`.bint/`, `.agents/`, `.claude/`, `.codex/`, `.mcp.json`, `AGENTS.md`, `CLAUDE.md`,
     khối `# bint:begin … # bint:end` trong `.gitignore`), commit "gỡ Bint". Sửa test hỏng do gỡ (lần trước: `TestDocCommandsExist`)
     trong cùng commit.
   - Bên B: `bint init --hooks` bằng binary đóng băng, index codegraph, skill bản của commit gốc.
   - `go test ./...` với `TMPDIR` riêng phải **xanh trên cả 4 bản**. Đỏ có sẵn thì thêm một commit `bench:` giống hệt nhau
     cho mọi bản, lưu patch vào `log/baseline-fix.patch`, ghi vào biên bản.
3. **Binary đóng băng** `bin/bint` build từ commit gốc; ghi sha256. Không bao giờ ghi đè nó (lần 2 suýt ghi đè khi thử
   `go build -o "$(command -v bint)"` trong bước thử dùng — xem mục 6.3).
4. **Codex trust.** Thêm khối có đánh dấu `# bench-bint-v3-<ngày> (gỡ sau benchmark)` vào `~/.codex/config.toml`, sao lưu tệp
   trước (`log/codex-config.toml.before-bench`). Trust 4 project và hook của 2 repo B. Băm hook lấy bằng
   `codex --no-daemon app-server --stdio` gửi `hooks/list`, đọc trường `currentHash` (làm như lần 2). Khối lần 2
   (`bench-bint-v2-20261116`) vẫn còn trong tệp; để nguyên trừ khi chủ dự án bảo gỡ.
5. **Script** chép từ `/home/baodq/bench-bint-v2-20261116/tools/` rồi đổi `BENCH`/`B` sang workspace mới: `human`, `runA.sh`, `runB.sh`, `hiddenrun.sh`, `usershell.sh`, `tokens.py`, `mkdiff.py`, `waitend.sh`.
   - Script chạy nền phải `export PATH=$BENCH/bin:$BENCH/tools:$PATH` (lần 2 lỗi "human not found").
   - Mỗi lượt `TMPDIR=$BENCH/tmp/<lượt>-run` riêng cho cả hai bên.
6. **Test ẩn**: chép `hidden/hidden2.py` và `hidden/controls/` từ workspace lần 2 (hoặc từ
   `evals/bench-bint-vs-plain-v2-20261116/hidden/`). Script không có đường dẫn cứng.
7. **Đối chứng âm trên binary gốc mới**: chạy lại `noop`, `naive`, `lockhog` (`hidden/controls/`) và T2 với binary gốc. Kết quả
   phải như lần 2 (`results/controls/` của lần 2 để so). Chú ý hành vi mới có thể làm fixture lệch:
   - **Cổng không đổi tệp:** bước implement không đổi tệp nào giờ dừng `blocked` trước kiểm chứng. Fixture nào dựng bằng
     `bint run b-feat --agent-command <agent giả>` (S2, D5) mà agent giả không ghi tệp có thể không đi tới bước cũ. Nếu vậy,
     cho agent giả ghi thêm một tệp vô hại trong repo fixture, áp cho **mọi bên**, ghi vào `log/fixture-changes.md`.
   - **Classify mới:** fixture tạo phiếu bằng `bint task new "<câu ngắn>"` có thể giờ cần b-req. Fixture không chạy b-do nên
     thường không ảnh hưởng; nếu có, truyền `--no-plan`.
   - Kiểm fixture: FX-S5 (≥ 2 sự kiện hook có nội dung), các FX-* của T1 phải qua trước khi chạy agent.
8. **`/tmp/.git`**: kiểm và xoá trước và sau mỗi đợt (sandbox Codex tạo lại nó; nó làm hỏng `go test` của mọi repo dưới `/tmp`).

## 5. Test ẩn (y như lần 2)

Kịch bản T1: D1–D9; T2: S1–S6, N1, N2; chung: build, vet, `GOOS=windows go vet`, staticcheck Linux/Windows, `go test ./...`.
Định nghĩa và mức: plan lần 2 mục 5. Lệnh kích hoạt T1 thích nghi: một agent mù nhãn đọc `--help`, tài liệu và diff của từng
bản để điền `trigger.json` (lần 2: `review/T1-X.trigger.json`). T2 chạy với cấu hình mặc định.

N2 (hiệu năng, ngưỡng 3× binary gốc): đo khi máy rảnh (không chạy lượt agent nào), **3 lần xen kẽ** bản gốc/bản kết quả, lấy
trung vị. Lần 2 có ca sát ngưỡng phải đo lại.

## 6. Giao thức chạy

### 6.1. Người dùng low-tech

Y như plan lần 2 mục 6.1 (chỉ đọc lời nhắn cuối; trả lời bằng lời thường theo thẻ; chạy **nguyên văn** lệnh được bảo chạy;
thử dùng một lần khi agent báo xong; không gợi ý kỹ thuật, không đọc mã, không chạy test). Trần: 4 tin nhắn người dùng mỗi
lượt, 90 phút mỗi lượt. Ghi nguyên văn mọi tin nhắn vào `log/user-actions.md`.

### 6.2. Quy tắc mới cho các lối ra của Bint

Bint giờ in lệnh cho người ở ba chỗ. Người low-tech xử lý như sau (giống nhau ở mọi lượt B):

| Bint in | Người làm | Tính là |
|---|---|---|
| b-feat hỏi (`… hỏi người dùng: "…" — trả lời bằng bint run resume <id> --answer "<nội dung>"`) | Trả lời câu hỏi bằng lời thường theo thẻ, chạy đúng lệnh đó với câu trả lời trong ngoặc | 1 tin nhắn người dùng |
| b-feat không đổi tệp nào (`--answer` hoặc `task cancel`) | Chạy `--answer` với câu: "tôi vẫn muốn bạn làm việc này, bạn tự quyết cho an toàn" (kèm ý của thẻ nếu lời nhắn nêu điều b-feat ghi) | 1 tin nhắn người dùng |
| Hết vòng sửa (`--accept` / `--fix-round` / `task cancel`) | Lần đầu: `--fix-round` (người muốn làm cho đúng). Lần hai trở đi: `--accept` | Lệnh chạy nguyên văn, ghi 1 "quyết định" |

Không chạy `--fresh`. Lệnh Bint in ra mà đòi định dạng (ví dụ ghi Markdown theo hợp đồng) vẫn tính "thao tác kỹ thuật"/"kẹt"
như lần 2. Câu hỏi mở của b-req (`## Câu hỏi mở`) trả lời như lần 2 (Opus làm phần định dạng, tính 1 thao tác kỹ thuật).

### 6.3. Thử dùng trên "máy người dùng" giả

Bước thử dùng chạy qua `tools/usershell.sh <lượt> <thư mục> "<lệnh>"`: `PATH`, `BINT_STATE_DIR`, `TMPDIR` riêng của lượt, qua
`human`. Nếu agent bảo cài bằng `go build -o "$(command -v bint)"` hay `go install`, lệnh phải ghi vào `tmp/user-<lượt>/bin`,
không bao giờ vào `bin/` đóng băng hay `~/go/bin`.

### 6.4. Can thiệp vì môi trường

Như lần 2 mục 6.2: lỗi rõ do môi trường (đĩa đầy, `/tmp/.git`, mạng codex-lb) sửa phía môi trường, áp như nhau, ghi vào
`log/env-incidents.md`; không can thiệp vào lượt đang chạy. Lượt bị môi trường làm hỏng rõ ràng (ví dụ kiểm chứng đỏ vì
"no space left on device") thì ghi chú; nếu chủ dự án đồng ý, chạy lại lượt đó và báo cả hai kết quả.

## 7. Cách đo

**Chính:** lỗi critical/medium đã xác minh còn lại ở kết quả cuối của **từng lượt**, gộp từ test ẩn, review đối kháng mù và
xác minh mù, bỏ trùng (y như lần 2 mục 7).

Review mù cho mỗi task nhận **2 diff** gán nhãn ngẫu nhiên X/Y, ánh xạ niêm phong trong
`results/blind-mapping.sealed.json` cho tới khi xác minh xong. Mỗi diff đi kèm binary build từ nó và (T1) `trigger.json`.

**Phụ:**
- Token vào/ra theo rollout Codex lọc theo `cwd` (`tools/tokens.py`), thời gian tường, số tin nhắn, thao tác kỹ thuật, kẹt,
  quyết định.
- **Kiểm giả thuyết của bản sửa** (đọc từ log và `state.json`/`task show` của B, ghi bảng riêng):
  - H1: Classify của B ra "cần b-req" cho cả hai prompt; b-req ghi mục "Bề mặt bị ảnh hưởng".
  - H2: Không lượt B nào `done` với diff rỗng.
  - H3: Không lượt B nào kẹt ở "hết vòng sửa".
  - H4: Vòng sửa của B có nối tiếp phiên b-feat với harness Codex không (notes "vòng sửa chạy với đề bài đầy đủ…" hoặc lời nhắn
    nối tiếp). Lần đầu đo thật cơ chế này với Codex 0.169.
  - H5: Review của B có ghi phát hiện "bỏ sót" (tệp ngoài diff) không, bao nhiêu cái được xác minh.
- Tách token B theo bước (b-req / b-feat / review / vòng sửa) như phân tích sau lần 2 (mỗi bước một phiên Codex; map theo thời
  điểm bắt đầu với `bint run status <id>`).

## 8. Đọc kết quả (chốt trước khi chạy, không đổi sau khi thấy số)

Mỗi task, so số lỗi critical/medium đã xác minh (C = critical, M = medium), như lần 2:

| Kết luận cho một task | Điều kiện |
|---|---|
| B tốt hơn | B có ít critical hơn; hoặc critical bằng nhau và B ít hơn A ít nhất 2 medium |
| A tốt hơn | Ngược lại |
| Hoà | Còn lại |

Kết luận chung (tốn kém = tổng token vào của B chia của A, cả hai task):

| Kết luận | Điều kiện |
|---|---|
| Bint có lợi rõ cho người dùng mơ hồ | B tốt hơn ở cả hai task, tốn kém ≤ 2,5× |
| Có lợi, đánh đổi | B tốt hơn ở một task, hoà ở task kia; hoặc tốt hơn cả hai nhưng tốn > 2,5×. Ghi tỷ lệ |
| Không có tín hiệu | Hoà cả hai, hoặc mỗi bên thắng một |
| Bint bất lợi | A tốt hơn ở ít nhất một task và B không tốt hơn ở task nào |

Ngưỡng tốn kém nới từ 2× (lần 2) lên 2,5× vì b-req giờ luôn chạy cho yêu cầu mơ hồ — đây là quyết định chốt trước, chủ dự án
đã duyệt ở mục 11. So với lần 2 báo thêm: lỗi của B lần 3 so với B lần 2 trên cùng kịch bản (để thấy bản sửa giúp tới đâu).

## 9. Rủi ro và thiên lệch

- **Một lượt.** Critical là biến rời rạc, dao động lớn; kết luận chênh 1 critical là kết luận yếu, ghi rõ trong biên bản.
- **Bản sửa là skill/prompt.** Mục "Bề mặt bị ảnh hưởng" và nhiệm vụ tìm chỗ bỏ sót phụ thuộc model có làm theo; H1/H5 đo trực
  tiếp điều đó.
- **Quy tắc 6.2 có lợi cho B ở ca kẹt cũ.** Lần 2 B-T2 kẹt; lần 3 người có lối ra. Đó chính là điều bản sửa nhắm tới, ghi rõ.
- **Lợi thế tri thức của B** (mục knowledge về fslock, Windows): hợp lệ, ghi lại.
- **Repository đích là mã nguồn Bint**, và bản sửa ở lần 3 chạm chính luồng b-do: B dùng binary đóng băng nên lượt chạy không
  bị ảnh hưởng bởi thay đổi agent đang làm.
- **Tải máy.** 4 lượt song song; N2 đo khi máy rảnh.

## 10. Bẫy đã gặp ở lần 2 (đọc trước khi viết script)

- `bint task show --json`: trạng thái nằm dưới khoá `"state"`; dùng `run_json`/`task_json` có sẵn trong `hidden2.py`.
- Ledger `tasks.ledger: git` chiếu phiếu vào `repo/.bint/tasks/<id>/`: lùi mốc thời gian phải áp cả ở đó rồi commit; D4 loại
  `.bint/tasks` (báo riêng D4i).
- Đổi tên ref/nhánh/worktree khi lùi ULID: đọc HEAD của worktree **trước** khi mở để ghi (lần 2 làm cụt HEAD).
- SQLite `UPDATE` với định danh trong nháy đơn chạy không lỗi mà không đổi gì: dùng nháy kép.
- Phiếu cũ cần `blocked` mà ra `in_review`: ép bằng `bint task status <id> blocked` (qua `human`).
- Hook chỉ ghi lệnh test (qua `detect.Redact`) và lời gọi MCP: payload S5 phải là lệnh test chứa bí mật thô,
  `mcp__bint__report_observation`, `mcp__bint__context_for_task`.
- `created → done` không hợp lệ: qua `in_progress` trước.
- **Không đụng dữ liệu thật** của người dùng: mọi lệnh `bint` ngoài lượt B chạy với `HOME`/`BINT_STATE_DIR` sandbox;
  không xoá gì trong `~/.local/state/bint/` (worktree của tác vụ đang chạy nằm ở đó).
- Thao tác chỉ-dành-cho-người (`approve`, `--accept`, `--fix-round`…) phải qua `tools/human` (gỡ biến môi trường agent).

## 11. Chờ chủ dự án quyết

1. ~~Commit gốc~~ — đã duyệt: `8757340`.
2. ~~Số lượt~~ — đã duyệt: 1 lượt mỗi ô.
3. ~~Quy tắc lối ra~~ — đã duyệt: mục 6.2 như viết (lần đầu `--fix-round`, lần sau `--accept`).
4. ~~Bảng kết luận~~ — đã duyệt: mục 8, ngưỡng tốn kém 2,5×.
5. Sau khi duyệt: agent chạy chuẩn bị (mục 4) và đối chứng âm, đưa chủ dự án xem `results/controls/` và
   `log/fixture-changes.md` **trước** khi chạy lượt agent nào.
6. Biên bản ở `docs/evals/bench-bint-vs-plain-v3-<ngày>/` (cùng cấu trúc lần 2: `README.md`, `diffs/`, `hidden/`,
   `results/`, `review/`, `log/`); không commit, không push trừ khi chủ dự án bảo.
7. Sau benchmark: gỡ khối trust `bench-bint-v3-…` khỏi `~/.codex/config.toml` (chỉ khi chủ dự án đồng ý), xoá `/tmp/.git`.

## 12. Thời gian và chi phí ước tính

| Việc | Thời gian | Ai |
|---|---|---|
| Chuẩn bị 4 repo, trust, script, đối chứng âm | ~60 phút | Agent chạy |
| Duyệt đối chứng | ~10 phút | Người |
| 4 lượt song song | ~60–90 phút | Codex, agent điều phối |
| Kích hoạt thích nghi, test ẩn (4 bản), N2, review mù + xác minh mù, biên bản | ~90 phút | Agent và subagent |

Token Codex (theo lần 2): A ~7M vào mỗi lượt, B ~15–17M → tổng khoảng 45–50M token vào qua codex-lb. Phần Claude (điều phối,
4–6 agent đánh giá) ước $15–25.
