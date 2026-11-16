package iso

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// KindLink là loại mục báo cáo của liên kết cấu hình (không có trong receipt: liên kết tự kiểm bằng EnsureLink).
const KindLink = "link"

// Entry là một khoá mong muốn (từ bản thật) mà sync nhân bản sang profile.
type Entry struct {
	Kind    string
	File    string   // tệp đích, tương đối theo home của profile, dấu '/' (".codex/config.toml", ".claude/.claude.json", …)
	Section []string // TOML: {"mcp_servers"}, {"hooks","state"}; JSON: {"mcpServers"}
	Name    string
	Value   any
}

// Key là tên khoá dạng đọc được (mcp_servers.x).
func (e Entry) Key() string { return entryKey(e.Section, e.Name) }

// HookTarget là tệp hook cấp user của profile (settings.json của Claude, hooks.json của Codex). Phần phụ thuộc
// harness (InstallHooks, đọc lệnh hook) do tầng app cấp, để internal/iso không import internal/harness.
type HookTarget struct {
	File string // tệp trong profile, dạng Entry.File
	// Get trả lệnh hook của bint đang cài và hiện hành ở profile (ok=false khi chưa cài hoặc matcher cũ).
	Get func() (cmd string, ok bool, err error)
	// Install cài hook với lệnh cmd (InstallHooks, đường dẫn tường minh); Uninstall gỡ hook của bint.
	Install   func(cmd string) error
	Uninstall func() error
}

// SyncSource là những gì tầng trên tính từ máy thật. Sync không tự đọc cấu hình Codex/Claude thật.
type SyncSource struct {
	// Entries là các khoá mong muốn. Khoá có trong receipt mà không còn trong Entries (và kind không bị Skip)
	// được coi là bản thật đã bỏ.
	Entries []Entry
	// Skip là các Kind không tính được lần này: khoá của chúng không bị gỡ, receipt giữ nguyên.
	Skip []string
	// Retain là các khoá (chỉ Kind/File/Section/Name được dùng) còn có ở bản thật nhưng nằm ngoài phạm vi lần sync
	// này (vd trust của repo khác khi profile dùng chung nhiều repo): khoá của chúng có trong receipt thì được giữ
	// nguyên, không bị coi là "bản thật đã bỏ" và không bị gỡ.
	Retain []Entry
	// Hooks là các tệp hook user; một Entry Kind=KindHook (Name "bint", Value = lệnh) trỏ tới HookTarget cùng File.
	Hooks []HookTarget
	// Late chạy sau khi các khoá của Entries đã ghi (không chạy khi Check) và trả thêm Entries Kind=KindHookState
	// (trust hook user, cần hooks/list với env profile nên không tính trước được). Ghi chú trả về nối vào báo cáo.
	// Hợp đồng: không tính được (không có harness, đọc cấu hình thật lỗi, hooks/list lỗi) thì PHẢI trả error —
	// Sync ghi chú và không đụng khoá nào. Trả nil error với danh sách rỗng nghĩa là "bản thật không có khoá
	// nào", và Sync gỡ khoá hook-state đã có trong receipt.
	Late  func() ([]Entry, []string, error)
	Notes []string
}

// SyncOptions là tuỳ chọn của Sync.
type SyncOptions struct {
	// Check: chỉ so sánh, không ghi gì (kể cả khoá phiên, receipt, thư mục), không chạy Late.
	Check bool
	Env   []string // env của tiến trình (mặc định os.Environ()); đích liên kết lấy từ env thật của nó
	Now   time.Time
}

// SyncItem là một dòng của báo cáo.
type SyncItem struct {
	Kind   string `json:"kind"`
	File   string `json:"file,omitempty"`
	Key    string `json:"key,omitempty"`
	Action Action `json:"action"`
	Detail string `json:"detail,omitempty"`
}

// SyncReport là kết quả sync một profile.
type SyncReport struct {
	Profile string     `json:"profile"`
	Check   bool       `json:"check"`
	Items   []SyncItem `json:"items"`
	Notes   []string   `json:"notes,omitempty"`
}

// Drift: có khoá/liên kết lệch bản thật, xung đột hoặc lỗi (--check thoát 1).
func (r SyncReport) Drift() bool {
	for _, it := range r.Items {
		if it.Action.Drift() {
			return true
		}
	}
	return false
}

// Conflicts là các mục xung đột (không bao giờ bị ghi đè).
func (r SyncReport) Conflicts() []SyncItem {
	var out []SyncItem
	for _, it := range r.Items {
		if it.Action == ActConflict {
			out = append(out, it)
		}
	}
	return out
}

// Sync đồng bộ cấu hình từ máy thật (src) sang profile p theo docs/iso-plan.md mục 5.6.
//
// Ghi (Check=false) lấy khoá X của profile không chờ: có phiên đang chạy thì trả ErrBusy và không đổi gì.
// Mỗi khoá đi qua máy trạng thái reconcile với receipt (sync-receipt.json, 0600): chưa sửa tay thì theo bản
// thật, sửa tay thì báo xung đột và giữ nguyên. Tệp đích chỉ ghi khi có khoá đổi, sau khi sao lưu vào
// <profile>/sync-backups. Check không tạo, không ghi, không lấy khoá nào.
func Sync(p Profile, src SyncSource, opt SyncOptions) (SyncReport, error) {
	rep := SyncReport{Profile: p.ID(), Check: opt.Check, Items: []SyncItem{}}
	env := opt.Env
	if env == nil {
		env = os.Environ()
	}
	now := opt.Now
	if now.IsZero() {
		now = time.Now()
	}
	if !opt.Check {
		release, busy, err := p.Lock(true)
		if err != nil {
			return rep, err
		}
		if busy {
			return rep, fmt.Errorf("%w: %s (thoát phiên rồi chạy lại bint iso sync)", ErrBusy, p.ID())
		}
		defer release()
	}
	rep.Items = append(rep.Items, syncLinks(p, realEnvList(env), opt.Check)...)

	rec, rerr := LoadReceipt(p.ReceiptPath())
	if rerr != nil {
		rep.Notes = append(rep.Notes, fmt.Sprintf("receipt hỏng, coi như rỗng (khoá khác bản thật sẽ là xung đột, không bị ghi đè): %v", rerr))
	}
	s := &syncRun{p: p, src: src, check: opt.Check, now: now, rep: &rep, rec: map[ident]ReceiptEntry{}, retain: map[ident]string{}}
	for _, e := range rec.Entries {
		s.rec[ident{e.File, e.Key}] = e
	}
	rep.Notes = append(rep.Notes, src.Notes...)
	skip := src.Skip
	for _, e := range src.Retain {
		s.retain[ident{e.File, e.Key()}] = e.Kind
	}
	s.reconcile(src.Entries, func(k string) bool { return !slices.Contains(skip, k) })
	s.retain = nil
	if !opt.Check && src.Late != nil {
		entries, notes, err := src.Late()
		rep.Notes = append(rep.Notes, notes...)
		if err != nil {
			rep.Notes = append(rep.Notes, fmt.Sprintf("trust hook user: %v", err))
		} else {
			s.reconcile(entries, func(k string) bool { return k == KindHookState })
		}
	}
	if !opt.Check && s.recDirty {
		out := Receipt{Version: ReceiptVersion}
		for _, e := range s.rec {
			out.Entries = append(out.Entries, e)
		}
		if err := SaveReceipt(p.ReceiptPath(), out); err != nil {
			return rep, fmt.Errorf("ghi receipt: %w", err)
		}
	}
	return rep, nil
}

type ident struct{ file, key string }

type syncRun struct {
	p        Profile
	src      SyncSource
	check    bool
	now      time.Time
	rep      *SyncReport
	rec      map[ident]ReceiptEntry
	recDirty bool
	retain   map[ident]string // khoá ngoài phạm vi nhưng còn ở bản thật (SyncSource.Retain) -> kind
	backends map[string]keyBackend
	badFiles map[string]error
}

func (s *syncRun) add(it SyncItem) { s.rep.Items = append(s.rep.Items, it) }

func (s *syncRun) backendRel(file string) string {
	return filepath.Join(s.p.Home, filepath.FromSlash(file))
}

// backend mở (một lần mỗi giai đoạn) tệp đích của file.
func (s *syncRun) backend(file string) (keyBackend, error) {
	if b, ok := s.backends[file]; ok {
		return b, nil
	}
	if err, ok := s.badFiles[file]; ok {
		return nil, err
	}
	var (
		b   keyBackend
		err error
	)
	if h := s.hookTarget(file); h != nil {
		b = &hookBackend{t: *h}
	} else {
		switch {
		case strings.HasSuffix(file, ".toml"):
			b, err = openTOML(s.backendRel(file))
		case strings.HasSuffix(file, ".json"):
			b, err = openJSON(s.backendRel(file))
		default:
			err = fmt.Errorf("không có backend cho tệp %s", file)
		}
	}
	if err != nil {
		if s.badFiles == nil {
			s.badFiles = map[string]error{}
		}
		s.badFiles[file] = err
		return nil, err
	}
	if s.backends == nil {
		s.backends = map[string]keyBackend{}
	}
	s.backends[file] = b
	return b, nil
}

func (s *syncRun) hookTarget(file string) *HookTarget {
	for i := range s.src.Hooks {
		if s.src.Hooks[i].File == file {
			return &s.src.Hooks[i]
		}
	}
	return nil
}

// reconcile đối chiếu desired với profile và receipt cho mọi khoá có include(kind). Sau khi áp dụng thì lưu
// các tệp đã đổi và bỏ bộ nhớ đệm (giai đoạn sau, sau khi harness chạy, đọc lại tệp).
func (s *syncRun) reconcile(desired []Entry, include func(kind string) bool) {
	type row struct {
		d     *Entry
		r     *ReceiptEntry
		kind  string
		file  string
		sec   []string
		name  string
		rowID ident
	}
	rows := map[ident]*row{}
	for i := range desired {
		e := desired[i]
		if !include(e.Kind) {
			continue
		}
		id := ident{e.File, e.Key()}
		rows[id] = &row{d: &desired[i], kind: e.Kind, file: e.File, sec: e.Section, name: e.Name, rowID: id}
	}
	for id, re := range s.rec {
		if !include(re.Kind) {
			continue
		}
		re := re
		if r, ok := rows[id]; ok {
			r.r = &re
			continue
		}
		if k, ok := s.retain[id]; ok && k == re.Kind {
			continue // còn ở bản thật nhưng ngoài phạm vi lần sync này: giữ nguyên
		}
		rows[id] = &row{r: &re, kind: re.Kind, file: re.File, sec: re.Section, name: re.Name, rowID: id}
	}
	ids := make([]ident, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b ident) int { return strings.Compare(a.file+"\x00"+a.key, b.file+"\x00"+b.key) })

	for _, id := range ids {
		r := rows[id]
		it := SyncItem{Kind: r.kind, File: r.file, Key: id.key}
		be, err := s.backend(r.file)
		if err != nil {
			it.Action, it.Detail = ActError, err.Error()
			s.add(it)
			continue
		}
		var hd, hc, hr string
		if r.d != nil {
			if hd, err = HashValue(r.d.Value); err != nil {
				it.Action, it.Detail = ActError, err.Error()
				s.add(it)
				continue
			}
		}
		if r.r != nil {
			hr = r.r.SHA256
		}
		cv, present, err := be.get(r.sec, r.name)
		if err == nil && present {
			hc, err = HashValue(cv)
		}
		if err != nil {
			it.Action, it.Detail = ActError, err.Error()
			s.add(it)
			continue
		}
		it.Action, it.Detail = reconcile(hd, hc, hr)
		if !s.check {
			if err := s.apply(be, r.d, r.kind, r.file, r.sec, r.name, id, it.Action, hd, hr); err != nil {
				it.Action, it.Detail = ActError, err.Error()
			}
		}
		s.add(it)
	}
	if !s.check {
		for file, be := range s.backends {
			if _, err := be.save(filepath.Join(s.p.Dir, "sync-backups"), file, s.now); err != nil {
				s.add(SyncItem{Kind: "file", File: file, Action: ActError, Detail: err.Error()})
			}
		}
	}
	s.backends, s.badFiles = nil, nil
}

func (s *syncRun) apply(be keyBackend, d *Entry, kind, file string, sec []string, name string, id ident, act Action, hd, hr string) error {
	switch act {
	case ActWrite, ActUpdate:
		if err := be.set(sec, name, d.Value); err != nil {
			return err
		}
		s.setRec(ReceiptEntry{Kind: kind, File: file, Key: id.key, Section: sec, Name: name, SHA256: hd})
	case ActKeep:
		if d != nil && hr != hd {
			s.setRec(ReceiptEntry{Kind: kind, File: file, Key: id.key, Section: sec, Name: name, SHA256: hd})
		}
	case ActRemove:
		if err := be.del(sec, name); err != nil {
			return err
		}
		s.delRec(id)
	case ActForget:
		s.delRec(id)
	}
	return nil
}

func (s *syncRun) setRec(e ReceiptEntry) {
	s.rec[ident{e.File, e.Key}] = e
	s.recDirty = true
}

func (s *syncRun) delRec(id ident) {
	delete(s.rec, id)
	s.recDirty = true
}

// hookBackend dùng HookTarget như một "tệp" một khoá: Get/Set/Del đi qua lệnh hook, ghi ngay (InstallHooks tự
// sao lưu và ghi nguyên tử), nên save không làm gì.
type hookBackend struct{ t HookTarget }

func (b *hookBackend) get([]string, string) (any, bool, error) {
	cmd, ok, err := b.t.Get()
	if err != nil || !ok {
		return nil, false, err
	}
	return cmd, true, nil
}

func (b *hookBackend) set(_ []string, _ string, v any) error {
	cmd, _ := v.(string)
	return b.t.Install(cmd)
}

func (b *hookBackend) del([]string, string) error { return b.t.Uninstall() }

func (b *hookBackend) save(string, string, time.Time) (bool, error) { return false, nil }
