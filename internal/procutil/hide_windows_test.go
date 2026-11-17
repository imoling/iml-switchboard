//go:build windows

package procutil

import (
	"syscall"
	"testing"
)

func TestHideSetsNoWindow(t *testing.T) {
	cmd := Command("cmd", "/c", "exit")
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("要设不开窗口：%+v", cmd.SysProcAttr)
	}
	// 已有的标志（比如脱离控制台）不能被冲掉
	c2 := Command("cmd")
	c2.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008}
	Hide(c2)
	if c2.SysProcAttr.CreationFlags&0x00000008 == 0 || c2.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("要保留原有标志：%x", c2.SysProcAttr.CreationFlags)
	}
}
