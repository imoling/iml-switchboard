package integrations

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Codex CLI 和 Gemini CLI：用各自官方的 `mcp add` 注册，用户级；调用约定写进它们的全局说明文件。
// IMLS_HOST 让账本分得清是谁派的活。

func (m *Manager) codexConfig() string { return filepath.Join(m.home, ".codex", "config.toml") }
func (m *Manager) codexAgentsMd() string {
	return filepath.Join(m.home, ".codex", "AGENTS.md")
}
func (m *Manager) geminiSettings() string { return filepath.Join(m.home, ".gemini", "settings.json") }
func (m *Manager) geminiMd() string       { return filepath.Join(m.home, ".gemini", "GEMINI.md") }

func (m *Manager) codexRegistered() bool {
	b, err := os.ReadFile(m.codexConfig())
	if err != nil {
		return false
	}
	return bytes.Contains(b, []byte("[mcp_servers."+mcpName+"]")) || bytes.Contains(b, []byte(`[mcp_servers."`+mcpName+`"]`))
}

func (m *Manager) geminiRegistered() bool {
	b, err := os.ReadFile(m.geminiSettings())
	if err != nil {
		return false
	}
	var d struct {
		MCP map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(b, &d) != nil {
		return false
	}
	_, ok := d.MCP[mcpName]
	return ok
}

// runHostCmd 跑宿主的官方命令，失败时把输出压成一行。
func runHostCmd(what string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s失败：%s", what, msg)
	}
	return nil
}

func (m *Manager) connectCodex() error {
	if _, err := exec.LookPath("codex"); err != nil {
		return errors.New("本机没有找到 codex 命令")
	}
	if !m.codexRegistered() {
		if err := runHostCmd("注册到 Codex ", "codex", "mcp", "add", mcpName, "--env", "IMLS_HOST=codex", "--", m.exe, "mcp"); err != nil {
			return err
		}
	}
	return setInstructionBlock(m.codexAgentsMd(), true)
}

func (m *Manager) disconnectCodex() error {
	if m.codexRegistered() {
		if err := runHostCmd("从 Codex 移除", "codex", "mcp", "remove", mcpName); err != nil {
			return err
		}
	}
	return setInstructionBlock(m.codexAgentsMd(), false)
}

func (m *Manager) connectGemini() error {
	if _, err := exec.LookPath("gemini"); err != nil {
		return errors.New("本机没有找到 gemini 命令")
	}
	if !m.geminiRegistered() {
		if err := runHostCmd("注册到 Gemini CLI ", "gemini", "mcp", "add", "-s", "user", "-e", "IMLS_HOST=gemini_cli", mcpName, m.exe, "mcp"); err != nil {
			return err
		}
	}
	return setInstructionBlock(m.geminiMd(), true)
}

func (m *Manager) disconnectGemini() error {
	if m.geminiRegistered() {
		if err := runHostCmd("从 Gemini CLI 移除", "gemini", "mcp", "remove", "-s", "user", mcpName); err != nil {
			return err
		}
	}
	return setInstructionBlock(m.geminiMd(), false)
}

// setInstructionBlock 在宿主的全局说明文件末尾加上或去掉带标记的调用约定，其余内容原样保留。
func setInstructionBlock(path string, on bool) error {
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s := string(b)
	if i := strings.Index(s, markStart); i >= 0 {
		if j := strings.Index(s, markEnd); j > i {
			s = strings.TrimRight(s[:i], "\n") + s[j+len(markEnd):]
		}
	}
	if on {
		snippet, _ := assets.ReadFile("assets/CLAUDE.snippet.md")
		s = strings.TrimLeft(strings.TrimRight(s, "\n")+"\n\n"+strings.TrimSpace(string(snippet))+"\n", "\n")
	} else if strings.TrimSpace(s) == "" {
		// 文件是我们建的、去掉块后空了：删掉，不留空文件
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(s), 0o644)
}

// hostKey 是带宿主标记的门面密钥：门面只校验点号前的部分，点号后的记成宿主，账本才分得清是谁派的活。
func (m *Manager) hostKey(host string) string { return m.facadeKey() + "." + host }
