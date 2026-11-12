//go:build !windows

package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCodexReaderConcurrentSymlinkReplacement(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".bint", "evals"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".bint", "evals", "hidden"), []byte("HIDDEN_EVAL_CANARY"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "source")
	if err := os.WriteFile(path, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		tmp := filepath.Join(dir, "replacement")
		for ctx.Err() == nil {
			if os.Symlink(".bint/evals/hidden", tmp) == nil {
				_ = os.Rename(tmp, path)
			}
			if os.WriteFile(tmp, []byte("safe"), 0600) == nil {
				_ = os.Rename(tmp, path)
			}
		}
	}()
	defer func() { cancel(); <-done }()
	for i := 0; i < 10000; i++ {
		out, _ := runCodexReaderContext(ctx, dir, "Read", json.RawMessage(`{"path":"source"}`))
		if strings.Contains(out, "HIDDEN_EVAL_CANARY") {
			t.Fatal("read hidden eval through concurrently replaced symlink")
		}
	}
}

func TestCodexReaderConcurrentDirectorySymlinkReplacement(t *testing.T) {
	dir := t.TempDir()
	hidden := filepath.Join(dir, ".bint", "evals")
	if err := os.MkdirAll(hidden, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hidden, "file"), []byte("HIDDEN_EVAL_CANARY"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "safe"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "safe", "file"), []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	// Swap symlinks and directories by renaming the visible directory away first.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		visible, parked, link := filepath.Join(dir, "source"), filepath.Join(dir, "parked"), filepath.Join(dir, "link")
		for ctx.Err() == nil {
			if os.Symlink(".bint/evals", link) == nil {
				_ = os.Rename(link, visible)
			}
			_ = os.Remove(visible)
			if os.Rename(filepath.Join(dir, "safe"), visible) == nil {
				_ = os.Rename(visible, parked)
				_ = os.Rename(parked, filepath.Join(dir, "safe"))
			}
		}
	}()
	defer func() { cancel(); <-done }()
	for i := 0; i < 10000; i++ {
		out, _ := runCodexReaderContext(ctx, dir, "Read", json.RawMessage(`{"path":"source/file"}`))
		if strings.Contains(out, "HIDDEN_EVAL_CANARY") {
			t.Fatal("read hidden eval through concurrently replaced directory")
		}
	}
}

func TestCodexReaderConcurrentFIFOReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source")
	if err := os.WriteFile(path, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			tmp := filepath.Join(dir, "replacement")
			if syscall.Mkfifo(tmp, 0600) == nil {
				_ = os.Rename(tmp, path)
			}
			if os.WriteFile(tmp, []byte("hello"), 0600) == nil {
				_ = os.Rename(tmp, path)
			}
		}
	}()
	defer func() { cancel(); <-done }()
	readCtx, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	for i := 0; i < 500; i++ {
		_, _ = runCodexReaderContext(readCtx, dir, "Read", json.RawMessage(`{"path":"source"}`))
		if err := readCtx.Err(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCodexReaderRejectsFIFOWithoutWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ tool, args string }{{"Read", `{"path":"pipe"}`}, {"Grep", `{"pattern":"x"}`}} {
		done := make(chan error, 1)
		go func() { _, err := runCodexReader(dir, tc.tool, json.RawMessage(tc.args)); done <- err }()
		select {
		case err := <-done:
			if tc.tool == "Read" && err == nil {
				t.Fatal("FIFO accepted")
			}
		case <-time.After(time.Second):
			f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
			if err == nil {
				f.Close()
			}
			<-done
			t.Fatalf("%s blocked opening FIFO", tc.tool)
		}
	}
}

func TestReviewGlobConcurrentDirectoryReplacement(t *testing.T) {
	dir := t.TempDir()
	hidden := filepath.Join(dir, ".bint", "evals")
	if err := os.MkdirAll(hidden, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if err := os.WriteFile(filepath.Join(hidden, fmt.Sprintf("HIDDEN_EVAL_NAME_%d", i)), []byte("secret"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	visible, parked, link := filepath.Join(dir, "source"), filepath.Join(dir, "parked"), filepath.Join(dir, "replacement-link")
	if err := os.Mkdir(visible, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if err := os.WriteFile(filepath.Join(visible, fmt.Sprintf("safe_%d", i)), []byte("safe"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			if os.Rename(visible, parked) == nil {
				if os.Symlink(".bint/evals", link) == nil {
					_ = os.Rename(link, visible)
				}
				time.Sleep(time.Microsecond)
				_ = os.Remove(visible)
				_ = os.Rename(parked, visible)
			}
		}
	}()
	defer func() { cancel(); <-done }()
	for i := 0; i < 20000; i++ {
		out, _ := runCodexReaderContext(ctx, dir, "Glob", json.RawMessage(`{"pattern":"source/**"}`))
		if strings.Contains(out, "HIDDEN_EVAL_NAME_") {
			t.Fatalf("Glob exposes eval names through concurrently replaced directory: %.200s", out)
		}
	}
}
