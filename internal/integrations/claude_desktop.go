package integrations

// Claude Desktop 和 Claude Code 是两个入口：Desktop 不看 ~/.claude/settings.json，第三方模式读 Claude-3p 目录下
// 自己的配置库，启动时读一次。接管就是往配置库加一份指向本机门面的配置并选中它，再把两边的 deploymentMode 切到 3p。
// 原值存到数据目录，关掉时放回。Desktop 的登录信息从头到尾不碰。

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

const (
	desktopProfileID   = "00000000-0000-4000-8000-000000180810"
	desktopProfileName = "Switchboard"
	desktopConfigFile  = "claude_desktop_config.json"
)

// desktopDirs 返回官方模式和第三方模式的配置目录。
func (m *Manager) desktopDirs() (normal, thirdParty string) {
	base := filepath.Join(m.home, ".config")
	switch runtime.GOOS {
	case "darwin":
		base = filepath.Join(m.home, "Library", "Application Support")
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(m.home, "AppData", "Local")
		}
	default:
		if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
			base = x
		}
	}
	return filepath.Join(base, "Claude"), filepath.Join(base, "Claude-3p")
}

func (m *Manager) desktopLibrary() string {
	_, third := m.desktopDirs()
	return filepath.Join(third, "configLibrary")
}

func (m *Manager) desktopProfilePath() string {
	return filepath.Join(m.desktopLibrary(), desktopProfileID+".json")
}

func (m *Manager) desktopMetaPath() string { return filepath.Join(m.desktopLibrary(), "_meta.json") }

// desktopInstalled 判断装没装：它第一次启动就会建自己的数据目录，macOS 上另认应用包。
func (m *Manager) desktopInstalled() bool {
	normal, _ := m.desktopDirs()
	if st, err := os.Stat(normal); err == nil && st.IsDir() {
		return true
	}
	if runtime.GOOS != "darwin" {
		return false
	}
	for _, app := range []string{"/Applications/Claude.app", filepath.Join(m.home, "Applications", "Claude.app")} {
		if _, err := os.Stat(app); err == nil {
			return true
		}
	}
	return false
}

// readJSONObject 读一个 JSON 对象，只解开第一层，别的内容原样留着；文件不存在时返回空对象。
func readJSONObject(path string) (map[string]json.RawMessage, error) {
	obj := map[string]json.RawMessage{}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return obj, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &obj); err != nil || obj == nil {
		return nil, errors.New("Claude Desktop 的配置不是预期的格式：" + filepath.Base(path))
	}
	return obj, nil
}

func writeJSONObject(path string, obj map[string]json.RawMessage) error {
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp-switchboard"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// desktopTakenOver 判断 Desktop 下次启动会不会走我们：配置库选中了我们，而且官方目录里切到了 3p。
func (m *Manager) desktopTakenOver() bool {
	meta, err := readJSONObject(m.desktopMetaPath())
	if err != nil {
		return false
	}
	var applied string
	_ = json.Unmarshal(meta["appliedId"], &applied)
	normal, _ := m.desktopDirs()
	cfg, err := readJSONObject(filepath.Join(normal, desktopConfigFile))
	if err != nil {
		return false
	}
	var mode string
	_ = json.Unmarshal(cfg["deploymentMode"], &mode)
	_, statErr := os.Stat(m.desktopProfilePath())
	return applied == desktopProfileID && mode == "3p" && statErr == nil
}

// desktopSaved 是接管前的原值；键不存在时对应字段留空，恢复时删掉。
type desktopSaved struct {
	NormalMode json.RawMessage `json:"normal_mode,omitempty"`
	ThirdMode  json.RawMessage `json:"third_mode,omitempty"`
	AppliedID  json.RawMessage `json:"applied_id,omitempty"`
}

// desktopProfile 是写进配置库的内容。菜单只放借的四个名字，labelOverride 是用户看到的档位名。
func (m *Manager) desktopProfile() map[string]json.RawMessage {
	var models []map[string]string
	for _, d := range core.DesktopModels {
		models = append(models, map[string]string{"name": d.ID, "labelOverride": d.Label})
	}
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	return map[string]json.RawMessage{
		"disableDeploymentModeChooser": raw(true),
		"inferenceProvider":            raw("gateway"),
		"inferenceGatewayBaseUrl":      raw(m.facade),
		"inferenceGatewayAuthScheme":   raw("bearer"),
		"inferenceGatewayApiKey":       raw(m.facadeKey() + ".claude_desktop"),
		"inferenceModels":              raw(models),
	}
}

func (m *Manager) setDesktopTakeover(on bool) error {
	normal, third := m.desktopDirs()
	normalCfg, thirdCfg := filepath.Join(normal, desktopConfigFile), filepath.Join(third, desktopConfigFile)
	nc, err := readJSONObject(normalCfg)
	if err != nil {
		return err
	}
	tc, err := readJSONObject(thirdCfg)
	if err != nil {
		return err
	}
	meta, err := readJSONObject(m.desktopMetaPath())
	if err != nil {
		return err
	}
	var entries []map[string]any
	if raw, ok := meta["entries"]; ok {
		if err := json.Unmarshal(raw, &entries); err != nil {
			return errors.New("Claude Desktop 的配置不是预期的格式：_meta.json")
		}
	}
	mine := func(e map[string]any) bool { return e["id"] == desktopProfileID }
	kept := entries[:0:0]
	for _, e := range entries {
		if !mine(e) {
			kept = append(kept, e)
		}
	}
	save := takeoverSavePath("claude_desktop")
	mode, _ := json.Marshal("3p")
	if on {
		if _, err := os.Stat(save); err != nil {
			// 第一次开才存原值；反复开关不会把我们自己写的值当成原值
			b, _ := json.MarshalIndent(desktopSaved{NormalMode: nc["deploymentMode"], ThirdMode: tc["deploymentMode"], AppliedID: meta["appliedId"]}, "", "  ")
			if err := os.WriteFile(save, b, 0o600); err != nil {
				return err
			}
			if b, err := os.ReadFile(normalCfg); err == nil {
				_ = os.WriteFile(normalCfg+".bak-imls-"+time.Now().Format("20060102-150405"), b, 0o600)
			}
		}
		if err := writeJSONObject(m.desktopProfilePath(), m.desktopProfile()); err != nil {
			return err
		}
		meta["entries"], _ = json.Marshal(append(kept, map[string]any{"id": desktopProfileID, "name": desktopProfileName}))
		meta["appliedId"], _ = json.Marshal(desktopProfileID)
		nc["deploymentMode"], tc["deploymentMode"] = mode, mode
	} else {
		var orig desktopSaved
		if b, err := os.ReadFile(save); err == nil {
			_ = json.Unmarshal(b, &orig)
		}
		restore := func(obj map[string]json.RawMessage, key string, v json.RawMessage) {
			if len(v) > 0 {
				obj[key] = v
			} else {
				delete(obj, key)
			}
		}
		restore(nc, "deploymentMode", orig.NormalMode)
		restore(tc, "deploymentMode", orig.ThirdMode)
		restore(meta, "appliedId", orig.AppliedID)
		meta["entries"], _ = json.Marshal(kept)
		if _, err := os.Stat(m.desktopProfilePath()); err == nil {
			if err := os.Remove(m.desktopProfilePath()); err != nil {
				return err
			}
		}
	}
	if err := writeJSONObject(m.desktopMetaPath(), meta); err != nil {
		return err
	}
	if err := writeJSONObject(thirdCfg, tc); err != nil {
		return err
	}
	// 官方目录最后写：中途失败时 Desktop 仍停在原来的模式
	if err := writeJSONObject(normalCfg, nc); err != nil {
		return err
	}
	if !on {
		_ = os.Remove(save)
	}
	return nil
}
