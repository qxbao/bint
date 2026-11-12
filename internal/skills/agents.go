package skills

import (
	"errors"
	"os"
	"path/filepath"

	"bint/internal/fsafe"
)

// installAgents xuất bản định nghĩa subagent (opts.Agents) bằng đúng cơ chế của skill gốc: tạo nếu chưa có, ghi lại khi
// bản cũ chưa bị sửa (băm trùng manifest hoặc core.lock), báo xung đột khi đã bị sửa ngoài Bint (--force để ghi đè).
func installAgents(opts InstallOptions, lock *Lock) ([]Outcome, error) {
	if len(opts.Agents) == 0 {
		return nil, nil
	}
	if lock.Agents == nil {
		lock.Agents = map[string]LockEntry{}
	}
	var outs []Outcome
	for _, a := range opts.Agents {
		s := Skill{Name: a.Name, Content: a.Content}
		prev := lock.Agents[a.Name]
		o, err := publish(opts, s, a.Rel, prev.SHA256)
		if err != nil {
			return outs, err
		}
		o.Agent = true
		if o.Result == fsafe.Conflict {
			o.Detail = "định nghĩa subagent đã bị sửa ngoài Bint; --force để ghi đè (bản cũ được sao lưu)"
		}
		outs = append(outs, o)
		if o.Result != fsafe.Conflict {
			lock.Agents[a.Name] = LockEntry{SHA256: s.Hash(), Published: []string{a.Rel}}
		}
	}
	return outs, nil
}

// AgentStatus cho biết định nghĩa subagent đã xuất bản so với nội dung mong muốn (missing | current | differs), cho doctor.
func AgentStatus(root string, a AgentFile) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(a.Rel)))
	if errors.Is(err, os.ErrNotExist) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	if fsafe.Hash(string(b)) == fsafe.Hash(string(a.Content)) {
		return "current", nil
	}
	return "differs", nil
}
