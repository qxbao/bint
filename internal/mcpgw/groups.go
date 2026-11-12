package mcpgw

import (
	"context"
	"fmt"
	"time"

	"bint/internal/mcpcat"
)

// groupState là trạng thái một nhóm thao tác trong một phân vùng: khoá độc quyền (một phiên giữ, có TTL) và
// semaphore giới hạn đồng thời.
type groupState struct {
	holder  string
	expires time.Time
	changed chan struct{} // đóng khi khoá đổi chủ hoặc được nhả
	sem     chan struct{}
}

func (p *partition) group(name string) *groupState {
	if p.groups == nil {
		p.groups = map[string]*groupState{}
	}
	g := p.groups[name]
	if g == nil {
		g = &groupState{changed: make(chan struct{})}
		p.groups[name] = g
	}
	return g
}

// lockExclusive giữ khoá của nhóm cho phiên sessID: chờ có giới hạn rồi trả CodeBusy. Chủ khoá dùng lại thì gia hạn TTL;
// khoá hết hạn thì phiên khác lấy được. Khoá nhả khi phiên đóng (unlockExclusive).
func (p *partition) lockExclusive(ctx context.Context, sessID string, g mcpcat.Group) error {
	wait, ttl := p.d.opts.ExclusiveWait, p.d.opts.ExclusiveTTL
	deadline := time.Now().Add(wait)
	for {
		p.gmu.Lock()
		gs := p.group(g.Name)
		now := time.Now()
		if gs.holder == "" || gs.holder == sessID || !now.Before(gs.expires) {
			if gs.holder != sessID {
				gs.holder = sessID
				close(gs.changed)
				gs.changed = make(chan struct{})
			}
			gs.expires = now.Add(ttl)
			p.gmu.Unlock()
			return nil
		}
		ch, expiry := gs.changed, gs.expires
		p.gmu.Unlock()
		until := deadline
		if expiry.Before(until) {
			until = expiry
		}
		t := time.NewTimer(time.Until(until))
		select {
		case <-ch:
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		}
		t.Stop()
		if !time.Now().Before(deadline) {
			p.gmu.Lock()
			gs := p.group(g.Name)
			stillHeld := gs.holder != "" && gs.holder != sessID && time.Now().Before(gs.expires)
			p.gmu.Unlock()
			if stillHeld {
				return &rpcError{Code: CodeBusy, Message: fmt.Sprintf("nhóm %s.%s đang do phiên khác giữ (độc quyền, TTL %s); thử lại sau hoặc chờ phiên đó đóng", p.svc.ID, g.Name, ttl)}
			}
		}
	}
}

// unlockExclusive nhả mọi khoá độc quyền phiên sessID đang giữ trong phân vùng.
func (p *partition) unlockExclusive(sessID string) {
	p.gmu.Lock()
	defer p.gmu.Unlock()
	for _, gs := range p.groups {
		if gs.holder == sessID {
			gs.holder = ""
			close(gs.changed)
			gs.changed = make(chan struct{})
		}
	}
}

// acquireSem chiếm một chỗ trong giới hạn đồng thời của nhóm (MaxConcurrent > 0); trả hàm nhả.
func (p *partition) acquireSem(ctx context.Context, g mcpcat.Group) (func(), error) {
	if g.MaxConcurrent <= 0 {
		return func() {}, nil
	}
	p.gmu.Lock()
	gs := p.group(g.Name)
	if gs.sem == nil {
		gs.sem = make(chan struct{}, g.MaxConcurrent)
	}
	sem := gs.sem
	p.gmu.Unlock()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
