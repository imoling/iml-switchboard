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
	if t.Kind == core.KindImage {
		return &core.Result{Path: "/tmp/imls-test/红杏.png", Model: tg.Model}, nil
	}
	if len(t.Tools) > 0 && strings.Contains(t.Prompt, "用工具") && (len(t.Messages) == 0 || t.Messages[len(t.Messages)-1].Role != "tool") {
		return &core.Result{Model: tg.Model, FinishReason: "tool_calls", ToolCalls: json.RawMessage(`[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"合肥\"}"}}]`)}, nil
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

// WorkBuddy 这类宿主每轮都带工具，用户说「生成图片」时要走用户配好的媒体渠道，而不是只回文字让宿主去调自己收费的出图工具
func TestToolsStillGetImageFromSwitchboard(t *testing.T) {
	ts, _, _ := setup(t)
	media := &fake{id: core.LaneMedia}
	// 复用 setup 里的路由器：通过门面拿不到，直接再建一个带媒体渠道的
	dir := t.TempDir()
	t.Setenv("IMLS_DATA_DIR", dir)
	cs, _ := config.Open()
	_ = cs.Update(func(c *config.Config) error {
		c.Settings.FacadeKey = "k"
		c.API = []config.APIAccount{{ID: "z", Provider: "zhipu", Enabled: true, AllowPaid: true, Models: map[string][]string{"low": {"glm-5.3-flash"}, "mid": {"glm-5.3"}, "high": {"glm-5.3"}}}}
		c.Media.Image = map[string]config.MediaTarget{"default": {Provider: "zhipu", Model: "cogview-x"}}
		return nil
	})
	l, _ := core.OpenLedger(filepath.Join(dir, "l.db"))
	t.Cleanup(func() { l.Close() })
	r := core.NewRouter(cs, l, core.NewRegistry(0.14), core.NewQuotaManager(l, func() float64 { return 70 }))
	text := &fake{id: core.LaneAPI}
	r.Register(text)
	r.Register(media)
	mux := http.NewServeMux()
	srv := New(r, func() string { return "k" })
	srv.MediaURL = func(p string) string { return "http://127.0.0.1:18081/media/" + filepath.Base(p) }
	srv.Routes(mux)
	ts.Close()
	ts = httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	prompt := `<system-reminder data-role="user-context">` + strings.Repeat("工作区信息 ", 200) + "</system-reminder>\n生成图片：一枝红杏出墙来"
	resp, body := post(ts, "/v1/chat/completions", "k", map[string]any{
		"model": "router-auto", "tools": []map[string]any{{"type": "function", "function": map[string]any{"name": "ImageGen"}}},
		"messages": []map[string]any{{"role": "user", "content": prompt}},
	})
	if resp.StatusCode != 200 || media.last == nil || text.last != nil {
		t.Fatalf("带工具的一句出图请求应走媒体渠道：%d %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "![](http://127.0.0.1:18081/media/红杏.png)") || !strings.Contains(body, "已生成 /tmp/imls-test/红杏.png") {
		t.Fatalf("要把产物路径回给宿主：%s", body)
	}
}

// 宿主在模型列表里点名某个型号就按它来；列表里有账号配好的型号
func TestModelPinAndList(t *testing.T) {
	ts, _, api := setup(t)
	resp, body := post(ts, "/v1/chat/completions", "k", map[string]any{"model": "glm-5.3", "messages": []map[string]any{{"role": "user", "content": "你好"}}})
	if resp.StatusCode != 200 || api.last == nil || !strings.Contains(body, `"model":"glm-5.3"`) {
		t.Fatalf("点名型号应走它：%d %s", resp.StatusCode, body)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/v1/models", nil)
	req.Header.Set("Authorization", "Bearer k")
	r2, _ := http.DefaultClient.Do(req)
	var buf bytes.Buffer
	buf.ReadFrom(r2.Body)
	if !strings.Contains(buf.String(), `"id":"glm-5.3"`) || !strings.Contains(buf.String(), `"id":"router-auto"`) {
		t.Fatalf("模型列表应含账号型号：%s", buf.String())
	}
}

func postAnth(ts *httptest.Server, key string, body any) (*http.Response, string) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", ts.URL+"/v1/messages", bytes.NewReader(b))
	req.Header.Set("x-api-key", key)
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return resp, buf.String()
}

// Anthropic 协议：工具两头转换、工具结果变成 tool 消息、用 x-api-key 鉴权
func TestAnthropicMessages(t *testing.T) {
	ts, _, api := setup(t)
	tools := []map[string]any{{"name": "get_weather", "description": "查天气", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"city": map[string]string{"type": "string"}}}}}
	resp, body := postAnth(ts, "k", map[string]any{"model": "router-mid", "max_tokens": 100, "system": "你是助手", "tools": tools,
		"messages": []map[string]any{{"role": "user", "content": "合肥天气？用工具查"}}})
	if resp.StatusCode != 200 || api.last == nil {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"type":"tool_use"`) || !strings.Contains(body, `"name":"get_weather"`) || !strings.Contains(body, `"city":"合肥"`) || !strings.Contains(body, `"stop_reason":"tool_use"`) {
		t.Fatalf("应回 tool_use 块：%s", body)
	}
	if !strings.Contains(string(api.last.Tools), `"parameters"`) || api.last.System != "你是助手" || api.last.Tier != core.TierMid {
		t.Fatalf("工具应转成 OpenAI 格式、系统提示与档位要带上：%s %q %s", api.last.Tools, api.last.System, api.last.Tier)
	}
	// 第二轮：助手的 tool_use 和用户的 tool_result 要变成 OpenAI 的 tool_calls / tool 消息
	resp, body = postAnth(ts, "k", map[string]any{"model": "router-auto", "max_tokens": 100, "tools": tools, "messages": []map[string]any{
		{"role": "user", "content": "合肥天气？用工具查"},
		{"role": "assistant", "content": []map[string]any{{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": map[string]string{"city": "合肥"}}}},
		{"role": "user", "content": []map[string]any{{"type": "tool_result", "tool_use_id": "toolu_1", "content": "晴 22 度"}}},
	}})
	if resp.StatusCode != 200 || !strings.Contains(body, `"type":"text"`) {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	msgs := api.last.Messages
	if len(msgs) != 3 || msgs[1].Role != "assistant" || !strings.Contains(string(msgs[1].ToolCalls), `"toolu_1"`) || msgs[2].Role != "tool" || msgs[2].ToolCallID != "toolu_1" || msgs[2].Content != "晴 22 度" {
		t.Fatalf("消息转换不对：%+v", msgs)
	}
	if resp, _ := postAnth(ts, "wrong", map[string]any{"model": "router-auto", "messages": []map[string]any{{"role": "user", "content": "x"}}}); resp.StatusCode != 401 {
		t.Fatalf("密钥不对应 401，得到 %d", resp.StatusCode)
	}
}

// 流式：事件顺序是 message_start → 文本块 → 工具块 → message_delta → message_stop
func TestAnthropicStream(t *testing.T) {
	ts, _, _ := setup(t)
	resp, body := postAnth(ts, "k", map[string]any{"model": "router-auto", "max_tokens": 50, "stream": true,
		"tools":    []map[string]any{{"name": "get_weather", "input_schema": map[string]any{"type": "object"}}},
		"messages": []map[string]any{{"role": "user", "content": "合肥天气？用工具查"}}})
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	order := []string{"event: message_start", "event: content_block_start", `"type":"tool_use"`, "input_json_delta", "event: content_block_stop", `"stop_reason":"tool_use"`, "event: message_stop"}
	pos := 0
	for _, want := range order {
		i := strings.Index(body[pos:], want)
		if i < 0 {
			t.Fatalf("缺少或顺序不对：%s\n%s", want, body)
		}
		pos += i
	}
	// 纯文字：流式文本块
	_, body = postAnth(ts, "k", map[string]any{"model": "router-auto", "max_tokens": 50, "stream": true, "messages": []map[string]any{{"role": "user", "content": "你好"}}})
	if !strings.Contains(body, `"text_delta"`) || !strings.Contains(body, `"stop_reason":"end_turn"`) {
		t.Fatalf("%s", body)
	}
	// 估算 token
	req, _ := http.NewRequest("POST", ts.URL+"/v1/messages/count_tokens", strings.NewReader(`{"model":"router-auto","messages":[{"role":"user","content":"你好世界"}]}`))
	req.Header.Set("x-api-key", "k")
	r2, _ := http.DefaultClient.Do(req)
	var buf bytes.Buffer
	buf.ReadFrom(r2.Body)
	if r2.StatusCode != 200 || !strings.Contains(buf.String(), "input_tokens") {
		t.Fatalf("%d %s", r2.StatusCode, buf.String())
	}
	// Claude 自家的模型名按档位理解
	task := &core.Task{}
	applyModelName(task, "claude-haiku-5-5")
	if task.Tier != core.TierLow || task.Model != "" {
		t.Fatalf("haiku 应当低档：%+v", task)
	}
	task = &core.Task{}
	applyModelName(task, "gpt-5.6-luna")
	if task.Tier != core.TierLow || task.Model != "" {
		t.Fatalf("gpt 小型号应当低档：%+v", task)
	}
	task = &core.Task{}
	applyModelName(task, "winwork-snt-5.5")
	if task.Model != "winwork-snt-5.5" {
		t.Fatalf("其它名字当点名：%+v", task)
	}
}

func postResp(ts *httptest.Server, key string, body any) (*http.Response, string) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", ts.URL+"/v1/responses", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, _ := http.DefaultClient.Do(req)
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return resp, buf.String()
}

// Responses 协议（Codex）：instructions 是系统提示，function_call / function_call_output 两头转换，流式事件齐全
func TestResponsesAPI(t *testing.T) {
	ts, _, api := setup(t)
	tools := []map[string]any{{"type": "function", "name": "get_weather", "description": "查天气", "parameters": map[string]any{"type": "object"}}, {"type": "local_shell"}}
	resp, body := postResp(ts, "k", map[string]any{"model": "router-mid", "instructions": "你是助手", "tools": tools, "input": "合肥天气？用工具查"})
	if resp.StatusCode != 200 || api.last == nil || !strings.Contains(body, `"type":"function_call"`) || !strings.Contains(body, `"call_id":"call_1"`) || !strings.Contains(body, `"object":"response"`) {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if api.last.System != "你是助手" || api.last.Tier != core.TierMid || strings.Contains(string(api.last.Tools), "local_shell") {
		t.Fatalf("系统提示、档位要带上，内置工具要去掉：%q %s %s", api.last.System, api.last.Tier, api.last.Tools)
	}
	resp, body = postResp(ts, "k", map[string]any{"model": "router-auto", "tools": tools, "input": []map[string]any{
		{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "合肥天气？用工具查"}}},
		{"type": "function_call", "call_id": "call_9", "name": "get_weather", "arguments": `{"city":"合肥"}`},
		{"type": "function_call_output", "call_id": "call_9", "output": "晴 22 度"},
	}})
	if resp.StatusCode != 200 || !strings.Contains(body, `"output_text"`) {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	msgs := api.last.Messages
	if len(msgs) != 3 || msgs[1].Role != "assistant" || !strings.Contains(string(msgs[1].ToolCalls), `"call_9"`) || msgs[2].Role != "tool" || msgs[2].ToolCallID != "call_9" || msgs[2].Content != "晴 22 度" {
		t.Fatalf("消息转换不对：%+v", msgs)
	}
	resp, body = postResp(ts, "k", map[string]any{"model": "router-auto", "stream": true, "tools": tools, "input": "合肥天气？用工具查"})
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	pos := 0
	for _, want := range []string{"event: response.created", "event: response.output_item.added", `"type":"function_call"`, "event: response.function_call_arguments.done", "event: response.output_item.done", "event: response.completed", `"status":"completed"`} {
		i := strings.Index(body[pos:], want)
		if i < 0 {
			t.Fatalf("缺少或顺序不对：%s\n%s", want, body)
		}
		pos += i
	}
	_, body = postResp(ts, "k", map[string]any{"model": "router-auto", "stream": true, "input": "你好"})
	if !strings.Contains(body, "response.output_text.delta") || !strings.Contains(body, "response.output_text.done") {
		t.Fatalf("%s", body)
	}
}
