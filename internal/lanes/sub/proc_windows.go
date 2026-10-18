//go:build windows

package sub

import (
	"os/exec"
	"strconv"
)

func ownGroup(cmd *exec.Cmd) {}

// killTree 用 taskkill 连同子进程一起结束（npm 装的 claude、codex 在 Windows 上是 .cmd 包装，真正干活的是它的子进程）。
func killTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	_ = cmd.Process.Kill()
}
