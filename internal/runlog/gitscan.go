package runlog

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"bint/internal/gitrepo"
	"bint/internal/store"
)

// ScanOptions điều khiển quét lịch sử Git để thu tín hiệu kết quả (mục 9.6).
type ScanOptions struct {
	// Lookback: chỉ xét lượt chạy bắt đầu trong khoảng này (mặc định 30 ngày).
	Lookback time.Duration
	// CommitWindow: commit trong khoảng này sau khi lượt kết thúc được coi là của lượt
	// (mặc định 24 giờ). Đây là quy tắc gần đúng: commit phải chạm tệp lượt đã sửa.
	CommitWindow time.Duration
	// RevertDays: hoàn tác trong N ngày sau commit là tín hiệu tiêu cực mạnh.
	RevertDays int
	Now        time.Time
}

// ScanResult tóm tắt một lần quét.
type ScanResult struct {
	Runs      int `json:"runs"`
	Committed int `json:"committed"`
	Reverted  int `json:"reverted"`
}

type gitRun struct {
	id, start, end, worktree string
	files                    []string
}

// ScanGit gắn commit với lượt chạy (commit chạm tệp lượt đã sửa, trong cửa sổ thời
// gian sau lượt) và phát hiện commit đó bị hoàn tác ("This reverts commit <sha>").
// Tín hiệu đến muộn (hoàn tác sau nhiều ngày) cập nhật lại nhãn ở lần quét sau.
func ScanGit(ctx context.Context, db *sql.DB, repoDir string, opts ScanOptions) (ScanResult, error) {
	var res ScanResult
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.Lookback <= 0 {
		opts.Lookback = 30 * 24 * time.Hour
	}
	if opts.CommitWindow <= 0 {
		opts.CommitWindow = 24 * time.Hour
	}
	if opts.RevertDays <= 0 {
		opts.RevertDays = 14
	}
	rows, err := db.QueryContext(ctx, `SELECT r.run_id, r.started_at, COALESCE(r.finished_at, r.last_event_at, r.started_at),
			COALESCE(r.worktree, '')
		FROM runs r WHERE r.started_at >= ? AND r.status IN ('completed','interrupted','failed')
		AND EXISTS (SELECT 1 FROM run_artifacts a WHERE a.run_id = r.run_id AND a.kind = 'file.modified')
		ORDER BY r.started_at DESC LIMIT 500`, store.FormatTime(opts.Now.Add(-opts.Lookback)))
	if err != nil {
		return res, err
	}
	var runs []gitRun
	for rows.Next() {
		var r gitRun
		if err := rows.Scan(&r.id, &r.start, &r.end, &r.worktree); err != nil {
			rows.Close()
			return res, err
		}
		runs = append(runs, r)
	}
	rows.Close()
	for i := range runs {
		fr, err := db.QueryContext(ctx, `SELECT path FROM run_artifacts WHERE run_id = ? AND kind = 'file.modified' ORDER BY path`, runs[i].id)
		if err != nil {
			return res, err
		}
		for fr.Next() {
			var p string
			fr.Scan(&p)
			runs[i].files = append(runs[i].files, p)
		}
		fr.Close()
	}
	res.Runs = len(runs)

	type commitInfo struct {
		run  string
		time time.Time
	}
	commits := map[string]commitInfo{}
	for _, r := range runs {
		start, err1 := store.ParseTime(r.start)
		end, err2 := store.ParseTime(r.end)
		if err1 != nil || err2 != nil {
			continue
		}
		dir := repoDir
		if r.worktree != "" {
			dir = r.worktree
		}
		args := []string{"log", "--all", "--no-merges", "--format=%H %ct",
			"--since=" + strconv.FormatInt(start.Unix(), 10), "--until=" + strconv.FormatInt(end.Add(opts.CommitWindow).Unix(), 10), "--"}
		args = append(args, r.files...)
		out, err := gitrepo.Run(ctx, dir, args...)
		if err != nil {
			// worktree đã bị xoá: thử từ repository chính.
			if out, err = gitrepo.Run(ctx, repoDir, args...); err != nil {
				continue
			}
		}
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			f := strings.Fields(line)
			if len(f) != 2 {
				continue
			}
			ts, _ := strconv.ParseInt(f[1], 10, 64)
			ct := time.Unix(ts, 0).UTC()
			commits[f[0]] = commitInfo{run: r.id, time: ct}
			err := UpsertSignal(ctx, db, Signal{RunID: r.id, Kind: SignalCommitted, Positive: true, Strength: "weak",
				Source: "git-scan", Detail: f[0], DedupKey: "committed:" + r.id + ":" + f[0], ObservedAt: store.FormatTime(ct)})
			if err != nil {
				return res, err
			}
			res.Committed++
		}
	}
	// Commit đã gắn từ các lần quét trước (lượt cũ hơn Lookback vẫn có thể bị hoàn tác).
	cr, err := db.QueryContext(ctx, `SELECT run_id, detail, observed_at FROM outcome_signals WHERE kind = 'committed'`)
	if err != nil {
		return res, err
	}
	for cr.Next() {
		var run, sha, at string
		cr.Scan(&run, &sha, &at)
		if _, ok := commits[sha]; !ok {
			t, _ := store.ParseTime(at)
			commits[sha] = commitInfo{run: run, time: t}
		}
	}
	cr.Close()
	if len(commits) == 0 {
		return res, nil
	}
	since := opts.Now.Add(-opts.Lookback - time.Duration(opts.RevertDays)*24*time.Hour)
	out, err := gitrepo.Run(ctx, repoDir, "log", "--all", "--format=%H %ct%n%B%x1e", "--since="+strconv.FormatInt(since.Unix(), 10))
	if err != nil {
		return res, fmt.Errorf("quét hoàn tác: %w", err)
	}
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimSpace(rec)
		head, body, _ := strings.Cut(rec, "\n")
		hf := strings.Fields(head)
		if len(hf) != 2 {
			continue
		}
		ts, _ := strconv.ParseInt(hf[1], 10, 64)
		rt := time.Unix(ts, 0).UTC()
		for _, m := range revertRe.FindAllStringSubmatch(body, -1) {
			for sha, ci := range commits {
				if !strings.HasPrefix(sha, strings.ToLower(m[1])) {
					continue
				}
				if rt.Sub(ci.time) > time.Duration(opts.RevertDays)*24*time.Hour {
					continue
				}
				err := UpsertSignal(ctx, db, Signal{RunID: ci.run, Kind: SignalReverted, Positive: false, Strength: "strong",
					Source: "git-scan", Detail: fmt.Sprintf("%s hoàn tác bởi %s", short(sha), short(hf[0])),
					DedupKey: "reverted:" + ci.run + ":" + sha, ObservedAt: store.FormatTime(rt)})
				if err != nil {
					return res, err
				}
				res.Reverted++
			}
		}
	}
	return res, nil
}

var revertRe = regexp.MustCompile(`(?i)This reverts commit ([0-9a-f]{7,40})`)

func short(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}
