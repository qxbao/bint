package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	bintapp "bint/internal/app"
	"bint/internal/runlog"
	knscreen "bint/internal/tui/screens/knowledge"
	rnscreen "bint/internal/tui/screens/runs"
	tkscreen "bint/internal/tui/screens/tasks"
	"bint/internal/tui/theme"
)

// longText là một chuỗi 300 ký tự gồm các "từ" duy nhất, để biết có từ nào bị cắt mất hay không.
func longText(tag string) (string, []string) {
	var words []string
	for i := 0; i < 43; i++ {
		words = append(words, fmt.Sprintf("%s%02d", tag, i)) // 6 ký tự + dấu cách
	}
	return strings.Join(words, " "), words
}

// readAll cuộn khung đang focus bằng phím pgdown, gộp mọi dòng đã thấy: nội dung cuộn được thì thấy đủ.
func readAll(t *testing.T, m *Model, down string) string {
	t.Helper()
	var seen strings.Builder
	prev := ""
	for i := 0; i < 40; i++ {
		out := plain(m.Render())
		seen.WriteString(out)
		if out == prev {
			break
		}
		prev = out
		press(t, m, down)
	}
	return seen.String()
}

func wantWords(t *testing.T, what, out string, words []string) {
	t.Helper()
	var missing []string
	for _, w := range words {
		if !strings.Contains(out, w) {
			missing = append(missing, w)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%s: thiếu %d/%d từ (ví dụ %v)", what, len(missing), len(words), missing[:min(3, len(missing))])
	}
}

// Tiêu đề và lỗi dài 300 ký tự ở 80×24 hiện đủ mọi từ ở khung chi tiết (cuộn được) của Tri thức, Tác vụ, Lượt chạy và Hệ thống; lỗi nạp
// ngắt dòng thay vì bị cắt bằng dấu ba chấm.
func TestDetailShowsFullLongTitlesAndErrors(t *testing.T) {
	newM := func(o Options) *Model {
		o.Mode, o.Lang = theme.Mono, "vi"
		return newModel(t, o, sampleData(), 80, 24)
	}
	t.Run("Tri thức: tiêu đề", func(t *testing.T) {
		title, words := longText("kt")
		m := newM(Options{KnowledgeLoader: func(context.Context, string) (knscreen.Data, error) {
			d := sampleKnowledge()
			d.Items[0].Title = title
			return d, nil
		}})
		press(t, m, "2", "enter")
		wantWords(t, "chi tiết tri thức", readAll(t, m, "pgdown"), words)
	})
	t.Run("Tri thức: lỗi nạp chi tiết", func(t *testing.T) {
		msg, words := longText("ke")
		m := newM(Options{KnowledgeDetailer: func(context.Context, string, string) (bintapp.KnowledgeDetail, error) {
			return bintapp.KnowledgeDetail{}, errors.New(msg)
		}})
		press(t, m, "2", "enter")
		wantWords(t, "lỗi chi tiết tri thức", readAll(t, m, "pgdown"), words)
	})
	t.Run("Tri thức: lỗi nạp danh sách", func(t *testing.T) {
		msg, words := longText("kl")
		m := newM(Options{KnowledgeLoader: func(context.Context, string) (knscreen.Data, error) { return knscreen.Data{}, errors.New(msg) }})
		press(t, m, "2")
		wantWords(t, "lỗi nạp danh sách tri thức", plain(m.Render()), words)
	})
	t.Run("Tác vụ: mục tiêu", func(t *testing.T) {
		goal, words := longText("tg")
		m := newM(Options{TasksLoader: func(context.Context, string, bool) (tkscreen.Data, error) {
			d := sampleTasks()
			for _, tk := range d.Tasks {
				tk.Ticket.Goal = goal
			}
			return d, nil
		}})
		press(t, m, "3", "enter")
		wantWords(t, "chi tiết tác vụ", readAll(t, m, "pgdown"), words)
	})
	t.Run("Tác vụ: lỗi nạp chi tiết", func(t *testing.T) {
		msg, words := longText("te")
		m := newM(Options{TasksDetailer: func(context.Context, string, string, bool) (tkscreen.Detail, error) {
			return tkscreen.Detail{}, errors.New(msg)
		}})
		press(t, m, "3", "enter")
		wantWords(t, "lỗi chi tiết tác vụ", readAll(t, m, "pgdown"), words)
	})
	t.Run("Tác vụ: lỗi nạp danh sách", func(t *testing.T) {
		msg, words := longText("tl")
		m := newM(Options{TasksLoader: func(context.Context, string, bool) (tkscreen.Data, error) { return tkscreen.Data{}, errors.New(msg) }})
		press(t, m, "3")
		wantWords(t, "lỗi nạp danh sách tác vụ", plain(m.Render()), words)
	})
	t.Run("Lượt chạy: lý do và tín hiệu", func(t *testing.T) {
		reason, words := longText("rr")
		det, dwords := longText("rd")
		m := newM(Options{
			RunsLoader: func(context.Context, string, int, bool) (rnscreen.Data, error) {
				d := sampleRuns()
				for i := range d.Rows {
					d.Rows[i].Reason = reason
				}
				return d, nil
			},
			RunsDetailer: func(ctx context.Context, dir, id string, ro bool) (*bintapp.RunDetail, error) {
				d, err := sampleRunDetail(ctx, dir, id, ro)
				if d != nil {
					d.Reason = reason
					d.Signals = []runlog.Signal{{Kind: "rating", Strength: "strong", Source: "human", Detail: det}}
				}
				return d, err
			}})
		press(t, m, "4", "enter")
		out := readAll(t, m, "pgdown")
		wantWords(t, "chi tiết lượt chạy (lý do)", out, words)
		wantWords(t, "chi tiết lượt chạy (tín hiệu)", out, dwords)
	})
	t.Run("Lượt chạy: lỗi nạp chi tiết", func(t *testing.T) {
		msg, words := longText("re")
		m := newM(Options{RunsDetailer: func(context.Context, string, string, bool) (*bintapp.RunDetail, error) { return nil, errors.New(msg) }})
		press(t, m, "4", "enter")
		wantWords(t, "lỗi chi tiết lượt chạy", readAll(t, m, "pgdown"), words)
	})
	t.Run("Lượt chạy: lỗi nạp danh sách", func(t *testing.T) {
		msg, words := longText("rl")
		m := newM(Options{RunsLoader: func(context.Context, string, int, bool) (rnscreen.Data, error) {
			return rnscreen.Data{}, errors.New(msg)
		}})
		press(t, m, "4")
		wantWords(t, "lỗi nạp danh sách lượt chạy", plain(m.Render()), words)
	})
	t.Run("MCP: lỗi nạp thẻ Công cụ Bint", func(t *testing.T) {
		msg, words := longText("ml")
		o := Options{}
		o.MCPLoad.Tools = func(context.Context, string, string) ([]bintapp.MCPTool, error) { return nil, errors.New(msg) }
		m := newM(o)
		press(t, m, "8", ".", ".")
		wantWords(t, "lỗi nạp thẻ Công cụ Bint", plain(m.Render()), words)
	})
	t.Run("Hệ thống: chi tiết Doctor", func(t *testing.T) {
		dm, dwords := longText("sd")
		o := Options{}
		o.SystemLoad.Doctor = func(context.Context, string) (*bintapp.DoctorReport, error) {
			return &bintapp.DoctorReport{Checks: []bintapp.Check{{Name: "git", Level: bintapp.LevelFail, Detail: dm}}}, nil
		}
		m := newM(o)
		press(t, m, "7", ".", ".", ".", ".", ".", ".", "enter")
		wantWords(t, "chi tiết Doctor", readAll(t, m, "pgdown"), dwords)
	})
}

// Bảng kiểm: mỗi danh sách có cột bị rút gọn bằng … ở 80×24 đều có nơi hiện bản đầy đủ (khung chi tiết sau Enter, hoặc dòng nguyên văn
// ngay dưới danh sách ở Tổng quan). Thêm danh sách mới có cột bị cắt mà không có chỗ đọc đủ thì thêm một dòng ở đây (và sửa màn hình).
func TestTruncatedListColumnsHaveFullView(t *testing.T) {
	cases := []struct {
		list   string   // danh sách
		column string   // cột bị cắt
		where  string   // nơi hiện bản đầy đủ
		keys   []string // phím tới danh sách rồi mở nơi đó
		full   string   // nội dung đầy đủ phải thấy
	}{
		{"Tổng quan: hộp thư", "nội dung hàng", "nguyên văn dưới danh sách (hàng đang chọn)", []string{"j", "j", "j"}, "nhấn U ở Hệ thống"},
		{"Tri thức: danh sách", "tiêu đề", "khung chi tiết (Enter)", []string{"2", "j", "j", "enter"}, "Handler đi qua wrap()"},
		{"Tác vụ: danh sách", "mục tiêu", "khung chi tiết (Enter)", []string{"3", "j", "enter"}, "Đổi cách tính phí"},
		{"Lượt chạy: danh sách", "nhãn/lý do", "khung chi tiết (Enter)", []string{"4", "enter"}, "không có tín hiệu đủ mạnh"},
		{"Đánh giá: ứng viên", "tiêu đề", "khung chi tiết (Enter)", []string{"5", "enter"}, "Thêm cột email vào bảng người dùng"},
		{"Tiến hoá: cơ hội", "tóm tắt", "khung chi tiết (Enter)", []string{"6", "enter"}, "internal/db bị sửa ở 3 lượt thất bại"},
		{"Tiến hoá: đề xuất", "lý do", "khung chi tiết (Enter)", []string{"6", ".", "enter"}, "Thất bại lặp lại ở internal/db"},
		{"Hệ thống: Harness", "mô tả", "khung chi tiết (Enter)", []string{"7", ".", "enter"}, "claude"},
	}
	for _, tc := range cases {
		t.Run(tc.list, func(t *testing.T) {
			m := newModel(t, Options{Mode: theme.Mono, Lang: "vi"}, sampleData(), 80, 24)
			press(t, m, tc.keys...)
			out := readAll(t, m, "pgdown")
			if !strings.Contains(out, tc.full) {
				t.Fatalf("%s (cột %s) phải đọc đủ ở %s: thiếu %q:\n%s", tc.list, tc.column, tc.where, tc.full, plain(m.Render()))
			}
		})
	}
}
