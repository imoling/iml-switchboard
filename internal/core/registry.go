package core

import (
	"strings"
	"sync"
)

// ModelInfo 是能力注册表的一条。公开目录（LiteLLM 的价格 JSON）只用来补价格，
// lane、tier、needs_proxy 这些字段任何公开目录都没有，必须自己维护，所以内置一份。
type ModelInfo struct {
	ID            string  `json:"id"`
	Provider      string  `json:"provider"`
	Display       string  `json:"display"`
	InputPerM     float64 `json:"input_per_m"`  // USD
	OutputPerM    float64 `json:"output_per_m"` // USD
	CacheReadPerM float64 `json:"cache_read_per_m"`
	PerImage      float64 `json:"per_image,omitempty"`
	PerSecond     float64 `json:"per_second,omitempty"`
	Currency      string  `json:"currency"` // 原币；价格字段已折成 USD
	Context       int     `json:"context"`
	Vision        bool    `json:"vision"`
	Tools         bool    `json:"tools"`
	NeedsProxy    bool    `json:"needs_proxy"`
}

// Registry 是模型注册表。
type Registry struct {
	mu        sync.RWMutex
	models    map[string]ModelInfo
	usdPerCNY float64
}

// NewRegistry 用内置表创建注册表。
func NewRegistry(usdPerCNY float64) *Registry {
	r := &Registry{models: map[string]ModelInfo{}, usdPerCNY: usdPerCNY}
	cny := func(v float64) float64 { return v * usdPerCNY }
	add := func(m ModelInfo) { r.models[m.ID] = m }
	// Anthropic（2026-10-18 官方页）
	add(ModelInfo{ID: "fable", Provider: "anthropic", Display: "Fable 5.1", InputPerM: 10, OutputPerM: 50, CacheReadPerM: 0.25, Currency: "USD", Context: 1000000, Vision: true, Tools: true})
	add(ModelInfo{ID: "opus", Provider: "anthropic", Display: "Opus 5.5", InputPerM: 4, OutputPerM: 20, CacheReadPerM: 0.2, Currency: "USD", Context: 1000000, Vision: true, Tools: true})
	add(ModelInfo{ID: "sonnet", Provider: "anthropic", Display: "Sonnet 5.5", InputPerM: 2, OutputPerM: 10, CacheReadPerM: 0.1, Currency: "USD", Context: 1000000, Vision: true, Tools: true})
	add(ModelInfo{ID: "haiku", Provider: "anthropic", Display: "Haiku 5.5", InputPerM: 0.10, OutputPerM: 0.50, CacheReadPerM: 0.01, Currency: "USD", Context: 200000, Vision: true, Tools: true})
	for _, a := range []string{"claude-fable-5-1:fable", "claude-opus-5-5:opus", "claude-sonnet-5-5:sonnet", "claude-haiku-5-5:haiku"} {
		p := strings.Split(a, ":")
		m := r.models[p[1]]
		m.ID = p[0]
		add(m)
	}
	// OpenAI
	add(ModelInfo{ID: "gpt-5.4", Provider: "openai", Display: "GPT-5.4", InputPerM: 2.5, OutputPerM: 15, CacheReadPerM: 0.25, Currency: "USD", Context: 400000, Vision: true, Tools: true, NeedsProxy: true})
	add(ModelInfo{ID: "gpt-5.4-mini", Provider: "openai", Display: "GPT-5.4 mini", InputPerM: 0.5, OutputPerM: 3, CacheReadPerM: 0.05, Currency: "USD", Context: 400000, Vision: true, Tools: true, NeedsProxy: true})
	add(ModelInfo{ID: "gpt-5.4-nano", Provider: "openai", Display: "GPT-5.4 nano", InputPerM: 0.2, OutputPerM: 1.25, CacheReadPerM: 0.02, Currency: "USD", Context: 400000, Vision: true, Tools: true, NeedsProxy: true})
	add(ModelInfo{ID: "gpt-6.1-sol", Provider: "openai", Display: "GPT-6.1 Sol", InputPerM: 2, OutputPerM: 10, CacheReadPerM: 0.2, Currency: "USD", Context: 400000, Vision: true, Tools: true, NeedsProxy: true})
	add(ModelInfo{ID: "gpt-6-astra", Provider: "openai", Display: "GPT-6 Astra", InputPerM: 10, OutputPerM: 40, CacheReadPerM: 1, Currency: "USD", Context: 400000, Vision: true, Tools: true, NeedsProxy: true})
	// Codex（ChatGPT 登录可用的型号；订阅不计费，价格只用于基线对比）
	add(ModelInfo{ID: "gpt-5.6-sol", Provider: "openai", Display: "GPT-5.6 Sol", InputPerM: 2.5, OutputPerM: 15, CacheReadPerM: 0.25, Currency: "USD", Context: 400000, Vision: true, Tools: true, NeedsProxy: true})
	add(ModelInfo{ID: "gpt-5.6-terra", Provider: "openai", Display: "GPT-5.6 Terra", InputPerM: 1, OutputPerM: 6, CacheReadPerM: 0.1, Currency: "USD", Context: 400000, Vision: true, Tools: true, NeedsProxy: true})
	add(ModelInfo{ID: "gpt-5.6-luna", Provider: "openai", Display: "GPT-5.6 Luna", InputPerM: 0.3, OutputPerM: 2, CacheReadPerM: 0.03, Currency: "USD", Context: 400000, Vision: true, Tools: true, NeedsProxy: true})
	// Antigravity（Google AI 订阅里可选的型号；订阅不计费，价格只用于额度权重与基线）
	add(ModelInfo{ID: "gemini-3.8-flash-high", Provider: "google", Display: "Gemini 3.8 Flash（高）", InputPerM: 0.5, OutputPerM: 3, CacheReadPerM: 0.05, Currency: "USD", Context: 1000000, Vision: true, Tools: true})
	add(ModelInfo{ID: "gemini-3.8-flash-medium", Provider: "google", Display: "Gemini 3.8 Flash（中）", InputPerM: 0.3, OutputPerM: 2, CacheReadPerM: 0.03, Currency: "USD", Context: 1000000, Vision: true, Tools: true})
	add(ModelInfo{ID: "gemini-3.8-flash-low", Provider: "google", Display: "Gemini 3.8 Flash（低）", InputPerM: 0.15, OutputPerM: 1, CacheReadPerM: 0.015, Currency: "USD", Context: 1000000, Vision: true, Tools: true})
	add(ModelInfo{ID: "claude-sonnet-4-6", Provider: "google", Display: "Sonnet 4.6（Antigravity）", InputPerM: 3, OutputPerM: 15, CacheReadPerM: 0.3, Currency: "USD", Context: 200000, Vision: true, Tools: true})
	add(ModelInfo{ID: "claude-opus-4-6-thinking", Provider: "google", Display: "Opus 4.6（Antigravity）", InputPerM: 5, OutputPerM: 25, CacheReadPerM: 0.5, Currency: "USD", Context: 200000, Vision: true, Tools: true})
	add(ModelInfo{ID: "gpt-oss-120b-medium", Provider: "google", Display: "GPT-OSS 120B", InputPerM: 0.1, OutputPerM: 0.5, Currency: "USD", Context: 128000, Tools: true})
	// 国产
	add(ModelInfo{ID: "glm-5.3", Provider: "zhipu", Display: "GLM-5.3", InputPerM: cny(8), OutputPerM: cny(28), CacheReadPerM: cny(1.6), Currency: "CNY", Context: 200000, Vision: false, Tools: true})
	add(ModelInfo{ID: "glm-5.3-flash", Provider: "zhipu", Display: "GLM-5.3-Flash", InputPerM: cny(0.8), OutputPerM: cny(2.8), CacheReadPerM: cny(0.16), Currency: "CNY", Context: 200000, Tools: true})
	add(ModelInfo{ID: "glm-5v", Provider: "zhipu", Display: "GLM-5V", InputPerM: cny(4), OutputPerM: cny(12), Currency: "CNY", Context: 128000, Vision: true, Tools: true})
	add(ModelInfo{ID: "qwen3.8-max", Provider: "dashscope", Display: "Qwen3.8-Max", InputPerM: cny(12), OutputPerM: cny(36), CacheReadPerM: cny(2.4), Currency: "CNY", Context: 256000, Vision: true, Tools: true})
	add(ModelInfo{ID: "qwen3.7-plus", Provider: "dashscope", Display: "Qwen3.7-Plus", InputPerM: cny(2), OutputPerM: cny(8), CacheReadPerM: cny(0.4), Currency: "CNY", Context: 256000, Vision: true, Tools: true})
	add(ModelInfo{ID: "qwen3.8-flash", Provider: "dashscope", Display: "Qwen3.8-Flash", InputPerM: cny(0.8), OutputPerM: cny(2.7), CacheReadPerM: cny(0.16), Currency: "CNY", Context: 256000, Vision: true, Tools: true})
	add(ModelInfo{ID: "deepseek-v4-pro", Provider: "deepseek", Display: "DeepSeek V4 Pro", InputPerM: 1.32, OutputPerM: 3.96, CacheReadPerM: 0.026, Currency: "USD", Context: 256000, Tools: true})
	add(ModelInfo{ID: "deepseek-v4.1-flash", Provider: "deepseek", Display: "DeepSeek V4.1 Flash", InputPerM: 0.30, OutputPerM: 1.20, CacheReadPerM: 0.006, Currency: "USD", Context: 256000, Tools: true})
	add(ModelInfo{ID: "kimi-k3", Provider: "moonshot", Display: "Kimi K3", InputPerM: cny(20), OutputPerM: cny(100), CacheReadPerM: cny(4), Currency: "CNY", Context: 256000, Vision: true, Tools: true})
	add(ModelInfo{ID: "minimax-m3", Provider: "minimax", Display: "MiniMax M3", InputPerM: cny(2.1), OutputPerM: cny(8.4), Currency: "CNY", Context: 200000, Tools: true})
	add(ModelInfo{ID: "anthropic/claude-sonnet-5.5", Provider: "openrouter", Display: "Sonnet 5.5", InputPerM: 2, OutputPerM: 10, CacheReadPerM: 0.2, Currency: "USD", Context: 1000000, Vision: true, Tools: true, NeedsProxy: true})
	add(ModelInfo{ID: "anthropic/claude-opus-5.5", Provider: "openrouter", Display: "Opus 5.5", InputPerM: 4, OutputPerM: 20, CacheReadPerM: 0.4, Currency: "USD", Context: 1000000, Vision: true, Tools: true, NeedsProxy: true})
	// 媒体
	add(ModelInfo{ID: "google/gemini-nano-banana-2.1", Provider: "openrouter", Display: "Nano Banana 2.1", PerImage: 0.045, Currency: "USD", NeedsProxy: true})
	add(ModelInfo{ID: "gpt-image-2.5-sunburst", Provider: "openai", Display: "GPT Image 2.5", PerImage: 0.17, Currency: "USD", NeedsProxy: true})
	add(ModelInfo{ID: "gpt-image-2.5-flare", Provider: "openai", Display: "GPT Image 2.5 Flare", PerImage: 0.04, Currency: "USD", NeedsProxy: true})
	add(ModelInfo{ID: "doubao-seedream-5-0-flash-260915", Provider: "ark", Display: "Seedream 5.0 Flash", PerImage: cny(0.12), Currency: "CNY"})
	add(ModelInfo{ID: "doubao-seedream-5-0-pro-260628", Provider: "ark", Display: "Seedream 5.0 Pro", PerImage: cny(0.30), Currency: "CNY"})
	add(ModelInfo{ID: "qwen-image-2.1-turbo", Provider: "dashscope", Display: "Qwen-Image 2.1 Turbo", PerImage: cny(0.10), Currency: "CNY"})
	add(ModelInfo{ID: "codex-imagegen", Provider: "codex", Display: "ChatGPT 订阅出图", PerImage: 0, Currency: "USD"})
	add(ModelInfo{ID: "doubao-seedance-2-0-fast-260128", Provider: "ark", Display: "Seedance 2.0 Fast", PerSecond: cny(0.80), Currency: "CNY"})
	add(ModelInfo{ID: "doubao-seedance-2-0-260128", Provider: "ark", Display: "Seedance 2.0", PerSecond: cny(0.99), Currency: "CNY"})
	add(ModelInfo{ID: "doubao-seedance-2-5", Provider: "ark", Display: "Seedance 2.5", PerSecond: cny(1.51), Currency: "CNY"})
	add(ModelInfo{ID: "bytedance/seedance-2.5", Provider: "openrouter", Display: "Seedance 2.5", PerSecond: 0.30, Currency: "USD", NeedsProxy: true})
	add(ModelInfo{ID: "google/veo-3.1-lite", Provider: "openrouter", Display: "Veo 3.1 Lite", PerSecond: 0.05, Currency: "USD", NeedsProxy: true})
	add(ModelInfo{ID: "google/veo-3.1", Provider: "openrouter", Display: "Veo 3.1", PerSecond: 0.40, Currency: "USD", NeedsProxy: true})
	add(ModelInfo{ID: "wan3.0-video", Provider: "dashscope", Display: "Wan 3.0", PerSecond: cny(0.6), Currency: "CNY"})
	return r
}

// Lookup 查模型；找不到返回 false。
func (r *Registry) Lookup(id string) (ModelInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.models[id]
	if !ok {
		// 允许带 provider 前缀或版本后缀的模糊匹配
		for k, v := range r.models {
			if strings.HasSuffix(id, k) || strings.HasPrefix(id, k) {
				return v, true
			}
		}
	}
	return m, ok
}

// Upsert 覆盖或新增（界面添加账号、覆盖表）。
func (r *Registry) Upsert(m ModelInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models[m.ID] = m
}

// Display 返回展示名。
func (r *Registry) Display(id string) string {
	if m, ok := r.Lookup(id); ok && m.Display != "" {
		return m.Display
	}
	return id
}

// TextCost 按 token 用量算费用（USD）。
func (r *Registry) TextCost(model string, in, out, cached int) float64 {
	m, ok := r.Lookup(model)
	if !ok {
		return 0
	}
	fresh := in - cached
	if fresh < 0 {
		fresh = 0
	}
	return float64(fresh)/1e6*m.InputPerM + float64(cached)/1e6*m.CacheReadPerM + float64(out)/1e6*m.OutputPerM
}

// EstimateText 估算一次文本请求（输出按输入的 0.6 倍估）。
func (r *Registry) EstimateText(model string, inTokens int) float64 {
	return r.TextCost(model, inTokens, int(float64(inTokens)*0.6)+200, 0)
}

// Baseline 是「同一请求全用 Opus 5.5」的估算，账本里的节省以它为基准。
func (r *Registry) Baseline(in, out, cached int) float64 {
	if in == 0 && out == 0 {
		return 0
	}
	return r.TextCost("opus", in, out, cached)
}

// MediaCost 按张数或秒数算费用。
func (r *Registry) MediaCost(model string, units float64) (usd float64, native float64, currency string) {
	m, ok := r.Lookup(model)
	if !ok {
		return 0, 0, "USD"
	}
	per := m.PerImage
	if m.PerSecond > 0 {
		per = m.PerSecond
	}
	usd = per * units
	native = usd
	if m.Currency == "CNY" && r.usdPerCNY > 0 {
		native = usd / r.usdPerCNY
	}
	return usd, native, m.Currency
}
