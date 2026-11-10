package core

import (
	"sort"
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
	// Free 表示平台当前标价为 0（刊例价另算）；MediaKind 标媒体型号的种类，免费媒体型号靠它识别
	Free      bool   `json:"free,omitempty"`
	MediaKind string `json:"media_kind,omitempty"`
}

// Registry 是模型注册表。
type Registry struct {
	mu        sync.RWMutex
	models    map[string]ModelInfo
	usdPerCNY float64
}

// PriceTableVersion 是内置价目表的版本：改了价就改它，启动时按新价把历史记录的应用原价重算一次。
const PriceTableVersion = "2026-11-05b" // b：WorkBuddy 按积分折算

// NewRegistry 用内置表创建注册表。
func NewRegistry(usdPerCNY float64) *Registry {
	r := &Registry{models: map[string]ModelInfo{}, usdPerCNY: usdPerCNY}
	cny := func(v float64) float64 { return v * usdPerCNY }
	add := func(m ModelInfo) { r.models[m.ID] = m }
	// Anthropic（2026-10-18 官方页）
	add(ModelInfo{ID: "fable", Provider: "anthropic", Display: "Fable 5.1", InputPerM: 10, OutputPerM: 50, CacheReadPerM: 0.25, Currency: "USD", Context: 1000000, Vision: true, Tools: true})
	add(ModelInfo{ID: "opus", Provider: "anthropic", Display: "Opus 5.5", InputPerM: 4, OutputPerM: 20, CacheReadPerM: 0.2, Currency: "USD", Context: 1000000, Vision: true, Tools: true})
	add(ModelInfo{ID: "sonnet", Provider: "anthropic", Display: "Sonnet 5.5", InputPerM: 2, OutputPerM: 10, CacheReadPerM: 0.1, Currency: "USD", Context: 1000000, Vision: true, Tools: true})
	add(ModelInfo{ID: "haiku", Provider: "anthropic", Display: "Haiku 5.5", InputPerM: 0.10, OutputPerM: 0.50, CacheReadPerM: 0.01, Currency: "USD", Context: 1000000, Vision: true, Tools: true})
	// WinWork（讯飞）网关里的型号：额度算在 WinWork 账上，对用户不计费，所以标成免费，排在收费型号前面
	for _, w := range WinWorkModels {
		add(ModelInfo{ID: w.ID, Provider: "winwork", Display: "WinWork " + w.Name, Currency: "CNY", Tools: true, Free: true, Vision: w.Vision, Context: w.Context})
	}
	for _, a := range []string{"claude-fable-5-1:fable", "claude-opus-5-5:opus", "claude-sonnet-5-5:sonnet", "claude-haiku-5-5:haiku"} {
		p := strings.Split(a, ":")
		m := r.models[p[1]]
		m.ID = p[0]
		add(m)
	}
	// OpenAI（2026-11-05 官方价目页 developers.openai.com/api/docs/pricing，标准档、272K 以内）。
	// Token Factory 网关的账单按这套价算，抽了 gpt-5.6-luna、gpt-6-luna 三条对过，分毫不差
	oai := func(id, display string, in, cached, out float64, ctx int) {
		add(ModelInfo{ID: id, Provider: "openai", Display: display, InputPerM: in, OutputPerM: out, CacheReadPerM: cached, Currency: "USD", Context: ctx, Vision: true, Tools: true, NeedsProxy: true})
	}
	// 窗口按 2026-11-10 官方型号页：6.1、6、5.6 系列都是 1,050,000；5.4 及以下没核实，沿用 400K
	oai("gpt-6.1-sol", "GPT-6.1 Sol", 2, 0.10, 10, 1050000)
	oai("gpt-6.1-terra", "GPT-6.1 Terra", 1, 0.05, 5, 1050000)
	oai("gpt-6-sol", "GPT-6 Sol", 2, 0.20, 10, 1050000)
	oai("gpt-6-luna", "GPT-6 Luna", 0.10, 0.01, 0.50, 1050000)
	oai("gpt-6-astra", "GPT-6 Astra", 10, 1, 50, 1050000)
	oai("gpt-5.6-sol", "GPT-5.6 Sol", 4, 0.40, 20, 1050000)
	oai("gpt-5.6-terra", "GPT-5.6 Terra", 2, 0.20, 12, 1050000)
	oai("gpt-5.6-luna", "GPT-5.6 Luna", 0.20, 0.02, 1.20, 1050000)
	oai("gpt-5.5", "GPT-5.5", 5, 0.50, 30, 1050000)
	oai("gpt-5.5-pro", "GPT-5.5 Pro", 30, 30, 180, 1050000) // 官方没有缓存价：缓存按原价算
	oai("gpt-5.4", "GPT-5.4", 2.5, 0.25, 15, 400000)
	oai("gpt-5.4-mini", "GPT-5.4 mini", 0.75, 0.075, 4.5, 400000)
	oai("gpt-5.4-nano", "GPT-5.4 nano", 0.2, 0.02, 1.25, 400000) // 2026-10 录入，这次官方页没列，沿用
	oai("gpt-5.3-codex", "GPT-5.3 Codex", 1.75, 0.175, 14, 400000)
	oai("gpt-5.2", "GPT-5.2", 1.75, 0.175, 14, 400000)
	oai("gpt-5.2-pro", "GPT-5.2 Pro", 21, 21, 168, 400000)
	// 同一型号的别名和带日期的快照：价格跟本体一样（网关报的名字里写明了是哪个型号）
	for _, a := range []string{"gpt-6:gpt-6-astra", "gpt-5.6:gpt-5.6-sol", "gpt-5.4-2026-03-05:gpt-5.4", "gpt-5.2-2025-12-11:gpt-5.2", "gpt-5.2-pro-2025-12-11:gpt-5.2-pro"} {
		p := strings.Split(a, ":")
		m := r.models[p[1]]
		m.ID = p[0]
		add(m)
	}
	// 官方没公布价格的，按最接近的型号估（2026-11-05）：
	// codex-auto-review 是 Codex 自动审批用的内部名字，Token Factory 账单按 gpt-5.6-luna 收（399 输入 + 103.8K 缓存 + 292 输出 = $0.002506）；
	// gpt-5.3-codex-spark 是 gpt-5.3-codex 的快速版，三方价目站都按本体价列；
	// gpt-5.2-chat-latest 按 gpt-5.2；gpt-6-chat-thinking 按同代的 gpt-6-sol
	for _, a := range []string{"codex-auto-review:gpt-5.6-luna", "gpt-5.3-codex-spark:gpt-5.3-codex", "gpt-5.2-chat-latest:gpt-5.2", "gpt-6-chat-thinking:gpt-6-sol"} {
		p := strings.Split(a, ":")
		m := r.models[p[1]]
		m.ID, m.Display = p[0], m.Display+"（估）"
		add(m)
	}
	r.models["codex-auto-review"] = func(m ModelInfo) ModelInfo { m.Display = "Codex 自动审批"; return m }(r.models["codex-auto-review"])
	// Antigravity（Google AI 订阅里可选的型号；订阅不计费，价格只用于额度权重与基线）
	add(ModelInfo{ID: "gemini-3.8-flash-high", Provider: "google", Display: "Gemini 3.8 Flash（高）", InputPerM: 0.5, OutputPerM: 3, CacheReadPerM: 0.05, Currency: "USD", Context: 1048576, Vision: true, Tools: true})
	add(ModelInfo{ID: "gemini-3.8-flash-medium", Provider: "google", Display: "Gemini 3.8 Flash（中）", InputPerM: 0.3, OutputPerM: 2, CacheReadPerM: 0.03, Currency: "USD", Context: 1048576, Vision: true, Tools: true})
	add(ModelInfo{ID: "gemini-3.8-flash-low", Provider: "google", Display: "Gemini 3.8 Flash（低）", InputPerM: 0.15, OutputPerM: 1, CacheReadPerM: 0.015, Currency: "USD", Context: 1048576, Vision: true, Tools: true})
	add(ModelInfo{ID: "claude-sonnet-4-6", Provider: "google", Display: "Sonnet 4.6", InputPerM: 3, OutputPerM: 15, CacheReadPerM: 0.3, Currency: "USD", Context: 200000, Vision: true, Tools: true})
	add(ModelInfo{ID: "claude-opus-4-6-thinking", Provider: "google", Display: "Opus 4.6", InputPerM: 5, OutputPerM: 25, CacheReadPerM: 0.5, Currency: "USD", Context: 200000, Vision: true, Tools: true})
	add(ModelInfo{ID: "gpt-oss-120b-medium", Provider: "google", Display: "GPT-OSS 120B", InputPerM: 0.1, OutputPerM: 0.5, Currency: "USD", Context: 128000, Tools: true})
	// 国产（窗口按 2026-11-10 各家官方文档；glm-5v 没核实，沿用）
	add(ModelInfo{ID: "glm-5.3", Provider: "zhipu", Display: "GLM-5.3", InputPerM: cny(8), OutputPerM: cny(28), CacheReadPerM: cny(1.6), Currency: "CNY", Context: 1000000, Vision: false, Tools: true})
	add(ModelInfo{ID: "glm-5.3-flash", Provider: "zhipu", Display: "GLM-5.3-Flash", InputPerM: cny(0.8), OutputPerM: cny(2.8), CacheReadPerM: cny(0.16), Currency: "CNY", Context: 1000000, Tools: true})
	add(ModelInfo{ID: "glm-5v", Provider: "zhipu", Display: "GLM-5V", InputPerM: cny(4), OutputPerM: cny(12), Currency: "CNY", Context: 128000, Vision: true, Tools: true})
	add(ModelInfo{ID: "qwen3.8-max", Provider: "dashscope", Display: "Qwen3.8-Max", InputPerM: cny(12), OutputPerM: cny(36), CacheReadPerM: cny(2.4), Currency: "CNY", Context: 1000000, Vision: true, Tools: true})
	add(ModelInfo{ID: "qwen3.7-plus", Provider: "dashscope", Display: "Qwen3.7-Plus", InputPerM: cny(2), OutputPerM: cny(8), CacheReadPerM: cny(0.4), Currency: "CNY", Context: 1000000, Vision: true, Tools: true})
	add(ModelInfo{ID: "qwen3.8-flash", Provider: "dashscope", Display: "Qwen3.8-Flash", InputPerM: cny(0.8), OutputPerM: cny(2.7), CacheReadPerM: cny(0.16), Currency: "CNY", Context: 1000000, Vision: true, Tools: true})
	add(ModelInfo{ID: "deepseek-v4-pro", Provider: "deepseek", Display: "DeepSeek V4 Pro", InputPerM: 1.32, OutputPerM: 3.96, CacheReadPerM: 0.026, Currency: "USD", Context: 1000000, Tools: true})
	add(ModelInfo{ID: "deepseek-flash", Provider: "deepseek", Display: "DeepSeek Flash", InputPerM: 0.30, OutputPerM: 1.20, CacheReadPerM: 0.006, Currency: "USD", Context: 1000000, Vision: true, Tools: true})
	add(ModelInfo{ID: "kimi-k3", Provider: "moonshot", Display: "Kimi K3", InputPerM: cny(20), OutputPerM: cny(100), CacheReadPerM: cny(4), Currency: "CNY", Context: 1000000, Vision: true, Tools: true})
	add(ModelInfo{ID: "minimax-m3", Provider: "minimax", Display: "MiniMax M3", InputPerM: cny(2.1), OutputPerM: cny(8.4), Currency: "CNY", Context: 1000000, Tools: true})
	add(ModelInfo{ID: "anthropic/claude-sonnet-5.5", Provider: "openrouter", Display: "Sonnet 5.5", InputPerM: 2, OutputPerM: 10, CacheReadPerM: 0.2, Currency: "USD", Context: 1000000, Vision: true, Tools: true, NeedsProxy: true})
	add(ModelInfo{ID: "anthropic/claude-opus-5.5", Provider: "openrouter", Display: "Opus 5.5", InputPerM: 4, OutputPerM: 20, CacheReadPerM: 0.4, Currency: "USD", Context: 1000000, Vision: true, Tools: true, NeedsProxy: true})
	// Agnes AI（国内直连，人民币；2026-10 官方价目，促销期多数为 0 元，这里记原价）
	// Agnes 的 flash 系列按官网定价页当前为 ¥0（刊例 ¥0.35 / ¥1.0）；pro 系列按量收费
	add(ModelInfo{ID: "agnes-3.0-flash", Provider: "agnes", Display: "Agnes 3.0 Flash", Currency: "CNY", Context: 512000, Vision: true, Tools: true, Free: true})
	add(ModelInfo{ID: "agnes-2.5-flash", Provider: "agnes", Display: "Agnes 2.5 Flash", Currency: "CNY", Context: 256000, Vision: true, Tools: true, Free: true})
	add(ModelInfo{ID: "agnes-2.5-pro-beta", Provider: "agnes", Display: "Agnes 2.5 Pro Beta", InputPerM: cny(0.7), OutputPerM: cny(2.1), CacheReadPerM: cny(0.07), Currency: "CNY", Context: 256000, Vision: true, Tools: true})
	add(ModelInfo{ID: "agnes-2.5-pro", Provider: "agnes", Display: "Agnes 2.5 Pro", InputPerM: cny(3), OutputPerM: cny(6), CacheReadPerM: cny(0.3), Currency: "CNY", Context: 256000, Vision: true, Tools: true})
	add(ModelInfo{ID: "agnes-3.0-pro", Provider: "agnes", Display: "Agnes 3.0 Pro", InputPerM: cny(3), OutputPerM: cny(6), CacheReadPerM: cny(0.3), Currency: "CNY", Context: 512000, Vision: true, Tools: true})
	add(ModelInfo{ID: "agnes-image-2.5-flash", Provider: "agnes", Display: "Agnes Image 2.5 Flash", Currency: "CNY", Free: true, MediaKind: "image"})
	add(ModelInfo{ID: "agnes-video-2.5-flash", Provider: "agnes", Display: "Agnes Video 2.5 Flash", Currency: "CNY", Free: true, MediaKind: "video"})
	add(ModelInfo{ID: "agnes-video-2.5", Provider: "agnes", Display: "Agnes Video 2.5", PerSecond: cny(0.25), Currency: "CNY", MediaKind: "video"})
	// 媒体
	add(ModelInfo{ID: "google/gemini-nano-banana-2.1", Provider: "openrouter", Display: "Nano Banana 2.1", PerImage: 0.045, Currency: "USD", NeedsProxy: true})
	// 出图按 1024×1024 默认档（medium）一张估，用来排候选；实际记账按 media/prices.go 里分质量的价
	add(ModelInfo{ID: "gpt-image-2.5-sunburst", Provider: "openai", Display: "GPT Image 2.5 Sunburst", PerImage: 0.053, Currency: "USD", NeedsProxy: true, MediaKind: "image"})
	add(ModelInfo{ID: "gpt-image-2.5-flare", Provider: "openai", Display: "GPT Image 2.5 Flare", PerImage: 0.053, Currency: "USD", NeedsProxy: true, MediaKind: "image"})
	add(ModelInfo{ID: "gpt-image-2", Provider: "openai", Display: "GPT Image 2", PerImage: 0.053, Currency: "USD", NeedsProxy: true, MediaKind: "image"})
	add(ModelInfo{ID: "gpt-image-1.5", Provider: "openai", Display: "GPT Image 1.5", PerImage: 0.034, Currency: "USD", NeedsProxy: true, MediaKind: "image"})
	add(ModelInfo{ID: "gpt-image-1", Provider: "openai", Display: "GPT Image 1", PerImage: 0.042, Currency: "USD", NeedsProxy: true, MediaKind: "image"})
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

// MediaByProvider 列出某供应商能出图 / 出视频的型号。
func (r *Registry) MediaByProvider(provider string) []ModelInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []ModelInfo
	for _, m := range r.models {
		if m.Provider == provider && (m.MediaKind != "" || m.PerImage > 0 || m.PerSecond > 0 || m.ID == "codex-imagegen") {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsVideo() != out[j].IsVideo() {
			return !out[i].IsVideo()
		}
		return out[i].ID < out[j].ID
	})
	return out
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

// Has 判断注册表里有没有这个型号，只认完全相同的名字（Lookup 会模糊匹配，gpt-6-luna 能配上 gpt-6）。
func (r *Registry) Has(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.models[id]
	return ok
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

// Priced 只认名字完全相同、而且标了价的型号：Lookup 会模糊匹配，账号里自动发现的型号也以 0 价登记过，
// 拿它们算原价会悄悄变成 0。
func (r *Registry) Priced(id string) (ModelInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.models[id]
	return m, ok && (m.InputPerM > 0 || m.OutputPerM > 0)
}

// USDPerCNY 是折算汇率。
func (r *Registry) USDPerCNY() float64 { return r.usdPerCNY }

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

// IsVideo 判断媒体型号是不是视频。
func (m ModelInfo) IsVideo() bool {
	return m.MediaKind == "video" || (m.MediaKind == "" && m.PerSecond > 0)
}

// IsFree 判断型号当前是否免费。
func (r *Registry) IsFree(id string) bool {
	m, ok := r.Lookup(id)
	return ok && m.Free
}

// FreeModels 返回某供应商当前免费的文本型号。
func (r *Registry) FreeModels(provider string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for _, m := range r.models {
		if m.Provider == provider && m.Free {
			out = append(out, m.ID)
		}
	}
	sort.Strings(out)
	return out
}

// WinWorkModels 是 WinWork 网关 availableModels 里的型号。snt 后面是 Claude Sonnet，其余按网关的叫法。
var WinWorkModels = []struct {
	ID, Name string
	Vision   bool
	Context  int
}{
	{"winwork-snt-5.5", "Sonnet 5.5", true, 200000},
	{"winwork-os-5.5", "OS 5.5", true, 0},
	{"winwork-os-5", "OS 5", true, 0},
	{"winwork-gk-4.6", "GK 4.6", false, 0},
	{"winwork-ds-v4-pro-0813", "DeepSeek V4 Pro", false, 0},
	{"winwork-ds-v4.1-flash", "DeepSeek V4.1 Flash", false, 0},
	{"winwork-qw-3.8-flash", "Qwen 3.8 Flash", false, 0},
	{"winwork-km-k3", "Kimi K3", false, 0},
}
