//go:build !windows

package procutil

import "os/exec"

// Hide 只在 Windows 上有事做。
func Hide(cmd *exec.Cmd) {}
