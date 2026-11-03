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
	if len(d.Candidates) != 1 || d.Candidates[0].Lane != LaneClaudeSub {
		t.Fatalf("WorkBuddy 应照自己的表：%+v %v", d.Candidates, d.Reason)
	}
	d = r.Decide(&Task{Prompt: "把这段话翻译成英文：你好", Source: "facade", Host: "codex"})
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
