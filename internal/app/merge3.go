package app

import (
	"strings"
)

// Dấu xung đột do gộp ba chiều chèn vào bản tạm. CheckConfigEdit từ chối nội dung còn các dòng này.
const (
	ConflictMine   = "<<<<<<< bint: bản sửa của bạn"
	ConflictSep    = "======="
	ConflictTheirs = ">>>>>>> bint: tệp gốc mới"
)

// HasConflictMarkers cho biết data còn dấu xung đột do merge3 chèn (khớp nguyên dòng đầu/cuối của khối).
func HasConflictMarkers(data []byte) bool {
	mine, theirs := false, false
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimRight(l, "\r")
		switch l {
		case ConflictMine:
			mine = true
		case ConflictTheirs:
			theirs = mine
		}
	}
	return mine && theirs
}

// mergeLines tách s thành các dòng giữ nguyên ký tự xuống dòng (dòng cuối có thể không có).
func mergeLines(s string) []string {
	out := strings.SplitAfter(s, "\n")
	if n := len(out); n > 0 && out[n-1] == "" {
		out = out[:n-1]
	}
	return out
}

// matchLines trả các cặp chỉ số (a[i] == b[j]) của một dãy con chung dài nhất, tăng dần cả hai chỉ số.
func matchLines(a, b []string) [][2]int {
	n, m := len(a), len(b)
	// Cắt phần đầu/cuối giống nhau để bảng quy hoạch động nhỏ trong trường hợp thường gặp (sửa vài dòng).
	pre := 0
	for pre < n && pre < m && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < n-pre && suf < m-pre && a[n-1-suf] == b[m-1-suf] {
		suf++
	}
	var out [][2]int
	for i := 0; i < pre; i++ {
		out = append(out, [2]int{i, i})
	}
	am, bm := a[pre:n-suf], b[pre:m-suf]
	if len(am) > 0 && len(bm) > 0 {
		dp := make([][]int32, len(am)+1)
		for i := range dp {
			dp[i] = make([]int32, len(bm)+1)
		}
		for i := len(am) - 1; i >= 0; i-- {
			for j := len(bm) - 1; j >= 0; j-- {
				if am[i] == bm[j] {
					dp[i][j] = dp[i+1][j+1] + 1
				} else {
					dp[i][j] = max(dp[i+1][j], dp[i][j+1])
				}
			}
		}
		for i, j := 0, 0; i < len(am) && j < len(bm); {
			switch {
			case am[i] == bm[j]:
				out = append(out, [2]int{pre + i, pre + j})
				i++
				j++
			case dp[i+1][j] >= dp[i][j+1]:
				i++
			default:
				j++
			}
		}
	}
	for i := 0; i < suf; i++ {
		out = append(out, [2]int{n - suf + i, m - suf + i})
	}
	return out
}

// Merge3 gộp ba chiều theo dòng: base là mốc chung, ours là bản của người dùng, theirs là bản mới của tệp gốc.
// Vùng chỉ một bên đổi thì lấy bên đó; hai bên đổi giống nhau thì lấy một; hai bên đổi khác nhau thì chèn khối
// xung đột (ConflictMine ... ConflictSep ... ConflictTheirs). conflicts là số khối xung đột.
func Merge3(base, ours, theirs string) (merged string, conflicts int) {
	b, o, t := mergeLines(base), mergeLines(ours), mergeLines(theirs)
	bo, bt := map[int]int{}, map[int]int{}
	for _, p := range matchLines(b, o) {
		bo[p[0]] = p[1]
	}
	for _, p := range matchLines(b, t) {
		bt[p[0]] = p[1]
	}
	var sb strings.Builder
	write := func(lines []string) {
		for _, l := range lines {
			sb.WriteString(l)
		}
	}
	eq := func(x, y []string) bool {
		if len(x) != len(y) {
			return false
		}
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	}
	nl := func(lines []string) []string { // bảo đảm dòng cuối có xuống dòng trước khi nối dấu
		if len(lines) > 0 && !strings.HasSuffix(lines[len(lines)-1], "\n") {
			out := append([]string(nil), lines...)
			out[len(out)-1] += "\n"
			return out
		}
		return lines
	}
	region := func(bc, oc, tc []string) {
		switch {
		case eq(oc, bc):
			write(tc)
		case eq(tc, bc), eq(oc, tc):
			write(oc)
		default:
			if m, ok := mergeHunks(bc, oc, tc); ok {
				write(m)
				return
			}
			conflicts++
			sb.WriteString(ConflictMine + "\n")
			write(nl(oc))
			sb.WriteString(ConflictSep + "\n")
			write(nl(tc))
			sb.WriteString(ConflictTheirs + "\n")
		}
	}
	bi, oi, ti := 0, 0, 0
	for k := 0; k < len(b); k++ {
		ok, okO := bo[k]
		tk, okT := bt[k]
		if !okO || !okT {
			continue // dòng mốc bị một bên sửa/xoá: thuộc vùng không ổn định kế tiếp
		}
		region(b[bi:k], o[oi:ok], t[ti:tk])
		write(o[ok : ok+1]) // dòng ổn định (giống nhau ở cả ba)
		bi, oi, ti = k+1, ok+1, tk+1
	}
	region(b[bi:], o[oi:], t[ti:])
	return sb.String(), conflicts
}

// hunk là một đoạn thay thế duy nhất của bản đổi so với mốc: mốc[a:b] thành repl.
type hunk struct {
	a, b int
	repl []string
}

// diffHunk tìm đoạn thay thế duy nhất bằng cách bỏ phần đầu và phần cuối giống nhau.
func diffHunk(base, edit []string) hunk {
	p := 0
	for p < len(base) && p < len(edit) && base[p] == edit[p] {
		p++
	}
	s := 0
	for s < len(base)-p && s < len(edit)-p && base[len(base)-1-s] == edit[len(edit)-1-s] {
		s++
	}
	return hunk{a: p, b: len(base) - s, repl: edit[p : len(edit)-s]}
}

// mergeHunks gộp hai bản đổi mà mỗi bản chỉ là MỘT đoạn thay thế và hai đoạn không chồng lên nhau (kể cả liền kề,
// ví dụ một bên sửa dòng cuối của khối, bên kia thêm dòng ngay sau đó). Hai đoạn chèn tại cùng một chỗ, hoặc đoạn
// chồng nhau, vẫn là xung đột (ok = false).
func mergeHunks(base, ours, theirs []string) ([]string, bool) {
	h1, h2 := diffHunk(base, ours), diffHunk(base, theirs)
	if h2.a < h1.a || (h2.a == h1.a && h2.b < h1.b) {
		h1, h2 = h2, h1
	}
	if h1.b > h2.a || (h1.a == h2.a && h1.b == h1.a && h2.b == h2.a) {
		return nil, false
	}
	var out []string
	out = append(out, base[:h1.a]...)
	out = append(out, h1.repl...)
	out = append(out, base[h1.b:h2.a]...)
	out = append(out, h2.repl...)
	out = append(out, base[h2.b:]...)
	return out, true
}
