package core

import (
	"context"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
)

func mediaRouter(t *testing.T) *Router {
	r, _, _ := newTestRouter(t)
	r.Register(&fakeDriver{id: LaneMedia})
	_ = r.cfg.Update(func(c *config.Config) error {
		c.API = append(c.API, config.APIAccount{ID: "gw", Provider: "custom", Enabled: true, AllowPaid: true,
			Media:      map[string]string{"image": "gpt-image-2.5-flare"},
			Discovered: []config.DiscoveredModel{{ID: "gpt-image-2.5-flare"}, {ID: "gpt-image-1.5"}, {ID: "gpt-6-luna"}}})
		return nil
	})
	return r
}

func TestPinMediaModel(t *testing.T) {
	r := mediaRouter(t)
	// 账号报上来的出图型号都能点名，文本型号不算
	var names []string
	for _, c := range r.MediaModels(KindImage) {
		names = append(names, c.Model)
	}
	all := strings.Join(names, ",")
	if !strings.Contains(all, "gpt-image-2.5-flare") || !strings.Contains(all, "gpt-image-1.5") || strings.Contains(all, "gpt-6-luna") {
		t.Fatalf("能点名的出图型号不对：%v", names)
	}
	// 点名一个只在账号里报过、没分进档位的型号
	call, _, err := r.Run(context.Background(), &Task{Prompt: "一只猫", Kind: KindImage, Model: "gpt-image-1.5", Source: "mcp"}, nil)
	if err != nil || call.Model != "gpt-image-1.5" {
		t.Fatalf("应用点名的 gpt-image-1.5：%v %v", call, err)
	}
	// 快速面板带着 lane=media 点名
	d := r.Decide(&Task{Prompt: "一只猫", Kind: KindImage, Model: "gpt-image-2.5-flare", Lane: LaneMedia})
	if d.Candidates[0].Model != "gpt-image-2.5-flare" || !strings.Contains(strings.Join(d.Reason, "；"), "调用方指定模型") {
		t.Fatalf("点名的型号应排第一：%+v", d.Candidates)
	}
	// 点名一个没配置的：直接说清楚，不悄悄换成别的
	_, _, err = r.Run(context.Background(), &Task{Prompt: "一只猫", Kind: KindImage, Model: "midjourney-v9", Source: "mcp"}, nil)
	if err == nil || !strings.Contains(err.Error(), "没配置这个生图型号") {
		t.Fatalf("没配置的型号应报错：%v", err)
	}
}

// 出图手排顺序：账号先后决定不了谁先上，用户排的说了算；指定了档位时那一档仍然先上
func TestMediaOrder(t *testing.T) {
	r := mediaRouter(t)
	_ = r.cfg.Update(func(c *config.Config) error {
		// Agnes 账号排在网关前面
		c.API = append([]config.APIAccount{{ID: "ag", Provider: "agnes", Enabled: true, Media: map[string]string{"image": "agnes-image-2.5-flash"}}}, c.API...)
		return nil
	})
	first := func(tier string) string {
		for _, c := range r.Decide(&Task{Prompt: "一只猫", Kind: KindImage, MediaTier: tier}).Candidates {
			if c.Skipped == "" {
				return c.Model
			}
		}
		return ""
	}
	if got := first(""); got != "agnes-image-2.5-flash" {
		t.Fatalf("没排时按账号先后，Agnes 在前：%s", got)
	}
	_ = r.cfg.Update(func(c *config.Config) error {
		c.Routing.MediaOrder = map[string][]string{"image": {"gw/gpt-image-2.5-flare", "ag/agnes-image-2.5-flash"}}
		return nil
	})
	if got := first(""); got != "gpt-image-2.5-flare" {
		t.Fatalf("手排后应先用 GPT：%s", got)
	}
	d := r.Decide(&Task{Prompt: "一只猫", Kind: KindImage})
	if len(live(d.Candidates)) != 2 {
		t.Fatalf("表外的不用：%+v", d.Candidates)
	}
	// 指定了最好档：最好档配的那个先上（这里没账号会被跳过），不按手排
	if d := r.Decide(&Task{Prompt: "一只猫", Kind: KindImage, MediaTier: "best"}); d.Candidates[0].Model == "gpt-image-2.5-flare" && d.Candidates[0].Skipped == "" && len(live(d.Candidates)) == 2 {
		t.Fatalf("指定档位时不该照手排：%+v", d.Candidates)
	}
}

// 出图智能分派：只在勾选的里挑，判官概率 × 顺位系数
func TestSmartMedia(t *testing.T) {
	r := mediaRouter(t)
	_ = r.cfg.Update(func(c *config.Config) error {
		c.API = append([]config.APIAccount{{ID: "ag", Provider: "agnes", Enabled: true, Media: map[string]string{"image": "agnes-image-2.5-flash"}}}, c.API...)
		c.Routing.MediaOrder = map[string][]string{"image": {"gw/gpt-image-2.5-flare", "ag/agnes-image-2.5-flash"}}
		c.Routing.Smart.Enabled = true
		return nil
	})
	var probs map[string]float64
	calls := 0
	r.SetSmartJudge(func(ctx context.Context, task string, sig map[string]any, criteria map[string]string, ins string) (map[string]float64, error) {
		calls++
		if len(criteria) != 2 || !strings.Contains(criteria["1"], "GPT Image 2.5 Flare") {
			t.Fatalf("候选应只有勾选的两个，顺序照手排：%v", criteria)
		}
		return probs, nil
	})
	first := func(d *Decision) string { return d.Candidates[0].Model }
	// 判官明显更看好第二个：0.9 × 0.7 > 0.1 × 1，换
	probs = map[string]float64{"1": 0.1, "2": 0.9}
	if got := first(r.DecideSmart(context.Background(), &Task{Prompt: "画一个简单的图标", Kind: KindImage}, false)); got != "agnes-image-2.5-flash" {
		t.Fatalf("判官明显更看好 Agnes 时应换：%s", got)
	}
	// 只是略微看好第二个：0.55 × 0.7 < 0.45 × 1，仍用排第一的
	probs = map[string]float64{"1": 0.45, "2": 0.55}
	if got := first(r.DecideSmart(context.Background(), &Task{Prompt: "画一张写实的海报", Kind: KindImage}, false)); got != "gpt-image-2.5-flare" {
		t.Fatalf("判官只是略微看好别的时应仍用排第一的：%s", got)
	}
	n := calls
	// 点名了型号：不问
	if d := r.DecideSmart(context.Background(), &Task{Prompt: "画一只猫", Kind: KindImage, Model: "agnes-image-2.5-flash"}, false); calls != n || first(d) != "agnes-image-2.5-flash" {
		t.Fatalf("点名了型号不该问判官：%d", calls)
	}
	// 没打开：不问，照顺序
	_ = r.cfg.Update(func(c *config.Config) error { c.Routing.Smart.Enabled = false; return nil })
	call, _, err := r.Run(context.Background(), &Task{Prompt: "画一只狗", Kind: KindImage, Source: "mcp"}, nil)
	if err != nil || calls != n || call.Model != "gpt-image-2.5-flare" {
		t.Fatalf("没打开智能分派时照顺序、不问判官：%v %d %v", call, calls, err)
	}
}
