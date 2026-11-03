package sub

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// CodexMissing 是找不到 codex 命令时给用户看的提示。
const CodexMissing = "本机没有找到 codex 命令：装 ChatGPT 桌面版，或 npm install -g @openai/codex"

// CodexAppBins 列出桌面应用自带的 codex 命令。ChatGPT 桌面版把它放在应用包里，不在 PATH 上；测试可替换。
var CodexAppBins = func() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	const rel = "ChatGPT.app/Contents/Resources/codex-cli/bin/codex"
	out := []string{filepath.Join("/Applications", rel)}
	if home, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(home, "Applications", rel))
	}
	return out
}

// CodexBin 找本机的 codex 命令：先看 PATH，再看桌面应用自带的那份。找不到返回空串。
func CodexBin() string {
	if p, err := exec.LookPath("codex"); err == nil {
		return p
	}
	for _, p := range CodexAppBins() {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// CodexOnPath 判断 codex 是不是在 PATH 上：不在的话给用户的命令要写全路径。
func CodexOnPath() bool {
	_, err := exec.LookPath("codex")
	return err == nil
}
