// Package integrations 负责把调度台接进三个宿主：Claude Code、Hermes Agent、WorkBuddy。
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
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/imoling/iml-switchboard/internal/config"
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
}

// New 创建管理器。
func New(cfg *config.Store, exe, facadeURL string, facadeKey func() string) *Manager {
	home, _ := os.UserHomeDir()
	return &Manager{cfg: cfg, exe: exe, facade: facadeURL, home: home, facadeKey: facadeKey}
}

func (m *Manager) claudeDir() string       { return filepath.Join(m.home, ".claude") }
func (m *Manager) hermesCfg() string       { return filepath.Join(m.home, ".hermes", "config.yaml") }
func (m *Manager) workbuddyModels() string { return filepath.Join(m.home, ".workbuddy", "models.json") }

// List 返回三个宿主的状态。
func (m *Manager) List() []map[string]any {
	return []map[string]any{m.view("claude_code"), m.view("hermes"), m.view("workbuddy")}
}

func (m *Manager) view(id string) map[string]any {
	st := m.cfg.Get().Integrations[id]
	v := map[string]any{"id": id, "status": "disconnected", "options": st.Options, "available": true}
	if v["options"] == nil {
		v["options"] = map[string]bool{}
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
		opts := map[string]bool{"write_claude_md": m.claudeMdHasBlock(), "opusplan": m.claudeModelIs("opusplan"), "commands": m.commandsInstalled()}
		v["options"] = opts
	case "hermes":
		if _, err := os.Stat(m.hermesCfg()); err != nil {
			v["available"] = false
			v["detail"] = "本机没有找到 Hermes 的配置"
		} else if m.hermesHasKeys() {
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
	}
	return v
}

// Connect 接入。
func (m *Manager) Connect(id string, opts map[string]bool) (map[string]any, error) {
	var err error
	switch id {
	case "claude_code":
		err = m.connectClaude(opts)
	case "hermes":
		err = m.connectHermes()
	case "workbuddy":
		err = m.connectWorkBuddy()
	default:
		return nil, errors.New("不认识的宿主")
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
	case "hermes":
		err = m.disconnectHermes()
	case "workbuddy":
		err = m.disconnectWorkBuddy()
	default:
		return nil, errors.New("不认识的宿主")
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

// Update 只改选项（目前只有 Claude Code 有）。
func (m *Manager) Update(id string, opts map[string]bool) (map[string]any, error) {
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
	path := m.claudeMdPath()
	b, _ := os.ReadFile(path)
	s := string(b)
	// 先去掉旧块
	if i := strings.Index(s, markStart); i >= 0 {
		if j := strings.Index(s, markEnd); j > i {
			s = strings.TrimRight(s[:i], "\n") + s[j+len(markEnd):]
		}
	}
	if on {
		snippet, _ := assets.ReadFile("assets/CLAUDE.snippet.md")
		s = strings.TrimRight(s, "\n") + "\n\n" + strings.TrimSpace(string(snippet)) + "\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(s), 0o644)
}

func (m *Manager) commandsInstalled() bool {
	_, err := os.Stat(filepath.Join(m.claudeDir(), "commands", "iml-img.md"))
	return err == nil
}

func (m *Manager) setCommands(on bool) error {
	dir := filepath.Join(m.claudeDir(), "commands")
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

func (m *Manager) wbEntries() []wbModel {
	return []wbModel{
		{ID: "router-auto", Name: "iML Switchboard 自动", Vendor: "iML Switchboard", URL: m.facade + "/v1/chat/completions", APIKey: m.facadeKey(), SupportsToolCall: true, SupportsImages: true},
		{ID: "router-sub", Name: "iML Switchboard 订阅优先", Vendor: "iML Switchboard", URL: m.facade + "/v1/chat/completions", APIKey: m.facadeKey(), SupportsToolCall: false, SupportsImages: false},
	}
}

func (m *Manager) workbuddySnippet() string {
	b, _ := json.MarshalIndent(m.wbEntries(), "", "  ")
	return "// 加到 ~/.workbuddy/models.json 的数组里，或在 WorkBuddy 设置 → 模型 → 自定义里按这两条填\n" + string(b)
}

func (m *Manager) workbuddyHas() bool {
	b, err := os.ReadFile(m.workbuddyModels())
	return err == nil && bytes.Contains(b, []byte(`"router-auto"`))
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
		var probe struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &probe)
		if probe.ID == "router-auto" || probe.ID == "router-sub" {
			continue
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
		var probe struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &probe)
		if probe.ID != "router-auto" && probe.ID != "router-sub" {
			keep = append(keep, raw)
		}
	}
	out, _ := json.MarshalIndent(keep, "", "  ")
	return os.WriteFile(path, append(out, '\n'), 0o600)
}
