package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/eval"
	"bint/internal/fsafe"
	"bint/internal/harness"
	"bint/internal/skills"
	"bint/internal/ulid"
)

// ConfirmSkillTestKey là khoá i18n của lời hỏi "chạy kiểm thử skill?".
const ConfirmSkillTestKey = "skill.test.confirm"

// SkillTestDir là thư mục kết quả `bint skill test` trong thư mục trạng thái (<runtime>/skill-tests).
const SkillTestDir = "skill-tests"

// Giá trị của --arm.
const (
	SkillTestArmBoth = "both"
)

// ErrSkillTestQuota: đợt dừng vì harness báo hết hạn mức; kết quả dở vẫn được ghi và trả kèm lỗi này.
var ErrSkillTestQuota = errors.New("dừng vì hết hạn mức sử dụng của harness (kết quả một phần đã ghi)")

// SkillTestOptions: `bint skill test <tên>`.
type SkillTestOptions struct {
	Name string `arg:"name" form:"required,suggest=custom_skills"`
	// Arm: both (mặc định) | without | with.
	Arm string `flag:"arm" form:"choices=both|without|with"`
	// Reps > 0 ghi đè reps của mọi kịch bản (1–20); 0 = theo từng kịch bản.
	Reps         int      `flag:"reps" form:"advanced"`
	Harness      string   `flag:"harness" form:"advanced,suggest=harnesses"`
	Model        string   `flag:"model" form:"advanced"`
	AgentCommand string   `flag:"agent-command" form:"advanced"`
	CostPerRun   *float64 `flag:"cost-per-run" form:"advanced"`
	Yes          bool     `flag:"yes" form:"hidden"`

	// Confirm hỏi "chạy?" (nil và không Yes: lỗi dùng sai "cần --yes khi chạy không tương tác"); agent không được gọi
	// trước khi lời hỏi được đồng ý.
	Confirm ConfirmFunc
	// OnEstimate nhận ước tính ngay trước lời hỏi xác nhận (kể cả khi có Yes), tức trước mọi lần gọi agent.
	OnEstimate func(SkillTestEstimate)
	// Progress báo từng lượt đã xong (n tính từ 1 trên tổng total).
	Progress func(r eval.SkillTestRun, n, total int)
	// Agent: dùng sẵn (kiểm thử); nil thì dựng từ AgentCommand hoặc harness của role skill-tester.
	Agent eval.Agent
}

// SkillTestDefaults: mặc định (cả hai nhánh, reps theo kịch bản).
func SkillTestDefaults(config.Config) SkillTestOptions {
	return SkillTestOptions{Arm: SkillTestArmBoth}
}

// Validate kiểm ràng buộc mức giá trị.
func (o SkillTestOptions) Validate() error {
	switch {
	case o.Name == "":
		return Usagef("cần tên skill")
	case o.Arm != "" && o.Arm != SkillTestArmBoth && o.Arm != eval.SkillArmWith && o.Arm != eval.SkillArmWithout:
		return Usagef("--arm phải là both | without | with")
	case o.Reps < 0 || o.Reps > eval.SkillTestMaxReps:
		return Usagef("--reps phải trong khoảng 1–%d", eval.SkillTestMaxReps)
	case o.AgentCommand != "" && o.Harness != "":
		return Usagef("--agent-command không đi cùng --harness")
	case o.CostPerRun != nil && *o.CostPerRun < 0:
		return Usagef("--cost-per-run không được âm")
	}
	return nil
}

func (o SkillTestOptions) arms() []string {
	switch o.Arm {
	case eval.SkillArmWith:
		return []string{eval.SkillArmWith}
	case eval.SkillArmWithout:
		return []string{eval.SkillArmWithout}
	}
	return []string{eval.SkillArmWithout, eval.SkillArmWith}
}

// SkillTestEstimate là ước tính in trước khi gọi agent.
type SkillTestEstimate struct {
	Skill     string
	Harness   string
	Model     string
	Arms      []string
	Scenarios int
	Runs      int
	// CostPerRun/Total nil = chưa biết (không có --cost-per-run, chưa có kết quả trước và eval.prior_run_usd = 0).
	CostPerRun *float64
	CostSource string
	Total      *float64
	// ReportsCost: harness có báo chi phí không (không thì chi phí thật không đo được).
	ReportsCost bool
	Timeout     time.Duration
}

// SkillTestResult là kết quả một lần chạy.
type SkillTestResult struct {
	Report eval.SkillTestReport
	// Path là tệp JSON; LogDir là thư mục log từng lượt (cạnh tệp JSON, cùng id).
	Path, LogDir string
}

// skillTesterHarness chọn harness cho role skill-tester (gốc planner): --harness, roles.skill-tester, roles.planner,
// harnesses.default.
func skillTesterHarness(cfg config.Config, requested string) (h, src string, err error) {
	return harness.ForRole(cfg, harness.Selection{All: requested}, config.RoleSkillTester, "planner")
}

// skillTesterModel chọn model: --model, harnesses.<h>.models.skill-tester, models.planner, "" (mặc định của harness).
func skillTesterModel(cfg config.Config, h, cli string) string {
	return harness.RoleModelFor(cfg, h, config.RoleSkillTester, "planner", cli)
}

// loadCustomSkillOrUsage nạp skill tuỳ biến theo tên; skill gốc/tiến hoá/không có → lỗi cách dùng nêu rõ.
func loadCustomSkillOrUsage(root, name string) (*skills.CustomSkill, error) {
	cs, err := skills.LoadCustomOne(root, name)
	if err == nil {
		return cs, nil
	}
	if _, core := skills.Get(name); core {
		return nil, Usagef("%q là skill gốc: lệnh này chỉ dùng cho skill tuỳ biến", name)
	}
	if _, err := skills.LoadEvolvedOne(root, name); err == nil {
		return nil, Usagef("%q là skill tiến hoá: lệnh này chỉ dùng cho skill tuỳ biến (kiểm bằng `bint skill check`)", name)
	}
	return nil, Usagef("không có skill tuỳ biến %q (tạo bằng `bint skill new`)", name)
}

// SkillTest chạy kịch bản áp lực của một skill tuỳ biến ở các nhánh được chọn, chấm và ghi JSON vào
// <runtime>/skill-tests/<tên>/<id>.json. Trình tự: nạp và kiểm kịch bản (lỗi thì dừng, chưa tính gì) → ước tính (OnEstimate)
// → xác nhận (ConfirmOrFail) → chạy tuần tự. Huỷ giữa chừng và hết hạn mức vẫn ghi phần đã chạy (trạng thái
// cancelled / quota); lỗi huỷ (context.Canceled) và ErrSkillTestQuota trả kèm kết quả.
func SkillTest(ctx context.Context, dir string, o SkillTestOptions) (*SkillTestResult, error) {
	if o.Arm == "" {
		o.Arm = SkillTestArmBoth
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}
	k, err := OpenKnowledge(ctx, dir)
	if err != nil {
		return nil, err
	}
	cs, err := loadCustomSkillOrUsage(k.Root, o.Name)
	if err != nil {
		return nil, err
	}
	scenarios, err := eval.LoadSkillScenarios(filepath.Join(cs.Dir, "tests"))
	if err != nil {
		return nil, Usagef("kịch bản của skill %s: %v", o.Name, err)
	}
	arms := o.arms()
	if slices.Contains(arms, eval.SkillArmWith) {
		if errs, _ := skills.CheckCustom(k.Root, cs); len(errs) > 0 {
			return nil, Usagef("skill %s có lỗi lint nên nhánh with không chạy được (sửa theo `bint skill lint %s`, hoặc dùng --arm without):\n  %s",
				o.Name, o.Name, strings.Join(errs, "\n  "))
		}
	}

	cfg := k.Config
	agent, harnessName, model, err := o.buildAgent(cfg)
	if err != nil {
		return nil, err
	}

	reps := func(sc eval.SkillScenario) int {
		if o.Reps > 0 {
			return o.Reps
		}
		return sc.Reps
	}
	est := SkillTestEstimate{Skill: o.Name, Harness: harnessName, Model: model, Arms: arms, Scenarios: len(scenarios),
		ReportsCost: agent.ReportsCost(), Timeout: time.Duration(cfg.Eval.TimeoutMinutes) * time.Minute}
	for _, sc := range scenarios {
		est.Runs += reps(sc) * len(arms)
	}
	est.CostPerRun, est.CostSource = skillTestCostPerRun(k.Paths.Runtime, o.Name, harnessName, model, o.CostPerRun, cfg.Eval.PriorRunUSD)
	if est.CostPerRun != nil {
		t := *est.CostPerRun * float64(est.Runs)
		est.Total = &t
	}
	if o.OnEstimate != nil {
		o.OnEstimate(est)
	}
	text := fmt.Sprintf("Chạy %d lượt agent (skill %s, %s)?", est.Runs, o.Name, strings.Join(arms, "+"))
	if err := ConfirmOrFail(o.Yes, o.Confirm, ConfirmRequest{Key: ConfirmSkillTestKey, Text: text}); err != nil {
		return nil, err
	}

	id := ulid.New()
	base := filepath.Join(k.Paths.Runtime, SkillTestDir, o.Name)
	res := &SkillTestResult{Path: filepath.Join(base, id+".json"), LogDir: filepath.Join(base, id)}
	sum := sha256.Sum256(cs.Content)
	rep := eval.SkillTestReport{ID: id, Skill: o.Name, SkillHash: hex.EncodeToString(sum[:]), Harness: harnessName, Model: model,
		Arms: arms, Status: eval.SkillTestCompleted, StartedAt: time.Now().UTC().Format(time.RFC3339)}
	skillBody := string(cs.Content)

	total, n := est.Runs, 0
run:
	for _, sc := range scenarios {
		for rep1 := 1; rep1 <= reps(sc); rep1++ {
			for _, arm := range arms {
				if ctx.Err() != nil {
					rep.Status = eval.SkillTestCancelled
					break run
				}
				body := ""
				if arm == eval.SkillArmWith {
					body = skillBody
				}
				run, quota := runSkillTestOnce(ctx, agent, cfg, cs, sc, arm, rep1, eval.SkillTestPrompt(sc, o.Name, body), res.LogDir)
				if ctx.Err() != nil {
					// Lượt bị cắt bởi huỷ: không phải kết quả của agent, bỏ đi.
					rep.Status = eval.SkillTestCancelled
					break run
				}
				rep.Runs = append(rep.Runs, run)
				n++
				if o.Progress != nil {
					o.Progress(run, n, total)
				}
				if quota {
					rep.Status = eval.SkillTestQuota
					break run
				}
			}
		}
	}
	rep.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	rep.Tally()
	res.Report = rep
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return res, err
	}
	if err := fsafe.WriteAtomic(res.Path, append(b, '\n'), 0o644); err != nil {
		return res, fmt.Errorf("ghi kết quả %s: %w", res.Path, err)
	}
	if rep.Status == eval.SkillTestCancelled {
		return res, fmt.Errorf("%w", context.Canceled)
	}
	if rep.Status == eval.SkillTestQuota {
		return res, ErrSkillTestQuota
	}
	return res, nil
}

// buildAgent dựng agent và cho biết harness/model hiệu lực. Chỉ dựng (không gọi mô hình): harness thiếu chương trình
// báo lỗi ở đây, trước lời hỏi xác nhận chi phí. model là model agent thật sự dùng: model chọn (--model,
// models.skill-tester/planner) nếu có, không thì model chung của harness (harnesses.<h>.model, workflow.agent.model)
// do agent báo qua eval.ModelNamer; rỗng chỉ khi chính CLI chọn mặc định. Báo cáo, ước tính và khoá lịch sử chi phí
// dùng model này (không thì lượt đo bằng model chung bị ghi model rỗng và lẫn với lượt dùng model khác).
func (o SkillTestOptions) buildAgent(cfg config.Config) (agent eval.Agent, harnessName, model string, err error) {
	switch {
	case o.Agent != nil:
		return o.Agent, o.Agent.Name(), effectiveModel(o.Agent, o.Model), nil
	case o.AgentCommand != "":
		return &eval.CommandAgent{Command: o.AgentCommand}, "command", o.Model, nil
	}
	h, _, err := skillTesterHarness(cfg, o.Harness)
	if err != nil {
		return nil, "", "", Usagef("%v", err)
	}
	selected := skillTesterModel(cfg, h, o.Model)
	agent, err = harness.NewAgent(h, harness.AgentOptions{Config: cfg, ReadOnly: true, Model: selected,
		// Như role planner: model chọn rỗng (không --model, không models.skill-tester/planner) để adapter dùng model chung
		// của harness (harnesses.<h>.model / workflow.agent.model) hoặc mặc định của CLI.
		ModelSet: selected != "",
		MaxCost:  cfg.Limits.PerRun.MaxCostUSD})
	if err != nil {
		return nil, "", "", err
	}
	return agent, h, effectiveModel(agent, selected), nil
}

// effectiveModel: model chọn nếu có, không thì model agent báo là sẽ yêu cầu ("" nếu agent không báo).
func effectiveModel(a eval.Agent, selected string) string {
	if selected != "" {
		return selected
	}
	if mn, ok := a.(eval.ModelNamer); ok {
		return mn.RequestedModel()
	}
	return ""
}

// runSkillTestOnce chạy một lượt trong thư mục tạm rỗng riêng (nhánh with được chép thêm tệp phụ của skill, không có
// tests/), chỉ đọc, rồi chấm. quota = harness báo hết hạn mức: đợt phải dừng.
func runSkillTestOnce(ctx context.Context, agent eval.Agent, cfg config.Config, cs *skills.CustomSkill, sc eval.SkillScenario,
	arm string, rep int, prompt, logRoot string) (run eval.SkillTestRun, quota bool) {
	run = eval.SkillTestRun{Arm: arm, Scenario: sc.Name, Rep: rep}
	runLog := filepath.Join(logRoot, sc.Name, fmt.Sprintf("%s-%d", arm, rep))
	run.Log = filepath.ToSlash(filepath.Join(filepath.Base(logRoot), sc.Name, fmt.Sprintf("%s-%d", arm, rep)))
	work, err := os.MkdirTemp("", "bint-skilltest-*")
	if err != nil {
		run.Error = "không tạo được thư mục làm việc: " + err.Error()
		return run, false
	}
	defer os.RemoveAll(work)
	if arm == eval.SkillArmWith {
		if err := copySkillFiles(work, cs.Files); err != nil {
			run.Error = "không chép được tệp phụ của skill: " + err.Error()
			return run, false
		}
	}
	r := agent.Run(ctx, eval.AgentRequest{Dir: work, Prompt: prompt, ReadOnly: true, CaseID: sc.Name, Arm: arm,
		Timeout: time.Duration(cfg.Eval.TimeoutMinutes) * time.Minute, LogPath: filepath.Join(runLog, "agent.log")})
	run.CostUSD = r.CostUSD
	if r.Text != "" && os.MkdirAll(runLog, 0o700) == nil {
		os.WriteFile(filepath.Join(runLog, "answer.txt"), []byte(r.Text), 0o600)
	}
	switch {
	case r.QuotaHit:
		run.Error = firstNonEmpty(r.Infra, "hết hạn mức sử dụng")
		return run, true
	case r.Infra != "":
		run.Error = r.Infra
	case r.TimedOut:
		run.Error = "hết giờ (eval.timeout_minutes)"
	case r.Error != "":
		run.Error = r.Error
	case strings.TrimSpace(r.Text) == "":
		run.Error = "agent không trả câu trả lời (harness không báo trường result)"
	}
	if run.Error != "" {
		return run, false
	}
	ok, failed := eval.ScoreSkillAnswer(r.Text, sc.Expect)
	run.Complied, run.Failed = ok, failed
	return run, false
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// copySkillFiles chép tệp phụ của skill vào work (giữ cấu trúc thư mục con).
func copySkillFiles(work string, files []skills.CustomFile) error {
	for _, f := range files {
		p := filepath.Join(work, filepath.FromSlash(f.Rel))
		if !filepath.IsLocal(filepath.FromSlash(f.Rel)) {
			return fmt.Errorf("đường dẫn tệp phụ không hợp lệ: %s", f.Rel)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, f.Content, f.Mode); err != nil {
			return err
		}
	}
	return nil
}

// skillTestCostPerRun chọn chi phí mỗi lượt: --cost-per-run → trung bình đo được ở kết quả gần nhất của cùng skill,
// harness và model → eval.prior_run_usd (0 = tắt). nil = chưa biết.
func skillTestCostPerRun(runtimeDir, skill, harnessName, model string, flag *float64, prior float64) (*float64, string) {
	if flag != nil {
		return flag, "do người dùng nhập"
	}
	if avg, ok := lastSkillTestCost(runtimeDir, skill, harnessName, model); ok {
		return &avg, "trung bình đo ở lần chạy gần nhất của skill này"
	}
	if prior > 0 {
		return &prior, "eval.prior_run_usd (chưa có lịch sử)"
	}
	return nil, "chưa có lịch sử và eval.prior_run_usd = 0"
}

// lastSkillTestCost đọc kết quả mới nhất (id ULID tăng dần theo thời gian) của skill với đúng harness và model này có chi phí đo được.
func lastSkillTestCost(runtimeDir, skill, harnessName, model string) (float64, bool) {
	dir := filepath.Join(runtimeDir, SkillTestDir, skill)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, false
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	for i := len(names) - 1; i >= 0; i-- {
		b, err := os.ReadFile(filepath.Join(dir, names[i]))
		if err != nil {
			continue
		}
		var rep eval.SkillTestReport
		if json.Unmarshal(b, &rep) != nil || rep.Harness != harnessName || rep.Model != model {
			continue
		}
		var sum float64
		n := 0
		for _, r := range rep.Runs {
			if r.CostUSD != nil {
				sum += *r.CostUSD
				n++
			}
		}
		if n > 0 {
			return sum / float64(n), true
		}
	}
	return 0, false
}

// IsSkillTestCancelled cho biết lỗi là do huỷ giữa chừng (kết quả một phần đã được ghi).
func IsSkillTestCancelled(err error) bool { return errors.Is(err, context.Canceled) }
