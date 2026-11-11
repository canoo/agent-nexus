//go:build unix

package main

import (
	"os/exec"
	"syscall"
	"time"
)

// Kill the entire delegation group, including curl/sleep children on timeout.
func configureRouteProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
}
