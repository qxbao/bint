package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bint/internal/knowledge"
)

// Nhánh minimal dựng từ tri thức (eval.minimal_source: knowledge, P2).
//
// strict (mục 12.3): tri thức phải được dựng TẠI commit gốc của tác vụ:
//  1. ảnh chụp do `bint eval bootstrap` sinh bằng cách chạy b-bootstrap trong thư
//     mục cô lập tại commit gốc (<eval-work>/knowledge/<base>/), hoặc
//  2. tệp .bint/knowledge/items đã được commit tại commit gốc.
//     Không có thì nhánh không khả dụng cho tác vụ đó.
//
// economy: tri thức hiện tại, chỉ giữ mục có mọi bằng chứng tồn tại và khớp
// fingerprint tại commit gốc, mọi commit bằng chứng là tổ tiên của commit gốc, và
// không rút ra từ chính tác vụ; nội dung dựng ra không được chứa mã của lời giải.
// Kết quả chỉ để sàng lọc.

// SnapshotDir là thư mục ảnh chụp tri thức của một commit gốc.
func SnapshotDir(workRoot, base string) string { return filepath.Join(workRoot, "knowledge", base) }

// SnapshotMeta mô tả một ảnh chụp tri thức do `bint eval bootstrap` sinh.
type SnapshotMeta struct {
	Base           string   `json:"base"`
	CreatedAt      string   `json:"created_at"`
	Harness        string   `json:"harness"`
	HarnessVersion string   `json:"harness_version,omitempty"`
	Model          string   `json:"model,omitempty"`
	CostUSD        *float64 `json:"cost_usd,omitempty"`
	DurationMS     int64    `json:"duration_ms"`
	AgentExitCode  int      `json:"agent_exit_code"`
	Error          string   `json:"error,omitempty"`
	Items          int      `json:"items"`
	SkillSHA       string   `json:"skill_sha"`
	BintVersion    string   `json:"bint_version"`
	Log            string   `json:"log"`
}

// ReadSnapshot đọc ảnh chụp của commit gốc; ok=false nếu chưa có.
func ReadSnapshot(workRoot, base string) (SnapshotMeta, []*knowledge.Item, bool, error) {
	var meta SnapshotMeta
	dir := SnapshotDir(workRoot, base)
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if errors.Is(err, os.ErrNotExist) {
		return meta, nil, false, nil
	}
	if err != nil {
		return meta, nil, false, err
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return meta, nil, false, err
	}
	items, bad, err := knowledge.Store{Dir: filepath.Join(dir, "items")}.List()
	if err != nil {
		return meta, nil, false, err
	}
	if len(bad) > 0 {
		return meta, nil, false, fmt.Errorf("ảnh chụp %s có tệp hỏng: %v", short(base), bad[0])
	}
	return meta, items, true, nil
}

// committedItems đọc .bint/knowledge/items tại một commit.
func committedItems(ctx context.Context, repoDir, commit string) ([]*knowledge.Item, error) {
	out, err := git(ctx, repoDir, "ls-tree", "-z", "--name-only", commit+":"+knowledge.ItemsRel)
	if err != nil {
		return nil, nil // thư mục không tồn tại tại commit đó
	}
	var items []*knowledge.Item
	for _, name := range strings.Split(string(out), "\x00") {
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		data, ok := showFile(ctx, repoDir, commit, knowledge.ItemsRel+"/"+name)
		if !ok {
			continue
		}
		it, err := knowledge.Parse(data)
		if err != nil {
			continue // tệp hỏng tại commit đó: bỏ qua, không đoán
		}
		items = append(items, it)
	}
	return items, nil
}

// knowledgeFor chọn tập tri thức của tác vụ cho các nhánh minimal, bint, ideal.
func knowledgeFor(ctx context.Context, env Env, mode string, c *Case, leakIn *LeakInput) (knowledgeSet, error) {
	cfg := env.Cfg.Eval
	opts := knowledge.RenderOptions{MaxItems: env.Cfg.Knowledge.AgentsMDMaxItems, IncludeCandidates: cfg.MinimalIncludeCandidates}
	notFromCase := func(items []*knowledge.Item) []*knowledge.Item {
		var out []*knowledge.Item
		for _, it := range items {
			if it.Origin.EvalCase != c.ID {
				out = append(out, it)
			}
		}
		return out
	}

	if mode == LeakStrict {
		_, items, ok, err := ReadSnapshot(env.WorkRoot, c.Source.Base)
		if err != nil {
			return knowledgeSet{}, err
		}
		src := "bootstrap@" + short(c.Source.Base)
		candidates := cfg.MinimalIncludeCandidates
		if ok {
			// Ảnh chụp là đầu ra tự động của bootstrap: mọi mục đều chưa duyệt, và đó
			// chính là thứ đang được đo.
			candidates = true
		} else {
			items, err = committedItems(ctx, env.RepoDir, c.Source.Base)
			if err != nil {
				return knowledgeSet{}, err
			}
			if len(items) == 0 {
				return knowledgeSet{}, fmt.Errorf("%w: strict cần tri thức dựng tại commit gốc %s — chạy `bint eval bootstrap` (hoặc dùng economy để sàng lọc)", ErrArmUnavailable, short(c.Source.Base))
			}
			src = short(c.Source.Base) + ":" + knowledge.ItemsRel
		}
		return knowledgeSet{items: notFromCase(items), src: src, candidates: candidates}, nil
	}

	// economy
	root := env.BintRoot
	if root == "" {
		root = env.RepoDir
	}
	items, _, err := knowledge.NewStore(root).List()
	if err != nil {
		return knowledgeSet{}, err
	}
	kept, err := EconomyFilter(ctx, env.RepoDir, c, notFromCase(items))
	if err != nil {
		return knowledgeSet{}, err
	}
	if len(kept) == 0 {
		return knowledgeSet{}, fmt.Errorf("%w: không mục tri thức nào có bằng chứng khớp tại commit gốc %s", ErrArmUnavailable, short(c.Source.Base))
	}
	if leakIn != nil {
		// Kiểm tra toàn bộ nội dung tri thức (không chỉ phần vào AGENTS.md): nhánh bint
		// có thể trả bất kỳ mục nào qua Context Engine.
		var all strings.Builder
		for _, it := range kept {
			all.WriteString(it.Title + "\n" + it.Body + "\n")
		}
		all.WriteString(string(renderMinimal(kept, opts)))
		if r := CheckLeak(all.String(), *leakIn); r.Status == "fail" {
			return knowledgeSet{}, fmt.Errorf("%w: tri thức chứa mã của lời giải (%s)", ErrArmUnavailable, r.Findings[0].Detail)
		}
	}
	return knowledgeSet{items: kept, src: fmt.Sprintf("worktree:%s[%d]", knowledge.ItemsRel, len(kept)), screening: true,
		candidates: cfg.MinimalIncludeCandidates}, nil
}

// EconomyFilter giữ các mục mà mọi bằng chứng tồn tại và khớp fingerprint tại
// commit gốc, và mọi commit được nêu làm bằng chứng là tổ tiên của commit gốc.
// Mục không có bằng chứng (tri thức do người viết) bị loại vì không chứng minh được
// nó không đến từ lời giải.
func EconomyFilter(ctx context.Context, repoDir string, c *Case, items []*knowledge.Item) ([]*knowledge.Item, error) {
	src := &knowledge.CommitSource{RepoDir: repoDir, Commit: c.Source.Base}
	var out []*knowledge.Item
next:
	for _, it := range items {
		if !it.Active() || len(it.Evidence) == 0 {
			continue
		}
		for _, e := range it.Evidence {
			b, ok := src.Read(e.Path)
			if !ok || e.Fingerprint == "" {
				continue next
			}
			if c := knowledge.CheckEvidence(e, b); c.Status != knowledge.EvMatch && c.Status != knowledge.EvRelocated {
				continue next
			}
			if e.Commit != "" {
				if _, err := git(ctx, repoDir, "merge-base", "--is-ancestor", e.Commit, c.Source.Base); err != nil {
					continue next
				}
			}
		}
		out = append(out, it)
	}
	return out, nil
}

func renderMinimal(items []*knowledge.Item, opts knowledge.RenderOptions) []byte {
	opts.ForEval = true
	s, _ := knowledge.RenderAgents(items, opts)
	return []byte(s)
}

// minimalFromKnowledge dựng AGENTS.md của nhánh minimal từ tri thức của tác vụ.
func minimalFromKnowledge(ctx context.Context, env Env, mode string, c *Case, leakIn *LeakInput) ([]byte, string, bool, error) {
	ks, err := knowledgeFor(ctx, env, mode, c, leakIn)
	if err != nil {
		return nil, "", mode == LeakEconomy, err
	}
	content := renderMinimal(ks.items, knowledge.RenderOptions{MaxItems: env.Cfg.Knowledge.AgentsMDMaxItems, IncludeCandidates: ks.candidates})
	return content, fmt.Sprintf("%s#%s", ks.src, hash12(content)), ks.screening, nil
}
