package mcpgw

import (
	"net/http"
	"strings"
	"testing"
)

// /admin/log trả stderr của upstream đang chạy, đã che credential; service lạ bị từ chối; chưa có phân vùng thì rỗng.
func TestAdminLog(t *testing.T) {
	h := newHarness(t, nil)
	writeSecrets(t, h, "JIRA_MCP_URL=http://j", "JIRA_MCP_USERNAME=u", "JIRA_MCP_PASSWORD=pw-trong-log")
	h.register("jira", "errmsg=1")
	ac, err := Connect(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	// Chưa mở phiên nào: chưa có phân vùng.
	l, err := ac.Log(t.Context(), "jira")
	if err != nil || l == nil || len(l) != 0 {
		t.Fatalf("chưa có phân vùng: %+v %v (muốn [] không lỗi)", l, err)
	}
	h.open("jira", nil).call(1, "jira_issue_get", nil)
	eventually(t, "stderr của upstream", 5e9, func() bool {
		l, _ = ac.Log(t.Context(), "jira")
		return len(l) == 1 && strings.Contains(l[0].Stderr, "lỗi thứ hai")
	})
	if !l[0].Running || l[0].Partition == "" {
		t.Errorf("phân vùng: %+v", l[0])
	}
	if strings.Contains(l[0].Stderr, "pw-trong-log") || !strings.Contains(l[0].Stderr, "warn ***") {
		t.Errorf("stderr phải che credential, giữ ngắt dòng: %q", l[0].Stderr)
	}
	if !strings.Contains(l[0].Stderr, "\n") {
		t.Errorf("mất ngắt dòng: %q", l[0].Stderr)
	}
	// Service lạ: 400. Không token: 401.
	if _, err := ac.Log(t.Context(), "khong-co"); err == nil || !strings.Contains(err.Error(), "400") {
		t.Errorf("service lạ: %v, muốn lỗi 400", err)
	}
	resp, err := ac.HTTP.Get(ac.Info.URL() + "/admin/log?service=jira")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("không token: %d, muốn 401", resp.StatusCode)
	}
	// Service hợp lệ mà không có phân vùng nào: danh sách rỗng, không lỗi.
	if l, err := ac.Log(t.Context(), "deepwiki"); err != nil || len(l) != 0 {
		t.Errorf("service chưa chạy: %+v %v", l, err)
	}
}

// tailBuffer bị cắt thì Lines bỏ dòng đầu (có thể chỉ còn đuôi một credential); chưa cắt thì giữ nguyên.
func TestTailBufferLinesDropsCutFirstLine(t *testing.T) {
	b := &tailBuffer{max: 12}
	b.Write([]byte("abc\ndef\n"))
	if got := b.Lines(); got != "abc\ndef\n" {
		t.Fatalf("chưa cắt: %q", got)
	}
	b.Write([]byte("SECRETVALUE\nok\n")) // tổng 23 byte > 12: đầu bộ đệm còn "RETVALUE", giữa dòng
	if got := b.Lines(); got != "ok\n" {
		t.Fatalf("đã cắt: %q, muốn chỉ còn các dòng đủ", got)
	}
	nb := &tailBuffer{max: 4}
	nb.Write([]byte("abcdefgh"))
	if got := nb.Lines(); got != "" {
		t.Fatalf("cắt mà không có ngắt dòng: %q, muốn rỗng", got)
	}
}
