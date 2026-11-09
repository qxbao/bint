package knowledge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bint/internal/fsafe"
	"bint/internal/ulid"
)

// ItemsRel là thư mục chứa tệp tri thức, tương đối với thư mục gốc repository.
const ItemsRel = ".bint/knowledge/items"

// Store đọc/ghi tệp tri thức trong một thư mục items.
type Store struct {
	Dir string
}

// NewStore trả về kho tri thức của repository có thư mục gốc root.
func NewStore(root string) Store {
	return Store{Dir: filepath.Join(root, filepath.FromSlash(ItemsRel))}
}

// LoadError là một tệp không đọc được; List vẫn trả về các mục còn lại.
type LoadError struct {
	Path string
	Err  error
}

func (e LoadError) Error() string { return fmt.Sprintf("%s: %v", filepath.Base(e.Path), e.Err) }

// List đọc mọi mục, sắp theo ID (tức theo thời điểm tạo). Tệp hỏng được trả về
// riêng để người dùng sửa, không làm hỏng cả danh sách.
func (s Store) List() ([]*Item, []LoadError, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var items []*Item
	var bad []LoadError
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := filepath.Join(s.Dir, e.Name())
		it, err := ReadFile(p)
		if err != nil {
			bad = append(bad, LoadError{Path: p, Err: err})
			continue
		}
		if want := strings.TrimSuffix(e.Name(), ".md"); it.ID != want {
			bad = append(bad, LoadError{Path: p, Err: fmt.Errorf("id %q không khớp tên tệp", it.ID)})
			continue
		}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, bad, nil
}

// ReadFile đọc một tệp tri thức.
func ReadFile(p string) (*Item, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	it, err := Parse(data)
	if err != nil {
		return nil, err
	}
	it.Path = p
	return it, nil
}

// PathFor trả về đường dẫn tệp của ID.
func (s Store) PathFor(id string) string { return filepath.Join(s.Dir, id+".md") }

// Save ghi mục (nguyên tử). Mục mới chưa có ID thì được cấp ULID.
func (s Store) Save(it *Item) error {
	if it.ID == "" {
		it.ID = ulid.New()
	}
	if problems := it.Validate(); len(problems) > 0 {
		return fmt.Errorf("mục %s không hợp lệ:\n  - %s", it.ID, strings.Join(problems, "\n  - "))
	}
	data, err := it.Marshal()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	it.Path = s.PathFor(it.ID)
	return fsafe.WriteAtomic(it.Path, data, 0o644)
}

// Remove xoá tệp của mục.
func (s Store) Remove(id string) error { return os.Remove(s.PathFor(id)) }

// Find chọn đúng một mục theo ID đầy đủ, tiền tố hoặc hậu tố (>= 4 ký tự, không
// phân biệt hoa thường). ULID tạo gần nhau có chung tiền tố thời gian nên hậu tố
// thường ngắn hơn để gõ.
func Find(items []*Item, ref string) (*Item, error) {
	ref = strings.ToUpper(strings.TrimSpace(ref))
	if len(ref) < 4 {
		return nil, fmt.Errorf("ID %q quá ngắn (cần ít nhất 4 ký tự)", ref)
	}
	var hits []*Item
	for _, it := range items {
		if it.ID == ref {
			return it, nil
		}
		if strings.HasPrefix(it.ID, ref) || strings.HasSuffix(it.ID, ref) {
			hits = append(hits, it)
		}
	}
	switch len(hits) {
	case 0:
		return nil, fmt.Errorf("không có mục tri thức %q", ref)
	case 1:
		return hits[0], nil
	}
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	return nil, fmt.Errorf("ID %q mơ hồ: %s", ref, strings.Join(ids, ", "))
}

// Now là định dạng thời gian dùng trong tệp tri thức.
func Now(t time.Time) string { return t.UTC().Format(time.RFC3339) }
