package integrations

// 接管：把宿主自己的模型接口指向 Switchboard，宿主的对话就走按量付费渠道（含 WinWork 网关），流式、工具都是原生的。
// Claude Code 改的是 ~/.claude/settings.json 的 env；Codex 改的是 ~/.codex/config.toml 的供应商。
// 动之前把原值存到数据目录，关掉接管时原样放回。宿主的登录凭据文件从头到尾不碰。

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
