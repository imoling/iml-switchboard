// Package procutil 统一后台拉起子进程的方式。
//
// Windows 上后台用 FreeConsole 脱离了控制台，此后它启动的每个命令行程序（claude、codex、reg…）
// 都会被系统新开一个黑框，启动自检一跑就一闪一闪。所有子进程都要经过 Command / Hide 设上不开窗口。
package procutil

import (
	"context"
	"os/exec"
)

// Command 等价于 exec.Command，在 Windows 上不弹命令行窗口。
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	Hide(cmd)
	return cmd
}

// CommandContext 等价于 exec.CommandContext，在 Windows 上不弹命令行窗口。
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	Hide(cmd)
	return cmd
}
