package actions

// Dữ liệu cho test phủ lệnh và phủ cờ (internal/cli/coverage_test.go): mọi lệnh lá của CLI phải có
// hành động trong sổ, hoặc nằm trong Excluded (kèm lý do), hoặc nằm trong Pending (kèm giai đoạn dự
// kiến). Cờ của lệnh đã có hành động hoặc dịch vụ phải ánh xạ tới một trường Options (thẻ `flag`),
// hoặc nằm trong ExcludedFlags/ExcludedCmdFlags (kèm lý do), hoặc trong PendingFlags.
//
// Pending chỉ được thu nhỏ: PendingCeiling là trần, giảm theo mỗi mục xoá. Ở giai đoạn 6
// (CurrentPhase >= 6) test đòi Pending và PendingFlags rỗng.

// Coverage gom dữ liệu phủ.
type Coverage struct {
	// Excluded: đường lệnh lá → lý do không có màn hình.
	Excluded map[string]string
	// ExcludedFlags: cờ (có "--") không thành trường form ở mọi lệnh → lý do.
	ExcludedFlags map[string]string
	// ExcludedCmdFlags: "đường lệnh --cờ" → lý do (cờ chỉ định dạng đầu ra văn bản...).
	ExcludedCmdFlags map[string]string
	// Pending: đường lệnh lá → giai đoạn (1–6) sẽ có hành động trong TUI.
	Pending map[string]int
	// PendingFlags: "đường lệnh --cờ" → giai đoạn sẽ có trường form cho cờ.
	PendingFlags map[string]int
	// PendingCeiling là trần tổng số mục của Pending và PendingFlags.
	PendingCeiling int
}

// CurrentPhase là giai đoạn của kế hoạch TUI đã làm xong (docs/tui-plan.md mục 9).
const CurrentPhase = 4

// Data là dữ liệu phủ hiện hành.
var Data = Coverage{
	Excluded: map[string]string{
		"docs":             "lệnh dự kiến (P3 của kế hoạch Bint), chưa được triển khai nên chưa có gì để hiển thị",
		"hook antigravity": "harness gọi qua hook, không phải thao tác của người; trạng thái hook hiện ở Hệ thống › Harness (HarnessRuntime)",
		"hook claude":      "harness gọi qua hook, không phải thao tác của người; trạng thái hook hiện ở Hệ thống › Harness (HarnessRuntime)",
		"hook codex":       "harness gọi qua hook, không phải thao tác của người; trạng thái hook hiện ở Hệ thống › Harness (HarnessRuntime)",
		"mcp serve":        "máy chủ MCP qua stdio cho agent, không có giao diện; trạng thái đăng ký MCP hiện ở Hệ thống › Harness (HarnessRuntime), danh sách công cụ ở Hệ thống › MCP",
		"tui":              "chính là điểm vào của giao diện terminal",
	},
	ExcludedFlags: map[string]string{
		"--json": "TUI luôn nhận dữ liệu có cấu trúc từ tầng dịch vụ, không cần chế độ xuất JSON",
		"--yes":  "thay bằng hộp xác nhận (callback Confirm của dịch vụ)",
	},
	ExcludedCmdFlags: map[string]string{
		"eval report --cases":       "chỉ định dạng đầu ra văn bản (bảng theo từng tác vụ); TUI luôn hiện cả hai",
		"knowledge add --body-file": "TUI soạn nội dung trong ô nhiều dòng thay vì đọc từ tệp",
		"task new --file":           "TUI soạn mô tả trong ô nhiều dòng thay vì đọc từ tệp",
	},
	Pending: map[string]int{
		"update": 5,
	},
	PendingFlags: map[string]int{
		"init --agents-md":   5,
		"init --force":       5,
		"init --git":         5,
		"init --harness":     5,
		"init --hooks":       5,
		"init --hooks-scope": 5,
		"init --mcp":         5,
		"init --no-hooks":    5,
		"init --no-mcp":      5,
		"init --no-skills":   5,
		"init --update":      5,
	},
	PendingCeiling: 12,
}
