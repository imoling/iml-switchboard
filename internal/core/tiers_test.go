package core

import (
	"context"
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
)

// 手排的候选表：顺序完全照表，表外的不用；带工具时订阅条目照样跳过；条目全失效时回到内置排法。
func TestTierTablePicks(t *testing.T) {
	r, _, _ := newTestRouter(t)
	// 内置排法：低档先按量付费
	chosen, avail, _, custom := r.TierTable("", TierLow)
	if custom || len(avail) < 2 || chosen[0].Lane != LaneAPI {
		t.Fatalf("没手排时应是内置顺序：%+v %+v", chosen, avail)
	}

	_ = r.cfg.Update(func(c *config.Config) error {
		c.Routing.Tiers = map[string][]config.TierPick{"low": {{Lane: "claude_sub"}, {Lane: "api", Account: "zhipu-1", Model: "glm-5.3-flash"}}}
		return nil
	})
	d := r.Decide(&Task{Prompt: "把这段话翻译成英文：你好", Source: "cli"})
	d.Candidates = sameTier(d.Candidates) // 后面接着的是提档用的中档、高档候选
	if d.Tier != TierLow || len(d.Candidates) != 2 || d.Candidates[0].Lane != LaneClaudeSub || d.Candidates[1].Model != "glm-5.3-flash" {
		t.Fatalf("应照表排：%+v %v", d.Candidates, d.Reason)
	}
	call, _, err := r.Run(context.Background(), &Task{Prompt: "把这段话翻译成英文：你好", Source: "cli"}, nil)
	if err != nil || call.Lane != LaneClaudeSub {
		t.Fatalf("表里第一个是 Claude 订阅：%+v %v", call, err)
	}
	// 带工具：订阅条目标「带工具」，落到表里的按量付费
	d = r.Decide(&Task{Prompt: "把这段话翻译成英文：你好", Source: "cli", Tools: []byte(`[{"type":"function"}]`)})
	if d.Candidates[0].Skipped != "带工具" || live(d.Candidates)[0].Model != "glm-5.3-flash" {
		t.Fatalf("带工具应跳过订阅：%+v", d.Candidates)
	}
	chosen, _, _, custom = r.TierTable("", TierLow)
	if !custom || len(chosen) != 2 || chosen[0].Lane != LaneClaudeSub {
		t.Fatalf("界面应看到手排的表：%+v", chosen)
	}

	// 表里只剩失效条目（账号没了）：回到内置排法，不能整档没人
	_ = r.cfg.Update(func(c *config.Config) error {
		c.Routing.Tiers = map[string][]config.TierPick{"low": {{Lane: "api", Account: "gone", Model: "x"}}}
		return nil
	})
	d = r.Decide(&Task{Prompt: "把这段话翻译成英文：你好", Source: "cli"})
	if len(live(d.Candidates)) == 0 || d.Candidates[0].Lane != LaneAPI {
		t.Fatalf("条目全失效应回到内置排法：%+v %v", d.Candidates, d.Reason)
	}
	if _, _, _, custom := r.TierTable("", TierLow); custom {
		t.Fatal("失效的表不算手排")
	}
}

func TestTierPickID(t *testing.T) {
	for _, p := range []config.TierPick{{Lane: "claude_sub"}, {Lane: "api", Account: "custom-befe2f", Model: "gpt-6.1-sol"}, {Lane: "api", Account: "a:b", Model: "m"}} {
		back, ok := config.ParseTierPick(p.ID())
		if !ok || back != p {
			t.Fatalf("%+v 经 %q 还原成 %+v", p, p.ID(), back)
		}
	}
	if _, ok := config.ParseTierPick("nonsense"); ok {
		t.Fatal("乱写的标识不该解析成功")
	}
}

// 宿主自己的表优先于全局的表；没排的宿主跟全局；界面能看到宿主继承的顺序。
func TestTierTablePerHost(t *testing.T) {
	r, _, _ := newTestRouter(t)
	_ = r.cfg.Update(func(c *config.Config) error {
		c.Routing.Tiers = map[string][]config.TierPick{"low": {{Lane: "api", Account: "zhipu-1", Model: "glm-5.3-flash"}, {Lane: "claude_sub"}}}
		if c.Integrations == nil {
			c.Integrations = map[string]config.Integration{}
		}
		it := c.Integrations["workbuddy"]
		it.Tiers = map[string][]config.TierPick{"low": {{Lane: "claude_sub"}}}
		c.Integrations["workbuddy"] = it
		return nil
	})
	d := r.Decide(&Task{Prompt: "把这段话翻译成英文：你好", Source: "facade", Host: "workbuddy"})
	d.Candidates = sameTier(d.Candidates)
	if len(d.Candidates) != 1 || d.Candidates[0].Lane != LaneClaudeSub {
		t.Fatalf("WorkBuddy 应照自己的表：%+v %v", d.Candidates, d.Reason)
	}
	d = r.Decide(&Task{Prompt: "把这段话翻译成英文：你好", Source: "facade", Host: "codex"})
	d.Candidates = sameTier(d.Candidates)
	if len(d.Candidates) != 2 || d.Candidates[0].Lane != LaneAPI {
		t.Fatalf("没排的宿主应跟全局：%+v %v", d.Candidates, d.Reason)
	}
	chosen, _, inherited, custom := r.TierTable("workbuddy", TierLow)
	if !custom || len(chosen) != 1 || len(inherited) != 2 || inherited[0].Lane != LaneAPI {
		t.Fatalf("宿主表和继承的全局表：%+v %+v", chosen, inherited)
	}
	if chosen, _, _, custom := r.TierTable("codex", TierLow); custom || len(chosen) != 2 {
		t.Fatalf("没排的宿主应显示全局顺序：%+v", chosen)
	}
}

// sameTier 只留决策那一档的候选。
func sameTier(ts []Target) []Target {
	var out []Target
	for _, t := range ts {
		if t.Tier == "" {
			out = append(out, t)
		}
	}
	return out
}

// 自动分档：低档都用不了（带图、模型不能看图；带工具、订阅接不了；按量付费被限流）就往上提档，记实际用的那一档
func TestAutoTierEscalates(t *testing.T) {
	r, _, api := newTestRouter(t)
	tools := []byte(`[{"type":"function"}]`)
	// 低档只有不能看图的 glm-5.3-flash；中档的 glm-5v 能看图
	_ = r.cfg.Update(func(c *config.Config) error {
		c.API[0].Models = map[string][]string{"low": {"glm-5.3-flash"}, "mid": {"glm-5v"}, "high": {}}
		return nil
	})
	task := &Task{Prompt: "这张图里写的是什么", Source: "facade", Host: "codex", Tools: tools,
		Messages: []Message{{Role: "user", Content: "这张图里写的是什么", Images: []string{"data:image/png;base64,AAAA"}}}}
	call, _, err := r.Run(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("低档不能看图时应提到中档，不该报错：%v", err)
	}
	if call.Model != "glm-5v" || call.Tier != TierMid || api.calls == 0 {
		t.Fatalf("应提到中档的 glm-5v：%s %s %v", call.Model, call.Tier, call.Reason)
	}
	// 调用方指定档位（MCP 参数、router-low）就不提档
	task = &Task{Prompt: "这张图里写的是什么", Source: "mcp", Tier: TierLow, Tools: tools,
		Messages: []Message{{Role: "user", Content: "这张图里写的是什么", Images: []string{"data:image/png;base64,AAAA"}}}}
	if d := r.Decide(task); len(d.Candidates) != len(sameTier(d.Candidates)) {
		t.Fatalf("指定了档位不该往上补：%+v", d.Candidates)
	}
}

// 应用点名了型号：记录的档位是这个型号在配置里的档，不是规则判的
func TestPinnedModelRecordsItsTier(t *testing.T) {
	r, _, _ := newTestRouter(t)
	_ = r.cfg.Update(func(c *config.Config) error {
		c.API[0].Models = map[string][]string{"low": {"glm-5.3-flash"}, "mid": {}, "high": {"glm-5.3"}}
		return nil
	})
	tools := []byte(`[{"type":"function"}]`)
	call, _, err := r.Run(context.Background(), &Task{Prompt: "你好", Model: "glm-5.3", Source: "facade", Host: "workbuddy", Tools: tools}, nil)
	if err != nil || call.Model != "glm-5.3" || call.Tier != TierHigh {
		t.Fatalf("点名高档型号应记高档：%v %s %s %v", err, call.Model, call.Tier, call.Reason)
	}
	// 哪档都没有：按型号名认；名字也认不出记空，界面显示「指定」
	for m, want := range map[string]Tier{"gpt-5.6-luna": TierLow, "gpt-5.6-terra": TierMid, "gpt-5.6-sol": TierHigh, "claude-haiku-5-5": TierLow, "glm-9-unknown": ""} {
		if got := r.TierOfModel(Target{Lane: LaneAPI, Model: m}); got != want {
			t.Errorf("%s 应为 %q，得到 %q", m, want, got)
		}
	}
}
