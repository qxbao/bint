# Planning Artifact Contract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Chặn tài liệu `b-req` thiếu hoặc đứt liên kết AC → TSK trước khi `b-feat` chạy, đồng thời cung cấp `bint task lint` để kiểm tra và sửa lỗi.

**Architecture:** Thêm bộ kiểm xác định được trong `internal/task`, đọc ba artifact hiện có và trả báo cáo lỗi/cảnh báo/câu hỏi mở. `workflow.Runner` và lệnh `task` cùng dùng báo cáo này. Chỉ task mới có `planning_contract_version: 1` bị chặn tự động; task cũ có thể tự chọn `task lint --strict`.

**Tech Stack:** Go 1.25.1, Cobra, Markdown văn bản, JSON state hiện có; không thêm dependency.

**Spec:** [Thiết kế chất lượng bước lập kế hoạch](../specs/2026-11-10-planning-quality-design.md), mục “Hợp đồng artifact v1”, “Tương thích” và “Điều kiện nghiệm thu”.

## Global Constraints

- Giữ ba artifact và thư mục `<state>/runtime/tasks/<id>/`; không ghi tài liệu mới vào Git.
- Không yêu cầu cấu trúc ở mô tả đầu vào của người dùng; chỉ kiểm đầu ra `b-req`.
- Không thay luật duyệt lệnh, đường dẫn bảo vệ, trạng thái `done` hoặc cấu trúc SQLite.
- `planning_contract_version` vắng mặt nghĩa là task cũ; không tự chặn `b-feat` của task cũ.
- Lỗi cấu trúc phải có mã ổn định và nêu artifact/ID cụ thể; Core không đánh giá ý nghĩa nghiệp vụ.

## Review Focus

1. Artifact bị sửa trực tiếp thành chỉ có khoảng trắng sau `task write`: lint phải báo `artifact_empty`.
2. Hai dòng `AC-1` hoặc `TSK-1` lặp: lint phải báo `duplicate_id`, không lặng lẽ dùng dòng cuối.
3. `task.yml` đổi tiêu chí sau khi plan xong: preflight phải báo `criterion_mismatch` trước `b-feat`.
4. `tasks.md` tham chiếu AC không tồn tại hoặc bỏ sót AC: lint phải báo `unknown_ac`/`uncovered_ac`.
5. Task legacy đang `planned` thiếu tài liệu: `b-feat` vẫn chạy; `task lint --strict` báo lỗi để nâng cấp chủ động.
6. Lint lỗi sau `b-req` → task `blocked` với `ResumeFrom=planned`; người sửa tay rồi `bint run resume` **không** gọi lại agent `b-req` (resume từ `created` sẽ chạy lại `b-req` và ghi đè bản đã sửa).
7. `bint task status planned` thủ công từ `failed`/`blocked`/`interrupted` cũng bị lint chặn với task v1, không chỉ `created→planned`.
8. `task.yml` có nhiều tiêu chí hơn `requirement.md`: lint phải báo lỗi (không chỉ kiểm từ phía AC).
9. `task set --criteria` chỉ nối thêm (`appendUnique`, `internal/workflow/ops.go`); chạy lại `b-req` trên task `planned` phải `--clear-criteria` trước, nếu không `AC-N ↔ Criteria[N-1]` lệch.

---

## Bản đồ file

| File | Trách nhiệm |
|---|---|
| `internal/task/artifactlint.go` | Kiểu báo cáo, parser dòng AC/TSK/Q và kiểm tra thuần trên nội dung; hàm đọc artifact của task. |
| `internal/task/artifactlint_test.go` | Luật parser, lỗi, câu hỏi và tương thích legacy. |
| `internal/task/task.go` | Trường `State.PlanningContractVersion`; không đổi schema tổng thể nếu JSON field vắng. |
| `internal/workflow/ops.go` | Đặt contract version khi tạo task và chặn chuyển `created → planned` thủ công nếu v1 lỗi. |
| `internal/workflow/run.go` | Lint sau `b-req`, trước `b-feat`, báo lỗi thay cho ghi chú thiếu artifact. |
| `internal/cli/task.go` | Lệnh `task lint <id> [--strict] [--json]`. |
| `internal/skills/core/b-req/SKILL.md` | Khuôn AC/TSK, gọi lint trước khi kết thúc khi chạy tay. |
| `internal/workflow/run_test.go`, `internal/cli/workflow_e2e_test.go` | Fixture hợp lệ và đường dừng trước `b-feat`. |
| `README.md` | Cú pháp mới và ranh giới legacy/v1. |

### Task 1: Bộ kiểm artifact xác định được

**Files:** Create `internal/task/artifactlint.go`, `internal/task/artifactlint_test.go`; modify `internal/task/task.go` để thêm trường state.

**Interfaces:**

- `ArtifactIssue` có ba trường string riêng `Code`, `Artifact`, `Detail` với JSON keys `code`, `artifact`, `detail`; mã ổn định, chi tiết tiếng Việt.
- `ArtifactReport` có ba slice riêng `Errors`, `Warnings`, `OpenQuestions` kiểu `[]ArtifactIssue`, với JSON keys `errors`, `warnings`, `open_questions`, và `func (r ArtifactReport) Blocking() bool` (chỉ `len(Errors) > 0`; `OpenQuestions` không làm `Blocking()` true — CLI xét riêng để thoát 1, Runner xét riêng ở plan thứ hai).
- `func LintPlanningText(ticket Ticket, docs map[string]string, strict bool) ArtifactReport`: không I/O; key `requirement|plan|tasks`, thiếu key nghĩa là thiếu tệp.
- `func LintPlanningTask(t *Task, strict bool) ArtifactReport`: đọc ba đường dẫn qua `ArtifactPath`; lỗi đọc thành `artifact_unreadable`.
- `State.PlanningContractVersion int` với tag `json:"planning_contract_version,omitempty"`; số 0 là legacy, số 1 là v1.

- [ ] **Step 1: Viết test đỏ** cho bộ tài liệu hợp lệ (`AC-1` khớp criterion thứ nhất; `TSK-1 [AC-1]` có `xong khi:`), ba artifact thiếu/rỗng, ID trùng, criterion đổi, unknown/uncovered AC và năm ca Review Focus. Thêm test: ticket có nhiều tiêu chí hơn số `AC` (`criteria_count_mismatch`), placeholder trong dòng AC/TSK (`placeholder_text`), heading `###` nằm trong section AC, dòng `AC-N` trong code fence bị bỏ qua. Test `strict=false` giữ lỗi định dạng dưới dạng warning nhưng vẫn liệt kê câu hỏi mở.
- [ ] **Step 2: Chạy** `go test ./internal/task -run 'TestLintPlanning' -count=1`; kỳ vọng FAIL vì API mới chưa có.
- [ ] **Step 3: Cài parser và report.** Chỉ nhận dòng AC/TSK ở đầu dòng checkbox `- [ ]` hoặc `- [x]`, ID số dương; AC chỉ tính dưới `## Tiêu chí chấp nhận`, Q chỉ tính dưới `## Câu hỏi mở`. `AC-N` phải liên tục 1..N và khớp từng `Verify.Criteria[N-1]` sau chuẩn hóa khoảng trắng bằng `strings.Fields`. TSK phải có ít nhất một AC trong `[...]`, mô tả và `xong khi:` không rỗng. Dòng Q chưa trả lời `- [ ] Q-N:` vào `OpenQuestions`; dòng đã trả lời chỉ hợp lệ khi có `— Trả lời:` và nội dung. Section kết thúc ở heading cấp 1–2 kế tiếp (`# `/`## `); `###` trở xuống nằm trong section; dòng trong code fence ``` bị bỏ qua. Số tiêu chí trong ticket phải bằng số `AC` (thừa hay thiếu đều là `criteria_count_mismatch`). Placeholder trong dòng AC/TSK: nội dung sau chuẩn hóa chứa `<...>`, `…`, hoặc từ `TODO`/`TBD` → `placeholder_text`. Không suy nghĩa nghiệp vụ từ văn xuôi.
- [ ] **Step 4: Chạy lại** test của bước 2; kỳ vọng PASS. Chạy thêm `go test ./internal/task -count=1` để bắt ảnh hưởng serialization của state.
- [ ] **Step 5: Commit** `internal/task/artifactlint.go`, test và `task.go` với thông điệp `feat(task): validate planning artifacts`.

### Task 2: Cổng Core tại ranh giới `b-req`/`b-feat`

**Files:** Modify `internal/workflow/ops.go`, `internal/workflow/run.go`, `internal/workflow/run_test.go`, `internal/task/task_test.go`.

**Interfaces:** Dùng `task.LintPlanningTask(t, true)` và `ArtifactReport.Errors`; bước kế tiếp vẫn do `nextStep` hiện tại chọn. `OpenQuestions` chưa chặn ở plan này; plan thứ hai nối nó vào luồng resume.

- [ ] **Step 1: Viết test đỏ**: task mới `b-do` có `Plan=true` hoặc workflow trực tiếp `b-req` lưu `PlanningContractVersion=1`; `b-do --no-plan` và workflow trực tiếp `b-feat` lưu 0. `b-do --task <created> --plan` nâng version lên 1. Agent giả bỏ riêng `plan.md` phải dừng ở `blocked` (status trước pause là `planned`) sau `b-req`, không gọi `b-feat`; kết quả nêu mọi mã lỗi (không chỉ mã đầu), gồm `artifact_missing`. Sau khi ghi lại `plan.md` bằng `task write`, `bint run resume` đi thẳng tới `b-feat` và agent giả đếm đúng 0 lượt `b-req` mới. Sau một plan hợp lệ, sửa `task.yml` criterion hoặc xóa artifact rồi chạy `b-feat --task`: phải blocked trước khi gọi agent. Task legacy `PlanningContractVersion=0` vẫn qua. Thêm: `SetStatus(planned)` không phải Orchestrator từ `failed`/`blocked`/`interrupted` trên task v1 lỗi bị từ chối; `b-req --task` trên task legacy `planned` mà agent lỗi giữa chừng vẫn là version 0; `--no-plan` trên task v1 đã `planned` vẫn bị gate; `b-feat --task` trên task v1 còn `created` không bị gate.
- [ ] **Step 2: Chạy** `go test ./internal/workflow -run 'TestPlanningContract' -count=1`; kỳ vọng FAIL.
- [ ] **Step 3: Đặt version trong `Env.NewTask`.** Đặt 1 khi workflow là `b-req`, hoặc là `b-do` và classification chọn plan; workflow trực tiếp `b-feat`/`b-review` luôn 0. Tạo một helper dùng chung `ensurePlanningContract(t)` (plan thứ hai cũng gọi nó mỗi khi `Plan` chuyển false→true, kể cả qua `--depth`). Task `created` được nâng lên 1 ngay khi `b-do --task <created> --plan` hoặc `b-req --task`, vì chưa có artifact để mất. Task legacy `planned` chạy lại `b-req` chỉ được nâng lên 1 **sau khi agent kết thúc, ngay trước lint**, để agent lỗi giữa chừng không biến task cũ thành v1. `--no-plan` chỉ hạ về 0 khi task đang `created`; task v1 đã rời `created` giữ v1 và vẫn bị gate. Mọi `SetStatus(..→planned)` của task v1 với `Orchestrator=false` (từ `created`, `failed`, `blocked`, `interrupted`) phải lint và trả lỗi có mã nếu cấu trúc sai; lời gọi của Runner (`Orchestrator=true`) không lint ở đây vì preflight bên dưới sẽ bắt; không đổi transition khác.
- [ ] **Step 4: Trong `Runner.afterStep(StepReq)`**, thay nhánh `os.Stat` hiện tại bằng lint strict. Khi lint có lỗi, vẫn chuyển `planned` rồi `r.block` (không dùng `failed`: lỗi này người sửa được, và `failed` từ `created` sẽ làm resume chạy lại `b-req`). Lý do nêu đủ mã lỗi, artifact và hướng `bint task lint <id>`; `Result` mang danh sách đầy đủ; không tự chạy lại agent. Preflight trước mọi lần gọi agent `StepFeat` trên task v1 có status `planned` hoặc `in_progress` (kể cả vòng sửa sau review; không áp cho `stepCheck`): lint lại, lỗi → `blocked` và không gọi agent. Task v1 còn `created` gọi thẳng `b-feat` không bị gate (khớp spec: gọi trực tiếp `b-feat` không cần artifact). Cổng khóa theo `PlanningContractVersion`, không theo `Classification.Plan`. Chỉ khi `Workflow=b-do` mới `r.block`; với workflow `b-req` chạy riêng, task ở lại `planned`, `Result.Reason` liệt kê lỗi và lệnh thoát khác 0, vì `resume` của workflow `b-req` chạy lại agent `b-req` (`nextStep` trả `wf`) — cổng preflight `b-feat` vẫn bắt lỗi sau đó.
- [ ] **Step 5: Cập nhật fixture agent giả** ở `run_test.go`: trong nhánh `StepReq`, ghi criterion vào ticket qua `Env.SetTicket` rồi viết bộ AC/TSK khớp criterion đó. Sửa luôn fixture E2E `internal/cli/workflow_e2e_test.go` (hiện ghi `# Yêu cầu\n- [ ] app.txt ghi v2`, không có `AC-N`/`TSK`; nếu để sang Task 3 thì commit Task 2 làm test CLI đỏ) bằng một helper fixture dùng chung tạo bộ AC/TSK hợp lệ. Chạy `go test ./internal/workflow ./internal/task ./internal/cli -count=1`; kỳ vọng PASS.
- [ ] **Step 6: Commit** các file của task này với thông điệp `feat(workflow): gate implementation on planning artifacts`.

### Task 3: CLI, skill và nghiệm thu từ đầu đến cuối

**Files:** Modify `internal/cli/task.go`, `internal/cli/workflow_e2e_test.go`, `internal/skills/core/b-req/SKILL.md`, `README.md`; add focused CLI test in `internal/cli` if existing E2E fixture alone does not cover JSON/exit code.

**Interfaces:** `bint task lint <id> [--strict] [--json]`; JSON gồm `task_id`, `contract_version`, `errors`, `warnings`, `open_questions`. Exit 1 khi có lỗi/câu hỏi mở; 0 khi chỉ có warning. CLI mặc định áp strict cho v1, advisory cho v0; `--strict` áp v1 rules lên v0 mà không ghi state.

- [ ] **Step 1: Viết test đỏ** cho CLI: task v1 lỗi trả mã 1 và mã lỗi JSON ổn định; task v0 mặc định không bị fail vì thiếu format; `--strict` trên v0 trả mã 1; E2E `b-do` với agent giả hợp lệ vẫn done.
- [ ] **Step 2: Chạy** `go test ./internal/cli -run 'TestTaskLint|TestWorkflowEndToEnd' -count=1`; kỳ vọng FAIL ở test mới.
- [ ] **Step 3: Thêm lệnh `task lint`** vào `newTaskCmd`; dùng bộ lint ở task 1, không viết state. In mỗi lỗi kèm artifact và cách sửa; JSON luôn chứa mảng rỗng thay vì null.
- [ ] **Step 4: Sửa `b-req` skill** để gọi `bint task set --clear-criteria --criteria ...` khi lập kế hoạch lại (`--criteria` một mình chỉ nối thêm và dedupe theo chuỗi đúng từng ký tự; chạy lại `b-req` mà không xóa sẽ lệch `AC-N ↔ Criteria[N-1]`, và `State.Criteria` theo index cũ cần được nhắc là đã mất) và ghi `AC-N`/`TSK-N` theo spec; khi chạy tay, gọi `bint task lint --strict` trước `task status planned`. Giữ hai anchor `project-rules` và `plan-template` đúng tên để overlay cũ còn gắn được. Sửa fixture E2E và README.
- [ ] **Step 5: Chạy** `go test ./internal/task ./internal/workflow ./internal/cli ./internal/skills -count=1`, `go vet ./...`, `go build ./...`; kỳ vọng tất cả qua bằng Go 1.25.1. Nếu cache/toolchain không sẵn, ghi rõ lệnh chưa chạy và không gọi là pass.
- [ ] **Step 6: Commit** CLI, skill, docs và fixture với thông điệp `feat(cli): expose planning artifact lint`.

## Kiểm tra cuối và ranh giới bàn giao

- Chạy `go test -count=1 ./...` và build `CGO_ENABLED=0 GOOS=windows go build ./...`, `GOOS=darwin go build ./...` trong môi trường Go 1.25.1; đọc exit code thật.
- Rà `git diff --check`, kiểm `bint sync` vẫn bảo vệ overlay và `core.lock`; không tạo artifact trong repo thử nghiệm ngoài file Bint quản lý.
- Không mở rộng sang chấm chất lượng nội dung, `@trace` trong source, xuất spec sang Git hoặc retry agent tự động. Các mục đó cần bằng chứng và plan riêng.
