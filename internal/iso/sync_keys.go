package iso

// Action là việc sync làm (hoặc sẽ làm khi --check) với một khoá hoặc liên kết.
type Action string

const (
	ActWrite    Action = "write"    // chưa có ở profile: ghi bản thật
	ActUpdate   Action = "update"   // chưa sửa tay, bản thật đổi: cập nhật
	ActKeep     Action = "ok"       // đã khớp bản thật
	ActRemove   Action = "remove"   // bản thật đã bỏ, profile chưa sửa tay: gỡ
	ActConflict Action = "conflict" // sửa tay hoặc mục riêng của profile: không ghi
	ActForget   Action = "forget"   // cả hai đã vắng: chỉ dọn receipt
	ActSkip     Action = "skip"     // giữ nguyên có chủ đích (thư mục riêng có dữ liệu), chỉ báo
	ActError    Action = "error"    // không đọc/ghi được tệp
)

// Drift cho biết hành động này là lệch so với bản thật (--check thoát 1).
func (a Action) Drift() bool {
	switch a {
	case ActWrite, ActUpdate, ActRemove, ActConflict, ActError:
		return true
	}
	return false
}

// reconcile là máy trạng thái của một khoá (docs/iso-plan.md mục 5.6; yêu cầu GĐ4 mục 6). d là băm giá trị
// mong muốn từ bản thật, c là băm giá trị hiện có ở profile, r là băm trong receipt; chuỗi rỗng = vắng.
// Xung đột không bao giờ dẫn tới ghi.
func reconcile(d, c, r string) (Action, string) {
	if d != "" {
		switch {
		case c == "":
			return ActWrite, ""
		case c == d:
			return ActKeep, ""
		case r == "":
			return ActConflict, "mục riêng của profile (khác bản thật, không do sync tạo)"
		case c == r:
			return ActUpdate, ""
		}
		return ActConflict, "đã sửa tay ở profile (khác cả bản thật lẫn lần sync trước)"
	}
	switch {
	case r == "":
		return ActKeep, "" // không có gì để làm (không xảy ra với khoá của receipt)
	case c == "":
		return ActForget, ""
	case c == r:
		return ActRemove, ""
	}
	return ActConflict, "bản thật đã bỏ khoá nhưng profile đã sửa tay: giữ nguyên"
}
