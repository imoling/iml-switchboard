package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imoling/iml-switchboard/internal/config"
	"github.com/imoling/iml-switchboard/internal/core"
)

type fake struct {
	id     core.LaneID
	health core.Health
}

func (f *fake) ID() core.LaneID { return f.id }
func (f *fake) Run(ctx context.Context, t *core.Task, tg core.Target, emit func(core.Event)) (*core.Result, error) {
	if emit != nil {
		emit(core.Event{Type: "text_delta", Text: "你好"})
	}
	return &core.Result{Text: "你好，来自 " + tg.Model, Model: tg.Model, Usage: core.Usage{InputTokens: 10, OutputTokens: 5, CostUSD: 0.0001}}, nil
}
func (f *fake) Health(ctx context.Context) core.Health {
	if f.health.Status == "" {
		return core.Health{Status: "ok"}
	}
	return f.health
}

type fakeAccounts struct{}

func (fakeAccounts) Verify(ctx context.Context, provider, baseURL, apiKey string) ([]string, error) {
	if apiKey == "bad" {
		return nil, &core.ErrProvider{Summary: provider + " 验证失败：密钥无效或已过期"}
	}
	return []string{"glm-5.3"}, nil
}
func (fakeAccounts) Preset(p string) (string, string, map[string][]string, bool, bool) {
	if p == "custom" {
		return "", "USD", map[string][]string{}, false, true // 和真的一样：地址由用户填
	}
	return "https://x/v1", "CNY", map[string][]string{"low": {"glm-5.3-flash"}, "mid": {"glm-5.3"}}, false, true
}
func (fakeAccounts) ListModels(ctx context.Context, baseURL, apiKey string) ([]config.DiscoveredModel, error) {
	if apiKey != "good" {
		return nil, errors.New("密钥不对：服务拒绝了这个密钥")
	}
	return []config.DiscoveredModel{{ID: "gpt-6.1-sol", Name: "GPT-6.1 Sol"}, {ID: "gpt-6.1-terra", Name: "GPT-6.1 Terra"}, {ID: "gpt-image-2", Name: "GPT Image 2"}}, nil
}
func (fakeAccounts) Providers() []string   { return []string{"zhipu"} }
func (fakeAccounts) Reload(*config.Config) {}
func (fakeAccounts) Discover() bool        { return false }

type fakeIntegr struct{}

func (fakeIntegr) List() []map[string]any {
	return []map[string]any{{"id": "claude_code", "status": "disconnected"}, {"id": "doubao_work", "status": "manual"}}
}
func (fakeIntegr) Connect(id string, o map[string]bool) (map[string]any, error) {
	return map[string]any{"id": id, "status": "connected"}, nil
}
func (fakeIntegr) Disconnect(id string) (map[string]any, error) { return map[string]any{"id": id}, nil }
func (fakeIntegr) Update(id string, o map[string]bool) (map[string]any, error) {
	return map[string]any{"id": id}, nil
}
func (fakeIntegr) HostModels(id string) (any, error) {
	return map[string]any{"models": []map[string]any{{"id": "router-auto", "on": true}}, "custom": false}, nil
}
func (fakeIntegr) SetHostModels(id string, ids []string) (any, error) {
	return map[string]any{"models": ids, "custom": len(ids) > 0}, nil
}

func newServer(t *testing.T) (*httptest.Server, *core.Router) {
	t.Helper()
	return newServerWith(t, nil)
}

func newServerWith(t *testing.T, tweak func(*Deps)) (*httptest.Server, *core.Router) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("IMLS_DATA_DIR", dir)
	t.Setenv("IMLS_SECRETS_FILE", "1")
	cs, _ := config.Open()
	l, _ := core.OpenLedger(filepath.Join(dir, "ledger.db"))
	t.Cleanup(func() { l.Close() })
	reg := core.NewRegistry(1.0 / 7.1)
	r := core.NewRouter(cs, l, reg, core.NewQuotaManager(l, func() float64 { return 70 }))
	r.Register(&fake{id: core.LaneClaudeSub})
	r.Register(&fake{id: core.LaneAPI})
	r.Register(&fake{id: core.LaneCodexSub})
	bus := core.NewBus()
	r.OnCall(func(c *core.Call) { bus.PublishCall(c) })
	d := Deps{Router: r, Jobs: core.NewJobStore(l, nil), Bus: bus, Accounts: fakeAccounts{}, Integr: fakeIntegr{}, Secrets: core.NewSecrets(dir), StartedAt: time.Now()}
	if tweak != nil {
		tweak(&d)
	}
	s := New(d)
	mux := http.NewServeMux()
	s.Routes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, r
}

func do(t *testing.T, ts *httptest.Server, method, path string, body any) (int, map[string]any, []byte) {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, ts.URL+path, rd)
	req.Header.Set("X-IMLS-Client", "test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(buf.Bytes(), &m)
	return resp.StatusCode, m, buf.Bytes()
}

func TestStatusAndAuth(t *testing.T) {
	ts, _ := newServer(t)
	resp, _ := http.Get(ts.URL + "/api/status")
	if resp.StatusCode != 403 {
		t.Fatalf("没有客户端标识应 403，得到 %d", resp.StatusCode)
	}
	code, m, _ := do(t, ts, "GET", "/api/status", nil)
	if code != 200 || m["theme"] != "dianjiang" || m["app_title"] != "点将台" {
		t.Fatalf("%d %v", code, m)
	}
	code, m, _ = do(t, ts, "POST", "/api/projection", map[string]bool{"on": true})
	if m["theme"] != "standard" || m["app_title"] != "Switchboard" {
		t.Fatalf("投屏模式应切回标准：%v", m)
	}
}

// 客户端靠 build 比对版本、靠 busy 提醒重启会打断几件事、靠 /api/quit 让旧后台让位
func TestBuildBusyAndQuit(t *testing.T) {
	quit := make(chan struct{})
	ts, _ := newServerWith(t, func(d *Deps) { d.Build = "abc123def456"; d.Quit = func() { close(quit) } })
	_, m, _ := do(t, ts, "GET", "/api/status", nil)
	if m["build"] != "abc123def456" || m["busy"] != float64(0) {
		t.Fatalf("状态里应有构建号和进行中的数量：%v", m)
	}
	code, m, _ := do(t, ts, "POST", "/api/quit", nil)
	if code != 200 || m["ok"] != true {
		t.Fatalf("%d %v", code, m)
	}
	select {
	case <-quit:
	case <-time.After(2 * time.Second):
		t.Fatal("回话之后应当退出")
	}
	if resp, _ := http.Post(ts.URL+"/api/quit", "application/json", nil); resp.StatusCode != 403 {
		t.Fatalf("没有客户端标识不能让后台退出，得到 %d", resp.StatusCode)
	}
}

func TestTryAndRun(t *testing.T) {
	ts, _ := newServer(t)
	_, m, _ := do(t, ts, "POST", "/api/rules/try", map[string]any{"prompt": "/fable 这个系统的架构怎么设计"})
	if m["tier"] != "top" || m["tier_label"] != "顶配" {
		t.Fatalf("%v", m)
	}
	// 快速面板预览给出备选
	_, m, _ = do(t, ts, "POST", "/api/quick/preview", map[string]any{"prompt": "翻译一下：hello"})
	if m["tier"] != "low" || len(m["alternatives"].([]any)) == 0 {
		t.Fatalf("%v", m)
	}
	// 执行并记账
	req, _ := http.NewRequest("POST", ts.URL+"/api/quick/run", strings.NewReader(`{"prompt":"翻译一下：hello"}`))
	req.Header.Set("X-IMLS-Client", "test")
	resp, _ := http.DefaultClient.Do(req)
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	if !strings.Contains(buf.String(), "event: text_delta") || !strings.Contains(buf.String(), "event: done") {
		t.Fatalf("SSE 内容不对：%s", buf.String())
	}
	_, _, raw := do(t, ts, "GET", "/api/calls", nil)
	var calls []map[string]any
	_ = json.Unmarshal(raw, &calls)
	if len(calls) != 1 || calls[0]["task_type"] != "rewrite" {
		t.Fatalf("%s", raw)
	}
	id := calls[0]["id"].(string)
	_, m, _ = do(t, ts, "POST", "/api/calls/"+id+"/escalate", nil)
	if m["escalations_for_task"].(float64) != 1 {
		t.Fatalf("%v", m)
	}
	_, m, _ = do(t, ts, "POST", "/api/calls/"+id+"/redo", nil)
	if m["call"].(map[string]any)["tier"] != "mid" {
		t.Fatalf("重做应升一档：%v", m)
	}
	_, m, _ = do(t, ts, "GET", "/api/overview?range=today", nil)
	if m["calls"].(float64) != 2 {
		t.Fatalf("%v", m)
	}
}

func TestAccountsAndSettings(t *testing.T) {
	ts, _ := newServer(t)
	code, m, _ := do(t, ts, "POST", "/api/accounts", map[string]string{"provider": "zhipu", "api_key": "bad"})
	if code != 400 || !strings.Contains(m["error"].(string), "验证失败") {
		t.Fatalf("%d %v", code, m)
	}
	code, m, _ = do(t, ts, "POST", "/api/accounts", map[string]string{"provider": "zhipu", "api_key": "good"})
	if code != 200 || m["status"] != "ok" {
		t.Fatalf("%d %v", code, m)
	}
	id := m["id"].(string)
	_, _, raw := do(t, ts, "GET", "/api/accounts", nil)
	var list []map[string]any
	_ = json.Unmarshal(raw, &list)
	if len(list) != 4 {
		t.Fatalf("应有 3 个订阅 + 1 个按量付费：%s", raw)
	}
	code, _, _ = do(t, ts, "DELETE", "/api/accounts/"+id, nil)
	if code != 200 {
		t.Fatal(code)
	}
	code, m, _ = do(t, ts, "PUT", "/api/settings", map[string]any{"theme": "harem", "monthly_api_limit_usd": 50})
	if code != 200 || m["theme"] != "harem" || m["monthly_api_limit_usd"].(float64) != 50 {
		t.Fatalf("%d %v", code, m)
	}
	code, _, _ = do(t, ts, "PUT", "/api/settings", map[string]any{"theme": "nope"})
	if code != 400 {
		t.Fatal("非法主题应报错")
	}
	code, m, _ = do(t, ts, "PUT", "/api/settings", map[string]any{"tray_icon": "patchbay"})
	if code != 200 || m["tray_icon"] != "patchbay" {
		t.Fatalf("菜单栏图标样式应能保存：%d %v", code, m)
	}
	if code, _, _ := do(t, ts, "PUT", "/api/settings", map[string]any{"tray_icon": "nope"}); code != 400 {
		t.Fatal("不认识的图标样式应报错")
	}
	_, m, _ = do(t, ts, "GET", "/api/rules", nil)
	rows := m["task_table"].([]any)
	if len(rows) < 10 {
		t.Fatalf("%v", m)
	}
	_, m, _ = do(t, ts, "GET", "/api/onboarding", nil)
	if m["done"] != false {
		t.Fatalf("%v", m)
	}
}

func TestInternalTool(t *testing.T) {
	ts, _ := newServer(t)
	_, m, _ := do(t, ts, "POST", "/api/internal/tool", map[string]any{"name": "ask_model", "args": map[string]any{"prompt": "翻译：hi", "tier": "low"}, "host": "claude_code"})
	if !strings.Contains(m["text"].(string), "你好") || !strings.Contains(m["text"].(string), "·") {
		t.Fatalf("%v", m)
	}
}

// 订阅没登录：没有额度可用，账号列表和额度里都不出现；登录后自动回来
func TestSignedOutSubscriptionHidden(t *testing.T) {
	ts, r := newServer(t)
	r.Register(&fake{id: core.LaneCodexSub, health: core.Health{Status: "warn", Detail: "Codex 还没登录：运行 codex login", SignedOut: true}})
	_, _, raw := do(t, ts, "GET", "/api/accounts", nil)
	if strings.Contains(string(raw), `"codex_sub"`) || !strings.Contains(string(raw), `"claude_sub"`) {
		t.Fatalf("没登录的订阅不该出现：%s", raw)
	}
	_, _, raw = do(t, ts, "GET", "/api/overview?range=today", nil)
	if strings.Contains(string(raw), `"account_id":"codex_sub"`) {
		t.Fatalf("没登录的订阅不该有额度行：%s", raw)
	}
	r.Register(&fake{id: core.LaneCodexSub})
	if _, _, raw = do(t, ts, "GET", "/api/accounts", nil); !strings.Contains(string(raw), `"codex_sub"`) {
		t.Fatal("登录后要回来")
	}
}

// 自定义服务：填地址和密钥，型号从服务拉；没填名称用主机名；重新验证时型号换新
func TestAddCustomService(t *testing.T) {
	ts, r := newServer(t)
	code, m, raw := do(t, ts, "POST", "/api/accounts", map[string]any{"provider": "custom", "api_key": "good", "base_url": "http://10.0.0.8:30880/v1/"})
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	if !strings.Contains(string(raw), "服务有 3 个型号，还没分到档位") {
		t.Fatalf("没分档位时要说清：%s", raw)
	}
	if m["label"] != "10.0.0.8:30880" || !strings.Contains(string(raw), `"gpt-6.1-sol"`) || !strings.Contains(string(raw), `"GPT-6.1 Sol"`) {
		t.Fatalf("要用主机名当名称、列出服务报的型号：%s", raw)
	}
	acc := r.Config().Get().API[len(r.Config().Get().API)-1]
	if acc.BaseURL != "http://10.0.0.8:30880/v1" || len(acc.Discovered) != 3 {
		t.Fatalf("地址要去掉末尾斜杠、型号要存下：%+v", acc)
	}
	// 出图型号进媒体选项、不进文本档位
	var view struct {
		Models       []string            `json:"models"`
		MediaOptions map[string][]string `json:"media_options"`
	}
	_ = json.Unmarshal(raw, &view)
	if len(view.MediaOptions["image"]) != 1 || view.MediaOptions["image"][0] != "gpt-image-2" {
		t.Fatalf("出图型号要能选：%+v", view.MediaOptions)
	}
	for _, m := range view.Models {
		if m == "gpt-image-2" {
			t.Fatalf("出图型号不该进文本档位：%v", view.Models)
		}
	}
	// 图标：添加时没选就按型号名自动认（界面认），可以随时换
	id, _ := m["id"].(string)
	if _, m2, raw2 := do(t, ts, "PUT", "/api/accounts/"+id, map[string]any{"icon": "openai"}); m2["icon"] != "openai" {
		t.Fatalf("要能换图标：%s", raw2)
	}
	if _, m2, raw2 := do(t, ts, "PUT", "/api/accounts/"+id, map[string]any{"icon": ""}); m2["icon"] != nil {
		t.Fatalf("空串表示回到自动：%s", raw2)
	}
	if code, _, raw := do(t, ts, "POST", "/api/accounts", map[string]any{"provider": "custom", "api_key": "good"}); code == 200 || !strings.Contains(string(raw), "接口地址") {
		t.Fatalf("没填地址要拦下：%d %s", code, raw)
	}
	if code, _, raw := do(t, ts, "POST", "/api/accounts", map[string]any{"provider": "custom", "api_key": "bad", "base_url": "http://10.0.0.8/v1"}); code == 200 || !strings.Contains(string(raw), "密钥不对") {
		t.Fatalf("密钥不对要说清：%d %s", code, raw)
	}
}

// winworkSecrets 仿生产里的做法：WinWork 的令牌不在凭据库里，没打开时报「不在这里」
type winworkSecrets struct{ core.Secrets }

func (w winworkSecrets) Get(name string) (string, error) {
	if name == "winwork" {
		return "", &core.ErrKeyNotHere{Reason: "WinWork 没打开，有请求时会自动启动"}
	}
	return w.Secrets.Get(name)
}

// WinWork 是本机登录的客户端：自动出现、不计费，界面上和订阅排在一起；没打开时提示会自动启动
func TestWinWorkAccountLocal(t *testing.T) {
	ts, r := newServerWith(t, func(d *Deps) { d.Secrets = winworkSecrets{d.Secrets} })
	_ = r.Config().Update(func(c *config.Config) error {
		c.API = append(c.API, config.APIAccount{ID: "winwork", Provider: "winwork", Label: "WinWork", Enabled: true, Models: map[string][]string{"high": {"winwork-snt-5.5"}}})
		return nil
	})
	_, _, raw := do(t, ts, "GET", "/api/accounts", nil)
	var list []map[string]any
	_ = json.Unmarshal(raw, &list)
	var ww map[string]any
	for _, a := range list {
		if a["id"] == "winwork" {
			ww = a
		}
	}
	if ww == nil || ww["local"] != true || ww["plan"] != "不计费" || ww["kind"] != "api" {
		t.Fatalf("WinWork 应标成本机登录、不计费：%v", ww)
	}
	if ww["status"] != "warn" || !strings.Contains(ww["detail"].(string), "自动启动") {
		t.Fatalf("没打开时应提示会自动启动：%v %v", ww["status"], ww["detail"])
	}
}

// 豆包工作的连接器在它自己的账号里，本机看不到：派过活就算接上了，别的宿主不受影响
func TestDoubaoWorkConnectedByActivity(t *testing.T) {
	ts, r := newServer(t)
	_, _, raw := do(t, ts, "GET", "/api/integrations", nil)
	if !strings.Contains(string(raw), `"id":"doubao_work","status":"manual"`) {
		t.Fatalf("没派过活时是手动：%s", raw)
	}
	_ = r.Ledger().Insert(&core.Call{ID: "c1", TS: time.Now(), Source: "mcp", Host: "doubao_work", Kind: "text", Lane: core.LaneAPI, OK: true})
	_, _, raw = do(t, ts, "GET", "/api/integrations", nil)
	var list []map[string]any
	_ = json.Unmarshal(raw, &list)
	got := map[string]any{}
	for _, it := range list {
		got[it["id"].(string)] = it["status"]
	}
	if got["doubao_work"] != "connected" || got["claude_code"] != "disconnected" {
		t.Fatalf("派过活的豆包工作应算接入：%v", got)
	}
}

// 按量付费账号配好后能改密钥、地址和名字：先验证再存，坏密钥不落盘
func TestUpdateAccountKeyAndAddress(t *testing.T) {
	ts, r := newServer(t)
	_, m, _ := do(t, ts, "POST", "/api/accounts", map[string]any{"provider": "zhipu", "api_key": "good"})
	id := m["id"].(string)
	if code, m, _ := do(t, ts, "PUT", "/api/accounts/"+id, map[string]any{"api_key": "bad"}); code != 400 || !strings.Contains(m["error"].(string), "验证失败") {
		t.Fatalf("坏密钥应被拒：%d %v", code, m)
	}
	code, m, _ := do(t, ts, "PUT", "/api/accounts/"+id, map[string]any{"api_key": "good2", "label": "智谱备用"})
	if code != 200 || m["label"] != "智谱备用" {
		t.Fatalf("改密钥和名字：%d %v", code, m)
	}
	if r.Config().Get().API[0].ID != id {
		t.Fatal("账号没了")
	}
	_, m, _ = do(t, ts, "POST", "/api/accounts", map[string]any{"provider": "custom", "api_key": "good", "base_url": "http://a.example/v1"})
	cid := m["id"].(string)
	code, m, _ = do(t, ts, "PUT", "/api/accounts/"+cid, map[string]any{"base_url": "http://b.example/v1/"})
	if code != 200 || m["base_url"] != "http://b.example/v1" {
		t.Fatalf("改地址应去掉末尾斜杠并回填：%d %v", code, m)
	}
}

// 档位候选表：读出来是生效顺序加其余可选；手排后照表，乱填的丢掉，清空回到内置排法
func TestTierPicks(t *testing.T) {
	ts, r := newServer(t)
	_ = r.Config().Update(func(c *config.Config) error {
		c.API = []config.APIAccount{{ID: "zhipu-1", Provider: "zhipu", Enabled: true, Currency: "CNY",
			Models: map[string][]string{"low": {"glm-5.3-flash"}, "mid": {"glm-5.3"}, "high": {}}}}
		return nil
	})
	ids := func(m map[string]any, tier string, onOnly bool) []string {
		var out []string
		for _, p := range m["tiers"].(map[string]any)[tier].(map[string]any)["picks"].([]any) {
			row := p.(map[string]any)
			if !onOnly || row["on"] == true {
				out = append(out, row["id"].(string))
			}
		}
		return out
	}
	_, m, _ := do(t, ts, "GET", "/api/rules", nil)
	if low := ids(m, "low", true); len(low) < 2 || low[0] != "api:zhipu-1:glm-5.3-flash" {
		t.Fatalf("低档内置排法先按量付费：%v", low)
	}
	if m["tiers"].(map[string]any)["low"].(map[string]any)["custom"] != false {
		t.Fatal("没手排时 custom 应为假")
	}
	code, m, _ := do(t, ts, "PUT", "/api/rules", map[string]any{"tier_picks": map[string][]string{"low": {"claude_sub", "bogus", "api:zhipu-1:nope", "api:zhipu-1:glm-5.3-flash"}}})
	if code != 200 {
		t.Fatalf("%d %v", code, m)
	}
	if low := ids(m, "low", true); len(low) != 2 || low[0] != "claude_sub" || low[1] != "api:zhipu-1:glm-5.3-flash" {
		t.Fatalf("应照表并丢掉乱填的：%v", low)
	}
	if m["tiers"].(map[string]any)["low"].(map[string]any)["custom"] != true {
		t.Fatal("手排后 custom 应为真")
	}
	if got := r.Config().Get().Routing.Tiers["low"]; len(got) != 2 || got[0].Lane != "claude_sub" || got[1].Model != "glm-5.3-flash" {
		t.Fatalf("配置里应存成结构化条目：%+v", got)
	}
	// 其余可选的跟在后面，不重复
	if all := ids(m, "low", false); len(all) != len(ids(m, "low", true))+countOff(m, "low") {
		t.Fatalf("候选列表不该重复：%v", all)
	}
	code, _, _ = do(t, ts, "PUT", "/api/rules", map[string]any{"tier_picks": map[string][]string{"top": {"claude_sub"}}})
	if code != 400 {
		t.Fatal("顶配没有候选表")
	}
	_, m, _ = do(t, ts, "PUT", "/api/rules", map[string]any{"tier_picks": map[string][]string{"low": {}}})
	if m["tiers"].(map[string]any)["low"].(map[string]any)["custom"] != false || len(r.Config().Get().Routing.Tiers) != 0 {
		t.Fatalf("清空应回到内置排法：%v", r.Config().Get().Routing.Tiers)
	}
}

func countOff(m map[string]any, tier string) int {
	n := 0
	for _, p := range m["tiers"].(map[string]any)[tier].(map[string]any)["picks"].([]any) {
		if p.(map[string]any)["on"] == false {
			n++
		}
	}
	return n
}

// 宿主自己的档位表：没排时显示全局顺序，排了照自己的，清空回到跟全局；不认识的宿主 404
func TestHostTierPicks(t *testing.T) {
	ts, r := newServer(t)
	_ = r.Config().Update(func(c *config.Config) error {
		c.API = []config.APIAccount{{ID: "zhipu-1", Provider: "zhipu", Enabled: true, Currency: "CNY",
			Models: map[string][]string{"low": {"glm-5.3-flash"}, "mid": {"glm-5.3"}, "high": {}}}}
		return nil
	})
	on := func(m map[string]any, tier string) []string {
		var out []string
		for _, p := range m[tier].(map[string]any)["picks"].([]any) {
			if row := p.(map[string]any); row["on"] == true {
				out = append(out, row["id"].(string))
			}
		}
		return out
	}
	// 全局先排成 Claude 在前
	if code, _, _ := do(t, ts, "PUT", "/api/rules", map[string]any{"tier_picks": map[string][]string{"low": {"claude_sub", "api:zhipu-1:glm-5.3-flash"}}}); code != 200 {
		t.Fatal(code)
	}
	code, m, _ := do(t, ts, "GET", "/api/integrations/workbuddy/tiers", nil)
	if code != 200 || m["low"].(map[string]any)["custom"] != false || on(m, "low")[0] != "claude_sub" {
		t.Fatalf("没排的宿主应显示全局顺序：%d %v", code, m)
	}
	code, m, _ = do(t, ts, "PUT", "/api/integrations/workbuddy/tiers", map[string][]string{"low": {"api:zhipu-1:glm-5.3-flash"}})
	if code != 200 || m["low"].(map[string]any)["custom"] != true || len(on(m, "low")) != 1 || on(m, "low")[0] != "api:zhipu-1:glm-5.3-flash" {
		t.Fatalf("宿主自己的表：%d %v", code, m)
	}
	inh := m["low"].(map[string]any)["inherited"].([]any)
	if len(inh) != 2 || inh[0] != "claude_sub" {
		t.Fatalf("继承的应是全局顺序：%v", inh)
	}
	_, lm, _ := do(t, ts, "GET", "/api/integrations", nil)
	_ = lm
	_, m, _ = do(t, ts, "PUT", "/api/integrations/workbuddy/tiers", map[string][]string{"low": {}})
	if m["low"].(map[string]any)["custom"] != false || r.Config().Get().Integrations["workbuddy"].Tiers != nil {
		t.Fatalf("清空应跟全局：%v", r.Config().Get().Integrations["workbuddy"])
	}
	if code, _, _ := do(t, ts, "GET", "/api/integrations/nope/tiers", nil); code != 404 {
		t.Fatalf("不认识的宿主应 404：%d", code)
	}
}
