//go:build !windows

package sub

import (
	"os/exec"
	"syscall"
)

// ownGroup 让子进程自成一个进程组：结束时能把它拉起的子进程一起结束，不留孤儿占着管道。
func ownGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// killTree 结束整个进程组。
func killTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}
