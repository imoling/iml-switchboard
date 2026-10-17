package integrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
)

func setup(t *testing.T) (*Manager, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("IMLS_DATA_DIR", filepath.Join(home, "data"))
	cs, err := config.Open()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	_ = os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"model":"fable","theme":"dark"}`), 0o644)
	_ = os.WriteFile(filepath.Join(home, ".claude", "CLAUDE.md"), []byte("# 我的规则\n\n- 中文交流\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(home, ".hermes"), 0o755)
	_ = os.WriteFile(filepath.Join(home, ".hermes", "config.yaml"), []byte("_config_version: 33\n# 这是注释，不能丢\nplatforms:\n  homeassistant:\n    enabled: false\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(home, ".workbuddy"), 0o755)
	_ = os.WriteFile(filepath.Join(home, ".workbuddy", "models.json"), []byte(`[{"id":"deepseek-v4-pro","name":"DeepSeek","vendor":"DeepSeek","url":"https://api.deepseek.com/chat/completions","apiKey":"x","supportsToolCall":true}]`), 0o644)
	m := New(cs, "/usr/local/bin/imls", "http://127.0.0.1:18081", func() string { return "imls-key" })
	return m, home
}

func TestClaudeMdAndCommandsAndOpusplan(t *testing.T) {
	m, home := setup(t)
	if err := m.setClaudeMd(true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md"))
	if !strings.HasPrefix(string(b), "# 我的规则") || strings.Count(string(b), markStart) != 1 || !strings.Contains(string(b), "generate_image") {
		t.Fatalf("块应追加在末尾且只有一份：\n%s", b)
	}
	_ = m.setClaudeMd(true) // 幂等
	b, _ = os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md"))
	if strings.Count(string(b), markStart) != 1 {
		t.Fatal("重复写入应只保留一份")
	}
	_ = m.setClaudeMd(false)
	b, _ = os.ReadFile(filepath.Join(home, ".claude", "CLAUDE.md"))
	if strings.Contains(string(b), markStart) || !strings.Contains(string(b), "中文交流") {
		t.Fatalf("移除后应只剩原内容：\n%s", b)
	}
	if err := m.setCommands(true); err != nil || !m.commandsInstalled() {
		t.Fatal(err)
	}
	_ = m.setCommands(false)
	if m.commandsInstalled() {
		t.Fatal("命令应被移除")
	}
	if err := m.setOpusplan(true); err != nil || !m.claudeModelIs("opusplan") {
		t.Fatal(err)
	}
	_ = m.setOpusplan(false)
	if !m.claudeModelIs("fable") {
		t.Fatal("关闭后应恢复 fable")
	}
	b, _ = os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if !strings.Contains(string(b), `"theme": "dark"`) {
		t.Fatalf("其它设置不能丢：%s", b)
	}
}

func TestHermes(t *testing.T) {
	m, home := setup(t)
	if _, err := m.Connect("hermes", nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, ".hermes", "config.yaml"))
	s := string(b)
	for _, want := range []string{"# 这是注释，不能丢", "mcp_servers:", "iml-switchboard:", "command: /usr/local/bin/imls", "IMLS_HOST: hermes", "image_gen:", "base_url: http://127.0.0.1:18081/v1", "api_key: imls-key", "_config_version: 33"} {
		if !strings.Contains(s, want) {
			t.Fatalf("缺少 %q：\n%s", want, s)
		}
	}
	if m.view("hermes")["status"] != "connected" {
		t.Fatal("应为 connected")
	}
	backups, _ := filepath.Glob(filepath.Join(home, ".hermes", "config.yaml.bak-imls-*"))
	if len(backups) != 1 {
		t.Fatal("应有一份备份")
	}
	if _, err := m.Disconnect("hermes"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(home, ".hermes", "config.yaml"))
	if strings.Contains(string(b), "iml-switchboard") || strings.Contains(string(b), "image_gen") || !strings.Contains(string(b), "homeassistant") {
		t.Fatalf("断开后应干净：\n%s", b)
	}
}

func TestWorkBuddy(t *testing.T) {
	m, home := setup(t)
	if _, err := m.Connect("workbuddy", nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(home, ".workbuddy", "models.json"))
	s := string(b)
	if !strings.Contains(s, `"router-auto"`) || !strings.Contains(s, `"router-sub"`) || !strings.Contains(s, `"deepseek-v4-pro"`) || strings.Count(s, `"apiKey": "imls-key"`) != 2 {
		t.Fatalf("%s", s)
	}
	_, _ = m.Connect("workbuddy", nil) // 幂等
	b, _ = os.ReadFile(filepath.Join(home, ".workbuddy", "models.json"))
	if strings.Count(string(b), `"router-auto"`) != 1 {
		t.Fatal("重复接入不应重复添加")
	}
	_, _ = m.Disconnect("workbuddy")
	b, _ = os.ReadFile(filepath.Join(home, ".workbuddy", "models.json"))
	if strings.Contains(string(b), "router-") || !strings.Contains(string(b), "deepseek-v4-pro") {
		t.Fatalf("%s", b)
	}
	if !strings.Contains(m.workbuddySnippet(), "router-auto") {
		t.Fatal("片段应包含模型")
	}
}
