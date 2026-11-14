package eval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"time"

	"bint/internal/config"
)

// Lý do bỏ qua riêng của đề dựng từ phiếu.
const (
	SkipNoSolution = "không dựng lại được lời giải (thiếu cây hoặc tệp của phiếu)"
)

// TaskSource là dữ liệu thuần của một phiếu b-do đã done mà eval cần để dựng ứng viên (gói eval không
// phụ thuộc gói task; internal/app đọc kho phiếu rồi dựng danh sách này).
type TaskSource struct {
	ID          string
	Goal        string
	Request     string
	Criteria    []string
	Requirement string // nội dung requirement.md (chỉ cho người duyệt)
	// VerifyCommand là verify.command của phiếu khi đã được người xác nhận; rỗng = dùng eval.test_command.
	VerifyCommand string
	Base          string   // commit gốc lúc lập phiếu
	BaseTree      string   // cây ngay trước b-feat đầu tiên ("" = như cây của Base)
	AfterTree     string   // cây có lời giải (lúc đưa về hoặc của lần kiểm chứng qua cuối cùng)
	Files         []string // tập tệp của phiếu (tương đối gốc repo)
	Runs          []string // mọi lượt chạy gắn với phiếu
	Author        string
	DoneAt        string // RFC3339
}

// TaskRefPrefix là tiền tố ref giữ sống commit tổng hợp của đề dựng từ phiếu.
const TaskRefPrefix = "refs/bint/eval/"

// MineTasks dựng ứng viên đánh giá từ các phiếu đã done (mục B2 của docs/next-directions.md). Lời giải
// của phiếu được đóng băng thành hai commit tổng hợp (gốc và lời giải) giữ bằng ref để mọi bước sau
// (kiểm chứng, rò rỉ, chấm) dùng nguyên đường ống của đề từ Git. Bộ lọc quy mô giống `Mine`.
func MineTasks(ctx context.Context, repoDir string, m config.EvalMine, st Store, tasks []TaskSource, opts MineOptions) (MineResult, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	res := MineResult{Skipped: map[string]int{}}
	known := map[string]bool{}
	for _, list := range []func() ([]*Case, error){st.Candidates, st.Cases} {
		cs, err := list()
		if err != nil {
			return res, err
		}
		for _, c := range cs {
			known[c.ID] = true
		}
	}
	keys, err := existingTestKeys(ctx, repoDir, st, func(c *Case) bool { return true })
	if err != nil {
		return res, err
	}
	cls := NewClassifier(m)
	for _, ts := range tasks {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		res.Scanned++
		if opts.Limit > 0 && len(res.New) >= opts.Limit {
			break
		}
		id := TaskCaseID(ts.ID)
		if known[id] {
			res.Skipped[SkipKnown]++
			continue
		}
		base, sol, ok, err := freezeSolution(ctx, repoDir, ts)
		if err != nil {
			return res, err
		}
		if !ok {
			res.Skipped[SkipNoSolution]++
			continue
		}
		out, err := git(ctx, repoDir, "diff", "--numstat", "--no-renames", base, sol)
		if err != nil {
			return res, err
		}
		c := &Case{
			Schema: CaseSchemaVersion,
			Status: StatusCandidate,
			Source: Source{Commit: sol, Base: base, Author: ts.Author, Date: ts.DoneAt,
				Subject: strings.TrimSpace(ts.Goal), Task: ts.ID, TaskRuns: slices.Clone(ts.Runs)},
			TestCommand: strings.TrimSpace(ts.VerifyCommand),
		}
		if reason := classifyChanges(c, parseNumstat(string(out)), cls, m); reason != "" {
			res.Skipped[reason]++
			continue
		}
		c.Prompt = taskPrompt(ts)
		c.OriginalMessage = taskOriginal(ts)
		if len([]rune(c.Prompt)) < m.MinMessageChars {
			res.Skipped[SkipShortMsg]++
			continue
		}
		key, err := testKey(ctx, repoDir, sol, c.TestFiles)
		if err != nil {
			return res, err
		}
		if keys[key] {
			res.Skipped[SkipDuplicate]++
			continue
		}
		for name, commit := range map[string]string{"base": base, "commit": sol} {
			if _, err := git(ctx, repoDir, "update-ref", TaskRefPrefix+id+"/"+name, commit); err != nil {
				return res, err
			}
		}
		c.ID = id
		c.MinedAt = nowString(opts.Now())
		in, err := BuildLeakInput(ctx, repoDir, c)
		if err != nil {
			return res, err
		}
		ApplySpec(ctx, repoDir, c, in)
		c.Leak = CheckLeak(c.Prompt, in)
		if err := st.SaveCandidate(c); err != nil {
			return res, err
		}
		known[id] = true
		keys[key] = true
		res.New = append(res.New, c)
	}
	return res, nil
}

// taskPrompt dựng đề bài từ yêu cầu (hoặc mục tiêu) và tiêu chí chấp nhận, làm sạch như DraftPrompt.
func taskPrompt(ts TaskSource) string {
	req := strings.TrimSpace(ts.Request)
	if req == "" {
		req = strings.TrimSpace(ts.Goal)
	}
	var b strings.Builder
	b.WriteString(DraftPrompt(req, ""))
	var crit []string
	for _, c := range ts.Criteria {
		if c = DraftPrompt(c, ""); c != "" {
			crit = append(crit, "- "+strings.ReplaceAll(c, "\n", " "))
		}
	}
	if len(crit) > 0 {
		b.WriteString("\n\nAcceptance criteria:\n")
		b.WriteString(strings.Join(crit, "\n"))
	}
	return strings.TrimSpace(b.String())
}

// taskOriginal là phần chỉ người duyệt thấy: yêu cầu, tiêu chí và requirement.md gốc.
func taskOriginal(ts TaskSource) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Phiếu %s\n\nYêu cầu:\n%s\n", ts.ID, strings.TrimSpace(strings.Join([]string{ts.Goal, ts.Request}, "\n")))
	if len(ts.Criteria) > 0 {
		b.WriteString("\nTiêu chí chấp nhận:\n")
		for i, c := range ts.Criteria {
			fmt.Fprintf(&b, "%d. %s\n", i+1, strings.TrimSpace(c))
		}
	}
	if r := strings.TrimSpace(ts.Requirement); r != "" {
		b.WriteString("\nrequirement.md:\n" + r + "\n")
	}
	return strings.TrimSpace(b.String())
}

// freezeSolution dựng commit gốc và commit lời giải tổng hợp (tất định); ok=false khi thiếu đối tượng.
func freezeSolution(ctx context.Context, repoDir string, ts TaskSource) (base, sol string, ok bool, err error) {
	if ts.Base == "" || ts.AfterTree == "" || len(ts.Files) == 0 {
		return "", "", false, nil
	}
	for _, spec := range []string{ts.Base + "^{commit}", ts.AfterTree + "^{tree}"} {
		if _, e := git(ctx, repoDir, "rev-parse", "--verify", "--quiet", spec); e != nil {
			return "", "", false, nil
		}
	}
	if ts.BaseTree != "" {
		if _, e := git(ctx, repoDir, "rev-parse", "--verify", "--quiet", ts.BaseTree+"^{tree}"); e != nil {
			return "", "", false, nil
		}
	}
	date, err := gitString(ctx, repoDir, "show", "-s", "--format=%cI", ts.Base)
	if err != nil {
		return "", "", false, err
	}
	env := []string{"GIT_AUTHOR_NAME=bint", "GIT_AUTHOR_EMAIL=bint@localhost", "GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_NAME=bint", "GIT_COMMITTER_EMAIL=bint@localhost", "GIT_COMMITTER_DATE=" + date}
	base = ts.Base
	baseTree, err := gitString(ctx, repoDir, "rev-parse", ts.Base+"^{tree}")
	if err != nil {
		return "", "", false, err
	}
	if ts.BaseTree != "" {
		want, err := gitString(ctx, repoDir, "rev-parse", ts.BaseTree+"^{tree}")
		if err != nil {
			return "", "", false, err
		}
		if want != baseTree {
			out, err := gitEnv(ctx, repoDir, env, nil, "commit-tree", want, "-p", ts.Base, "-m", "bint eval base "+ts.ID)
			if err != nil {
				return "", "", false, err
			}
			base = strings.TrimSpace(string(out))
		}
	}
	tmp, err := os.MkdirTemp("", "bint-eval-index-")
	if err != nil {
		return "", "", false, err
	}
	defer os.RemoveAll(tmp)
	ienv := append(slices.Clone(env), "GIT_INDEX_FILE="+tmp+string(os.PathSeparator)+"index")
	if _, err := gitEnv(ctx, repoDir, ienv, nil, "read-tree", base); err != nil {
		return "", "", false, err
	}
	files := slices.Clone(ts.Files)
	sort.Strings(files)
	var info bytes.Buffer
	var removed []string
	for _, f := range files {
		if err := checkRelPath(f); err != nil {
			return "", "", false, nil
		}
		out, err := gitEnv(ctx, repoDir, ienv, nil, "--literal-pathspecs", "ls-tree", "-z", ts.AfterTree, "--", f)
		if err != nil {
			return "", "", false, err
		}
		meta, path, found := strings.Cut(strings.TrimSuffix(string(out), "\x00"), "\t")
		fs := strings.Fields(meta)
		if !found || len(fs) != 3 || fs[1] != "blob" && fs[1] != "commit" {
			removed = append(removed, f)
			continue
		}
		info.WriteString(fs[0] + " " + fs[2] + "\t" + path + "\x00")
	}
	if info.Len() > 0 {
		if _, err := gitEnv(ctx, repoDir, ienv, info.Bytes(), "update-index", "-z", "--index-info"); err != nil {
			return "", "", false, err
		}
	}
	for _, f := range removed {
		if _, err := gitEnv(ctx, repoDir, ienv, nil, "update-index", "--force-remove", "--", f); err != nil {
			return "", "", false, err
		}
	}
	tree, err := gitEnv(ctx, repoDir, ienv, nil, "write-tree")
	if err != nil {
		return "", "", false, err
	}
	out, err := gitEnv(ctx, repoDir, env, nil, "commit-tree", strings.TrimSpace(string(tree)), "-p", base, "-m", "bint eval solution "+ts.ID)
	if err != nil {
		return "", "", false, err
	}
	return base, strings.TrimSpace(string(out)), true, nil
}

// gitEnv như git nhưng thêm biến môi trường và stdin cho đúng lệnh này (không đụng môi trường tiến trình).
func gitEnv(ctx context.Context, dir string, env []string, stdin []byte, args ...string) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
	}
	// Tùy chọn toàn cục (--literal-pathspecs) phải đứng trước lệnh con.
	var global []string
	for len(args) > 0 && strings.HasPrefix(args[0], "--") && args[0] != "--" {
		global = append(global, args[0])
		args = args[1:]
	}
	full := append(append([]string{"-C", dir, "-c", "core.quotePath=false", "-c", "commit.gpgsign=false"}, global...), args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = cleanEnv(append([]string{"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1"}, env...)...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// testKey băm tập (đường dẫn, blob) của các tệp kiểm thử tại commit: hai đề cùng khoá là cùng thay đổi kiểm thử.
func testKey(ctx context.Context, repoDir, commit string, testFiles []string) (string, error) {
	files := slices.Clone(testFiles)
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		out, err := git(ctx, repoDir, "--literal-pathspecs", "ls-tree", "-z", commit, "--", f)
		if err != nil {
			return "", err
		}
		meta, path, _ := strings.Cut(strings.TrimSuffix(string(out), "\x00"), "\t")
		fs := strings.Fields(meta)
		sha := "-" // tệp bị xoá tại commit
		if len(fs) == 3 {
			sha = fs[2]
		}
		h.Write([]byte(f + "\x00" + sha + "\x00"))
		_ = path
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// existingTestKeys tính khoá kiểm thử của các đề/ứng viên đã có thoả keep; đề mà commit không đọc được
// (kho nông, đã dọn) thì bỏ qua.
func existingTestKeys(ctx context.Context, repoDir string, st Store, keep func(*Case) bool) (map[string]bool, error) {
	keys := map[string]bool{}
	for _, list := range []func() ([]*Case, error){st.Candidates, st.Cases} {
		cs, err := list()
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			if !keep(c) || len(c.TestFiles) == 0 {
				continue
			}
			if k, err := testKey(ctx, repoDir, c.Source.Commit, c.TestFiles); err == nil {
				keys[k] = true
			}
		}
	}
	return keys, nil
}
