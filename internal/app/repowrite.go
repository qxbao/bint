package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/fsafe"
	"bint/internal/hcomp"
	"bint/internal/knowledge"
	"bint/internal/skills"
	"bint/internal/state"
	"bint/internal/workflow"
)

// repoWriter là hàm ghi chung cho MỌI thao tác ghi tệp trong repo của tầng dịch vụ (CLI và TUI cùng đi qua
// đây). Mỗi tệp đã ghi được ghi nhận vào <runtime>/writes.jsonl để bước chỉ đọc của `bint run` không tính
// thay đổi do Bint ghi thay người là vi phạm (docs/isolation-subagent-plan.md 4.5, H8).
//
// Hai dạng: (a) tự ghi (WriteFile, WriteNew, Remove, SyncBlock, RemoveBlock); (b) ghi nhận ngay SAU lời gọi
// một gói thấp hơn đã tự ghi tệp (Record): knowledge.Store.Save/Remove, skills.Install, hcomp.Save,
// evolve.Apply/Rollback — các gói đó không thuộc tầng dịch vụ nên không gọi được writer.
//
// Lỗi ghi nhật ký không làm hỏng thao tác (tệp đã ghi xong): hậu quả xấu nhất là bước chỉ đọc báo vi phạm
// như trước, tức là hướng an toàn. Chỉ ghi nhận khi thật sự đổi tệp. Hook không dùng repoWriter.
type repoWriter struct {
	log    string
	actor  string
	retain time.Duration
}

// newRepoWriter dựng writer cho thao tác actor ("bint config set"...). Thời hạn giữ dòng nhật ký là
// workflow.timeout_minutes × 2 (0 thì 120 phút).
func newRepoWriter(paths state.Paths, cfg config.Config, actor string) *repoWriter {
	mins := cfg.Workflow.TimeoutMinutes
	if mins <= 0 {
		mins = 60
	}
	w := &repoWriter{actor: actor, retain: time.Duration(mins) * 2 * time.Minute}
	if paths.Runtime != "" {
		w.log = workflow.WritesLogPath(paths.Runtime)
	}
	return w
}

// WriteFile ghi nguyên tử data vào abs rồi ghi nhận.
func (w *repoWriter) WriteFile(abs string, data []byte, perm os.FileMode) error {
	if err := fsafe.WriteAtomic(abs, data, perm); err != nil {
		return err
	}
	w.Record(abs)
	return nil
}

// WriteNew tạo tệp mới (không ghi đè: lỗi nếu tệp đã tồn tại) rồi ghi nhận.
func (w *repoWriter) WriteNew(abs string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(abs)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	w.Record(abs)
	return nil
}

// Remove xoá tệp abs rồi ghi nhận (băm rỗng). Tệp không có thì không ghi nhận và trả lỗi như os.Remove.
func (w *repoWriter) Remove(abs string) error {
	if err := os.Remove(abs); err != nil {
		return err
	}
	w.Record(abs)
	return nil
}

// SyncBlock gọi fsafe.SyncBlock và ghi nhận tệp nếu nó thật sự được tạo/cập nhật/gỡ.
func (w *repoWriter) SyncBlock(req fsafe.BlockSync) (fsafe.Result, error) {
	r, err := fsafe.SyncBlock(req)
	if err == nil && changedOutcome(r.Outcome) {
		w.Record(req.Path)
	}
	return r, err
}

// RemoveBlock gọi fsafe.RemoveBlock và ghi nhận tệp nếu nó thật sự bị đổi (kể cả bị xoá vì rỗng).
func (w *repoWriter) RemoveBlock(req fsafe.BlockSync) (fsafe.Result, error) {
	r, err := fsafe.RemoveBlock(req)
	if err == nil && changedOutcome(r.Outcome) {
		w.Record(req.Path)
	}
	return r, err
}

// SaveItem lưu mục tri thức (knowledge.Store.Save) rồi ghi nhận tệp của nó.
func (w *repoWriter) SaveItem(st knowledge.Store, it *knowledge.Item) error {
	if err := st.Save(it); err != nil {
		return err
	}
	w.Record(st.PathFor(it.ID))
	return nil
}

// RemoveItem xoá mục tri thức (knowledge.Store.Remove) rồi ghi nhận (băm rỗng).
func (w *repoWriter) RemoveItem(st knowledge.Store, id string) error {
	if err := st.Remove(id); err != nil {
		return err
	}
	w.Record(st.PathFor(id))
	return nil
}

// SaveComponent lưu thành phần quy trình (hcomp.Save) rồi ghi nhận mọi tệp nó đã ghi (đường dẫn trả về
// tương đối với dir).
func (w *repoWriter) SaveComponent(dir string, c hcomp.Component) error {
	names, err := hcomp.Save(dir, c)
	var paths []string
	for _, n := range names {
		paths = append(paths, filepath.Join(dir, filepath.FromSlash(n)))
	}
	w.Record(paths...)
	return err
}

// InstallSkills gọi skills.Install rồi ghi nhận mọi skill thật sự được ghi (Created/Updated/Removed; đường dẫn
// trong Outcome tương đối với opts.Root) và core.lock nếu nội dung của nó đổi. Ghi nhận cả khi Install trả lỗi
// giữa chừng: các tệp đã ghi trước đó vẫn là do Bint ghi.
func (w *repoWriter) InstallSkills(opts skills.InstallOptions) ([]skills.Outcome, error) {
	lock := filepath.Join(opts.Root, filepath.FromSlash(skills.LockRel))
	before := workflow.HashFile(lock)
	outs, err := skills.Install(opts)
	w.recordSkillOutcomes(opts.Root, outs)
	if workflow.HashFile(lock) != before {
		w.Record(lock)
	}
	return outs, err
}

// recordSkillOutcomes ghi nhận các tệp skill mà Outcome báo là đã đổi.
func (w *repoWriter) recordSkillOutcomes(root string, outs []skills.Outcome) {
	var paths []string
	for _, o := range outs {
		if changedOutcome(o.Result) {
			paths = append(paths, filepath.Join(root, filepath.FromSlash(o.Path)))
		}
	}
	w.Record(paths...)
}

func changedOutcome(o fsafe.Outcome) bool {
	return o == fsafe.Created || o == fsafe.Appended || o == fsafe.Updated || o == fsafe.Removed
}

// Record ghi nhận các tệp (đường dẫn tuyệt đối) vừa được một gói thấp hơn ghi hoặc xoá: mỗi tệp một dòng
// {path, sha256 hiện tại ("" nếu không còn), at, actor, agent}. Đường dẫn rỗng và tệp trong .git bị bỏ qua.
func (w *repoWriter) Record(paths ...string) {
	if w == nil || w.log == "" {
		return
	}
	agent, _ := InAgentSession()
	now := time.Now()
	var recs []workflow.WriteRecord
	for _, p := range paths {
		if p == "" || inDotGit(p) {
			continue
		}
		recs = append(recs, workflow.WriteRecord{Path: workflow.NormalizeWritePath(p), SHA256: workflow.HashFile(p),
			At: now, Actor: w.actor, Agent: agent})
	}
	_ = workflow.AppendWrites(w.log, recs, w.retain, now)
}

func inDotGit(p string) bool {
	return slices.Contains(strings.Split(filepath.ToSlash(p), "/"), ".git")
}

// absUnder trả p nếu đã tuyệt đối, ngược lại ghép với root.
func absUnder(root, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, filepath.FromSlash(p))
}
