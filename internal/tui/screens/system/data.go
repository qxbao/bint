// Package system là màn Hệ thống (docs/tui-plan.md mục 5.7, giai đoạn 5a): bảy thẻ con — Cấu hình, Harness, Thành phần
// quy trình, Skill, Chỉ mục, Ngôn ngữ, Doctor (MCP có màn riêng: screens/mcp) — và hai thẻ vòng đời của giai đoạn 5b: Cài đặt repo, Cập nhật Bint. Mọi thao tác đọc gọi tầng dịch vụ (internal/app, evolvesvc); thao
// tác ghi đi qua actions.RequestMsg để mô hình gốc kiểm chế độ chỉ đọc/khoá theo sổ hành động (màn hình không tự kiểm).
package system

import (
	"context"

	"bint/internal/app"
	"bint/internal/harness"
	"bint/internal/tui/actions"
)

// Các thẻ con của màn hình.
const (
	TabConfig = iota
	TabHarness
	TabComponents
	TabSkills
	TabIndex
	TabLanguage
	TabDoctor
	TabSetup
	TabUpdate
	numTabs
)

// TabNames là tên (khoá i18n sy.tab.<tên>) của các thẻ con theo thứ tự.
var TabNames = [numTabs]string{"config", "harness", "components", "skills", "index", "language", "doctor", "setup", "update"}

// TabIndexOf trả chỉ số thẻ theo tên ("" hoặc lạ → -1).
func TabIndexOf(name string) int {
	for i, n := range TabNames {
		if n == name {
			return i
		}
	}
	return -1
}

// ComponentsData là danh sách thành phần quy trình kèm chi tiết (nội dung) của từng thành phần.
type ComponentsData struct {
	Details  []app.ComponentDetail
	Problems []string
	// Templates là các mẫu dựng sẵn (gợi ý của form thêm).
	Templates []string
}

// HarnessData là bảng harness (không gồm phiên bản: gọi chương trình ngoài nên lấy riêng bằng V).
type HarnessData struct {
	Infos []harness.Info
}

// Loaders gom các hàm nạp dữ liệu của từng thẻ (thay được trong kiểm thử; nil = nạp thật từ dịch vụ).
type Loaders struct {
	Config     func(ctx context.Context, dir string) (app.ConfigViewData, error)
	Harness    func(ctx context.Context, dir string) (HarnessData, error)
	Runtime    func(ctx context.Context, dir string) ([]app.HarnessStatus, error)
	Components func(ctx context.Context, dir string) (ComponentsData, error)
	Skills     func(ctx context.Context, dir string) (app.SkillList, error)
	Doctor     func(ctx context.Context, dir string) (*app.DoctorReport, error)
	// Init đọc trạng thái init (thẻ Cài đặt repo); UpdateInfo đọc bản đang chạy và mã nguồn (thẻ Cập nhật Bint).
	Init       func(ctx context.Context, dir string) (app.InitState, error)
	UpdateInfo func(ctx context.Context, dir, version string) (UpdateInfo, error)
}

func (l Loaders) withDefaults() Loaders {
	if l.Config == nil {
		l.Config = app.ConfigView
	}
	if l.Harness == nil {
		l.Harness = func(ctx context.Context, dir string) (HarnessData, error) {
			infos, err := app.ListHarnesses(ctx, dir, app.HarnessListOptions{NoVersion: true})
			return HarnessData{Infos: infos}, err
		}
	}
	if l.Runtime == nil {
		l.Runtime = app.HarnessRuntime
	}
	if l.Components == nil {
		l.Components = loadComponents
	}
	if l.Skills == nil {
		l.Skills = func(ctx context.Context, dir string) (app.SkillList, error) {
			k, err := app.OpenKnowledge(ctx, dir)
			if err != nil {
				return app.SkillList{}, err
			}
			return app.ListSkills(k, app.SkillListOptions{})
		}
	}
	if l.Doctor == nil {
		l.Doctor = func(ctx context.Context, dir string) (*app.DoctorReport, error) { return app.Doctor(ctx, dir), nil }
	}
	if l.Init == nil {
		l.Init = app.ReadInitState
	}
	if l.UpdateInfo == nil {
		l.UpdateInfo = loadUpdateInfo
	}
	return l
}

func loadComponents(ctx context.Context, dir string) (ComponentsData, error) {
	w, err := app.OpenComponents(ctx, dir)
	if err != nil {
		return ComponentsData{}, err
	}
	lst, err := app.ListComponents(w, app.ComponentListOptions{})
	if err != nil {
		return ComponentsData{}, err
	}
	d := ComponentsData{Problems: lst.Problems, Templates: app.ComponentTemplates()}
	for _, c := range w.Lib.Components {
		det, err := app.ShowComponent(w, app.ComponentShowOptions{ID: c.ID, Kind: c.Kind})
		if err != nil {
			return ComponentsData{}, err
		}
		d.Details = append(d.Details, det)
	}
	return d, nil
}

// LoadedMsg là kết quả nạp dữ liệu của một thẻ (Value có kiểu theo thẻ).
type LoadedMsg struct {
	Tab   int
	Value any
	Err   error
}

// RuntimeMsg là kết quả nạp trạng thái hook/MCP của các harness (thẻ Harness).
type RuntimeMsg struct {
	Status []app.HarnessStatus
	Err    error
}

// Target cho mô hình gốc biết các thông điệp này thuộc màn Hệ thống.
func (LoadedMsg) Target() actions.Screen  { return actions.System }
func (RuntimeMsg) Target() actions.Screen { return actions.System }
