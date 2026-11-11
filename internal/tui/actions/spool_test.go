package actions_test

import (
	"testing"
	"time"

	"bint/internal/app/evolvesvc"
	"bint/internal/spool"
	"bint/internal/ulid"
)

// writeSpoolSession ghi một phiên vào spool (chưa nạp).
func writeSpoolSession(t *testing.T, e *evolvesvc.Workspace) {
	t.Helper()
	at := time.Now().Add(-time.Hour)
	ev := func(typ string, ts time.Time, p map[string]any) spool.Event {
		return spool.Event{EventID: ulid.New(), Type: typ, Source: "hook:claude", Timestamp: ts, Harness: "claude", SessionID: "sx", Payload: p}
	}
	if _, err := spool.Write(e.K.Paths.Spool, ev(spool.SessionStarted, at, map[string]any{"model": "m"}), ev(spool.SessionEnded, at.Add(time.Minute), map[string]any{"reason": "exit"})); err != nil {
		t.Fatal(err)
	}
}
