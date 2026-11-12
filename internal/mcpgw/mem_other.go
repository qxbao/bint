//go:build !linux && !darwin && !windows

package mcpgw

// procMemory: nền tảng này chưa có cách đo; trả 0.
func procMemory(int) (rssKB, pssKB uint64) { return 0, 0 }
