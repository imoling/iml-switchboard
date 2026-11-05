package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
)

func withAccounts(t *testing.T, m *Manager) {
	t.Helper()
	_ = m.cfg.Update(func(c *config.Config) error {
		c.API = []config.APIAccount{
			{ID: "winwork", Provider: "winwork", Enabled: true, Models: map[string][]string{"high": {"winwork-snt-5.5"}, "low": {"winwork-ds-v4.1-flash"}}},
			{ID: "custom-1", Provider: "custom", Label: "ChatGPT 网关", Enabled: true, Models: map[string][]string{"mid": {"gpt-5.5"}},
				Discovered: []config.DiscoveredModel{{ID: "gpt-5.5", Name: "GPT-5.5"}, {ID: "gpt-5.5-mini", Name: "GPT-5.5 mini"}, {ID: "gpt-image-2", Name: "GPT Image 2"}}},
			{ID: "off", Provider: "zhipu", Enabled: false, Models: map[string][]string{"mid": {"glm-5.3"}}},
		}
		return nil
	})
}

func ids(list []HostModel, onlyOn bool) []string {
	var out []string
	for _, h := range list {
		if !onlyOn || h.On {
			out = append(out, h.ID)
		}
	}
	return out
}

// 默认：路由型号在前，再是各账号分进档位的型号；服务报的但没分档位的能选、默认不选；出图型号和停用账号不出现
func TestHostModelsDefault(t *testing.T) {
	m, _ := setup(t)
	withAccounts(t, m)
	v, err := m.HostModels("workbuddy")
	if err != nil {
		t.Fatal(err)
	}
	list := v.(HostModelList)
	if list.Custom {
		t.Fatal("没改过不算自定义")
	}
	want := []string{"router-auto", "router-smart", "router-high", "router-mid", "router-low", "router-sub", "winwork-snt-5.5", "winwork-ds-v4.1-flash", "gpt-5.5"}
	if got := ids(list.Models, true); !slices.Equal(got, want) {
		t.Fatalf("默认选中：%v", got)
	}
	if got := ids(list.Models, false); !slices.Equal(got, append(want, "gpt-5.5-mini")) {
		t.Fatalf("候选：%v", got)
	}
	byID := map[string]HostModel{}
	for _, h := range list.Models {
		byID[h.ID] = h
	}
	if byID["gpt-5.5"].Group != "ChatGPT 网关" || byID["winwork-snt-5.5"].Name != "WinWork Sonnet 5.5" || byID["router-sub"].Tools {
		t.Fatalf("分组和名字：%+v %+v %+v", byID["gpt-5.5"], byID["winwork-snt-5.5"], byID["router-sub"])
	}
	// Codex 不给 router-sub
	v, _ = m.HostModels("codex")
	if slices.Contains(ids(v.(HostModelList).Models, false), "router-sub") {
		t.Fatal("Codex 不该有订阅优先")
	}
	if _, err := m.HostModels("claude_code"); err == nil {
		t.Fatal("没有型号表的宿主要报错")
	}
}

// 用户排的顺序照写进宿主，第一个是默认；没选的留在候选里；不认识的名字丢掉；和默认一样就按没改过存
func TestSetHostModelsWritesHosts(t *testing.T) {
	m, home := setup(t)
	withAccounts(t, m)
	if _, err := m.Connect("workbuddy", nil); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o755)
	if _, err := m.Connect("pi", nil); err != nil {
		t.Fatal(err)
	}
	v, err := m.SetHostModels("workbuddy", []string{"gpt-5.5-mini", "router-auto", "ghost-model", "router-auto"})
	if err != nil {
		t.Fatal(err)
	}
	list := v.(HostModelList)
	if !list.Custom || !slices.Equal(ids(list.Models, true), []string{"gpt-5.5-mini", "router-auto"}) {
		t.Fatalf("选中应按用户顺序：%v", ids(list.Models, true))
	}
	if m.cfg.Get().Integrations["workbuddy"].Models[0] != "gpt-5.5-mini" {
		t.Fatal("要存进配置")
	}
	b, _ := os.ReadFile(filepath.Join(home, ".workbuddy", "models.json"))
	var rows []struct {
		ID, Name, Vendor string
		SupportsToolCall bool `json:"supportsToolCall"`
	}
	_ = json.Unmarshal(b, &rows)
	var got []string
	for _, r := range rows {
		got = append(got, r.ID)
	}
	if !slices.Equal(got, []string{"deepseek-v4-pro", "gpt-5.5-mini", "router-auto"}) {
		t.Fatalf("WorkBuddy 的表要立刻重写、原有条目保留：%v", got)
	}
	if rows[1].Name != "Switchboard · GPT-5.5 mini" || rows[1].Vendor != "Switchboard" || rows[2].Name != "Switchboard 自动分档" {
		t.Fatalf("名字：%+v %+v", rows[1], rows[2])
	}
	// 另一个宿主不受影响，还是默认
	pb, _ := os.ReadFile(filepath.Join(home, ".pi", "agent", "models.json"))
	if !strings.Contains(string(pb), `"router-sub"`) || strings.Contains(string(pb), "gpt-5.5-mini") {
		t.Fatalf("pi 应还是默认：%s", pb)
	}
	// pi 改表后也立刻重写，窗口不知道时写 200000
	if _, err := m.SetHostModels("pi", []string{"gpt-5.5"}); err != nil {
		t.Fatal(err)
	}
	pb, _ = os.ReadFile(filepath.Join(home, ".pi", "agent", "models.json"))
	if strings.Contains(string(pb), `"router-auto"`) || !strings.Contains(string(pb), `"gpt-5.5"`) || !strings.Contains(string(pb), `"contextWindow": 200000`) {
		t.Fatalf("pi 的表应只剩选的：%s", pb)
	}

	// 回到默认：空表，或者和默认一模一样
	for _, back := range [][]string{nil, {"router-auto", "router-smart", "router-high", "router-mid", "router-low", "router-sub", "winwork-snt-5.5", "winwork-ds-v4.1-flash", "gpt-5.5"}} {
		v, _ = m.SetHostModels("workbuddy", back)
		if v.(HostModelList).Custom || m.cfg.Get().Integrations["workbuddy"].Models != nil {
			t.Fatalf("%v 应算回到默认", back)
		}
	}
	b, _ = os.ReadFile(filepath.Join(home, ".workbuddy", "models.json"))
	if !strings.Contains(string(b), `"router-sub"`) || !strings.Contains(string(b), "WinWork Sonnet 5.5") {
		t.Fatalf("回到默认后 WorkBuddy 的表要写回：%s", b)
	}
}

// 存过的型号全没了（账号停用）时回到默认，宿主不能没有型号；Codex 的目录跟着表走，第一个是默认
func TestHostModelsGoneAndCodex(t *testing.T) {
	m, _ := setup(t)
	withAccounts(t, m)
	if err := m.setCodexTakeover(true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetHostModels("codex", []string{"gpt-5.5", "router-low"}); err != nil {
		t.Fatal(err)
	}
	rows := readCodexCatalog(t)
	if rows["gpt-5.5"]["priority"] != float64(0) || rows["router-low"]["priority"] != float64(1) || rows["router-auto"] != nil {
		t.Fatalf("目录应按表的顺序只列选中的：%v %v %v", rows["gpt-5.5"], rows["router-low"], rows["router-auto"])
	}
	_ = m.cfg.Update(func(c *config.Config) error {
		c.API[1].Enabled = false
		return nil
	})
	v, _ := m.HostModels("codex")
	list := v.(HostModelList)
	if slices.Contains(ids(list.Models, false), "gpt-5.5") || !slices.Equal(ids(list.Models, true), []string{"router-low"}) {
		t.Fatalf("停用账号的型号不再出现，剩下的照旧：%v", ids(list.Models, true))
	}
	_ = m.cfg.Update(func(c *config.Config) error {
		it := c.Integrations["codex"]
		it.Models = []string{"gpt-5.5"}
		c.Integrations["codex"] = it
		return nil
	})
	v, _ = m.HostModels("codex")
	list = v.(HostModelList)
	if list.Custom || !slices.Contains(ids(list.Models, true), "router-auto") {
		t.Fatalf("选的都没了要回到默认：%v", ids(list.Models, true))
	}
}

// 没装命令的宿主不算接入：配置目录和文件是接入时自己建的，不能当装了的证据
func TestNotInstalledHostsStayDisconnected(t *testing.T) {
	m, home := setup(t)
	t.Setenv("PATH", t.TempDir())
	_ = os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o755)
	_ = os.WriteFile(filepath.Join(home, ".pi", "agent", "models.json"), []byte(`{"providers":{"switchboard":{}}}`), 0o600)
	_ = os.MkdirAll(filepath.Join(home, ".openclaw"), 0o755)
	_ = os.WriteFile(filepath.Join(home, ".openclaw", "openclaw.json"), []byte(`{"models":{"providers":{"switchboard":{}}}}`), 0o600)
	t.Setenv("DSH_HOME", filepath.Join(home, ".dsh"))
	_ = os.MkdirAll(filepath.Join(home, ".dsh"), 0o755)
	_ = os.WriteFile(filepath.Join(home, ".dsh", "cordis.patch.yml"), []byte("- id: llm-pi-ai\n  config:\n    providers:\n      switchboard: {}\n"), 0o600)
	for _, id := range []string{"pi", "openclaw", "deepseek_harness"} {
		v := m.view(id)
		if v["available"] != false || v["status"] != "disconnected" {
			t.Fatalf("%s 没装命令就不该算接入：%v", id, v)
		}
	}
}

// 新加的路由型号补进自己排过的表：插在 after 后面；表里没有 after 的不动
func TestAddRouterModel(t *testing.T) {
	m, _ := setup(t)
	_ = m.cfg.Update(func(c *config.Config) error {
		c.Integrations["workbuddy"] = config.Integration{Models: []string{"gpt-5.5", "router-auto", "router-low"}}
		c.Integrations["pi"] = config.Integration{Models: []string{"gpt-5.5"}}
		return nil
	})
	m.AddRouterModel("router-smart", "router-auto")
	if got := m.cfg.Get().Integrations["workbuddy"].Models; !slices.Equal(got, []string{"gpt-5.5", "router-auto", "router-smart", "router-low"}) {
		t.Fatalf("应插在自动分档后面：%v", got)
	}
	if got := m.cfg.Get().Integrations["pi"].Models; !slices.Equal(got, []string{"gpt-5.5"}) {
		t.Fatalf("不用路由型号的表不动：%v", got)
	}
}
