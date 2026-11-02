package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
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
	if strings.Contains(s, codexCatalogKey) {
		t.Fatalf("原本没有目录指针，关掉后不该留下：\n%s", s)
	}
	if _, err := os.Stat(codexCatalogPath()); err == nil {
		t.Fatal("关掉后目录文件应清掉")
	}
}

func readCodexCatalog(t *testing.T) map[string]map[string]any {
	t.Helper()
	b, err := os.ReadFile(codexCatalogPath())
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	rows := map[string]map[string]any{}
	for _, r := range doc.Models {
		rows[r["slug"].(string)] = r
	}
	return rows
}

// Codex 目录：/model 列档位和账号型号，官方型号隐藏保留；每一行都不能开门面接不住的工具形状
func TestCodexCatalog(t *testing.T) {
	m, home := setup(t)
	_ = m.cfg.Update(func(c *config.Config) error {
		c.API = []config.APIAccount{
			{ID: "a", Enabled: true, Models: map[string][]string{"high": {"glm-5.3"}, "low": {"winwork-snt-5.5"}}},
			{ID: "b", Enabled: false, Models: map[string][]string{"mid": {"off-model"}}},
		}
		return nil
	})
	m.ModelName = func(id string) string { return "展示名 " + id }
	m.ModelContext = func(id string) int { return map[string]int{"glm-5.3": 500000, "winwork-snt-5.5": 200000}[id] }
	if err := m.setCodexTakeover(true); err != nil {
		t.Fatal(err)
	}
	rows := readCodexCatalog(t)
	for _, slug := range []string{"router-auto", "router-high", "router-mid", "router-low", "glm-5.3", "winwork-snt-5.5"} {
		if rows[slug]["visibility"] != "list" {
			t.Fatalf("%s 应在列表里：%v", slug, rows[slug])
		}
	}
	if _, ok := rows["off-model"]; ok {
		t.Fatal("停用账号的型号不该列出")
	}
	if rows["gpt-5.5"]["visibility"] != "hide" || rows["codex-auto-review"]["visibility"] != "hide" {
		t.Fatal("官方型号要隐藏保留")
	}
	for slug, r := range rows {
		_, code := r["tool_mode"]
		_, patch := r["apply_patch_tool_type"] // 这个字段只接受 freeform，写别的 Codex 起不来；freeform 又是门面接不住的 custom 工具
		if code || patch || r["supports_search_tool"] != false || r["use_responses_lite"] != false {
			t.Fatalf("%s 会让工具被门面丢掉：%v", slug, r)
		}
	}
	if rows["router-auto"]["base_instructions"] != "模板提示词" || rows["glm-5.3"]["display_name"] != "展示名 glm-5.3" {
		t.Fatalf("应从没开代码模式的行克隆：%v", rows["router-auto"])
	}
	if rows["router-auto"]["context_window"] != float64(200000) || rows["router-high"]["context_window"] != float64(500000) || rows["router-mid"]["context_window"] != float64(272000) {
		t.Fatalf("窗口取可能落到的型号里最小的，不知道时沿用模板：%v %v %v", rows["router-auto"]["context_window"], rows["router-high"]["context_window"], rows["router-mid"]["context_window"])
	}

	// 账号增减后目录跟上
	_ = m.cfg.Update(func(c *config.Config) error {
		c.API[1].Enabled = true
		return nil
	})
	m.SyncCodexCatalog()
	if _, ok := readCodexCatalog(t)["off-model"]; !ok {
		t.Fatal("账号启用后目录要跟上")
	}

	// 目录没变就不重新生成
	m.codexBundled = func() ([]byte, error) { t.Fatal("没变不该重新生成"); return nil, nil }
	m.SyncCodexCatalog()

	// 早先开的接管没有目录指针：补上
	path := filepath.Join(home, ".codex", "config.toml")
	b, _ := os.ReadFile(path)
	_ = os.WriteFile(path, []byte(setTopLevel(string(b), codexCatalogKey, "")), 0o600)
	m.codexBundled = func() ([]byte, error) { return []byte(fakeCodexCatalog), nil }
	m.SyncCodexCatalog()
	b, _ = os.ReadFile(path)
	if topLevelValue(string(b), codexCatalogKey) != codexCatalogPath() {
		t.Fatalf("应补上目录指针：\n%s", b)
	}
}

// 用户自己的目录指针：接管时让位，关掉时放回；生成不了目录也不拦接管
func TestCodexCatalogRestoreAndFailure(t *testing.T) {
	m, home := setup(t)
	path := filepath.Join(home, ".codex", "config.toml")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte("model = \"gpt-5.5\"\nmodel_catalog_json = \"/mine/catalog.json\"\n"), 0o600)
	m.codexBundled = func() ([]byte, error) { return nil, os.ErrNotExist }
	if err := m.setCodexTakeover(true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !m.codexTakenOver() || topLevelValue(string(b), codexCatalogKey) != "" {
		t.Fatalf("生成失败时接管照常，指针不留：\n%s", b)
	}
	if err := m.setCodexTakeover(false); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if topLevelValue(string(b), codexCatalogKey) != "/mine/catalog.json" {
		t.Fatalf("原来的目录指针要放回：\n%s", b)
	}
}

// 本机 Codex 试读不过的目录不能留下：接管照常，指针不写，否则 Codex 起不来
func TestCodexCatalogRejected(t *testing.T) {
	m, home := setup(t)
	path := filepath.Join(home, ".codex", "config.toml")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte("model = \"gpt-5.5\"\n"), 0o600)
	m.codexCheck = func(p string) error {
		if _, err := os.Stat(p); err != nil || p == codexCatalogPath() {
			t.Fatalf("要试读临时文件：%s %v", p, err)
		}
		return os.ErrInvalid
	}
	if err := m.setCodexTakeover(true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !m.codexTakenOver() || topLevelValue(string(b), codexCatalogKey) != "" {
		t.Fatalf("试读失败不能写指针：\n%s", b)
	}
	left, _ := filepath.Glob(codexCatalogPath() + "*")
	if len(left) != 0 {
		t.Fatalf("不能留下文件：%v", left)
	}
}

// Codex 升级后要重新校验目录：读得了就换新的；读不了就撤掉指针，否则 Codex 起不来；失败过不反复试
func TestCodexCatalogAfterUpgrade(t *testing.T) {
	m, home := setup(t)
	path := filepath.Join(home, ".codex", "config.toml")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte("model = \"gpt-5.5\"\n"), 0o600)
	if err := m.setCodexTakeover(true); err != nil {
		t.Fatal(err)
	}
	pointer := func() string {
		b, _ := os.ReadFile(path)
		return topLevelValue(string(b), codexCatalogKey)
	}
	checks := 0
	m.codexCheck = func(string) error { checks++; return nil }

	m.SyncCodexCatalog()
	if checks != 0 {
		t.Fatal("版本没变不该重新生成")
	}
	m.codexStamp = func() string { return "" }
	m.SyncCodexCatalog()
	if checks != 0 || pointer() == "" {
		t.Fatal("找不到 Codex 时不算过期")
	}

	m.codexStamp = func() string { return "codex 2" }
	m.SyncCodexCatalog()
	if checks != 1 || pointer() != codexCatalogPath() {
		t.Fatalf("升级后要重新生成：%d %q", checks, pointer())
	}

	m.codexStamp = func() string { return "codex 3" }
	m.codexCheck = func(string) error { checks++; return os.ErrInvalid }
	m.SyncCodexCatalog()
	if checks != 2 || pointer() != "" {
		t.Fatalf("新版读不了要撤掉指针：%d %q", checks, pointer())
	}
	if _, err := os.Stat(codexCatalogPath()); err == nil {
		t.Fatal("读不了的目录不留")
	}
	if !m.codexTakenOver() {
		t.Fatal("接管不受影响")
	}
	m.SyncCodexCatalog()
	if checks != 2 {
		t.Fatal("同样的情况失败过就不再试")
	}

	m.codexCheck = func(string) error { return nil }
	m.codexStamp = func() string { return "codex 4" }
	m.SyncCodexCatalog()
	if pointer() != codexCatalogPath() {
		t.Fatal("再升级后要补回来")
	}
}

// 接管 Claude Desktop：写配置库并切到第三方模式；关掉后原值放回，别人的配置和其它设置一字不动
func TestDesktopTakeover(t *testing.T) {
	m, _ := setup(t)
	normal, third := m.desktopDirs()
	_ = os.MkdirAll(normal, 0o755)
	_ = os.MkdirAll(m.desktopLibrary(), 0o755)
	normalCfg := filepath.Join(normal, desktopConfigFile)
	_ = os.WriteFile(normalCfg, []byte(`{"preferences":{"coworkWebSearchEnabled":true},"mcpServers":{"x":{"command":"x"}}}`), 0o600)
	_ = os.WriteFile(m.desktopMetaPath(), []byte(`{"appliedId":"other-id","entries":[{"id":"other-id","name":"别人的"}]}`), 0o600)

	if m.desktopTakenOver() || !m.desktopInstalled() {
		t.Fatal("还没接管，但目录在就算装了")
	}
	for i := 0; i < 2; i++ { // 重复开是幂等的，原值不会被覆盖成我们写的
		if err := m.setDesktopTakeover(true); err != nil {
			t.Fatal(err)
		}
	}
	if !m.desktopTakenOver() {
		t.Fatal("应判定为已接管")
	}
	profile, err := readJSONObject(m.desktopProfilePath())
	if err != nil {
		t.Fatal(err)
	}
	var key, base string
	var models []struct {
		Name          string `json:"name"`
		LabelOverride string `json:"labelOverride"`
	}
	_ = json.Unmarshal(profile["inferenceGatewayApiKey"], &key)
	_ = json.Unmarshal(profile["inferenceGatewayBaseUrl"], &base)
	_ = json.Unmarshal(profile["inferenceModels"], &models)
	if key != "imls-key.claude_desktop" || base != "http://127.0.0.1:18081" {
		t.Fatalf("网关不对：%s %s", key, base)
	}
	// Desktop 对菜单型号名的校验：一个不合规整张表作废
	valid := regexp.MustCompile(`^claude-(sonnet|opus|haiku|fable)-\d{1,2}(-\d{1,2})?$`)
	if len(models) != 4 || models[0].LabelOverride != "自动分档" {
		t.Fatalf("菜单不对：%+v", models)
	}
	for _, d := range models {
		if !valid.MatchString(d.Name) || d.LabelOverride == "" {
			t.Fatalf("Desktop 会拒收：%+v", d)
		}
	}
	meta, _ := os.ReadFile(m.desktopMetaPath())
	if strings.Count(string(meta), desktopProfileID) != 2 || !strings.Contains(string(meta), "other-id") {
		t.Fatalf("配置库登记不对：%s", meta)
	}
	for _, p := range []string{normalCfg, filepath.Join(third, desktopConfigFile)} {
		if b, _ := os.ReadFile(p); !strings.Contains(string(b), `"deploymentMode": "3p"`) {
			t.Fatalf("没切到第三方模式：%s %s", p, b)
		}
	}
	if b, _ := os.ReadFile(normalCfg); !strings.Contains(string(b), "coworkWebSearchEnabled") || !strings.Contains(string(b), "mcpServers") {
		t.Fatalf("其它设置不能动：%s", b)
	}
	if baks, _ := filepath.Glob(normalCfg + ".bak-imls-*"); len(baks) != 1 {
		t.Fatalf("改之前要备份一次：%v", baks)
	}

	if err := m.setDesktopTakeover(false); err != nil {
		t.Fatal(err)
	}
	if m.desktopTakenOver() {
		t.Fatal("关掉后不算接管")
	}
	if _, err := os.Stat(m.desktopProfilePath()); err == nil {
		t.Fatal("关掉后我们的配置要删")
	}
	b, _ := os.ReadFile(normalCfg)
	if strings.Contains(string(b), "deploymentMode") || !strings.Contains(string(b), "coworkWebSearchEnabled") {
		t.Fatalf("原本没有的键要删掉，其它照留：%s", b)
	}
	meta, _ = os.ReadFile(m.desktopMetaPath())
	if strings.Contains(string(meta), desktopProfileID) || !strings.Contains(string(meta), `"appliedId": "other-id"`) || !strings.Contains(string(meta), "别人的") {
		t.Fatalf("配置库要放回：%s", meta)
	}
	if _, err := os.Stat(takeoverSavePath("claude_desktop")); err == nil {
		t.Fatal("关掉后备份应清掉")
	}
}

// 配置文件坏了不能硬改
func TestDesktopTakeoverBadConfig(t *testing.T) {
	m, _ := setup(t)
	normal, _ := m.desktopDirs()
	_ = os.MkdirAll(normal, 0o755)
	cfg := filepath.Join(normal, desktopConfigFile)
	_ = os.WriteFile(cfg, []byte("不是 JSON"), 0o600)
	if err := m.setDesktopTakeover(true); err == nil {
		t.Fatal("应报错")
	}
	if b, _ := os.ReadFile(cfg); string(b) != "不是 JSON" {
		t.Fatal("原文件不能动")
	}
	if _, err := os.Stat(m.desktopProfilePath()); err == nil {
		t.Fatal("失败时不能留下半截配置")
	}
}
