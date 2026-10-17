package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Preset 是内置供应商预设：地址、货币、价格表。
type Preset struct {
	ID         string
	Name       string
	BaseURL    string
	Currency   string
	NeedsProxy bool
	Models     map[string]ModelPrice
	// ProbeModel 是 /models 不可用时做最小 chat 校验用的模型。
	ProbeModel string
	// Headers 是额外请求头。
	Headers map[string]string
}

// Presets 是内置供应商列表（按推荐顺序）。
var Presets = []Preset{
	{ID: "zhipu", Name: "智谱", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Currency: "CNY", ProbeModel: "glm-5.3-flash",
		Models: map[string]ModelPrice{"glm-5.3": {InputPerM: 8, OutputPerM: 28}, "glm-5.3-flash": {InputPerM: 0.8, OutputPerM: 2.8}}},
	{ID: "dashscope", Name: "阿里云百炼", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Currency: "CNY", ProbeModel: "qwen3.8-flash",
		Models: map[string]ModelPrice{"qwen3.8-max": {InputPerM: 12, OutputPerM: 36}, "qwen3.7-plus": {InputPerM: 2, OutputPerM: 8}, "qwen3.8-flash": {InputPerM: 0.8, OutputPerM: 2.7}}},
	{ID: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", Currency: "USD", ProbeModel: "deepseek-v4.1-flash",
		Models: map[string]ModelPrice{"deepseek-v4-pro": {InputPerM: 1.32, OutputPerM: 3.96}, "deepseek-v4.1-flash": {InputPerM: 0.30, OutputPerM: 1.20}}},
	{ID: "moonshot", Name: "月之暗面", BaseURL: "https://api.moonshot.cn/v1", Currency: "CNY", ProbeModel: "kimi-k3",
		Models: map[string]ModelPrice{"kimi-k3": {InputPerM: 20, OutputPerM: 100}}},
	{ID: "minimax", Name: "MiniMax", BaseURL: "https://api.minimaxi.com/v1", Currency: "CNY", ProbeModel: "minimax-m3",
		Models: map[string]ModelPrice{"minimax-m3": {InputPerM: 2.1, OutputPerM: 8.4}}},
	{ID: "openrouter", Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", Currency: "USD", NeedsProxy: true, ProbeModel: "anthropic/claude-sonnet-5.5",
		Headers: map[string]string{"HTTP-Referer": "https://github.com/imoling/iml-switchboard", "X-Title": "iML Switchboard"},
		Models: map[string]ModelPrice{"anthropic/claude-sonnet-5.5": {InputPerM: 2, OutputPerM: 10}, "anthropic/claude-opus-5.5": {InputPerM: 4, OutputPerM: 20}}},
	// Anthropic 走它的 OpenAI 兼容端点 /v1/chat/completions。
	{ID: "anthropic", Name: "Anthropic", BaseURL: "https://api.anthropic.com/v1", Currency: "USD", NeedsProxy: true, ProbeModel: "claude-haiku-5-5",
		Models: map[string]ModelPrice{"claude-haiku-5-5": {InputPerM: 0.10, OutputPerM: 0.50}, "claude-sonnet-5-5": {InputPerM: 2, OutputPerM: 10}, "claude-opus-5-5": {InputPerM: 4, OutputPerM: 20}}},
	{ID: "302ai", Name: "302.AI", BaseURL: "https://api.302.ai/v1", Currency: "USD", ProbeModel: "gpt-5.4-mini", Models: map[string]ModelPrice{}},
}

// PresetByID 按 ID 找预设。
func PresetByID(id string) (Preset, bool) {
	for _, p := range Presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// Account 用预设生成一个账号，密钥由 getter 提供。
func (p Preset) Account(accountID string, getter func() (string, error)) Account {
	if accountID == "" {
		accountID = p.ID
	}
	models := make(map[string]ModelPrice, len(p.Models))
	for k, v := range p.Models {
		models[k] = v
	}
	return Account{ID: accountID, Provider: p.ID, BaseURL: p.BaseURL, APIKeyGetter: getter, Currency: p.Currency, Models: models, NeedsProxy: p.NeedsProxy, Headers: p.Headers}
}

// Verify 用给定密钥校验：先 GET /models；接口不存在（404/405）时再发一次 max_tokens=1 的最小 chat。
// 返回 nil 表示密钥可用；错误是一行人话。
func (p Preset) Verify(ctx context.Context, client *http.Client, key string) error {
	if client == nil {
		client = http.DefaultClient
	}
	acct := p.Account("", func() (string, error) { return key, nil })
	req, err := acct.newRequest(ctx, http.MethodGet, endpoint(p.BaseURL, "/models"), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return ClassifyNetError(p.Name, err)
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	resp.Body.Close()
	switch {
	case resp.StatusCode/100 == 2:
		return nil
	case resp.StatusCode == 404 || resp.StatusCode == 405 || resp.StatusCode == 400:
		// 继续用最小 chat 校验
	default:
		return ClassifyHTTPError(p.Name, resp.StatusCode, b)
	}
	body, _ := json.Marshal(map[string]interface{}{
		"model":      p.ProbeModel,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		"max_tokens": 1,
	})
	req, err = acct.newRequest(ctx, http.MethodPost, endpoint(p.BaseURL, "/chat/completions"), body)
	if err != nil {
		return err
	}
	resp, err = client.Do(req)
	if err != nil {
		return ClassifyNetError(p.Name, err)
	}
	b, _ = io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		return nil
	}
	// 模型不存在但密钥有效（400 且提示 model）也算验证通过。
	if resp.StatusCode == 400 && strings.Contains(strings.ToLower(string(b)), "model") {
		return nil
	}
	return ClassifyHTTPError(p.Name, resp.StatusCode, b)
}
