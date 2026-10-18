package main

import "syscall"

// hideConsole 让后台脱离命令行窗口：开机自启时 Windows 会给控制台程序开一个黑框，脱离后它就关掉了。
func hideConsole() {
	_, _, _ = syscall.NewLazyDLL("kernel32.dll").NewProc("FreeConsole").Call()
}
