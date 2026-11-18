# Yêu cầu

## Mục tiêu và phạm vi
Bước b-goal (internal/workflow/goalfreeze.go) đã có trong mã. Phiếu này chỉ sửa sáu phát hiện review còn mở của GĐ1
(docs/progress/core-improvements-20261118/README.md mục 2): #7, #4, #3, #8, #9, #5. Phát hiện #6 (TMPDIR Windows) đã sửa ở
commit 856802f; #2 là chủ ý của người dùng; #10, #11 (low) không được yêu cầu.

## Yêu cầu chức năng
1. (#7) Khi tệp b-goal viết cần người duyệt phạm vi (protected/deny/ngoài allow khi ask_always/chồng lấn), Bint dừng chờ duyệt như
   hiện nay; resume sau `bint task approve` chỉ chạy phần sau kiểm phạm vi của b-goal (đóng băng, cổng mốc), không gọi lại agent b-goal.
   Áp cho cả `bint run` lẫn driver session (`bint run next`). Trường hợp b-goal không viết tệp nào giữ nguyên: resume chạy lại b-goal.
2. (#4) Người có lối thoát khi test đóng băng sai: cổng mục tiêu đỏ (`BlockGoalFailed`) bỏ qua được bằng `land --force --reason`
   như kiểm chứng đỏ (tác vụ shared đã bỏ qua được bằng `bint task status <id> done --force`). Agent không dùng được cờ ép.
3. (#3) Cổng mục tiêu chỉ chạy test mục tiêu: với lệnh `go test …` và tệp đóng băng `*_test.go`, Bint thêm `-run '^(TestA|TestB)$'`
   gồm đúng các hàm `Test*` khai báo trong tệp đóng băng. Lệnh/tệp khác giữ hành vi cũ.
4. (#8) Rào thực thi b-goal không đọc plan.md/tasks.md: Runner đặt biến môi trường `BINT_DENY_READ` (đường dẫn tuyệt đối plan.md và
   tasks.md của phiếu, mỗi dòng một đường) chỉ cho bước kind `goal`; hook PreToolUse trả deny cho công cụ đọc (Read/Grep/Glob…) trỏ
   tới các tệp đó và lệnh shell có chứa các đường dẫn đó. requirement.md và tệp khác không bị ảnh hưởng.
5. (#9) `TestMilestoneTaskToDone` không đỏ ngẫu nhiên vì đọc `state.json` trong lúc action duyệt đang ghi (Windows báo
   "being used by another process"): điều kiện chờ trong `pump` đọc tác vụ kiểu thử lại (lỗi coi là "chưa đạt"), không `Fatal`.
6. (#5) Bổ sung khẳng định còn thiếu cho `TestGoalFreezeBlackBox` (xem AC-6).

## Tiêu chí chấp nhận
- [ ] AC-1: #7: tệp do b-goal viết cần duyệt phạm vi thì sau bint task approve và resume, agent b-goal không chạy lại (chuỗi bước chỉ có một b-goal) và State.GoalTest ghi sha256 đúng nội dung tệp đã duyệt
- [ ] AC-2: #4: BlockGoalFailed thuộc Forceable nên người chạy bint task land --force --reason đưa được tác vụ worktree có cổng mục tiêu đỏ về (lý do ghi vào Bypassed); agent vẫn bị từ chối --force; BlockGoalMissing vẫn là điều chặn cứng; docs/workflows.md nêu lối thoát này
- [ ] AC-3: #3: lệnh cổng mục tiêu bắt đầu bằng go test và tệp đóng băng là *_test.go có hàm Test thì lệnh thêm -run chỉ gồm đúng tên các hàm Test trong tệp đóng băng; tệp không phải Go (như goal/check.sh) hoặc lệnh đã có -run thì giữ lệnh cũ
- [ ] AC-4: #8: chỉ bước b-goal do Bint khởi chạy nhận biến môi trường BINT_DENY_READ liệt kê đường dẫn tuyệt đối plan.md và tasks.md; hook PreToolUse trả deny cho công cụ đọc hoặc lệnh shell nhắc tới các đường dẫn đó, requirement.md vẫn đọc được
- [ ] AC-5: #9: TestMilestoneTaskToDone không Fatal khi đọc state.json trùng lúc action đang ghi (điều kiện chờ của pump thử lại thay vì Fatal); go test -race -count=3 -run TestMilestoneTaskToDone ./internal/tui/app xanh
- [ ] AC-6: #5: TestGoalFreezeBlackBox kiểm lỗi của r.Approve và ApprovedPaths chứa tệp đóng băng, ca thiếu mục kiểm không có goal-1.log và đề bài b-feat không có khối đóng băng, ca driver session kiểm model b-goal theo models.planner
- [ ] AC-7: kiểm tra mục tiêu TestGoalFixRound xanh

## Kiểm tra mục tiêu
Một test hộp đen ở gói workflow, dùng harness agent giả sẵn có của gói (như `goalBox`/`newHarness` trong goalfreeze_test.go), mỗi phát
hiện có hành vi quan sát được một subtest. #9 và #5 là sửa test, không có hành vi sản phẩm nên không nằm ở đây (đã có AC-5, AC-6).

- **Chuẩn bị**:
  - (#7) b-do có b-goal; agent b-goal giả ghi tệp test vào đường dẫn cần duyệt (vd thêm `goal/**` vào `limits.protected_paths`).
  - (#4) tác vụ cô lập worktree; agent b-goal giả ghi test luôn đỏ (test "sai"), b-feat giả làm đúng mã.
  - (#3) cấu hình `workflow.verify.affected_command: go test -timeout 30m {test_dirs}`; tệp đóng băng giả `x/goal_test.go` khai báo
    `TestAlpha`, `TestBeta` và hàm phụ không phải test; đối chứng tệp `goal/check.sh`.
  - (#8) b-do có b-goal chạy bằng Runner (không phải driver session).
- **Hành động**:
  - (#7) `Run` b-do → blocked chờ duyệt → `Approve` (như người) → `Run` resume.
  - (#4) `Run` tới khi blocked vì cổng mục tiêu đỏ → `Land` với Force+Reason khi Human=true; lặp lại với Human=false.
  - (#3) gọi cổng mục tiêu (hoặc đọc `State.GoalChecks[].Command` sau khi chạy) với hai loại tệp đóng băng.
  - (#8) `Run` b-do, đọc `AgentRequest.Env` của từng bước; gọi trình xử lý hook PreToolUse (hookcore) với biến môi trường đó cho:
    Read plan.md, Read tasks.md, Grep vào tasks.md, Bash `cat <đường plan.md>`, Read requirement.md.
- **Quét/đo**: danh sách bước đã gọi (`h.calls`); `State.GoalTest` (đường dẫn + sha256); `State.Landed`/`Landing.Bypassed`, lỗi trả về
  khi agent; chuỗi lệnh cổng mục tiêu; `Env` của mọi bước (b-req, b-goal, b-feat, b-review); quyết định hook từng lời gọi.
- **Điều kiện đạt**:
  - (#7) chuỗi bước là `b-req,b-goal,b-feat,b-review` (một b-goal), tác vụ done, sha256 trong GoalTest = sha256 nội dung đã duyệt.
  - (#4) người: land thành công, lý do và điều chặn cổng mục tiêu nằm trong Bypassed; agent: lỗi `ErrAgent`, không land.
  - (#3) lệnh chứa `-run` với đúng `TestAlpha|TestBeta` (không có hàm phụ) và vẫn chạy thư mục `./x`; với check.sh lệnh không có `-run`.
  - (#8) chỉ request của b-goal có `BINT_DENY_READ` chứa đúng hai đường dẫn tuyệt đối; hook deny bốn lời gọi đầu, không deny Read requirement.md.
  Mỗi điều kiện sai báo riêng tên subtest và giá trị đo được.
- **Tên test và tệp dự kiến**: `TestGoalFixRound` trong `internal/workflow/goalfix_test.go` (phần hook có thể gọi hàm của hookcore
  nếu xuất được; nếu không, đặt phần hook vào `TestGoalDenyReadHook` trong `internal/harness/hookcore/` và `TestGoalFixRound` chỉ kiểm Env).
  Cả hai nằm trong `go test ./...` của `verify.command`.

## Ngoài phạm vi
- #6 (TMPDIR Windows): đã sửa ở 856802f. #2: chủ ý người dùng. #10, #11 (low): người dùng không nêu; hậu quả: `bint task status done`
  thủ công vẫn không phát hiện cổng mục tiêu lỗi thời (chỉ Runner kiểm).
- Lệnh chạy lại b-goal / đóng băng lại (`--regoal`): không làm; lối thoát của #4 là bỏ qua có lý do. Hậu quả: người muốn sửa test đóng
  băng thì phải ép done/land rồi lập phiếu mới, chưa sửa test rồi chạy tiếp vòng tự động được.
- Rào #8 ở driver session (`bint run next`, subagent chạy trong phiên Claude chính, Runner không đặt được env cho nó): vẫn là ràng buộc
  mềm (đề bài). docs/workflows.md ghi rõ.
- Race đọc `state.json` trên Windows ở mã sản phẩm (TUI đọc lúc ghi): chỉ sửa test (#9).

## Câu hỏi mở
Không có.
