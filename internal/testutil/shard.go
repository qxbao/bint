package testutil

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"bint/internal/fslock"
)

// Chia test của một gói ra nhiều tiến trình con.
//
// Test E2E của Bint bị chặn bởi việc sinh tiến trình (git, sh, cmd), không phải CPU: trên Windows mỗi lần sinh
// tiến trình tốn 20–70 ms nên tiến trình test gần như chỉ ngồi chờ. Không dùng được t.Parallel vì Isolate
// dùng t.Setenv (biến môi trường là trạng thái chung của tiến trình). Cách thay là chạy chính binary test nhiều
// lần song song, mỗi lần một nhóm test top-level: môi trường của mỗi tiến trình con vẫn riêng.
//
// BINT_TEST_PROCS=n đặt số tiến trình con (1 tắt việc chia). Không chia khi lệnh đã lọc hay đo đạc
// (-run, -skip, -list, -bench, cover, profile…): khi đó chạy y như m.Run.

const (
	envShardProcs = "BINT_TEST_PROCS"
	envShardChild = "BINT_TEST_SHARD_CHILD"
	maxShardProcs = 8
)

// noShardFlags: cờ làm việc chia sai nghĩa (lọc test, đo đạc cần một tệp kết quả, fuzz).
var noShardFlags = []string{
	"run", "skip", "list", "bench", "fuzz", "coverprofile", "gocoverdir", "cpuprofile", "memprofile",
	"blockprofile", "mutexprofile", "trace",
}

// shardProcs trả số tiến trình con sẽ dùng; 0 nghĩa là không chia.
func shardProcs(args []string) int {
	if os.Getenv(envShardChild) != "" {
		return 0
	}
	for _, a := range args {
		name := strings.TrimLeft(a, "-")
		if i := strings.IndexByte(name, '='); i >= 0 {
			name = name[:i]
		}
		name = strings.TrimPrefix(name, "test.")
		for _, f := range noShardFlags {
			if name == strings.TrimPrefix(f, "test.") {
				return 0
			}
		}
	}
	n := min(runtime.NumCPU(), maxShardProcs)
	if v := os.Getenv(envShardProcs); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 {
			fmt.Fprintf(os.Stderr, "testutil: %s=%q không hợp lệ, bỏ qua\n", envShardProcs, v)
		} else {
			n = p
		}
	}
	if n < 2 {
		return 0
	}
	return n
}

// RunTests chạy test của gói, chia ra nhiều tiến trình con khi được (xem đầu tệp); trả mã thoát.
func RunTests(m *testing.M) int {
	preferDirectGit()
	procs := shardProcs(os.Args[1:])
	if procs == 0 {
		return m.Run()
	}
	names, err := listTests()
	if err != nil {
		fmt.Fprintf(os.Stderr, "testutil: liệt kê test để chia tiến trình: %v; chạy tuần tự\n", err)
		return m.Run()
	}
	if len(names) < 2 {
		return m.Run()
	}
	return runShards(names, procs)
}

// preferDirectGit: trên Windows, PATH ngoài Git Bash (PowerShell, cmd) tìm thấy Git\cmd\git.exe, một trình bọc tự
// sinh thêm Git\mingw64\bin\git.exe, nên mỗi lệnh git tốn hai lần tạo tiến trình (đo được ~65 ms so với ~33 ms).
// Đưa mingw64\bin lên đầu PATH của tiến trình test để gọi thẳng git thật, giống môi trường Git Bash của CI.
func preferDirectGit() {
	if runtime.GOOS != "windows" {
		return
	}
	exe, err := exec.LookPath("git")
	if err != nil {
		return
	}
	dir := filepath.Dir(exe)
	if !strings.EqualFold(filepath.Base(dir), "cmd") {
		return
	}
	direct := filepath.Join(filepath.Dir(dir), "mingw64", "bin")
	if _, err := os.Stat(filepath.Join(direct, "git.exe")); err != nil {
		return
	}
	os.Setenv("PATH", direct+string(os.PathListSeparator)+os.Getenv("PATH"))
}

var testLine = regexp.MustCompile(`^(Test|Example|Fuzz)\w*$`)

// listTests liệt kê test top-level bằng chính binary (-test.list), theo thứ tự khai báo.
func listTests() ([]string, error) {
	cmd := exec.Command(os.Args[0], "-test.list=.*")
	cmd.Env = append(os.Environ(), envShardChild+"=1")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		if testLine.MatchString(l) && !strings.HasPrefix(l, "Fuzz") {
			names = append(names, l)
		}
	}
	return names, nil
}

// testlogFlag là cờ go test truyền để ghi các đầu vào (biến môi trường, tệp) của lần chạy, dùng cho bộ nhớ đệm kết
// quả test. Mỗi mẻ ghi nhật ký riêng; tiến trình cha gộp lại vào tệp go test chờ (xem mergeTestlogs).
const testlogFlag = "-test.testlogfile="

// runShards phân test thành nhiều mẻ nhỏ cho procs tiến trình con lấy dần (mẻ nhỏ để test dài không dồn vào một
// tiến trình). Đầu ra mỗi mẻ được in nguyên khối khi mẻ xong, bỏ dòng PASS/FAIL cuối; dòng kết thúc của gói in một
// lần ở cuối để go test và test2json đọc như một lần chạy.
func runShards(names []string, procs int) int {
	procs = min(procs, len(names))
	size := max(1, len(names)/(procs*6))
	var batches [][]string
	for i := 0; i < len(names); i += size {
		batches = append(batches, names[i:min(i+size, len(names))])
	}
	failfast := hasFlag(os.Args[1:], "failfast")
	args, testlog := splitTestlog(os.Args[1:])
	var logDir string
	if testlog != "" {
		dir, err := os.MkdirTemp("", "bint-testlog-")
		if err != nil {
			fmt.Fprintf(os.Stderr, "testutil: %v\n", err)
			return 1
		}
		defer os.RemoveAll(dir)
		logDir = dir
	}

	var (
		mu     sync.Mutex
		failed bool
		next   int
		wg     sync.WaitGroup
	)
	take := func() []string {
		mu.Lock()
		defer mu.Unlock()
		if next >= len(batches) || (failed && failfast) {
			return nil
		}
		b := batches[next]
		next++
		return b
	}
	for range procs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for b := take(); b != nil; b = take() {
				out, ok := runBatch(args, b, logDir)
				mu.Lock()
				os.Stdout.Write(out)
				if !ok {
					failed = true
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if testlog != "" {
		if err := mergeTestlogs(testlog, logDir); err != nil {
			fmt.Fprintf(os.Stderr, "testutil: gộp nhật ký đầu vào test: %v\n", err)
			failed = true
		}
	}
	if failed {
		fmt.Println("FAIL")
		return 1
	}
	fmt.Println("PASS")
	return 0
}

// envShardSlots đặt tổng số tiến trình con được chạy cùng lúc trên cả máy, cộng mọi gói (go test ./... chạy nhiều
// gói song song, gói nào cũng chia). Mặc định max(4, số CPU/2): quá tải làm test có hạn chờ (TUI, codegraph) hỏng.
const envShardSlots = "BINT_TEST_SLOTS"

func shardSlots() int {
	n := max(4, runtime.NumCPU()/2)
	if v := os.Getenv(envShardSlots); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p >= 1 {
			n = p
		}
	}
	return n
}

// acquireSlot giữ một chỗ chạy chung của cả máy: khoá tệp %TEMP%/bint-test-slots/<k> (fslock, hệ điều hành tự
// nhả khi tiến trình chết). Không xin được chỗ nào thì chờ; lỗi mở tệp thì chạy luôn, không chặn test.
func acquireSlot() (release func()) {
	dir := filepath.Join(os.TempDir(), "bint-test-slots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return func() {}
	}
	total := shardSlots()
	for delay := 5 * time.Millisecond; ; delay = min(delay*2, 100*time.Millisecond) {
		for k := range total {
			f, err := os.OpenFile(filepath.Join(dir, strconv.Itoa(k)), os.O_CREATE|os.O_RDWR, 0o644)
			if err != nil {
				return func() {}
			}
			busy, err := fslock.Try(f, true)
			if err == nil && !busy {
				return func() { f.Close() }
			}
			f.Close()
			if err != nil {
				return func() {}
			}
		}
		time.Sleep(delay)
	}
}

// runBatch chạy một mẻ test trong tiến trình con với đúng cờ của tiến trình cha.
func runBatch(base, names []string, logDir string) ([]byte, bool) {
	defer acquireSlot()()
	args := append([]string{}, base...)
	if logDir != "" {
		args = append(args, testlogFlag+batchLog(logDir, names[0]))
	}
	// Một đối số "-test.run=…": test gọi CLI không đối số có thể khiến cobra đọc os.Args; cobra bỏ qua cờ -test.*
	// nhưng giá trị tách rời sẽ thành đối số vị trí.
	args = append(args, "-test.run=^("+strings.Join(names, "|")+")$")
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), envShardChild+"=1")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return trimResultLine(buf.Bytes()), err == nil
}

// trimResultLine bỏ dòng kết quả gói ("PASS"/"FAIL", có thể kèm byte khung \x16 của test2json) ở cuối đầu ra.
func trimResultLine(out []byte) []byte {
	body := bytes.TrimRight(out, "\r\n")
	i := bytes.LastIndexByte(body, '\n')
	last := bytes.TrimLeft(body[i+1:], "\x16")
	if string(last) == "PASS" || string(last) == "FAIL" {
		return out[:i+1]
	}
	return out
}

func batchLog(dir, first string) string { return filepath.Join(dir, first+".log") }

// splitTestlog tách cờ -test.testlogfile khỏi danh sách cờ, trả về cờ còn lại và đường dẫn nhật ký.
func splitTestlog(args []string) (rest []string, path string) {
	for _, a := range args {
		if strings.HasPrefix(a, testlogFlag) {
			path = strings.TrimPrefix(a, testlogFlag)
			continue
		}
		rest = append(rest, a)
	}
	return rest, path
}

// mergeTestlogs ghi nhật ký đầu vào gộp từ mọi mẻ, đã rút gọn (xem compactTestlog). Mỗi tiến trình con bắt đầu ở
// thư mục gói, nên đường dẫn tương đối của từng mẻ được tính theo thư mục gói và các dòng chdir của chính mẻ đó.
func mergeTestlogs(path, dir string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	c := newTestlogCompactor(cwd, os.TempDir())
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		c.add(string(b))
	}
	return os.WriteFile(path, c.buf.Bytes(), 0o644)
}

// testlogCompactor rút gọn nhật ký đầu vào test trước khi go test lưu nó.
//
// Mỗi lần lưu và mỗi lần tra bộ nhớ đệm kết quả test, go test duyệt từng dòng open/stat; đường dẫn nằm ngoài
// module bị bỏ qua nhưng chỉ sau hai lần filepath.EvalSymlinks (search.InDir), rất đắt trên Windows. Test E2E của
// Bint mở/stat hàng trăm nghìn tệp trong repo tạm, nên nhật ký gói workflow lên ~1,1 triệu dòng và go test ngồi tính
// hơn 20 phút với một nhân CPU, không có tiến trình con, trông như treo. Rút gọn:
//   - bỏ open/stat trong thư mục tạm (repo tạm của test, nằm ngoài module nên go test vốn không dùng làm đầu vào);
//   - đổi đường dẫn tương đối thành tuyệt đối rồi bỏ dòng trùng (getenv lặp lại theo từng test).
//
// Nếu chính module nằm trong thư mục tạm thì không bỏ dòng nào theo thư mục tạm.
type testlogCompactor struct {
	cwd, tmp string
	seen     map[string]bool
	buf      bytes.Buffer
}

func newTestlogCompactor(cwd, tmp string) *testlogCompactor {
	tmp = filepath.Clean(tmp)
	if tmp == "" || tmp == "." || underDir(cwd, tmp) {
		tmp = ""
	}
	c := &testlogCompactor{cwd: cwd, tmp: tmp, seen: map[string]bool{}}
	c.buf.WriteString(testlogMagic)
	c.emit("chdir " + cwd)
	return c
}

func (c *testlogCompactor) emit(line string) {
	if c.seen[line] {
		return
	}
	c.seen[line] = true
	c.buf.WriteString(line)
	c.buf.WriteByte('\n')
}

// add thêm nhật ký của một mẻ.
func (c *testlogCompactor) add(log string) {
	pwd := c.cwd
	for line := range strings.SplitSeq(strings.TrimPrefix(log, testlogMagic), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		op, name, ok := strings.Cut(line, " ")
		switch {
		case !ok:
			c.emit(line) // giữ nguyên: go test tự coi nhật ký hỏng là không dùng được bộ nhớ đệm
		case op == "chdir":
			pwd = name
			c.emit(line)
		case op == "open" || op == "stat":
			if !filepath.IsAbs(name) {
				name = filepath.Join(pwd, name)
			}
			if c.tmp != "" && underDir(name, c.tmp) {
				continue
			}
			c.emit(op + " " + name)
		default:
			c.emit(line)
		}
	}
}

// underDir cho biết path nằm trong dir (hoặc chính là dir), so theo từ vựng; trên Windows filepath.Rel không phân
// biệt hoa thường.
func underDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// testlogMagic: dòng đầu nhật ký đầu vào (testing/internal/testdeps, cmd/go/internal/test).
const testlogMagic = "# test log\n"

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		name := strings.TrimPrefix(strings.TrimLeft(a, "-"), "test.")
		if name == flag || name == flag+"=true" {
			return true
		}
	}
	return false
}
