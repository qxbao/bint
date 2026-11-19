# Review tác vụ 01M7PD9T5R587EKT2Z82GJVZAJ

## Vòng 1 — 2026-11-18T08:02:35Z

Lớp lát mỏng (slice.go) đạt phần lớn AC-1..AC-3, AC-5 và TestSliceRatchet đúng đặc tả; còn một lỗ ở AC-4 (agent chạm trần chi phí/quota giữa lát bị coi là lát bỏ và resume bỏ qua lát đó), ref slice-N-discarded không bao giờ bị xoá, và rủi ro lát 1 không bao giờ chốt được khi test mục tiêu Go chưa biên dịch.

### [01M7PD9T5R587EKT2Z82GJVZAJ.1] bug/high — Hết ngân sách/quota giữa một lát: lát dở bị đánh dấu discarded và resume bỏ qua nó (AC-4)

- Vị trí: `internal/workflow/run.go:1596-1608`
- Bằng chứng: beginStep truyền phần ngân sách còn lại làm maxCost của agent (maxCost = minPtr(maxCost, rem)); agent vượt trần thì res failed với CostUSD≈maxCost. Nhánh lát mới (run.go:1597-1603) đứng TRƯỚC kiểm costCap (1604-1608) nên mọi lỗi (chạm chi phí, QuotaHit, Infra, mạng) thành sliceFail → sliceSettleFailure → discardSlice(..., stop=true) (slice.go:437). discardSlice ghi lát Status=discarded và activateNext kích hoạt lát KẾ (slice.go:475-482), nên Result.Slices xếp lát dở vào 'đã bỏ' (lý do 'agent lỗi: …') chứ không phải 'chưa làm', và resume sau khi nâng ngân sách chạy lát N+1, không chạy lại lát N. Với lát 1, resume chạy lát 2 trên điểm xanh đầu mà lát 1 không bao giờ được làm. AC-4 đòi 'hết ngân sách limits.workflows … lát dở đưa về điểm xanh, blocked, … lát chưa làm; resume chạy tiếp các lát chưa làm'; plan.md (Giả định) cũng ghi 'Agent lỗi trong lát … người resume là chạy lại lát đó'. Ca (e) của TestSliceRatchet chỉ phủ trường hợp ngân sách cạn đúng ranh giới (b-feat tốn đúng 1.0 = phần còn lại, chặn ở beginStep), không phủ ca thực tế agent bị cắt giữa lát.
- Tiêu chí: 4

Với lỗi không phải quá trần lát (costCap, QuotaHit, Infra, lỗi agent khác): vẫn đưa cây về điểm xanh (ghim slice-N-discarded làm đường lui) nhưng giữ lát ở active/pending (không activateNext), lý do block nêu 'hết ngân sách'/'lỗi agent' kèm sliceTail; resume chạy lại đúng lát đó. Thêm ca test: costFor của b-feat lát 2 > phần còn lại → blocked vì ngân sách, L2 vẫn 'chưa làm', resume chạy lát 2/3.

### [01M7PD9T5R587EKT2Z82GJVZAJ.2] bug/medium — deleteTaskRefs không xoá được refs/bint/tasks/<id>/slice-N-discarded (for-each-ref không khớp tiền tố 'slice-')

- Vị trí: `internal/workflow/touched.go:116-122`
- Bằng chứng: git for-each-ref chỉ khớp pattern theo fnmatch hoặc tiền tố trọn tới dấu '/' (git-for-each-ref(1): 'matching completely or from the beginning up to a slash'; ref-filter.c match_name_as_path yêu cầu refname[plen] là NUL hoặc '/', hoặc pattern kết thúc bằng '/'). Pattern 'refs/bint/tasks/<id>/slice-' không có ký tự đại diện, không kết thúc bằng '/', nên với refname 'refs/bint/tasks/<id>/slice-2-discarded' (ký tự kế là '2') không khớp → danh sách rỗng, ref lát bị bỏ còn mãi (giữ object, trái với docs/workflows.md:61 'Các ref lát xoá cùng base khi tác vụ kết thúc'). slice-green vẫn xoá được vì được liệt kê tên đầy đủ. Không có test nào kiểm deleteTaskRefs với ref slice-*-discarded.
- Tiêu chí: 3

Dùng pattern 'refs/bint/tasks/<id>/' (rồi lọc tiền tố slice-) hoặc glob 'refs/bint/tasks/<id>/slice-*'; thêm test land/done/cancel sau khi có lát bị bỏ kiểm ref biến mất.

### [01M7PD9T5R587EKT2Z82GJVZAJ.3] risk/medium — Test mục tiêu Go chưa biên dịch làm lát 1 không đo được test cũ → mọi kế hoạch chia lát dừng ở lát 1

- Vị trí: `internal/workflow/slice.go:294-314`
- Bằng chứng: oldTestsGreen đòi log kiểm chứng đo được (failSet trả measured=false khi có '[build failed]'). Với verify `go test ./...`, nếu tệp test đóng băng (hộp đen, thường gọi API mới) tham chiếu ký hiệu mà chỉ lát sau mới thêm, cả gói không biên dịch → kiểm chứng của lát 1 'không đo được test cũ' → lát 1 discarded → blocked (slice.go:492-498). sliceVerdict có nới 'điểm xanh trước chưa đo được' cho cổng mục tiêu nhưng oldTestsGreen chạy trước và không nới. sliceGuideBlock (slice.go:167-170) và sliceBlock không dặn b-req/b-feat rằng lát 1 phải làm cho toàn bộ test mục tiêu biên dịch được (stub mọi ký hiệu); goalPromptBlock (goalfreeze.go) cũng không dặn b-goal tránh phụ thuộc biên dịch. Chính TestSliceRatchet phải né việc này bằng reflection/JSON (setSliceTimeout, stateSlices).
- Tiêu chí: 2

Ít nhất nêu trong sliceGuideBlock/sliceBlock: 'lát 1 phải làm test mục tiêu biên dịch được (stub đủ ký hiệu)'; hoặc khi điểm xanh trước không đo được thì áp cùng luật nới cho oldTestsGreen.

### [01M7PD9T5R587EKT2Z82GJVZAJ.4] risk/medium — Lát 1 bị bỏ: resume chạy lát 2 trên điểm xanh đầu, không có cách chạy lại lát 1

- Vị trí: `internal/workflow/slice.go:479-498`
- Bằng chứng: discardSlice luôn activateNext (dòng 482) kể cả khi n==1 và tác vụ blocked; resume sau khi người xử lý nguyên nhân (vd. test cũ đỏ sẵn — plan.md nêu ca này làm lát 1 bị bỏ) chạy lát 2 không có lát 1. plan.md (Giả định) lý giải 'Lát 1 bị bỏ thì dừng (không chạy lát sau): lát sau thường dựa trên lát 1, chạy tiếp chỉ tốn tiền' — resume lại làm đúng điều đó. docs/workflows.md chỉ cho cách lấy lại cây (git restore --source) hoặc lập phiếu mới.
- Tiêu chí: 3

Với lát 1 bị bỏ, giữ lát 1 ở pending (Backup vẫn ghi) để resume chạy lại lát 1; hoặc có lựa chọn resume rõ ràng (chạy lại lát / bỏ qua).

### [01M7PD9T5R587EKT2Z82GJVZAJ.5] suggestion/low — Driver session không có ghi chú khi gặp tác vụ đã chia lát; lát active treo trong state

- Vị trí: `internal/workflow/session.go`
- Bằng chứng: plan.md mục Bề mặt: 'Driver session bint run next (session.go): … ghi chú khi gặp tác vụ đã chia lát' — session.go không tham chiếu Slices. Tác vụ blocked giữa lát rồi tiếp bằng driver session chạy một b-feat cả khối nhưng State.Slices vẫn có lát active; nếu sau đó quay lại driver process, b-feat vòng sửa sẽ thấy activeSlice() khác nil và đi qua sliceGate.
- Tiêu chí: 1

Ghi note ở session khi State.Slices có lát chưa xong, và/hoặc đóng các lát còn lại khi b-feat chạy không theo lát.

### [01M7PD9T5R587EKT2Z82GJVZAJ.6] suggestion/low — LastGreen trỏ điểm xanh đầu (cây sau b-goal) với nhãn 'test cũ xanh' dù chưa lát nào chốt

- Vị trí: `internal/workflow/green.go:42-51`
- Bằng chứng: sliceStart (slice.go:149-159) đặt State.SliceGreen ngay ở đầu lát 1; lastGreenPoint dùng nó với Command 'lát mỏng (ô đỏ của kiểm tra mục tiêu giảm, test cũ xanh)'. Khi lát 1 bị bỏ, Result.LastGreen/restore-green trình bày cây sau b-goal như điểm đã kiểm 'test cũ xanh' trong khi không có lần kiểm chứng nào ở cây đó.
- Tiêu chí: 4

Phân biệt nhãn điểm xanh đầu (chưa kiểm test cũ) với điểm sau lát chốt.

### [01M7PD9T5R587EKT2Z82GJVZAJ.7] suggestion/low — Lỗi ghi nhận khi lát quá trần nêu sai trần (timeout_minutes thay vì slice_timeout_minutes)

- Vị trí: `internal/workflow/run.go:1444-1445`
- Bằng chứng: endReason = 'quá workflow.timeout_minutes (%d phút)' dùng TimeoutMinutes; với lát, trần thực là min(timeout, slice_timeout) (sliceTimeoutFor). RecordError (run.go:1600) và StepStop ghi lý do này nên nhật ký lỗi nói 'quá 60 phút' khi lát bị cắt ở 30 phút; chỉ State.Slices.Reason (slice.go:431-435) đúng.
- Tiêu chí: 4

Dùng sliceTimeoutFor để dựng endReason khi có lát active.

### [01M7PD9T5R587EKT2Z82GJVZAJ.8] suggestion/low — failSet coi mọi chuỗi 'panic:' trong log là không đo được

- Vị trí: `internal/workflow/slice.go:224-228`
- Bằng chứng: strings.Contains(text, 'panic:') trên toàn log kiểm chứng go test ./...: một test cũ đỏ có in 'panic:' trong t.Log/thông báo (test recover, test parse lỗi) làm cả log 'không đo được' → lát bị bỏ với lý do chung chung thay vì nêu tên test đỏ.
- Tiêu chí: 2

Khớp dòng bắt đầu bằng 'panic: ' ở đầu dòng (không thụt) thay vì chuỗi con bất kỳ.

## Vòng 2 — 2026-11-18T08:24:23Z

Vòng 2: phát hiện high của vòng 1 (hết quota/ngân sách giữa lát bị coi là bỏ) đã sửa đúng (discardSlice stop=true giữ lát active, có TestSliceAgentFailureKeepsSlicePending); test mục tiêu đóng băng TestSliceRatchet khớp đặc tả, không bị nới; còn 2 lỗi medium (ref slice-N-discarded không bao giờ bị xoá; kiểm chứng của cây đã bỏ vẫn là 'kiểm chứng cuối' khi bỏ lát cuối) và 1 rủi ro medium, 1 low. Không có critical/high.

### [01M7PD9T5R587EKT2Z82GJVZAJ.9] bug/medium — deleteTaskRefs không xoá được slice-N-discarded: for-each-ref với tiền tố 'slice-' không khớp

- Vị trí: `internal/workflow/touched.go:117-124`
- Bằng chứng: git for-each-ref chỉ khớp mẫu không có ký tự đại diện khi tên ref khớp trọn vẹn hoặc khớp tới dấu '/'. Tái hiện trên ref thật của repo: `git for-each-ref --format='%(refname)' refs/bint/tasks/01M7MPZSNATRP3756F00WSX89V/base-` không in gì, còn `'.../base-*'` in ra `.../base-tree`. Vì vậy `for-each-ref refs/bint/tasks/<id>/slice-` ở dòng 118 trả rỗng, các ref refs/bint/tasks/<id>/slice-<N>-discarded còn lại vĩnh viễn sau land/done/cancel (slice-green chỉ bị xoá nhờ nằm trong danh sách cố định). Trái với docs/workflows.md ('Các ref lát xoá cùng base khi tác vụ kết thúc') và mục 7 của plan. Chưa có test nào kiểm deleteTaskRefs với ref lát.
- Tiêu chí: 3

Dùng mẫu glob 'refs/bint/tasks/<id>/slice-*' (hoặc liệt kê 'refs/bint/tasks/<id>/' rồi lọc tiền tố trong Go); thêm test tạo slice-2-discarded rồi gọi deleteTaskRefs và kiểm ref đã mất.

### [01M7PD9T5R587EKT2Z82GJVZAJ.10] bug/medium — Bỏ lát cuối rồi sang review: kiểm chứng/cổng mục tiêu của cây đã bỏ vẫn là 'lần cuối', review và vòng sửa dựa vào log cũ

- Vị trí: `internal/workflow/slice.go:480-525`
- Bằng chứng: discardSlice đưa cây về điểm xanh nhưng không đụng tới State.Verifications/GoalChecks; khi đó là lát cuối (sliceDone), afterStep gọi implementDone rồi chuyển in_review. reviewDecision (run.go:2124) gọi redVerification(rs.t) (gate.go:351): lần kiểm chứng cuối là của cây đã bỏ (RunID khác rỗng, Reason rỗng, Passed=false). Kết quả: (1) nếu điểm xanh đã 0 ô đỏ và test cũ xanh (các lát trước đã xong hết, lát cuối bị bỏ vì làm đỏ test cũ), reviewRedVerify vẫn mở vòng sửa (hoặc blockExhausted khi hết vòng) dù cây hiện tại xanh; (2) nếu cổng còn đỏ, verifyRedBlock đưa agent sửa log của cây đã bỏ (vd. 'TestOld' đỏ không còn tồn tại). reverifyIfStale mà plan.md mục 6 dựa vào chỉ chạy trong finalize, không chạy trước reviewDecision. TestSliceRatchet không phủ trường hợp này vì L3 (lát cuối) được chốt.
- Tiêu chí: 3

Sau khi bỏ lát cuối (hoặc bất kỳ lát nào trước khi sang review), chạy lại kiểm chứng + cổng mục tiêu trên cây điểm xanh, hoặc đánh dấu hai lần chạy của lát bị bỏ là không còn hiệu lực (vd. đặt Reason) để redVerification bỏ qua chúng; thêm ca test với lát cuối bị bỏ.

### [01M7PD9T5R587EKT2Z82GJVZAJ.11] risk/medium — Dự án Go: test mục tiêu đóng băng tham chiếu API của nhóm sau thì lát 1 luôn bị bỏ ([build failed] = không đo được), mà đề bài không dặn điều này

- Vị trí: `internal/workflow/slice.go:167-170`
- Bằng chứng: failSet trả measured=false khi log có '[build failed]' (slice.go:227-231); oldTestsGreen (dòng 303-306) khi đó bỏ lát với 'kiểm chứng sau lát không đo được test cũ'. Với `go test ./...`, chỉ cần tệp test đóng băng tham chiếu một kiểu/hàm do nhóm 2 hoặc 3 thêm là cả gói không biên dịch sau lát 1 -> lát 1 bị bỏ -> tác vụ blocked (slice.go:512-521). sliceGuideBlock (167-170) chỉ dặn b-req xếp nhóm đầu là 'bản nhỏ nhất vẫn làm giảm số ô đỏ', sliceBlock không dặn b-feat ở lát 1 phải làm test đóng băng biên dịch được. Chính TestSliceRatchet phải đọc State.Slices qua reflect để tránh phụ thuộc biên dịch, cho thấy ràng buộc này là có thật.
- Tiêu chí: 1

Thêm vào sliceGuideBlock/sliceBlock (lát 1): lát đầu phải làm cho kiểm tra mục tiêu đóng băng biên dịch và chạy được (khai báo đủ kiểu/hàm nó dùng, có thể là bản tối thiểu); hoặc ghi rõ hạn chế này trong docs/workflows.md.

### [01M7PD9T5R587EKT2Z82GJVZAJ.12] suggestion/low — Đề bài chuyển phiên b-req -> b-feat (planHandoffPrompt) không có sliceBlock; frozenGoalBlock vẫn bảo 'làm cho chúng xanh'

- Vị trí: `internal/workflow/plan_handoff.go:73-112`
- Bằng chứng: run.go:1145-1147 thay toàn bộ đề bài bằng planHandoffPrompt khi lớp dừng là StopPlanHandoff; hàm này gọi goalCheckBlock nhưng không gọi sliceBlock. Trong b-do có b-goal thì đường này hiện gần như không xảy ra (b-goal ghi tệp làm dấu vân tay cây khác lúc b-req xong nên không nối tiếp được), nhưng nếu xảy ra thì lát 1 sẽ nhận kế hoạch đầy đủ mà không có 'LẦN NÀY CHỈ LÀM lát 1/N', với trần thời gian đã bị hạ còn slice_timeout_minutes. Ngoài ra frozenGoalBlock (goalfreeze.go:381) vẫn nói 'làm mã sản phẩm sao cho chúng xanh', ngược ý với khối lát nằm ngay sau.
- Tiêu chí: 1

Nối sliceBlock(t) vào planHandoffPrompt (hoặc không cho nối tiếp phiên b-req khi đã có lát active); trong frozenGoalBlock, khi có lát active thì nói rõ chỉ cần giảm ô đỏ của nhóm hiện tại.
