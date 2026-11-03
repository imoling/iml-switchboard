package core

import (
	"context"
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
)

// 粘住的账号被限流后，同一会话要能回退到别的渠道，而不是整轮报「没有可用的模型」。
func TestStickyFallsBackWhenRateLimited(t *testing.T) {
	r, claude, api := newTestRouter(t)
	_ = r.cfg.Update(func(c *config.Config) error {
		for _, k := range []string{"codex_sub", "agy_sub"} {
			sl := c.Sub[k]
			sl.Enabled = false
			c.Sub[k] = sl
		}
		return nil
	})
	ctx := context.Background()
	first, _, err := r.Run(ctx, &Task{Prompt: "把这段话翻译成英文：你好", Source: "cli", SessionID: "s1"}, nil)
	if err != nil || first.Lane != LaneAPI {
		t.Fatalf("第一句应走按量付费：%+v %v", first, err)
	}

	api.fail = &ErrRateLimited{Lane: LaneAPI, Detail: "zhipu 请求过于频繁"}
	call, _, err := r.Run(ctx, &Task{Prompt: "再翻译一句：谢谢", Source: "cli", SessionID: "s1"}, nil)
	if err != nil {
		t.Fatalf("粘住的账号限流后应回退到别的渠道：%v", err)
	}
	if call.Lane != LaneClaudeSub || call.FallbackFrom != "glm-5.3-flash" {
		t.Fatalf("应从 glm-5.3-flash 回退到 Claude 订阅：%+v", call)
	}

	// 账号在冷却：下一轮粘的是刚成功的 Claude，冷却中的账号标了原因排在后面
	d := r.Decide(&Task{Prompt: "再翻译一句：再见", Source: "cli", SessionID: "s1"})
	if !d.Sticky || d.Candidates[0].Lane != LaneClaudeSub {
		t.Fatalf("应粘到 Claude：%+v %v", d.Candidates, d.Reason)
	}
	for _, c := range d.Candidates[1:] {
		if c.Lane == LaneAPI && c.Skipped == "" {
			t.Fatalf("冷却中的账号应标跳过原因：%+v", c)
		}
	}

	// 粘住的目标也倒了、别的都不可用：报错，并清掉粘性，下一轮重新选
	claude.fail = &ErrRateLimited{Lane: LaneClaudeSub, Detail: "resets in 1h"}
	if _, _, err := r.Run(ctx, &Task{Prompt: "再翻译一句：早安", Source: "cli", SessionID: "s1"}, nil); err == nil {
		t.Fatal("都限流了应报错")
	}
	if _, _, ok := r.sticky.Get("s1"); ok {
		t.Fatal("粘住的目标出错后应清掉")
	}
	d = r.Decide(&Task{Prompt: "再翻译一句：晚安", Source: "cli", SessionID: "s1"})
	if d.Sticky {
		t.Fatalf("清掉之后不该再粘：%v", d.Reason)
	}
}
