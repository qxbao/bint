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
const CurrentPhase = 6

// Data là dữ liệu phủ hiện hành.
var Data = Coverage{
	Excluded: map[string]string{
		"docs":             "lệnh dự kiến (P3 của kế hoạch Bint), chưa được triển khai nên chưa có gì để hiển thị",
		"hook antigravity": "harness gọi qua hook, không phải thao tác của người; trạng thái hook hiện ở Hệ thống › Harness (HarnessRuntime)",
		"hook claude":      "harness gọi qua hook, không phải thao tác của người; trạng thái hook hiện ở Hệ thống › Harness (HarnessRuntime)",
		"hook codex":       "harness gọi qua hook, không phải thao tác của người; trạng thái hook hiện ở Hệ thống › Harness (HarnessRuntime)",
		"run next":         "giao thức của phiên điều phối (orchestrator) chạy bước bằng subagent, không phải thao tác của người; bước đang chạy bằng subagent hiện ở thẻ Phiếu và bảng tác vụ",
		"run complete":     "giao thức của phiên điều phối (orchestrator) chạy bước bằng subagent, không phải thao tác của người; bước đang chạy bằng subagent hiện ở thẻ Phiếu và bảng tác vụ",
		"knowledge cite":   "agent báo trích dẫn tri thức trong lượt bint run (gắn BINT_RUN_ID), không phải thao tác của người; trích dẫn được nạp vào knowledge_citations cùng sự kiện của hook",
		"mcp add":          "màn MCP thuộc M4 (docs/mcp-gateway-plan.md mục 8): lệnh đã có tầng dịch vụ app.MCP* nhưng chưa có hành động trong TUI",
		"mcp check":        "màn MCP thuộc M4 (docs/mcp-gateway-plan.md mục 8): lệnh đã có tầng dịch vụ app.MCP* nhưng chưa có hành động trong TUI",
		"mcp grants":       "màn MCP thuộc M4 (docs/mcp-gateway-plan.md mục 8): lệnh đã có tầng dịch vụ app.MCP* nhưng chưa có hành động trong TUI",
		"mcp index":        "màn MCP thuộc M4 (docs/mcp-gateway-plan.md mục 8): lệnh đã có tầng dịch vụ app.MCP* nhưng chưa có hành động trong TUI",
		"mcp list":         "màn MCP thuộc M4 (docs/mcp-gateway-plan.md mục 8): lệnh đã có tầng dịch vụ app.MCP* nhưng chưa có hành động trong TUI",
		"mcp remove":       "màn MCP thuộc M4 (docs/mcp-gateway-plan.md mục 8): lệnh đã có tầng dịch vụ app.MCP* nhưng chưa có hành động trong TUI",
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
	Pending:        map[string]int{},
	PendingFlags:   map[string]int{},
	PendingCeiling: 0,
}
