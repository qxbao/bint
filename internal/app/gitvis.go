package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"bint/internal/fsafe"
	"bint/internal/gitrepo"
	"bint/internal/harness"
	"bint/internal/skills"
	"bint/internal/state"
)

// Cách Bint xuất hiện trong Git của repository (bint init --git).
const (
	// GitTrack: tệp của Bint được commit cho cả nhóm; tệp riêng của máy vào .gitignore.
	GitTrack = "track"
	// GitLocal: Bint chỉ dùng trên máy này: tệp chưa được theo dõi vào .git/info/exclude,
	// tệp đã được theo dõi mà Bint sửa được đặt skip-worktree; .gitignore không bị đụng.
	GitLocal = "local"
)

// GitModes là các giá trị hợp lệ của --git.
var GitModes = []string{GitTrack, GitLocal}

// gitModeFile lưu lựa chọn --git của máy này (ngoài repository).
func gitModeFile(p state.Paths) string { return filepath.Join(p.Root, "git-mode") }

// LoadGitMode đọc lựa chọn đã lưu ("" = chưa chọn).
func LoadGitMode(p state.Paths) string {
	b, err := os.ReadFile(gitModeFile(p))
	if err != nil {
		return ""
	}
	m := strings.TrimSpace(string(b))
	if !slices.Contains(GitModes, m) {
		return ""
	}
	return m
}

func saveGitMode(p state.Paths, mode string) error {
	return fsafe.WriteAtomic(gitModeFile(p), []byte(mode+"\n"), 0o600)
}

// excludeRel là tên dùng cho sao lưu/manifest của .git/info/exclude.
const excludeRel = ".git/info/exclude"

// excludePath trả về đường dẫn tệp exclude của repository (dùng chung mọi worktree).
func excludePath(ctx context.Context, root string) (string, error) {
	out, err := gitrepo.Run(ctx, root, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return "", err
	}
	p := strings.TrimSpace(out)
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	return filepath.Clean(p), nil
}

// bintPaths liệt kê đường dẫn (tương đối, dạng /, thư mục kết thúc bằng /) của Bint
// trong worktree: thư mục .bint, skill đã xuất bản, tệp hook trong repository, và các
// tệp dùng chung có khối/mục của Bint (MCP, AGENTS.md, CLAUDE.md).
func bintPaths(root string, enabled []string, hookFiles []string) []string {
	set := map[string]bool{".bint/": true}
	for _, t := range harness.SkillTargets(enabled) {
		for _, s := range skills.Core() {
			set[t.Dir+"/"+s.Name+"/"] = true
		}
	}
	if slices.Contains(enabled, "claude") {
		// Định nghĩa subagent: từng tệp (thư mục .claude/agents còn chứa subagent của người dùng nên không loại cả thư mục).
		for _, role := range harness.Roles {
			set[harness.SubagentDir+"/"+harness.SubagentName(role)+".md"] = true
		}
	}
	if l, err := skills.ReadLock(root); err == nil {
		for _, e := range l.Agents {
			for _, p := range e.Published {
				set[filepath.ToSlash(p)] = true
			}
		}
		for _, m := range []map[string]skills.LockEntry{l.Skills, l.Evolved} {
			for _, e := range m {
				for _, p := range e.Published {
					set[filepath.ToSlash(filepath.Dir(filepath.FromSlash(p)))+"/"] = true
				}
			}
		}
		// Skill tuỳ biến: cả thư mục {target}/{tên}/ (tệp phụ có thể nằm ở thư mục con nên không lấy theo Dir của từng tệp).
		for name := range l.Custom {
			for _, t := range harness.SkillTargets(enabled) {
				set[t.Dir+"/"+name+"/"] = true
			}
		}
	}
	for _, h := range hookFiles {
		set[h] = true
	}
	marked := map[string]string{".mcp.json": "bint", ".codex/config.toml": "bint:begin", "AGENTS.md": "bint:begin", "CLAUDE.md": "bint:begin"}
	for f, marker := range marked {
		if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f))); err == nil && strings.Contains(string(b), marker) {
			set[f] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// GitVisibility là kết quả áp chế độ --git.
type GitVisibility struct {
	ExcludePath  string
	Patterns     []string
	SkipWorktree []string // tệp đã theo dõi vừa được đặt (local) hoặc gỡ (track) skip-worktree
	Exclude      fsafe.Result
}

// applyGitLocal: ghi khối exclude và đặt skip-worktree cho tệp đã được theo dõi.
func applyGitLocal(ctx context.Context, w *repoWriter, root string, paths []string, p state.Paths, m *fsafe.Manifest, force bool, now time.Time) (GitVisibility, error) {
	var v GitVisibility
	ex, err := excludePath(ctx, root)
	if err != nil {
		return v, err
	}
	v.ExcludePath = ex
	for _, x := range paths {
		v.Patterns = append(v.Patterns, "/"+x)
	}
	if v.Exclude, err = w.SyncBlock(fsafe.BlockSync{Path: ex, RelName: excludeRel, Markers: fsafe.HashMarkers,
		Inner: strings.Join(v.Patterns, "\n"), Force: force, BackupDir: p.Backups, Manifest: m, Now: now}); err != nil {
		return v, err
	}
	tracked, err := trackedFiles(ctx, root, paths, false)
	if err != nil {
		return v, err
	}
	if len(tracked) > 0 {
		if _, err := gitrepo.Run(ctx, root, append([]string{"update-index", "--skip-worktree", "--"}, tracked...)...); err != nil {
			return v, err
		}
	}
	v.SkipWorktree = tracked
	return v, nil
}

// revertGitLocal: gỡ khối exclude và cờ skip-worktree trên tệp của Bint.
func revertGitLocal(ctx context.Context, w *repoWriter, root string, paths []string, p state.Paths, m *fsafe.Manifest, force bool, now time.Time) (GitVisibility, error) {
	var v GitVisibility
	ex, err := excludePath(ctx, root)
	if err != nil {
		return v, err
	}
	v.ExcludePath = ex
	if _, err := os.Stat(ex); err == nil {
		if v.Exclude, err = w.RemoveBlock(fsafe.BlockSync{Path: ex, RelName: excludeRel, Markers: fsafe.HashMarkers,
			Force: force, BackupDir: p.Backups, Manifest: m, Now: now}); err != nil {
			return v, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return v, err
	}
	flagged, err := trackedFiles(ctx, root, paths, true)
	if err != nil {
		return v, err
	}
	if len(flagged) > 0 {
		if _, err := gitrepo.Run(ctx, root, append([]string{"update-index", "--no-skip-worktree", "--"}, flagged...)...); err != nil {
			return v, err
		}
	}
	v.SkipWorktree = flagged
	return v, nil
}

// trackedFiles trả về tệp đã được Git theo dõi thuộc paths; skipOnly = chỉ tệp đang
// có cờ skip-worktree.
func trackedFiles(ctx context.Context, root string, paths []string, skipOnly bool) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	args := []string{"ls-files", "-z"}
	if skipOnly {
		args = append(args, "-v")
	}
	args = append(args, "--")
	for _, p := range paths {
		args = append(args, ":(literal)"+strings.TrimSuffix(p, "/"))
	}
	out, err := gitrepo.Run(ctx, root, args...)
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if f == "" {
			continue
		}
		if skipOnly {
			// -v: "S <tệp>" = skip-worktree (chữ hoa: không assume-unchanged).
			tag, name, ok := strings.Cut(f, " ")
			if !ok || (tag != "S" && tag != "s") {
				continue
			}
			f = name
		}
		files = append(files, f)
	}
	return files, nil
}
