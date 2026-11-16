package iso

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// linkSpec là một liên kết cấu hình: Rel (theo home của profile, dùng '/') trỏ tới Target (bản thật).
type linkSpec struct{ Rel, Target string }

// syncLinkSpecs là danh mục liên kết cấu hình theo tool (docs/iso-plan.md mục 5.6): skill/agent/command
// của Claude, skill của Codex và ~/.agents/skills (cả hai). Đích lấy từ env thật; bản thật chưa có thì không
// có liên kết (bỏ qua). e phải là env thật.
func syncLinkSpecs(p Profile, e *envList) ([]linkSpec, error) {
	var specs []linkSpec
	switch p.Tool {
	case Claude:
		dir, err := realClaudeDirOf(e)
		if err != nil {
			return nil, err
		}
		for _, n := range []string{"skills", "agents", "commands"} {
			specs = append(specs, linkSpec{".claude/" + n, filepath.Join(dir, n)})
		}
	case Codex:
		dir, err := realCodexHomeOf(e)
		if err != nil {
			return nil, err
		}
		specs = append(specs, linkSpec{".codex/skills", filepath.Join(dir, "skills")})
	}
	home, err := realHomeOf(e)
	if err != nil {
		return nil, err
	}
	return append(specs, linkSpec{".agents/skills", filepath.Join(home, ".agents", "skills")}), nil
}

// syncLinks tạo (hoặc, khi check, chỉ kiểm) liên kết cấu hình. Thư mục riêng có dữ liệu hoặc liên kết lạ được giữ
// và báo (ActSkip), không bao giờ ghi đè.
func syncLinks(p Profile, e *envList, check bool) []SyncItem {
	specs, err := syncLinkSpecs(p, e)
	if err != nil {
		return []SyncItem{{Kind: KindLink, Action: ActError, Detail: err.Error()}}
	}
	var items []SyncItem
	for _, s := range specs {
		if _, err := os.Stat(s.Target); errors.Is(err, os.ErrNotExist) || within(s.Target, p.StoreRoot) {
			continue // bản thật không có (hoặc là chính store): không có gì để liên kết
		}
		link := filepath.Join(p.Home, filepath.FromSlash(s.Rel))
		it := SyncItem{Kind: KindLink, File: s.Rel, Key: s.Target}
		var st LinkState
		if check {
			st, err = InspectLink(link, s.Target)
		} else {
			st, err = EnsureLink(link, s.Target)
		}
		switch {
		case err != nil:
			it.Action, it.Detail = ActError, err.Error()
		case st == LinkOK:
			it.Action = ActKeep
		case st == LinkCreated || st == LinkReplacedEmpty:
			it.Action = ActWrite
			if !check {
				it.Detail = "đã liên kết"
			}
		case st == LinkConflictLink:
			it.Action, it.Detail = ActSkip, fmt.Sprintf("giữ nguyên: %s là liên kết tới chỗ khác", link)
		default:
			it.Action, it.Detail = ActSkip, fmt.Sprintf("giữ nguyên: %s là thư mục riêng của profile có dữ liệu (không liên kết tới bản thật)", link)
		}
		items = append(items, it)
	}
	return items
}
