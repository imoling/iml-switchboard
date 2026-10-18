//go:build !windows

package mcp

import (
	"os/exec"
	"syscall"
)

// detach 让拉起的后台自成一个会话：宿主退出时不会把它一起带走。
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
