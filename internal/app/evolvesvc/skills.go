package evolvesvc

import (
	"context"
	"strings"
	"time"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/evolve"
	"bint/internal/skills"
)

// Dịch vụ `skill check` và `skill regress` (cần gói evolve nên nằm ở đây; các lệnh skill còn lại ở app).

// SkillCheckOptions: `bint skill check <tên-skill | id-đề-xuất>`.
type SkillCheckOptions struct {
	Target string `arg:"target" form:"required,suggest=evolved_skills"`
}

func SkillCheckDefaults(config.Config) SkillCheckOptions { return SkillCheckOptions{} }

func (o SkillCheckOptions) Validate() error {
	if strings.TrimSpace(o.Target) == "" {
		return app.Usagef("cần tên skill tiến hoá hoặc ID đề xuất skill")
	}
	return nil
}

// SkillCheckResult là các báo cáo điều kiện kích hoạt; OK = mọi báo cáo đạt.
type SkillCheckResult struct {
	Reports []evolve.TriggerReport
	OK      bool
}

// SkillCheck kiểm tra xác định được (không gọi mô hình) điều kiện kích hoạt của một skill tiến hoá hoặc của
// các skill trong một đề xuất chưa áp dụng (Target chứa "ep_").
func SkillCheck(ctx context.Context, e *Workspace, o SkillCheckOptions) (SkillCheckResult, error) {
	if err := o.Validate(); err != nil {
		return SkillCheckResult{}, err
	}
	list, _, err := skills.LoadEvolved(e.K.Root)
	if err != nil {
		return SkillCheckResult{}, err
	}
	h, err := evolve.LoadHistory(ctx, e.Store.DB, time.Now(), 10*365*24*time.Hour)
	if err != nil {
		return SkillCheckResult{}, err
	}
	files := e.K.Source().Files()
	var reports []evolve.TriggerReport
	if strings.Contains(o.Target, "ep_") {
		p, err := evolve.GetProposal(ctx, e.Store.DB, o.Target)
		if err != nil {
			return SkillCheckResult{}, err
		}
		if !p.IsSkill() {
			return SkillCheckResult{}, errNotSkillProposal(p.ID)
		}
		var sup []string
		for _, r := range p.SupportingRuns {
			sup = append(sup, r.RunID)
		}
		for _, d := range evolve.DraftsOf(p) {
			reports = append(reports, evolve.CheckTriggerConditions(d.Name, d.Description, d.Triggers, files, sup, h, list))
		}
	} else {
		ev, err := skills.LoadEvolvedOne(e.K.Root, o.Target)
		if err != nil {
			return SkillCheckResult{}, err
		}
		reports = append(reports, evolve.CheckTriggerConditions(ev.Meta.Name, ev.Doc().Description, ev.Meta.Triggers, files, ev.Meta.Origin.Runs, h, list))
	}
	res := SkillCheckResult{Reports: reports, OK: true}
	for _, r := range reports {
		res.OK = res.OK && r.OK
	}
	return res, nil
}

type errNotSkillProposal string

func (e errNotSkillProposal) Error() string {
	return "đề xuất " + string(e) + " không phải đề xuất skill"
}

// SkillRegressOptions: `bint skill regress [--model <mô hình>]`.
type SkillRegressOptions struct {
	Model string `flag:"model"`
}

func SkillRegressDefaults(config.Config) SkillRegressOptions { return SkillRegressOptions{} }
func (o SkillRegressOptions) Validate() error                { return nil }

// SkillRegressResult cho biết skill nào chưa xác thực với mô hình hiện dùng.
type SkillRegressResult struct {
	// Model là mô hình hiện dùng (Options.Model hoặc mô hình của lượt chạy gần nhất).
	Model    string
	Statuses []evolve.SkillModelStatus
}

// SkillRegress đối chiếu mô hình đã kiểm chứng của từng skill tiến hoá đang hoạt động với mô hình hiện dùng.
func SkillRegress(ctx context.Context, e *Workspace, o SkillRegressOptions) (SkillRegressResult, error) {
	if err := o.Validate(); err != nil {
		return SkillRegressResult{}, err
	}
	list, _, err := skills.LoadEvolved(e.K.Root)
	if err != nil {
		return SkillRegressResult{}, err
	}
	cur := o.Model
	if cur == "" {
		cur = evolve.CurrentModel(ctx, e.Store.DB, nil)
	}
	sts, err := evolve.ModelStatuses(ctx, e.Store.DB, list, cur)
	if err != nil {
		return SkillRegressResult{}, err
	}
	if sts == nil {
		sts = []evolve.SkillModelStatus{}
	}
	return SkillRegressResult{Model: cur, Statuses: sts}, nil
}
