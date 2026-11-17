# Biên bản: benchmark Bint so với không dùng Bint (2026-11-16)

Kế hoạch: [docs/benchmark-bint-vs-plain-plan.md](../../benchmark-bint-vs-plain-plan.md). Chủ dự án đã duyệt kế hoạch và test
ẩn trước khi chạy. Mỗi bên chạy một lần cho mỗi task, nên kết quả dưới đây là **tín hiệu, không phải kết luận thống kê**.

## Kết luận

**Không có tín hiệu cho thấy Bint cho kết quả tốt hơn, trong khi chi phí rõ ràng cao hơn.** Theo bảng ở mục 7 của kế hoạch,
kết luận là "Không có tín hiệu" về kết quả, kèm chi phí và thời gian tăng.

| Task | Test ẩn A / B | Chấm mù A / B (tổng 20) | Token input B so với A | Token output B so với A | Thời gian tường B so với A | Can thiệp A / B |
|---|---|---|---|---|---|---|
| Trung (GĐ3) | 12/12 · 12/12 | 14 / **17** | +51 % | +123 % | +90 % | 1 / 2 |
| Lớn (GĐ2a+2b) | **11/12** · 10/12 | **15** / 13 | +48 % | +52 % | +55 % | 0 / 2 |

- **Kết quả.** Task trung hoà ở test ẩn; người chấm mù nghiêng về B, chủ yếu vì test kỹ hơn. Task lớn nghiêng về A ở cả test
  ẩn lẫn chấm mù: phần phân tích offline của B không đưa ra tín hiệu cho phiếu lịch sử. Hai task cho hai hướng ngược nhau, nên
  coi là hoà.
- **Tốn kém và thời gian.** B tốn hơn A ở mọi số đo, và vượt xa ngưỡng hoà 25 %. Khi bỏ phần tốn thêm do lỗi môi trường (mục
  "Sự cố môi trường"), B vẫn tốn hơn: task trung khoảng +22 % input, +87 % output, khoảng +40 % thời gian; task lớn khoảng
  +32 % input, +40 % output, khoảng +41 % thời gian.
- **Can thiệp của người.** B cần nhiều hơn. Phần lớn can thiệp của B sinh ra từ điểm vướng của chính Bint (mục "Điểm vướng
  của Bint"), không phải từ việc agent làm sai.

Đề xuất: chưa đủ cơ sở để nói Bint giúp ích ở dạng chạy `b-do` với Codex. Chi phí thêm có nguyên nhân cụ thể, sửa được
(mục "Điểm vướng của Bint"). Nên sửa các điểm đó trước rồi mới chạy lần lặp thứ hai, vì chạy lại ngay sẽ chỉ đo lại cùng
các điểm vướng này.

## Thiết lập

| | Bên A (không Bint) | Bên B (Bint) |
|---|---|---|
| Repository | Clone ở `2b2f74d` cộng commit "gỡ Bint": bỏ `.bint/ .agents/ .claude/ .codex/ .mcp.json AGENTS.md CLAUDE.md` và khối `bint` trong `.gitignore`. `TestDocCommandsExist` đỏ vì thiếu `AGENTS.md`, nên bỏ `AGENTS.md` khỏi `docCommandSources` ngay trong commit đó | Clone ở `2b2f74d`, `bint init --hooks`, `bint mcp index codegraph` |
| Lệnh | `codex exec --json --sandbox workspace-write --skip-git-repo-check --cd <repo> --model gpt-6.1-sol` (cùng cờ Bint dùng cho bước Codex) | `bint run b-do --harness codex --model gpt-6.1-sol --autonomy auto`, binary đóng băng build từ `2b2f74d` |
| Prompt | Đúng hai câu ở mục 4 của kế hoạch, giống nhau cho hai bên (`timeline/prompt-*.txt`) | như A |

Chung cho hai bên:

- Bốn lượt chạy song song, bắt đầu lúc 07:37:02Z, qua codex-lb. Không ghim được tài khoản cho từng lượt.
- Cả hai bên chạy với vai người: gỡ `CLAUDECODE`/`AI_AGENT`… qua `hidden/human`.
- `~/.codex/config.toml` được thêm trust project cho cả bốn repo và hash hook cho B, trong khối có đánh dấu
  `bench-bint-20261116`.
- Cấu hình Codex toàn cục giữ nguyên, gồm MCP gitnexus/playwright không kết nối được ở cả hai bên, hook của orca/herdr và
  `model_reasoning_effort = "high"`.
- Trước khi chạy, `go build`, `go vet`, `GOOS=windows go vet` và `go test ./...` đều xanh trên A-mid và B-mid. A-big và B-big
  có cây giống hệt A-mid và B-mid.

Hook Codex của Bint có chạy (rủi ro `NT-CODEX-HOOK` không xảy ra). Bảng `events` ghi 44/39 `file.modified` và 6/4
`session.started`. Các bước của B trích dẫn tri thức 18/17 lần, chủ yếu ở các mục "thêm khoá cấu hình sửa đủ bốn chỗ",
"`%%` trong mẫu Render", "vet/staticcheck GOOS=windows", "lệnh CLI mới cần `cliRefs`". Đây đúng là thiên lệch tri thức đã ghi
ở mục 4 của kế hoạch.

## Số liệu từng lượt

Token lấy từ rollout Codex (`~/.codex/sessions`) của mọi phiên có `cwd` nằm trong repo của lượt, tính từ 07:30Z
(`hidden/tokens.py`). Mỗi phiên lấy `total_token_usage` cuối cùng. Cả hai bên đo cùng một cách, gồm cả subagent Codex.

| Lượt | Phiên | Input | Cached | Input không cache | Output (reasoning) |
|---|---|---|---|---|---|
| A-mid | 2 | 5 636 649 | 5 125 632 | 511 017 | 24 795 (6 847) |
| B-mid | 6 | 8 505 144 | 7 518 592 | 986 552 | 55 323 (15 865) |
| A-big | 2 | 11 322 887 | 10 859 904 | 462 983 | 57 765 (16 569) |
| B-big | 5 | 16 762 557 | 16 002 560 | 759 997 | 88 028 (25 010) |

Một subagent của B-big dùng `gpt-6-astra` (830 845 input) vì agent tự chọn; mọi phiên còn lại dùng `gpt-6.1-sol`.

Thời gian tường tính từ 07:37:02Z tới khi xong theo người dùng. Với A, đó là lúc lần kiểm `go build && go test ./...` của
người dùng xanh; với B là `done`. Lịch sử chi tiết ở `timeline/`.

| Lượt | Tường | Diễn biến |
|---|---|---|
| A-mid | 35m46 | agent 11m36 → người dùng kiểm (10m39) đỏ → nhắc "build/test đỏ, sửa đi" → agent 3m54 → kiểm lại (7m44) xanh |
| A-big | 38m54 | agent 31m24 → kiểm (7m04) xanh |
| B-mid | 67m52 | b-req 6m06 → chờ người (Q-1) → b-req 1m30 → b-feat 14m56 → kiểm chứng → b-review 4m47 (0 phát hiện) → kiểm chứng đầy đủ đỏ (môi trường) + chặn hợp đồng artifact → chờ người 7m22 → b-feat 2m24 → b-review 4m14 (0 phát hiện) → kiểm chứng đầy đủ xanh → done |
| B-big | 60m26 | b-req 8m37 → b-feat 32m41 → kiểm chứng đầy đủ đỏ (môi trường) → b-review 7m38 (3 phát hiện, 0 nghiêm trọng) → chặn "tiêu chí 8 chưa đạt" → b-review 5m17 → done |

### Can thiệp của người

| Lượt | Thời điểm (Z) | Nội dung |
|---|---|---|
| A-mid | 08:00:55 | Theo giao thức: "build/test đỏ, sửa đi". Lỗi thật là `TestNoUnusedKeys` (6 khoá i18n không dùng); hai gói khác đỏ vì môi trường |
| B-mid | 07:43:59 | Trả lời Q-1 của planner ("chọn mặc định số từ repo mẫu nào?"): "Tự quyết theo kế hoạch." |
| B-mid | 08:25:31 | (a) `tasks.md` bị b-feat ghi lại, mất `— xong khi:` nên vi phạm hợp đồng artifact → thêm `— xong khi: như mô tả.` cho từng TSK; (b) kiểm chứng đầy đủ đỏ vì `/tmp/.git` → chạy lại `bint task verify` với `TMPDIR` riêng (xanh) rồi resume |
| B-big | 08:25:50 | (môi trường) kiểm chứng đầy đủ đỏ vì `/tmp/.git` → chạy lại `bint task verify` với `TMPDIR` riêng (xanh), song song với b-review |
| B-big | 08:32:10 | Tiêu chí 8 ("go test toàn bộ đạt") bị agent ghi chưa đạt vì không chạy được test socket trong sandbox → người ghi đạt, bằng chứng là `verify-2.log`, rồi resume |

## Test ẩn

Định nghĩa và mã: `hidden/hidden.py`. Bản thật sự dùng để chấm là v2; v1 là bản đã duyệt. Đối chứng:

- Binary gốc FAIL mọi mục tính năng.
- Binary giả trong `hidden/controls/fake_bint.py` qua 11/12 ở cả hai task. Mục còn lại là mẫu config; binary giả không sửa
  mẫu nên mục đó phải FAIL.

| Mục | A-mid | B-mid | | Mục | A-big | B-big |
|---|---|---|---|---|---|---|
| M1a–d publish --dry-run (thoát 0, trần, kích thước, cảnh báo) | ✓ | ✓ | | B1 progress/--json, đủ ID | ✓ | ✓ |
| M2 doctor cảnh báo | ✓ | ✓ | | B2a X có no_change/same_failure | ✓ | **✗** |
| M3 trần cao/null không cảnh báo | ✓ | ✓ | | B2b Y không có | ✓ | ✓ |
| M4 mặc định không cảnh báo | ✓ | ✓ | | B3 Z không làm lỗi | ✓ | ✓ |
| M5 AGENTS.md giống từng byte bản gốc | ✓ | ✓ | | B4 chỉ đọc | ✓ | ✓ |
| M6 số âm bị từ chối | ✓ | ✓ | | B5a off/shadow nhận | ✓ | ✓ |
| M7 (phụ) mẫu config có khoá | ✓ | ✓ | | B5a stop nhận | ✗ | ✗ |
| | | | | B5b giá trị lạ bị từ chối, B5c mặc định off | ✓ | ✓ |
| Repo: build, vet, vet windows, staticcheck ×2, go test | 6/6 | 6/6 | | Repo | 6/6 | 6/6 |

Ghi chú:

- **Đổi fixture từ v1 sang v2, sau khi đã thấy kết quả của bên A, trước khi có kết quả nào của bên B.** Fixture v1 ghi mọi
  `verify` và `review` trong cùng một giây. A-big cố ý coi bản ghi cùng giây là không rõ thứ tự (nghiêng về "có tiến triển")
  nên FAIL B2a. Lượt chạy thật không bao giờ có như vậy, nên v2 chèn 1,2 giây giữa các thao tác.
  - Với v1, A-big đạt 10/12; với v2 đạt 11/12. A-mid không đổi.
  - Đối chứng với v2: binary giả 11/12, binary gốc 0/12.
- **B2a của B-big** hỏng vì B-big chỉ đánh giá được khi ghép vòng review với **lượt sửa do Runner chạy**. Phiếu dựng hoàn toàn
  qua CLI không có lượt chạy nào nên mọi tín hiệu là `unknown`. Người chấm mù coi đây là lệch đặc tả GĐ2a, vì đặc tả nói dựng
  lại từ `review.json` và `Verifications`.
- **B5a stop.** Cả hai bên đều từ chối `stop` vì hành vi dừng thuộc GĐ2c. Hai bên như nhau, nên mục này không ảnh hưởng so
  sánh.
- **Phép đo phụ trên sổ phiếu thật** (bản sao `runtime/tasks` cùng `state.sqlite` của repo bint chính, 90 phiếu, 93 vòng
  review): cả hai bản cho 0 sự kiện "lẽ ra dừng".
  - B-big báo 93/93 vòng `unknown` và 17 lỗi đọc (phiếu không có `review.json`).
  - A-big chỉ báo tổng.
  - Sổ phiếu hiện gần như không có phiếu nào từ hai vòng trở lên, nên phép đo này không phân biệt được hai bên.

## Chấm mù

Chi tiết ở [results/blind-grading.md](results/blind-grading.md). Hai agent Opus mới chấm, nhãn X/Y gán ngẫu nhiên, và bảng
ghép nhãn chỉ mở sau khi cả hai đã nộp.

- **Task trung:** B được 17 điểm, A 14. B hơn vì test kỹ hơn, xử lý lỗi chặt hơn và ít sửa ngoài phạm vi; A hơn ở i18n và ở
  việc dùng chung công thức token.
- **Task lớn:** A được 15 điểm, B 13. A hơn vì bám đặc tả GĐ2a và dùng được dữ liệu lịch sử. B hơn ở test Runner và đọc log
  an toàn, nhưng có chỗ lách test i18n.

## Sự cố môi trường: `/tmp/.git`

Lúc 07:42Z, khi các phiên Codex đang chạy, thư mục rỗng `/tmp/.git` xuất hiện. Sandbox `workspace-write` của Codex tạo nó làm
điểm mount bảo vệ `.git` trong gốc ghi được `/tmp`, và tạo lại sau mỗi lần bị xoá. Hệ quả:

- Mọi `go` chạy trong thư mục tạm dưới `/tmp` cố đọc trạng thái VCS và lỗi 128. `internal/affected` (`TestGoSourceSampleModule`,
  `TestGoSourceNeverNarrows`) đỏ ở mọi bản, kể cả bản clone gốc.
- **Bên B bị ảnh hưởng nặng hơn.** Bint chạy kiểm chứng ngoài sandbox với `TMPDIR=/tmp`, nên lần kiểm chứng đầy đủ của cả
  B-mid và B-big đều đỏ giả. Ở B-mid, Bint mở thêm một vòng b-feat và b-review để agent đuổi theo lỗi môi trường.
- Lần kiểm của người dùng cho bên A cũng gặp lỗi này (A-mid lần 1). Từ lần kiểm thứ hai, A chạy với `/tmp/.git` đã xoá hoặc
  `TMPDIR` riêng.
- Từ lần resume sau can thiệp, B chạy với `TMPDIR` riêng (`runB.sh`).
- Test ẩn chạy với `TMPDIR` riêng. Sau benchmark đã xoá `/tmp/.git`.

## Điểm vướng của Bint (đề xuất sửa)

1. **Kiểm chứng ngoài sandbox dùng `TMPDIR` chung với sandbox Codex.** Các `.git` giả do sandbox tạo trong `/tmp` làm kiểm
   chứng đỏ giả, và Bint mở vòng sửa vô ích. Đề xuất: chạy lệnh kiểm chứng với `TMPDIR` riêng trong thư mục trạng thái, và có
   chẩn đoán trong `doctor`.
2. **b-feat ghi lại `tasks.md` làm mất `— xong khi:`.** Hợp đồng artifact chỉ được kiểm khi Runner chuẩn bị vòng sửa, nên tác
   vụ bị chặn chờ người. Đề xuất: kiểm hợp đồng ngay sau mỗi bước ghi `tasks.md`, hoặc không cho bước implement ghi đè dòng
   TSK, chỉ cho đánh dấu `[x]`.
3. **Tiêu chí "go test toàn bộ đạt" do agent tự chấm trong sandbox.** Agent không chạy được test socket nên ghi chưa đạt, và
   tác vụ bị chặn dù lần kiểm chứng đầy đủ của Bint đã xanh. Đề xuất: một lần kiểm chứng đầy đủ mới, qua, trên đúng cây hiện
   hành thì tự đáp ứng tiêu chí đó; hoặc agent ghi "chờ Core" thay vì "chưa đạt".
4. **Resume sau khi người đã chạy lại kiểm chứng xanh vẫn chạy vòng b-feat sửa** (B-mid, 2m24s, rồi thêm b-review 4m14s). Đề
   xuất: lúc resume, nếu lần kiểm chứng mới nhất xanh trên cây hiện hành thì đi thẳng tới review hoặc finalize.
5. **Planner hỏi điều kế hoạch đã trả lời** (Q-1 của B-mid: chọn mặc định số; kế hoạch nói "chọn sau khi đo"). Câu hỏi này tốn
   một lần chờ người. Đây là vấn đề nhỏ, thuộc về prompt.

## Rủi ro và thiên lệch đã gặp

- **Một lần chạy.** Hai task cho hai hướng ngược nhau, nên dao động của model có thể lớn hơn hiệu ứng của Bint.
- **Bốn lượt song song, cùng với các lần kiểm của người dùng.** Máy bị tải nặng: lần kiểm đầu của A-mid có gói `workflow` quá
  10 phút. Mọi lượt chịu cùng điều kiện tải.
- **Thời gian của A tính cả thời gian người dùng kiểm** (7–11 phút mỗi lần). Thời gian của B tính cả kiểm chứng của Bint và
  thời gian chờ người, gồm 7m22 can thiệp ở B-mid.
- **Bên B tự duyệt hoàn toàn.** Bước xác nhận lệnh kiểm chứng không xuất hiện vì `eval.test_command` đã xác nhận sẵn trong
  `.bint/config.yml`.

## Tệp

- `diffs/`: diff của bốn lượt so với gốc tương ứng (A: commit "gỡ Bint"; B: `2b2f74d`). Đã bỏ tệp của Bint.
- `results/<lượt>/`: `task.json`/`task.txt` (v2; A còn `task.v1.*`), `repo.json`, `tokens.json`. Ngoài ra có
  `results/blind-grading.md` và `results/blind-mapping.json`.
- `timeline/`: dòng thời gian, can thiệp, prompt, `tasks.md` của B-mid trước khi sửa.
- `hidden/`: test ẩn v1/v2, đối chứng, script chạy (`runA.sh`, `runB.sh`, `hiddenrun.sh`), đo token, xuất diff.
- Repo của từng lượt và log thô: `/home/baodq/bench-bint-20261116/` (ngoài repository). Sổ phiếu của B ở
  `~/.local/state/bint/B-{mid,big}-*`.
