package knowledge

// Health là mức sức khoẻ của một mục theo kết quả kiểm tra xác định được; dùng chung cho TUI.
type Health int

// Các mức sức khoẻ.
const (
	HealthOK Health = iota
	HealthWarn
	HealthError
)

// HealthOf xếp kết quả kiểm tra: có phát hiện mức error thì lỗi (fingerprint đổi, bằng chứng mất, khối chuyển
// tệp), có phát hiện mức warn thì cảnh báo (vd dời dòng), ngược lại ổn.
func HealthOf(r Result) Health {
	h := HealthOK
	for _, f := range r.Findings {
		switch f.Level {
		case LevelError:
			return HealthError
		case LevelWarn:
			h = HealthWarn
		}
	}
	return h
}
