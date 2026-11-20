package testreport

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"
)

// GoText đọc log văn bản thường của `go test` (có hoặc không -v).
type GoText struct{}

// GoJSON đọc log `go test -json`.
type GoJSON struct{}

var (
	goFailRe    = regexp.MustCompile(`^\s*--- FAIL: (\S+)`)
	goSummaryRe = regexp.MustCompile(`^(ok|FAIL|\?)\s+(\S+)(?:\s+(\[build failed\]|\[setup failed\]|\[no test files\]|\(cached\)|[0-9.]+s))?`)
	goRunningRe = regexp.MustCompile(`^\s+(Test\S*|Example\S*|Fuzz\S*) \(`)
)

const goTimeoutPanic = "panic: test timed out"

func (GoText) Name() string { return "gotest" }

func (GoText) Detect(text string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if goFailRe.MatchString(line) || strings.HasPrefix(line, goTimeoutPanic) {
			return true
		}
		if m := goSummaryRe.FindStringSubmatch(line); m != nil && m[3] != "" {
			return true
		}
	}
	return false
}

func (GoText) Parse(text string) Report {
	r := Report{Measured: true, Format: "gotest"}
	var acc goAcc
	for line := range strings.SplitSeq(text, "\n") {
		acc.line(&r, strings.TrimRight(line, "\r"), "")
	}
	acc.flush(&r, "")
	finish(&r)
	return r
}

// goAcc gom các dòng của một gói cho tới dòng tổng kết của gói (ok/FAIL <gói>), rồi gán test đỏ/hết giờ cho gói đó.
type goAcc struct {
	failed, timedOut []string
	timeout, running bool
	panicked         bool
}

func (a *goAcc) line(r *Report, line, pkg string) {
	switch {
	case strings.HasPrefix(line, goTimeoutPanic):
		a.timeout, a.running = true, false
		return
	case strings.HasPrefix(line, "panic: "):
		if !a.timeout {
			a.panicked = true
		}
		return
	case a.timeout && strings.TrimSpace(line) == "running tests:":
		a.running = true
		return
	case a.running:
		if m := goRunningRe.FindStringSubmatch(line); m != nil {
			a.timedOut = append(a.timedOut, m[1])
			return
		}
		a.running = false
	}
	if m := goFailRe.FindStringSubmatch(line); m != nil {
		if !slices.Contains(a.failed, m[1]) {
			a.failed = append(a.failed, m[1])
		}
		return
	}
	m := goSummaryRe.FindStringSubmatch(line)
	if m == nil || m[3] == "" && pkg == "" {
		return // dòng "FAIL" trần (tổng kết cả lệnh) hoặc chữ "ok" giữa văn bản
	}
	name := m[2]
	if pkg != "" && name != pkg {
		return
	}
	switch m[3] {
	case "[no test files]":
		a.reset()
		return
	case "[build failed]", "[setup failed]":
		r.BuildFailed = append(r.BuildFailed, name)
		a.reset()
		return
	}
	p := Package{Name: name, Passed: m[1] == "ok", Cached: m[3] == "(cached)"}
	if d, err := time.ParseDuration(m[3]); err == nil {
		p.Duration = d
	}
	r.Packages = append(r.Packages, p)
	a.flush(r, name)
}

func (a *goAcc) flush(r *Report, pkg string) {
	timed := map[string]bool{}
	for _, n := range a.timedOut {
		timed[n] = true
		r.TimedOut = append(r.TimedOut, Test{Package: pkg, Name: n})
	}
	if a.timeout && len(a.timedOut) == 0 {
		r.TimedOut = append(r.TimedOut, Test{Package: pkg}) // Go cũ không liệt kê test đang chạy: cả gói hết giờ
	}
	for _, n := range a.failed {
		// Test hết giờ cũng in "--- FAIL" (Go mới) hoặc là cha của test con hết giờ: không tính là đỏ thật.
		if timed[n] || slices.ContainsFunc(a.timedOut, func(t string) bool { return strings.HasPrefix(t, n+"/") }) {
			continue
		}
		r.Failed = append(r.Failed, Test{Package: pkg, Name: n})
	}
	if a.panicked && pkg != "" {
		r.Panicked = append(r.Panicked, pkg)
	}
	a.reset()
}

func (a *goAcc) reset() { *a = goAcc{} }

func finish(r *Report) {
	r.Failed = leaves(r.Failed)
	sortTests(r.Failed)
	sortTests(r.TimedOut)
	slices.Sort(r.BuildFailed)
	r.BuildFailed = slices.Compact(r.BuildFailed)
	slices.Sort(r.Panicked)
	r.Panicked = slices.Compact(r.Panicked)
}

func (GoJSON) Name() string { return "gotest-json" }

func (GoJSON) Detect(text string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return strings.HasPrefix(line, "{") && strings.Contains(line, `"Action"`)
	}
	return false
}

type goEvent struct {
	Action      string
	Package     string
	Test        string
	Elapsed     float64
	Output      string
	FailedBuild string
}

// Parse gom Output theo gói (sự kiện của các gói có thể xen nhau) rồi đọc từng gói như log văn bản; thời lượng lấy từ sự kiện pass/fail của gói.
func (GoJSON) Parse(text string) Report {
	r := Report{Measured: true, Format: "gotest-json"}
	var order []string
	outs := map[string]*strings.Builder{}
	elapsed := map[string]float64{}
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev goEvent
		if json.Unmarshal([]byte(line), &ev) != nil || ev.Package == "" {
			continue
		}
		if _, ok := outs[ev.Package]; !ok {
			outs[ev.Package] = &strings.Builder{}
			order = append(order, ev.Package)
		}
		switch {
		case ev.Action == "output":
			outs[ev.Package].WriteString(ev.Output)
		case ev.Test == "" && (ev.Action == "pass" || ev.Action == "fail"):
			elapsed[ev.Package] = ev.Elapsed
			if ev.FailedBuild != "" {
				r.BuildFailed = append(r.BuildFailed, ev.Package)
			}
		}
	}
	for _, pkg := range order {
		var acc goAcc
		n := len(r.Packages)
		for l := range strings.SplitSeq(outs[pkg].String(), "\n") {
			acc.line(&r, strings.TrimRight(l, "\r"), pkg)
		}
		if len(r.Packages) == n {
			acc.flush(&r, pkg) // gói không in dòng tổng kết (lỗi build): vẫn gán phần đã đọc
		} else if e, ok := elapsed[pkg]; ok && r.Packages[n].Duration == 0 && !r.Packages[n].Cached {
			r.Packages[n].Duration = time.Duration(e * float64(time.Second))
		}
	}
	finish(&r)
	return r
}

// GoRerunCommand dựng lệnh `go test` chạy lại riêng các test trong ts (mỗi gói một biểu thức -run neo theo tên cấp đầu), giữ các cờ flags của
// lệnh gốc. Trả rỗng khi có test không rõ gói (không chạy lại riêng được).
func GoRerunCommand(flags []string, ts []Test, timeout time.Duration) string {
	byPkg := map[string][]string{}
	var pkgs []string
	for _, t := range ts {
		if t.Package == "" || t.Name == "" {
			return ""
		}
		if _, ok := byPkg[t.Package]; !ok {
			pkgs = append(pkgs, t.Package)
		}
		if top := t.Top(); !slices.Contains(byPkg[t.Package], top) {
			byPkg[t.Package] = append(byPkg[t.Package], top)
		}
	}
	if len(pkgs) == 0 {
		return ""
	}
	slices.Sort(pkgs)
	var cmds []string
	for _, p := range pkgs {
		names := byPkg[p]
		slices.Sort(names)
		parts := []string{"go", "test", "-count=1"}
		for _, f := range flags {
			if strings.HasPrefix(f, "-count=") || timeout > 0 && strings.HasPrefix(f, "-timeout=") || strings.HasPrefix(f, "-run=") {
				continue
			}
			parts = append(parts, f)
		}
		if timeout > 0 {
			parts = append(parts, "-timeout="+timeout.String())
		}
		parts = append(parts, "-run", "'^("+strings.Join(names, "|")+")$'", p)
		cmds = append(cmds, strings.Join(parts, " "))
	}
	return strings.Join(cmds, " && ")
}
