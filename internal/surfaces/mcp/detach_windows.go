package mcp

import (
	"os/exec"
	"syscall"
)

// detach 让拉起的后台不弹窗口、不挂在宿主的控制台上。
func detach(cmd *exec.Cmd) {
	const createNoWindow, detachedProcess = 0x08000000, 0x00000008
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow | detachedProcess, HideWindow: true}
}
