package evolvesvc

import (
	"context"
	"strings"
	"time"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/evolve"
)

// Dịch vụ bài học từ thất bại (`bint experience digest/show`, kế hoạch E): không gọi mô hình; rút bài học
// là việc của `bint evolve propose`.

// ExperienceDigestOptions: `bint experience digest <đợt|lượt>` (ID, tiền tố, `latest` cho đợt gần nhất).
type ExperienceDigestOptions struct {
	Ref string `arg:"ref" form:"required,suggest=batches"`
}

func ExperienceDigestDefaults(config.Config) ExperienceDigestOptions {
	return ExperienceDigestOptions{}
}

func (o ExperienceDigestOptions) Validate() error {
	if strings.TrimSpace(o.Ref) == "" {
		return app.Usagef("cần ID đợt hoặc lượt chạy (hoặc latest)")
	}
	return nil
}

// DigestResult là bản tóm tắt thất bại kèm ngưỡng số tác vụ để rút bài học (evolution.trigger).
type DigestResult struct {
	Report   evolve.DigestReport
	MinUnits int
}

// Digest dựng bản tóm tắt thất bại xác định cho một đợt đánh giá hoặc một lượt chạy.
func Digest(ctx context.Context, e *Workspace, o ExperienceDigestOptions) (*DigestResult, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	cases, err := e.Cases.Cases()
	if err != nil {
		return nil, err
	}
	items, _, err := e.K.Items()
	if err != nil {
		return nil, err
	}
	tr := e.K.Config.Evolution.Trigger
	minUnits, minNew := max(tr.MinSimilarRuns, 1), max(tr.MinNewEvidence, 1)
	rep, err := evolve.DigestRef(ctx, e.Store.DB, cases, o.Ref, e.K.Paths.Runtime, minUnits, minNew, evolve.LessonIDs(items))
	if err != nil {
		return nil, err
	}
	return &DigestResult{Report: rep, MinUnits: minUnits}, nil
}

// ExperienceShowOptions: `bint experience show` (không có tham số).
type ExperienceShowOptions struct{}

func ExperienceShowDefaults(config.Config) ExperienceShowOptions { return ExperienceShowOptions{} }
func (o ExperienceShowOptions) Validate() error                  { return nil }

// Experience trả thư viện bài học: chung trước, riêng nhóm theo vùng, kèm số lần chèn và đạt/trượt.
func Experience(ctx context.Context, e *Workspace, o ExperienceShowOptions) ([]evolve.LessonRow, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	items, _, err := e.K.Items()
	if err != nil {
		return nil, err
	}
	h, err := evolve.LoadHistory(ctx, e.Store.DB, time.Now(), 10*365*24*time.Hour)
	if err != nil {
		return nil, err
	}
	return evolve.ExperienceShow(ctx, e.Store.DB, items, h)
}
