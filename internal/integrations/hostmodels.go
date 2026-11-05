package integrations

// 宿主的型号表。Codex、WorkBuddy、pi、DeepSeek Harness、OpenClaw 都要把一张型号表写进自己的配置，
// 用户在它们的菜单里才有得选。表的内容在接入页定：默认是四个路由型号加每个按量付费账号分进档位的型号；
// 用户可以增删、排序，第一个是宿主的默认型号。没改过就跟着默认走，新加的账号自动进表；改过就以用户的为准。

import (
	"errors"
	"slices"

	"github.com/imoling/iml-switchboard/internal/config"
	"github.com/imoling/iml-switchboard/internal/core"
)

// HostModel 是宿主型号表里的一个候选。
type HostModel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`              // 宿主里显示的名字
	Group   string `json:"group"`             // 来源：Switchboard 或账号名
	Icon    string `json:"icon"`              // 界面上的图标：switchboard、账号选的图标或它的服务商
	On      bool   `json:"on"`                // 现在写进宿主的
	Default bool   `json:"default,omitempty"` // 跟着默认走时会选上
	Vision  bool   `json:"-"`
	Tools   bool   `json:"-"`
	Context int    `json:"-"` // 0 表示不知道
}

// HostModelList 是接入页看到的整张表：选中的按顺序排前面，其余候选跟在后面。
type HostModelList struct {
	Models []HostModel `json:"models"`
	Custom bool        `json:"custom"` // 用户改过；为假时新账号的型号会自动加进来
}

// modelListHosts 是有型号表的宿主。router-sub 只给走对话接口的宿主：Codex 走 Responses 接口，而且它自己就是订阅
var modelListHosts = map[string]bool{"codex": true, "workbuddy": true, "pi": true, "deepseek_harness": true, "openclaw": true}

const subModelName = "Switchboard 订阅优先"

// hostCandidates 列出宿主能选的全部型号：路由型号，加每个启用账号分进档位的和它自己报的文本型号。
// 只列门面认得的名字：没分档位也没被服务报出来的型号，点名了也路由不到。
func (m *Manager) hostCandidates(host string) []HostModel {
	cfg := m.cfg.Get()
	tiers := map[string][]string{}
	for _, acc := range cfg.API {
		if !acc.Enabled {
			continue
		}
		for _, tier := range []string{"high", "mid", "low"} {
			tiers["router-"+tier] = append(tiers["router-"+tier], acc.Models[tier]...)
		}
	}
	tiers["router-auto"] = slices.Concat(tiers["router-high"], tiers["router-mid"], tiers["router-low"])
	tiers["router-smart"] = tiers["router-auto"]
	var out []HostModel
	for _, r := range core.RouterModels {
		// 路由型号的窗口取它可能落到的型号里最小的，免得宿主压缩得太晚
		out = append(out, HostModel{ID: r.ID, Name: r.Name, Group: "Switchboard", Icon: "switchboard", Default: true, Vision: true, Tools: true, Context: m.smallestContext(tiers[r.ID])})
	}
	if host != "codex" {
		out = append(out, HostModel{ID: "router-sub", Name: subModelName, Group: "Switchboard", Icon: "switchboard", Default: true})
	}
	seen := map[string]bool{}
	for _, acc := range cfg.API {
		if !acc.Enabled {
			continue
		}
		group, icon := acc.Label, acc.Icon
		if group == "" {
			group = core.ProviderLabel(acc.Provider)
		}
		if icon == "" {
			icon = acc.Provider // 自定义服务没选图标时界面按型号名认
		}
		add := func(id, name string, def bool) {
			if id == "" || seen[id] {
				return
			}
			seen[id] = true
			info := m.lookup(id)
			out = append(out, HostModel{ID: id, Name: name, Group: group, Icon: icon, Default: def, Vision: info.Vision, Tools: info.Tools, Context: m.contextOf(id)})
		}
		for _, tier := range []string{"high", "mid", "low"} {
			for _, id := range acc.Models[tier] {
				add(id, m.modelName(id), true)
			}
		}
		for _, d := range acc.Discovered {
			if core.GuessMediaKind(d.ID) != "" {
				continue // 出图、出视频型号不进对话菜单
			}
			name := d.Name
			if name == "" {
				name = d.ID
			}
			add(d.ID, name, false)
		}
	}
	return out
}

// lookup 查型号信息；没接注册表时当作都能用工具、都能看图。
func (m *Manager) lookup(id string) core.ModelInfo {
	if m.Lookup != nil {
		if info, ok := m.Lookup(id); ok {
			return info
		}
	}
	return core.ModelInfo{Vision: true, Tools: true}
}

// hostModels 按用户保存的顺序排出整张表；保存的里面已经没了的型号（账号停用、删掉）不算。
func (m *Manager) hostModels(host string) HostModelList {
	cands := m.hostCandidates(host)
	saved := m.cfg.Get().Integrations[host].Models
	idx := map[string]int{}
	for i, c := range cands {
		idx[c.ID] = i
	}
	var out []HostModel
	used := map[string]bool{}
	for _, id := range saved {
		if i, ok := idx[id]; ok && !used[id] {
			used[id] = true
			c := cands[i]
			c.On = true
			out = append(out, c)
		}
	}
	custom := len(out) > 0 // 保存的一个都不剩时回到默认，宿主不能没有型号
	for _, c := range cands {
		if used[c.ID] {
			continue
		}
		c.On = !custom && c.Default
		out = append(out, c)
	}
	if !custom {
		// 默认选上的排前面，和保存过的看起来一样
		slices.SortStableFunc(out, func(a, b HostModel) int {
			if a.On != b.On {
				if a.On {
					return -1
				}
				return 1
			}
			return 0
		})
	}
	return HostModelList{Models: out, Custom: custom}
}

// selectedHostModels 是真正写进宿主的那部分，按顺序。
func (m *Manager) selectedHostModels(host string) []HostModel {
	var out []HostModel
	for _, h := range m.hostModels(host).Models {
		if h.On {
			out = append(out, h)
		}
	}
	return out
}

// HostModels 返回宿主的型号表。
func (m *Manager) HostModels(id string) (any, error) {
	if !modelListHosts[id] {
		return nil, errors.New("这个应用没有型号表")
	}
	return m.hostModels(id), nil
}

// SetHostModels 保存宿主的型号表并立刻写进宿主；ids 为空表示回到默认。
// 和默认一样的选择也按「没改过」存，这样新账号的型号还能自动进表。
func (m *Manager) SetHostModels(id string, ids []string) (any, error) {
	if !modelListHosts[id] {
		return nil, errors.New("这个应用没有型号表")
	}
	known := map[string]bool{}
	var def []string
	for _, c := range m.hostCandidates(id) {
		known[c.ID] = true
		if c.Default {
			def = append(def, c.ID)
		}
	}
	var keep []string
	for _, x := range ids {
		if known[x] && !slices.Contains(keep, x) {
			keep = append(keep, x)
		}
	}
	if slices.Equal(keep, def) {
		keep = nil
	}
	if err := m.cfg.Update(func(c *config.Config) error {
		it := c.Integrations[id]
		it.Models = keep
		c.Integrations[id] = it
		return nil
	}); err != nil {
		return nil, err
	}
	return m.hostModels(id), m.rewriteHostModels(id)
}

// rewriteHostModels 把新的型号表写进宿主自己的配置；没接入的不动，等接入时一起写。
func (m *Manager) rewriteHostModels(id string) error {
	switch id {
	case "codex":
		m.SyncCodexCatalog()
	case "workbuddy":
		if m.workbuddyHas() {
			return m.connectWorkBuddy()
		}
	case "pi":
		if m.piHas() {
			return m.connectPi()
		}
	case "deepseek_harness":
		if m.dshHas() {
			return m.connectDsh()
		}
	case "openclaw":
		if m.openclawHas() {
			return m.connectOpenClaw()
		}
	}
	return nil
}

// contextOr 给宿主配置里的窗口字段：不知道就写 def。
func contextOr(c, def int) int {
	if c > 0 {
		return c
	}
	return def
}

// AddRouterModel 把新加的路由型号补进各应用：自己排过型号表的，插在 after 后面（表里没有 after 就不动，说明用户不用路由型号）；
// 然后把型号表重新写进已接入的应用。只在升级时跑一次。
func (m *Manager) AddRouterModel(id, after string) {
	_ = m.cfg.Update(func(c *config.Config) error {
		for host, it := range c.Integrations {
			if len(it.Models) == 0 || slices.Contains(it.Models, id) {
				continue
			}
			if i := slices.Index(it.Models, after); i >= 0 {
				it.Models = slices.Insert(it.Models, i+1, id)
				c.Integrations[host] = it
			}
		}
		return nil
	})
	for host := range modelListHosts {
		_ = m.rewriteHostModels(host)
	}
}
