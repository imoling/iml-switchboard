package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Linux：systemd 用户服务，登录会话起来就跑。
const unitName = "switchboard.service"

func unitPath() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "systemd", "user", unitName)
}

// Install 写入并启用用户服务。
func Install(exe string, logDir string) error {
	body := fmt.Sprintf(`[Unit]
Description=Switchboard

[Service]
ExecStart=%s serve
Restart=on-failure

[Install]
WantedBy=default.target
`, exe)
	p := unitPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		return err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return exec.Command("systemctl", "--user", "enable", "--now", unitName).Run()
}

// Uninstall 停用并删除。
func Uninstall() error {
	_ = exec.Command("systemctl", "--user", "disable", "--now", unitName).Run()
	return os.Remove(unitPath())
}

// Installed 判断是否已安装。
func Installed() bool {
	_, err := os.Stat(unitPath())
	return err == nil
}
