package actions_test

import "os/exec"

type execCmd = exec.Cmd

func newCmd(name string) *exec.Cmd { return exec.Command(name) }
