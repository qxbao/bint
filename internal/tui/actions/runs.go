package actions

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"bint/internal/app"
	"bint/internal/app/evolvesvc"
)

// Hành động của màn Lượt chạy (giai đoạn 3): `runs list/show/rate/scan`, `events ingest` và thẻ Bài học
// (`experience show/digest`). Ở chế độ chỉ đọc (Env.ReadOnly) việc mở không nạp spool vào cơ sở dữ liệu.

// openRuns mở không gian lượt chạy; nạp spool trừ khi TUI ở chế độ chỉ đọc.
func openRuns(ctx context.Context, env Env) (*app.RunsWS, error) {
	return app.OpenRuns(ctx, env.Dir, !env.ReadOnly)
}

func withRuns(id string, f func(ctx context.Context, r *app.RunsWS, opts any) (any, error)) DoFunc {
	return func(ctx context.Context, env Env, opts any) tea.Msg {
		r, err := openRuns(ctx, env)
		if err != nil {
			return res(id, opts, nil, err)
		}
		defer r.Close()
		v, err := f(ctx, r, opts)
		return res(id, opts, v, err)
	}
}

// withLessons mở không gian tiến hoá (chỉ để đọc thư viện bài học) rồi chạy f.
func withLessons(id string, f func(ctx context.Context, e *evolvesvc.Workspace, opts any) (any, error)) DoFunc {
	return func(ctx context.Context, env Env, opts any) tea.Msg {
		e, err := evolvesvc.OpenIngest(ctx, env.Dir, !env.ReadOnly)
		if err != nil {
			return res(id, opts, nil, err)
		}
		defer e.Close()
		v, err := f(ctx, e, opts)
		return res(id, opts, v, err)
	}
}

// runsActions là các hành động của màn Lượt chạy.
func runsActions() []Action {
	mk := func(id, name, key string, footer bool, prio int, cli string, o any, writes, human bool, do DoFunc) Action {
		return Action{ID: id, Screen: Runs, Key: key, Footer: footer, Priority: prio, LabelKey: "action.runs." + name + ".label",
			TitleKey: "action.runs." + name + ".title", CLIPath: cli, Options: o, Writes: writes, HumanOnly: human, Do: do}
	}
	return []Action{
		mk("runs.rate", "rate", "+/-", true, 1, "runs rate", app.RunsRateOptions{}, true, true,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				// Ghi sự kiện vào spool rồi nạp: dịch vụ tự mở workspace (kiểm chỉ-dành-cho-người nằm ở đó).
				v, err := app.RateRun(ctx, env.Dir, opts.(app.RunsRateOptions))
				return res("runs.rate", opts, v, err)
			}),
		mk("runs.scan", "scan", "S", true, 2, "runs scan", app.RunsScanOptions{}, true, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				r, err := app.OpenRuns(ctx, env.Dir, true) // quét ghi tín hiệu: luôn nạp spool như CLI
				if err != nil {
					return res("runs.scan", opts, nil, err)
				}
				defer r.Close()
				v, err := app.ScanRuns(ctx, r, opts.(app.RunsScanOptions))
				return res("runs.scan", opts, v, err)
			}),
		mk("runs.ingest", "ingest", "I", true, 3, "events ingest", app.EventsIngestOptions{}, true, false,
			func(ctx context.Context, env Env, opts any) tea.Msg {
				r, err := app.OpenRuns(ctx, env.Dir, true)
				if err != nil {
					return res("runs.ingest", opts, nil, err)
				}
				defer r.Close()
				v, err := app.IngestEvents(ctx, r, opts.(app.EventsIngestOptions))
				return res("runs.ingest", opts, v, err)
			}),
		mk("runs.list", "list", "l", false, 6, "runs list", app.RunsListOptions{}, false, false,
			withRuns("runs.list", func(ctx context.Context, r *app.RunsWS, o any) (any, error) {
				return app.ListRuns(ctx, r, o.(app.RunsListOptions))
			})),
		mk("runs.show", "show", "Enter", true, 4, "runs show", app.RunsShowOptions{}, false, false,
			withRuns("runs.show", func(ctx context.Context, r *app.RunsWS, o any) (any, error) {
				return app.ShowRun(ctx, r, o.(app.RunsShowOptions))
			})),
		mk("runs.lesson_show", "lesson_show", "", false, 0, "experience show", evolvesvc.ExperienceShowOptions{}, false, false,
			withLessons("runs.lesson_show", func(ctx context.Context, e *evolvesvc.Workspace, o any) (any, error) {
				return evolvesvc.Experience(ctx, e, o.(evolvesvc.ExperienceShowOptions))
			})),
		mk("runs.lesson_digest", "lesson_digest", "d", false, 5, "experience digest", evolvesvc.ExperienceDigestOptions{}, false, false,
			withLessons("runs.lesson_digest", func(ctx context.Context, e *evolvesvc.Workspace, o any) (any, error) {
				return evolvesvc.Digest(ctx, e, o.(evolvesvc.ExperienceDigestOptions))
			})),
		mk("runs.filter", "filter", "/", true, 7, "", nil, false, false, nil),
		mk("runs.refresh", "refresh", "ctrl+r", false, 9, "", nil, false, false, nil),
		mk("runs.next_view", "next_view", ".", false, 9, "", nil, false, false, nil),
		mk("runs.prev_view", "prev_view", ",", false, 9, "", nil, false, false, nil),
	}
}
