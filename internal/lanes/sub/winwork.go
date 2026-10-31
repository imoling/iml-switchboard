package sub

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/imoling/iml-switchboard/internal/core"
)

// NewWinWorkDriver 把 WinWork（讯飞）内嵌的 Claude Code 当成一条渠道。
// WinWork 自带一个包装脚本，运行时自己设好 CLAUDE_CONFIG_DIR 指向 ~/.kooky-data/claude-config，
// 网关地址和令牌都在那个目录的 settings.json 里，由它自己读；这里只启动脚本，不碰那个文件。
func NewWinWorkDriver() *ClaudeDriver {
	home, _ := os.UserHomeDir()
	return newWinWorkDriver(home, runtime.GOOS)
}

func newWinWorkDriver(home, goos string) *ClaudeDriver {
	bin := ""
	switch goos {
	case "darwin":
		bin = filepath.Join(home, "Library", "Application Support", "Kooky", "claude-wrapper", "claude")
	case "windows":
		bin = filepath.Join(os.Getenv("APPDATA"), "Kooky", "claude-wrapper", "claude.cmd")
	}
	return &ClaudeDriver{Lane: core.LaneWinWorkSub, Name: "WinWork", Bin: bin, ConfigDir: filepath.Join(home, ".kooky-data", "claude-config")}
}
