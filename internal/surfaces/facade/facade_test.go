package facade

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
	"github.com/imoling/iml-switchboard/internal/core"
)

type fake struct {
	id   core.LaneID
	last *core.Task
}

func (f *fake) ID() core.LaneID { return f.id }
func (f *fake) Run(ctx context.Context, t *core.Task, tg core.Target, emit func(core.Event)) (*core.Result, error) {
	f.last = t
	if emit != nil {
		emit(core.Event{Type: "text_delta", Text: "流式"})
	}
	return &core.Result{Text: "回答", Model: tg.Model, Usage: core.Usage{InputTokens: 3, OutputTokens: 2}}, nil
}
func (f *fake) Health(ctx context.Context) core.Health { return core.Health{Status: "ok"} }

func setup(t *testing.T) (*httptest.Server, *fake, *fake) {
	dir := t.TempDir()
	t.Setenv("IMLS_DATA_DIR", dir)
	cs, _ := config.Open()
	_ = cs.Update(func(c *config.Config) error {
		c.Settings.FacadeKey = "k"
		c.API = []config.APIAccount{{ID: "z", Provider: "zhipu", Enabled: true, Models: map[string][]string{"low": {"glm-5.3-flash"}, "mid": {"glm-5.3"}, "high": {"glm-5.3"}}}}
		return nil
	})
	l, _ := core.OpenLedger(filepath.Join(dir, "l.db"))
	t.Cleanup(func() { l.Close() })
	r := core.NewRouter(cs, l, core.NewRegistry(0.14), core.NewQuotaManager(l, func() float64 { return 70 }))
	claude, api := &fake{id: core.LaneClaudeSub}, &fake{id: core.LaneAPI}
	r.Register(claude)
	r.Register(api)
	mux := http.NewServeMux()
	New(r, func() string { return "k" }).Routes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts, claude, api
}

func post(ts *httptest.Server, path, key string, body any) (*http.Response, string) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", ts.URL+path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	resp.Body.Close()
	return resp, buf.String()
}

func TestChat(t *testing.T) {
	ts, claude, api := setup(t)
	resp, _ := post(ts, "/v1/chat/completions", "wrong", map[string]any{"model": "router-auto", "messages": []map[string]string{{"role": "user", "content": "hi"}}})
	if resp.StatusCode != 401 {
		t.Fatal("密钥错应 401")
	}
	// 带工具 → 按量付费
	resp, body := post(ts, "/v1/chat/completions", "k", map[string]any{"model": "router-auto", "tools": []map[string]string{{"type": "function"}},
		"messages": []map[string]string{{"role": "system", "content": "你是助手"}, {"role": "user", "content": "这个系统的架构怎么设计"}}})
	if resp.StatusCode != 200 || api.last == nil || claude.last != nil || !strings.Contains(body, "回答") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if api.last.System == "" || resp.Header.Get("X-IMLS-Model") == "" {
		t.Fatalf("system 与响应头应透传：%+v", api.last)
	}
	// router-sub 带工具 → 400
	resp, _ = post(ts, "/v1/chat/completions", "k", map[string]any{"model": "router-sub", "tools": []map[string]string{{"type": "function"}}, "messages": []map[string]string{{"role": "user", "content": "x"}}})
	if resp.StatusCode != 400 {
		t.Fatal("router-sub 带工具应 400")
	}
	// 流式
	resp, body = post(ts, "/v1/chat/completions", "k", map[string]any{"model": "router-auto", "stream": true, "messages": []map[string]string{{"role": "user", "content": "这个系统的架构怎么设计"}}})
	if !strings.Contains(body, `"content":"流式"`) || !strings.HasSuffix(strings.TrimSpace(body), "[DONE]") {
		t.Fatalf("%s", body)
	}
	// 分段 content（含图片）
	resp, body = post(ts, "/v1/chat/completions", "k", map[string]any{"model": "router-auto", "messages": []map[string]any{{"role": "user", "content": []map[string]any{{"type": "text", "text": "看图"}, {"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64,AAAA"}}}}}})
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	resp, body = post(ts, "/v1/models", "k", nil)
	_ = resp
	req, _ := http.NewRequest("GET", ts.URL+"/v1/models", nil)
	req.Header.Set("Authorization", "Bearer k")
	r2, _ := http.DefaultClient.Do(req)
	var buf bytes.Buffer
	buf.ReadFrom(r2.Body)
	if !strings.Contains(buf.String(), "router-auto") {
		t.Fatal(buf.String())
	}
}

// 接入时写进密钥的宿主标记要进账本：Pi、OpenClaw 这些走兼容入口的宿主才分得清。
func TestHostTaggedKey(t *testing.T) {
	ts, _, api := setup(t)
	for key, want := range map[string]int{"k.pi": 200, "k.openclaw": 200, "kk.pi": 401, "k.": 401, "k.PI": 401, "x.pi": 401} {
		resp, _ := post(ts, "/v1/chat/completions", key, map[string]any{"model": "router-auto", "messages": []map[string]string{{"role": "user", "content": "这个系统的架构怎么设计"}}})
		if resp.StatusCode != want {
			t.Errorf("密钥 %q：状态 %d，想要 %d", key, resp.StatusCode, want)
		}
	}
	api.last = nil
	post(ts, "/v1/chat/completions", "k.pi", map[string]any{"model": "router-auto", "tools": []map[string]string{{"type": "function"}}, "messages": []map[string]string{{"role": "user", "content": "hi"}}})
	if api.last == nil || api.last.Host != "pi" {
		t.Fatalf("宿主没记成 pi：%+v", api.last)
	}
}

// 谁都装不下时回 400 + context_length_exceeded：宿主认这个码，会自己压缩再重试。流式也要在开始输出前就报。
func TestContextLengthExceeded(t *testing.T) {
	ts, claude, api := setup(t)
	huge := strings.Repeat("abcd", 950000)
	for _, stream := range []bool{false, true} {
		resp, body := post(ts, "/v1/chat/completions", "k", map[string]any{"model": "router-auto", "stream": stream, "messages": []map[string]string{{"role": "user", "content": huge}}})
		if resp.StatusCode != 400 || !strings.Contains(body, `"code":"context_length_exceeded"`) {
			t.Fatalf("stream=%v：%d %.200s", stream, resp.StatusCode, body)
		}
	}
	if claude.last != nil || api.last != nil {
		t.Fatal("装不下就不该转给任何模型")
	}
	// 装得下的照常
	resp, _ := post(ts, "/v1/chat/completions", "k", map[string]any{"model": "router-auto", "messages": []map[string]string{{"role": "user", "content": strings.Repeat("abcd", 300000)}}})
	if resp.StatusCode != 200 {
		t.Fatalf("30 万应装得下：%d", resp.StatusCode)
	}
}
