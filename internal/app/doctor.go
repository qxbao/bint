package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/detect"
	"bint/internal/fsafe"
	"bint/internal/gitrepo"
	"bint/internal/harness"
	"bint/internal/knowledge"
	"bint/internal/runlog"
	"bint/internal/skills"
	"bint/internal/spool"
	"bint/internal/state"
	"bint/internal/store"
)

// Level là mức của một kết quả kiểm tra.
type Level string

const (
	LevelOK   Level = "ok"
	LevelInfo Level = "info"
	LevelWarn Level = "warn"
	LevelFail Level = "fail"
)

// Check là một kết quả của `bint doctor`.
type Check struct {
	Name   string
	Level  Level
	Detail string
}

// DoctorReport tổng hợp các kiểm tra.
type DoctorReport struct{ Checks []Check }

func (r *DoctorReport) add(name string, l Level, format string, a ...any) {
	r.Checks = append(r.Checks, Check{Name: name, Level: l, Detail: fmt.Sprintf(format, a...)})
}

// Failed cho biết có kiểm tra nào thất bại.
func (r *DoctorReport) Failed() bool {
	for _, c := range r.Checks {
		if c.Level == LevelFail {
			return true
		}
	}
	return false
}

// Doctor kiểm tra môi trường. Chỉ đọc, ngoại trừ việc áp migration còn thiếu khi
// mở cơ sở dữ liệu.
func Doctor(ctx context.Context, dir string) *DoctorReport {
	r := &DoctorReport{}

	gv, err := gitrepo.Version(ctx)
	if err != nil {
		r.add("git", LevelFail, "không chạy được git: %v", err)
		return r
	}
	if !gitAtLeast(gv, 2, 31) {
		r.add("git", LevelFail, "phiên bản %s; cần >= 2.31 (rev-parse --path-format)", gv)
		return r
	}
	r.add("git", LevelOK, "%s", gv)

	repo, err := gitrepo.Discover(ctx, dir)
	if err != nil {
		r.add("repository", LevelFail, "%v", err)
		return r
	}
	kind := "worktree chính"
	if repo.IsLinkedWorktree() {
		kind = "worktree phụ"
	}
	r.add("repository", LevelOK, "%s (%s)", repo.Toplevel, kind)

	if err := store.CheckFTS5(ctx); err != nil {
		r.add("sqlite fts5", LevelFail, "%v", err)
	} else {
		r.add("sqlite fts5", LevelOK, "khả dụng (modernc.org/sqlite, không cần CGO)")
	}

	cfg, src, err := config.LoadFirst(repo.ConfigRoots(), true)
	switch {
	case errors.Is(err, config.ErrNoProjectConfig):
		r.add("cấu hình", LevelFail, "%v", err)
		return r
	case err != nil:
		r.add("cấu hình", LevelFail, "%v", err)
		return r
	}
	layers := []string{src.Project}
	if src.Global != "" {
		layers = append([]string{src.Global}, layers...)
	}
	if src.Local != "" {
		layers = append(layers, src.Local)
	}
	r.add("cấu hình", LevelOK, "hợp lệ, schema v%d (%s)", cfg.Version, strings.Join(layers, " → "))
	if src.StrategyProblem != "" {
		r.add("strategy", LevelWarn, "strategy.yml (verified) bị bỏ qua ở phiên thật: %s", src.StrategyProblem)
	}

	paths, err := state.Resolve(repo, cfg)
	if err != nil {
		r.add("thư mục trạng thái", LevelFail, "%v", err)
		return r
	}
	if !paths.Initialized() {
		r.add("thư mục trạng thái", LevelFail, "%s chưa được tạo trên máy này (chạy `bint init`)", paths.Root)
		return r
	}
	if info, err := paths.ReadRepoInfo(); err != nil {
		r.add("thư mục trạng thái", LevelFail, "repo.json: %v", err)
	} else if state.NormalizePath(info.CommonDir) != state.NormalizePath(repo.CommonDir) {
		r.add("thư mục trạng thái", LevelFail, "thuộc repository khác: %s", info.CommonDir)
	} else if err := checkWritable(paths.Spool); err != nil {
		r.add("thư mục trạng thái", LevelFail, "spool không ghi được: %v", err)
	} else {
		r.add("thư mục trạng thái", LevelOK, "%s (dùng chung cho mọi worktree)", paths.Root)
	}

	s, err := store.Open(ctx, paths.DB)
	if err != nil {
		r.add("cơ sở dữ liệu", LevelFail, "%v", err)
	} else {
		v, _ := s.Version(ctx)
		mode, _ := s.JournalMode(ctx)
		lvl := LevelOK
		if mode != "wal" {
			lvl = LevelWarn
		}
		r.add("cơ sở dữ liệu", lvl, "schema v%d/%d, journal=%s", v, store.LatestVersion(), mode)
		stuck := 0
		s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE status = 'running' AND COALESCE(last_event_at, started_at) < ?`,
			store.FormatTime(time.Now().Add(-runlog.DefaultStaleAfter))).Scan(&stuck)
		if stuck > 0 {
			r.add("lượt chạy treo", LevelInfo, "%d lượt chạy sẽ được đánh dấu interrupted ở lần nạp tới", stuck)
		}
		s.Close()
	}

	pending, _ := spool.Pending(paths.Spool)
	switch {
	case len(pending) > 200:
		r.add("spool", LevelWarn, "%d tệp chờ nạp; chạy `bint events ingest`", len(pending))
	default:
		r.add("spool", LevelOK, "%d tệp chờ nạp", len(pending))
	}
	if n := spool.RejectedCount(paths.Spool); n > 0 {
		r.add("spool hỏng", LevelWarn, "%d tệp trong %s", n, filepath.Join(paths.Spool, "rejected"))
	}
	if fi, err := os.Stat(filepath.Join(paths.Root, HookErrorLog)); err == nil && fi.Size() > 0 {
		r.add("lỗi hook", LevelWarn, "xem %s", filepath.Join(paths.Root, HookErrorLog))
	}

	detected := map[string]detect.Harness{}
	for _, h := range detect.Harnesses() {
		detected[h.Name] = h
	}
	for _, h := range cfg.Harnesses.Enabled {
		d := detected[h]
		if !d.Present {
			r.add("harness "+h, LevelWarn, "không tìm thấy trên máy này")
			continue
		}
		where := d.Binary
		if where == "" {
			where = d.Home
		}
		ad, ok := harness.Get(h)
		if !ok {
			r.add("harness "+h, LevelOK, "%s", where)
			continue
		}
		info := harness.Describe(ad, cfg, nil)
		r.add("harness "+h, LevelOK, "%s — cấp %d (%s), xem `bint harness show %s`", where, info.Level, info.LevelTitle, h)
		rt := runtimeOf(ctx, ad, cfg, repo.Toplevel)
		if rt.Hook != nil {
			r.add("hook "+h, rt.Hook.Level, "%s", rt.Hook.Detail)
		}
		if rt.MCP != nil {
			if rt.MCP.Registered {
				r.add("MCP "+h, LevelOK, "đã đăng ký")
				if rt.MCP.CitationUnverified {
					r.add("MCP codex citation", LevelWarn, "chưa xác minh cite_knowledge được phép chạy với approval policy never; chạy bint init --mcp hoặc đặt [mcp_servers.bint.tools.cite_knowledge] approval_mode = \"approve\" trong .codex/config.toml")
				}
			} else {
				r.add("MCP "+h, LevelInfo, "chưa đăng ký MCP server của Bint (bint init --mcp)")
			}
		}
		if rt.CodexUntrusted {
			r.add("codex trust", LevelInfo, "repository chưa được Codex tin cậy: .codex/config.toml và .codex/hooks.json cấp dự án chưa có hiệu lực")
		}
	}

	if block, err := fsafe.ReadBlock(filepath.Join(repo.Toplevel, ".gitignore"), fsafe.HashMarkers); err != nil {
		r.add(".gitignore", LevelWarn, "%v", err)
	} else if block == nil {
		r.add(".gitignore", LevelWarn, "thiếu khối bint (chạy lại `bint init`)")
	} else {
		r.add(".gitignore", LevelOK, "có khối bint")
	}

	if cfg.Eval.TestCommand == nil {
		hint := ""
		if cfg.Eval.SuggestedTestCommand != nil {
			hint = fmt.Sprintf(" (đề xuất: %q)", *cfg.Eval.SuggestedTestCommand)
		}
		r.add("lệnh kiểm thử", LevelInfo, "eval.test_command chưa được xác nhận%s", hint)
	} else {
		r.add("lệnh kiểm thử", LevelOK, "%s", *cfg.Eval.TestCommand)
	}
	knowledgeChecks(r, filepath.Dir(filepath.Dir(src.Project)), cfg)
	return r
}

// knowledgeChecks kiểm tra tệp tri thức đọc được và skill gốc đã xuất bản còn
// nguyên bản (mục 6.6). Kiểm tra bằng chứng đầy đủ: `bint knowledge verify`.
func knowledgeChecks(r *DoctorReport, root string, cfg config.Config) {
	items, bad, err := knowledge.NewStore(root).List()
	switch {
	case err != nil:
		r.add("tri thức", LevelFail, "%v", err)
	case len(bad) > 0:
		r.add("tri thức", LevelFail, "%d tệp không đọc được, ví dụ %s", len(bad), bad[0].Error())
	default:
		counts := map[string]int{}
		for _, it := range items {
			counts[it.Status]++
		}
		lvl := LevelOK
		detail := fmt.Sprintf("%d mục: %d verified, %d candidate, %d stale", len(items), counts[knowledge.StatusVerified], counts[knowledge.StatusCandidate], counts[knowledge.StatusStale])
		if counts[knowledge.StatusCandidate]+counts[knowledge.StatusStale] > 0 {
			lvl, detail = LevelInfo, detail+" — duyệt: `bint knowledge review`"
		}
		r.add("tri thức", lvl, "%s", detail)
	}
	evolvedChecks(r, root)
	targets := harness.SkillTargets(cfg.Harnesses.Enabled)
	for _, core := range skills.Core() {
		sk, _, cerr := skills.Composed(root, core)
		if cerr != nil {
			r.add("skill "+core.Name, LevelWarn, "%v", cerr)
			continue
		}
		for _, t := range targets {
			who := strings.Join(harness.HarnessesForTarget(cfg.Harnesses.Enabled, t.Dir), ", ")
			rel := filepath.ToSlash(filepath.Join(t.Dir, sk.Name, "SKILL.md"))
			st, err := skills.Status(root, t, sk)
			switch {
			case err != nil:
				r.add("skill "+sk.Name, LevelWarn, "%v", err)
			case st == "missing":
				r.add("skill "+sk.Name, LevelInfo, "chưa xuất bản cho %s (bint sync)", who)
			case st == "differs":
				r.add("skill "+sk.Name, LevelWarn, "%s khác bản của Bint (bị sửa tay, lớp phủ đổi hoặc Bint đã nâng cấp): chạy `bint sync`", rel)
			default:
				r.add("skill "+sk.Name, LevelOK, "%s", rel)
			}
		}
	}
}

// evolvedChecks kiểm tra toàn vẹn skill tiến hoá (P7): xung đột merge giữa hai thay đổi cùng
// sửa một skill phải do người xử lý (mục 8.7).
func evolvedChecks(r *DoctorReport, root string) {
	list, bad, err := skills.LoadEvolved(root)
	if err != nil {
		r.add("skill tiến hoá", LevelFail, "%v", err)
		return
	}
	probs := skills.CheckEvolved(root)
	active := 0
	for _, e := range list {
		if e.Active() {
			active++
		}
	}
	if len(list) == 0 && len(bad) == 0 && len(probs) == 0 {
		return
	}
	for _, p := range probs {
		lvl := LevelWarn
		if p.Conflict {
			lvl = LevelFail
		}
		r.add("skill tiến hoá "+p.Skill, lvl, "%s", p.Detail)
	}
	if len(probs) == 0 {
		r.add("skill tiến hoá", LevelOK, "%d skill (%d đang hoạt động)", len(list), active)
	}
}

func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".doctor-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

func gitAtLeast(v string, major, minor int) bool {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return false
	}
	ma, err1 := strconv.Atoi(parts[0])
	mi, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return ma > major || (ma == major && mi >= minor)
}

// HookErrorLog là tệp (trong thư mục trạng thái) ghi lỗi của hook; hook không
// bao giờ làm gián đoạn agent nên lỗi chỉ được ghi lại ở đây.
const HookErrorLog = "hook-errors.log"
