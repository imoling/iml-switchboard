//go:build windows

package procutil

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// Hide 让子进程不开命令行窗口；保留调用方已设的其他标志（进程组、脱离控制台）。
func Hide(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
