//go:build !windows && !darwin

package mcpgw

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func processZombie(pid int) (bool, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false, err
	}
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return false, fmt.Errorf("process %d: invalid stat", pid)
	}
	f := strings.Fields(s[i+1:])
	if len(f) == 0 {
		return false, fmt.Errorf("process %d: missing state", pid)
	}
	return f[0] == "Z", nil
}
