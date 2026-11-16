package iso

import "strings"

// envList là danh sách KEY=VALUE giữ thứ tự, tra khoá theo quy tắc của OS: Windows không phân biệt
// hoa thường ("Path" = "PATH"), Unix phân biệt. goos tách khỏi runtime.GOOS để test nhánh Windows trên Linux.
type envList struct {
	goos string
	ents []envEnt
	idx  map[string]int
}

type envEnt struct {
	key, val string
	dead     bool
}

func foldKey(goos, k string) string {
	if goos == "windows" {
		return strings.ToUpper(k)
	}
	return k
}

// cutEnv tách "K=V". Windows có biến bắt đầu bằng '=' ("=C:=C:\x"): khoá chứa dấu '=' đầu.
func cutEnv(s string) (k, v string, ok bool) {
	if s == "" {
		return "", "", false
	}
	i := strings.IndexByte(s[1:], '=')
	if i < 0 {
		return "", "", false
	}
	return s[:i+1], s[i+2:], true
}

func newEnvList(goos string, env []string) *envList {
	e := &envList{goos: goos, idx: map[string]int{}}
	for _, s := range env {
		if k, v, ok := cutEnv(s); ok {
			e.set(k, v)
		}
	}
	return e
}

func (e *envList) get(k string) (string, bool) {
	if i, ok := e.idx[foldKey(e.goos, k)]; ok && !e.ents[i].dead {
		return e.ents[i].val, true
	}
	return "", false
}

// val trả giá trị hoặc "" khi không đặt.
func (e *envList) val(k string) string { v, _ := e.get(k); return v }

func (e *envList) set(k, v string) {
	f := foldKey(e.goos, k)
	if i, ok := e.idx[f]; ok {
		// Tên do iso đặt lấy chính tả chuẩn (Windows: "Temp" thành "TEMP").
		e.ents[i] = envEnt{key: k, val: v}
		return
	}
	e.idx[f] = len(e.ents)
	e.ents = append(e.ents, envEnt{key: k, val: v})
}

func (e *envList) del(k string) {
	if i, ok := e.idx[foldKey(e.goos, k)]; ok {
		e.ents[i].dead = true
	}
}

// delPrefix gỡ mọi biến có tên bắt đầu bằng prefix (so theo quy tắc của OS).
func (e *envList) delPrefix(prefix string) {
	p := foldKey(e.goos, prefix)
	for i := range e.ents {
		if strings.HasPrefix(foldKey(e.goos, e.ents[i].key), p) {
			e.ents[i].dead = true
		}
	}
}

func (e *envList) list() []string {
	out := make([]string, 0, len(e.ents))
	for _, en := range e.ents {
		if !en.dead {
			out = append(out, en.key+"="+en.val)
		}
	}
	return out
}

// delPrefixList trả các khoá còn sống có tiền tố prefix (dùng trong test).
func (e *envList) delPrefixList(prefix string) []string {
	var out []string
	p := foldKey(e.goos, prefix)
	for _, en := range e.ents {
		if !en.dead && strings.HasPrefix(foldKey(e.goos, en.key), p) {
			out = append(out, en.key)
		}
	}
	return out
}
