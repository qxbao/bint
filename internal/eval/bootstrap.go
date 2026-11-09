package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/fsafe"
	"bint/internal/knowledge"
	"bint/internal/skills"
)

// BootstrapOptions điều khiển việc dựng ảnh chụp tri thức tại commit gốc.
type BootstrapOptions struct {
	Agent Agent
	// BintPath là tệp thực thi bint; thư mục của nó được thêm vào PATH của agent.
	BintPath    string
	BintVersion string
	Keep        bool
	Now         func() time.Time
}

// bootstrapPrompt bọc skill b-bootstrap thành đề bài cho agent không tương tác.
func bootstrapPrompt(s skills.Skill) string {
	var b strings.Builder
	b.WriteString("Bạn đang chạy skill b-bootstrap của Bint trên repository ở thư mục hiện tại, không có người tương tác.\n")
	b.WriteString("CLI `bint` có sẵn trong PATH. Làm đủ các bước dưới đây rồi dừng; không sửa mã nguồn.\n\n")
	b.WriteString(s.Body())
	b.WriteString("\n")
	return b.String()
}

// BootstrapSnapshot chạy b-bootstrap bằng agent trong một repository cô lập tại
// commit base (chỉ có lịch sử tới base, đã gỡ mọi tệp ngữ cảnh agent) và lưu các
// mục tri thức agent ghi được thành ảnh chụp cho nhánh minimal ở chế độ strict
// (mục 12.3). Ảnh chụp cũ của base bị thay thế.
func BootstrapSnapshot(ctx context.Context, env Env, base string, opts BootstrapOptions) (SnapshotMeta, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	start := opts.Now()
	skill, ok := skills.Get("b-bootstrap")
	if !ok {
		return SnapshotMeta{}, fmt.Errorf("thiếu skill b-bootstrap trong bản này")
	}
	meta := SnapshotMeta{Base: base, CreatedAt: nowString(start), Harness: opts.Agent.Name(),
		HarnessVersion: opts.Agent.Version(ctx), SkillSHA: skill.Hash(), BintVersion: opts.BintVersion}
	root := filepath.Join(env.WorkRoot, "bootstrap", base)
	work := filepath.Join(root, "work")
	stateDir := filepath.Join(root, "state")
	cfgHome := filepath.Join(root, "config-home")
	logDir := filepath.Join(env.WorkRoot, "logs", "bootstrap", base)
	meta.Log = filepath.Join(logDir, "agent.log")
	defer func() {
		if !opts.Keep {
			os.RemoveAll(root)
		}
	}()
	for _, d := range []string{stateDir, cfgHome, logDir} {
		os.RemoveAll(d)
		if err := os.MkdirAll(d, 0o700); err != nil {
			return meta, err
		}
	}

	if err := prepareWorkdir(ctx, env.RepoDir, env.WorkRoot, base, work); err != nil {
		return meta, fmt.Errorf("dựng thư mục làm việc: %w", err)
	}
	if _, err := stripContext(work); err != nil {
		return meta, err
	}
	// Cấu hình Bint mặc định để `bint knowledge add` chạy được trong thư mục tạm.
	cfg := config.Render(config.InitParams{ProjectName: filepath.Base(env.RepoDir), Harnesses: []string{"claude"}})
	if err := fsafe.WriteAtomic(filepath.Join(work, ".bint", "config.yml"), []byte(cfg), 0o644); err != nil {
		return meta, err
	}
	if _, err := gitCommitAll(ctx, work, "bint-eval: bootstrap"); err != nil {
		return meta, err
	}

	agentEnv := []string{
		"BINT_STATE_DIR=" + stateDir,
		"BINT_CONFIG_HOME=" + cfgHome,
		"BINT_AGENT=1",
	}
	if opts.BintPath != "" {
		agentEnv = append(agentEnv, "PATH="+filepath.Dir(opts.BintPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	ar := opts.Agent.Run(ctx, AgentRequest{
		Dir: work, Prompt: bootstrapPrompt(skill), Timeout: env.timeout(), LogPath: meta.Log,
		CaseID: "bootstrap-" + short(base), Arm: "bootstrap", Env: agentEnv,
	})
	meta.DurationMS = ar.Duration.Milliseconds()
	meta.AgentExitCode, meta.CostUSD, meta.Model, meta.Error = ar.ExitCode, ar.CostUSD, ar.Model, ar.Error
	if ar.TimedOut {
		meta.Error = strings.TrimSpace(meta.Error + " (quá thời gian)")
	}
	if ctx.Err() != nil {
		return meta, ctx.Err()
	}
	// Bootstrap thất bại không được ghi thành ảnh chụp rỗng: nhánh minimal khi đó sẽ
	// đo nhầm "bootstrap hỏng" như "bootstrap không tìm ra gì".
	if ar.ExitCode != 0 || ar.TimedOut || ar.Error != "" {
		return meta, fmt.Errorf("agent không hoàn thành bootstrap: %s (log: %s)", strings.TrimSpace(meta.Error+fmt.Sprintf(" mã thoát %d", ar.ExitCode)), meta.Log)
	}

	// Chỉ nhận mục hợp lệ do agent ghi (candidate, origin agent); mục khác bị bỏ.
	items, bad, err := knowledge.NewStore(work).List()
	if err != nil {
		return meta, err
	}
	snap := SnapshotDir(env.WorkRoot, base)
	tmp := snap + ".tmp"
	os.RemoveAll(tmp)
	store := knowledge.Store{Dir: filepath.Join(tmp, "items")}
	if err := os.MkdirAll(store.Dir, 0o700); err != nil {
		return meta, err
	}
	var dropped []string
	for _, b := range bad {
		dropped = append(dropped, b.Error())
	}
	for _, it := range items {
		if it.Origin.Kind != knowledge.OriginAgent || it.Status != knowledge.StatusCandidate {
			dropped = append(dropped, fmt.Sprintf("%s: origin %s/%s", it.ID, it.Origin.Kind, it.Status))
			continue
		}
		if err := store.Save(it); err != nil {
			dropped = append(dropped, err.Error())
			continue
		}
		meta.Items++
	}
	if len(dropped) > 0 {
		meta.Error = strings.TrimSpace(meta.Error + fmt.Sprintf(" bỏ %d mục: %s", len(dropped), strings.Join(dropped, "; ")))
	}
	data, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(tmp, "meta.json"), append(data, '\n'), 0o600); err != nil {
		return meta, err
	}
	os.RemoveAll(snap)
	if err := os.Rename(tmp, snap); err != nil {
		return meta, err
	}
	return meta, nil
}
