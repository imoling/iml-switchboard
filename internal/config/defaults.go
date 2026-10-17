package config

import (
	"crypto/rand"
	"encoding/hex"
)

func b(v bool) *bool { return &v }

func randomKey() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return "imls-" + hex.EncodeToString(buf)
}

// Default 是第一次启动时写出的配置，也是设计稿里的规则表。
func Default() *Config {
	c := &Config{Version: 1}
	c.Settings = Settings{
		MonthlyAPILimitUSD:   30,
		SubscriptionYieldPct: 70,
		DefaultMode:          "balanced",
		Theme:                "dianjiang",
		Hotkey:               "Alt+Space",
		SavePrompts:          true,
		LaunchAtLogin:        true,
		FacadeKey:            randomKey(),
		Notifications:        Notifications{Quota: true, Budget: true, Media: true, Suggestions: true},
		USDPerCNY:            1.0 / 7.1,
	}
	c.Sub = map[string]SubLane{
		"claude_sub": {Enabled: true, Models: map[string]string{"top": "fable", "high": "opus", "mid": "sonnet", "low": "haiku"}},
		"codex_sub":  {Enabled: true, Models: map[string]string{"high": "gpt-5.4", "mid": "gpt-5.4-mini", "low": "gpt-5.4-nano"}, ImageGen: true},
		"agy_sub":    {Enabled: false, Models: map[string]string{"high": "gemini-3.1-pro", "mid": "gemini-3.1-flash", "low": "gemini-3.1-flash-lite"}},
	}
	c.Media.Image = map[string]MediaTarget{
		"cheap":   {Provider: "ark", Model: "doubao-seedream-5-0-flash-260915"},
		"default": {Provider: "openrouter", Model: "google/gemini-nano-banana-2.1"},
		"best":    {Provider: "openai", Model: "gpt-image-2.5-sunburst", Quality: "high"},
		"free":    {Provider: "codex", Model: "codex-imagegen"},
	}
	c.Media.Video = map[string]MediaTarget{
		"cheap":   {Provider: "ark", Model: "doubao-seedance-2-0-fast-260128", Resolution: "720p"},
		"default": {Provider: "openrouter", Model: "google/veo-3.1-lite"},
		"best":    {Provider: "openrouter", Model: "bytedance/seedance-2.5", Resolution: "1080p"},
	}
	c.Media.Animation.CoderLane = "claude_sub"
	c.Media.Animation.CoderModel = "opus"
	c.Media.Animation.Renderer = "remotion"
	c.Integrations = map[string]Integration{}
	c.Routing = Routing{
		Sticky: "session",
		Explicit: []Explicit{
			{ID: "top", Words: []string{"/fable", "仔细想", "想清楚再答", "think hard"}, Tier: "top", Builtin: true, Enabled: true},
			{ID: "high", Words: []string{"/opus", "/hard"}, Tier: "high", Builtin: true, Enabled: true},
			{ID: "low", Words: []string{"/quick", "/cheap"}, Tier: "low", Builtin: true, Enabled: true},
			{ID: "sub", Words: []string{"/sub"}, Effect: "sub_only", Enabled: true},
			{ID: "img-best", Words: []string{"/img-best"}, Effect: "media_best", Enabled: true},
		},
		ContextRules: []ContextRule{
			{ID: "no_low_over_100k", Title: "超过 100K 不用低档", Note: "低档模型处理超长内容反而更贵", Enabled: true},
			{ID: "keep_over_60k", Title: "超过 60K 不换模型", Note: "换模型要重读全部内容，又慢又贵", Enabled: true},
			{ID: "sticky_session", Title: "同一会话不换模型", Note: "只在新任务或分派子任务时重新选", Enabled: true},
			{ID: "vision_only_with_image", Title: "带图片时只用能看图的模型", Note: "关掉后图片会被忽略", Enabled: true},
		},
		TaskTable: []TaskType{
			{Type: "image", Name: "生图", Hint: "调用 generate_image，或「画」「配图」「封面」", Kind: "image", MediaTier: "default", Match: Match{Keywords: []string{"画一张", "画个", "配图", "封面图", "生成图片", "生成一张图", "做一张图", "插图", "海报"}}, Order: 1},
			{Type: "video", Name: "生视频", Hint: "调用 generate_video，或「视频」「短片」", Kind: "video", MediaTier: "cheap", Match: Match{Keywords: []string{"生成视频", "做个视频", "做一个视频", "短片", "秒的视频", "图生视频", "介绍视频", "宣传视频", "短视频", "做成视频", "一段视频", "秒视频"}}, Order: 2},
			{Type: "animation", Name: "动画", Hint: "调用 make_animation，或「动画」「动效」「Remotion」", Kind: "animation", Match: Match{Keywords: []string{"动画", "动效", "remotion", "转场"}}, Order: 3},
			{Type: "debug_hard", Name: "疑难调试", Hint: "有报错栈，或「试了很多次」「还是不行」", Tier: "high", Prefer: []string{"claude_sub", "codex_sub"}, Match: Match{HasStack: b(true)}, Order: 10},
			{Type: "debug_stuck", Name: "疑难调试", Hint: "反复失败", Tier: "high", Prefer: []string{"claude_sub", "codex_sub"}, Match: Match{Keywords: []string{"试了很多次", "还是不行", "还是报错", "一直失败", "怎么都不行"}}, Order: 11},
			{Type: "architecture", Name: "架构设计", Hint: "「架构」「方案」「怎么设计」「重构」", Tier: "high", Prefer: []string{"claude_sub"}, Match: Match{Keywords: []string{"架构", "技术方案", "怎么设计", "重构", "系统设计", "选型"}}, Order: 12},
			{Type: "review", Name: "代码评审", Hint: "review、帮我看看、另一个思路", Tier: "mid", Prefer: []string{"codex_sub", "api"}, Match: Match{Keywords: []string{"review", "评审", "帮我看看", "另一个思路", "第二意见", "有没有问题"}}, Order: 20},
			{Type: "rewrite", Name: "改写与翻译", Hint: "改写、润色、翻译、转成表格", Tier: "low", Prefer: []string{"api"}, Match: Match{Keywords: []string{"改写", "润色", "翻译", "转成表格", "换个说法", "精简一下", "扩写"}}, Order: 30},
			{Type: "summarize", Name: "摘要与归类", Hint: "总结、归类、提取；长日志", Tier: "low", Prefer: []string{"api"}, Match: Match{Keywords: []string{"总结", "归类", "提取", "分类", "摘要", "归纳", "列出"}}, Order: 31},
			{Type: "code_small", Name: "小段代码", Hint: "有代码块，80 行以内，无报错栈", Tier: "mid", Prefer: []string{"api", "claude_sub"}, Match: Match{HasCode: b(true), HasStack: b(false), MaxCodeLines: 80}, Order: 40},
			{Type: "code_feature", Name: "写功能", Hint: "代码超过 80 行，或要新建模块", Tier: "mid", Prefer: []string{"claude_sub", "api"}, Match: Match{HasCode: b(true), HasStack: b(false)}, Order: 41},
			{Type: "qa_short", Name: "短问答", Hint: "300 字以内，无代码、无附件", Tier: "low", Prefer: []string{"api", "codex_sub"}, Match: Match{MaxChars: 300, HasCode: b(false), HasImage: b(false)}, Order: 50},
		},
	}
	return c
}
