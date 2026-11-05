//go:build windows

package laya

import (
	"os"
	"os/exec"
)

func ownGroup(cmd *exec.Cmd) {}

func stopGroup(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}
