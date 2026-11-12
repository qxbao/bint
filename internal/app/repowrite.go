package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"bint/internal/config"
	"bint/internal/fsafe"
	"bint/internal/harness"
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

// writeEntry là một tệp cần ghi nhận: prev là băm ngay trước lời gọi ghi (nil = không biết), sha là băm sau
// khi ghi khi biết từ dữ liệu ghi (nil = băm tệp trên đĩa). Dùng dữ liệu ghi thay vì đọc lại đĩa để một bên
// khác ghi chen giữa không bị tính là kết quả của lần ghi này.
type writeEntry struct {
	path      string
	prev, sha *string
}

func strp(s string) *string { return &s }

// WriteFile ghi nguyên tử data vào abs rồi ghi nhận.
func (w *repoWriter) WriteFile(abs string, data []byte, perm os.FileMode) error {
	prev := w.hashBefore(abs)
	if err := fsafe.WriteAtomic(abs, data, perm); err != nil {
		return err
	}
	w.record(writeEntry{abs, prev, strp(workflow.HashBytes(data))})
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
	// O_EXCL bảo đảm tệp chưa có trước đó.
	w.record(writeEntry{abs, strp(""), strp(workflow.HashBytes(data))})
	return nil
}

// Remove xoá tệp abs rồi ghi nhận (băm rỗng). Tệp không có thì không ghi nhận và trả lỗi như os.Remove.
func (w *repoWriter) Remove(abs string) error {
	prev := w.hashBefore(abs)
	if err := os.Remove(abs); err != nil {
		return err
	}
	w.record(writeEntry{abs, prev, strp("")})
	return nil
}

// SyncBlock gọi fsafe.SyncBlock và ghi nhận tệp nếu nó thật sự được tạo/cập nhật/gỡ.
func (w *repoWriter) SyncBlock(req fsafe.BlockSync) (fsafe.Result, error) {
	prev := w.hashBefore(req.Path)
	r, err := fsafe.SyncBlock(req)
	if err == nil && changedOutcome(r.Outcome) {
		w.record(writeEntry{path: req.Path, prev: prev})
	}
	return r, err
}

// RemoveBlock gọi fsafe.RemoveBlock và ghi nhận tệp nếu nó thật sự bị đổi (kể cả bị xoá vì rỗng).
func (w *repoWriter) RemoveBlock(req fsafe.BlockSync) (fsafe.Result, error) {
	prev := w.hashBefore(req.Path)
	r, err := fsafe.RemoveBlock(req)
	if err == nil && changedOutcome(r.Outcome) {
		w.record(writeEntry{path: req.Path, prev: prev})
	}
	return r, err
}

// SaveItem lưu mục tri thức (knowledge.Store.Save) rồi ghi nhận tệp của nó.
func (w *repoWriter) SaveItem(st knowledge.Store, it *knowledge.Item) error {
	path := st.PathFor(it.ID)
	prev := w.hashBefore(path)
	if err := st.Save(it); err != nil {
		return err
	}
	w.record(writeEntry{path: path, prev: prev})
	return nil
}

// RemoveItem xoá mục tri thức (knowledge.Store.Remove) rồi ghi nhận (băm rỗng).
func (w *repoWriter) RemoveItem(st knowledge.Store, id string) error {
	path := st.PathFor(id)
	prev := w.hashBefore(path)
	if err := st.Remove(id); err != nil {
		return err
	}
	w.record(writeEntry{path, prev, strp("")})
	return nil
}

// SaveComponent lưu thành phần quy trình (hcomp.Save) rồi ghi nhận mọi tệp nó đã ghi (đường dẫn trả về
// tương đối với dir). Băm trước và băm sau lấy từ hcomp.Files (cùng nội dung hcomp.Save ghi).
func (w *repoWriter) SaveComponent(dir string, c hcomp.Component) error {
	files, _ := hcomp.Files(c) // lỗi: hcomp.Save trả cùng lỗi, không có tệp nào được ghi
	prev := map[string]*string{}
	for n := range files {
		prev[n] = w.hashBefore(filepath.Join(dir, filepath.FromSlash(n)))
	}
	names, err := hcomp.Save(dir, c)
	var es []writeEntry
	for _, n := range names {
		e := writeEntry{path: filepath.Join(dir, filepath.FromSlash(n)), prev: prev[n]}
		if data, ok := files[n]; ok {
			e.sha = strp(workflow.HashBytes(data))
		}
		es = append(es, e)
	}
	w.record(es...)
	return err
}

// InstallSkills gọi skills.Install rồi ghi nhận mọi skill thật sự được ghi (Created/Updated/Removed; đường dẫn
// trong Outcome tương đối với opts.Root) và core.lock nếu nội dung của nó đổi. Ghi nhận cả khi Install trả lỗi
// giữa chừng: các tệp đã ghi trước đó vẫn là do Bint ghi. Băm trước lấy từ mọi tệp dưới thư mục skill của các
// target và core.lock (tệp ngoài các thư mục đó không có băm trước: prev nil).
func (w *repoWriter) InstallSkills(opts skills.InstallOptions) ([]skills.Outcome, error) {
	lock := filepath.Join(opts.Root, filepath.FromSlash(skills.LockRel))
	before := map[string]string{lock: workflow.HashFile(lock)}
	var dirs []string
	scan := []string{}
	for _, t := range opts.Targets {
		scan = append(scan, t.Dir)
	}
	if len(opts.Agents) > 0 {
		scan = append(scan, harness.SubagentDir) // định nghĩa subagent (.claude/agents) cũng là tệp do Bint ghi
	}
	for _, rel := range scan {
		dir := filepath.Join(opts.Root, filepath.FromSlash(rel))
		dirs = append(dirs, dir)
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				before[p] = workflow.HashFile(p)
			}
			return nil
		})
	}
	prevOf := func(p string) *string {
		if h, ok := before[p]; ok {
			return strp(h)
		}
		for _, d := range dirs {
			if rel, err := filepath.Rel(d, p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				return strp("") // dưới thư mục đã quét mà không có trước đó: tệp mới
			}
		}
		return nil
	}
	outs, err := skills.Install(opts)
	var es []writeEntry
	for _, o := range outs {
		if changedOutcome(o.Result) {
			p := filepath.Join(opts.Root, filepath.FromSlash(o.Path))
			es = append(es, writeEntry{path: p, prev: prevOf(p)})
		}
	}
	if workflow.HashFile(lock) != before[lock] {
		es = append(es, writeEntry{path: lock, prev: strp(before[lock])})
	}
	w.record(es...)
	return outs, err
}

// hashBefore là băm tệp abs ngay lúc gọi ("" nếu chưa có); nil nếu writer không ghi nhật ký (khỏi tốn công băm).
func (w *repoWriter) hashBefore(abs string) *string {
	if w == nil || w.log == "" {
		return nil
	}
	return strp(workflow.HashFile(abs))
}

func changedOutcome(o fsafe.Outcome) bool {
	return o == fsafe.Created || o == fsafe.Appended || o == fsafe.Updated || o == fsafe.Removed
}

// Record ghi nhận các tệp (đường dẫn tuyệt đối) vừa được một gói thấp hơn ghi hoặc xoá, KHÔNG biết trạng thái
// trước: mỗi tệp một dòng {path, sha256 hiện tại ("" nếu không còn), at, actor, agent} không có prev_sha256, nên
// ReadonlyViolations không miễn cho các dòng này (hướng an toàn). Đường dẫn rỗng và tệp trong .git bị bỏ qua.
func (w *repoWriter) Record(paths ...string) {
	es := make([]writeEntry, len(paths))
	for i, p := range paths {
		es[i] = writeEntry{path: p}
	}
	w.record(es...)
}

// record ghi nhận các tệp đã ghi, mỗi tệp một dòng nhật ký.
func (w *repoWriter) record(es ...writeEntry) {
	if w == nil || w.log == "" {
		return
	}
	agent, _ := InAgentSession()
	now := time.Now()
	var recs []workflow.WriteRecord
	for _, e := range es {
		if e.path == "" || inDotGit(e.path) {
			continue
		}
		sha := ""
		if e.sha != nil {
			sha = *e.sha
		} else {
			sha = workflow.HashFile(e.path)
		}
		recs = append(recs, workflow.WriteRecord{Path: workflow.NormalizeWritePath(e.path), SHA256: sha, Prev: e.prev,
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
