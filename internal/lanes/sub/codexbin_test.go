package sub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// PATH 上没有 codex 时，要认出 ChatGPT 桌面版应用包里自带的那份。
func TestCodexBinFallsBackToAppBundle(t *testing.T) {
	needShell(t) // 假 codex 是 sh 脚本，而且 Windows 没有可执行位；应用包里的 codex 本来也只有 macOS 有
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	fake := filepath.Join(dir, "codex")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho codex-cli 0.0.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := CodexAppBins
	t.Cleanup(func() { CodexAppBins = old })
	CodexAppBins = func() []string { return []string{filepath.Join(dir, "missing"), fake} }
	if got := CodexBin(); got != fake {
		t.Fatalf("应找到应用包里的 codex：%q", got)
	}
	d := &CodexDriver{Home: t.TempDir()}
	h := d.Health(context.Background())
	if h.Status == "missing" {
		t.Fatalf("应用包里的 codex 应被认出来：%+v", h)
	}
	if !h.SignedOut || !strings.Contains(h.Detail, fake+" login") {
		t.Fatalf("命令不在 PATH 上时，登录提示要写全路径：%+v", h)
	}

	CodexAppBins = func() []string { return nil }
	if got := CodexBin(); got != "" {
		t.Fatalf("PATH 和应用包都没有时应返回空：%q", got)
	}
	if h := d.Health(context.Background()); h.Status != "missing" || h.Detail != CodexMissing {
		t.Fatalf("都没有时应报没找到：%+v", h)
	}
}

// 版本号要缓存：健康检查每页都跑，不能每次都起一个进程；文件换了（升级）才重问。
func TestVersionOfCached(t *testing.T) {
	needShell(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "cli")
	log := filepath.Join(dir, "calls.log")
	write := func(ver string) {
		if err := os.WriteFile(bin, []byte("#!/bin/sh\necho x >> "+log+"\necho "+ver+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("1.0.0")
	ctx := context.Background()
	if v := versionOf(ctx, bin); v != "1.0.0" {
		t.Fatalf("第一次应问到 1.0.0：%q", v)
	}
	versionOf(ctx, bin)
	versionOf(ctx, bin)
	if b, _ := os.ReadFile(log); strings.Count(string(b), "x") != 1 {
		t.Fatalf("十分钟内应复用缓存，只起一次进程：%q", b)
	}
	// 文件变了：大小不同，立刻重问
	write("1.0.10")
	if v := versionOf(ctx, bin); v != "1.0.10" {
		t.Fatalf("文件换了应重问：%q", v)
	}
}
