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
const CurrentPhase = 0

// Data là dữ liệu phủ hiện hành.
var Data = Coverage{
	Excluded: map[string]string{
		"docs":             "lệnh dự kiến (P3 của kế hoạch Bint), chưa được triển khai nên chưa có gì để hiển thị",
		"hook antigravity": "harness gọi qua hook, không phải thao tác của người; trạng thái hook hiện ở Hệ thống › Harness (giai đoạn 5)",
		"hook claude":      "harness gọi qua hook, không phải thao tác của người; trạng thái hook hiện ở Hệ thống › Harness (giai đoạn 5)",
		"hook codex":       "harness gọi qua hook, không phải thao tác của người; trạng thái hook hiện ở Hệ thống › Harness (giai đoạn 5)",
		"mcp serve":        "máy chủ MCP qua stdio cho agent, không có giao diện; trạng thái hiện ở Hệ thống › Harness (giai đoạn 5)",
		"tui":              "chính là điểm vào của giao diện terminal",
	},
	ExcludedFlags: map[string]string{
		"--json": "TUI luôn nhận dữ liệu có cấu trúc từ tầng dịch vụ, không cần chế độ xuất JSON",
		"--yes":  "thay bằng hộp xác nhận (callback Confirm của dịch vụ)",
	},
	ExcludedCmdFlags: map[string]string{
		"eval report --cases": "chỉ định dạng đầu ra văn bản (bảng theo từng tác vụ); TUI luôn hiện cả hai",
	},
	Pending: map[string]int{
		"bootstrap":                   1,
		"bootstrap publish":           1,
		"bootstrap skill":             1,
		"context":                     1,
		"eval approve":                4,
		"eval bootstrap":              4,
		"eval clean":                  4,
		"eval estimate":               4,
		"eval list":                   4,
		"eval mine":                   4,
		"eval reject":                 4,
		"eval report":                 4,
		"eval run":                    4,
		"eval show":                   4,
		"eval spec":                   4,
		"eval verify":                 4,
		"events ingest":               3,
		"evolve apply":                3,
		"evolve cycle":                3,
		"evolve evaluate":             3,
		"evolve history":              3,
		"evolve list":                 3,
		"evolve process":              3,
		"evolve propose":              3,
		"evolve queue":                3,
		"evolve reject":               3,
		"evolve rollback":             3,
		"evolve scan":                 3,
		"evolve show":                 3,
		"experience digest":           3,
		"experience show":             3,
		"harness component add":       5,
		"harness component approve":   5,
		"harness component deprecate": 5,
		"harness component list":      5,
		"harness component show":      5,
		"harness list":                5,
		"harness render":              5,
		"harness show":                5,
		"index rebuild":               5,
		"inspect":                     1,
		"knowledge add":               1,
		"knowledge approve":           1,
		"knowledge deprecate":         1,
		"knowledge diff":              1,
		"knowledge list":              1,
		"knowledge reject":            1,
		"knowledge revalidate":        1,
		"knowledge review":            1,
		"knowledge rm":                1,
		"knowledge search":            1,
		"knowledge show":              1,
		"knowledge stats":             1,
		"knowledge transitions":       1,
		"knowledge verify":            1,
		"mcp tools":                   5,
		"run":                         2,
		"run resume":                  2,
		"run status":                  2,
		"runs list":                   3,
		"runs rate":                   3,
		"runs scan":                   3,
		"runs show":                   3,
		"skill check":                 5,
		"skill diff":                  5,
		"skill list":                  5,
		"skill overlay":               5,
		"skill regress":               5,
		"skill show":                  5,
		"skill versions":              5,
		"sync":                        5,
		"task approve":                2,
		"task criteria":               2,
		"task error":                  2,
		"task lint":                   2,
		"task list":                   2,
		"task new":                    2,
		"task review":                 2,
		"task set":                    2,
		"task show":                   2,
		"task status":                 2,
		"task transitions":            2,
		"task verify":                 2,
		"task write":                  2,
		"update":                      5,
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
	PendingCeiling: 96,
}
