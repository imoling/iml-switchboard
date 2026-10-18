package sub

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 跨平台的假 CLI：测试二进制带上 SB_FAKE_DIR 重新启动自己，就扮演 claude / codex / agy——
// 记下收到的参数和环境变量，读完标准输入，再把 SB_FAKE_OUT 指向的夹具原样输出。
// 不依赖 sh，Windows 上也能跑（红线测试靠它）。
func TestMain(m *testing.M) {
	if dir := os.Getenv("SB_FAKE_DIR"); dir != "" {
		runFakeCLI(dir)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runFakeCLI(dir string) {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("9.9.9")
		return
	}
	n, _ := filepath.Glob(filepath.Join(dir, "args-*"))
	idx := len(n)
	_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("args-%03d", idx)), []byte(strings.Join(os.Args[1:], "\n")), 0o644)
	_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("env-%03d", idx)), []byte(strings.Join(os.Environ(), "\n")), 0o644)
	_, _ = io.Copy(io.Discard, os.Stdin)
	if out := os.Getenv("SB_FAKE_OUT"); out != "" {
		b, _ := os.ReadFile(out)
		_, _ = os.Stdout.Write(b)
	}
}

// needShell 用于还靠 sh 脚本冒充 CLI 的测试：Windows 上跳过。
func needShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("这条测试的假 CLI 是 sh 脚本，Windows 上跳过；红线测试已改用跨平台的假 CLI")
	}
}
