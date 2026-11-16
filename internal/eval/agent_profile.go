package eval

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"bint/internal/iso"
)

// Profile `bint iso` của lượt chạy harness (docs/iso-plan.md mục 5.7). Mỗi chỗ Bint tự chạy Claude/Codex (bước bint run,
// lượt bint eval) dựng env qua agentEnv: có profile thì iso.Begin (khoá S, TMPDIR có trước khi spawn, dọn temp khi xong),
// không có profile mà tiến trình bint đang chạy trong một profile thì iso.RealEnv (không lan tài khoản của phiên chủ).

// ProfileAgent là agent mang profile mặc định (lấy từ harnesses.<h>.profile lúc harness.NewAgent); workflow đọc nó để ghi
// profile vào handle phiên và để quyết định nối tiếp.
type ProfileAgent interface {
	// AgentProfile trả profile mặc định của agent (nil = không dùng profile).
	AgentProfile() *iso.Profile
}

// EffectiveProfile là profile mà agent sẽ chạy với yêu cầu req: req.Profile nếu đặt, không thì profile của agent.
func EffectiveProfile(a Agent, req AgentRequest) *iso.Profile {
	if req.Profile != nil {
		return req.Profile
	}
	if pa, ok := a.(ProfileAgent); ok {
		return pa.AgentProfile()
	}
	return nil
}

// AgentProfile: profile mặc định của agent.
func (a *ClaudeAgent) AgentProfile() *iso.Profile { return a.Profile }

// AgentProfile: profile mặc định của agent.
func (a *CodexAgent) AgentProfile() *iso.Profile { return a.Profile }

// stepEnv là môi trường đã dựng cho một lượt chạy harness.
type stepEnv struct {
	Env []string
	// Profile là profile hiệu lực (nil = không dùng profile).
	Profile *iso.Profile
	// LB là bộ cờ -c Codex LB của profile linked (rỗng nếu không), phải đứng ở cấp exec trước ExtraArgs/resume.
	LB []string
	// End nhả khoá S và dọn temp; luôn gọi được (no-op khi không có profile).
	End func()
}

// agentEnv dựng env của harness cho req. base = cleanEnv + req.Env. profile = req.Profile, không thì def. Có profile:
// iso.Begin (lỗi Begin hoặc LB không sẵn sàng thì trả lỗi; LB không sẵn sàng bọc iso.ErrLBUnavailable). Không có profile
// mà env gốc đang trong profile: iso.RealEnv cho phần của tiến trình rồi mới nối req.Env (bước không lan profile của phiên chủ).
func agentEnv(ctx context.Context, req AgentRequest, def *iso.Profile) (stepEnv, error) {
	prof := req.Profile
	if prof == nil {
		prof = def
	}
	if prof == nil {
		base := cleanEnv()
		if iso.InProfile(base) {
			base = iso.RealEnv(base)
		}
		return stepEnv{Env: append(base, req.Env...), End: func() {}}, nil
	}
	base := cleanEnv(req.Env...)
	lb, err := iso.HarnessArgs(ctx, *prof, base)
	if err != nil {
		return stepEnv{}, fmt.Errorf("profile %s: %w", prof.ID(), err)
	}
	sess, err := iso.Begin(*prof, base, nil)
	if err != nil {
		return stepEnv{}, fmt.Errorf("profile %s: %w", prof.ID(), err)
	}
	return stepEnv{Env: sess.Env, Profile: prof, LB: lb, End: sess.End}, nil
}

// profileFailure đổi lỗi dựng env profile thành kết quả lượt: LB không sẵn sàng là lỗi hạ tầng (Infra), lỗi khác là lỗi
// của lượt (bước thất bại, không âm thầm chạy bằng tài khoản thật).
func profileFailure(err error) AgentResult {
	res := AgentResult{ExitCode: -1, Error: err.Error()}
	if errors.Is(err, iso.ErrLBUnavailable) {
		res.Infra = "Codex LB của profile không sẵn sàng: " + strings.TrimSpace(err.Error())
	}
	return res
}
