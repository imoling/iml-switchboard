package facade

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/lanes/api"
)

// 宿主（WorkBuddy、pi、OpenClaw）把 Switchboard 当智能体模型用时，工具调用必须两头原样过：
// 历史里的 tool_calls / tool_call_id 要转给上游，上游要调工具时要转回宿主。走真实的按量付费驱动，上游是假的 OpenAI 服务。
func setupWithUpstream(t *testing.T, upstream http.HandlerFunc) *httptest.Server {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)
	dir := t.TempDir()
	t.Setenv("IMLS_DATA_DIR", dir)
	cs, _ := config.Open()
	_ = cs.Update(func(c *config.Config) error {
		c.Settings.FacadeKey = "k"
		c.API = []config.APIAccount{{ID: "z", Provider: "zhipu", Enabled: true, AllowPaid: true, Models: map[string][]string{"low": {"glm-5.3-flash"}, "mid": {"glm-5.3"}, "high": {"glm-5.3"}}}}
		return nil
	})
	l, _ := core.OpenLedger(filepath.Join(dir, "l.db"))
	t.Cleanup(func() { l.Close() })
	r := core.NewRouter(cs, l, core.NewRegistry(0.14), core.NewQuotaManager(l, func() float64 { return 70 }))
	r.Register(api.NewAPIDriver([]api.Account{{ID: "z", Provider: "zhipu", BaseURL: up.URL, Currency: "CNY",
		APIKeyGetter: func() (string, error) { return "sk", nil },
		Models:       map[string]api.ModelPrice{"glm-5.3": {InputPerM: 8, OutputPerM: 28}, "glm-5.3-flash": {InputPerM: 1, OutputPerM: 2}}}}, up.Client()))
	mux := http.NewServeMux()
	New(r, func() string { return "k" }).Routes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// 一段典型的智能体历史：用户提问 → 助手调工具 → 工具结果 → 现在轮到模型
var agentTurn = map[string]any{
	"model":       "router-auto",
	"tool_choice": "auto",
	"tools":       []map[string]any{{"type": "function", "function": map[string]any{"name": "read_file", "parameters": map[string]any{"type": "object"}}}},
	"messages": []map[string]any{
		{"role": "system", "content": "你是编程助手"},
		{"role": "user", "content": "看看 a.go 里写了什么"},
		{"role": "assistant", "content": nil, "tool_calls": []map[string]any{{"id": "call_1", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"a.go"}`}}}},
		{"role": "tool", "tool_call_id": "call_1", "content": "package main"},
	},
}

func TestToolCallsRoundTrip(t *testing.T) {
	var got map[string]any
	ts := setupWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		if got["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			// 参数分两段到，id 和名字只在第一段
			for _, c := range []string{
				`{"model":"glm-5.3","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_2","type":"function","function":{"name":"read_file","arguments":"{\"path\":"}}]}}]}`,
				`{"model":"glm-5.3","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"b.go\"}"}}]},"finish_reason":"tool_calls"}]}`,
				`{"model":"glm-5.3","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
			} {
				fmt.Fprintf(w, "data: %s\n\n", c)
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"glm-5.3","choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_2","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"b.go\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	})

	// 非流式
	resp, body := post(ts, "/v1/chat/completions", "k", agentTurn)
	if resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	// 上游收到的历史：助手消息带 tool_calls、content 为 null；工具消息带 tool_call_id；tool_choice 原样转发
	msgs, _ := got["messages"].([]any)
	var sawAssistantCall, sawToolID bool
	for _, m := range msgs {
		mm := m.(map[string]any)
		if mm["role"] == "assistant" && mm["tool_calls"] != nil && mm["content"] == nil {
			sawAssistantCall = true
		}
		if mm["role"] == "tool" && mm["tool_call_id"] == "call_1" {
			sawToolID = true
		}
	}
	if !sawAssistantCall || !sawToolID || got["tool_choice"] != "auto" {
		t.Fatalf("上游没收到完整的工具历史：%v", got)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content   *string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct{ Name, Arguments string }
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	_ = json.Unmarshal([]byte(body), &out)
	c := out.Choices[0]
	if c.FinishReason != "tool_calls" || len(c.Message.ToolCalls) != 1 || c.Message.ToolCalls[0].ID != "call_2" || c.Message.ToolCalls[0].Function.Arguments != `{"path":"b.go"}` || c.Message.Content != nil {
		t.Fatalf("非流式没把工具调用转回宿主：%s", body)
	}

	// 流式：分段的参数要拼好，带 index，结束原因是 tool_calls
	stream := map[string]any{}
	for k, v := range agentTurn {
		stream[k] = v
	}
	stream["stream"] = true
	_, body = post(ts, "/v1/chat/completions", "k", stream)
	if !strings.Contains(body, `"tool_calls":[{"function":{"arguments":"{\"path\":\"b.go\"}","name":"read_file"},"id":"call_2","index":0,"type":"function"}]`) {
		t.Fatalf("流式没把工具调用转回宿主：%s", body)
	}
	if !strings.Contains(body, `"finish_reason":"tool_calls"`) || !strings.HasSuffix(strings.TrimSpace(body), "[DONE]") {
		t.Fatalf("流式结束原因不对：%s", body)
	}
}

// 超长错误的文字要让按文字匹配的宿主认出来
func TestContextTooLongWording(t *testing.T) {
	ts, _, _ := setup(t)
	_, body := post(ts, "/v1/chat/completions", "k", map[string]any{"model": "router-auto", "messages": []map[string]string{{"role": "user", "content": strings.Repeat("abcd", 950000)}}})
	for _, want := range []string{"maximum context length is 900000 tokens", "context length exceeded", "内容太长"} {
		if !strings.Contains(body, want) {
			t.Errorf("缺少 %q：%.300s", want, body)
		}
	}
}
