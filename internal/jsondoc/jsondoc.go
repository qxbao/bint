// Package jsondoc sửa tệp cấu hình JSON của harness mà vẫn giữ thứ tự các khoá cấp
// cao nhất, để thay đổi của Bint ít xáo trộn tệp của người dùng.
package jsondoc

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Doc là một JSON object với thứ tự khoá cấp cao nhất được giữ nguyên.
type Doc struct {
	keys []string
	vals map[string]json.RawMessage
}

// Parse đọc raw (rỗng = object rỗng). Lỗi nếu không phải JSON object.
func Parse(raw []byte) (*Doc, error) {
	d := &Doc{vals: map[string]json.RawMessage{}}
	if len(bytes.TrimSpace(raw)) == 0 {
		return d, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("tệp không phải JSON object")
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		k := kt.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		if _, dup := d.vals[k]; !dup {
			d.keys = append(d.keys, k)
		}
		d.vals[k] = v
	}
	return d, nil
}

// Len là số khoá cấp cao nhất.
func (d *Doc) Len() int { return len(d.keys) }

// Get trả về giá trị thô của khoá k.
func (d *Doc) Get(k string) (json.RawMessage, bool) {
	v, ok := d.vals[k]
	return v, ok
}

// Set gán giá trị thô; khoá mới được thêm vào cuối.
func (d *Doc) Set(k string, v json.RawMessage) {
	if _, ok := d.vals[k]; !ok {
		d.keys = append(d.keys, k)
	}
	d.vals[k] = v
}

// SetValue mã hoá v rồi gán cho khoá k.
func (d *Doc) SetValue(k string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	d.Set(k, b)
	return nil
}

// Del xoá khoá k (không có thì thôi).
func (d *Doc) Del(k string) {
	if _, ok := d.vals[k]; !ok {
		return
	}
	delete(d.vals, k)
	for i, kk := range d.keys {
		if kk == k {
			d.keys = append(d.keys[:i], d.keys[i+1:]...)
			break
		}
	}
}

// MarshalIndent mã hoá lại với thụt lề 2 dấu cách, kết thúc bằng dòng mới.
func (d *Doc) MarshalIndent() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("{")
	for i, k := range d.keys {
		if i > 0 {
			buf.WriteString(",")
		}
		kb, _ := json.Marshal(k)
		buf.WriteString("\n  ")
		buf.Write(kb)
		buf.WriteString(": ")
		var ind bytes.Buffer
		if err := json.Indent(&ind, d.vals[k], "  ", "  "); err != nil {
			return nil, err
		}
		buf.Write(ind.Bytes())
	}
	if len(d.keys) > 0 {
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}

// Normalize trả về dạng chuẩn hoá (để so sánh hai bản có khác nhau về nội dung không).
func Normalize(b []byte) []byte {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return b
	}
	out, _ := json.Marshal(v)
	return out
}

// Equal cho biết hai tệp JSON có cùng nội dung.
func Equal(a, b []byte) bool { return bytes.Equal(Normalize(a), Normalize(b)) }
