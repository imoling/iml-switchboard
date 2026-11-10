package core

import "testing"

// 主流型号的窗口按 2026-11-10 各家官方文档；改动前先查官方页，别凭印象
func TestOfficialContextWindows(t *testing.T) {
	r := NewRegistry(7.2)
	want := map[string]int{
		"haiku": 1000000, "claude-haiku-5-5": 1000000,
		"gpt-6.1-sol": 1050000, "gpt-6.1-terra": 1050000, "gpt-6-sol": 1050000, "gpt-6-luna": 1050000, "gpt-6-astra": 1050000,
		"gpt-5.6-sol": 1050000, "gpt-5.6-terra": 1050000, "gpt-5.6-luna": 1050000,
		"gpt-6": 1050000, "codex-auto-review": 1050000,
		"gemini-3.8-flash-high": 1048576, "gemini-3.8-flash-medium": 1048576, "gemini-3.8-flash-low": 1048576,
		"glm-5.3": 1000000, "glm-5.3-flash": 1000000,
		"qwen3.8-max": 1000000, "qwen3.7-plus": 1000000, "qwen3.8-flash": 1000000,
		"deepseek-v4-pro": 1000000, "deepseek-flash": 1000000,
		"kimi-k3": 1000000, "minimax-m3": 1000000,
	}
	for id, w := range want {
		if !r.Has(id) {
			t.Errorf("注册表里没有 %s", id)
			continue
		}
		m, _ := r.Lookup(id)
		if m.Context != w {
			t.Errorf("%s 窗口 = %d，官方是 %d", id, m.Context, w)
		}
	}
	if m, _ := r.Lookup("deepseek-flash"); !m.Vision {
		t.Error("deepseek-flash 能看图")
	}
}
