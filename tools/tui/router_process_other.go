//go:build !unix

package main

import (
	"os/exec"
	"time"
)

func configureRouteProcess(cmd *exec.Cmd) { cmd.WaitDelay = time.Second }
