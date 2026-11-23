package workflow

import (
	"slices"
	"strings"
	"time"

	"bint/internal/affected"
	"bint/internal/task"
)

// Lệnh chọn lọc do bgraph dựng chỉ là `go test <gói>…`, không mang cờ của lệnh kiểm chứng của phiếu. Lệnh kiểm chứng
// `go test -timeout 30m ./...` mà kiểm hẹp chạy `go test ./a ./b` thì mỗi gói dùng trần mặc định 10 phút của go test: gói chậm (workflow,
// tui/app trên Windows, nhất là khi agent và thăm dò chạy song song) chết ở `panic: test timed out after 10m0s` và mọi lần kiểm hẹp đều đỏ.
// withRunnerArgs chép cờ đó (và workflow.verify.runner_args cho mọi runner) sang các lệnh của phần chọn.

// goValued: cờ của go test/go build nhận giá trị, viết được dạng rời (`-timeout 30m`).
var goValued = map[string]bool{
	"-timeout": true, "-count": true, "-p": true, "-parallel": true, "-cpu": true, "-tags": true, "-run": true, "-skip": true,
	"-bench": true, "-benchtime": true, "-fuzz": true, "-fuzztime": true, "-list": true, "-shuffle": true, "-vet": true, "-exec": true,
	"-covermode": true, "-coverpkg": true, "-coverprofile": true, "-cpuprofile": true, "-memprofile": true, "-blockprofile": true,
	"-mutexprofile": true, "-trace": true, "-outputdir": true, "-o": true, "-C": true, "-mod": true, "-ldflags": true, "-gcflags": true,
	"-asmflags": true, "-modfile": true, "-overlay": true, "-pgo": true, "-buildvcs": false,
}

// goNoInherit: cờ chọn test hoặc ghi tệp riêng của lệnh gốc: không chép sang lệnh chọn lọc (lệnh đó tự chọn gói; tệp xuất sẽ bị các lô
// ghi đè lẫn nhau).
var goNoInherit = map[string]bool{
	"-run": true, "-skip": true, "-bench": true, "-fuzz": true, "-list": true, "-coverprofile": true, "-cpuprofile": true,
	"-memprofile": true, "-blockprofile": true, "-mutexprofile": true, "-trace": true, "-outputdir": true, "-o": true, "-C": true,
}

// goTestFlags trả cờ chép được của lệnh `go test` thuần cmd, chuẩn hoá về dạng `-tên=giá_trị` (cờ bool giữ nguyên). ok=false khi cmd không
// phải `go test …` thuần (có dấu nháy, ký tự shell, lệnh khác đứng trước): khi đó không chép gì.
func goTestFlags(cmd string) (flags []string, ok bool) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" || strings.ContainsAny(cmd, "&|;<>$`'\"()\\\n") {
		return nil, false
	}
	f := strings.Fields(cmd)
	if len(f) < 2 || f[0] != "go" || f[1] != "test" {
		return nil, false
	}
	for i := 2; i < len(f); i++ {
		a := f[i]
		if !strings.HasPrefix(a, "-") {
			continue // mẫu gói, {test_dirs}
		}
		if strings.HasPrefix(a, "--") {
			a = a[1:]
		}
		name, val, hasVal := strings.Cut(a, "=")
		if !hasVal && goValued[name] && i+1 < len(f) {
			val, hasVal = f[i+1], true
			i++
		}
		if goNoInherit[name] {
			continue
		}
		if hasVal {
			flags = append(flags, name+"="+val)
		} else {
			flags = append(flags, name)
		}
	}
	return flags, true
}

// goFlagName là tên cờ của a (`--x=1` và `-x` cùng là `-x`).
func goFlagName(a string) string {
	name, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")
	return "-" + name
}

// withRunnerArgs trả bản sao của sel mà mỗi lệnh mang thêm tham số của runner đó trong runnerArgs (workflow.verify.runner_args): lệnh `go test`
// chèn ngay sau `go test`, runner khác nối vào cuối (pytest, jest, vitest, cargo, dotnet, maven, gradle đều nhận tùy chọn ở cuối; tham số cho
// chương trình test của cargo thì người dùng tự ghi kèm `--`). Runner go không có khoá thì dùng cờ của verify.command (goTestFlags). Tham số
// mà lệnh đã có (cùng tên cờ) thì giữ của lệnh.
func withRunnerArgs(sel affected.Selection, cmd string, runnerArgs map[string][]string) affected.Selection {
	if len(sel.Commands) == 0 {
		return sel
	}
	argsFor := func(runner string) []string {
		if a, ok := runnerArgs[runner]; ok {
			return a
		}
		if runner == "go" {
			f, _ := goTestFlags(cmd)
			return f
		}
		return nil
	}
	cmds := make([]affected.Command, len(sel.Commands))
	for i, c := range sel.Commands {
		c.Argv = slices.Clone(c.Argv)
		if extra := missingArgs(c.Argv, argsFor(c.Runner)); len(extra) > 0 {
			if len(c.Argv) >= 2 && c.Argv[0] == "go" && c.Argv[1] == "test" {
				c.Argv = append(append([]string{"go", "test"}, extra...), c.Argv[2:]...)
			} else {
				c.Argv = append(c.Argv, extra...)
			}
		}
		cmds[i] = c
	}
	sel.Commands = cmds
	return sel
}

// missingArgs trả các tham số của args mà argv chưa có: cờ (bắt đầu bằng -) so theo tên, kèm giá trị rời ngay sau nó; tham số khác so nguyên văn.
func missingArgs(argv, args []string) []string {
	have := map[string]bool{}
	for _, a := range argv {
		have[a] = true
		if strings.HasPrefix(a, "-") {
			have[goFlagName(a)] = true
		}
	}
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		key := a
		if strings.HasPrefix(a, "-") && a != "--" {
			key = goFlagName(a)
		}
		if have[key] && a != "--" {
			// Cờ đã có: bỏ cả giá trị rời đi kèm (tham số kế không bắt đầu bằng -).
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

// goFlagsTimeoutOf là cờ và trần -timeout của lần kiểm chứng v: theo lệnh đã chạy (Ran) khi đó là `go test` thuần, không thì theo lệnh kiểm
// chứng của phiếu (Ran là script nhiều lô của bgraph, mang cùng cờ đó).
func goFlagsTimeoutOf(v task.Verification) ([]string, time.Duration) {
	for _, cmd := range []string{v.Ran, v.Command} {
		if flags, d, ok := goFlagsTimeoutOK(cmd); ok {
			return flags, d
		}
	}
	return nil, triageDefaultTimeout
}
