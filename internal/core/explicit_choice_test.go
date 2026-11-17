package core

import (
	"context"
	"errors"
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
)

func TestExplicitTierDoesNotCrossTier(t *testing.T) {
	for _, tier := range []Tier{TierHigh, TierLow, TierTop} {
		t.Run(string(tier), func(t *testing.T) {
			r, claude, api := newTestRouter(t)
			task := &Task{Prompt: "你好", Tier: tier, Tools: []byte(`[{"type":"function"}]`), Source: "facade"}
			if tier == TierLow {
				task.Prompt = big(1200000)
			}
			d := r.Decide(task)
			if d.Auto {
				t.Fatal("显式档位不能标为自动")
			}
			for _, c := range d.Candidates {
				if c.Tier != "" && c.Tier != tier {
					t.Fatalf("不能跨档：%+v", d)
				}
			}
			call, _, err := r.Run(context.Background(), task, nil)
			if err == nil || call.OK || claude.calls != 0 || api.calls != 0 {
				t.Fatalf("所选档不可用应失败：%+v %v", call, err)
			}
		})
	}
}

func TestPinnedModelFailureDoesNotSwitchModel(t *testing.T) {
	r, claude, api := newTestRouter(t)
	api.fail = errors.New("上游不可用")
	call, _, err := r.Run(context.Background(), &Task{Prompt: "你好", Model: "glm-5.3-flash"}, nil)
	if err == nil || call.OK || api.calls != 1 || claude.calls != 0 {
		t.Fatalf("点名失败不能换模型：%+v %v api=%d sub=%d", call, err, api.calls, claude.calls)
	}
}

func TestExplicitTierStillFallsBackWithinTier(t *testing.T) {
	r, claude, api := newTestRouter(t)
	claude.fail = &ErrRateLimited{Lane: LaneClaudeSub, Detail: "限流"}
	_ = r.cfg.Update(func(c *config.Config) error { c.API[0].Models["high"] = []string{"glm-5.3"}; return nil })
	call, _, err := r.Run(context.Background(), &Task{Prompt: "你好", Tier: TierHigh}, nil)
	if err != nil || call.Tier != TierHigh || call.Model != "glm-5.3" || api.calls != 1 {
		t.Fatalf("允许同档换候选：%+v %v", call, err)
	}
}
