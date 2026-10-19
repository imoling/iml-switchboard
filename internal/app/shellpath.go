//go:build !windows

package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// FixPath 补全 PATH。从访达、Dock、开机自启拉起的后台只拿到 /usr/bin:/bin:/usr/sbin:/sbin，
// 找不到装在 Homebrew、npm 全局目录里的 claude、codex、node；这里问一次用户的登录 shell，再补上常见目录。
func FixPath() {
	parts := strings.Split(os.Getenv("PATH"), ":")
	parts = append(parts, loginShellPath()...)
	if home, err := os.UserHomeDir(); err == nil {
		parts = append(parts,
			"/opt/homebrew/bin", "/usr/local/bin",
			filepath.Join(home, ".local", "bin"), filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, ".bun", "bin"), filepath.Join(home, ".volta", "bin"), filepath.Join(home, ".cargo", "bin"))
	}
	os.Setenv("PATH", mergePath(parts))
}

// mergePath 去重、去空，保持先来后到：已有的 PATH 优先
func mergePath(parts []string) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return strings.Join(out, ":")
}

// loginShellPath 用登录交互 shell 读 PATH（nvm、fnm 这类只在 .zshrc 里生效）；3 秒拿不到就算了
func loginShellPath() []string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	const mark = "__IMLS_PATH__"
	cmd := exec.CommandContext(ctx, sh, "-ilc", "printf '"+mark+"%s"+mark+"' \"$PATH\"")
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	s := string(out)
	i := strings.Index(s, mark)
	j := strings.LastIndex(s, mark)
	if i < 0 || j <= i {
		return nil
	}
	return strings.Split(s[i+len(mark):j], ":")
}
