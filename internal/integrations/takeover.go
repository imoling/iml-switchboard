package integrations

// 接管：把宿主自己的模型接口指向 Switchboard，宿主的对话就走按量付费渠道（含 WinWork 网关），流式、工具都是原生的。
// Claude Code 改的是 ~/.claude/settings.json 的 env；Codex 改的是 ~/.codex/config.toml 的供应商。
// 动之前把原值存到数据目录，关掉接管时原样放回。宿主的登录凭据文件从头到尾不碰。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/imoling/iml-switchboard/internal/config"
)

// claudeTakeoverKeys 是接管时会改动的 env 键；原值全部保存，关掉时逐个恢复（原本没有的删掉）。
var claudeTakeoverKeys = []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_MODEL",
	"ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "NO_PROXY"}

func takeoverSavePath(host string) string {
	return filepath.Join(config.DataDir(), "takeover-"+host+".json")
}

func (m *Manager) claudeSettingsPath() string { return filepath.Join(m.claudeDir(), "settings.json") }

// claudeTakenOver 判断 Claude Code 现在是不是指着我们。
func (m *Manager) claudeTakenOver() bool {
	_, env, err := readSettingsEnv(m.claudeSettingsPath())
	if err != nil {
		return false
	}
	var base string
	_ = json.Unmarshal(env["ANTHROPIC_BASE_URL"], &base)
	return strings.HasPrefix(base, m.facade)
}

func readSettingsEnv(path string) (top map[string]json.RawMessage, env map[string]json.RawMessage, err error) {
	top = map[string]json.RawMessage{}
	env = map[string]json.RawMessage{}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return top, env, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(b, &top); err != nil {
		return nil, nil, errors.New("Claude Code 的 settings.json 不是预期的格式")
	}
	if raw, ok := top["env"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, nil, errors.New("Claude Code 的 settings.json 里 env 不是预期的格式")
		}
	}
	return top, env, nil
}

func writeSettingsEnv(path string, top, env map[string]json.RawMessage) error {
	raw, _ := json.Marshal(env)
	top["env"] = raw
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp-switchboard"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// setClaudeTakeover 开：env 指向本机门面，opus/sonnet/haiku 映射成高/中/低档；关：恢复原值。
func (m *Manager) setClaudeTakeover(on bool) error {
	path := m.claudeSettingsPath()
	top, env, err := readSettingsEnv(path)
	if err != nil {
		return err
	}
	save := takeoverSavePath("claude_code")
	if on {
		if _, err := os.Stat(save); err != nil {
			// 第一次开才存原值；反复开关不会把我们自己写的值当成原值
			orig := map[string]json.RawMessage{}
			for _, k := range claudeTakeoverKeys {
				if v, ok := env[k]; ok {
					orig[k] = v
				}
			}
			b, _ := json.MarshalIndent(orig, "", "  ")
			if err := os.WriteFile(save, b, 0o600); err != nil {
				return err
			}
		}
		set := map[string]string{
			"ANTHROPIC_BASE_URL":             m.facade,
			"ANTHROPIC_AUTH_TOKEN":           m.facadeKey() + ".claude_code",
			"ANTHROPIC_MODEL":                "router-auto",
			"ANTHROPIC_DEFAULT_OPUS_MODEL":   "router-high",
			"ANTHROPIC_DEFAULT_SONNET_MODEL": "router-mid",
			"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "router-low",
		}
		delete(env, "ANTHROPIC_API_KEY")
		for k, v := range set {
			env[k], _ = json.Marshal(v)
		}
		// 设置里配了代理的话，发往本机的请求也会被送去代理而卡住：本机地址必须绕过
		var noProxy string
		_ = json.Unmarshal(env["NO_PROXY"], &noProxy)
		for _, h := range []string{"127.0.0.1", "localhost"} {
			if !strings.Contains(","+noProxy+",", ","+h+",") {
				if noProxy != "" {
					noProxy += ","
				}
				noProxy += h
			}
		}
		env["NO_PROXY"], _ = json.Marshal(noProxy)
		return writeSettingsEnv(path, top, env)
	}
	orig := map[string]json.RawMessage{}
	if b, err := os.ReadFile(save); err == nil {
		_ = json.Unmarshal(b, &orig)
	}
	for _, k := range claudeTakeoverKeys {
		if v, ok := orig[k]; ok {
			env[k] = v
		} else {
			delete(env, k)
		}
	}
	if err := writeSettingsEnv(path, top, env); err != nil {
		return err
	}
	_ = os.Remove(save)
	return nil
}

// ---------- Codex ----------

const codexProviderID = "switchboard"

// codexTakenOver 判断 Codex 现在的供应商是不是我们。
func (m *Manager) codexTakenOver() bool {
	b, err := os.ReadFile(m.codexConfig())
	return err == nil && topLevelValue(string(b), "model_provider") == codexProviderID
}

// topLevelValue 读 TOML 顶层（第一个 [section] 之前）的字符串值；没有返回空串。
func topLevelValue(toml, key string) string {
	for _, line := range strings.Split(toml, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			break
		}
		if k, v, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), "\"")
		}
	}
	return ""
}

// setTopLevel 改或加 TOML 顶层的一个字符串值；value 为空表示删掉这行。
func setTopLevel(toml, key, value string) string {
	lines := strings.Split(toml, "\n")
	out := make([]string, 0, len(lines)+1)
	done := false
	inTop := true
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if inTop && strings.HasPrefix(t, "[") {
			inTop = false
			if !done && value != "" {
				out = append(out, fmt.Sprintf("%s = %q", key, value))
				done = true
			}
		}
		if inTop {
			if k, _, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == key {
				if value != "" && !done {
					out = append(out, fmt.Sprintf("%s = %q", key, value))
				}
				done = true
				continue
			}
		}
		out = append(out, line)
	}
	if !done && value != "" {
		out = append([]string{fmt.Sprintf("%s = %q", key, value)}, out...)
	}
	return strings.Join(out, "\n")
}

// stripSection 去掉某个 [section] 整块（到下一个 section 为止）。
func stripSection(toml, section string) string {
	lines := strings.Split(toml, "\n")
	var out []string
	skip := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			skip = t == "["+section+"]"
		}
		if !skip {
			out = append(out, line)
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
}

// setCodexTakeover 开：加一个 switchboard 供应商（Responses 协议，密钥放在请求头里，登录文件不碰）并切过去，
// 再生成模型目录让 /model 列出档位；关：恢复原来的供应商、模型和目录指针。
func (m *Manager) setCodexTakeover(on bool) error {
	m.catalogMu.Lock()
	defer m.catalogMu.Unlock()
	path := m.codexConfig()
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	toml := string(b)
	save := takeoverSavePath("codex")
	if on {
		if _, err := os.Stat(save); err != nil {
			orig := map[string]string{"model_provider": topLevelValue(toml, "model_provider"), "model": topLevelValue(toml, "model"), codexCatalogKey: topLevelValue(toml, codexCatalogKey)}
			ob, _ := json.MarshalIndent(orig, "", "  ")
			if err := os.WriteFile(save, ob, 0o600); err != nil {
				return err
			}
		}
		toml = stripSection(toml, "model_providers."+codexProviderID)
		toml = setTopLevel(toml, "model_provider", codexProviderID)
		toml = setTopLevel(toml, "model", "router-auto")
		toml = setTopLevel(toml, codexCatalogKey, "")
		if m.writeCodexCatalog() == nil { // 生成不了也不拦接管，只是 /model 里看不到档位
			toml = setTopLevel(toml, codexCatalogKey, codexCatalogPath())
		}
		toml = strings.TrimRight(toml, "\n") + fmt.Sprintf("\n\n[model_providers.%s]\nname = \"Switchboard\"\nbase_url = %q\nwire_api = \"responses\"\nhttp_headers = { Authorization = %q }\n",
			codexProviderID, m.facade+"/v1", "Bearer "+m.facadeKey()+".codex")
	} else {
		orig := map[string]string{}
		if ob, err := os.ReadFile(save); err == nil {
			_ = json.Unmarshal(ob, &orig)
		}
		toml = stripSection(toml, "model_providers."+codexProviderID)
		toml = setTopLevel(toml, "model_provider", orig["model_provider"])
		toml = setTopLevel(toml, "model", orig["model"])
		toml = setTopLevel(toml, codexCatalogKey, orig[codexCatalogKey])
		removeCodexCatalog()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(toml), 0o600); err != nil {
		return err
	}
	if !on {
		_ = os.Remove(save)
	}
	return nil
}

// ---------- Hermes ----------

// hermesTakenOver 看 Hermes 的主模型是不是指向 Switchboard。
func (m *Manager) hermesTakenOver() bool {
	b, err := os.ReadFile(m.hermesCfg())
	if err != nil {
		return false
	}
	var doc yaml.Node
	if yaml.Unmarshal(b, &doc) != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return false
	}
	md := findMap(doc.Content[0], "model")
	return md != nil && findScalar(md, "provider") == "custom" && findScalar(md, "base_url") == m.facade+"/v1"
}

// setHermesTakeover 打开时把 ~/.hermes/config.yaml 的 model 段改成 Switchboard 的兼容接口（默认型号 router-auto），
// 原来的 model 段存到数据目录的 takeover-hermes.yaml；关掉时原样放回。Hermes 重启后生效。
func (m *Manager) setHermesTakeover(on bool) error {
	path := m.hermesCfg()
	b, err := os.ReadFile(path)
	if err != nil {
		return errors.New("本机没有找到 Hermes 的配置")
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return errors.New("Hermes 配置无法解析：" + err.Error())
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return errors.New("Hermes 配置顶层不是键值表")
	}
	save := strings.TrimSuffix(takeoverSavePath("hermes"), ".json") + ".yaml"
	if on {
		if _, err := os.Stat(save); err != nil {
			orig := []byte("")
			if md := findMap(root, "model"); md != nil {
				wrap := yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{{Kind: yaml.ScalarNode, Value: "model"}, md}}
				orig, _ = yaml.Marshal(&wrap)
			}
			if err := os.WriteFile(save, orig, 0o600); err != nil {
				return err
			}
		}
		md := ensureMap(root, "model")
		setScalar(md, "default", "router-auto")
		setScalar(md, "provider", "custom")
		setScalar(md, "base_url", m.facade+"/v1")
		setScalar(md, "api_key", m.facadeKey())
		deleteKey(md, "key_env")
	} else {
		deleteKey(root, "model")
		if ob, err := os.ReadFile(save); err == nil {
			var saved yaml.Node
			if yaml.Unmarshal(ob, &saved) == nil && len(saved.Content) > 0 && saved.Content[0].Kind == yaml.MappingNode {
				if md := findMap(saved.Content[0], "model"); md != nil {
					root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "model"}, md)
				}
			}
			_ = os.Remove(save)
		}
	}
	_ = os.WriteFile(path+".bak-imls-"+time.Now().Format("20060102-150405"), b, 0o600)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}

// OriginalModel 是应用不经过 Switchboard 时自己用的型号，算「应用原价」用：接管前存下来的那份，
// 没接管就读它现在的配置；路由型号（router-*）不算。认不出返回空，调用方按 Opus 算。
func (m *Manager) OriginalModel(host string) string {
	pick := func(v string) string {
		v = strings.Trim(strings.TrimSpace(v), `"`)
		if v == "" || strings.HasPrefix(strings.ToLower(v), "router-") {
			return ""
		}
		return v
	}
	switch host {
	case "workbuddy":
		return "workbuddy:" + WorkBuddyDefaultModel
	case "codex":
		if b, err := os.ReadFile(takeoverSavePath("codex")); err == nil {
			var orig map[string]string
			if json.Unmarshal(b, &orig) == nil {
				return pick(orig["model"])
			}
		}
		if b, err := os.ReadFile(m.codexConfig()); err == nil {
			return pick(topLevelValue(string(b), "model"))
		}
	case "hermes":
		src := strings.TrimSuffix(takeoverSavePath("hermes"), ".json") + ".yaml"
		if _, err := os.Stat(src); err != nil {
			src = m.hermesCfg()
		}
		if b, err := os.ReadFile(src); err == nil {
			var doc struct {
				Model struct {
					Default string `yaml:"default"`
				} `yaml:"model"`
			}
			if yaml.Unmarshal(b, &doc) == nil {
				return pick(doc.Model.Default)
			}
		}
	}
	return ""
}
