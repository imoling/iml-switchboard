package autostart

import (
	"fmt"
	"os/exec"
)

// Windows：写当前用户的 Run 键（不需要管理员权限）。--background 让后台关掉自己的命令行窗口，登录时不留黑框。
const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
const valueName = "Switchboard"

// Install 写入 Run 键。logDir 在 Windows 上由 serve --background 自己处理。
func Install(exe string, logDir string) error {
	cmd := fmt.Sprintf(`"%s" serve --background`, exe)
	if out, err := exec.Command("reg", "add", runKey, "/v", valueName, "/t", "REG_SZ", "/d", cmd, "/f").CombinedOutput(); err != nil {
		return fmt.Errorf("写注册表失败：%s", out)
	}
	return nil
}

// Uninstall 删除 Run 键里的这一项。
func Uninstall() error {
	_ = exec.Command("reg", "delete", runKey, "/v", valueName, "/f").Run()
	return nil
}

// Installed 判断是否已注册。
func Installed() bool {
	return exec.Command("reg", "query", runKey, "/v", valueName).Run() == nil
}
