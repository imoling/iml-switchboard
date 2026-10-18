package sub

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/core"
)

// 订阅子进程不能再加载 Switchboard 自己，否则子任务可能派回来，一层套一层。
func TestSubprocessesDoNotLoopBack(t *testing.T) {
	cd := &ClaudeDriver{}
	for _, task := range []*core.Task{{Kind: core.KindText, Prompt: "x"}, {Kind: core.KindAnimation, Prompt: "x", ProjectDir: t.TempDir()}} {
		if args := cd.BuildArgs(task, core.Target{Model: "haiku"}); !slices.Contains(args, "--strict-mcp-config") {
			t.Errorf("claude 参数里没有 --strict-mcp-config：%v", args)
		}
	}

	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	xd := &CodexDriver{Home: home}
	args, _ := xd.BuildArgs(&core.Task{Kind: core.KindText, Prompt: "x"}, core.Target{})
	if strings.Contains(strings.Join(args, " "), "mcp_servers") {
		t.Errorf("没注册时不能覆盖，Codex 会报配置加载失败：%v", args)
	}
	_ = os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("[mcp_servers.iml-switchboard]\ncommand = \"imls\"\n"), 0o644)
	args, _ = xd.BuildArgs(&core.Task{Kind: core.KindText, Prompt: "x"}, core.Target{})
	if !strings.Contains(strings.Join(args, " "), "mcp_servers.iml-switchboard.enabled=false") {
		t.Errorf("注册过就要在子进程里关掉：%v", args)
	}
}

func TestSubprocessGetsInnerMarker(t *testing.T) {
	needShell(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "p")
	_ = os.WriteFile(script, []byte("#!/bin/sh\necho \"$"+InnerEnv+"\"\n"), 0o755)
	var got string
	runProc(t.Context(), procSpec{Bin: script, Env: []string{"PATH=/bin:/usr/bin"}, OnLine: func(l string) { got = l }})
	if got != "1" {
		t.Fatalf("子进程没拿到 %s 标记：%q", InnerEnv, got)
	}
}

// Windows 上 claude 是 npm 的 .cmd 包装：宿主的系统提示不能进参数（cmd.exe 会解析 & | %），改走标准输入。
func TestBatchWrapperKeepsHostTextOutOfArgs(t *testing.T) {
	if !isBatch(`C:\Users\a\AppData\Roaming\npm\claude.CMD`) || isBatch("/opt/homebrew/bin/claude") || isBatch(`C:\x\claude.exe`) {
		t.Fatal("isBatch 判断不对")
	}
	d := &ClaudeDriver{}
	evil := "你是翻译 & del /q C:\\* | %PATH%"
	for _, task := range []*core.Task{{Kind: core.KindText, Prompt: "x", System: evil}, {Kind: core.KindAnimation, Prompt: "x", System: evil, ProjectDir: t.TempDir()}} {
		args := d.buildArgs(task, core.Target{Model: "haiku"}, true)
		if strings.Contains(strings.Join(args, " "), "del /q") {
			t.Fatalf("宿主的系统提示进了参数：%q", args)
		}
		if p := stdinPayload(task, true); !strings.Contains(p, "del /q") {
			t.Fatalf("宿主的系统提示应该在标准输入里：%s", p)
		}
	}
}
