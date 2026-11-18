# Review tác vụ 01M7PD9T2ASG8XXMEM7SJ2G6C1

## Vòng 1 — 2026-11-18T04:53:06Z

Sáu sửa đúng hướng, test mục tiêu TestGoalFixRound/TestGoalDenyReadHook đúng đặc tả và kiểm chứng xanh; còn một chỗ sót của #7 ngoài diff: khi hook chặn trước (Codex, ProtectedPathDeny) rồi người duyệt bằng --path như hook hướng dẫn, resume nhảy thẳng sang b-feat và bỏ hẳn b-goal. Chưa tự chạy -race -count=3 cho #9 và chưa thử harness thật.

### [01M7PD9T2ASG8XXMEM7SJ2G6C1.1] bug/high — Duyệt b-goal bằng approve --path (đường của Codex) làm resume bỏ hẳn b-goal, không có tệp đóng băng

- Vị trí: `internal/workflow/ops.go:1121-1126`
- Bằng chứng: Codex dialect có ProtectedPathDeny=true (internal/harness/codex/codex.go:115-118): b-goal ghi vào protected_paths bị hook deny trước khi có tệp, approvalHelp (codex.go:126-135) bảo người chạy `bint task approve <id> --path '<tệp>'` rồi resume. Runner: deny → PendingPaths + blocked, runStep trả false nên afterGoal không chạy, GoalAwaitingCheck=false (run.go:1551-1561). Approve có --path trên tác vụ đang dừng đặt ResumeFrom=in_progress (ops.go:1121-1126, ghi chú 'b-feat chưa thực hiện phần việc này'), nên resume: pickStep thấy in_progress → currentImplement → b-feat; goalPending đòi status planned nên b-goal không bao giờ chạy lại, GoalTest nil, DoneBlockers không có điều chặn cổng mục tiêu → tác vụ done mà không có kiểm tra mục tiêu độc lập. TestGoalFixRound #7 chỉ kiểm agent giả ghi thẳng tệp (bỏ qua hook) nên không bắt được đường này.
- Tiêu chí: 1

Ở nhánh explicit của Approve, chỉ đặt ResumeFrom=in_progress khi tác vụ đã qua b-goal (GoalTest khác nil hoặc flow không có b-goal); khi bước bị chặn là b-goal giữ ResumeFrom=planned để resume chạy lại b-goal với BINT_APPROVED_PATHS mới. Thêm ca test: hook deny (tệp denials của child run) + approve --path + resume → chuỗi có b-goal và GoalTest khác nil.

### [01M7PD9T2ASG8XXMEM7SJ2G6C1.2] bug/low — docs nói status done --force của tác vụ shared ghi lý do vào Landed.Bypassed — không đúng

- Vị trí: `docs/workflows.md:402-404`
- Bằng chứng: SetStatus (internal/workflow/ops.go:548-562) với --force chỉ nối danh sách điều chặn vào reason của lịch sử trạng thái; Landed.Bypassed chỉ được đặt trong Land (land.go:283-284). status done --force cũng không bắt buộc --reason và bỏ qua mọi điều chặn (kể cả điều chặn cứng), không chỉ loại Forceable.
- Tiêu chí: 2

Sửa câu: worktree → land --force --reason (ghi Landed.Bypassed); shared → status done --force (ghi vào lịch sử trạng thái).

### [01M7PD9T2ASG8XXMEM7SJ2G6C1.3] risk/low — GoalTest.RunID rỗng khi đóng băng ở lượt resume (stepGoalCheck)

- Vị trí: `internal/workflow/goalfreeze.go:125-131`
- Bằng chứng: runID lấy từ rs.res.Steps của lượt Run hiện tại; ở resume qua stepGoalCheck không có bước agent nào chạy nên rs.res.Steps rỗng và GoalTest.RunID thành chuỗi rỗng, mất liên kết tới lượt b-goal đã viết tệp.
- Tiêu chí: 1

Lưu run id của b-goal (vd từ LastStep, hoặc ghi cùng lúc đặt GoalAwaitingCheck) và dùng khi đóng băng ở lượt resume.

### [01M7PD9T2ASG8XXMEM7SJ2G6C1.4] suggestion/low — Chưa có test khẳng định BINT_DENY_READ bị lọc khỏi env kiểm chứng

- Vị trí: `internal/eval/verify_env_test.go:12`
- Bằng chứng: shell.go đã thêm BINT_DENY_READ vào cleanEnv/verifyStripped, nhưng danh sách stripped trong verify_env_test.go:12 và resume_finalize_test.go:203 không có biến này nên bỏ nó khỏi map sẽ không làm test đỏ.
- Tiêu chí: 4

Thêm BINT_DENY_READ vào hai danh sách stripped của test.

### [01M7PD9T2ASG8XXMEM7SJ2G6C1.5] risk/low — Rào đọc không khớp dạng đường /c/Users/... của Git Bash trên Windows

- Vị trí: `internal/harness/hookcore/deny_read.go:36-69`
- Bằng chứng: denyReadDecision chỉ đổi dấu gạch ngược thành / và hạ chữ thường; lệnh Bash trên Windows hay dùng dạng MSYS /c/users/.../plan.md, không chứa chuỗi c:/users/.../plan.md nên không bị deny. Đây là cách viết đường mặc định của shell, không phải né cố ý.
- Tiêu chí: 4

Khi so lệnh shell, thêm biến thể MSYS (/<ổ>/...) của mỗi đường dẫn tuyệt đối có ký tự ổ đĩa.

## Vòng 2 — 2026-11-18T05:17:14Z

KHÔNG HỘI TỤ: 1 nghiêm trọng (vòng trước 1). Phát hiện high vòng 1 (.1, approve --path làm bỏ b-goal) đã sửa đúng ở ops.go:1121 kèm TestGoalHookDenyThenApprovePathRerunsGoal; high lần này là lỗi MỚI ở chỗ sót ngoài diff của #8: matcher PreToolUse cài cho Claude không gồm Grep/Glob nên rào đọc không chặn được Grep vào tasks.md trong phiên thật (test mục tiêu gọi thẳng HandleHook nên vẫn xanh). Ngoài ra lệnh -run của #3 vỡ dưới cmd /C trên Windows không có sh. Đã chạy: go test -race -count=3 -run TestMilestoneTaskToDone ./internal/tui/app (3 passed); TestGoalFixRound, TestGoalHookDenyThenApprovePathRerunsGoal, TestGoalRunOnly, TestGoalFreezeBlackBox, TestGoalDenyReadHook, TestBlockerKindForceableAllowlist (36 passed). Chưa thử harness Claude/Codex thật, chưa chạy trên Windows; các low .3/.4/.5 của vòng 1 vẫn còn nguyên, không ghi lại.

### [01M7PD9T2ASG8XXMEM7SJ2G6C1.6] bug/high — Hook PreToolUse của Claude không nhận Grep/Glob nên rào đọc b-goal không chặn được Grep vào tasks.md

- Vị trí: `internal/harness/claude/install.go:63-107`
- Bằng chứng: install.go:67 pathMatcher = "Read|Edit|Write|MultiEdit|NotebookEdit|Bash" và :107 đăng ký PreToolUse với matcher này; Claude Code chỉ gọi hook cho công cụ khớp matcher nên lời gọi Grep(path=<task dir>/tasks.md) không bao giờ tới hookcore.Handle, denyReadDecision (hook.go:376) không được chạy và nội dung tasks.md trả về cho agent b-goal. Runner không ghi đè hook cho tiến trình bước (workflow không đặt AgentRequest.Settings), nên b-goal dùng đúng matcher đã cài. Kiểm tra mục tiêu liệt kê 'Grep vào tasks.md' trong bốn lời gọi phải deny; TestGoalDenyReadHook xanh vì gọi thẳng claude.HandleHook, bỏ qua matcher.
- Tiêu chí: 4

Thêm Grep|Glob vào matcher PreToolUse (pathMatcher hoặc một group riêng cho rào đọc) và đảm bảo bint init/upgrade cập nhật settings đã cài; thêm test khẳng định matcher đã cài gồm mọi công cụ mà deny_read xử lý (Read, Grep, Glob, Bash). Cân nhắc thêm: Grep/Glob với path là chính thư mục tác vụ (không nêu plan.md) vẫn đọc được plan/tasks; có thể deny Grep có path là thư mục chứa một đường dẫn bị cấm.

### [01M7PD9T2ASG8XXMEM7SJ2G6C1.7] bug/medium — Lệnh cổng mục tiêu với -run '^(A|B)$' vỡ khi Windows chạy qua cmd /C (không có sh)

- Vị trí: `internal/workflow/goalfreeze.go:241`
- Bằng chứng: goalRunOnly ghép "go test -run '^(" + strings.Join(names, "|") + ")$'"; eval.RunShell trên Windows rơi về cmd /C khi không tìm thấy sh trong PATH (internal/eval/shell.go:36-40; Git for Windows mặc định chỉ thêm Git\cmd, không thêm sh). Dưới cmd, nháy đơn không phải dấu trích dẫn, ^ là ký tự thoát và | là pipe: lệnh thành 'go test -run '(TestAlpha' | 'TestBeta)$' -timeout 30m ./x' → vế phải không phải lệnh, mã thoát khác 0, cổng mục tiêu đỏ dù mã đúng. Trước thay đổi lệnh 'go test -timeout 30m ./x' chạy được dưới cmd nên đây là hồi quy; CI Windows có sh nên không bắt được.
- Tiêu chí: 3

Dùng nháy kép: -run "^(TestA|TestB)$" chạy đúng cả dưới sh ("$" trước dấu nháy đóng là ký tự thường) lẫn cmd (| và ^ trong nháy kép là ký tự thường); thêm ca so chuỗi trong TestGoalRunOnly.

### [01M7PD9T2ASG8XXMEM7SJ2G6C1.8] bug/low — docs nói status done --force của tác vụ shared ghi Landed.Bypassed và giữ 'chưa chạy cổng mục tiêu' là điều chặn cứng — cả hai sai với tác vụ shared

- Vị trí: `docs/workflows.md:402-404`
- Bằng chứng: SetStatus (internal/workflow/ops.go:548-560) với Force bỏ qua mọi điều chặn của doneBlockersFor (gồm cả BlockGoalMissing, không xét Forceable) và chỉ ghi lý do vào lịch sử trạng thái; Landed.Bypassed chỉ do land --force ghi. Forceable chỉ được dùng ở land.go:121. Phát hiện .2 của vòng 1 chưa được sửa, nay thêm câu 'Chưa chạy cổng mục tiêu vẫn là điều chặn cứng' cũng chỉ đúng với land.
- Tiêu chí: 2

Tách hai đường: worktree → land --force --reason (Bypassed, BlockGoalMissing cứng); shared → status done --force (lý do vào lịch sử, ép được mọi điều chặn về bằng chứng).

## Vòng 3 — 2026-11-18T05:52:26Z

Vòng 3: phát hiện high .6 (Grep/Glob không qua hook) đã sửa đúng bằng settings riêng của bước b-goal (mcp.go stepDenyReadSettings, run.go truyền Settings, eval/agent.go:218 thêm --settings), kèm TestGoalStepGetsGrepGlobHookSettings/TestGoalStepNoGrepGlobSettingsOnOtherHarness; số phát hiện nghiêm trọng giảm 1 → 0. Còn: rào đọc không chặn đọc theo thư mục tác vụ (thư mục lộ qua đường requirement.md trong đề bài), lệnh -run nháy đơn vỡ dưới cmd /C (.7 chưa sửa), docs lối thoát shared sai (.8 chưa sửa), GoalTest.RunID rỗng khi đóng băng qua resume. Đã chạy: go test -count=1 -run 'TestGoalFixRound|TestGoalStepGetsGrepGlobHookSettings|TestGoalStepNoGrepGlobSettingsOnOtherHarness|TestGoalHookDenyThenApprovePathRerunsGoal|TestGoalRunOnly|TestGoalFreezeBlackBox|TestGoalDenyReadHook' ./internal/workflow ./internal/harness/hookcore → 37 passed. Chưa kiểm: Claude CLI thật có hợp nhất hook từ --settings với settings dự án hay không, chạy trên Windows, driver session đi qua stepGoalCheck (không có test).

### [01M7PD9T2ASG8XXMEM7SJ2G6C1.9] risk/medium — Rào BINT_DENY_READ không chặn đọc theo thư mục tác vụ, mà đề bài b-goal lại lộ thư mục đó

- Vị trí: `internal/harness/hookcore/deny_read.go:36-60`
- Bằng chứng: denyReadDecision chỉ deny khi một chuỗi trong tool_input chứa NGUYÊN đường dẫn tuyệt đối plan.md/tasks.md. Đề bài b-goal vẫn in '- requirement: <state>/runtime/tasks/<id>/requirement.md' (run.go:2869-2876 chỉ bỏ plan/tasks), nên agent biết thư mục chứa plan.md/tasks.md. Các lời gọi tự nhiên, không cố ý né, đều lọt: Grep với path=<thư mục tác vụ> (output_mode content) trả cả dòng của plan.md/tasks.md; Grep path=<thư mục> glob='plan.md'; Bash 'cat <thư mục>/*.md', 'grep -rn X <thư mục>', 'cd <thư mục> && cat plan.md' — không chuỗi nào chứa '<thư mục>/plan.md'. AC-4 theo câu chữ đạt (lời gọi nêu đúng đường dẫn bị deny), nhưng mục tiêu '#8 rào thực thi b-goal không đọc plan/tasks' chỉ đạt một phần; docs/workflows.md chỉ nhận ghép chuỗi/biến là lối lọt 'cố ý'.
- Tiêu chí: 4

Rẻ nhất: không in đường dẫn requirement.md cho b-goal (nội dung requirement đã chép nguyên văn vào đề bài qua goalPromptBlock) hoặc chép requirement.md sang thư mục tạm riêng của bước. Thêm: deny khi tool_input nhắc tới thư mục cha của plan.md mà không phải đúng requirement.md (Grep/Glob path là thư mục, lệnh shell có '<thư mục>/' hoặc kết thúc bằng '<thư mục>'). Thêm ca vào test hook.

### [01M7PD9T2ASG8XXMEM7SJ2G6C1.10] bug/medium — Lệnh cổng mục tiêu -run '^(A|B)$' vỡ khi Windows chạy qua cmd /C (phát hiện .7 vẫn chưa sửa)

- Vị trí: `internal/workflow/goalfreeze.go:241`
- Bằng chứng: goalRunOnly vẫn trả "go test -run '^(" + ... + ")$'" bằng nháy đơn. eval/shell.go:37-40: Windows không có sh trên PATH thì chạy 'cmd /C <lệnh>'; cmd không hiểu nháy đơn nên '|' thành pipe và '^' là ký tự thoát → cổng mục tiêu đỏ/lỗi dù test đúng (goal-N.log sai, BlockGoalFailed). Git for Windows mặc định chỉ đưa git.exe lên PATH, không có sh.
- Tiêu chí: 3

Dùng nháy kép: -run "^(TestA|TestB)$" hợp lệ cả sh lẫn cmd (trong sh, '$' đứng trước '"' là chữ thường; trong cmd, '|' và '^' trong nháy kép không bị diễn giải). Cập nhật bảng TestGoalRunOnly và docs/workflows.md (đang ghi dạng nháy đơn).

### [01M7PD9T2ASG8XXMEM7SJ2G6C1.11] bug/low — docs: status done --force (shared) không ghi Landed.Bypassed và bỏ qua cả 'chưa chạy cổng mục tiêu' (phát hiện .8 vẫn còn)

- Vị trí: `docs/workflows.md:402-406`
- Bằng chứng: Docs nói sau 'bint task land --force' hoặc 'bint task status <id> done --force' thì 'lý do và điều chặn được ghi vào Landed.Bypassed' và 'Chưa chạy cổng mục tiêu vẫn là điều chặn cứng'. Thực tế chỉ land.go:284 ghi Bypassed; SetStatus (ops.go:548-551) với Force bỏ qua mọi blocker (kể cả BlockGoalMissing) và chỉ phát sự kiện spool forced=true (ops.go:588).
- Tiêu chí: 2

Tách câu: worktree → land --force --reason, ghi Landed.Bypassed, goal-missing cứng; shared → status done --force bỏ qua mọi điều chặn, chỉ ghi sự kiện task.status forced.

### [01M7PD9T2ASG8XXMEM7SJ2G6C1.12] bug/low — GoalTest.RunID rỗng khi đóng băng qua stepGoalCheck sau duyệt phạm vi (#7)

- Vị trí: `internal/workflow/goalfreeze.go:124-128`
- Bằng chứng: afterGoal lấy runID từ rs.res.Steps cuối; khi resume vào stepGoalCheck (run.go:549-557, session.go:576-581) lượt mới chưa có bước nào nên rs.res.Steps rỗng → GoalTest.RunID = ''. Dấu vết lượt b-goal đã viết tệp đóng băng bị mất đúng ở đường #7; TestGoalFixRound/#7 chỉ kiểm sha256, không kiểm RunID.
- Tiêu chí: 1

Lấy run ID b-goal từ State.LastStep/StepStop của bước b-goal (hoặc lưu cùng cờ GoalAwaitingCheck) khi rs.res.Steps rỗng.

### [01M7PD9T2ASG8XXMEM7SJ2G6C1.13] risk/low — Rào đọc b-goal tắt im lặng khi hook lỗi nạp cấu hình/Git

- Vị trí: `internal/harness/hookcore/hook.go:207-221`
- Bằng chứng: Handle trả sớm (readonlyFailClosed chỉ cho guardrail chỉ đọc của subagent, hoặc Skipped) trước nhánh EvPreTool ở dòng 373-388, nên khi .bint/config.yml hỏng hoặc git lỗi, BINT_DENY_READ có giá trị cũng không deny. Kiểm BINT_DENY_READ chỉ cần biến môi trường + tool_input, không cần cấu hình.
- Tiêu chí: 4

Đưa kiểm denyReadDecision lên trước các lối thoát phụ thuộc cấu hình (giống readonlyFailClosed), vẫn không trả lỗi [k:01M746NPTW1B304MRQQPBCAFAM].
