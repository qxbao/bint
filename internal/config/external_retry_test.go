package config

import (
	"strings"
	"testing"
	"time"
)

func TestExternalRetryDefaultAndValidate(t *testing.T) {
	c := Default()
	got := c.Workflow.ExternalRetrySchedule()
	want := []time.Duration{30 * time.Second, 2 * time.Minute, 5 * time.Minute}
	if len(got) != len(want) {
		t.Fatalf("lịch mặc định = %v, muốn %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("lịch mặc định = %v, muốn %v", got, want)
		}
	}
	c.Workflow.ExternalRetry = []string{}
	if err := c.Validate(); err != nil || len(c.Workflow.ExternalRetrySchedule()) != 0 {
		t.Fatalf("danh sách rỗng phải hợp lệ và tắt: %v", err)
	}
	for _, bad := range []string{"0s", "-1m", "abc", ""} {
		c.Workflow.ExternalRetry = []string{"30s", bad}
		err := c.Validate()
		if err == nil || !strings.Contains(err.Error(), "workflow.external_retry[1]") {
			t.Fatalf("%q phải bị từ chối, được %v", bad, err)
		}
	}
}
