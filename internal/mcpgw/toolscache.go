package mcpgw

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
)

// ToolsCacheDirName là thư mục trong thư mục gateway giữ cache initialize/tools/list theo (service, revision).
const ToolsCacheDirName = "tools-cache"

var cacheNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// diskEntry là dạng lưu trên đĩa của cacheEntry.
type diskEntry struct {
	Service  string          `json:"service"`
	Revision string          `json:"revision"`
	Init     json.RawMessage `json:"initialize"`
	Tools    json.RawMessage `json:"tools"`
}

func (d *Daemon) cachePath(svc, rev string) (string, bool) {
	if !cacheNameRE.MatchString(svc) || !cacheNameRE.MatchString(rev) {
		return "", false
	}
	return filepath.Join(d.dir, ToolsCacheDirName, svc+"-"+rev+".json"), true
}

// cacheStore ghi cache xuống đĩa (0600, nguyên tử). Lỗi chỉ ghi nhật ký: cache đĩa chỉ để tránh spawn upstream.
func (d *Daemon) cacheStore(svc, rev string, e cacheEntry) {
	path, ok := d.cachePath(svc, rev)
	if !ok {
		return
	}
	data, err := json.Marshal(diskEntry{Service: svc, Revision: rev, Init: e.init, Tools: e.tools})
	if err != nil {
		return
	}
	if err := writeFileAtomic(path, data); err != nil {
		d.logf("ghi cache tools/list của %s: %v", svc, err)
	}
}

func (d *Daemon) cacheLoad(svc, rev string) (cacheEntry, bool) {
	path, ok := d.cachePath(svc, rev)
	if !ok {
		return cacheEntry{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cacheEntry{}, false
	}
	var de diskEntry
	if json.Unmarshal(data, &de) != nil || de.Service != svc || de.Revision != rev || len(de.Init) == 0 || len(de.Tools) == 0 {
		return cacheEntry{}, false // tệp hỏng hoặc của revision khác: bỏ, sẽ lấy lại từ upstream
	}
	return cacheEntry{init: de.Init, tools: de.Tools}, true
}

// writeFileAtomic ghi tệp 0600 qua tệp tạm cùng thư mục rồi đổi tên.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil && !isWindows {
		return fail(err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
