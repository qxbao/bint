package flow

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaxFileBytes là trần kích thước một tệp flow.
const MaxFileBytes = 64 << 10

// ErrNotFound: không có flow với tên đó (không nhúng, không có tệp trong .bint/flows).
var ErrNotFound = errors.New("không có flow")

// Decode giải mã một tệp flow. Khoá YAML lạ (ở gốc hay ở bước) là lỗi; tệp rỗng hay có nhiều tài liệu YAML cũng vậy.
// Chỉ giải mã, chưa kiểm luật: dùng Validate.
func Decode(b []byte) (Def, error) {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var d Def
	if err := dec.Decode(&d); err != nil {
		if errors.Is(err, io.EOF) {
			return Def{}, errors.New("tệp rỗng")
		}
		return Def{}, err
	}
	var extra yaml.Node
	switch err := dec.Decode(&extra); {
	case errors.Is(err, io.EOF):
	case err != nil:
		return Def{}, err
	default:
		return Def{}, errors.New("tệp chỉ được có một tài liệu YAML")
	}
	return d, nil
}

// File là một tệp trong .bint/flows. Err khác nil khi tệp không đọc/giải mã được (khi đó Def rỗng).
type File struct {
	Name string // tên tệp không đuôi
	Path string // tương đối với gốc repository, dấu '/'
	Def  Def
	Err  error
}

// Load đọc mọi tệp .bint/flows/*.yml (sắp theo tên, cùng tên thì .yml đứng trước .yaml). Mỗi tệp hỏng mang lỗi riêng,
// không chặn tệp khác. Thư mục không tồn tại → rỗng, không lỗi. Tệp .yaml (đổi đuôi, hoặc trùng với tệp .yml cùng
// tên), symlink, tệp không thường và tệp quá MaxFileBytes là lỗi của tệp đó.
func Load(root string) ([]File, error) {
	dir := filepath.Join(root, filepath.FromSlash(Rel))
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []File
	ymlFiles := map[string]File{} // tệp .yml theo tên, để .yaml trùng tên biết bản .yml có lỗi không
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".") || e.IsDir() || filepath.Ext(n) != ".yml" {
			continue
		}
		f := File{Name: strings.TrimSuffix(n, ".yml"), Path: path.Join(Rel, n)}
		f.Def, f.Err = loadFile(filepath.Join(dir, n))
		ymlFiles[f.Name] = f
		out = append(out, f)
	}
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".") || e.IsDir() || filepath.Ext(n) != ".yaml" {
			continue
		}
		f := File{Name: strings.TrimSuffix(n, ".yaml"), Path: path.Join(Rel, n)}
		switch y, dup := ymlFiles[f.Name]; {
		case dup && y.Err != nil:
			// Bản .yml cũng lỗi: khuyên xoá nó sẽ làm mất bản người đang sửa dở; sửa .yml rồi mới bỏ .yaml.
			f.Err = fmt.Errorf("trùng với %s.yml (tra theo tên luôn dùng bản .yml), mà bản .yml cũng lỗi: sửa %s.yml rồi xoá %s, "+
				"hoặc xoá %s.yml rồi đổi tên %s thành %s.yml", f.Name, f.Name, n, f.Name, n, f.Name)
		case dup:
			f.Err = fmt.Errorf("trùng với %s.yml (tra theo tên luôn dùng bản .yml; xoá %s)", f.Name, n)
		default:
			f.Err = fmt.Errorf("tệp flow dùng đuôi .yml (đổi tên %s thành %s.yml)", n, f.Name)
		}
		out = append(out, f)
	}
	// Thứ tự xác định: theo tên, cùng tên thì .yml trước .yaml (Find lấy bản đầu khớp tên nên luôn là .yml).
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return strings.HasSuffix(out[i].Path, ".yml") && !strings.HasSuffix(out[j].Path, ".yml")
	})
	return out, nil
}

func loadFile(p string) (Def, error) {
	fi, err := os.Lstat(p)
	switch {
	case err != nil:
		return Def{}, err
	case !fi.Mode().IsRegular():
		return Def{}, errors.New("không phải tệp thường (symlink hoặc loại khác): bị từ chối")
	case fi.Size() > MaxFileBytes:
		return Def{}, fmt.Errorf("tệp quá lớn (%d byte, tối đa %d)", fi.Size(), MaxFileBytes)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Def{}, err
	}
	return Decode(b)
}

// Find tra flow theo tên: flow nhúng trước (File.Path rỗng), rồi .bint/flows/<tên>.yml. Khi có cả <tên>.yml và
// <tên>.yaml thì luôn trả bản .yml. File.Err là lỗi nạp riêng của tệp (Def rỗng); flow tuỳ biến chưa được kiểm luật
// (dùng Validate). Không có → lỗi bọc ErrNotFound.
func Find(root, name string) (File, error) {
	if d, ok := GetBuiltin(name); ok {
		return File{Name: name, Def: d}, nil
	}
	if name == "" || strings.HasPrefix(name, ".") || strings.ContainsAny(name, `/\`) {
		return File{}, fmt.Errorf("%w %q", ErrNotFound, name)
	}
	files, err := Load(root)
	if err != nil {
		return File{}, err
	}
	for _, f := range files {
		if f.Name == name {
			return f, nil
		}
	}
	return File{}, fmt.Errorf("%w %q", ErrNotFound, name)
}

// Get tra flow theo tên như Find; tệp không nạp được là lỗi (không phải ErrNotFound).
func Get(root, name string) (Def, error) {
	f, err := Find(root, name)
	if err != nil {
		return Def{}, err
	}
	if f.Err != nil {
		return Def{}, fmt.Errorf("%s: %w", f.Path, f.Err)
	}
	return f.Def, nil
}
