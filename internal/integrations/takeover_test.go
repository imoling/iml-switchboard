package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 接管 Claude Code：env 指向本机门面；关掉后原值放回，原本没有的键删掉；其它设置一字不动
func TestClaudeTakeover(t *testing.T) {
	m, home := setup(t)
	path := filepath.Join(home, ".claude", "settings.json")
	_ = os.WriteFile(path, []byte(`{"model":"fable","theme":"dark","env":{"HTTP_PROXY":"http://127.0.0.1:7890","ANTHROPIC_MODEL":"opus"}}`), 0o600)
	if m.claudeTakenOver() {
		t.Fatal("还没接管")
	}
	for i := 0; i < 2; i++ { // 重复开是幂等的，原值不会被覆盖成我们写的
		if err := m.setClaudeTakeover(true); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(path)
	var d struct {
		Model string            `json:"model"`
		Theme string            `json:"theme"`
		Env   map[string]string `json:"env"`
	}
	_ = json.Unmarshal(b, &d)
	if d.Env["NO_PROXY"] != "127.0.0.1,localhost" {
		t.Fatalf("配了代理时本机地址要绕过：%s", b)
	}
	if !m.claudeTakenOver() || d.Env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:18081" || d.Env["ANTHROPIC_AUTH_TOKEN"] != "imls-key.claude_code" || d.Env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "router-low" || d.Env["ANTHROPIC_MODEL"] != "router-auto" {
		t.Fatalf("接管后的 env 不对：%s", b)
	}
	if d.Model != "fable" || d.Theme != "dark" || d.Env["HTTP_PROXY"] != "http://127.0.0.1:7890" {
		t.Fatalf("其它设置不能动：%s", b)
	}
	if err := m.setClaudeTakeover(false); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	d.Env = nil // 重新解析，别把上一次的键带进来
	_ = json.Unmarshal(b, &d)
	if m.claudeTakenOver() || d.Env["ANTHROPIC_MODEL"] != "opus" || d.Env["HTTP_PROXY"] == "" {
		t.Fatalf("关掉后应恢复原值：%s", b)
	}
	for _, k := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_DEFAULT_OPUS_MODEL"} {
		if _, ok := d.Env[k]; ok {
			t.Fatalf("原本没有的键应删掉：%s", k)
		}
	}
	if _, err := os.Stat(takeoverSavePath("claude_code")); err == nil {
		t.Fatal("关掉后备份应清掉")
	}
}

// 接管 Codex：加一个走 Chat 协议的供应商并切过去，密钥在请求头里；关掉恢复原供应商和模型；不碰 auth.json
func TestCodexTakeover(t *testing.T) {
	m, home := setup(t)
	path := filepath.Join(home, ".codex", "config.toml")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	orig := "model_provider = \"OpenAI\"\nmodel = \"gpt-5.5\"\nmodel_reasoning_effort = \"high\"\n\n[model_providers.OpenAI]\nname = \"OpenAI\"\nwire_api = \"responses\"\n\n[mcp_servers.x]\ncommand = \"x\"\n"
	_ = os.WriteFile(path, []byte(orig), 0o600)
	_ = os.WriteFile(filepath.Join(home, ".codex", "auth.json"), []byte(`{"tokens":"secret"}`), 0o600)
	for i := 0; i < 2; i++ {
		if err := m.setCodexTakeover(true); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if !m.codexTakenOver() || topLevelValue(s, "model") != "router-auto" || strings.Count(s, "[model_providers.switchboard]") != 1 || !strings.Contains(s, `wire_api = "responses"`) || !strings.Contains(s, `Authorization = "Bearer imls-key.codex"`) {
		t.Fatalf("接管后的配置不对：\n%s", s)
	}
	if !strings.Contains(s, `model_reasoning_effort = "high"`) || !strings.Contains(s, "[mcp_servers.x]") || !strings.Contains(s, "[model_providers.OpenAI]") {
		t.Fatalf("其它内容不能丢：\n%s", s)
	}
	if err := m.setCodexTakeover(false); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	s = string(b)
	if m.codexTakenOver() || topLevelValue(s, "model_provider") != "OpenAI" || topLevelValue(s, "model") != "gpt-5.5" || strings.Contains(s, "switchboard") {
		t.Fatalf("关掉后应恢复：\n%s", s)
	}
	if a, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json")); string(a) != `{"tokens":"secret"}` {
		t.Fatal("auth.json 不能动")
	}
}
