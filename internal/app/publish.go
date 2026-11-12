package app

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/fsafe"
	"bint/internal/harness/claude"
	"bint/internal/hcomp"
	"bint/internal/knowledge"
	"bint/internal/state"
)

// AgentsBlock dựng nội dung khối Bint của AGENTS.md từ tri thức trong root.
func AgentsBlock(root string, cfg config.Config) (string, knowledge.Selection, error) {
	items, _, err := knowledge.NewStore(root).List()
	if err != nil {
		return "", knowledge.Selection{}, err
	}
	inner, sel := knowledge.RenderAgents(items, knowledge.RenderOptions{MaxItems: cfg.Knowledge.AgentsMDMaxItems})
	// Chỉ dẫn quy trình CHUNG đã duyệt (H1): guidance verified trong .bint/harness và danh sách
	// script đã duyệt. Bản dựng sẵn (ghi chú MCP của nhánh eval) không vào AGENTS.md thật;
	// repository không có thành phần nào thì khối không đổi một byte.
	if g := hcomp.Load(root).Select(hcomp.ModeOn).Files().RenderGuidance(hcomp.RenderOpts{MCP: cfg.Context.Interfaces.MCP}); g != "" {
		inner = strings.TrimRight(inner, "\n") + "\n" + g
	}
	return inner, sel, nil
}

// PublishOptions điều khiển `bint bootstrap publish`.
type PublishOptions struct {
	Dir    string
	DryRun bool `flag:"dry-run" form:"hidden"`
	Force  bool `flag:"force"`
	// Yes bỏ qua lời hỏi xác nhận. Không Yes và không Confirm (không ai trả lời được) thì ghi thay
	// đổi AGENTS.md là lỗi dùng sai, như eval run/bootstrap và evolve evaluate; DryRun thì không cần.
	Yes bool `flag:"yes" form:"hidden"`
	// Confirm được gọi trước khi ghi thay đổi (Detail = bản thay đổi); trả false thì không ghi và
	// PublishAgents trả báo cáo với Written=false (không phải lỗi).
	Confirm ConfirmFunc
	Now     time.Time
}

// Validate kiểm tra ràng buộc mức giá trị của tuỳ chọn xuất bản.
func (o PublishOptions) Validate() error {
	if o.Dir == "" {
		return Usagef("thiếu thư mục repository")
	}
	return nil
}

// PublishDefaults trả tuỳ chọn mặc định (cấu hình hiện chưa ảnh hưởng tới xuất bản).
func PublishDefaults(_ config.Config, dir string) PublishOptions {
	return PublishOptions{Dir: dir}
}

// ConfirmPublishKey là khoá i18n của lời hỏi xác nhận xuất bản.
const ConfirmPublishKey = "bootstrap.publish.confirm"

// PublishReport là kết quả xuất bản.
type PublishReport struct {
	Diff      string
	Selection knowledge.Selection
	Steps     []Step
	Written   bool
	Changed   bool
}

// PublishAgents cập nhật khối Bint trong AGENTS.md (và dòng nhập lại trong
// CLAUDE.md khi harness claude được bật) từ các quy tắc đã duyệt. Luôn tạo bản
// thay đổi để xem trước; nội dung ngoài khối không bị chạm tới (mục 4.2 bước 6).
func PublishAgents(ctx context.Context, opts PublishOptions) (*PublishReport, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	ws, err := OpenWorkspace(ctx, opts.Dir, true)
	if err != nil {
		return nil, err
	}
	root := filepath.Dir(filepath.Dir(ws.Sources.Project))
	inner, sel, err := AgentsBlock(root, ws.Config)
	if err != nil {
		return nil, err
	}
	rep := &PublishReport{Selection: sel}
	agentsPath := filepath.Join(root, "AGENTS.md")
	old, err := fsafe.ReadBlock(agentsPath, fsafe.HTMLMarkers)
	if err != nil {
		return nil, err
	}
	before := ""
	if old != nil {
		before = *old
	}
	rep.Changed = strings.TrimSpace(before) != strings.TrimSpace(inner)
	rep.Diff = UnifiedDiff("AGENTS.md (khối bint, hiện tại)", "AGENTS.md (khối bint, mới)", before, inner)
	if opts.DryRun || (!rep.Changed && old != nil && claudeImportOK(root, ws.Config)) {
		return rep, nil
	}
	if rep.Changed && !opts.Yes {
		if opts.Confirm == nil {
			return nil, errNeedYes()
		}
		if !opts.Confirm(ConfirmRequest{Key: ConfirmPublishKey, Text: "Ghi thay đổi này vào AGENTS.md?", Detail: rep.Diff}) {
			return rep, nil
		}
	}
	manifest, err := fsafe.LoadManifest(ws.Paths.Manifest)
	if err != nil {
		return nil, err
	}
	ir := &InitReport{}
	if err := syncAgentDocs(ir, newRepoWriter(ws.Paths, ws.Config, "bint publish"), InitOptions{Force: opts.Force}, root, ws.Paths, manifest, ws.Config, inner, opts.Now); err != nil {
		return nil, err
	}
	if err := manifest.Save(); err != nil {
		return nil, err
	}
	rep.Steps, rep.Written = ir.Steps, true
	return rep, nil
}

func claudeImportOK(root string, cfg config.Config) bool {
	if !slices.Contains(cfg.Harnesses.Enabled, claude.HarnessName) {
		return true
	}
	b, _ := fsafe.ReadBlock(filepath.Join(root, "CLAUDE.md"), fsafe.HTMLMarkers)
	return b != nil && strings.TrimSpace(*b) == "@AGENTS.md"
}

// syncAgentDocs ghi khối Bint vào AGENTS.md và nhập lại AGENTS.md trong CLAUDE.md.
func syncAgentDocs(rep *InitReport, w *repoWriter, opts InitOptions, root string, paths state.Paths, m *fsafe.Manifest, cfg config.Config, inner string, now time.Time) error {
	r, err := w.SyncBlock(fsafe.BlockSync{
		Path: filepath.Join(root, "AGENTS.md"), RelName: "AGENTS.md", Markers: fsafe.HTMLMarkers,
		Inner: inner, Force: opts.Force, BackupDir: paths.Backups, Manifest: m, Now: now,
	})
	if err != nil {
		return err
	}
	rep.addSync("AGENTS.md", r)
	if !slices.Contains(cfg.Harnesses.Enabled, claude.HarnessName) {
		return nil
	}
	return syncClaudeImport(rep, w, opts, root, paths, m, now)
}

// UnifiedDiff trả về bản so sánh dòng đơn giản (LCS) giữa a và b; rỗng nếu giống nhau.
func UnifiedDiff(nameA, nameB, a, b string) string {
	la, lb := splitLines(a), splitLines(b)
	if slices.Equal(la, lb) {
		return ""
	}
	n, m := len(la), len(lb)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if la[i] == lb[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", nameA, nameB)
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && la[i] == lb[j]:
			sb.WriteString("  " + la[i] + "\n")
			i++
			j++
		case j < m && (i == n || dp[i][j+1] >= dp[i+1][j]):
			sb.WriteString("+ " + lb[j] + "\n")
			j++
		default:
			sb.WriteString("- " + la[i] + "\n")
			i++
		}
	}
	return sb.String()
}

func splitLines(s string) []string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
