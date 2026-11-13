# Tái nghiệm thu P0–P5 sau các bản sửa

> **Bản ghi lịch sử:** Biên bản dưới đây phản ánh trạng thái tại commit được kiểm tra. Trạng thái nghiệm thu hiện tại và tiêu chí còn mở xem ở [docs/acceptance.md](acceptance.md); trạng thái triển khai ở [docs/status.md](status.md).

Ngày: 2026-11-10. Commit kiểm tra: **`137dbf939ea783357e23be9e87b70b5d7199a5b4`**.

Các bản sửa được đối chiếu: `49a85ef` (A1–A3), `8ff7416` (P5-01–03), `1a12bc6` (kiểm thử tranh chấp nhiều tiến trình). P6 đã có trong source nhưng không thuộc phạm vi nghiệm thu sản phẩm của biên bản này.

Đây là kết quả kiểm tra mới, không lấy ghi chú “đã sửa” trong commit hoặc báo cáo cũ làm bằng chứng đã nghiệm thu. Mã sản phẩm được giữ nguyên; chỉ thêm báo cáo và fixture tái hiện ngoài các package triển khai.

## Kết luận

**Chưa đủ điều kiện ký nghiệm thu toàn bộ P0–P5.** Một số lỗi cũ đã sửa đạt, nhưng còn ba vấn đề tái hiện được. Bộ test thường thất bại ở khoá nhiều tiến trình; bộ race qua không phủ định lỗi giao thức tệp này.

| Giai đoạn | Kết quả tại commit này |
|---|---|
| P0 | **Đạt chức năng đã kiểm tra trên Linux**: init/doctor, cấu hình, SQLite/FTS5, spool, nhiều worktree, bảo toàn tệp người dùng |
| P1 | **Chưa đủ nghiệm thu thực nghiệm**: cơ chế đánh giá và economy đã được xác minh; đã có pilot thật nhưng chỉ 2 tác vụ validation |
| P2 | **Chức năng có; chưa qua gate 1**: pilot không đủ repo/cỡ mẫu/chỉ số hiệu quả theo hợp đồng gate |
| P3 | **Chưa đạt đầy đủ**: gate 2 chưa mở; còn giới hạn fingerprint, tín hiệu người dùng sửa diff và quyền ghi citation trong pilot Codex |
| P4 | **Chưa đạt trọn vẹn**: chuỗi b-do/overlays qua; luồng task CLI trực tiếp bị kẹt phê duyệt (R3) |
| P5 | **Chưa đạt**: thu hồi khoá chết vẫn có thể mất độc quyền (R1); ngoại lệ chỉ đọc còn cho sửa tri thức verified (R2) |

## Lệnh và bằng chứng mới

Binary dùng cho probe CLI được build từ source đang kiểm tra: `/tmp/bint-reacceptance-137dbf9`. Không dùng kết quả của binary cũ trong PATH để chứng minh các bản sửa mới.

| Kiểm tra | Kết quả |
|---|---|
| `CGO_ENABLED=0 go build -o /tmp/bint-reacceptance-137dbf9 ./cmd/bint` | PASS, exit 0 |
| `go vet ./...` | PASS, exit 0 |
| `go test -count=1 ./...` | **FAIL**, exit 1; package workflow, `TestAcquireWriteLockExclusiveAcrossProcesses` |
| `go test -race -count=1 ./...` | PASS, exit 0 |
| Chạy riêng test nhiều tiến trình, `-count=2` | PASS cả hai lần; thể hiện tính phụ thuộc lịch tranh chấp, không đủ đóng R1 |
| Probe cũ: truyền max_turns và loại tri thức có origin sau base | PASS cả hai |
| Probe cũ: chỉ đọc không sửa config và tranh khoá bình thường | PASS cả hai |
| Probe mới: nhiều bên thu hồi khoá chết | **FAIL hai lần**, ghi nhận 65 và 76 lượt lấy khoá khi đã có chủ khác |
| Probe mới: chỉ đọc sửa một rule verified đã có | **FAIL assertion**: bước vẫn completed, task planned |
| CLI: done chạm migration chưa duyệt | Bị chặn đúng; thử duyệt rồi done lại vẫn bị chặn — R3 |
| CLI: binary Codex không tồn tại | Báo `installed: false`, đúng |
| Chuỗi b-do qua Codex/Antigravity/role hỗn hợp, binary giả lập, cùng knowledge ID | PASS cả ba cấu hình trên binary mới |

Các package có test khác ngoài workflow đều qua ở lượt thường; bộ race qua cả workflow. Test toàn bộ cũng chạy phần P6 có trong source, nhưng không đồng nghĩa đã nghiệm thu P6.

Chi tiết có cấu trúc: [checks.json](evals/reacceptance-p0-p5-137dbf9/checks.json). Fixture tái hiện được giữ trong [thư mục bằng chứng](evals/reacceptance-p0-p5-137dbf9/), không chỉ nằm ở `/tmp`.

Chạy lại hai probe R1/R2 từ checkout tương ứng:

```sh
python3 docs/evals/reacceptance-p0-p5-137dbf9/run-probes.py
```

Script tạo Go overlay tạm, không sửa source package. Trên commit đang kiểm tra, exit 1 là kết quả assertion thất bại đã mô tả; sau bản sửa đúng, các assertion phải qua.

## Trạng thái các lỗi đã ghi trước đây

| ID cũ | Tái kiểm và kết luận |
|---|---|
| A1 — agent hoàn thành thay đổi protected chưa duyệt | **Đã chặn được lỗi quyền ban đầu**: CLI mới trả lỗi nêu migration. **Chưa đóng đầy đủ luồng sử dụng** vì R3 |
| A2 — b-do bỏ qua tổng max_turns | **Đã sửa đạt trong phạm vi hỗ trợ**: truyền phần còn lại, cộng lượt thực dùng, dừng trước bước tiếp theo; test cả cấu hình riêng và per_run qua |
| A3 — economy giữ tri thức tạo sau base | **Đã sửa đạt**: kiểm tra origin commit, ancestry, created_at và bằng chứng; probe tương lai cũ qua |
| A4 — test hook giả định đường dẫn Windows trên Linux | **Đã đóng thất bại trên Linux đã nêu**: package claude qua thường/race. Không suy rộng thành xác minh Windows thực tế |
| P5-01 — khoá cho nhiều chủ cùng giữ | **Mới sửa một phần**: công bố nguyên tử khắc phục cửa sổ tệp rỗng; nhánh thu hồi vẫn lỗi R1 |
| P5-02 — chỉ đọc sửa config trong .bint | **Config/skills/evals đã được đưa lại vào snapshot**; probe config cũ qua. Ngoại lệ knowledge còn rộng — R2 |
| P5-03 — missing binary vẫn installed true | **Đã sửa đạt**: Describe/Available kiểm tra khả dụng; unit test và CLI mới đều báo false cho binary không tồn tại |

## R1 — Cao: thu hồi khoá chết có thể tạm gỡ khoá của chủ đang sống

Vị trí: `internal/workflow/isolation.go:90`, đặc biệt rename tại dòng 92 và kiểm tra nội dung sau rename tại dòng 95.

Kịch bản:

1. Hai bên cùng đọc được nội dung của khoá cũ có PID đã chết.
2. Một bên thu hồi và lấy khoá mới, bắt đầu làm việc.
3. Bên còn lại tiếp tục `os.Rename(p, stale)` dựa trên kết quả đọc cũ, nên có thể chuyển đi khoá mới của chủ đang sống.
4. Trong khoảng đường dẫn khoá trống, bên thứ ba lấy khoá thành công. Việc nhận ra đã chuyển nhầm rồi cố link lại xảy ra quá muộn; nếu đã có khoá khác thì khôi phục còn thất bại.

Bản sửa đã công bố nội dung bằng link nguyên tử, nên nguyên nhân này khác cửa sổ “tệp rỗng/chưa ghi PID” ban đầu. Thu hồi cần phối hợp với cả các bên lấy khoá, không chỉ chuyển tệp rồi kiểm tra sau.

**Bằng chứng:** test chuẩn nhiều tiến trình thất bại với thông báo `hai tiến trình cùng giữ khoá: open .../inside: file exists`; các tiến trình khác sau đó không hoàn thành đủ vòng trong 30 giây. Đây có bằng chứng xung đột marker, không chỉ thông báo đói lượt.

Probe mới trên implementation không sửa, chỉ thêm test qua Go overlay: 24 đợt, mỗi đợt 24 goroutine × 30 lần thử, giữ vùng làm việc 150 µs. Mỗi đợt bắt đầu bằng một khoá có PID chết. Hai lần chạy độc lập ghi nhận **65/2497** và **76/2407** lượt lấy khoá bị chồng với chủ khác. Các bộ đếm dùng atomic; test không sửa hàm AcquireWriteLock hoặc thêm hook đổi lịch thực thi vào implementation.

`go test -race` qua không chứng minh giao thức khoá trên filesystem đúng. Test tranh chấp bình thường và hai lần chạy riêng nhiều tiến trình qua cũng không phủ định nhánh thu hồi đã tái hiện lỗi.

**Điều kiện đóng:** không tạm gỡ khoá sống dựa vào quan sát stale; dùng primitive khoá phù hợp hoặc giao thức thu hồi/lấy khoá được điều phối nguyên tử. Bổ sung trường hợp nhiều reclaimer và chủ mới xuất hiện giữa đọc–thu hồi, ngoài các test tranh chấp khoá đang sống.

## R2 — Cao: ngoại lệ chỉ đọc vẫn cho sửa tri thức verified

Vị trí: `internal/workflow/isolation.go:120`, pathspec loại cả `.bint/knowledge` tại dòng 146.

Ngoại lệ intended là để ghi **ứng viên** tri thức, nhưng thư mục bị bỏ khỏi snapshot chứa cả rule/pattern đã verified. Probe tạo một rule hợp lệ có origin human, reviewed_by owner và fingerprint bằng chứng, rồi chạy b-req với fake agent sửa nội dung rule, giữ nguyên trạng thái verified và metadata duyệt.

**Kết quả:** lưu mục thành công, bước completed, tác vụ planned; kiểm tra chỉ đọc không phát hiện thay đổi. Probe không đổi mã nguồn bằng chứng và không gọi lệnh nâng candidate lên verified: nó sửa một mục đã verified sẵn.

Fixture qua Verifier trước khi chạy. Sau khi nội dung bị sửa, Context Engine vẫn trả mục đó với **`label=rule`**, tức quy tắc bắt buộc, vì trạng thái/metadata duyệt và fingerprint của mã nguồn bằng chứng chưa đổi. Đây không chỉ là thay đổi một tệp candidate bị bỏ qua.

Phạm vi phát hiện là bảo đảm chỉ đọc của Bint đối với nguồn tri thức, không phải tuyên bố vượt qua mọi quyền của hệ điều hành. Với Codex, Bint chạy trong workspace-write và dựa vào hậu kiểm để bảo vệ bước chỉ đọc; vì vậy ngoại lệ này có ý nghĩa thực tế. Review Git sau đó không làm việc chấp nhận nhầm bước chỉ đọc trở nên đúng.

**Điều kiện đóng:** phân biệt tạo candidate hợp lệ với sửa/xoá mục đã tồn tại hoặc đã verified; giữ các thay đổi không được phép trong phạm vi kiểm tra. Không mở ngoại lệ cho toàn bộ knowledge chỉ vì một số lệnh cần tạo candidate.

## R3 — Trung bình: task CLI báo cần approve nhưng approve không thấy thay đổi

Vị trí: `internal/workflow/ops.go:258` và `internal/workflow/ops.go:448`.

SetStatus(done) đã tính lại unapproved changes để chặn lỗi A1. Tuy nhiên khi chặn, nó trả NotDoneError mà không điền PendingPaths. Approve chỉ đọc PendingPaths, không tính lại diff. Do đó tác vụ được quản lý trực tiếp bằng CLI có thể không đi qua được chính bước duyệt được thông báo.

Đã tái hiện trên binary mới và repository tạm:

1. Init, tạo tác vụ có tiêu chí; chuyển in_progress.
2. Trong vai agent của fixture, sửa `migrations/001.sql`, ghi tiêu chí met.
3. `task status <id> done` trả 1, nêu migration cần `bint task approve`.
4. Trong vai người duyệt của fixture, `task approve <id>` trả 0 nhưng in **“không có gì chờ duyệt”**.
5. Quay lại agent, `task status <id> done` vẫn trả 1 với đúng migration đó.

Không dùng --force hoặc sửa trực tiếp ProtectedApproval để làm kịch bản qua. Test mới trong run_test.go kiểm tra chặn rồi tự điền ProtectedApproval bằng cập nhật state, nên không phát hiện được đoạn Approve bị thiếu này. Luồng bint run đã điền PendingPaths vẫn có thể hoạt động; smoke b-do của đợt này đã qua.

Bằng chứng: [approval-deadend.json](evals/reacceptance-p0-p5-137dbf9/approval-deadend.json).

**Điều kiện đóng:** khi người duyệt, tính được đúng diff cần duyệt ngay cả khi tác vụ chưa đi qua Runner/checkScope; thêm CLI E2E cho chuỗi done bị chặn → approve → done thành công bằng API công khai.

## Đối chiếu toàn bộ hạng mục P0–P5

Ký hiệu: **Có** là có triển khai và bằng chứng trong phạm vi kiểm tra; không thay thế tiêu chí hiệu quả hoặc đóng các lỗi được dẫn chiếu.

### P0

| Hạng mục | Kết quả / bằng chứng |
|---|---|
| Go module/package | Có; build từ source hiện tại |
| CLI và quản lý lỗi | Có; CLI E2E, mã thoát cho repo chưa init và lỗi tham số |
| Config schema v1 | Có; validate, nạp lớp cấu hình, từ chối khoá lạ |
| State ngoài repo, theo git-common-dir | Có; state/app tests nhiều worktree |
| SQLite/migration/FTS5, không cần CGO | Có; store tests và build CGO=0 qua |
| init/doctor | Có; tạo mới, lặp lại, cấu hình hiện có |
| Đồng bộ an toàn, backup, marker AGENTS.md | Có; fsafe và init bảo toàn chỉnh sửa người dùng |
| Spool/ingest/dedup event_id | Có; test ghi/nạp song song và event trùng |
| Hook phiên/tệp sửa/lệnh test | Có; claude/hookcore/app tests qua |
| CLI nhiều trạng thái repo/worktree | Có; gồm 4 worktree dùng cùng DB, đủ sự kiện, không trùng |

P0 đủ bằng chứng chức năng trên nền tảng Linux được kiểm tra. Thất bại giao thức khoá workflow ở R1 không được che bằng kết luận P0 này; toàn sản phẩm vẫn chưa được ký nghiệm thu.

### P1

| Hạng mục | Kết quả / bằng chứng |
|---|---|
| eval mine | Có; unit/integration và pilot thật khai thác 562 ứng viên (repository A) |
| Môi trường tại base cô lập | Có; kho Git riêng không chứa commit lời giải; integrity tests qua |
| Viết lại đề/kiểm rò rỉ/duyệt tác vụ | Có cơ chế; cặp đề bài–oracle của pilot còn cần owner review |
| Nhiều nhánh/lần lặp/một harness | Có; tests hiện tại qua, pilot Codex thật đã lưu |
| Ước tính chi phí/cỡ mẫu | Có; giữ unknown khi thiếu dữ liệu, không ghi 0 thay thiếu |
| Báo cáo theo cặp/CI/dao động | Có; BuildReport/stats tests qua |
| strict/economy | Có; integrity tests và probe A3 cũ qua |
| Baseline thực tế | Có pilot thật; mới 2 tác vụ validation ×3, chưa đủ “tập tác vụ đủ lớn” |

Pilot Codex hiện có được chạy bằng Bint `b5896a4`, không được ghi là một lượt đo lại của `137dbf9`. Nó chứng minh đường chạy thật, đồng thời cho thấy thiếu cỡ mẫu và vấn đề oracle; không đủ ký P1 thực nghiệm.

### P2

| Hạng mục | Kết quả / bằng chứng |
|---|---|
| Static discovery | Có; inspect tests |
| Định dạng tri thức/ULID/nguồn/bằng chứng | Có; knowledge/ulid tests |
| b-bootstrap tối giản | Có; skill nhúng, CLI và snapshot fake-agent tests |
| Kiểm đường dẫn/ký hiệu/ví dụ | Có; Verifier và fingerprint tests |
| approve và bảng trạng thái | Có; chặn agent tự nâng rule, kiểm chứng nguồn |
| add/review theo lô | Có; CLI knowledge E2E |
| Sinh AGENTS.md/nhập CLAUDE.md | Có; render/publish và bảo toàn marker |
| minimal so với none | Có pilot, chưa đủ điều kiện gate 1 chính thức |

Gate 1 vẫn yêu cầu ≥2 repository khác hệ sinh thái, ≥20 tác vụ test đã fail→pass/repository, 3 lần lặp, và thước đo hiệu quả đã chọn. Pilot một repo/hai tác vụ validation và Codex thiếu USD/num_turns không đáp ứng hợp đồng đó.

### P3

| Hạng mục | Kết quả / bằng chứng |
|---|---|
| FTS5 dẫn xuất/index rebuild | Có; index và CLI E2E |
| Truy xuất mô tả/đường dẫn | Có; ctxengine tests |
| MCP sáu công cụ | Có ở tầng server; CLI stdio E2E. Quyền citation của provider trong pilot còn hạn chế |
| Path hook có timeout | Có ở harness hỗ trợ; adapter báo giới hạn Codex/Antigravity |
| Fingerprint ký hiệu/tree-sitter/vùng dòng | Một phần; Go parser thật, ngoài Go heuristic. D-FP-01 chấp nhận hunk cho pilot, không chứng nhận tree-sitter |
| Xác thực lại theo rủi ro | Có; low-risk deterministic, high-risk cần người duyệt |
| Truy xuất/cite | Có cơ chế và tests; pilot Codex 6/6 retrieval, 4 cite calls bị policy chặn |
| Outcome signals/quét revert | Có phần tests/verify/rating/commit/revert/criteria/review; user diff-edit attribution còn hoãn |
| ideal/chẩn đoán | Có implementation/tests; pilot chưa chạy ideal nên không kết luận nguyên nhân nội dung/truy xuất |
| bint so với minimal | Pilot cùng đạt 50%, gate 2 chưa mở chính thức |

Các quyết định ở [fingerprint-outcome-decisions.md](fingerprint-outcome-decisions.md) giữ hiệu lực nhưng không tự chuyển các mục thiếu thành hoàn thành.

### P4

| Hạng mục | Kết quả / bằng chứng |
|---|---|
| Core bất biến/anchor/overlay | Có; compose và xung đột nâng cấp |
| Bốn skill tối thiểu | Có; source nhúng, CLI/workflow E2E |
| Artifacts/state machine/task ID | Có; b-do cha/con và tài liệu liên kết |
| Tiêu chí chấp nhận/outcome signals | Có; chặn thiếu bằng chứng, verify fingerprint |
| Lỗi/gián đoạn/resume | Có; tests thất bại, huỷ, runner chết, resume |
| Nâng cấp core/áp lại overlay | Có; giữ bản cũ/lock khi mất anchor, không mất overlay |
| Ticket/giới hạn vận hành | Có; tổng lượt A2 đã sửa, protected done đã chặn; duyệt task CLI còn R3 |
| Review thành tri thức | Có; --from-review, agent tạo candidate |

Chuỗi b-do bình thường và overlay qua, nhưng chưa ký P4 toàn bộ vì đường dùng task CLI trực tiếp chưa hoàn thành được vòng phê duyệt chuẩn.

### P5

| Hạng mục | Kết quả / bằng chứng |
|---|---|
| Adapter/capability matrix | Có; Describe/Available đã nhất quán cho missing/nonexec binary trong test |
| Claude Code | Có regression tests hook/MCP/exec |
| Codex | Có formatter/hook/MCP/JSONL/exec tests; smoke mới bằng fake binary; pilot thật trước đây có giới hạn cite |
| Antigravity | Có formatter/hook/JSON/exec tests; smoke mới bằng fake binary; tool-hook payload thực tế vẫn chưa đủ log |
| Skill theo định dạng | Có; giữ body/overlay, metadata đúng từng target |
| Role override | Có; thứ tự ưu tiên và reject harness chưa enabled; smoke role hỗn hợp qua |
| Chuẩn hoá sự kiện | Có ở fixture/contract; không tổng hợp session.ended giả cho AGY |
| Cô lập tác vụ ghi/chỉ đọc | Chưa đạt; R1 và R2 |
| Eval nhiều harness | Có contract/integration tests; batch ghi harness, skip arm không hỗ trợ |
| Adapter không khả dụng | Có; lỗi thực thi rõ ràng và CLI installed false đã được xác minh |

Smoke hiện tại dùng đúng adapter native của Bint với binary giả lập, yêu cầu cùng knowledge ID trong đề bài; cả toàn Codex, toàn Antigravity và role hỗn hợp đều done. Xem [multiharness-smoke.json](evals/reacceptance-p0-p5-137dbf9/multiharness-smoke.json). Đây không phải bằng chứng suy luận/live-provider mới ở commit này.

## Điều kiện tái nghiệm thu tiếp theo

1. Đóng R1, giữ được độc quyền cả khi nhiều bên cùng thu hồi khoá chết; suite thường và race phải qua sau sửa.
2. Đóng R2, kiểm riêng candidate creation và verified/existing item edits trong bước chỉ đọc.
3. Đóng R3 bằng E2E API công khai, không tự gán ProtectedApproval hoặc dùng --force để làm test qua.
4. Hoàn thiện corpus/owner review/chỉ số hiệu quả và điều kiện hai repository để quyết định P1 thực nghiệm/gate 1/gate 2; không chạy lại pilot cũ rồi coi là tập test độc lập.
5. Lưu bằng chứng tích hợp provider theo phiên bản/cấu hình, đặc biệt citation approval của Codex và hook công cụ của Antigravity.

Đợt tái nghiệm thu không khởi thêm benchmark mô hình trả phí hoặc sửa các ngưỡng gate. Các probe được lưu để có thể chạy lại từ repository, không cần phục dựng lịch sử hội thoại.
