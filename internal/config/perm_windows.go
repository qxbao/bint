//go:build windows

package config

import "os"

// SecretsPermProblem: Windows không có bit quyền kiểu Unix và Bint chưa kiểm ACL, nên luôn trả rỗng.
func SecretsPermProblem(os.FileInfo) string { return "" }
