package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// big 造一段约 n 个 token 的英文（估算按 4 个字符一个 token）。
func big(n int) string { return strings.Repeat("abcd", n) }

func TestContextWindowFit(t *testing.T) {
	r, _, _ := newTestRouter(t)
	// 中档：GLM-5.3 只有 20 万，装不下 25 万；Claude Sonnet 有 100 万
	d := r.Decide(&Task{Prompt: big(250000), Tier: TierMid})
	var glm, sonnet *Target
	for i := range d.Candidates {
		switch d.Candidates[i].Model {
		case "glm-5.3":
			glm = &d.Candidates[i]
		case "sonnet":
			sonnet = &d.Candidates[i]
		}
	}
	if glm == nil || !strings.HasPrefix(glm.Skipped, "装不下") {
		t.Fatalf("GLM 应标「装不下」：%+v", d.Candidates)
	}
	if sonnet == nil || sonnet.Skipped != "" {
		t.Fatalf("Sonnet 应可用：%+v", d.Candidates)
	}

	// 低档全都装不下（Haiku、GLM-Flash 20 万，GPT-5.6 Luna 40 万）：自动升到中档。「超过 100K 不用低档」先关掉，单看窗口
	for i := range r.cfg.Get().Routing.ContextRules {
		r.cfg.Get().Routing.ContextRules[i].Enabled = false
	}
	d = r.Decide(&Task{Prompt: big(450000), Tier: TierLow})
	if len(live(d.Candidates)) == 0 || !strings.Contains(strings.Join(d.Reason, "；"), "装不下，改用中档") {
		t.Fatalf("低档装不下应升档：%v %+v", d.Reason, d.Candidates)
	}

	// 谁都装不下（最大 100 万，留一成余量）：返回专门的错误，让宿主去压缩
	_, _, err := r.Run(context.Background(), &Task{Prompt: big(950000), Source: "facade"}, nil)
	var tl *ErrContextTooLong
	if !errors.As(err, &tl) || tl.MaxWindow != 1000000 {
		t.Fatalf("应返回 ErrContextTooLong：%v", err)
	}
	if err := r.FitsAny(950000); err == nil {
		t.Fatal("FitsAny 应报装不下")
	}
	if err := r.FitsAny(500000); err != nil {
		t.Fatalf("50 万应装得下：%v", err)
	}
}

func TestStickyDropsWhenTooLong(t *testing.T) {
	r, _, _ := newTestRouter(t)
	if _, _, err := r.Run(context.Background(), &Task{Prompt: "你好", Tier: TierLow, SessionID: "s1"}, nil); err != nil {
		t.Fatal(err)
	}
	for i := range r.cfg.Get().Routing.ContextRules {
		if r.cfg.Get().Routing.ContextRules[i].ID == "no_low_over_100k" {
			r.cfg.Get().Routing.ContextRules[i].Enabled = false
		}
	}
	d := r.Decide(&Task{Prompt: big(250000), SessionID: "s1"})
	if d.Sticky {
		t.Fatalf("原模型装不下了还在粘：%+v", d)
	}
}
