# Risk Adaptive b-req Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cho cùng skill `b-req` phân tích ở mức `standard` hoặc `deep` theo tín hiệu rủi ro/độ phức tạp, và chặn `b-feat` khi còn câu hỏi cản trở triển khai.

**Architecture:** Giữ `Classification.Plan` để chọn có chạy `b-req` hay không; thêm `Depth` và lý do vào cùng state, chọn một lần lúc tạo task rồi dùng lại khi resume. Core truyền depth vào prompt của skill hiện có. Bộ lint của plan artifact nhận diện câu hỏi mở; Runner chặn sau khi đã chuyển task sang `planned`, nên khi có câu trả lời có thể resume thẳng vào `b-feat`.

**Tech Stack:** Go 1.25.1, Cobra, `internal/task` và `internal/workflow` hiện có; không thêm dependency, skill hay workflow engine.

**Spec:** [Thiết kế chất lượng bước lập kế hoạch](../specs/2026-11-10-planning-quality-design.md), mục “Độ sâu phân tích”, “Tương thích” và “Điều kiện nghiệm thu”.

**Prerequisite:** Hoàn thành [Planning Artifact Contract](2026-11-10-planning-artifact-contract.md); dùng `ArtifactReport.OpenQuestions` và `PlanningContractVersion` của plan đó. Plan này vẫn có test và kết quả nghiệm thu riêng.

## Global Constraints

- Giữ bốn workflow gốc và thứ tự `b-do` hiện tại; `standard`/`deep` chỉ đổi nội dung làm việc của `b-req`.
- Không tăng `Assessment.Risk` chỉ vì input dài. Rủi ro đường dẫn bảo vệ và độ phức tạp mô tả phải có lý do riêng.
- `--plan` giữ nghĩa “chạy `b-req`”; `--no-plan` vẫn được người dùng chọn, kể cả khi auto nhận thấy rủi ro. Không thêm cổng người cho mọi task.
- Task cũ thiếu `Classification.Depth` được hiểu là `standard`; resume không tính lại depth và không bắt chạy lại `b-req` đã xong.
- Nội dung trả lời câu hỏi do người dùng/agent ghi lại; Core chỉ kiểm hình thức có câu trả lời, không xác thực quyết định nghiệp vụ.

## Review Focus

1. Mô tả có từ “đổi tên” nhưng chạm `migrations/**`: auto phải chọn `Plan=true`, `Depth=deep`; `--no-plan` vẫn được tôn trọng.
2. Mô tả dài nhưng không chạm đường dẫn bảo vệ: `Depth=deep` vì `complex_request`, còn `Assessment.Risk` phải là `low`.
3. `--no-plan --depth deep` phải lỗi trước khi tạo task; `--plan` một mình không ép `deep`.
4. Task cũ không có field depth và đã `planned`: resume đi theo `standard`, không phát sinh thêm `b-req`.
5. Q chưa trả lời: `b-feat` không được gọi; sau khi ghi `— Trả lời:` và resume, `b-req` không chạy lại.
6. `bint run b-req` chạy riêng gặp Q mở: task ở lại `planned` (không `blocked`), vì `resume` của workflow `b-req` sẽ gọi lại agent `b-req`; `Result.Questions` vẫn có Q.
7. Mô tả `smallWords` ≤25 từ nhưng ≥3 dòng (`Classify` ra `Plan=false`) hoặc `--no-plan`: không lưu `Depth=deep`; depth chỉ có nghĩa khi `Plan=true`.
8. `b-do --task <created, Plan=false> --depth deep`: `Plan` thành true và `PlanningContractVersion` phải lên 1 (qua `ensurePlanningContract` của plan trước), không để task `deep` mà không bị gate artifact.

---

## Bản đồ file

| File | Trách nhiệm |
|---|---|
| `internal/task/ticket.go`, `internal/task/task.go` | Chọn độ sâu và lưu `Classification.Depth`, `DepthReason`. |
| `internal/task/task_test.go` | Phân loại auto/override, legacy JSON. |
| `internal/workflow/ops.go`, `internal/workflow/run.go` | Lưu depth một lần, giữ khi override plan/resume, truyền vào prompt, chặn câu hỏi mở. |
| `internal/cli/run.go`, `internal/cli/task.go` | `--depth auto|standard|deep`, lỗi tổ hợp flag, hiển thị depth/lý do. |
| `internal/skills/core/b-req/SKILL.md`, `internal/skills/core/b-do/SKILL.md` | Một skill `b-req` với hướng dẫn thêm cho deep; b-do chuyển giao depth. |
| `internal/workflow/run_test.go`, `internal/cli/workflow_e2e_test.go` | Prompt, blocked/resume và giữ nguyên đường standard. |
| `README.md` | Cú pháp và ví dụ rủi ro cao/việc nhỏ. |

### Task 1: Chọn và lưu độ sâu, không trộn với rủi ro quyền ghi

**Files:** Modify `internal/task/ticket.go`, `internal/task/task.go`, `internal/task/task_test.go`, `internal/workflow/ops.go`, `internal/workflow/run.go` phần options/override.

**Interfaces:**

- `const DepthStandard = "standard"`, `DepthDeep = "deep"`, `DepthAuto = "auto"` trong `internal/task`.
- `func ChoosePlanningDepth(request string, assessment Assessment) (depth, reason string)`: `deep/protected_paths` khi `Risk=high`; `deep/complex_request` khi >=5 dòng không trống hoặc >100 từ (hằng `DeepMinLines=5`, `DeepMinWords=101` riêng, cao hơn ngưỡng `Plan` của `Classify`); còn lại `standard/default`. Chỉ gọi khi `Plan=true` sau cùng; nếu `Plan=false` thì `Depth`/`DepthReason` để rỗng (`EffectiveDepth()` vẫn trả `standard`).
- `Classification` thêm `Depth string json:"depth,omitempty"` và `DepthReason string json:"depth_reason,omitempty"`; helper `func (c Classification) EffectiveDepth() string` trả `standard` khi field rỗng.
- `NewTaskOptions.Depth string` và `RunOptions.Depth string`; `""`/`auto` nghĩa là tự chọn.

- [ ] **Step 1: Viết test đỏ** cho phân loại protected/complex/default, ưu tiên `protected_paths` khi đồng thời dài và chạm protected, và round-trip JSON thiếu depth. Test `--plan` không ép deep, explicit `deep` ép `Plan=true`, explicit `--no-plan` giữ `Plan=false` khi auto high risk. Các tổ hợp flag CLI được test ở Task 2.
- [ ] **Step 2: Chạy** `go test ./internal/task ./internal/workflow -run 'TestChoosePlanningDepth|TestDepthSelection' -count=1`; kỳ vọng FAIL.
- [ ] **Step 3: Tách helper đếm từ/dòng** từ `Classify` để `ChoosePlanningDepth` dùng cùng cách đếm (không lệch cách tính), nhưng ngưỡng là hằng riêng: ngưỡng `Plan` của `Classify` (>=3 dòng / >60 từ) giữ nguyên để không đổi việc chọn có chạy `b-req` hay không; chỉ ngưỡng `deep` được nâng. Test biên: 4 dòng và đúng 100 từ → `standard`; 5 dòng hoặc 101 từ → `deep`; mô tả 3 dòng có `Plan=true` → `standard/default`. Trong `Env.NewTask`, sau `FromRequest` và `TicketEdit`, gọi trực tiếp `task.Assess(t, e.Cfg.Limits.ProtectedPaths, verifyConfirmed)` trên ticket (task chưa tồn tại trước `Tasks.Create`, nên không dùng `Env.Assess` nhận `*Task`) để quyết `Plan` rồi chọn depth, chỉ khi `Plan=true`; mọi chỗ khiến `Plan` chuyển false→true (kể cả `--depth` tường minh) gọi `ensurePlanningContract` của plan trước; auto high risk đặt `Plan=true` nếu người dùng chưa chỉ định `--no-plan`. Lưu cả `Plan`, `Reason`, `Depth`, `DepthReason`; mọi chỗ đang gán lại `Classification` trong `Run` phải giữ depth hoặc tính lại chỉ khi người dùng đổi plan lúc task chưa bắt đầu.
- [ ] **Step 4: Quy định override cho task có sẵn.** `--depth` tường minh chỉ nhận khi task ở `created`, hoặc khi gọi riêng `bint run b-req --task` trên `planned` để lập kế hoạch lại. Từ `in_progress` trở đi trả lỗi có hướng dẫn, không lặng lẽ đổi depth giữa lượt. Resume luôn đọc depth đã lưu.
- [ ] **Step 5: Chạy** `go test ./internal/task ./internal/workflow -count=1`; kỳ vọng PASS. Commit với thông điệp `feat(task): choose planning depth once`.

### Task 2: CLI và cùng skill `b-req` ở hai mức

**Files:** Modify `internal/cli/run.go`, `internal/cli/task.go`, `internal/skills/core/b-req/SKILL.md`, `internal/skills/core/b-do/SKILL.md`, `internal/workflow/run.go` phần `prompt`, `internal/workflow/run_test.go`, `internal/cli/workflow_e2e_test.go`, `README.md`.

**Interfaces:** `bint task new --depth auto|standard|deep` khi workflow là `b-do`/`b-req`; `bint run b-do|b-req --depth auto|standard|deep`; `task show --json` có `classification.depth`/`depth_reason`. `--depth standard|deep` hàm ý có plan; kết hợp `--no-plan` hoặc workflow `b-feat`/`b-review` bị CLI từ chối. `--depth auto` không đổi nghĩa `--plan`/`--no-plan`.

- [ ] **Step 1: Viết test đỏ** cho parse flag hợp lệ/sai, gồm `--no-plan --depth deep`, `b-feat --depth deep`, và `b-do --task <planned> --depth deep`; `task show` văn bản/JSON; prompt `b-req` có depth và lý do, prompt `b-feat` không tự tạo bước mới. Test core skill vẫn có anchor `project-rules`/`plan-template` để overlay cũ hoạt động.
- [ ] **Step 2: Chạy** `go test ./internal/cli ./internal/workflow ./internal/skills -run 'Test.*Depth|TestCoreSkills' -count=1`; kỳ vọng FAIL ở test mới.
- [ ] **Step 3: Gắn flag và prompt.** `addRunFlags` nhận depth cho `run` chính, không cho `resume`; `newTaskNewCmd` nhận depth. Chỉ xét xung đột khi người dùng đặt `--depth` tường minh (`Flag.Changed`); trả usage error mã 2 với giá trị ngoài enum, `--no-plan` + depth tường minh, hoặc workflow không có `b-req`. `Runner.prompt(StepReq)` thêm `Phân tích: standard|deep` và `Lý do:` từ classification, cả khi state cũ thiếu field.
- [ ] **Step 4: Cập nhật một skill `b-req`.** Với `standard`, giữ ba artifact v1 ngắn. Với `deep`, thêm rà giả định/mâu thuẫn, lựa chọn và lý do, vùng ảnh hưởng, rủi ro/rollback, chiến lược kiểm thử và checklist đối chiếu AC↔TSK; ghi Q cản trở theo khuôn của spec. `b-do` yêu cầu đọc depth trong task và giao cùng task ID. Không thêm skill/phân vai mới; không chèn quy tắc duyệt mới vào prompt.
- [ ] **Step 5: Chạy** `go test ./internal/cli ./internal/workflow ./internal/skills -count=1`; kỳ vọng PASS. Commit với thông điệp `feat(skill): adapt b-req depth to task`.

### Task 3: Chặn câu hỏi mở và resume đúng bước

**Files:** Modify `internal/workflow/run.go`, `internal/workflow/ops.go`, `internal/workflow/run_test.go`, `internal/cli/workflow_e2e_test.go`; nếu cần, sửa parser Q trong `internal/task/artifactlint.go` từ plan trước.

**Interfaces:** Dùng `task.LintPlanningTask(t, true).OpenQuestions`. Không thêm state workflow mới. `Result.Questions` chứa câu hỏi chưa trả lời; reason chỉ rõ `requirement.md` và `bint task lint <id>`.

- [ ] **Step 1: Viết test đỏ**: agent giả `b-req` tạo ba artifact hợp lệ và `- [ ] Q-1`; `b-do` kết thúc `blocked`, status trước pause là `planned`, không có lượt `b-feat`. Câu trả lời `[x] Q-1 ... — Trả lời: ...` được ghi bằng `task write requirement`; `bint run resume` gọi `b-feat`, rồi review, không gọi lại `b-req`. Thêm test `bint run b-req` riêng gặp Q mở: task ở `planned`, `Result.Questions` có Q, không `blocked`; `[x]` nhưng câu trả lời trống vẫn blocked; task legacy và no-plan không bị preflight Q mới chặn.
- [ ] **Step 2: Chạy** `go test ./internal/workflow ./internal/cli -run 'TestOpenQuestion|TestWorkflowEndToEnd' -count=1`; kỳ vọng FAIL.
- [ ] **Step 3: Trong `afterStep(StepReq)`**, sau khi artifact hợp lệ, chuyển `planned` rồi nếu có `OpenQuestions` thì đưa Q vào `Result.Questions`; chỉ `r.block` khi `Workflow=b-do`. Workflow `b-req` chạy riêng giữ `planned` (vì `resume` dùng `State.Workflow` và `nextStep` sẽ gọi lại agent `b-req`) và lệnh thoát khác 0. Trong preflight `StepFeat` trên task v1 planned, chặn Q trước khi gọi agent; trong `Env.SetStatus(planned→in_progress)` thủ công, trả lỗi cùng nội dung. Không chặn `created→planned`: trạng thái planned cho phép ghi câu trả lời rồi resume từ đúng bước.
- [ ] **Step 4: Chạy** các test của bước 2 và `go test ./internal/task ./internal/workflow ./internal/cli -count=1`; kỳ vọng PASS. Commit với thông điệp `feat(workflow): pause for planning questions`.

## Kiểm chứng và đo sau triển khai

- Chạy `go test -count=1 ./...`, `go vet ./...`, `go build ./...`, build Windows/macOS bằng Go 1.25.1; chỉ ghi “qua” khi có exit code thật. Dùng `git diff --check` trước commit.
- Dùng agent giả để chứng minh routing/blocked/resume; sau đó pilot nhỏ trên tác vụ thật **cùng loại** để đo số lần hỏi, tỷ lệ qua lượt đầu, vòng review, thời gian và chi phí/token nếu harness có báo. Không coi pilot nhỏ là gate P1–P3 đã qua.
- Nếu `deep` tăng chi phí mà không giảm câu hỏi muộn hoặc vòng sửa, giữ `--depth deep` thủ công và thu hẹp điều kiện auto; thay đổi ngưỡng cần quyết định riêng, không chỉnh âm thầm theo kết quả pilot.
