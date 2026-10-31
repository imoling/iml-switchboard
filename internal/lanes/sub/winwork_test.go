package sub

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/imoling/iml-switchboard/internal/core"
)

func TestStripANSIPrefix(t *testing.T) {
	if got := stripANSIPrefix("\x1b[2J\x1b[3J\x1b[H{\"type\":\"system\"}"); got != `{"type":"system"}` {
		t.Fatalf("%q", got)
	}
	if got := stripANSIPrefix(`{"a":1}`); got != `{"a":1}` {
		t.Fatalf("没有控制符的行不该动：%q", got)
	}
}

// WinWork 渠道：没装时报未安装；装了但它的配置目录里没有 settings.json 算没登录；
// 它的网关是自己的，不能按「切到了第三方接口」拦下来；套餐不读用户自己的 ~/.claude.json
func TestWinWorkDriver(t *testing.T) {
	home := t.TempDir()
	d := newWinWorkDriver(home, "darwin")
	if d.ID() != core.LaneWinWorkSub || d.name() != "WinWork" {
		t.Fatalf("%s %s", d.ID(), d.name())
	}
	if h := d.Health(context.Background()); h.Status != "missing" || h.Detail != "本机没有安装 WinWork" {
		t.Fatalf("%+v", h)
	}
	// 假的包装脚本：先打清屏控制符再报版本，和真的一样
	_ = os.MkdirAll(filepath.Dir(d.Bin), 0o755)
	_ = os.WriteFile(d.Bin, []byte("#!/bin/sh\nprintf '\\033[2J\\033[3J\\033[H'\necho '2.1.287 (Claude Code)'\n"), 0o755)
	if h := d.Health(context.Background()); h.Status != "warn" {
		t.Fatalf("没有 settings.json 应算没登录：%+v", h)
	}
	_ = os.MkdirAll(d.ConfigDir, 0o755)
	_ = os.WriteFile(filepath.Join(d.ConfigDir, "settings.json"), []byte(`{"env":{"ANTHROPIC_BASE_URL":"https://one.example/kc","ANTHROPIC_AUTH_TOKEN":"x"}}`), 0o600)
	_ = os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"oauthAccount":{"organizationType":"claude_max"}}`), 0o600)
	h := d.Health(context.Background())
	if h.Status != "ok" || h.Detail != "2.1.287 (Claude Code)" || h.Plan != "" {
		t.Fatalf("有网关的配置目录应算正常，版本号不带控制符，不读用户自己的套餐：%+v", h)
	}
	if files := d.claudeSettingsFiles(""); files != nil {
		t.Fatalf("不该去查第三方接口：%v", files)
	}
}
