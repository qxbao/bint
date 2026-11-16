package iso

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"bint/internal/fsafe"
	"bint/internal/jsondoc"
)

// jsonFile là .claude.json của Claude trong profile: chỉ chạm khoá mức một của section (mcpServers), mọi khoá
// khác (oauthAccount, projects, …) giữ nguyên từng byte giá trị và thứ tự (jsondoc).
type jsonFile struct {
	path     string
	doc      *jsondoc.Doc
	sections map[string]*jsondoc.Doc
	dirty    map[string]bool
}

func openJSON(path string) (*jsonFile, error) {
	f := &jsonFile{path: path, sections: map[string]*jsondoc.Doc{}, dirty: map[string]bool{}}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if f.doc, err = jsondoc.Parse(data); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

func (f *jsonFile) section(section []string, create bool) (*jsondoc.Doc, string, error) {
	if len(section) != 1 {
		return nil, "", fmt.Errorf("%s: section phải có một cấp, nhận %v", f.path, section)
	}
	s := section[0]
	if d, ok := f.sections[s]; ok {
		return d, s, nil
	}
	raw, ok := f.doc.Get(s)
	if !ok {
		if !create {
			return nil, s, nil
		}
		raw = json.RawMessage("{}")
	}
	d, err := jsondoc.Parse(raw)
	if err != nil {
		return nil, s, fmt.Errorf("%s: khoá %s: %w", f.path, s, err)
	}
	f.sections[s] = d
	return d, s, nil
}

func (f *jsonFile) get(section []string, name string) (any, bool, error) {
	d, _, err := f.section(section, false)
	if err != nil || d == nil {
		return nil, false, err
	}
	raw, ok := d.Get(name)
	if !ok {
		return nil, false, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false, fmt.Errorf("%s: %s: %w", f.path, entryKey(section, name), err)
	}
	return v, true, nil
}

func (f *jsonFile) set(section []string, name string, v any) error {
	d, s, err := f.section(section, true)
	if err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	d.Set(name, b)
	f.dirty[s] = true
	return nil
}

func (f *jsonFile) del(section []string, name string) error {
	d, s, err := f.section(section, false)
	if err != nil || d == nil {
		return err
	}
	if _, ok := d.Get(name); !ok {
		return nil
	}
	d.Del(name)
	f.dirty[s] = true
	return nil
}

func (f *jsonFile) save(backupDir, rel string, now time.Time) (bool, error) {
	if len(f.dirty) == 0 {
		return false, nil
	}
	for s := range f.dirty {
		b, err := f.sections[s].MarshalIndent()
		if err != nil {
			return false, err
		}
		f.doc.Set(s, json.RawMessage(bytes.TrimSpace(b)))
	}
	out, err := f.doc.MarshalIndent()
	if err != nil {
		return false, err
	}
	if _, err := fsafe.Backup(f.path, backupDir, rel, now); err != nil {
		return false, err
	}
	if err := fsafe.WriteAtomic(f.path, out, 0o600); err != nil {
		return false, err
	}
	return true, nil
}
