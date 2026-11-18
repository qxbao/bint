# Review tác vụ 01M7NA28NAXVSE14HNHACRZG97

## Vòng 1 — 2026-11-17T19:50:53Z

Cài đặt b-goal/đóng băng/cổng mục tiêu bám sát 5 tiêu chí và TestGoalFreezeBlackBox xanh (gói workflow chỉ đỏ ở TestVerifyUsesPrivateTempDir), nhưng kiểm chứng cuối đỏ vì các test ngoài diff, .bint/config.yml bị đổi làm verify.command mất xác nhận, và cổng mục tiêu có hai rủi ro thiết kế (chạy cả gói, không có lối thoát khi test đóng băng sai). Chưa kiểm: chạy thật với model/harness khác, đường Codex của driver session.

### [01M7NA28NAXVSE14HNHACRZG97.1] risk/high — Kiểm chứng cuối đỏ: 9 test ngoài diff hỏng (eval hạ tầng, TMPDIR dạng MSYS), không do mã của tác vụ

- Vị trí: `internal/workflow/bench_frictions_test.go:20-46`
- Bằng chứng: verify-1.log: FAIL ở internal/cli (TestEvalMineFromTasksDebugEndToEnd, TestEvalMinimalFromBootstrapSnapshot: fail→pass 'error'), internal/eval (TestRunHarnessVariantRecordsGuardrailUsage: thiếu agents.txt), internal/evolve (TestEvaluateSkillProposal: '6/12 lượt lỗi hạ tầng'), internal/tui/actions, internal/tui/app (eval), internal/workflow TestVerifyUsesPrivateTempDir (bench_frictions_test.go:46: TMPDIR="/c/Users/..." dạng MSYS thay vì đường dẫn Windows). Không tệp nào trong số này nằm trong diff; TestGoalFreezeBlackBox và các test khác của gói workflow qua.
- Tiêu chí: 5

Chạy lại go test cho các gói này trên HEAD (3987a8d) không có thay đổi để xác nhận là lỗi sẵn có/môi trường (Windows, sh lồng dưới bint verify). Nếu đúng thì đây không phải lỗi của tác vụ nhưng vẫn chặn done; cần người quyết (--force kiểm chứng đỏ, hoặc sửa riêng). TestVerifyUsesPrivateTempDir nên so sau khi chuẩn hoá đường dẫn MSYS.

### [01M7NA28NAXVSE14HNHACRZG97.2] risk/medium — .bint/config.yml bỏ -count=1 khỏi eval.test_command: verify.command của phiếu mất xác nhận

- Vị trí: `.bint/config.yml:96-162`
- Bằng chứng: Diff đổi test_command thành 'go test -timeout 30m ./...' (dòng 96) và affected_command bỏ -count=1 (dòng 162), thêm small_plan_max_files; yêu cầu không nhắc. commandConfirmed (internal/workflow/ops.go:746-750) so khớp nguyên văn với eval.test_command, nên phiếu này (verify.command 'go test -count=1 -timeout 30m ./...') giờ hiện '[chưa xác nhận]' và 'Chưa thể done: lệnh kiểm chứng chưa được người xác nhận' trong bint task show. State.touched ghi .bint/config.yml (có thể do người sửa trên cây shared lúc b-feat chạy, log b-feat không thấy lệnh sửa).
- Tiêu chí: 3

Nếu không phải chủ ý của người: tách thay đổi này khỏi tác vụ hoặc khôi phục -count=1. Bỏ -count=1 còn cho phép go test dùng cache cho test phụ thuộc git/tệp ngoài.

### [01M7NA28NAXVSE14HNHACRZG97.3] risk/medium — Cổng mục tiêu với {test_dirs} chạy cả gói: không độc lập và nhân đôi thời gian kiểm chứng

- Vị trí: `internal/workflow/goalfreeze.go:188-194`
- Bằng chứng: goalCommand dùng eval.ExpandTestCommand(affected_command, GoalTest.Paths()); với cấu hình repo 'go test -timeout 30m {test_dirs}' tệp internal/workflow/goalfreeze_test.go thành 'go test -timeout 30m ./internal/workflow' — chạy mọi test của gói (~600 s trên Windows, k:01M7CJ92ZB2DRYYHGFBQABYM9D) sau MỖI bước implement, cộng với kiểm chứng. Test không liên quan đỏ trong cùng gói (vd TestVerifyUsesPrivateTempDir ở lần chạy này) làm cổng mục tiêu đỏ → BlockGoalFailed cứng ở DoneBlockersTyped.
- Tiêu chí: 3

Thu hẹp bằng -run theo tên test trong mục Kiểm tra mục tiêu (hoặc biến {goal_tests}), hoặc ít nhất ghi rõ trong docs rằng cổng mục tiêu đo cả gói.

### [01M7NA28NAXVSE14HNHACRZG97.4] risk/medium — Không có lối thoát khi test đóng băng sai: BlockGoalFailed không forceable, không lệnh chạy lại b-goal

- Vị trí: `internal/task/status.go:153-158`
- Bằng chứng: BlockGoalFailed không nằm trong Forceable() (status.go:153-158) trong khi BlockVerifyFailed thì có; goalPending chỉ chạy b-goal khi GoalTest==nil và status planned (goalfreeze.go:37-53), không có lệnh nào xoá GoalTest. docs/workflows.md tự ghi 'Chưa có đường bỏ đóng băng khi chính test sai: người huỷ phiếu'. b-feat ghi tiêu chí unmet theo skill cũng không gỡ được chặn.
- Tiêu chí: 3

Thêm lối của người (vd bint run resume --regoal hoặc cho BlockGoalFailed vào Forceable với land --force), nhất quán với kiểm chứng đỏ.

### [01M7NA28NAXVSE14HNHACRZG97.5] suggestion/low — TestGoalFreezeBlackBox bỏ qua vài điểm đặc tả

- Vị trí: `internal/workflow/goalfreeze_test.go:214-304`
- Bằng chứng: r.Approve(res.TaskID) (dòng 214) bỏ qua lỗi và không kiểm tệp đóng băng đã vào ApprovedPaths, nên 'approve không bỏ qua' có thể qua vacuous nếu Approve lỗi; ca thiếu mục (273-287) không so đề bài b-feat với bản cũ và không kiểm không có goal-*.log; ca driver session (289-304) không kiểm action.Model = model role planner.
- Tiêu chí: 5

Kiểm err của Approve và ApprovedPaths chứa goal/check.sh; thêm các khẳng định còn thiếu.

## Vòng 2 — 2026-11-17T20:24:35Z

KHÔNG HỘI TỤ: 1 nghiêm trọng (vòng trước 1) — vẫn là lỗi cũ chưa xử lý: kiểm chứng cuối đỏ do hạ tầng tạm của Bint trên Windows (ngoài diff), không do mã tác vụ; TestGoalFreezeBlackBox và gói flow/task/harness xanh. Mã b-goal/đóng băng/cổng mục tiêu đúng các AC chính; còn vài rủi ro mức trung bình (chạy lại b-goal sau duyệt phạm vi, cô lập plan chỉ ở đề bài). Chưa tự chạy lại được go test (reviewer không có quyền chạy go).

### [01M7NA28NAXVSE14HNHACRZG97.6] risk/high — Kiểm chứng cuối vẫn đỏ: 8 test hạ tầng do TMPDIR riêng của Bint (đường dẫn quá dài, dạng MSYS) — lỗi cũ, ngoài diff

- Vị trí: `internal/workflow/ops.go:872-896`
- Bằng chứng: verify-2.log giống verify-1.log: TestEvalRunJobPlanBeforeConfirmAndDeclineCreatesNoBatch 'git init -q --bare ... Filename too long' dưới C:/Users/admin/AppData/Local/bint/state/bint-8d262468937e/tmp/verify-1877255678/...; TestVerifyUsesPrivateTempDir (bench_frictions_test.go:46) nhận TMPDIR='/c/Users/...' (sh.exe đổi sang dạng MSYS); TestRunHarnessVariantRecordsGuardrailUsage, TestEvaluateCreateProposal/SkillProposal (4/8, 6/12 lượt lỗi hạ tầng), TestEvalMineFromTasksDebugEndToEnd, TestEvalMinimalFromBootstrapSnapshot, TestEvalActionsAgainstRealService, TestEvalRunEscCancelsBatchAndShowsResumeCommand. verifyTempDir đặt thư mục tạm dưới state.TmpDirFor (đường sâu) nên repo eval lồng vượt MAX_PATH. Không tệp nào trong số này thuộc diff; gói workflow chỉ đỏ ở TestVerifyUsesPrivateTempDir, TestGoalFreezeBlackBox qua.

b-feat không sửa được trong phạm vi tác vụ: cần người quyết (chạy verify ngoài Bint, hoặc tác vụ riêng rút ngắn đường dẫn tạm/chuẩn hoá TMPDIR trên Windows). Đừng tính đây là lỗi của G1.

### [01M7NA28NAXVSE14HNHACRZG97.7] bug/medium — Phạm vi b-goal cần duyệt thì resume chạy lại b-goal từ đầu thay vì đóng băng tệp đã duyệt

- Vị trí: `internal/workflow/goalfreeze.go:91-94`
- Bằng chứng: afterGoal gọi checkScope trước khi ghi State.GoalTest; checkScope dừng chờ duyệt (tệp test ngoài scope.allow hoặc protected) thì trả false khi GoalTest còn nil và trạng thái vẫn planned. Khi resume, pickStep (run.go:824-833) chỉ dùng AwaitingCheck cho in_progress nên rơi vào goalPending()==true (goalfreeze.go:42) → chạy lại agent b-goal phiên mới; tệp người vừa duyệt không được đóng băng trực tiếp và có thể bị viết lại.
- Tiêu chí: 2

Cho bước goal một nhánh tiếp tục (vd đánh dấu AwaitingCheck cho goal, hoặc goalPending trả false khi Touched đã có tệp và phạm vi vừa được duyệt) để resume chỉ chạy phần sau checkScope; thêm subtest.

### [01M7NA28NAXVSE14HNHACRZG97.8] risk/medium — b-goal không thấy plan/tasks chỉ được giữ ở đề bài, không có rào thực thi

- Vị trí: `internal/workflow/goalfreeze.go:273-287`
- Bằng chứng: Đề bài chỉ bỏ đường dẫn plan/tasks (run.go:2847) và dặn không đọc; nhưng requirement.md nằm cùng thư mục với plan.md/tasks.md (đường dẫn requirement có trong đề bài), bint task show vẫn trả đường dẫn plan, và ở driver session b-goal chạy bằng subagent bint-implementer có Read/Bash đầy đủ (subagents.go:72, flowkind.go:137). Model đọc nhầm plan là mất tính độc lập mà yêu cầu nhắm tới.
- Tiêu chí: 1

Chấp nhận được cho G1 nếu ghi rõ ở docs là ràng buộc mềm; cân nhắc guardrail hook deny đọc plan.md/tasks.md khi PendingStep là b-goal, hoặc ghi dấu vết (transcript có đọc plan.md) để review thấy.

### [01M7NA28NAXVSE14HNHACRZG97.9] risk/medium — TestMilestoneTaskToDone mới đỏ ở verify-2 (khoá tệp state.json trên Windows), test bị diff sửa

- Vị trí: `internal/tui/app/tasks_milestone_test.go:76-77`
- Bằng chứng: verify-2.log:121 'open .../runtime/tasks/01M7NHTTE1GHZS9BXKV7DS87P0/state.json: The process cannot access the file because it is being used by another process' trong f.pump đọc f.task(id) lúc action duyệt đang ghi; verify-1 qua. Diff chỉ thêm bước b-goal (tasks_fixture_test.go ghi goal_check.sh) nên luồng dài hơn, nhiều lần ghi state hơn — nhiều khả năng là race có sẵn lộ ra, chưa tái hiện được.

Cho f.task trong pump bỏ qua lỗi chia sẻ tạm (thử lại) thay vì Fatal, hoặc chờ kết quả action trước khi đọc (k:01M7EGY5D5F79ZD69328GENX02).

### [01M7NA28NAXVSE14HNHACRZG97.10] suggestion/low — TestGoalFreezeBlackBox chưa quét hết mục Quét/đo của đặc tả

- Vị trí: `internal/workflow/goalfreeze_test.go:95-304`
- Bằng chứng: Đặc tả đòi kiểm kết quả cổng mục tiêu trong bint task show, harness/model b-goal = role planner, và ca --fast/thiếu mục không có goal-*.log. Test chỉ kiểm role (dòng 106), không kiểm model, không gọi bint task show (cli/task.go:450-455 không được test nào trong diff phủ), ca thiếu mục (273-287) không kiểm vắng goal-*.log và đề bài b-feat.
- Tiêu chí: 5

Thêm assert model theo models.planner, stat goal-1.log vắng ở hai ca đối chứng, và một kiểm output show (ở gói cli).

### [01M7NA28NAXVSE14HNHACRZG97.11] suggestion/low — doneBlockers không kiểm cổng mục tiêu lỗi thời cho đường done thủ công

- Vị trí: `internal/task/status.go:222-229`
- Bằng chứng: Chỉ chặn khi chưa chạy hoặc lần cuối đỏ; không so Worktree của lần cổng với cây hiện tại (khác cách verify làm ở 211-217) và không kiểm sha256. goalFinal trong workflow có kiểm, nhưng bint task status <id> done của người đi qua doneBlockers.
- Tiêu chí: 3

Thêm BlockGoalStale khi g.Worktree != in.Worktree, tương tự BlockVerifyStale.
