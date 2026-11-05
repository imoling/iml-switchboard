// Package integrations 负责把 Switchboard 接进各个宿主（Claude Code、Codex、Gemini CLI、Hermes、WorkBuddy、WinWork 等）。
// 原则：能用宿主官方命令就用官方命令；改文件前先备份；只增删带标记的内容。
package integrations

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/imoling/iml-switchboard/internal/config"
	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/lanes/sub"
)

//go:embed assets
var assets embed.FS

const (
	mcpName   = "iml-switchboard"
	markStart = "<!-- iml-switchboard:start -->"
	markEnd   = "<!-- iml-switchboard:end -->"
)

// Manager 实现 admin.IntegrationOps。
type Manager struct {
	cfg       *config.Store
	exe       string // imls 可执行文件绝对路径
	facade    string // http://127.0.0.1:18081
	home      string
	facadeKey func() string
	// ModelName 和 ModelContext 查型号的展示名和上下文窗口，写 Codex 模型目录时用；没设就用型号名和模板的窗口
	ModelName    func(id string) string
	ModelContext func(id string) int
	// Lookup 查型号能不能用工具、看图，写宿主的型号表时用；没设就当都能
	Lookup       func(id string) (core.ModelInfo, bool)
	codexBundled func() ([]byte, error)
	codexCheck   func(path string) error
	codexStamp   func() string

	catalogMu     sync.Mutex // Codex 模型目录：接入页和启动时的同步可能同时走到
	catalogFailed string     // 上一次生成失败时的 Codex 版本和型号
}

// New 创建管理器。
func New(cfg *config.Store, exe, facadeURL string, facadeKey func() string) *Manager {
	home, _ := os.UserHomeDir()
	return &Manager{cfg: cfg, exe: exe, facade: facadeURL, home: home, facadeKey: facadeKey, codexBundled: codexBundledCatalog, codexCheck: codexCheckCatalog, codexStamp: codexVersionStamp}
}

func (m *Manager) claudeDir() string       { return filepath.Join(m.home, ".claude") }
func (m *Manager) hermesCfg() string       { return filepath.Join(m.home, ".hermes", "config.yaml") }
func (m *Manager) workbuddyModels() string { return filepath.Join(m.home, ".workbuddy", "models.json") }
func (m *Manager) piModels() string        { return filepath.Join(m.home, ".pi", "agent", "models.json") }
func (m *Manager) doubaoWorkApp() string   { return "/Applications/DoubaoWork.app" }
func (m *Manager) dshHome() string {
	if h := os.Getenv("DSH_HOME"); h != "" {
		return h
	}
	return filepath.Join(m.home, ".dsh")
}
func (m *Manager) dshPatch() string { return filepath.Join(m.dshHome(), "cordis.patch.yml") }

// dshInstalled 判断本机有没有 DeepSeek Harness：PATH 上有 dsh 命令，或数据目录里有它自己建的东西
// （桌面版用的是垫片，装完要重开终端 PATH 才有；profiles、sessions 这些只有它自己会建，我们接入时只写 cordis.patch.yml 和 .env）。
func (m *Manager) dshInstalled() bool {
	if _, err := exec.LookPath("dsh"); err == nil {
		return true
	}
	for _, name := range []string{"profiles", "sessions", "storages", ".credentials.yaml", "settings.yaml", "settings.yaml.imported"} {
		if _, err := os.Stat(filepath.Join(m.dshHome(), name)); err == nil {
			return true
		}
	}
	return false
}
func (m *Manager) openclawCfg() string { return filepath.Join(m.home, ".openclaw", "openclaw.json") }
func (m *Manager) dshEnv() string      { return filepath.Join(m.dshHome(), ".env") }

// List 返回三个宿主的状态。
func (m *Manager) List() []map[string]any {
	var out []map[string]any
	for _, id := range config.HostIDs {
		out = append(out, m.view(id))
	}
	return out
}

func (m *Manager) view(id string) map[string]any {
	st := m.cfg.Get().Integrations[id]
	v := map[string]any{"id": id, "status": "disconnected", "options": st.Options, "available": true}
	if st.Options == nil {
		v["options"] = map[string]bool{} // 空 map 放进 any 后不等于 nil，不单独判会把 null 发给界面
	}
	switch id {
	case "claude_code":
		if _, err := exec.LookPath("claude"); err != nil {
			v["available"] = false
			v["detail"] = "本机没有找到 claude 命令"
		}
		if m.claudeRegistered() {
			v["status"] = "connected"
		}
		opts := map[string]bool{"write_claude_md": m.claudeMdHasBlock(), "opusplan": m.claudeModelIs("opusplan"), "commands": m.commandsInstalled(), "takeover": m.claudeTakenOver()}
		v["options"] = opts
	case "claude_desktop":
		if !m.desktopInstalled() {
			v["available"] = false
			v["detail"] = "本机没有安装 Claude Desktop"
		}
		on := m.desktopTakenOver()
		if on {
			v["status"] = "connected"
		}
		v["options"] = map[string]bool{"takeover": on}
	case "codex":
		if sub.CodexBin() == "" {
			v["available"] = false
			v["detail"] = sub.CodexMissing
		}
		if m.codexRegistered() {
			v["status"] = "connected"
		}
		m.SyncCodexCatalog()
		v["options"] = map[string]bool{"takeover": m.codexTakenOver()}
	case "gemini_cli":
		if _, err := exec.LookPath("gemini"); err != nil {
			v["available"] = false
			v["detail"] = "本机没有找到 gemini 命令"
		}
		if m.geminiRegistered() {
			v["status"] = "connected"
		}
	case "hermes":
		if _, err := os.Stat(m.hermesCfg()); err != nil {
			v["available"] = false
			v["detail"] = "本机没有找到 Hermes 的配置"
		}
		v["options"] = map[string]bool{"takeover": m.hermesTakenOver()}
		if _, err := os.Stat(m.hermesCfg()); err == nil && m.hermesHasKeys() {
			v["status"] = "connected"
		}
		v["snippet"] = m.hermesSnippet()
	case "workbuddy":
		if _, err := os.Stat(m.workbuddyModels()); err != nil {
			v["available"] = false
			v["status"] = "manual"
			v["detail"] = "本机没有找到 WorkBuddy 的模型配置，按下面的片段手动添加"
		} else if m.workbuddyHas() {
			v["status"] = "connected"
		}
		v["snippet"] = m.workbuddySnippet()
	case "winwork":
		if !m.winworkInstalled() {
			v["available"] = false
			v["detail"] = "本机没有安装 WinWork"
		} else if m.winworkRegistered() {
			v["status"] = "connected"
		}
		v["options"] = map[string]bool{"write_claude_md": instructionBlockIn(m.winworkMdPath()), "commands": commandsInstalledIn(m.winworkCfgDir())}
	case "doubao_work":
		v["status"] = "manual"
		// 只在 macOS 上能可靠判断装没装；其他系统照常给出手动配置的片段
		if _, err := os.Stat(m.doubaoWorkApp()); err != nil && runtime.GOOS == "darwin" {
			v["available"] = false
			v["detail"] = "本机没有安装豆包工作"
		}
		v["snippet"] = m.doubaoWorkSnippet()
	// 下面三个只认装没装命令：配置目录是接入时自己建的，不能当装了的证据
	case "pi":
		if _, err := exec.LookPath("pi"); err != nil {
			v["available"] = false
			v["detail"] = "本机没有找到 pi 命令"
		} else if m.piHas() {
			v["status"] = "connected"
		}
		v["snippet"] = m.piSnippet()
	case "deepseek_harness":
		if !m.dshInstalled() {
			v["available"] = false
			v["detail"] = "本机没有找到 dsh 命令，也没有它的数据目录"
		} else if m.dshHas() {
			v["status"] = "connected"
		}
		v["snippet"] = m.dshSnippet()
	case "openclaw":
		if _, err := exec.LookPath("openclaw"); err != nil {
			v["available"] = false
			v["detail"] = "本机没有找到 openclaw 命令"
		} else if m.openclawHas() {
			v["status"] = "connected"
		}
		v["snippet"] = m.openclawSnippet()
	}
	if modelListHosts[id] {
		v["models_count"] = len(m.selectedHostModels(id))
	}
	if n := len(st.Tiers); n > 0 {
		v["tiers_custom"] = n
	}
	return v
}

// Connect 接入。
func (m *Manager) Connect(id string, opts map[string]bool) (map[string]any, error) {
	var err error
	switch id {
	case "claude_code":
		err = m.connectClaude(opts)
	case "codex":
		err = m.connectCodex()
	case "gemini_cli":
		err = m.connectGemini()
	case "hermes":
		err = m.connectHermes()
	case "workbuddy":
		err = m.connectWorkBuddy()
	case "winwork":
		err = m.connectWinWork(opts)
	case "pi":
		err = m.connectPi()
	case "deepseek_harness":
		err = m.connectDsh()
	case "openclaw":
		err = m.connectOpenClaw()
	case "doubao_work":
		err = nil // 豆包工作只能在它自己的界面里填，这里只记一笔
	default:
		return nil, errors.New("不认识的应用")
	}
	if err != nil {
		return nil, err
	}
	_ = m.cfg.Update(func(c *config.Config) error {
		it := c.Integrations[id]
		it.Connected = true
		if it.Options == nil {
			it.Options = map[string]bool{}
		}
		for k, v := range opts {
			it.Options[k] = v
		}
		c.Integrations[id] = it
		return nil
	})
	return m.view(id), nil
}

// Disconnect 断开。
func (m *Manager) Disconnect(id string) (map[string]any, error) {
	var err error
	switch id {
	case "claude_code":
		err = m.disconnectClaude()
	case "codex":
		err = m.disconnectCodex()
	case "gemini_cli":
		err = m.disconnectGemini()
	case "hermes":
		err = m.disconnectHermes()
	case "workbuddy":
		err = m.disconnectWorkBuddy()
	case "winwork":
		err = m.disconnectWinWork()
	case "pi":
		err = m.disconnectPi()
	case "deepseek_harness":
		err = m.disconnectDsh()
	case "openclaw":
		err = m.disconnectOpenClaw()
	case "doubao_work":
		err = nil
	default:
		return nil, errors.New("不认识的应用")
	}
	if err != nil {
		return nil, err
	}
	_ = m.cfg.Update(func(c *config.Config) error {
		it := c.Integrations[id]
		it.Connected = false
		c.Integrations[id] = it
		return nil
	})
	return m.view(id), nil
}

// Update 只改选项（Claude Code 和 WinWork 有）。
func (m *Manager) Update(id string, opts map[string]bool) (map[string]any, error) {
	if id == "winwork" {
		if v, ok := opts["write_claude_md"]; ok {
			if err := setInstructionBlock(m.winworkMdPath(), v); err != nil {
				return nil, err
			}
		}
		if v, ok := opts["commands"]; ok {
			if err := setCommandsIn(m.winworkCfgDir(), v); err != nil {
				return nil, err
			}
		}
		return m.view(id), nil
	}
	if id == "codex" {
		if v, ok := opts["takeover"]; ok {
			if err := m.setCodexTakeover(v); err != nil {
				return nil, err
			}
		}
		return m.view(id), nil
	}
	if id == "claude_desktop" {
		if v, ok := opts["takeover"]; ok {
			if err := m.setDesktopTakeover(v); err != nil {
				return nil, err
			}
		}
		return m.view(id), nil
	}
	if id == "hermes" {
		if v, ok := opts["takeover"]; ok {
			if err := m.setHermesTakeover(v); err != nil {
				return nil, err
			}
		}
		return m.view(id), nil
	}
	if id != "claude_code" {
		return m.view(id), nil
	}
	if v, ok := opts["write_claude_md"]; ok {
		if err := m.setClaudeMd(v); err != nil {
			return nil, err
		}
	}
	if v, ok := opts["opusplan"]; ok {
		if err := m.setOpusplan(v); err != nil {
			return nil, err
		}
	}
	if v, ok := opts["commands"]; ok {
		if err := m.setCommands(v); err != nil {
			return nil, err
		}
	}
	if v, ok := opts["takeover"]; ok {
		if err := m.setClaudeTakeover(v); err != nil {
			return nil, err
		}
	}
	return m.view(id), nil
}

// ---------- Claude Code ----------

func (m *Manager) claudeRegistered() bool {
	b, err := os.ReadFile(filepath.Join(m.home, ".claude.json"))
	if err != nil {
		return false
	}
	var d struct {
		MCP map[string]any `json:"mcpServers"`
	}
	if json.Unmarshal(b, &d) != nil {
		return false
	}
	_, ok := d.MCP[mcpName]
	return ok
}

func (m *Manager) connectClaude(opts map[string]bool) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return errors.New("本机没有找到 claude 命令")
	}
	if !m.claudeRegistered() {
		cmd := exec.Command("claude", "mcp", "add", "-s", "user", mcpName, "--", m.exe, "mcp")
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("注册到 Claude Code 失败：%s", strings.TrimSpace(out.String()))
		}
	}
	if opts == nil {
		opts = map[string]bool{"write_claude_md": true, "commands": true}
	}
	if v, ok := opts["write_claude_md"]; ok {
		if err := m.setClaudeMd(v); err != nil {
			return err
		}
	}
	if v, ok := opts["commands"]; ok {
		if err := m.setCommands(v); err != nil {
			return err
		}
	}
	if v, ok := opts["opusplan"]; ok {
		if err := m.setOpusplan(v); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) disconnectClaude() error {
	if m.claudeRegistered() {
		cmd := exec.Command("claude", "mcp", "remove", "-s", "user", mcpName)
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("从 Claude Code 移除失败：%s", strings.TrimSpace(out.String()))
		}
	}
	_ = m.setClaudeMd(false)
	_ = m.setCommands(false)
	return nil
}

func (m *Manager) claudeMdPath() string { return filepath.Join(m.claudeDir(), "CLAUDE.md") }

func (m *Manager) claudeMdHasBlock() bool {
	b, err := os.ReadFile(m.claudeMdPath())
	return err == nil && bytes.Contains(b, []byte(markStart))
}

func (m *Manager) setClaudeMd(on bool) error {
	return setInstructionBlock(m.claudeMdPath(), on)
}

func (m *Manager) commandsInstalled() bool { return commandsInstalledIn(m.claudeDir()) }

func commandsInstalledIn(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "commands", "iml-img.md"))
	return err == nil
}

func instructionBlockIn(path string) bool {
	b, err := os.ReadFile(path)
	return err == nil && bytes.Contains(b, []byte(markStart))
}

func (m *Manager) setCommands(on bool) error { return setCommandsIn(m.claudeDir(), on) }

// setCommandsIn 往某个 Claude 配置目录装或卸斜杠命令。
func setCommandsIn(cfgDir string, on bool) error {
	dir := filepath.Join(cfgDir, "commands")
	entries, _ := assets.ReadDir("assets/commands")
	if !on {
		for _, e := range entries {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		b, _ := assets.ReadFile("assets/commands/" + e.Name())
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) settingsPath() string { return filepath.Join(m.claudeDir(), "settings.json") }

func (m *Manager) claudeModelIs(model string) bool {
	b, err := os.ReadFile(m.settingsPath())
	if err != nil {
		return false
	}
	var d map[string]any
	if json.Unmarshal(b, &d) != nil {
		return false
	}
	v, _ := d["model"].(string)
	return v == model
}

// setOpusplan 把 Claude Code 的默认模型切到 opusplan；关掉时恢复之前记住的值。
func (m *Manager) setOpusplan(on bool) error {
	b, err := os.ReadFile(m.settingsPath())
	if err != nil {
		return errors.New("没有找到 Claude Code 的设置文件")
	}
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		return errors.New("Claude Code 的设置文件无法解析")
	}
	prevPath := filepath.Join(config.DataDir(), "claude-model.prev")
	if on {
		if cur, _ := d["model"].(string); cur != "opusplan" {
			_ = os.WriteFile(prevPath, []byte(cur), 0o600)
		}
		d["model"] = "opusplan"
	} else {
		prev, _ := os.ReadFile(prevPath)
		if p := strings.TrimSpace(string(prev)); p != "" {
			d["model"] = p
		} else {
			delete(d, "model")
		}
	}
	_ = os.WriteFile(m.settingsPath()+".bak-imls", b, 0o600)
	out, _ := json.MarshalIndent(d, "", "  ")
	return os.WriteFile(m.settingsPath(), append(out, '\n'), 0o644)
}

// ---------- Hermes ----------

func (m *Manager) hermesSnippet() string {
	return fmt.Sprintf(`# 加到 ~/.hermes/config.yaml
mcp_servers:
  %s:
    command: %s
    args: ["mcp"]
    env:
      IMLS_HOST: hermes
image_gen:
  provider: openai
  openai:
    base_url: %s/v1
    api_key: %s
`, mcpName, m.exe, m.facade, m.facadeKey())
}

func (m *Manager) hermesHasKeys() bool {
	b, err := os.ReadFile(m.hermesCfg())
	return err == nil && bytes.Contains(b, []byte(mcpName))
}

// connectHermes 用 yaml.Node 改配置，保留原文件的注释。
func (m *Manager) connectHermes() error {
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
	mcp := ensureMap(root, "mcp_servers")
	entry := ensureMap(mcp, mcpName)
	setScalar(entry, "command", m.exe)
	setSeq(entry, "args", []string{"mcp"})
	env := ensureMap(entry, "env")
	setScalar(env, "IMLS_HOST", "hermes")
	ig := ensureMap(root, "image_gen")
	setScalar(ig, "provider", "openai")
	oa := ensureMap(ig, "openai")
	setScalar(oa, "base_url", m.facade+"/v1")
	setScalar(oa, "api_key", m.facadeKey())
	_ = os.WriteFile(path+".bak-imls-"+time.Now().Format("20060102-150405"), b, 0o600)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}

func (m *Manager) disconnectHermes() error {
	path := m.hermesCfg()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc yaml.Node
	if yaml.Unmarshal(b, &doc) != nil || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if mcp := findMap(root, "mcp_servers"); mcp != nil {
		deleteKey(mcp, mcpName)
		if len(mcp.Content) == 0 {
			deleteKey(root, "mcp_servers")
		}
	}
	if ig := findMap(root, "image_gen"); ig != nil {
		if oa := findMap(ig, "openai"); oa != nil {
			if v := findScalar(oa, "base_url"); strings.HasPrefix(v, m.facade) {
				deleteKey(root, "image_gen")
			}
		}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}

func findMap(parent *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key && parent.Content[i+1].Kind == yaml.MappingNode {
			return parent.Content[i+1]
		}
	}
	return nil
}

func findScalar(parent *yaml.Node, key string) string {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			return parent.Content[i+1].Value
		}
	}
	return ""
}

func ensureMap(parent *yaml.Node, key string) *yaml.Node {
	if n := findMap(parent, key); n != nil {
		return n
	}
	deleteKey(parent, key)
	k := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
	v := &yaml.Node{Kind: yaml.MappingNode}
	parent.Content = append(parent.Content, k, v)
	return v
}

func setScalar(parent *yaml.Node, key, val string) {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content[i+1] = &yaml.Node{Kind: yaml.ScalarNode, Value: val}
			return
		}
	}
	parent.Content = append(parent.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, &yaml.Node{Kind: yaml.ScalarNode, Value: val})
}

func setSeq(parent *yaml.Node, key string, vals []string) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, v := range vals {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: v})
	}
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content[i+1] = seq
			return
		}
	}
	parent.Content = append(parent.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, seq)
}

func deleteKey(parent *yaml.Node, key string) {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
			return
		}
	}
}

// ---------- WinWork ----------
// WinWork（讯飞）内嵌了一份 Claude Code，配置目录固定在 ~/.kooky-data/claude-config。
// 接法和 Claude Code 一样：往它的 .claude.json 登记 MCP、写调用约定、装斜杠命令。
// 它的 settings.json 里有讯飞网关的令牌，这里从不读它。

func (m *Manager) winworkCfgDir() string {
	return filepath.Join(m.home, ".kooky-data", "claude-config")
}
func (m *Manager) winworkMdPath() string { return filepath.Join(m.winworkCfgDir(), "CLAUDE.md") }
func (m *Manager) winworkJSON() string   { return filepath.Join(m.winworkCfgDir(), ".claude.json") }

func (m *Manager) winworkInstalled() bool {
	st, err := os.Stat(m.winworkCfgDir())
	return err == nil && st.IsDir()
}

func (m *Manager) winworkRegistered() bool {
	servers, _, err := readMCPServers(m.winworkJSON())
	if err != nil {
		return false
	}
	_, ok := servers[mcpName]
	return ok
}

// readMCPServers 读 .claude.json 里的 mcpServers，其余字段原样保留（按原始字节），写回时不会改动。
func readMCPServers(path string) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return nil, nil, fmt.Errorf("WinWork 的 .claude.json 不是预期的格式")
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := top["mcpServers"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, nil, fmt.Errorf("WinWork 的 .claude.json 里 mcpServers 不是预期的格式")
		}
	}
	return servers, top, nil
}

func writeMCPServers(path string, top map[string]json.RawMessage, servers map[string]json.RawMessage) error {
	raw, _ := json.Marshal(servers)
	top["mcpServers"] = raw
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	// 第一次动它之前留一份备份
	bak := path + ".bak-switchboard"
	if _, err := os.Stat(bak); err != nil {
		if orig, err := os.ReadFile(path); err == nil {
			_ = os.WriteFile(bak, orig, 0o600)
		}
	}
	tmp := path + ".tmp-switchboard"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (m *Manager) connectWinWork(opts map[string]bool) error {
	if !m.winworkInstalled() {
		return errors.New("本机没有安装 WinWork")
	}
	servers, top, err := readMCPServers(m.winworkJSON())
	if errors.Is(err, os.ErrNotExist) {
		servers, top = map[string]json.RawMessage{}, map[string]json.RawMessage{}
	} else if err != nil {
		return err
	}
	if _, ok := servers[mcpName]; !ok {
		entry, _ := json.Marshal(map[string]any{"type": "stdio", "command": m.exe, "args": []string{"mcp"}, "env": map[string]string{"IMLS_HOST": "winwork"}})
		servers[mcpName] = entry
		if err := writeMCPServers(m.winworkJSON(), top, servers); err != nil {
			return fmt.Errorf("写入 WinWork 配置失败：%s", err)
		}
	}
	if opts == nil {
		opts = map[string]bool{"write_claude_md": true, "commands": true}
	}
	if v, ok := opts["write_claude_md"]; ok {
		if err := setInstructionBlock(m.winworkMdPath(), v); err != nil {
			return err
		}
	}
	if v, ok := opts["commands"]; ok {
		if err := setCommandsIn(m.winworkCfgDir(), v); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) disconnectWinWork() error {
	if servers, top, err := readMCPServers(m.winworkJSON()); err == nil {
		if _, ok := servers[mcpName]; ok {
			delete(servers, mcpName)
			if err := writeMCPServers(m.winworkJSON(), top, servers); err != nil {
				return fmt.Errorf("改写 WinWork 配置失败：%s", err)
			}
		}
	}
	_ = setInstructionBlock(m.winworkMdPath(), false)
	_ = setCommandsIn(m.winworkCfgDir(), false)
	return nil
}

// ---------- WorkBuddy ----------

type wbModel struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Vendor            string `json:"vendor"`
	URL               string `json:"url"`
	APIKey            string `json:"apiKey"`
	SupportsToolCall  bool   `json:"supportsToolCall"`
	SupportsImages    bool   `json:"supportsImages"`
	SupportsReasoning bool   `json:"supportsReasoning"`
}

// wbEntries 是写进 WorkBuddy 模型表的条目。它的表里各家混在一起，账号型号的名字前面带上 Switchboard，用户才知道走的是谁；
// 厂商统一写 Switchboard，断开时靠它认出我们写的全部条目。
func (m *Manager) wbEntries() []wbModel {
	var out []wbModel
	for _, h := range m.selectedHostModels("workbuddy") {
		name := h.Name
		if h.Group != "Switchboard" {
			name = "Switchboard · " + h.Name
		}
		out = append(out, wbModel{ID: h.ID, Name: name, Vendor: "Switchboard", URL: m.facade + "/v1/chat/completions", APIKey: m.facadeKey(), SupportsToolCall: h.Tools, SupportsImages: h.Vision})
	}
	return out
}

func (m *Manager) workbuddySnippet() string {
	b, _ := json.MarshalIndent(m.wbEntries(), "", "  ")
	return "// 加到 ~/.workbuddy/models.json 的数组里，或在 WorkBuddy 设置 → 模型 → 自定义里按这两条填\n" + string(b)
}

func (m *Manager) workbuddyHas() bool {
	b, err := os.ReadFile(m.workbuddyModels())
	return err == nil && bytes.Contains(b, []byte(`"router-auto"`))
}

// isOurWBEntry 判断 WorkBuddy 模型表里的一条是不是 Switchboard 写的：看厂商名，老版本只写了 id 的也认
func isOurWBEntry(raw json.RawMessage) bool {
	var probe struct {
		ID     string `json:"id"`
		Vendor string `json:"vendor"`
	}
	_ = json.Unmarshal(raw, &probe)
	return probe.Vendor == "Switchboard" || probe.ID == "router-auto" || probe.ID == "router-sub"
}

func (m *Manager) connectWorkBuddy() error {
	path := m.workbuddyModels()
	b, err := os.ReadFile(path)
	if err != nil {
		return errors.New("本机没有找到 WorkBuddy 的模型配置，按片段手动添加")
	}
	var list []json.RawMessage
	if err := json.Unmarshal(b, &list); err != nil {
		return errors.New("WorkBuddy 的模型配置不是预期的格式，按片段手动添加")
	}
	keep := list[:0]
	for _, raw := range list {
		if isOurWBEntry(raw) {
			continue // 我们写过的条目全部换新，免得重复
		}
		keep = append(keep, raw)
	}
	for _, e := range m.wbEntries() {
		raw, _ := json.Marshal(e)
		keep = append(keep, raw)
	}
	_ = os.WriteFile(path+".bak-imls-"+time.Now().Format("20060102-150405"), b, 0o600)
	out, _ := json.MarshalIndent(keep, "", "  ")
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

func (m *Manager) disconnectWorkBuddy() error {
	path := m.workbuddyModels()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var list []json.RawMessage
	if json.Unmarshal(b, &list) != nil {
		return nil
	}
	keep := list[:0]
	for _, raw := range list {
		if !isOurWBEntry(raw) {
			keep = append(keep, raw)
		}
	}
	out, _ := json.MarshalIndent(keep, "", "  ")
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

// ---------- 豆包工作 ----------

// doubaoWorkSnippet 给「技能 · 连接器 · 伙伴 → 新建 → 新建自定义连接器」用；桌面版支持本地命令和 HTTP 两种。
func (m *Manager) doubaoWorkSnippet() string {
	return fmt.Sprintf(`豆包工作 → 左侧「技能 · 连接器 · 伙伴」→ 右上角「新建」→「新建自定义连接器」

方式一（推荐）：传输类型选 STDIO / 本地命令
  命令：%s
  参数：mcp
  环境变量：IMLS_HOST=doubao_work

方式二：传输类型选 HTTP
  地址：%s/mcp
  请求头：Authorization: Bearer %s
          X-IMLS-Host: doubao_work
`, m.exe, m.facade, m.facadeKey())
}

// ---------- pi ----------

type piModel struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Reasoning     bool     `json:"reasoning"`
	Input         []string `json:"input"`
	ContextWindow int      `json:"contextWindow"`
	MaxTokens     int      `json:"maxTokens"`
}

type piProvider struct {
	BaseURL string    `json:"baseUrl"`
	API     string    `json:"api"`
	APIKey  string    `json:"apiKey"`
	Models  []piModel `json:"models"`
}

// inputKinds 是宿主配置里「能收什么」的写法：能看图就多一个 image。
func inputKinds(h HostModel) []string {
	if h.Vision {
		return []string{"text", "image"}
	}
	return []string{"text"}
}

func (m *Manager) piProvider() piProvider {
	p := piProvider{BaseURL: m.facade + "/v1", API: "openai-completions", APIKey: m.hostKey("pi")}
	for _, h := range m.selectedHostModels("pi") {
		p.Models = append(p.Models, piModel{ID: h.ID, Name: h.Name, Input: inputKinds(h), ContextWindow: contextOr(h.Context, 200000), MaxTokens: 32000})
	}
	return p
}

func (m *Manager) piSnippet() string {
	b, _ := json.MarshalIndent(map[string]any{"providers": map[string]any{"switchboard": m.piProvider()}}, "", "  ")
	return "// 合并进 ~/.pi/agent/models.json；之后 pi --provider switchboard --model router-auto\n" + string(b)
}

func (m *Manager) piHas() bool {
	b, err := os.ReadFile(m.piModels())
	return err == nil && bytes.Contains(b, []byte(`"switchboard"`))
}

// connectPi 把 switchboard 提供方合并进 models.json，其余内容原样保留。
func (m *Manager) connectPi() error {
	path := m.piModels()
	doc := map[string]json.RawMessage{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &doc); err != nil {
			return errors.New("pi 的 models.json 不是预期的格式，按片段手动添加")
		}
		_ = os.WriteFile(path+".bak-imls-"+time.Now().Format("20060102-150405"), b, 0o600)
	}
	providers := map[string]json.RawMessage{}
	if raw, ok := doc["providers"]; ok {
		_ = json.Unmarshal(raw, &providers)
	}
	pb, _ := json.Marshal(m.piProvider())
	providers["switchboard"] = pb
	prb, _ := json.Marshal(providers)
	doc["providers"] = prb
	out, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

func (m *Manager) disconnectPi() error {
	path := m.piModels()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	doc := map[string]json.RawMessage{}
	if json.Unmarshal(b, &doc) != nil {
		return nil
	}
	providers := map[string]json.RawMessage{}
	_ = json.Unmarshal(doc["providers"], &providers)
	delete(providers, "switchboard")
	prb, _ := json.Marshal(providers)
	doc["providers"] = prb
	out, _ := json.MarshalIndent(doc, "", "  ")
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

// ---------- DeepSeek Harness ----------

const dshKeyRef = "SWITCHBOARD_API_KEY"

func (m *Manager) dshRoute() map[string]any {
	models := []map[string]any{}
	for _, h := range m.selectedHostModels("deepseek_harness") {
		models = append(models, map[string]any{"id": h.ID, "name": h.Name, "contextWindow": contextOr(h.Context, 200000), "maxTokens": 32000})
	}
	return map[string]any{
		"displayName": "Switchboard",
		"apiKeyEnv":   dshKeyRef,
		"api":         "openai-completions",
		"baseURL":     m.facade + "/v1",
		"models":      models,
	}
}

func (m *Manager) dshSnippet() string {
	route, _ := yaml.Marshal([]map[string]any{{"id": "llm-pi-ai", "config": map[string]any{"providers": map[string]any{"switchboard": m.dshRoute()}}}})
	return fmt.Sprintf("# 追加到 %s（对 web、headless 等所有配置都生效）\n%s\n# 密钥写在 %s 里一行：\n%s=%s\n# 之后在 dsh web 的模型列表里选 Switchboard\n", m.dshPatch(), route, m.dshEnv(), dshKeyRef, m.hostKey("deepseek_harness"))
}

func (m *Manager) dshHas() bool {
	b, err := os.ReadFile(m.dshPatch())
	return err == nil && bytes.Contains(b, []byte("switchboard:"))
}

// connectDsh：patch 文件是一个顶层数组，找 id 为 llm-pi-ai 的条目合并进 providers.switchboard，没有就追加；密钥引用写进 .env。
func (m *Manager) connectDsh() error {
	path := m.dshPatch()
	var doc yaml.Node
	if b, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(b)) > 0 {
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return errors.New("dsh 的 cordis.patch.yml 无法解析，按片段手动添加")
		}
		_ = os.WriteFile(path+".bak-imls-"+time.Now().Format("20060102-150405"), b, 0o600)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.SequenceNode, Tag: "!!seq"}}}
	}
	seq := doc.Content[0]
	if seq.Kind != yaml.SequenceNode {
		return errors.New("dsh 的 cordis.patch.yml 顶层不是数组，按片段手动添加")
	}
	var entry *yaml.Node
	for _, it := range seq.Content {
		if it.Kind == yaml.MappingNode && findScalar(it, "id") == "llm-pi-ai" {
			entry = it
			break
		}
	}
	if entry == nil {
		entry = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		setScalar(entry, "id", "llm-pi-ai")
		seq.Content = append(seq.Content, entry)
	}
	providers := ensureMap(ensureMap(entry, "config"), "providers")
	deleteKey(providers, "switchboard")
	var routeNode yaml.Node
	if err := routeNode.Encode(m.dshRoute()); err != nil {
		return err
	}
	providers.Content = append(providers.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "switchboard"}, &routeNode)
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.dshHome(), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return err
	}
	return m.setEnvLine(m.dshEnv(), dshKeyRef, m.hostKey("deepseek_harness"))
}

func (m *Manager) disconnectDsh() error {
	path := m.dshPatch()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc yaml.Node
	if yaml.Unmarshal(b, &doc) != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.SequenceNode {
		return nil
	}
	seq := doc.Content[0]
	keep := seq.Content[:0]
	for _, it := range seq.Content {
		if it.Kind == yaml.MappingNode && findScalar(it, "id") == "llm-pi-ai" {
			if cfg := findMap(it, "config"); cfg != nil {
				if prov := findMap(cfg, "providers"); prov != nil {
					deleteKey(prov, "switchboard")
					if len(prov.Content) == 0 {
						deleteKey(cfg, "providers")
					}
				}
				if len(cfg.Content) == 0 {
					deleteKey(it, "config")
				}
			}
			if len(it.Content) <= 2 { // 只剩 id
				continue
			}
		}
		keep = append(keep, it)
	}
	seq.Content = keep
	out, _ := yaml.Marshal(&doc)
	if len(keep) == 0 {
		out = []byte("[]\n")
	}
	_ = os.WriteFile(path, out, 0o600)
	return m.setEnvLine(m.dshEnv(), dshKeyRef, "")
}

// setEnvLine 在 .env 里写或删一行 KEY=value；其他行原样保留。
func (m *Manager) setEnvLine(path, key, val string) error {
	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			if strings.HasPrefix(l, key+"=") {
				continue
			}
			if l != "" || len(lines) > 0 {
				lines = append(lines, l)
			}
		}
	}
	if val != "" {
		lines = append(lines, key+"="+val)
	}
	if len(lines) == 0 {
		_ = os.Remove(path)
		return nil
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

// ---------- OpenClaw ----------

func (m *Manager) openclawProvider() map[string]any {
	models := []map[string]any{}
	for _, h := range m.selectedHostModels("openclaw") {
		models = append(models, map[string]any{"id": h.ID, "name": h.Name, "reasoning": false, "input": inputKinds(h), "contextWindow": contextOr(h.Context, 200000), "maxTokens": 32000})
	}
	return map[string]any{
		"baseUrl": m.facade + "/v1",
		"apiKey":  m.hostKey("openclaw"),
		"api":     "openai-completions",
		"models":  models,
	}
}

func (m *Manager) openclawSnippet() string {
	b, _ := json.MarshalIndent(map[string]any{"models": map[string]any{"providers": map[string]any{"switchboard": m.openclawProvider()}}}, "", "  ")
	return "// 合并进 ~/.openclaw/openclaw.json；之后用 openclaw models set switchboard/router-auto 或在会话里 /model 选\n" + string(b)
}

func (m *Manager) openclawHas() bool {
	b, err := os.ReadFile(m.openclawCfg())
	return err == nil && bytes.Contains(b, []byte(`"switchboard"`))
}

// connectOpenClaw 把 switchboard 合并进 models.providers；文件带注释（JSON5）时解析不了，让用户按片段手动贴。
func (m *Manager) connectOpenClaw() error {
	path := m.openclawCfg()
	doc := map[string]json.RawMessage{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &doc); err != nil {
			return errors.New("openclaw.json 带注释或格式特殊，这里改不了：复制片段手动合并")
		}
		_ = os.WriteFile(path+".bak-imls-"+time.Now().Format("20060102-150405"), b, 0o600)
	}
	models := map[string]json.RawMessage{}
	if raw, ok := doc["models"]; ok {
		_ = json.Unmarshal(raw, &models)
	}
	providers := map[string]json.RawMessage{}
	if raw, ok := models["providers"]; ok {
		_ = json.Unmarshal(raw, &providers)
	}
	pb, _ := json.Marshal(m.openclawProvider())
	providers["switchboard"] = pb
	prb, _ := json.Marshal(providers)
	models["providers"] = prb
	mb, _ := json.Marshal(models)
	doc["models"] = mb
	out, _ := json.MarshalIndent(doc, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

func (m *Manager) disconnectOpenClaw() error {
	path := m.openclawCfg()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	doc := map[string]json.RawMessage{}
	if json.Unmarshal(b, &doc) != nil {
		return nil
	}
	models := map[string]json.RawMessage{}
	_ = json.Unmarshal(doc["models"], &models)
	providers := map[string]json.RawMessage{}
	_ = json.Unmarshal(models["providers"], &providers)
	delete(providers, "switchboard")
	prb, _ := json.Marshal(providers)
	models["providers"] = prb
	mb, _ := json.Marshal(models)
	doc["models"] = mb
	out, _ := json.MarshalIndent(doc, "", "  ")
	return os.WriteFile(path, append(out, '\n'), 0o600)
}
