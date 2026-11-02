package integrations

// Codex 的 /model 列什么只看模型目录（config.toml 的 model_catalog_json），而且是整表替换，启动时读一次。
// 行的格式跟着 Codex 版本变，所以从本机 Codex 自带的目录克隆，不自己拼。
// 官方型号隐藏保留：Codex 内部的小请求和自动审批还会点名它们。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/imoling/iml-switchboard/internal/config"
)

const codexCatalogKey = "model_catalog_json"

type codexModel struct {
	slug, name string
	context    int // 0 表示不知道，沿用模板的窗口
}

func codexCatalogPath() string {
	return filepath.ToSlash(filepath.Join(config.DataDir(), "codex-model-catalog.json"))
}

// codexVersionStamp 标识本机装的是哪个 Codex：升级后可执行文件就变了，比起子进程问版本号省事。找不到时返回空串。
func codexVersionStamp() string {
	p, err := exec.LookPath("codex")
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	st, err := os.Stat(p)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s %d %d", p, st.Size(), st.ModTime().UnixNano())
}

func codexBundledCatalog() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "codex", "debug", "models", "--bundled").Output()
}

// codexCheckCatalog 让本机 Codex 试读目录。目录有一个字段不合它的格式，它就启动不了，所以试读过了才能写进配置。
func codexCheckCatalog(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "codex", "debug", "models", "-c", codexCatalogKey+"="+strconv.Quote(path)).Run()
}

// codexModels 是目录里要列出来的型号，按接入页定的型号表；第一个是 /model 里的默认。
func (m *Manager) codexModels() []codexModel {
	var out []codexModel
	for _, h := range m.selectedHostModels("codex") {
		out = append(out, codexModel{slug: h.ID, name: h.Name, context: h.Context})
	}
	return out
}

func (m *Manager) modelName(id string) string {
	if m.ModelName != nil {
		return m.ModelName(id)
	}
	return id
}

func (m *Manager) contextOf(id string) int {
	if m.ModelContext != nil {
		return m.ModelContext(id)
	}
	return 0
}

func (m *Manager) smallestContext(ids []string) int {
	best := 0
	for _, id := range ids {
		if c := m.contextOf(id); c > 0 && (best == 0 || c < best) {
			best = c
		}
	}
	return best
}

// plainRow 把克隆来的行改成门面接得住的样子，发出来的工具和没有目录时一样。门面只转 function 工具，其余形状会被丢掉：
// freeform 的 apply_patch 是 custom 工具，代码模式和工具搜索要把工具藏起来。没了 apply_patch，Codex 用 exec_command 改文件。
func plainRow(r map[string]any) {
	r["supports_search_tool"] = false
	r["use_responses_lite"] = false
	r["supports_parallel_tool_calls"] = false
	r["support_verbosity"] = false
	r["additional_speed_tiers"] = []any{}
	r["service_tiers"] = []any{}
	delete(r, "apply_patch_tool_type")
	delete(r, "tool_mode")
	delete(r, "multi_agent_version")
}

// codexTemplateRow 挑一行官方型号当模板：优先列表里能选、没开代码模式和精简协议的。
func codexTemplateRow(rows []map[string]any) map[string]any {
	for _, r := range rows {
		lite, _ := r["use_responses_lite"].(bool)
		if _, code := r["tool_mode"]; r["visibility"] == "list" && !lite && !code {
			return r
		}
	}
	return rows[0]
}

// writeCodexCatalog 生成模型目录文件。
func (m *Manager) writeCodexCatalog() error {
	b, err := m.codexBundled()
	if err != nil {
		return err
	}
	var doc struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return err
	}
	if len(doc.Models) == 0 {
		return errors.New("Codex 自带的模型目录是空的")
	}
	tpl := codexTemplateRow(doc.Models)
	rows := make([]map[string]any, 0, len(doc.Models)+8)
	listed := map[string]bool{}
	for i, c := range m.codexModels() {
		listed[c.slug] = true
		r := maps.Clone(tpl)
		plainRow(r)
		r["slug"], r["display_name"], r["description"] = c.slug, c.name, c.name
		r["visibility"], r["priority"] = "list", i
		r["upgrade"], r["availability_nux"] = nil, nil
		// 门面不转思考强度，只留一档，菜单里就不会有选了也没用的项
		r["default_reasoning_level"] = "medium"
		r["supported_reasoning_levels"] = []any{map[string]any{"effort": "medium", "description": "由 Switchboard 决定"}}
		if c.context > 0 {
			r["context_window"], r["max_context_window"] = c.context, c.context
		}
		rows = append(rows, r)
	}
	for _, r := range doc.Models {
		if slug, _ := r["slug"].(string); listed[slug] {
			continue // 账号里的型号和官方同名（网关转的 gpt-5.5 之类）：一个名字只留我们列出的那行
		}
		plainRow(r)
		r["visibility"] = "hide"
		rows = append(rows, r)
	}
	out, err := json.Marshal(map[string]any{"models": rows})
	if err != nil {
		return err
	}
	path := codexCatalogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	if err := m.codexCheck(tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return os.WriteFile(path+".stamp", []byte(m.codexStamp()), 0o600)
}

func removeCodexCatalog() {
	_ = os.Remove(codexCatalogPath())
	_ = os.Remove(codexCatalogPath() + ".stamp")
}

// codexCatalogCurrent 判断目录是否还对得上：列出的型号和现在该列的一致，生成时的 Codex 也还是这一个。
// 找不到 Codex 时比不了版本，不算过期，免得白白撤掉能用的目录。
func (m *Manager) codexCatalogCurrent() bool {
	b, err := os.ReadFile(codexCatalogPath())
	if err != nil {
		return false
	}
	if now := m.codexStamp(); now != "" {
		if s, err := os.ReadFile(codexCatalogPath() + ".stamp"); err != nil || string(s) != now {
			return false
		}
	}
	var doc struct {
		Models []struct {
			Slug       string `json:"slug"`
			Visibility string `json:"visibility"`
		} `json:"models"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return false
	}
	var have, want []string
	for _, r := range doc.Models {
		if r.Visibility == "list" {
			have = append(have, r.Slug)
		}
	}
	for _, c := range m.codexModels() {
		want = append(want, c.slug)
	}
	return slices.Equal(have, want)
}

// SyncCodexCatalog 让接管中的 Codex 目录跟上账号增减和 Codex 升级，也让早先开的接管补上目录；已经一致就不动。
// 新版 Codex 读不了旧目录时撤掉指针：没有目录只是看不到档位，留着坏目录它就起不来。
func (m *Manager) SyncCodexCatalog() {
	m.catalogMu.Lock()
	defer m.catalogMu.Unlock()
	if !m.codexTakenOver() {
		return
	}
	path := m.codexConfig()
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	toml := string(b)
	pointed := topLevelValue(toml, codexCatalogKey) == codexCatalogPath()
	if pointed && m.codexCatalogCurrent() {
		return
	}
	// 同样的 Codex 和型号已经失败过就不再试：接入页每次打开都会走到这里
	attempt := m.codexStamp()
	for _, c := range m.codexModels() {
		attempt += "\n" + c.slug
	}
	if !pointed && attempt == m.catalogFailed {
		return
	}
	if m.writeCodexCatalog() != nil {
		m.catalogFailed = attempt
		if pointed {
			removeCodexCatalog()
			_ = os.WriteFile(path, []byte(setTopLevel(toml, codexCatalogKey, "")), 0o600)
		}
		return
	}
	m.catalogFailed = ""
	_ = os.WriteFile(path, []byte(setTopLevel(toml, codexCatalogKey, codexCatalogPath())), 0o600)
}
