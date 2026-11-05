//go:build !windows

package laya

import (
	"os/exec"
	"syscall"
	"time"
)

func ownGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// stopGroup 先请整组退出，五秒没退就强制结束。
func stopGroup(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	for i := 0; i < 20; i++ {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
