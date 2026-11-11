package app

import (
	"fmt"
	"os"
	"path/filepath"
)

func fileName(i int) string { return fmt.Sprintf("src/f%02d.go", i) }

func titleOf(i int) string { return fmt.Sprintf("Sự thật số %02d của lô", i) }

func removeFile(root, rel string) error { return os.Remove(filepath.Join(root, rel)) }
