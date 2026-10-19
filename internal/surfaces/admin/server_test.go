package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imoling/iml-switchboard/internal/config"
	"github.com/imoling/iml-switchboard/internal/core"
)

type fake struct{ id core.LaneID }

func (f *fake) ID() core.LaneID { return f.id }
func (f *fake) Run(ctx context.Context, t *core.Task, tg core.Target, emit func(core.Event)) (*core.Result, error) {
	if emit != nil {
		emit(core.Event{Type: "text_delta", Text: "你好"})
	}
	return &core.Result{Text: "你好，来自 " + tg.Model, Model: tg.Model, Usage: core.Usage{InputTokens: 10, OutputTokens: 5, CostUSD: 0.0001}}, nil
}
func (f *fake) Health(ctx context.Context) core.Health { return core.Health{Status: "ok"} }

type fakeAccounts struct{}

func (fakeAccounts) Verify(ctx context.Context, provider, baseURL, apiKey string) ([]string, error) {
	if apiKey == "bad" {
		return nil, &core.ErrProvider{Summary: provider + " 验证失败：密钥无效或已过期"}
	}
	return []string{"glm-5.3"}, nil
}
func (fakeAccounts) Preset(p string) (string, string, map[string][]string, bool, bool) {
	return "https://x/v1", "CNY", map[string][]string{"low": {"glm-5.3-flash"}, "mid": {"glm-5.3"}}, false, true
}
func (fakeAccounts) Providers() []string   { return []string{"zhipu"} }
func (fakeAccounts) Reload(*config.Config) {}

type fakeIntegr struct{}

func (fakeIntegr) List() []map[string]any {
	return []map[string]any{{"id": "claude_code", "status": "disconnected"}}
}
func (fakeIntegr) Connect(id string, o map[string]bool) (map[string]any, error) {
	return map[string]any{"id": id, "status": "connected"}, nil
}
func (fakeIntegr) Disconnect(id string) (map[string]any, error) { return map[string]any{"id": id}, nil }
func (fakeIntegr) Update(id string, o map[string]bool) (map[string]any, error) {
	return map[string]any{"id": id}, nil
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
	r.Register(&fake{core.LaneClaudeSub})
	r.Register(&fake{core.LaneAPI})
	r.Register(&fake{core.LaneCodexSub})
	bus := core.NewBus()
	r.OnCall(func(c *core.Call) { bus.Publish("call", c) })
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
