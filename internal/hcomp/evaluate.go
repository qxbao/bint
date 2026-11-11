package hcomp

import (
	"regexp"

	"bint/internal/pathglob"
)

// MaxStopDenies là số lần tối đa một guardrail `deny` chặn kết thúc phiên; vượt trần thì
// cho qua và ghi bypass — Bint không bao giờ nhốt agent.
const MaxStopDenies = 3

// Caps là các mức hành động mà harness thực thi được ở một sự kiện.
type Caps struct{ Deny, Ask, Remind bool }

// Degrade hạ hành động xuống mức gần nhất harness hỗ trợ theo thứ tự deny → ask → remind
// → bỏ (không bao giờ nâng). Trả "" nếu harness không hỗ trợ mức nào.
func (c Caps) Degrade(action string) string {
	switch action {
	case ActDeny:
		if c.Deny {
			return ActDeny
		}
		fallthrough
	case ActAsk:
		if c.Ask {
			return ActAsk
		}
		fallthrough
	case ActRemind:
		if c.Remind {
			return ActRemind
		}
	}
	return ""
}

// Event là một điểm kiểm tra guardrail.
type Event struct {
	On      string
	Paths   []string // tệp của lần gọi (pre_edit), tương đối theo repository
	Command string   // lệnh của lần gọi (pre_bash)
	// StopHookActive: harness báo phiên đang tiếp tục do một hook stop trước đó.
	StopHookActive bool
}

// Decision là kết quả đánh giá một guardrail ở một sự kiện.
type Decision struct {
	ID        string
	Requested string // hành động khai báo
	// Action là hành động thực thi sau khi hạ mức; "" khi không thực thi.
	Action  string
	Message string
	// Eligible: điều kiện when đúng (kể cả khi unless đã thoả nên không kích hoạt).
	Eligible bool
	// Fired: guardrail được thực thi (có tác động tới agent).
	Fired bool
	// Block: chặn kết thúc phiên (on: stop).
	Block bool
	// Bypass: deny ở stop vượt trần MaxStopDenies.
	Bypass bool
	// Reason giải thích khi không thực thi: "unless", "unsupported", "once", "bypass".
	Reason string
}

// Evaluate đánh giá guardrail ở một sự kiện. Hàm thuần: chỉ đọc trạng thái phiên; người gọi
// ghi các bản ghi kết quả. Trả về một Decision cho mỗi guardrail có điều kiện when đúng.
func Evaluate(gs []Component, ev Event, st *State, caps Caps) []Decision {
	if st == nil {
		st = newState()
	}
	var out []Decision
	for _, g := range gs {
		if g.Kind != KindGuardrail || g.Status == StatusDeprecated || g.On != ev.On {
			continue
		}
		if !whenHolds(g, ev, st) {
			continue
		}
		d := Decision{ID: g.ID, Requested: g.Action, Message: g.Message, Eligible: true}
		if Satisfied(g, st) {
			d.Reason = "unless"
			out = append(out, d)
			continue
		}
		d.Action = caps.Degrade(g.Action)
		if d.Action == "" {
			d.Reason = "unsupported"
			out = append(out, d)
			continue
		}
		switch {
		case ev.On == OnStop && d.Action == ActRemind:
			if ev.StopHookActive || st.StopBlocks[g.ID] >= 1 {
				d.Reason = "once"
			} else {
				d.Fired, d.Block = true, true
			}
		case ev.On == OnStop: // deny (ask đã hạ xuống remind)
			if st.StopBlocks[g.ID] >= MaxStopDenies {
				d.Reason, d.Bypass = "bypass", true
			} else {
				d.Fired, d.Block = true, true
			}
		case d.Action == ActRemind:
			if st.Fired[g.ID] >= 1 {
				d.Reason = "once"
			} else {
				d.Fired = true
			}
		default:
			d.Fired = true
		}
		out = append(out, d)
	}
	return out
}

func whenHolds(g Component, ev Event, st *State) bool {
	if g.Domain() != "" {
		files := ev.Paths
		if len(files) == 0 {
			files = st.EditedPaths()
		}
		if !anyMatch(g.Domain(), files) {
			return false
		}
	}
	w := g.When
	if w.Paths != "" && !anyMatch(w.Paths, ev.Paths) {
		return false
	}
	if w.Edited != "" && !anyMatch(w.Edited, st.EditedPaths()) {
		return false
	}
	if w.Command != "" {
		re, err := regexp.Compile(w.Command)
		if err != nil || !re.MatchString(ev.Command) {
			return false
		}
	}
	return true
}

func anyMatch(glob string, files []string) bool {
	p := pathglob.Compile(glob)
	for _, f := range files {
		if p.Match(f) {
			return true
		}
	}
	return false
}

// Satisfied cho biết điều kiện unless của guardrail đã thoả. Có when.edited thì chỉ tính
// các lệnh chạy SAU lần sửa cuối khớp glob đó ("chưa chạy test cho phần đã sửa"); không
// thì tính mọi lệnh của phiên.
func Satisfied(g Component, st *State) bool {
	ran := g.Unless.Ran
	if ran == "" {
		return false
	}
	after := 0
	if g.When.Edited != "" {
		p := pathglob.Compile(g.When.Edited)
		for _, e := range st.Edits {
			if p.Match(e.Path) && e.Seq > after {
				after = e.Seq
			}
		}
	}
	var re *regexp.Regexp
	if ran != RanTest {
		var err error
		if re, err = regexp.Compile(ran); err != nil {
			return false
		}
	}
	for _, c := range st.Cmds {
		if c.Seq <= after {
			continue
		}
		if ran == RanTest && c.Test || re != nil && re.MatchString(c.Cmd) {
			return true
		}
	}
	return false
}

// Followed trả về ID các guardrail đã kích hoạt, chưa được ghi "làm theo", mà điều kiện
// unless nay đã thoả. Gọi sau khi đã nối lệnh mới vào trạng thái phiên.
func Followed(gs []Component, st *State) []string {
	var out []string
	for _, g := range gs {
		if g.Kind == KindGuardrail && st.Fired[g.ID] > 0 && !st.Followed[g.ID] && Satisfied(g, st) {
			out = append(out, g.ID)
		}
	}
	return out
}
