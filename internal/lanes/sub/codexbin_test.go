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
