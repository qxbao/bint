package overview

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"reflect"
	"testing"

	"bint/internal/app"
	"bint/internal/i18n"
	"bint/internal/tui/theme"
	"bint/internal/tui/ui"
)

func TestRunParts(t *testing.T) {
	env := ui.Env{Th: theme.New(theme.Mono), Cat: i18n.MustLoad("vi")}
	for _, c := range []struct {
		name string
		runs map[string]int
		want []string
	}{
		{"rỗng", nil, nil},
		{"số 0 và âm bị bỏ", map[string]int{"completed": 0, "failed": -1, "running": 2}, []string{"2 đang chạy"}},
		{"thứ tự cố định", map[string]int{"running": 1, "interrupted": 2, "cancelled": 3, "failed": 4, "completed": 5},
			[]string{"5 xong", "4 lỗi", "3 đã huỷ", "2 bị ngắt", "1 đang chạy"}},
		{"trạng thái lạ hiện nguyên dạng, xếp sau theo tên", map[string]int{"zeta": 1, "queued": 7, "completed": 2, "alpha": 0},
			[]string{"2 xong", "7 queued", "1 zeta"}},
	} {
		if got := runParts(env, c.runs); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: runParts = %q, muốn %q", c.name, got, c.want)
		}
	}
}

func TestRunsLineStackedBarAndLegend(t *testing.T) {
	runs := map[string]int{"completed": 33, "failed": 3, "interrupted": 1, "running": 5}
	for _, mode := range []theme.Mode{theme.Dark, theme.Mono} {
		env := ui.Env{Th: theme.New(mode), Cat: i18n.MustLoad("vi")}
		for _, vw := range []int{12, 20, 40, 80} {
			line := runsLine(env, 42, runs, vw)
			plain := ansi.Strip(line)
			if !strings.HasPrefix(plain, "42") {
				t.Fatalf("vw=%d: phải mở đầu bằng tổng: %q", vw, plain)
			}
			if vw >= 80 { // đủ chỗ: thanh xếp chồng và ký hiệu khớp chú giải
				for _, g := range []string{"█", "▓", "▚", "▞"} {
					if !strings.Contains(plain, g) {
						t.Fatalf("vw=%d: thiếu ký hiệu %s: %q", vw, g, plain)
					}
				}
			}
			if vw >= 62 && ansi.StringWidth(plain) > vw {
				t.Fatalf("vw=%d: dòng rộng %d, phải gọn trên một dòng: %q", vw, ansi.StringWidth(plain), plain)
			}
		}
	}
	if got := runsLine(ui.Env{Th: theme.New(theme.Mono), Cat: i18n.MustLoad("vi")}, 0, nil, 40); got != "0" {
		t.Fatalf("không có lượt: %q", got)
	}
}

// Hộp thư xếp theo mức nghiêm trọng; mỗi loại từ 2 mục gộp một dòng có số đếm; có dòng tri thức lỗi thời/dời dòng.
func TestInboxSeverityOrderAndGrouping(t *testing.T) {
	var ev []string
	for i := range 81 {
		ev = append(ev, fmt.Sprintf("E%03d", i))
	}
	d := Data{Pending: &app.PendingWorkReport{
		EvalCandidates: ev, KnowledgeStale: 10, KnowledgeRelocated: 16,
		Doctor: []app.Check{{Name: "git", Level: app.LevelWarn, Detail: "a"}, {Name: "x", Level: app.LevelWarn, Detail: "b"}, {Name: "h", Level: app.LevelFail, Detail: "c"}},
	}, Binary: "1", LockVersion: "2"}
	var keys []string
	for _, in := range d.Inbox() {
		keys = append(keys, in.Level+":"+in.Key)
	}
	want := "fail:overview.inbox.knowledge_stale fail:overview.inbox.doctor_fail warn:overview.inbox.knowledge_relocated warn:overview.inbox.version_skew warn:overview.inbox.doctor_warn_many info:overview.inbox.eval_many"
	if got := strings.Join(keys, " "); got != want {
		t.Errorf("thứ tự/gộp: %s\nmuốn:       %s", got, want)
	}
	for _, in := range d.Inbox() {
		if in.Key == "overview.inbox.eval_many" && in.Count != 81 {
			t.Errorf("số đếm ứng viên: %d", in.Count)
		}
	}
}
