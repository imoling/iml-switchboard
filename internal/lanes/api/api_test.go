package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/core"
)

func testAccount(base string) Account {
	return Account{ID: "t1", Provider: "zhipu", BaseURL: base, Currency: "CNY",
		APIKeyGetter: func() (string, error) { return "sk-test", nil },
		Models:       map[string]ModelPrice{"glm-5.3": {InputPerM: 8, OutputPerM: 28}, "glm-5.3-flash": {InputPerM: 0.8, OutputPerM: 2.8, CachedPerM: 0.4}}}
}

func TestChatNonStream(t *testing.T) {
	var got chatRequest
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v4/chat/completions" {
			t.Errorf("路径不对：%s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, `{"id":"x","model":"glm-5.3-flash","choices":[{"message":{"role":"assistant","content":"你好"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":500,"prompt_tokens_details":{"cached_tokens":200}}}`)
	}))
	defer srv.Close()
	d := NewAPIDriver([]Account{testAccount(srv.URL + "/v4")}, srv.Client())
	task := &core.Task{Kind: core.KindText, Prompt: "hi", System: "你是助手", MaxTokens: 100}
	res, err := d.Run(context.Background(), task, core.Target{Lane: core.LaneAPI, Provider: "zhipu", Model: "glm-5.3-flash"}, nil)
	if err != nil {
		t.Fatalf("Run 失败：%v", err)
	}
	if auth != "Bearer sk-test" {
		t.Errorf("认证头不对：%q", auth)
	}
	if len(got.Messages) != 2 || got.Messages[0].Role != "system" || got.MaxTokens != 100 || got.Stream {
		t.Errorf("请求体不对：%+v", got)
	}
	if res.Text != "你好" || res.Usage.InputTokens != 1000 || res.Usage.OutputTokens != 500 || res.Usage.CacheReadTokens != 200 {
		t.Errorf("结果不对：%+v", res)
	}
	// 费用：800*0.8/1e6 + 200*0.4/1e6 + 500*2.8/1e6 = 0.00064+0.00008+0.0014 = 0.00212 元
	if res.Usage.Currency != "CNY" || fmt.Sprintf("%.5f", res.Usage.CostNative) != "0.00212" {
		t.Errorf("人民币费用不对：%+v", res.Usage)
	}
	if fmt.Sprintf("%.7f", res.Usage.CostUSD) != fmt.Sprintf("%.7f", 0.00212/USDPerCNY) {
		t.Errorf("美元折算不对：%+v", res.Usage)
	}
}

func TestChatStreamWithToolsAndImages(t *testing.T) {
	var got map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, l := range []string{
			`data: {"model":"m","choices":[{"delta":{"content":"你"}}]}`,
			`data: {"choices":[{"delta":{"content":"好"}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"ci"}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ty\":\"北京\"}"}}]},"finish_reason":"tool_calls"}]}`,
			`data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
			`data: [DONE]`,
		} {
			fmt.Fprintln(w, l)
			fmt.Fprintln(w)
		}
	}))
	defer srv.Close()
	img := filepath.Join(t.TempDir(), "a.png")
	os.WriteFile(img, []byte("\x89PNG\r\n\x1a\nxxxx"), 0o644)
	d := NewAPIDriver([]Account{testAccount(srv.URL)}, srv.Client())
	task := &core.Task{Kind: core.KindText, Prompt: "看图", Stream: true,
		Messages: []core.Message{{Role: "user", Content: "看图", Images: []string{img}}},
		Tools:    json.RawMessage(`[{"type":"function","function":{"name":"get_weather"}}]`)}
	var deltas []string
	res, err := d.Run(context.Background(), task, core.Target{AccountID: "t1", Model: "glm-5.3"}, func(e core.Event) {
		if e.Type == "text_delta" {
			deltas = append(deltas, e.Text)
		}
	})
	if err != nil {
		t.Fatalf("Run 失败：%v", err)
	}
	if strings.Join(deltas, "") != "你好" || res.Text != "你好" {
		t.Errorf("流式文本不对：%v %q", deltas, res.Text)
	}
	if res.Usage.InputTokens != 10 || res.Usage.OutputTokens != 5 {
		t.Errorf("流式 usage 不对：%+v", res.Usage)
	}
	if got["stream"] != true || got["tools"] == nil {
		t.Errorf("请求体应为流式并带 tools：%v", got)
	}
	so, _ := got["stream_options"].(map[string]interface{})
	if so["include_usage"] != true {
		t.Errorf("应请求 include_usage：%v", got["stream_options"])
	}
	msgs := got["messages"].([]interface{})
	parts := msgs[0].(map[string]interface{})["content"].([]interface{})
	if len(parts) != 2 {
		t.Fatalf("图片消息应有两段：%v", parts)
	}
	u := parts[1].(map[string]interface{})["image_url"].(map[string]interface{})["url"].(string)
	if !strings.HasPrefix(u, "data:image/png;base64,") {
		t.Errorf("图片应转成 data URL：%s", u[:40])
	}
	var rawMsg struct {
		ToolCalls []toolCall `json:"tool_calls"`
	}
	json.Unmarshal(res.Raw, &rawMsg)
	if len(rawMsg.ToolCalls) != 1 || rawMsg.ToolCalls[0].Function.Arguments != `{"city":"北京"}` || rawMsg.ToolCalls[0].ID != "c1" {
		t.Errorf("tool_calls 没拼好：%s", res.Raw)
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		status    int
		body      string
		wantRL    bool
		wantRetry bool
		wantIn    string
	}{
		{401, `{"error":{"message":"invalid api key"}}`, false, false, "验证失败"},
		{403, ``, false, false, "验证失败"},
		{402, ``, false, true, "余额不足"},
		{400, `{"error":{"message":"insufficient balance"}}`, false, true, "余额不足"},
		{429, ``, true, false, ""},
		{500, `oops`, false, true, "服务端出错"},
		{400, `{"error":{"message":"bad param"}}`, false, false, "返回 400"},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			fmt.Fprint(w, c.body)
		}))
		d := NewAPIDriver([]Account{testAccount(srv.URL)}, srv.Client())
		_, err := d.Run(context.Background(), &core.Task{Prompt: "x"}, core.Target{AccountID: "t1", Model: "glm-5.3"}, nil)
		srv.Close()
		var rl *core.ErrRateLimited
		var pe *core.ErrProvider
		if c.wantRL {
			if !errors.As(err, &rl) {
				t.Errorf("%d 应为 ErrRateLimited：%v", c.status, err)
			}
			continue
		}
		if !errors.As(err, &pe) {
			t.Fatalf("%d 应为 ErrProvider：%v", c.status, err)
		}
		if pe.Retryable != c.wantRetry || !strings.Contains(pe.Summary, c.wantIn) || !strings.HasPrefix(pe.Summary, "zhipu ") || pe.Raw == "" {
			t.Errorf("%d 映射不对：%+v", c.status, pe)
		}
		if strings.Contains(pe.Summary, "\n") {
			t.Errorf("摘要必须是一行：%q", pe.Summary)
		}
	}
	// 网络错误
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	d := NewAPIDriver([]Account{testAccount(url)}, nil)
	_, err := d.Run(context.Background(), &core.Task{Prompt: "x"}, core.Target{AccountID: "t1", Model: "glm-5.3"}, nil)
	var pe *core.ErrProvider
	if !errors.As(err, &pe) || !strings.Contains(pe.Summary, "连接失败：检查网络代理") || !pe.Retryable {
		t.Errorf("网络错误映射不对：%v", err)
	}
	// 没有密钥
	a := testAccount(url)
	a.APIKeyGetter = nil
	d = NewAPIDriver([]Account{a}, nil)
	_, err = d.Run(context.Background(), &core.Task{Prompt: "x"}, core.Target{AccountID: "t1", Model: "glm-5.3"}, nil)
	if !errors.As(err, &pe) || !strings.Contains(pe.Summary, "没有配置密钥") {
		t.Errorf("缺密钥映射不对：%v", err)
	}
	if h := d.Health(context.Background()); h.Status != "warn" {
		t.Errorf("缺密钥 Health 应为 warn：%+v", h)
	}
}

func TestImagesGenerations(t *testing.T) {
	var got imagesRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Errorf("路径不对：%s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, `{"created":1,"data":[{"b64_json":"iVBORw0KGgo="}]}`)
	}))
	defer srv.Close()
	a := testAccount(srv.URL + "/v1")
	a.Provider = "openai"
	d := NewAPIDriver([]Account{a}, srv.Client())
	out := filepath.Join(t.TempDir(), "sub", "x.png")
	res, err := d.GenerateImage(context.Background(), a, ImageGenRequest{Model: "gpt-image-2.5-low", Prompt: "猫", Size: "1024x1024", Quality: "low", OutPath: out})
	if err != nil {
		t.Fatalf("出图失败：%v", err)
	}
	if got.ResponseFormat != "" || got.Quality != "low" || got.N != 1 {
		t.Errorf("gpt-image 请求体不对：%+v", got)
	}
	b, _ := os.ReadFile(out)
	if string(b) != "\x89PNG\r\n\x1a\n" || res.Path != out {
		t.Errorf("落盘不对：%q %+v", b, res)
	}
	// 非 gpt-image 模型要显式要 b64
	_, _ = d.GenerateImage(context.Background(), a, ImageGenRequest{Model: "flux-1", Prompt: "x"})
	if got.ResponseFormat != "b64_json" {
		t.Errorf("非 gpt-image 应要求 b64_json：%+v", got)
	}
}

func TestPresetsAndVerify(t *testing.T) {
	if len(Presets) != 10 {
		t.Errorf("应有 10 个预设，得到 %d", len(Presets))
	}
	for _, p := range Presets {
		if p.BaseURL == "" || p.Currency == "" || p.Name == "" {
			t.Errorf("预设字段不全：%+v", p)
		}
	}
	zp, _ := PresetByID("zhipu")
	if zp.Models["glm-5.3"].OutputPerM != 28 || zp.Currency != "CNY" {
		t.Errorf("智谱价格不对：%+v", zp)
	}
	or, _ := PresetByID("openrouter")
	if !or.NeedsProxy {
		t.Errorf("openrouter 应标记需要代理")
	}
	// /models 成功
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/v1/models":
			if r.Header.Get("Authorization") != "Bearer k" {
				w.WriteHeader(401)
				return
			}
			fmt.Fprint(w, `{"data":[]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	p := Preset{ID: "x", Name: "X", BaseURL: srv.URL + "/v1", ProbeModel: "m"}
	if err := p.Verify(context.Background(), srv.Client(), "k"); err != nil {
		t.Errorf("Verify 应通过：%v", err)
	}
	err := p.Verify(context.Background(), srv.Client(), "bad")
	var pe *core.ErrProvider
	if !errors.As(err, &pe) || !strings.Contains(pe.Summary, "X 验证失败") {
		t.Errorf("错误密钥应报验证失败：%v", err)
	}
	// /models 不存在时退回最小 chat
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			b, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(b), `"max_tokens":1`) {
				t.Errorf("最小 chat 应 max_tokens=1：%s", b)
			}
			fmt.Fprint(w, `{"choices":[]}`)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv2.Close()
	p2 := Preset{ID: "y", Name: "Y", BaseURL: srv2.URL + "/v1", ProbeModel: "m"}
	if err := p2.Verify(context.Background(), srv2.Client(), "k"); err != nil {
		t.Errorf("退回 chat 校验应通过：%v", err)
	}
}

func TestCostPrefixMatchAndUSD(t *testing.T) {
	a := Account{Provider: "deepseek", Currency: "USD", Models: map[string]ModelPrice{"deepseek-v4-pro": {InputPerM: 1.32, OutputPerM: 3.96}}}
	native, usd, cur := a.Cost("deepseek-v4-pro-20260101", 1_000_000, 1_000_000, 0)
	if cur != "USD" || native != usd || fmt.Sprintf("%.2f", usd) != "5.28" {
		t.Errorf("美元费用不对：%v %v %s", native, usd, cur)
	}
	if _, usd, _ := a.Cost("unknown", 100, 100, 0); usd != 0 {
		t.Errorf("未知模型费用应为 0")
	}
}
