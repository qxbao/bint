package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/task"
)

// Lát mỏng và chốt một chiều (b-do chạy kế hoạch lớn). Benchmark Bint-vs-plain: b-feat một khối 20–47 phút, bị cắt giữa chừng thì không có
// gì để giao, và review không hội tụ là cách dừng duy nhất. Với kế hoạch lớn dưới `bint run` (driver process), b-feat đầu tiên chia việc theo
// nhóm `## Bề mặt bị ảnh hưởng` của plan.md (nhóm có AC-N): mỗi lát là một lượt b-feat riêng có trần thời gian (workflow.slice_timeout_minutes).
// Sau mỗi lát Runner đo cổng mục tiêu đóng băng (goalfreeze.go): lát chỉ được CHỐT khi test cũ vẫn xanh và tập ô đỏ của cổng mục tiêu là tập
// con thực sự của tập ở điểm xanh trước; chốt thì ghim cây làm điểm xanh mới (refs/bint/tasks/<id>/slice-green), không thì GIỮ NGUYÊN cây của lát
// (holdSlice: ghim slice-<N>-discarded làm đường lui, tác vụ blocked) và người chọn `resume --slice keep|discard|more`. Hết ngân sách hoặc agent lỗi
// cũng dừng chờ người như vậy, không tự quay về điểm xanh.
// Flow không đổi: lát là các lần chạy lặp của bước implement; quyết định nằm ở sliceGate (afterStep) và sliceSettleFailure (lát lỗi/quá trần).

// sliceOutcome là kết quả một cổng lát: tiếp tục lát kế, hết lát (đi tiếp review), hoặc dừng (tác vụ đã blocked/failed).
type sliceOutcome int

const (
	sliceDone sliceOutcome = iota
	sliceNext
	sliceStop
)

// activeSlice trả lát đang chạy; nil khi không chia lát hoặc ở driver session (lát chỉ chạy dưới `bint run`).
func (rs *runState) activeSlice() *task.Slice {
	if rs.session {
		return nil
	}
	return rs.t.State.ActiveSlice()
}

// sliceTimeoutFor là trần thời gian agent của bước: workflow.timeout_minutes, và với lát đang chạy thì min với slice_timeout_minutes.
func (r *Runner) sliceTimeoutFor(rs *runState, implement bool) time.Duration {
	d := time.Duration(r.Cfg.Workflow.TimeoutMinutes) * time.Minute
	// Luồng B′: vòng sửa duy nhất sau review (FastFixed) có trần như một lát.
	if implement && (rs.activeSlice() != nil || (task.IsBPrime(rs.t) && rs.t.State.FastFixed)) {
		if sd := time.Duration(r.Cfg.Workflow.SliceTimeoutMinutes) * time.Minute; sd > 0 && sd < d {
			d = sd
		}
	}
	if implement {
		if b, ok := r.budgetLeft(rs); ok && b < d {
			rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("luồng B′: workflow.budget_minutes còn %s trước phần giữ cho review và một vòng sửa; lượt b-feat này bị rút trần còn %s",
				b.Round(time.Minute), b.Round(time.Minute)))
			d = b
		}
		if f, ok := r.fixRoundLeft(rs); ok && f > d {
			// Vòng sửa duy nhất sau review dùng hết phần ngân sách tổng còn lại thay vì trần lát (đợt 8: vòng sửa bị cắt ở 30 phút
			// khi ngân sách 170 phút còn khoảng 35 phút, nên B′ không kết thúc được). Không vượt workflow.timeout_minutes.
			if lim := time.Duration(r.Cfg.Workflow.TimeoutMinutes) * time.Minute; lim > 0 && f > lim {
				f = lim
			}
			rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("luồng B′: vòng sửa sau review dùng phần còn lại của workflow.budget_minutes: %s", f.Round(time.Minute)))
			d = f
		}
	}
	return d
}

// fixRoundLeft (luồng B′, workflow.budget_minutes > 0): thời gian còn của ngân sách tổng cho vòng sửa sau review (FastFixed), tính từ State.BudgetStart.
// ok=false khi không áp dụng (không phải vòng sửa B′, không có ngân sách hay chưa có mốc).
func (r *Runner) fixRoundLeft(rs *runState) (time.Duration, bool) {
	t := rs.t
	budget := time.Duration(r.Cfg.Workflow.BudgetMinutes) * time.Minute
	if budget <= 0 || !task.IsBPrime(t) || !t.State.FastFixed {
		return 0, false
	}
	start, err := time.Parse(time.RFC3339, t.State.BudgetStart)
	if err != nil {
		return 0, false
	}
	return max(budget-r.now().Sub(start), budgetMinTurn), true
}

// timeoutReason là lý do dừng khi agent quá trần: nêu trần thật đã giao (lát, vòng sửa, ngân sách tổng) thay cho workflow.timeout_minutes
// (đợt 8: báo "quá 180 phút" cho vòng sửa thật ra bị cắt ở 30 phút).
func (r *Runner) timeoutReason(sr *stepRun) string {
	cfg := time.Duration(r.Cfg.Workflow.TimeoutMinutes) * time.Minute
	if sr == nil || sr.timeout <= 0 || sr.timeout == cfg {
		return fmt.Sprintf("quá workflow.timeout_minutes (%d phút)", r.Cfg.Workflow.TimeoutMinutes)
	}
	return fmt.Sprintf("quá trần của lượt này (%s; lát/vòng sửa/ngân sách tổng, workflow.timeout_minutes là %d phút)", sr.timeout.Round(time.Second), r.Cfg.Workflow.TimeoutMinutes)
}

// defaultReviewReserve là phần giữ cho review và một vòng sửa khi workflow.review_reserve_minutes không đặt (0).
const defaultReviewReserve = config.DefaultReviewReserveMinutes * time.Minute

// budgetMinTurn là trần nhỏ nhất của một lượt b-feat khi ngân sách tổng gần hết (một lượt ngắn hơn thì gần như không làm được gì).
const budgetMinTurn = 3 * time.Minute

// budgetLeft (luồng B′, workflow.budget_minutes > 0): thời gian còn cho lượt b-feat TRƯỚC review, đã trừ review_reserve_minutes, tính từ lúc b-feat đầu tiên
// của tác vụ bắt đầu (State.BudgetStart, giữ qua resume). Vòng sửa sau review (FastFixed) dùng phần giữ sẵn nên không bị rút. ok=false khi không áp dụng.
// Đợt 6–7: trần ngoài cắt B/B′ trước hoặc giữa review vì không bước nào biết tổng còn bao nhiêu.
func (r *Runner) budgetLeft(rs *runState) (time.Duration, bool) {
	t := rs.t
	budget := time.Duration(r.Cfg.Workflow.BudgetMinutes) * time.Minute
	if budget <= 0 || !task.IsBPrime(t) || t.State.FastFixed {
		return 0, false
	}
	start, err := time.Parse(time.RFC3339, t.State.BudgetStart)
	if err != nil {
		now := r.now()
		if nt, err := r.Tasks.Update(t.State.ID, now, func(t *task.Task) error { t.State.BudgetStart = task.Now(now); return nil }); err == nil {
			rs.t = nt
		}
		start = now
	}
	reserve := time.Duration(r.Cfg.Workflow.ReviewReserveMinutes) * time.Minute
	if reserve <= 0 {
		reserve = defaultReviewReserve // khoá để trống/0 (mẫu cấu hình để chú thích cho binary cũ đọc được): dùng mặc định
	}
	left := budget - reserve - r.now().Sub(start)
	return max(left, budgetMinTurn), true
}

// planStrategy đọc `## Chiến lược thực thi` của plan.md của tác vụ ("có" | "không" | "" khi thiếu/sai).
func planStrategy(t *task.Task) (slice, goal string) {
	data, err := os.ReadFile(t.ArtifactPath("plan"))
	if err != nil {
		return task.StrategyMissing, task.StrategyMissing
	}
	return task.PlanStrategy(string(data))
}

// slicingEnabled: cấu hình cho phép chia lát (slice_timeout_minutes > 0 và workflow.slicing không off), chưa xét plan.md.
func (r *Runner) slicingEnabled() bool {
	return r.Cfg.Workflow.SliceTimeoutMinutes > 0 && r.Cfg.Workflow.Slicing != config.GateOff
}

// wantSlices: tác vụ này được chia lát không. always: như cũ; auto: khi plan.md ghi `Chia lát: có`; off hoặc slice_timeout_minutes 0: không.
func (r *Runner) wantSlices(t *task.Task) bool {
	if !r.slicingEnabled() {
		return false
	}
	if r.Cfg.Workflow.Slicing == config.GateAlways {
		return true
	}
	sl, _ := planStrategy(t)
	return sl == task.StrategyYes
}

// wantGoal: tác vụ này chạy b-goal không. off: không; always: như cũ; auto: không khi plan.md ghi `b-goal: không` (thiếu mục thì chạy như cũ).
func (r *Runner) wantGoal(t *task.Task) bool {
	switch r.Cfg.Workflow.GoalCheck {
	case config.GateOff:
		return false
	case config.GateAlways:
		return true
	}
	_, g := planStrategy(t)
	return g != task.StrategyNo
}

// dropSlicesIfOff: tắt chia lát (slicing off hoặc slice_timeout_minutes 0) khi tác vụ đang chia lát thì các lát chưa chốt thành `unsliced`
// (cây giữ nguyên, lát chốt giữ nguyên): phần còn lại chạy một b-feat thường.
func (r *Runner) dropSlicesIfOff(rs *runState) {
	st := &rs.t.State
	if r.slicingEnabled() || len(st.OpenSlices()) == 0 {
		return
	}
	n := len(st.OpenSlices())
	t, err := r.Tasks.Update(st.ID, r.now(), func(t *task.Task) error {
		for i := range t.State.Slices {
			if s := &t.State.Slices[i]; s.Status == task.SlicePending || s.Status == task.SliceActive {
				s.Status, s.Held = task.SliceUnsliced, false
			}
		}
		t.State.SliceMark = nil
		return nil
	})
	if err != nil {
		rs.res.Notes = append(rs.res.Notes, "không ghi được việc thôi chia lát: "+err.Error())
		return
	}
	rs.t = t
	rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("chia lát đã tắt (workflow.slicing hoặc slice_timeout_minutes): %d lát chưa chốt gộp vào một b-feat thường, cây giữ nguyên", n))
}

// sliceCandidates đọc plan.md và trả các nhóm có AC-N khi tác vụ đủ điều kiện chạy theo lát, trừ điều kiện cần b-goal đã chạy (GoalTest) và
// cỡ kế hoạch (người gọi kiểm). Rỗng = không chia lát.
func (r *Runner) sliceCandidates(rs *runState) []task.PlanSlice {
	t := rs.t
	if rs.session || !r.wantSlices(t) || !r.wantGoal(t) || !rs.planSizeGated() {
		return nil // lát cần kiểm tra mục tiêu đóng băng để đo: b-goal không chạy thì không chia lát
	}
	if c := t.State.Classification; c != nil && c.Fast {
		return nil
	}
	if strings.TrimSpace(t.Ticket.Verify.Command) == "" || (!r.VerifyConfirmed(t) && r.Cfg.Security.RequireApprovalForCommands) {
		return nil // không chạy được cổng mục tiêu nên không đo được lát
	}
	if req, err := os.ReadFile(t.ArtifactPath("requirement")); err != nil || task.GoalCheck(string(req)) == "" {
		return nil // không có kiểm tra mục tiêu: không có số đo để chốt
	}
	plan, err := os.ReadFile(t.ArtifactPath("plan"))
	if err != nil {
		return nil
	}
	ps := task.PlanSlices(string(plan))
	if len(ps) < 2 {
		return nil
	}
	return ps
}

// slicePreview là câu liệt kê các lát Bint sẽ chạy, nối vào lý do chặn của cổng cỡ kế hoạch (rỗng khi không chia lát).
func (r *Runner) slicePreview(rs *runState) string {
	ps := r.sliceCandidates(rs)
	if len(ps) == 0 {
		return ""
	}
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = fmt.Sprintf("lát %d/%d: %s", i+1, len(ps), truncate(p.Text, 100))
	}
	return fmt.Sprintf("Bint sẽ chạy %d lát (mỗi lát một b-feat tối đa %d phút; lát không giảm ô đỏ của kiểm tra mục tiêu hoặc làm đỏ test cũ thì được giữ cây và dừng chờ người quyết định): %s",
		len(ps), r.Cfg.Workflow.SliceTimeoutMinutes, strings.Join(parts, "; "))
}

// initSlices bật lát mỏng ở b-feat đầu tiên của tác vụ (firstFeat) khi đủ điều kiện: b-do gốc dưới bint run, kế hoạch lớn, đã có kiểm tra mục
// tiêu đóng băng, ≥ 2 nhóm bề mặt gắn AC, slice_timeout_minutes > 0, và theo dõi được tệp của tác vụ (để quay về chỉ đụng tệp của nó).
// Danh sách lát ghi một lần: resume giữ nguyên, plan.md sửa sau đó không chia lại.
func (r *Runner) initSlices(rs *runState, firstFeat bool) {
	st := &rs.t.State
	if !firstFeat || len(st.Slices) > 0 || st.GoalTest == nil {
		return
	}
	if !st.TracksTouched() && !(st.Origin != "" && st.Worktree != "") {
		return
	}
	ps := r.sliceCandidates(rs)
	if len(ps) == 0 || len(r.planSizeReasons(rs.t)) == 0 {
		return
	}
	items := make([]task.Slice, len(ps))
	for i, p := range ps {
		items[i] = task.Slice{N: i + 1, Text: p.Text, ACs: p.ACs, Status: task.SlicePending}
	}
	t, err := r.Tasks.Update(st.ID, r.now(), func(t *task.Task) error {
		t.State.Slices, t.State.SliceGreen = items, nil
		activateSlice(&t.State, 0)
		return nil
	})
	if err != nil {
		rs.res.Notes = append(rs.res.Notes, "không ghi được danh sách lát (chạy một b-feat như thường): "+err.Error())
		return
	}
	rs.t = t
	rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("kế hoạch lớn chạy theo %d lát mỏng, mỗi lát một b-feat tối đa %d phút (workflow.slice_timeout_minutes)",
		len(items), r.Cfg.Workflow.SliceTimeoutMinutes))
}

// activateSlice đặt lát idx là lát đang chạy và ghi mốc đếm kiểm chứng/cổng mục tiêu (kết quả của lát là các lần sau mốc).
func activateSlice(st *task.State, idx int) {
	st.Slices[idx].Status = task.SliceActive
	st.SliceMark = &task.SliceMark{VerifyN: len(st.Verifications), GoalN: len(st.GoalChecks)}
}

// sliceStart chạy ở beginStep của lát (sau khi có khoá độc quyền, trước agent): lát đầu ghim điểm xanh đầu = cây ngay sau b-goal, kèm số đo
// của cổng mốc (lần chạy cổng mục tiêu ngay sau b-goal).
func (r *Runner) sliceStart(ctx context.Context, rs *runState) {
	st := &rs.t.State
	if st.SliceGreen != nil {
		return
	}
	snap, err := WorktreeSnapshot(ctx, r.Root)
	if err != nil {
		rs.res.Notes = append(rs.res.Notes, "lát mỏng: không chụp được điểm xanh đầu: "+err.Error())
		return
	}
	if err := pinTree(ctx, r.Root, snap, sliceGreenRef(st.ID), "bint: điểm xanh đầu của lát mỏng, tác vụ "+st.ID); err != nil {
		rs.res.Notes = append(rs.res.Notes, "lát mỏng: không ghim được điểm xanh đầu: "+err.Error())
		return
	}
	g := &task.SliceGreen{Tree: snap, At: r.at()}
	var base *task.Verification
	for i := range st.GoalChecks {
		if v := &st.GoalChecks[i]; v.Reason == goalReasonBaseline || base == nil {
			base = v
		}
	}
	if base != nil {
		g.Red, g.Measured = failSetOf(base)
	}
	if t, err := r.Tasks.Update(st.ID, r.now(), func(t *task.Task) error { t.State.SliceGreen = g; return nil }); err == nil {
		rs.t = t
	}
}

// ---- đề bài ----

// sliceGuideBlock là luật của b-req (b-do gốc): thứ tự nhóm bề mặt là thứ tự lát mà Bint chạy.
const sliceGuideBlock = "\nLát mỏng: Bint chỉ chia lát khi plan.md ghi `Chia lát: có — <lý do>` trong `## Chiến lược thực thi` (mặc định `không`; chỉ chọn `có` khi ≥ 2 nhóm " +
	"bề mặt độc lập, mỗi nhóm đo riêng được bằng kiểm tra mục tiêu, và cần `Kiểm tra mục tiêu độc lập (b-goal): có`). Khi chia lát, kế hoạch lớn được Bint chạy theo từng lát, mỗi nhóm cấp ngoài cùng của `## Bề mặt bị ảnh hưởng` có nêu `AC-N` là MỘT lát " +
	"(một lượt b-feat riêng, có trần thời gian). Hãy xếp các nhóm theo thứ tự chạy: nhóm ĐẦU là bản nhỏ nhất vẫn làm giảm số ô đỏ của " +
	"`## Kiểm tra mục tiêu` VÀ làm cho test đóng băng biên dịch được (nếu test gọi API của nhóm sau thì nhóm đầu thêm stub: `[build failed]` làm lát đầu luôn đỏ); mỗi nhóm sau độc lập đủ để chạy riêng; phần tài liệu/cấu hình gộp vào nhóm cần nó hoặc đặt cuối. Nhóm không nêu AC " +
	"(ghi \"Ngoài phạm vi\") không thành lát. Bint chốt lát khi ô đỏ của kiểm tra mục tiêu giảm và test cũ vẫn xanh, không thì giữ cây của lát và dừng chờ người quyết định.\n\n"

// sliceFirstStubNote: test đóng băng của b-goal thường gọi API mà các nhóm/lát sau mới tạo; nếu lát 1 không làm cho gói biên dịch được thì
// cổng đo ra `[build failed]` (không đo được) và lát 1 luôn đỏ.
const sliceFirstStubNote = "Lát đầu tiên PHẢI làm cho kiểm tra mục tiêu đóng băng BIÊN DỊCH ĐƯỢC: nếu test đó gọi API (hàm, kiểu, cờ) mà các lát sau mới cài đặt, " +
	"hãy thêm bản stub tối thiểu (chữ ký đúng, thân trả giá trị rỗng/lỗi chưa cài) ngay trong lát này. Nếu không, cổng đo ra `[build failed]`, " +
	"không đếm được ô đỏ và lát dừng chờ người quyết định. Ô đỏ còn lại sau stub là việc của các lát sau.\n"

// sliceBlock là đề bài riêng của lát đang chạy (rỗng khi không có lát).
func sliceBlock(t *task.Task) string {
	cur := t.State.ActiveSlice()
	if cur == nil {
		return ""
	}
	n := len(t.State.Slices)
	var b strings.Builder
	fmt.Fprintf(&b, "\nLát mỏng: Bint chạy kế hoạch này theo %d lát, mỗi lát một lượt b-feat riêng có trần thời gian. LẦN NÀY CHỈ LÀM lát %d/%d, nguyên văn nhóm trong plan.md:\n%s\n",
		n, cur.N, n, cur.Text)
	if cur.Reason != "" {
		fmt.Fprintf(&b, "Lần chạy trước của lát này dừng giữa chừng (%s); cây của lát được GIỮ NGUYÊN (phần dở ghim ở %s): làm tiếp từ cây hiện tại, đừng làm lại từ đầu.\n", cur.Reason, cur.Backup)
	}
	b.WriteString("Giữ trong phạm vi lát: chỉ sửa tệp thuộc nhóm này, không làm trước phần của lát sau. Chỉ chạy test của phần đã sửa, " +
		"không chạy toàn bộ bộ test hay lệnh kiểm chứng đầy đủ: Bint chạy kiểm chứng sau lát và chạy đầy đủ một lần ở cổng done.\n")
	if t.State.GoalTest != nil && cur.N == t.State.Slices[0].N {
		b.WriteString(sliceFirstStubNote)
	}
	if g := t.State.SliceGreen; g != nil && len(g.OldRed) > 0 {
		fmt.Fprintf(&b, "Test cũ đang ĐỎ mà người đã cho giữ ở lát trước: %s. Đọc log kiểm chứng gần nhất: test nào đỏ do tác vụ gây ra thì sửa TRƯỚC phần của lát này; "+
			"test đỏ sẵn từ trước tác vụ thì để nguyên và ghi rõ trong báo cáo. Bint không giữ lát lại vì đúng các test này, chỉ vì test cũ MỚI đỏ.\n", strings.Join(g.OldRed, ", "))
	}
	var done, later, dropped []string
	for _, s := range t.State.Slices {
		switch {
		case s.N == cur.N, s.Status == task.SliceUnsliced:
		case s.Status == task.SliceAccepted:
			done = append(done, fmt.Sprintf("lát %d/%d: %s", s.N, n, truncate(s.Text, 100)))
		case s.Status == task.SliceDiscarded:
			dropped = append(dropped, fmt.Sprintf("lát %d/%d: %s (%s)", s.N, n, truncate(s.Text, 100), s.Reason))
		default:
			later = append(later, fmt.Sprintf("lát %d/%d: %s", s.N, n, truncate(s.Text, 100)))
		}
	}
	for _, g := range []struct {
		head  string
		items []string
	}{
		{"Đã chốt, đừng làm lại", done},
		{"Đã bỏ (cây đã quay về điểm xanh theo lựa chọn của người, làm lại nếu cần cho lát này, tránh lỗi cũ)", dropped},
		{"Lát sau, đừng làm trước", later},
	} {
		if len(g.items) > 0 {
			fmt.Fprintf(&b, "%s: %s\n", g.head, strings.Join(g.items, " | "))
		}
	}
	b.WriteString("Sau lát này Bint đo cổng mục tiêu: lát chỉ được chốt khi số ô đỏ giảm (không ô xanh nào chuyển đỏ) và test cũ vẫn xanh; không thì Bint giữ cây của lát và dừng chờ người quyết định.\n")
	return b.String()
}

// ---- đo ----

var failLineRe = regexp.MustCompile(`(?m)^[ \t]*--- FAIL: (\S+)`)

// failSet là tập ô đỏ của một log kiểm tra kiểu `go test`: tên các dòng `--- FAIL: <tên>` là lá (không có dòng FAIL nào khác bắt đầu bằng
// `<tên>/`), sắp xếp. measured=false (không đo được) khi lệnh quá giờ, log có `[build failed]`/`[setup failed]`/`panic:`, hoặc mã thoát khác
// 0 mà không có dòng `--- FAIL:` nào. Mã thoát 0 là tập rỗng đo được.
func failSet(text string, exit int, timedOut bool) (names []string, measured bool) {
	if timedOut {
		return nil, false
	}
	if exit == 0 {
		return []string{}, true
	}
	for _, bad := range []string{"[build failed]", "[setup failed]", "panic:"} {
		if strings.Contains(text, bad) {
			return nil, false
		}
	}
	var all []string
	for _, m := range failLineRe.FindAllStringSubmatch(text, -1) {
		if !slices.Contains(all, m[1]) {
			all = append(all, m[1])
		}
	}
	for _, n := range all {
		leaf := true
		for _, o := range all {
			if strings.HasPrefix(o, n+"/") {
				leaf = false
				break
			}
		}
		if leaf {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil, false
	}
	sort.Strings(names)
	return names, true
}

// failSetOf đọc log của một lần kiểm chứng/cổng mục tiêu rồi đo (failSet); không đọc được log mà mã thoát khác 0 là không đo được.
func failSetOf(v *task.Verification) ([]string, bool) {
	if v == nil {
		return nil, false
	}
	text := ""
	if v.Log != "" && v.ExitCode != 0 && !v.TimedOut {
		data, err := os.ReadFile(v.Log)
		if err != nil {
			return nil, false
		}
		text = string(data)
	}
	return failSet(text, v.ExitCode, v.TimedOut)
}

// topName là tên test cấp đầu của một ô (TestGoal/c1 → TestGoal).
func topName(cell string) string {
	n, _, _ := strings.Cut(cell, "/")
	return n
}

// goalTestNames là tên test cấp đầu thuộc kiểm tra mục tiêu: hàm Test* của tệp đóng băng *_test.go và tên cấp đầu của các ô đỏ trong log cổng mục tiêu.
func goalTestNames(root string, t *task.Task, goalRed []string) map[string]bool {
	out := map[string]bool{}
	for _, c := range goalRed {
		out[topName(c)] = true
	}
	for _, rel := range t.State.GoalTest.Paths() {
		if strings.HasSuffix(rel, "_test.go") {
			if ns, ok := goTestFuncs(filepath.Join(root, filepath.FromSlash(rel))); ok {
				for _, n := range ns {
					out[n] = true
				}
			}
		}
	}
	return out
}

// oldTestsGreen: test cũ (ngoài kiểm tra mục tiêu) vẫn xanh sau lát. v là lần kiểm chứng của lát (nil: không chạy được, coi là xanh). Qua, hoặc
// scope none (không test nào bị ảnh hưởng) là xanh; ngược lại log phải đo được và mọi ô đỏ thuộc kiểm tra mục tiêu (goal).
// accepted là test cũ đỏ người đã chấp nhận ở lát trước (SliceGreen.OldRed): chúng không giữ lát lại, chỉ test cũ MỚI đỏ mới giữ (đợt 7: 4 lần dừng
// liên tiếp vì cùng các test đỏ sẵn ở mốc, người chọn giữ cả 4 lần).
func oldTestsGreen(v *task.Verification, goal map[string]bool, accepted []string) (ok bool, why string) {
	if v == nil || v.Passed || v.Scope == "none" {
		return true, ""
	}
	red, measured := failSetOf(v)
	if !measured {
		return false, "kiểm chứng sau lát không đo được test cũ (biên dịch hỏng, panic, quá giờ hoặc không có dòng --- FAIL: trong " + cmpStr(v.Log, "log") + ")"
	}
	var bad []string
	for _, c := range oldRedOf(red, goal) {
		if !slices.Contains(accepted, c) {
			bad = append(bad, c)
		}
	}
	if len(bad) > 0 {
		return false, "test cũ đỏ: " + strings.Join(bad, ", ")
	}
	return true, ""
}

// oldRedOf lọc tập ô đỏ của lần kiểm chứng: bỏ ô thuộc kiểm tra mục tiêu (goal), còn lại là test cũ đỏ.
func oldRedOf(red []string, goal map[string]bool) []string {
	var out []string
	for _, c := range red {
		if !goal[topName(c)] {
			out = append(out, c)
		}
	}
	return out
}

// sliceVerdict quyết định chốt (ok) hay giữ lát chờ người theo số đo: test cũ xanh VÀ ô đỏ của cổng mục tiêu giảm so với điểm xanh trước. Điểm xanh trước
// không đo được thì lát chỉ cần làm cho đo được; điểm xanh đã 0 ô đỏ thì lát phải giữ 0. why là số đo làm lý do (cả khi chốt).
func sliceVerdict(prev *task.SliceGreen, red []string, measured, oldOK bool, oldWhy string) (ok bool, why string) {
	switch {
	case !oldOK:
		return false, oldWhy
	case !measured:
		return false, "cổng mục tiêu không đo được sau lát (biên dịch hỏng, panic, quá giờ hoặc không có dòng --- FAIL:)"
	case !prev.Measured:
		return true, fmt.Sprintf("điểm xanh trước chưa đo được, nay đo được: %d ô đỏ", len(red))
	case len(prev.Red) == 0:
		if len(red) == 0 {
			return true, "giữ 0 ô đỏ"
		}
		return false, fmt.Sprintf("điểm xanh có 0 ô đỏ nhưng lát làm đỏ %d ô: %s", len(red), strings.Join(red, ", "))
	}
	var worse []string
	for _, c := range red {
		if !slices.Contains(prev.Red, c) {
			worse = append(worse, c)
		}
	}
	switch {
	case len(worse) > 0:
		return false, "ô xanh chuyển đỏ: " + strings.Join(worse, ", ")
	case len(red) >= len(prev.Red):
		return false, fmt.Sprintf("ô đỏ %d→%d không giảm", len(prev.Red), len(red))
	}
	return true, fmt.Sprintf("ô đỏ %d→%d", len(prev.Red), len(red))
}

// ---- chốt / giữ chờ người ----

// sliceGate chạy sau bước implement của lát (đã qua kiểm phạm vi, kiểm chứng và cổng mục tiêu): đo, rồi chốt lát (đạt) hoặc giữ cây chờ người (không đạt, holdSlice).
func (r *Runner) sliceGate(ctx context.Context, rs *runState) sliceOutcome {
	st := &rs.t.State
	cur := st.ActiveSlice()
	if cur == nil {
		return sliceDone
	}
	mark := task.SliceMark{}
	if st.SliceMark != nil {
		mark = *st.SliceMark
	}
	var ver, goal *task.Verification
	if n := len(st.Verifications); n > mark.VerifyN {
		ver = &st.Verifications[n-1]
	}
	if n := len(st.GoalChecks); n > mark.GoalN {
		goal = &st.GoalChecks[n-1]
	}
	if st.SliceGreen == nil || st.SliceGreen.Tree == "" {
		r.block(rs, fmt.Sprintf("lát %d/%d: không có điểm xanh để so và quay về (không ghim được cây ngay sau b-goal); xem Notes, rồi %s chạy lại lát", cur.N, len(st.Slices), r.resumeCmd(rs)))
		return sliceStop
	}
	red, measured := failSetOf(goal)
	oldOK, oldWhy := oldTestsGreen(ver, goalTestNames(r.Root, rs.t, red), st.SliceGreen.OldRed)
	ok, why := sliceVerdict(st.SliceGreen, red, measured, oldOK, oldWhy)
	if !ok {
		var rd []string
		if measured {
			rd = red
		}
		return r.holdSlice(ctx, rs, why, rd)
	}
	snap, err := WorktreeSnapshot(ctx, r.Root)
	if err == nil {
		err = pinTree(ctx, r.Root, snap, sliceGreenRef(st.ID), fmt.Sprintf("bint: điểm xanh sau lát %d của tác vụ %s", cur.N, st.ID))
	}
	if err != nil {
		// Không ghim được thì không có điểm quay về: không chốt (không giữ điểm xanh cũ như thể lát chưa làm).
		r.block(rs, fmt.Sprintf("lát %d/%d đạt số đo (%s) nhưng không ghim được điểm xanh: %v; %s", cur.N, len(st.Slices), why, err, r.resumeCmd(rs)))
		return sliceStop
	}
	idx, runID := cur.N-1, ""
	if n := len(rs.res.Steps); n > 0 {
		runID = rs.res.Steps[n-1].RunID
	}
	// Test cũ đỏ đã chấp nhận mà vẫn còn đỏ thì mang sang điểm xanh mới (đã xanh lại thì thôi).
	var carry []string
	if ver != nil && !ver.Passed {
		if vr, ok := failSetOf(ver); ok {
			for _, c := range oldRedOf(vr, goalTestNames(r.Root, rs.t, red)) {
				if slices.Contains(st.SliceGreen.OldRed, c) {
					carry = append(carry, c)
				}
			}
		}
	}
	next := false
	t, uerr := r.Tasks.Update(st.ID, r.now(), func(t *task.Task) error {
		s := &t.State.Slices[idx]
		s.Status, s.Reason, s.Red, s.Tree, s.RunID = task.SliceAccepted, why, append([]string{}, red...), snap, runID
		t.State.SliceGreen = &task.SliceGreen{Tree: snap, At: r.at(), Measured: true, Red: append([]string{}, red...), OldRed: carry}
		next = activateNext(&t.State)
		return nil
	})
	if uerr != nil {
		r.fail(rs, "slice", "ghi kết quả lát: "+uerr.Error())
		return sliceStop
	}
	rs.t = t
	r.logf("  lát %d/%d chốt (%s)", cur.N, len(t.State.Slices), why)
	if next {
		return sliceNext
	}
	return sliceDone
}

// activateNext kích hoạt lát chờ đầu tiên; false khi không còn lát nào chờ.
func activateNext(st *task.State) bool {
	for i := range st.Slices {
		if st.Slices[i].Status == task.SlicePending {
			activateSlice(st, i)
			return true
		}
	}
	return false
}

// sliceSettleFailure xử lý lát có agent lỗi hoặc quá trần (endStep đặt rs.sliceFail): cả hai giữ cây của lát và dừng blocked hỏi người
// (holdSlice), không tự quay về điểm xanh.
func (r *Runner) sliceSettleFailure(ctx context.Context, rs *runState) sliceOutcome {
	reason, timedOut := rs.sliceFail, rs.sliceTimedOut
	rs.sliceFail, rs.sliceTimedOut = "", false
	if timedOut {
		mins := r.Cfg.Workflow.TimeoutMinutes
		if sm := r.Cfg.Workflow.SliceTimeoutMinutes; sm > 0 && sm < mins {
			mins = sm
		}
		return r.holdSlice(ctx, rs, fmt.Sprintf("quá trần thời gian agent của lát (%d phút)", mins), nil)
	}
	return r.holdSlice(ctx, rs, "agent lỗi: "+reason, nil)
}

// holdSlice giữ lát đang chạy chờ người quyết (quá trần, không đạt số đo, vượt ngân sách diff/hiệu năng, agent lỗi): ghim cây hiện tại ở
// slice-<N>-discarded (đường lui), KHÔNG đưa tệp về điểm xanh, đánh dấu lát Held kèm lý do và dừng tác vụ blocked với ba lựa chọn
// `resume --slice keep|discard|more`. red là tập ô đỏ đo được của lát (nil nếu không đo được).
func (r *Runner) holdSlice(ctx context.Context, rs *runState, why string, red []string) sliceOutcome {
	st := &rs.t.State
	cur := st.ActiveSlice()
	if cur == nil {
		return sliceDone
	}
	n, total, id := cur.N, len(st.Slices), st.ID
	lock, ok := r.lockStep(ctx, rs, "slice", LockExclusive)
	if !ok {
		return sliceStop
	}
	defer lock.Release()
	backup := sliceDiscardRef(id, n)
	snap, err := WorktreeSnapshot(ctx, r.Root)
	if err == nil {
		err = pinTree(ctx, r.Root, snap, backup, fmt.Sprintf("bint: cây của lát %d đang giữ chờ người, tác vụ %s", n, id))
	}
	if err != nil {
		// Cây vẫn được giữ nguyên (không ghi đè gì); chỉ không có ref đường lui.
		backup = ""
		rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("lát %d: không ghim được cây làm đường lui (cây vẫn giữ nguyên): %v", n, err))
	}
	t, uerr := r.Tasks.Update(id, r.now(), func(t *task.Task) error {
		s := &t.State.Slices[n-1]
		s.Held, s.Reason, s.Backup = true, why, backup
		if len(red) > 0 {
			s.Red = append([]string{}, red...)
		}
		// Phiên của lát dở mang ngữ cảnh sai: lần chạy sau chạy đề bài đầy đủ, không nối tiếp.
		t.State.LastStep, t.State.LastImplement, t.State.AwaitingCheck = nil, nil, false
		return nil
	})
	if uerr != nil {
		r.fail(rs, "slice", "ghi kết quả lát: "+uerr.Error())
		return sliceStop
	}
	rs.t = t
	r.logf("  lát %d/%d dừng chờ người (%s); cây của lát giữ nguyên, ghim ở %s", n, total, why, cmpStr(backup, "(không ghim được)"))
	resume := r.resumeCmd(rs)
	r.block(rs, fmt.Sprintf("lát %d/%d dừng chờ bạn (%s); cây của lát được GIỮ NGUYÊN, Bint không tự quay về điểm xanh%s. Chọn: "+
		"`%s --slice keep` (chốt cây hiện tại làm điểm xanh rồi sang lát kế) | `%s --slice discard` (đưa tệp của tác vụ về điểm xanh rồi sang lát kế) | "+
		"`%s --slice more` (chạy lại lát từ cây hiện tại, thêm một trần thời gian); %s",
		n, total, why, ifSet(backup, ", ghim ở ", ""), resume, resume, resume, sliceSummary(rs.t)))
	return sliceStop
}

// ifSet là prefix+s+suffix khi s không rỗng, ngược lại rỗng.
func ifSet(s, prefix, suffix string) string {
	if s == "" {
		return ""
	}
	return prefix + s + suffix
}

// Lựa chọn của người với lát đang giữ (resume --slice).
const (
	SliceKeep    = "keep"
	SliceDiscard = "discard"
	SliceMore    = "more"
)

// SliceChoices là các giá trị hợp lệ của `bint run resume --slice`.
var SliceChoices = []string{SliceKeep, SliceDiscard, SliceMore}

// checkSliceFlag kiểm --slice trên phiếu t trước khi nhận quyền điều phối: chỉ khi tác vụ đang chờ người ở một lát giữ.
func checkSliceFlag(t *task.Task, choice string) error {
	if choice == "" {
		return nil
	}
	if !slices.Contains(SliceChoices, choice) {
		return fmt.Errorf("--slice %q không hợp lệ: chọn %s", choice, strings.Join(SliceChoices, " | "))
	}
	if !task.Paused(t.State.Status) || t.State.HeldSlice() == nil {
		return fmt.Errorf("--slice chỉ dùng khi tác vụ %s đang chờ người ở một lát bị giữ (quá trần, không đạt, vượt ngân sách hoặc agent lỗi); đang %s (bint task show %s)",
			t.State.ID, t.State.Status, t.State.ID)
	}
	return nil
}

// heldMeasure đo cổng mục tiêu ở lần chạy gần nhất của lát đang giữ (sau mốc đầu lát): measured=false khi chưa có lần chạy nào hoặc không đo được.
func heldMeasure(st *task.State) (red []string, measured bool) {
	mark := task.SliceMark{}
	if st.SliceMark != nil {
		mark = *st.SliceMark
	}
	if n := len(st.GoalChecks); n > mark.GoalN {
		return failSetOf(&st.GoalChecks[n-1])
	}
	return nil, false
}

// applySliceChoice áp lựa chọn của người với lát đang giữ khi resume: keep chốt cây hiện tại, discard đưa tệp của tác vụ về điểm xanh,
// more (mặc định khi không nêu --slice, không mất công) chạy lại lát từ cây hiện tại. Trả false khi workflow phải dừng (đã đặt trạng thái).
// Hết lát sau keep/discard thì đặt AwaitingCheck để bước kiểm tra sau implement (kiểm phạm vi, kiểm chứng, cổng mục tiêu) chạy trên cây cuối.
func (r *Runner) applySliceChoice(ctx context.Context, rs *runState, choice string) bool {
	st := &rs.t.State
	cur := st.HeldSlice()
	if cur == nil {
		return true
	}
	n, total, id, idx := cur.N, len(st.Slices), st.ID, cur.N-1
	if choice == "" {
		choice = SliceMore
		if r.slicingEnabled() {
			rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("lát %d đang giữ chờ người mà không có --slice: chạy lại lát từ cây hiện tại (--slice more)", n))
		} else {
			// Chia lát đã tắt: dropSlicesIfOff ngay sau đây gộp lát vào một b-feat thường, không có "chạy lại lát" nào.
			rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("lát %d đang giữ chờ người, người không chọn gì và chia lát đã tắt: lát gộp vào một b-feat thường trên cây hiện tại", n))
		}
	}
	if choice == SliceMore {
		t, err := r.Tasks.Update(id, r.now(), func(t *task.Task) error {
			t.State.Slices[idx].Held = false
			activateSlice(&t.State, idx) // mốc đếm mới: kết quả của lần chạy lại là các lần sau mốc
			return nil
		})
		if err != nil {
			r.fail(rs, "slice", "ghi lựa chọn lát: "+err.Error())
			return false
		}
		rs.t = t
		return true
	}
	lock, ok := r.lockStep(ctx, rs, "slice", LockExclusive)
	if !ok {
		return false
	}
	defer lock.Release()
	var snap string
	switch choice {
	case SliceKeep:
		var err error
		if snap, err = WorktreeSnapshot(ctx, r.Root); err == nil {
			err = pinTree(ctx, r.Root, snap, sliceGreenRef(id), fmt.Sprintf("bint: điểm xanh sau lát %d (người giữ), tác vụ %s", n, id))
		}
		if err != nil {
			r.block(rs, fmt.Sprintf("lát %d/%d: không ghim được cây để chốt (--slice keep): %v; xử lý rồi %s --slice keep", n, total, err, r.resumeCmd(rs)))
			return false
		}
	case SliceDiscard:
		if st.SliceGreen == nil || st.SliceGreen.Tree == "" {
			r.block(rs, fmt.Sprintf("lát %d/%d: không có điểm xanh để quay về (--slice discard); chọn --slice keep hoặc more", n, total))
			return false
		}
		res, rerr := restoreTree(ctx, r.Root, rs.t, st.SliceGreen.Tree, true, "")
		if res != nil && len(res.Skipped) > 0 {
			rs.res.Notes = append(rs.res.Notes, fmt.Sprintf("lát %d bỏ theo lựa chọn của người: tệp khác điểm xanh nhưng không thuộc tác vụ nên giữ nguyên: %s", n, strings.Join(res.Skipped, ", ")))
		}
		if rerr != nil {
			r.block(rs, fmt.Sprintf("lát %d/%d: quay về điểm xanh dở dang (--slice discard): %v; cây của lát giữ ở %s", n, total, rerr, cur.Backup))
			return false
		}
	}
	red, measured := heldMeasure(st)
	var prevGreen *task.SliceGreen
	if st.SliceGreen != nil {
		pg := *st.SliceGreen
		prevGreen = &pg
	}
	// keep: test cũ đang đỏ trên cây được giữ thành tập đã chấp nhận, mang sang lát sau (gộp với tập đã chấp nhận trước đó).
	var oldRed []string
	if choice == SliceKeep {
		if prevGreen != nil {
			oldRed = slices.Clone(prevGreen.OldRed)
		}
		mark := task.SliceMark{}
		if st.SliceMark != nil {
			mark = *st.SliceMark
		}
		if n := len(st.Verifications); n > mark.VerifyN {
			if vr, ok := failSetOf(&st.Verifications[n-1]); ok && !st.Verifications[n-1].Passed {
				oldRed = unionSorted(oldRed, oldRedOf(vr, goalTestNames(r.Root, rs.t, red)))
			}
		}
	}
	next := false
	t, err := r.Tasks.Update(id, r.now(), func(t *task.Task) error {
		s := &t.State.Slices[idx]
		old := s.Reason
		s.Held = false
		if choice == SliceKeep {
			s.Status, s.Reason, s.Tree = task.SliceAccepted, "người giữ cây của lát ("+old+")", snap
			s.Red = append([]string{}, red...)
			gRed, gMeasured := red, measured
			if !measured && prevGreen != nil {
				// Lát chưa đo (quá trần/agent lỗi): giữ số đo của điểm xanh trước để sliceVerdict không bị chốt thành "chưa đo được".
				gRed, gMeasured = prevGreen.Red, prevGreen.Measured
			}
			t.State.SliceGreen = &task.SliceGreen{Tree: snap, At: r.at(), Measured: gMeasured, Red: append([]string{}, gRed...), OldRed: oldRed}
		} else {
			s.Status, s.Reason = task.SliceDiscarded, "người bỏ lát ("+old+")"
			voidRedChecks(&t.State) // cây đã quay về điểm xanh: số đo đỏ của lát bỏ không còn nói về cây hiện tại
		}
		t.State.LastStep, t.State.LastImplement = nil, nil
		next = activateNext(&t.State)
		t.State.AwaitingCheck = !next // hết lát: chạy cổng sau bước implement trên cây cuối
		return nil
	})
	if err != nil {
		r.fail(rs, "slice", "ghi lựa chọn lát: "+err.Error())
		return false
	}
	rs.t = t
	r.logf("  lát %d/%d: người chọn %s", n, total, choice)
	return true
}

// voidRedChecks đánh dấu Voided lần đo cuối đang đỏ của kiểm chứng, cổng mục tiêu, ngân sách và perf (thuộc cây lát vừa bỏ).
func voidRedChecks(st *task.State) {
	for _, list := range []*[]task.Verification{&st.Verifications, &st.GoalChecks, &st.BudgetChecks, &st.PerfChecks} {
		if n := len(*list); n > 0 && !(*list)[n-1].Passed {
			(*list)[n-1].Voided = true
		}
	}
}

// ---- kết luận ----

// sliceSummary là tóm tắt các lát: đã chốt, đã bỏ (lý do, ref lấy lại), chưa làm. Rỗng khi không chia lát.
func sliceSummary(t *task.Task) string {
	n := len(t.State.Slices)
	if n == 0 {
		return ""
	}
	var acc, disc, todo []string
	for _, s := range t.State.Slices {
		switch s.Status {
		case task.SliceUnsliced:
			// thôi chia lát giữa chừng: gộp vào b-feat thường, không còn là lát chưa làm
		case task.SliceAccepted:
			acc = append(acc, fmt.Sprintf("lát %d/%d (%s)", s.N, n, truncate(s.Text, 60)))
		case task.SliceDiscarded:
			disc = append(disc, fmt.Sprintf("lát %d/%d (%s): %s, cây của lát ở %s", s.N, n, truncate(s.Text, 60), s.Reason, s.Backup))
		default:
			item := fmt.Sprintf("lát %d/%d (%s)", s.N, n, truncate(s.Text, 60))
			if s.Status == task.SliceActive && s.Reason != "" {
				item += fmt.Sprintf(" — lần chạy trước dừng giữa chừng: %s, phần dở ở %s", s.Reason, s.Backup)
			}
			todo = append(todo, item)
		}
	}
	parts := []string{fmt.Sprintf("lát mỏng: đã chốt %d/%d", len(acc), n)}
	if len(acc) > 0 {
		parts[0] += " — " + strings.Join(acc, ", ")
	}
	if len(disc) > 0 {
		parts = append(parts, "đã bỏ: "+strings.Join(disc, "; "))
	}
	if len(todo) > 0 {
		parts = append(parts, "chưa làm: "+strings.Join(todo, ", "))
	}
	return strings.Join(parts, "; ")
}

// sliceTail là đoạn nối vào lý do dừng của tác vụ đã chia lát (hết ngân sách...): "; " + tóm tắt, rỗng khi không chia lát hoặc ở driver session.
func (r *Runner) sliceTail(rs *runState) string {
	if rs.session {
		return ""
	}
	if s := sliceSummary(rs.t); s != "" {
		return "; " + s
	}
	return ""
}
