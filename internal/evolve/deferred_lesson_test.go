package evolve

import (
	"context"
	"strings"
	"testing"
)

// lessonReplyTwoKinds là đầu ra của reflector gồm một ứng viên chung và một ứng viên riêng; cả
// hai đều qua kiểm tra xác định nên cho ra hai đề xuất.
const lessonReplyTwoKinds = `{"lessons":[` +
	`{"scope":"global","trigger":"Khi sửa mã có test trong nhiều vùng","mistake":"Agent kết thúc mà không chạy test",` +
	`"instead":"Chạy go test của vùng đã sửa trước khi báo xong","evidence":["ea-v1","ea-v3"]},` +
	`{"scope":"domain:internal/core/**","trigger":"Khi sửa internal/core","mistake":"Bỏ qua ngân sách truy vấn",` +
	`"instead":"Kiểm tra ngân sách trước khi trả kết quả","evidence":["ea-v1","ea-v2"]}` +
	`]}`

// TestDeferredFailureLessonProcess (E6, không tốn token): yêu cầu failure_lesson đi đường deferred
// (ProcessRequests, như `bint evolve process`) phải truyền tác vụ đánh giá và repository cho bước
// kiểm rò rỉ. Reflector được gọi đúng một lần với đầu vào không có tập test; yêu cầu thành công kèm
// đề xuất, và hai ứng viên cho hai đề xuất (không bị đánh dấu skipped).
func TestDeferredFailureLessonProcess(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	cases, scan, h := seedLessonFailures(t, f)
	rt := f.k.Paths.Runtime
	added, err := Enqueue(rt, scan, now)
	if err != nil || len(added) != 1 || added[0].Kind != KindFailureLesson {
		t.Fatalf("enqueue: %v %+v", err, added)
	}

	reflector := &fakeReflector{reply: lessonReplyTwoKinds}
	rep, err := ProcessRequests(ctx, ProcessInput{K: f.k, DB: f.db, Scan: scan, History: h, Reflector: reflector,
		Cases: cases, RepoDir: f.root, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if reflector.reflected != 1 {
		t.Fatalf("reflector được gọi %d lần, muốn 1", reflector.reflected)
	}
	if prompt := reflector.prompts[0]; strings.Contains(prompt, testSetMarker) || strings.Contains(prompt, "ea-t1") {
		t.Fatalf("đầu vào reflector (đường deferred) lọt dữ liệu tập test:\n%s", prompt)
	}

	if len(rep.Processed) != 1 {
		t.Fatalf("muốn 1 yêu cầu đã xử lý: %+v", rep)
	}
	r := rep.Processed[0]
	if r.Status != ReqDone || r.ProposalID == "" {
		t.Fatalf("yêu cầu failure_lesson phải xong kèm đề xuất, được %s (%q)", r.Status, r.Note)
	}
	ids := []string{r.ProposalID}
	if extra, ok := strings.CutPrefix(r.Note, "đề xuất thêm: "); ok {
		ids = append(ids, strings.Split(extra, ", ")...)
	}
	if len(ids) != 2 {
		t.Fatalf("hai ứng viên phải cho hai đề xuất, được %d (ghi chú %q)", len(ids), r.Note)
	}
	for _, id := range ids {
		p, err := GetProposal(ctx, f.db, id)
		if err != nil || len(p.Changes) != 1 || p.Changes[0].Op != ActionCreate || p.Changes[0].Draft.Type != "lesson" {
			t.Fatalf("đề xuất %s phải là create lesson: %v %+v", id, err, p)
		}
	}
	if pending, _ := ListRequests(rt, ReqPending); len(pending) != 0 {
		t.Fatalf("còn yêu cầu chờ: %+v", pending)
	}
}

// TestDeferredFailureLessonNeedsCasesBeforeReflector (E6, không tốn token): thiếu tác vụ đánh giá
// (đường deferred từng không truyền Cases) thì yêu cầu failure_lesson bị bỏ TRƯỚC khi gọi
// reflector, kèm lý do; không được có lời gọi mô hình nào vô ích.
func TestDeferredFailureLessonNeedsCasesBeforeReflector(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	_, scan, h := seedLessonFailures(t, f)
	if _, err := Enqueue(f.k.Paths.Runtime, scan, now); err != nil {
		t.Fatal(err)
	}

	reflector := &fakeReflector{reply: lessonReplyTwoKinds}
	rep, err := ProcessRequests(ctx, ProcessInput{K: f.k, DB: f.db, Scan: scan, History: h, Reflector: reflector, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if reflector.reflected != 0 {
		t.Fatalf("reflector được gọi %d lần dù thiếu tác vụ đánh giá (tốn tiền vô ích)", reflector.reflected)
	}
	if len(rep.Processed) != 1 || rep.Processed[0].Status != ReqSkipped ||
		!strings.Contains(rep.Processed[0].Note, "không tìm thấy tác vụ") {
		t.Fatalf("yêu cầu phải bị bỏ kèm lý do thiếu tác vụ: %+v", rep.Processed)
	}
}
