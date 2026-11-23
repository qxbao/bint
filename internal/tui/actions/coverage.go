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
		"docs":               "lệnh dự kiến (P3 của kế hoạch Bint), chưa được triển khai nên chưa có gì để hiển thị",
		"hook antigravity":   "harness gọi qua hook, không phải thao tác của người; trạng thái hook hiện ở Hệ thống › Harness (HarnessRuntime)",
		"hook claude":        "harness gọi qua hook, không phải thao tác của người; trạng thái hook hiện ở Hệ thống › Harness (HarnessRuntime)",
		"hook codex":         "harness gọi qua hook, không phải thao tác của người; trạng thái hook hiện ở Hệ thống › Harness (HarnessRuntime)",
		"run next":           "giao thức của phiên điều phối (orchestrator) chạy bước bằng subagent, không phải thao tác của người; bước đang chạy bằng subagent hiện ở thẻ Phiếu và bảng tác vụ",
		"run complete":       "giao thức của phiên điều phối (orchestrator) chạy bước bằng subagent, không phải thao tác của người; bước đang chạy bằng subagent hiện ở thẻ Phiếu và bảng tác vụ",
		"knowledge audit":    "agent rà soát tri thức qua skill b-knowledge-audit (liệt kê mục cần xem, tự áp mục chỉ dời dòng), không phải thao tác của người; mục cần xem và nhận định hiện ở màn Tri thức",
		"knowledge verdict":  "agent lưu nhận định về một mục (không đổi trạng thái), không phải thao tác của người; người xem và áp bằng knowledge.verdicts_apply ở màn Tri thức",
		"knowledge cite":     "agent báo trích dẫn tri thức trong lượt bint run (gắn BINT_RUN_ID), không phải thao tác của người; trích dẫn được nạp vào knowledge_citations cùng sự kiện của hook",
		"runs history":       "lịch sử lượt chạy theo tệp cho agent (cùng hàm dựng với công cụ MCP history_for_paths), không phải thao tác của người; lượt chạy hiện ở màn Lượt chạy",
		"runs get":           "chi tiết một lượt chạy cho agent (cùng hàm dựng với công cụ MCP run_get); người xem bằng runs.show ở màn Lượt chạy",
		"findings list":      "TUI xem phát hiện open bằng phím F ở màn Tác vụ (hành động đọc không gắn lệnh vì không có ô --task); số phát hiện open hiện ở hàng danh sách",
		"task restore-green": "thao tác chỉ dành cho người (ghi đè tệp về điểm verify xanh cuối của tác vụ dừng), chưa đưa vào TUI; điểm xanh và tệp đã đổi từ đó hiện ở bint task show",
		"findings set":       "thao tác chỉ dành cho người (đổi kết cục phát hiện), chưa đưa vào TUI",
		"test affected":      "công cụ dòng lệnh cho CI và người dùng (chọn/chạy test bị ảnh hưởng theo thay đổi); vòng kiểm chứng của tác vụ ghi phạm vi (scope) vào lần kiểm chứng hiện ở thẻ Kiểm chứng (màn Tác vụ)",
		"task check":         "agent gọi sau mỗi mốc của b-feat luồng B′ (kiểm hẹp theo gói bị chạm), không phải thao tác của người; kết quả ghi vào state.checks và state.background_verifications của phiếu, chưa có màn TUI",
		"task ready":         "TUI đánh dấu phiếu sẵn sàng/chờ phụ thuộc ở danh sách tác vụ và chế độ cây (phím P), dùng task.Ready",
		"mcp serve":          "máy chủ MCP qua stdio cho agent, không có giao diện; trạng thái đăng ký MCP hiện ở Hệ thống › Harness (HarnessRuntime), công cụ của nó hiện ở thẻ Công cụ Bint của màn MCP",
		"iso create":         "thao tác của máy (profile tài khoản ngoài repo, chạy harness tương tác hoặc dọn đĩa), chưa có màn TUI; số profile, dung lượng và cảnh báo hiện ở dòng iso của thẻ Doctor (Hệ thống)",
		"iso du":             "thao tác của máy (profile tài khoản ngoài repo, chạy harness tương tác hoặc dọn đĩa), chưa có màn TUI; số profile, dung lượng và cảnh báo hiện ở dòng iso của thẻ Doctor (Hệ thống)",
		"iso gc":             "thao tác của máy (profile tài khoản ngoài repo, chạy harness tương tác hoặc dọn đĩa), chưa có màn TUI; số profile, dung lượng và cảnh báo hiện ở dòng iso của thẻ Doctor (Hệ thống)",
		"iso import":         "thao tác của máy (profile tài khoản ngoài repo, chạy harness tương tác hoặc dọn đĩa), chưa có màn TUI; số profile, dung lượng và cảnh báo hiện ở dòng iso của thẻ Doctor (Hệ thống)",
		"iso login":          "thao tác của máy (profile tài khoản ngoài repo, chạy harness tương tác hoặc dọn đĩa), chưa có màn TUI; số profile, dung lượng và cảnh báo hiện ở dòng iso của thẻ Doctor (Hệ thống)",
		"iso remove":         "thao tác của máy (profile tài khoản ngoài repo, chạy harness tương tác hoặc dọn đĩa), chưa có màn TUI; số profile, dung lượng và cảnh báo hiện ở dòng iso của thẻ Doctor (Hệ thống)",
		"iso run":            "thao tác của máy (profile tài khoản ngoài repo, chạy harness tương tác hoặc dọn đĩa), chưa có màn TUI; số profile, dung lượng và cảnh báo hiện ở dòng iso của thẻ Doctor (Hệ thống)",
		"iso shell-init":     "thao tác của máy (profile tài khoản ngoài repo, chạy harness tương tác hoặc dọn đĩa), chưa có màn TUI; số profile, dung lượng và cảnh báo hiện ở dòng iso của thẻ Doctor (Hệ thống)",
		"iso status":         "thao tác của máy (profile tài khoản ngoài repo, chạy harness tương tác hoặc dọn đĩa), chưa có màn TUI; số profile, dung lượng và cảnh báo hiện ở dòng iso của thẻ Doctor (Hệ thống)",
		"iso sync":           "thao tác của máy (profile tài khoản ngoài repo, chạy harness tương tác hoặc dọn đĩa), chưa có màn TUI; số profile, dung lượng và cảnh báo hiện ở dòng iso của thẻ Doctor (Hệ thống)",
		"tui":                "chính là điểm vào của giao diện terminal",
		"task done":          "bí danh của `bint task status <id> done` (cùng tùy chọn); TUI dùng hành động tasks.status",
	},
	ExcludedFlags: map[string]string{
		"--json": "TUI luôn nhận dữ liệu có cấu trúc từ tầng dịch vụ, không cần chế độ xuất JSON",
		"--yes":  "thay bằng hộp xác nhận (callback Confirm của dịch vụ)",
	},
	ExcludedCmdFlags: map[string]string{
		"eval report --cases":         "chỉ định dạng đầu ra văn bản (bảng theo từng tác vụ); TUI luôn hiện cả hai",
		"knowledge add --body-file":   "TUI soạn nội dung trong ô nhiều dòng thay vì đọc từ tệp",
		"task new --from-findings":    "B1 phần 1 chỉ làm CLI: lập phiếu sửa từ phát hiện review; form TUI chưa có lựa chọn phát hiện",
		"task new --parent":           "B1 phần 2 chỉ làm CLI: phiếu con kế thừa phạm vi từ phiếu cha; form TUI chưa có lựa chọn phiếu cha",
		"task new --depends-on":       "B1 phần 2 chỉ làm CLI: quan hệ depends_on giữa các phiếu; form TUI chưa có lựa chọn phiếu phụ thuộc",
		"task set --depends-on":       "B1 phần 2 chỉ làm CLI: quan hệ depends_on giữa các phiếu",
		"task set --clear-depends-on": "B1 phần 2 chỉ làm CLI: quan hệ depends_on giữa các phiếu",
		"task new --fast":             "đường nhanh do người dùng chọn bằng CLI (`bint task new --fast`) hoặc skill /b-do-fast; form TUI chưa có ô chọn",
		"run --fast":                  "đường nhanh do người dùng chọn bằng CLI (`bint run b-do --fast`) hoặc skill /b-do-fast; form TUI chưa có ô chọn",
		"task new --variant":          "B′ là mặc định; luồng cũ chọn bằng CLI (`bint task new --variant classic`) hoặc workflow.variant; form TUI chưa có ô chọn",
		"run --variant":               "B′ là mặc định; luồng cũ chọn bằng CLI (`bint run b-do --variant classic`) hoặc workflow.variant; form TUI chưa có ô chọn",
		"task new --file":             "TUI soạn mô tả trong ô nhiều dòng thay vì đọc từ tệp",
	},
	Pending:        map[string]int{},
	PendingFlags:   map[string]int{},
	PendingCeiling: 0,
}
