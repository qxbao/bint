package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bint/internal/i18n"
	"bint/internal/tui/actions"
)

func cats(t *testing.T) (vi, en *i18n.Catalog) {
	t.Helper()
	vi, err := i18n.Load("vi")
	if err != nil {
		t.Fatal(err)
	}
	en, err = i18n.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	return vi, en
}

func TestRewriteTable(t *testing.T) {
	vi, en := cats(t)
	cases := []struct{ name, in, wantVi, wantEn string }{
		{"không có lệnh", "không có gì để đổi", "không có gì để đổi", "không có gì để đổi"},
		{"động từ + dấu huyền", "thiếu khối (chạy lại `bint init --update` để thêm)", "thiếu khối (nhấn U ở Hệ thống › Cấu hình để thêm)", "thiếu khối (press U in System › Config để thêm)"},
		{"ngoặc, ID", "có thay đổi chờ duyệt (bint task approve 01M769D0Y45T2Y178ZC5GHD5QW)", "có thay đổi chờ duyệt (phím a ở Tác vụ)", "có thay đổi chờ duyệt (key a in Tasks)"},
		{"cờ đặc thù --apply", "bằng chứng X (`bint knowledge verify --apply` cập nhật vùng dòng)", "bằng chứng X (phím v ở Tri thức [ô Áp dụng] cập nhật vùng dòng)", ""},
		{"cờ --refresh", "xem lại (`bint knowledge show 01ABC`) rồi `bint knowledge approve --refresh 01ABC`", "xem lại (phím Enter ở Tri thức) rồi phím f ở Tri thức", ""},
		{"nhiều lệnh một câu", "bint task set 01M769D0Y45T2Y178ZC5GHD5QW --verify-command '…' rồi bint run resume 01M769D0Y45T2Y178ZC5GHD5QW", "phím e ở Tác vụ [ô Lệnh kiểm chứng] rồi phím c ở Tác vụ", ""},
		{"lệnh con anh em", "người dùng (`bint knowledge reject/deprecate`)", "người dùng (phím r ở Tri thức / phím d ở Tri thức)", ""},
		{"lệnh ngoài sổ giữ nguyên", "bint hook: lỗi; bint mcp serve", "bint hook: lỗi; bint mcp serve", "bint hook: lỗi; bint mcp serve"},
		{"chữ bint không kèm lệnh", "bint không có trong PATH; nhánh bint thì không", "bint không có trong PATH; nhánh bint thì không", "bint không có trong PATH; nhánh bint thì không"},
		{"đường dẫn chứa bint", "xem .bint/config.yml và internal/bint sync", "xem .bint/config.yml và internal/bint sync", "xem .bint/config.yml và internal/bint sync"},
		{"bint run danh từ", "tác vụ đang được `bint run` điều phối (lượt X)", "tác vụ đang được Bint điều phối (lượt X)", ""},
		{"bint run lồng", "không chạy `bint run` lồng trong bước khác", "không chạy tác vụ lồng trong bước khác", ""},
		{"chỉ có tên màn", "Khung đánh giá (bint eval --help) chỉ cần khi muốn đo", "Khung đánh giá (thẻ Đánh giá) chỉ cần khi muốn đo", ""},
		{"thẻ phụ, mục bảng lệnh", "chưa xuất bản (bint sync)", "chưa xuất bản (phím s ở Hệ thống › Skill)", ""},
		{"run kèm workflow và cờ giá trị", "hoặc lập kế hoạch lại bằng `bint run b-req --task X --depth deep` trên tác vụ planned", "hoặc lập kế hoạch lại bằng phím r ở Tác vụ [ô Tác vụ, Độ sâu b-req] trên tác vụ planned", ""},
		{"chạy tiếp bằng", "chạy tiếp bằng `bint eval run --resume eb_1 --budget <mới>`", "tiếp tục bằng phím R ở Đánh giá [ô Ngân sách ($)]", ""},
		{"thẻ doctor", "chạy `bint doctor`", "mở thẻ Hệ thống › Doctor", "open the System › Doctor tab"},
	}
	for _, c := range cases {
		got := Rewriter{Cat: vi}.Rewrite(c.in)
		if !strings.HasPrefix(got, c.wantVi) && got != c.wantVi {
			t.Errorf("%s (vi):\n  in:   %s\n  got:  %s\n  want: %s", c.name, c.in, got, c.wantVi)
		}
		if strings.Contains(got, "bint ") && !strings.Contains(c.wantVi, "bint ") {
			t.Errorf("%s: còn lệnh bint: %s", c.name, got)
		}
		if c.wantEn != "" {
			if got := (Rewriter{Cat: en}).Rewrite(c.in); got != c.wantEn {
				t.Errorf("%s (en):\n  got:  %s\n  want: %s", c.name, got, c.wantEn)
			}
		}
	}
}

// Review 6b .3: lệnh mang cờ ghi (knowledge verify --apply) vẫn chỉ phím v kèm ô Áp dụng; ở chỉ đọc có ghi chú vì phần áp dụng chạy
// knowledge.verify_apply (Writes), ở chế độ thường không có. "knowledge verify" không cờ ghi (chỉ đọc) không bao giờ có ghi chú.
func TestRewriteWriteFlagReadOnlyNote(t *testing.T) {
	vi, en := cats(t)
	const in = "bằng chứng X (`bint knowledge verify --apply` cập nhật vùng dòng)"
	const note = " (không dùng được ở chế độ chỉ đọc)"
	normal := Rewriter{Cat: vi}.Rewrite(in)
	ro := Rewriter{Cat: vi, ReadOnly: true}.Rewrite(in)
	if want := "bằng chứng X (phím v ở Tri thức [ô Áp dụng] cập nhật vùng dòng)"; normal != want {
		t.Errorf("thường:\n got %s\nwant %s", normal, want)
	}
	if strings.Contains(normal, "chỉ đọc") {
		t.Errorf("chế độ thường không có ghi chú chỉ đọc: %s", normal)
	}
	if want := "bằng chứng X (phím v ở Tri thức [ô Áp dụng]" + note + " cập nhật vùng dòng)"; ro != want {
		t.Errorf("chỉ đọc:\n got %s\nwant %s", ro, want)
	}
	if got := (Rewriter{Cat: en, ReadOnly: true}).Rewrite(in); !strings.Contains(got, "read-only") && !strings.Contains(got, "chỉ đọc") {
		t.Errorf("en chỉ đọc thiếu ghi chú: %s", got)
	}
	// Không cờ ghi: lệnh chỉ đọc, không ghi chú ở cả hai chế độ.
	if got := (Rewriter{Cat: vi, ReadOnly: true}).Rewrite("chạy `bint knowledge verify`"); strings.Contains(got, "chỉ đọc") {
		t.Errorf("knowledge verify không cờ ghi không được có ghi chú chỉ đọc: %s", got)
	}
}

// Review 6b .4/.10: chế độ chỉ đọc là của từng Rewriter, không phải trạng thái chung của gói: dựng theo thứ tự nào cũng đúng.
func TestRewritersWithDifferentReadOnlyAreIndependent(t *testing.T) {
	vi, _ := cats(t)
	const in = "duyệt bằng `bint knowledge approve 01M769D0Y45T2Y178ZC5GHD5QW`"
	for _, order := range [][2]bool{{false, true}, {true, false}} {
		a, b := Rewriter{Cat: vi, ReadOnly: order[0]}, Rewriter{Cat: vi, ReadOnly: order[1]}
		for _, rw := range []Rewriter{a, b} {
			if got := strings.Contains(rw.Rewrite(in), "chỉ đọc"); got != rw.ReadOnly {
				t.Errorf("ReadOnly=%v nhưng ghi chú=%v (thứ tự dựng %v)", rw.ReadOnly, got, order)
			}
		}
	}
}

// Sổ tuỳ chỉnh (Options.Registry) quyết định phím và việc có Writes hay không, không phải sổ mặc định.
func TestRewriterUsesItsOwnRegistry(t *testing.T) {
	vi, _ := cats(t)
	var list []actions.Action
	for _, a := range actions.Default().All() {
		switch a.ID {
		case "knowledge.approve":
			a.Key = "Y"
			a.Writes = false // sổ này coi duyệt là không ghi: không có ghi chú chỉ đọc
		}
		list = append(list, a)
	}
	reg := actions.NewRegistry(list)
	const in = "duyệt bằng `bint knowledge approve 01M769D0Y45T2Y178ZC5GHD5QW`"
	custom := Rewriter{Cat: vi, Reg: reg, ReadOnly: true}.Rewrite(in)
	if want := "duyệt bằng phím Y ở Tri thức"; custom != want {
		t.Errorf("sổ tuỳ chỉnh:\n got %s\nwant %s", custom, want)
	}
	def := Rewriter{Cat: vi, ReadOnly: true}.Rewrite(in)
	if !strings.Contains(def, "phím a ở Tri thức") || !strings.Contains(def, "chỉ đọc") {
		t.Errorf("sổ mặc định: %s", def)
	}
}

// Review 6b .12: mã có tiền tố chữ thường + gạch dưới (ep_, eb_...) là đối số của lệnh, không để sót sau tham chiếu; từ thường có gạch
// dưới và cờ trần của công cụ ngoài không bị đổi.
func TestRewritePrefixedIDsAndLooseFlags(t *testing.T) {
	vi, en := cats(t)
	ulid := "01M76KN3M9BZCNMHKN4KNP1639"
	cases := []struct{ name, in, wantVi, wantEn string }{
		{"ep_ sau tham chiếu", "đề xuất quay lui (người áp dụng): bint evolve rollback ep_" + ulid, "đề xuất quay lui (người áp dụng): phím u ở Tiến hoá", ""},
		{"eb_ có chữ số", "chạy tiếp: bint eval run --resume eb_123 --budget 5", "chạy tiếp: phím R ở Đánh giá [ô Ngân sách ($)]", ""},
		{"ea_ ec_ er_ ee_ ead_", "bint evolve show ea_1 và bint evolve show ead_" + ulid + " rồi bint evolve show ee_7", "", ""},
		{"từ thường có gạch dưới giữ nguyên", "bint knowledge show snake_case_word", "", ""},
		{"cờ unverified", "chưa qua kiểm chứng — hoặc dùng --unverified", "chưa qua kiểm chứng — hoặc chọn ô bỏ qua kiểm chứng trong hộp xác nhận", "chưa qua kiểm chứng — or tick the skip-verification box in the confirmation dialog"},
		{"cờ confirm-risk", "xem lại rồi thêm --confirm-risk", "xem lại rồi đánh dấu ô chấp nhận rủi ro cao trong hộp xác nhận", "xem lại rồi tick the accept-high-risk box in the confirmation dialog"},
		{"cờ force", "cần bằng chứng; người có thể --force", "cần bằng chứng; người có thể ép (ô Ép của form đổi trạng thái)", "cần bằng chứng; a human can force it (the Force field of the status form)"},
		{"cờ git local", "chế độ riêng máy (--git local): tệp không hiện", "chế độ riêng máy (ô Chế độ Git: local): tệp không hiện", "chế độ riêng máy (Git mode field: local): tệp không hiện"},
		{"tiền tố --update", "--update: cấu hình nằm ở worktree chính", "Nâng cấp cấu hình: cấu hình nằm ở worktree chính", "Config upgrade: cấu hình nằm ở worktree chính"},
		{"không chứa bint", "người có thể --force", "người có thể ép (ô Ép của form đổi trạng thái)", ""},
		{"cờ trần của công cụ ngoài giữ nguyên", "git: use --force to override; thêm --foo", "git: use --force to override; thêm --foo", "git: use --force to override; thêm --foo"},
	}
	for _, c := range cases {
		got := Rewriter{Cat: vi}.Rewrite(c.in)
		if c.wantVi != "" && got != c.wantVi {
			t.Errorf("%s (vi):\n got %s\nwant %s", c.name, got, c.wantVi)
		}
		if strings.Contains(got, "ep_") || strings.Contains(got, "eb_") || strings.Contains(got, "ea_") || strings.Contains(got, "ead_") || strings.Contains(got, "ee_") {
			if !strings.Contains(c.name, "từ thường") {
				t.Errorf("%s: còn sót mã sau tham chiếu: %s", c.name, got)
			}
		}
		if c.wantEn != "" {
			if got := (Rewriter{Cat: en}).Rewrite(c.in); got != c.wantEn {
				t.Errorf("%s (en):\n got %s\nwant %s", c.name, got, c.wantEn)
			}
		}
	}
	// Từ thường sau lệnh: lệnh được đổi, từ có gạch dưới còn nguyên (không bị nuốt nhầm).
	if got := (Rewriter{Cat: vi}).Rewrite("bint knowledge show snake_case_word"); !strings.HasSuffix(got, "snake_case_word") {
		t.Errorf("từ thường có gạch dưới bị nuốt: %s", got)
	}
	if got := (Rewriter{Cat: vi}).Rewrite("bint knowledge show ea_1 và bint evolve show ead_" + ulid); strings.Contains(got, "ea_") || strings.Contains(got, "ead_") {
		t.Errorf("còn sót mã: %s", got)
	}
}

// Review 6b-sửa .4: đối số mã dạng tiền tố + gạch dưới phải có ít nhất một chữ số ở phần sau; hậu tố chỉ có chữ (task_id, key_name,
// item_x) là từ thường của câu và không bị nuốt vào tham chiếu.
func TestRewritePositionalIDNeedsDigit(t *testing.T) {
	vi, en := cats(t)
	ulid := "01M76KN3M9BZCNMHKN4KNP1639"
	for _, word := range []string{"task_id", "key_name", "item_x"} {
		for name, c := range map[string]*i18n.Catalog{"vi": vi, "en": en} {
			got := Rewriter{Cat: c}.Rewrite("bint task show " + word)
			if !strings.HasSuffix(got, " "+word) {
				t.Errorf("%s: %q bị nuốt vào tham chiếu: %s", name, word, got)
			}
			if strings.Contains(got, "bint ") {
				t.Errorf("%s: lệnh vẫn còn nguyên: %s", name, got)
			}
		}
	}
	for _, id := range []string{"ep_" + ulid, "eb_123", "ea_1", "ead_" + ulid, "ee_7"} {
		got := Rewriter{Cat: vi}.Rewrite("bint evolve show " + id)
		if strings.Contains(got, id) || strings.Contains(got, "_") {
			t.Errorf("mã %s phải được nuốt vào tham chiếu: %s", id, got)
		}
	}
}

// ErrText đi qua Rewriter của mô hình: ghi chú chỉ đọc theo từng Rewriter.
func TestErrTextUsesRewriterReadOnly(t *testing.T) {
	vi, _ := cats(t)
	err := errors.New("bằng chứng đã đổi (`bint knowledge verify --apply` cập nhật vùng dòng)")
	if got := ErrText(Rewriter{Cat: vi}, err); strings.Contains(got, "chỉ đọc") || !strings.Contains(got, "phím v ở Tri thức") {
		t.Errorf("thường: %s", got)
	}
	if got := ErrText(Rewriter{Cat: vi, ReadOnly: true}, err); !strings.Contains(got, "chỉ đọc") {
		t.Errorf("chỉ đọc: %s", got)
	}
}

// Mọi khoá cụm thay thế (cliref.phrase.*, cliref.flag.*) có ở vi và en, và cụm thay thế của en không còn token cờ "--x".
func TestPhraseKeysExistInBothLanguages(t *testing.T) {
	vi, en := cats(t)
	if len(phraseRE) < 35 {
		t.Fatalf("thiếu cụm thay thế: %d", len(phraseRE))
	}
	for _, p := range phraseRE {
		for name, c := range map[string]*i18n.Catalog{"vi": vi, "en": en} {
			if !c.Has(p.key) {
				t.Errorf("thiếu khoá %s ở %s", p.key, name)
			} else if strings.Contains(c.T(p.key), "--") {
				t.Errorf("khoá %s (%s) còn token cờ: %q", p.key, name, c.T(p.key))
			}
		}
	}
}

// srcFrag trả frag sau khi kiểm nó còn nằm nguyên văn trong tệp nguồn của dịch vụ (đường tương đối từ gốc repo): bảng test không lệch
// khỏi chuỗi thật mà người dùng nhận.
func srcFrag(t *testing.T, file, frag string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", filepath.FromSlash(file)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), frag) {
		t.Fatalf("%s không còn chứa %q: cập nhật test theo mã dịch vụ", file, frag)
	}
	return frag
}

// Review 6b-sửa .2: cờ CLI rời trong thông điệp của luồng duyệt ứng viên, đề xuất, đánh giá, quay lui và chạy đánh giá được thay bằng
// cụm chỉ tới ô form/hộp tương ứng; chuỗi vào lấy nguyên văn từ nguồn dịch vụ, ở cả vi và en, và không còn token cờ nào.
func TestRewriteLooseFlagsOfServiceFlows(t *testing.T) {
	vi, en := cats(t)
	ulid := "01M76KN3M9BZCNMHKN4KNP1639"
	cases := []struct{ name, in, wantVi, wantEn string }{
		{"duyệt ứng viên đã bị loại", ulid + " đã bị loại (trùng); " + srcFrag(t, "internal/app/eval_cases.go", "dùng --force nếu vẫn muốn duyệt"),
			ulid + " đã bị loại (trùng); chọn ô “Vẫn duyệt dù …” trong hộp duyệt nếu vẫn muốn duyệt",
			ulid + " đã bị loại (trùng); tick the “Approve anyway although …” box in the approve dialog if you still want to approve"},
		{"duyệt ứng viên lộ lời giải", ulid + ": đề bài lộ lời giải (x) — sửa `prompt` trong /tmp/c.yml " + srcFrag(t, "internal/app/eval_cases.go", "hoặc dùng --force"),
			ulid + ": đề bài lộ lời giải (x) — sửa `prompt` trong /tmp/c.yml hoặc chọn ô “Vẫn duyệt dù …” trong hộp duyệt",
			ulid + ": đề bài lộ lời giải (x) — sửa `prompt` trong /tmp/c.yml or tick the “Approve anyway although …” box in the approve dialog"},
		{"đánh giá không khả thi", "kế hoạch không khả thi: thêm tác vụ, nâng evolution.evaluation.min_effect, " + srcFrag(t, "internal/app/evolvesvc/evaluate.go", "hoặc dùng --force để chạy vẫn"),
			"kế hoạch không khả thi: thêm tác vụ, nâng evolution.evaluation.min_effect, hoặc chọn ô “Chạy dù không khả thi” của form đánh giá để chạy vẫn",
			"kế hoạch không khả thi: thêm tác vụ, nâng evolution.evaluation.min_effect, or tick the “Run although infeasible” box of the evaluate form to run anyway"},
		{"quay lui", "tệp đã bị sửa sau khi áp dụng: a.go; xem lại rồi " + srcFrag(t, "internal/evolve/apply.go", "dùng --force để ghi đè") + " bằng bản trước khi áp dụng",
			"tệp đã bị sửa sau khi áp dụng: a.go; xem lại rồi chọn ô ghi đè tệp đã bị sửa trong hộp quay lui để ghi đè bằng bản trước khi áp dụng",
			"tệp đã bị sửa sau khi áp dụng: a.go; xem lại rồi tick the overwrite-edited-files box in the rollback dialog to overwrite bằng bản trước khi áp dụng"},
		{"đề xuất cần agent (generate)", srcFrag(t, "internal/evolve/generate.go", "cần agent evolver (bỏ --no-agent)"),
			"cần agent evolver (bỏ chọn ô “Không dùng agent” của form đề xuất)", "cần agent evolver (untick the “No agent” box of the propose form)"},
		{"đề xuất cần agent (quy trình)", srcFrag(t, "internal/evolve/harness_propose.go", "cần agent đề xuất quy trình (roles.evolver = claude; bỏ --no-agent)"),
			"cần agent đề xuất quy trình (roles.evolver = claude; bỏ chọn ô “Không dùng agent” của form đề xuất)", ""},
		{"đề xuất cần agent (reflector)", srcFrag(t, "internal/evolve/reflector.go", "cần agent reflector (bỏ --no-agent)"),
			"cần agent reflector (bỏ chọn ô “Không dùng agent” của form đề xuất)", ""},
		{"ngân sách chạy đánh giá", "còn $1.00 (eval.prior_run_usd); " + srcFrag(t, "internal/eval/runner.go", "tăng --budget") + " hoặc hạ eval.prior_run_usd",
			"còn $1.00 (eval.prior_run_usd); tăng ô “Ngân sách ($)” của form chạy hoặc hạ eval.prior_run_usd",
			"còn $1.00 (eval.prior_run_usd); raise the “Budget ($)” field of the run form hoặc hạ eval.prior_run_usd"},
	}
	for _, c := range cases {
		if got := (Rewriter{Cat: vi}).Rewrite(c.in); got != c.wantVi {
			t.Errorf("%s (vi):\n got %s\nwant %s", c.name, got, c.wantVi)
		}
		got := (Rewriter{Cat: en}).Rewrite(c.in)
		if c.wantEn != "" && got != c.wantEn {
			t.Errorf("%s (en):\n got %s\nwant %s", c.name, got, c.wantEn)
		}
		for _, tok := range []string{"--force", "--no-agent", "--budget"} {
			if strings.Contains(got, tok) {
				t.Errorf("%s (en): còn token %s: %s", c.name, tok, got)
			}
		}
	}
	// Cờ của công cụ ngoài vẫn giữ nguyên.
	const ext = "git: use --force to override"
	if got := (Rewriter{Cat: vi}).Rewrite(ext); got != ext {
		t.Errorf("lỗi ngoài bị đổi: %s", got)
	}
}

// Review 6b-sửa-2 .1: cụm "dùng --force để ghi đè" chỉ khớp thông điệp quay lui của evolve; thông điệp xung đột tệp của fsafe/skills
// (chuỗi thật lấy từ nguồn) giữ nguyên nghĩa ở cả vi và en.
func TestRewriteForceOverwriteOnlyRollback(t *testing.T) {
	vi, en := cats(t)
	for _, c := range []struct{ name, in string }{
		{"fsafe ghi đè", "tệp đã bị sửa ngoài Bint; " + srcFrag(t, "internal/fsafe/fsafe.go", "dùng --force để ghi đè (bản cũ sẽ được sao lưu)")},
		{"fsafe gỡ", "tệp đã bị sửa ngoài Bint; " + srcFrag(t, "internal/fsafe/fsafe.go", "dùng --force để gỡ")},
		{"skills", "kỹ năng đã có; " + srcFrag(t, "internal/skills/skills.go", "--force để ghi đè")},
	} {
		for name, cat := range map[string]*i18n.Catalog{"vi": vi, "en": en} {
			if got := (Rewriter{Cat: cat}).Rewrite(c.in); got != c.in {
				t.Errorf("%s (%s): bị đổi:\n got %s\nwant %s", c.name, name, got, c.in)
			}
		}
	}
	// Thông điệp quay lui thật vẫn thành cụm hộp quay lui, không còn --force.
	rb := srcFrag(t, "internal/evolve/apply.go", "xem lại rồi dùng --force để ghi đè bằng bản trước khi áp dụng")
	want := "xem lại rồi chọn ô ghi đè tệp đã bị sửa trong hộp quay lui để ghi đè bằng bản trước khi áp dụng"
	if got := (Rewriter{Cat: vi}).Rewrite(rb); got != want {
		t.Errorf("quay lui (vi):\n got %s\nwant %s", got, want)
	}
	if got := (Rewriter{Cat: en}).Rewrite(rb); strings.Contains(got, "--force") {
		t.Errorf("quay lui (en): còn --force: %s", got)
	}
}

// Review 6b-sửa-2 .2: cờ trong một lệnh bint đọc được ("`bint evolve process --budget <USD>`") do bộ đọc lệnh xử lý (chỉ tới ô form);
// "--budget <USD>" rời (không kèm lệnh bint) vẫn thành cụm ô ngân sách.
func TestRewriteBudgetFlagInsideCommand(t *testing.T) {
	vi, en := cats(t)
	note := srcFrag(t, "internal/evolve/deferred.go", "cần agent evolver: đặt evolution.deferred.budget_usd hoặc chạy `bint evolve process --budget <USD>`")
	got := (Rewriter{Cat: vi}).Rewrite(note)
	if !strings.Contains(got, "[ô Ngân sách USD]") || strings.Contains(got, "--budget") {
		t.Errorf("deferred (vi): %s", got)
	}
	if got := (Rewriter{Cat: en}).Rewrite(note); strings.Contains(got, "--budget") {
		t.Errorf("deferred (en): còn --budget: %s", got)
	}
	loose := srcFrag(t, "internal/app/eval_run.go", "cần ngân sách: --budget <USD> hoặc eval.budget_usd")
	if got, want := (Rewriter{Cat: vi}).Rewrite(loose), "cần ngân sách: ô ngân sách hoặc eval.budget_usd"; got != want {
		t.Errorf("thiếu ngân sách (vi):\n got %s\nwant %s", got, want)
	}
}
