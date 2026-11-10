// Package cli là tầng dòng lệnh của Bint (cobra). Logic nằm ở package app.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"bint/internal/app"
	"bint/internal/config"
	"bint/internal/gitrepo"
)

// Version được gán khi build: -ldflags "-X bint/internal/cli.Version=v0.1.0".
var Version = "0.1.0-dev"

// Mã thoát.
const (
	ExitOK             = 0
	ExitError          = 1
	ExitUsage          = 2
	ExitNotImplemented = 3
	ExitNotInitialized = 4
)

// ExitError mang mã thoát cụ thể.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func withCode(code int, err error) error { return &exitError{code: code, err: err} }

// IO gom các luồng vào/ra để kiểm thử.
type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Execute chạy CLI và trả về mã thoát.
func Execute(ctx context.Context, args []string, stdio IO) int {
	root := NewRoot(stdio)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return ExitOK
	}
	fmt.Fprintln(stdio.Err, "lỗi:", err)
	var ee *exitError
	switch {
	case errors.As(err, &ee):
		return ee.code
	case errors.Is(err, gitrepo.ErrNotRepo), errors.Is(err, app.ErrNotInitialized), errors.Is(err, config.ErrNoProjectConfig):
		return ExitNotInitialized
	case isUsageError(err):
		return ExitUsage
	}
	return ExitError
}

type usageError struct{ error }

func isUsageError(err error) bool {
	var u usageError
	if errors.As(err, &u) {
		return true
	}
	// Lỗi cú pháp do cobra sinh ra (lệnh lạ, sai số đối số).
	msg := err.Error()
	return strings.HasPrefix(msg, "unknown command") || strings.HasPrefix(msg, "unknown flag") ||
		strings.Contains(msg, "arg(s), received")
}

// NewRoot dựng cây lệnh.
func NewRoot(stdio IO) *cobra.Command {
	root := &cobra.Command{
		Use:           "bint",
		Short:         "Bint — ghi nhận, đo lường và tri thức cho coding agent",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}
	root.SetIn(stdio.In)
	root.SetOut(stdio.Out)
	root.SetErr(stdio.Err)
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return usageError{fmt.Errorf("%w\n\n%s", err, c.UsageString())}
	})
	root.CompletionOptions.HiddenDefaultCmd = true

	root.AddGroup(
		&cobra.Group{ID: "project", Title: "Dự án:"},
		&cobra.Group{ID: "knowledge", Title: "Tri thức:"},
		&cobra.Group{ID: "workflow", Title: "Workflow:"},
		&cobra.Group{ID: "runs", Title: "Lượt chạy:"},
		&cobra.Group{ID: "eval", Title: "Đánh giá:"},
		&cobra.Group{ID: "planned", Title: "Dự kiến (chưa có trong bản này):"},
	)
	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add("project", newInitCmd(stdio), newDoctorCmd(stdio), newStatusCmd(stdio), newSyncCmd(stdio))
	add("knowledge", newInspectCmd(stdio), newBootstrapCmd(stdio), newKnowledgeCmd(stdio),
		newContextCmd(stdio), newIndexCmd(stdio), newMCPCmd(stdio))
	add("workflow", newRunCmd(stdio), newTaskCmd(stdio), newSkillCmd(stdio))
	add("runs", newRunsCmd(stdio), newEventsCmd(stdio))
	add("eval", newEvalCmd(stdio))
	add("planned", plannedCommands()...)
	root.AddCommand(newHookCmd(stdio))
	return root
}

func cwd() (string, error) { return os.Getwd() }
