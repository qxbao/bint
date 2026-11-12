package mcpgw

import "time"

// Status là kết quả /admin/status.
type Status struct {
	PID           int              `json:"pid"`
	Port          int              `json:"port"`
	Version       string           `json:"version,omitempty"`
	StartedAt     time.Time        `json:"started_at"`
	UptimeSec     int64            `json:"uptime_sec"`
	Sessions      int              `json:"sessions"`
	DroppedEvents int64            `json:"dropped_events"`
	Upstreams     []UpstreamStatus `json:"upstreams"`
}

// UpstreamStatus là trạng thái một phân vùng (một upstream của một service).
type UpstreamStatus struct {
	Service    string `json:"service"`
	Partition  string `json:"partition"`
	Share      string `json:"share"`
	Running    bool   `json:"running"`
	Generation int    `json:"generation"`
	PID        int    `json:"pid,omitempty"`
	RSSKB      uint64 `json:"rss_kb,omitempty"`
	PSSKB      uint64 `json:"pss_kb,omitempty"`
	Sessions   int    `json:"sessions"`
	Inflight   int    `json:"inflight"`
	Restarts   int    `json:"restarts"`
	Limited    bool   `json:"restart_limited,omitempty"`
	// LastCall là lời gọi gần nhất (không có đối số hay kết quả).
	LastCall *LastCall `json:"last_call,omitempty"`
	// LastError là lỗi gần nhất của upstream, đã che giá trị credential.
	LastError string `json:"last_error,omitempty"`
}

// LastCall là lời gọi tools/call gần nhất của một phân vùng.
type LastCall struct {
	At         time.Time `json:"at"`
	Tool       string    `json:"tool"`
	Outcome    string    `json:"outcome"`
	DurationMS int64     `json:"duration_ms"`
}

func (p *partition) status() UpstreamStatus {
	p.mu.Lock()
	st := UpstreamStatus{Service: p.svc.ID, Partition: p.label, Share: string(p.svc.Share), Generation: p.genNo,
		Sessions: len(p.sessions), Inflight: p.inflight, Restarts: p.restarts, Limited: p.limited, LastError: p.lastErr}
	up := p.up
	if !p.lastCall.At.IsZero() {
		lc := p.lastCall
		st.LastCall = &LastCall{At: lc.At.UTC(), Tool: lc.Tool, Outcome: lc.Outcome, DurationMS: lc.Duration.Milliseconds()}
	}
	p.mu.Unlock()
	if up != nil && up.alive() {
		st.Running = true
		st.PID = up.pid()
		st.Generation = up.generation()
		if st.PID > 0 {
			st.RSSKB, st.PSSKB = procMemory(st.PID)
		}
	}
	return st
}
