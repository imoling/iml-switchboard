package sub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/core"
)

func TestClaudePausedWhenSwitchedToThirdParty(t *testing.T) {
	needShell(t)
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	settings := filepath.Join(home, ".claude", "settings.json")
	_ = os.WriteFile(settings, []byte(`{"env":{"HTTPS_PROXY":"http://127.0.0.1:7890"}}`), 0o644)
	called := filepath.Join(t.TempDir(), "called")
	bin := filepath.Join(t.TempDir(), "claude")
	_ = os.WriteFile(bin, []byte("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 9.9.9; exit 0; fi\ntouch \""+called+"\"\nexit 1\n"), 0o755)
	d := &ClaudeDriver{Bin: bin, Home: home}

	if h := d.Health(context.Background()); h.Status == "warn" && strings.Contains(h.Detail, "第三方") {
		t.Fatalf("只有代理设置不该报：%+v", h)
	}
	// cc-switch 切到第三方后的样子
	_ = os.WriteFile(settings, []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://example.com","ANTHROPIC_AUTH_TOKEN":"sk-secret"}}`), 0o644)
	_, err := d.Run(context.Background(), &core.Task{Kind: core.KindText, Prompt: "x"}, core.Target{Model: "haiku"}, nil)
	var pe *core.ErrProvider
	if !errors.As(err, &pe) || !pe.Retryable || !strings.Contains(pe.Summary, "cc-switch") {
		t.Fatalf("应暂停并允许降级：%v", err)
	}
	if strings.Contains(pe.Raw, "sk-secret") || strings.Contains(pe.Summary, "sk-secret") {
		t.Fatal("令牌的值不能出现在任何地方")
	}
	if _, err := os.Stat(called); err == nil {
		t.Fatal("发现第三方接口后不该再启动 claude")
	}
	if h := d.Health(context.Background()); h.Status != "warn" {
		t.Fatalf("健康状态应提示：%+v", h)
	}
	// 项目里的设置也算（动画在项目目录里跑）
	_ = os.WriteFile(settings, []byte(`{}`), 0o644)
	proj := t.TempDir()
	_ = os.MkdirAll(filepath.Join(proj, ".claude"), 0o755)
	_ = os.WriteFile(filepath.Join(proj, ".claude", "settings.local.json"), []byte(`{"apiKeyHelper":"/bin/echo k"}`), 0o644)
	if _, err := d.Run(context.Background(), &core.Task{Kind: core.KindAnimation, Prompt: "x", ProjectDir: proj}, core.Target{Model: "opus"}, nil); !errors.As(err, &pe) || !strings.Contains(pe.Raw, "apiKeyHelper") {
		t.Fatalf("项目设置里的 apiKeyHelper 也要拦：%v", err)
	}
}
