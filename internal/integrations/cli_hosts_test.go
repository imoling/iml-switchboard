package integrations

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 假的 codex、gemini：记下参数，并像真的一样改配置文件，测试不碰本机真实配置。
func fakeHostBins(t *testing.T, home string) string {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(bin, "calls.log")
	codex := `#!/bin/sh
echo "codex $*" >> "` + log + `"
cfg="$HOME/.codex/config.toml"; mkdir -p "$HOME/.codex"
case "$2" in
  add) printf '[mcp_servers.iml-switchboard]\ncommand = "imls"\n' >> "$cfg";;
  remove) grep -v 'iml-switchboard\|command = "imls"' "$cfg" > "$cfg.tmp"; mv "$cfg.tmp" "$cfg";;
esac
`
	gemini := `#!/bin/sh
echo "gemini $*" >> "` + log + `"
mkdir -p "$HOME/.gemini"
case "$2" in
  add) echo '{"mcpServers":{"iml-switchboard":{"command":"imls"}}}' > "$HOME/.gemini/settings.json";;
  remove) echo '{"mcpServers":{}}' > "$HOME/.gemini/settings.json";;
esac
`
	_ = os.WriteFile(filepath.Join(bin, "codex"), []byte(codex), 0o755)
	_ = os.WriteFile(filepath.Join(bin, "gemini"), []byte(gemini), 0o755)
	t.Setenv("PATH", bin+":/bin:/usr/bin")
	return log
}

func TestCodexAndGemini(t *testing.T) {
	needShell(t)
	m, home := setup(t)
	log := fakeHostBins(t, home)
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	_ = os.WriteFile(filepath.Join(home, ".codex", "AGENTS.md"), []byte("# 我的 Codex 规则\n"), 0o644)

	for _, id := range []string{"codex", "gemini_cli"} {
		if v := m.view(id); v["status"] != "disconnected" || v["available"] != true {
			t.Fatalf("%s 接入前状态不对：%v", id, v)
		}
		v, err := m.Connect(id, nil)
		if err != nil || v["status"] != "connected" {
			t.Fatalf("%s 接入失败：%v %v", id, err, v)
		}
		// 再接一次不重复注册
		if _, err := m.Connect(id, nil); err != nil {
			t.Fatal(err)
		}
	}
	calls, _ := os.ReadFile(log)
	if strings.Count(string(calls), "mcp add") != 2 {
		t.Fatalf("每个宿主只该注册一次：\n%s", calls)
	}
	if !strings.Contains(string(calls), "--env IMLS_HOST=codex -- /usr/local/bin/imls mcp") || !strings.Contains(string(calls), "-s user -e IMLS_HOST=gemini_cli iml-switchboard /usr/local/bin/imls mcp") {
		t.Fatalf("注册参数不对：\n%s", calls)
	}
	agents, _ := os.ReadFile(filepath.Join(home, ".codex", "AGENTS.md"))
	if !strings.HasPrefix(string(agents), "# 我的 Codex 规则") || strings.Count(string(agents), markStart) != 1 {
		t.Fatalf("AGENTS.md 应保留原内容并只加一段：\n%s", agents)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".gemini", "GEMINI.md")); !strings.Contains(string(b), "ask_model") {
		t.Fatalf("GEMINI.md 没写调用约定：%s", b)
	}

	for _, id := range []string{"codex", "gemini_cli"} {
		if v, err := m.Disconnect(id); err != nil || v["status"] != "disconnected" {
			t.Fatalf("%s 断开失败：%v %v", id, err, v)
		}
	}
	agents, _ = os.ReadFile(filepath.Join(home, ".codex", "AGENTS.md"))
	if strings.TrimSpace(string(agents)) != "# 我的 Codex 规则" {
		t.Fatalf("断开后 AGENTS.md 应恢复原样：%q", agents)
	}
	if _, err := os.Stat(filepath.Join(home, ".gemini", "GEMINI.md")); !os.IsNotExist(err) {
		t.Fatal("GEMINI.md 是我们建的，去掉调用约定后应删掉")
	}
}

// 走兼容入口的宿主写进去的密钥要带宿主标记。
func TestFacadeHostsUseTaggedKeys(t *testing.T) {
	m, _ := setup(t)
	if k := m.piProvider().APIKey; k != "imls-key.pi" {
		t.Errorf("pi 的密钥：%s", k)
	}
	if k, _ := m.openclawProvider()["apiKey"].(string); k != "imls-key.openclaw" {
		t.Errorf("openclaw 的密钥：%s", k)
	}
	if s := m.dshSnippet(); !strings.Contains(s, "imls-key.deepseek_harness") {
		t.Errorf("dsh 片段里的密钥：%s", s)
	}
}

// needShell：这里的假 codex、gemini 是 sh 脚本，Windows 上跳过。
func needShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("假 CLI 是 sh 脚本，Windows 上跳过")
	}
}
