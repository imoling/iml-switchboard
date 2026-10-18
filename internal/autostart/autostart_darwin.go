package autostart

// macOS：往 ~/Library/LaunchAgents 写一个 plist，让 imls serve 登录即起。

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const label = "cn.imoling.switchboard"

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

// Install 写入并加载 plist。
func Install(exe string, logDir string) error {
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>%s</string><string>serve</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
  <key>StandardOutPath</key><string>%s/serve.log</string>
  <key>StandardErrorPath</key><string>%s/serve.log</string>
</dict></plist>
`, label, exe, logDir, logDir)
	p := plistPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), label)).Run()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		return err
	}
	return exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), p).Run()
}

// Uninstall 卸载。
func Uninstall() error {
	_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), label)).Run()
	return os.Remove(plistPath())
}

// Installed 判断是否已安装。
func Installed() bool {
	_, err := os.Stat(plistPath())
	return err == nil
}
