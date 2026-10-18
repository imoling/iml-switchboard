package core

import (
	"context"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
)

// judgeDriver 对判档请求回答固定的档位，对正常请求照常回答。
type judgeDriver struct {
	fakeDriver
	label  string
	judged int
}

func (j *judgeDriver) Run(ctx context.Context, t *Task, tg Target, emit func(Event)) (*Result, error) {
	if t.Source == "classifier" {
		j.judged++
		return &Result{Text: j.label, Model: tg.Model, Usage: Usage{CostUSD: 0.0002}}, nil
	}
	return j.fakeDriver.Run(ctx, t, tg, emit)
}

func newJudgeRouter(t *testing.T, label string) (*Router, *judgeDriver) {
	r, _, _ := newTestRouter(t)
	j := &judgeDriver{fakeDriver: fakeDriver{id: LaneAPI}, label: label}
	r.Register(j)
	return r, j
}

func TestParseLabel(t *testing.T) {
	for in, want := range map[string]string{"high": "high", " Mid\n": "mid", "档位：low": "low", "animation": "animation", "高档": "", "": "", "midway": ""} {
		if got := ParseLabel(in); got != want {
			t.Errorf("%q → %q，想要 %q", in, got, want)
		}
	}
}

func TestJudgeRaisesUnsureTier(t *testing.T) {
	r, j := newJudgeRouter(t, "high")
	call, _, err := r.Run(context.Background(), &Task{Prompt: "CI 里这条测试十次挂一次，本地永远过", Source: "mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if j.judged != 1 || call.Tier != TierHigh || call.TaskType != "judged" {
		t.Fatalf("判档 %d 次，档位 %s，类型 %s", j.judged, call.Tier, call.TaskType)
	}
	if call.CostUSD < 0.0002 {
		t.Fatalf("判档花费没记上：%v", call.CostUSD)
	}
	if !strings.Contains(strings.Join(call.Reason, "；"), "低档模型判为高档") {
		t.Fatalf("理由里没写判档：%v", call.Reason)
	}
	// 同一句话再来一次走缓存，不再判
	if _, _, err := r.Run(context.Background(), &Task{Prompt: "CI 里这条测试十次挂一次，本地永远过", Source: "mcp"}, nil); err != nil {
		t.Fatal(err)
	}
	if j.judged != 1 {
		t.Fatalf("缓存没生效，判了 %d 次", j.judged)
	}
}

func TestJudgeGarbageFallsBackToRules(t *testing.T) {
	r, j := newJudgeRouter(t, "我觉得挺难的")
	call, _, err := r.Run(context.Background(), &Task{Prompt: "今天几号", Source: "mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if j.judged == 0 || call.Tier != TierLow || call.TaskType != "qa_short" {
		t.Fatalf("乱答应按规则走：判档 %d 次，档位 %s，类型 %s", j.judged, call.Tier, call.TaskType)
	}
}

func TestJudgeSkippedWhenRuleIsSure(t *testing.T) {
	r, j := newJudgeRouter(t, "high")
	for _, p := range []string{"把这段话翻译成英文：你好", "/quick 今天几号", "/sub 今天几号"} {
		if _, _, err := r.Run(context.Background(), &Task{Prompt: p, Source: "mcp"}, nil); err != nil && !strings.Contains(err.Error(), "没有可用") {
			t.Fatal(err)
		}
	}
	if _, _, err := r.Run(context.Background(), &Task{Prompt: "今天几号", Tier: TierMid, Source: "mcp"}, nil); err != nil {
		t.Fatal(err)
	}
	if j.judged != 0 {
		t.Fatalf("规则有把握或有显式指令时不该判档，判了 %d 次", j.judged)
	}
}

func TestRulesOnlyTurnsJudgeOff(t *testing.T) {
	r, j := newJudgeRouter(t, "high")
	_ = r.cfg.Update(func(c *config.Config) error { c.Settings.RulesOnly = true; return nil })
	call, _, err := r.Run(context.Background(), &Task{Prompt: "CI 里这条测试十次挂一次", Source: "mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if j.judged != 0 || call.Tier != TierLow {
		t.Fatalf("只按规则时不该判档：判档 %d 次，档位 %s", j.judged, call.Tier)
	}
}

func TestJudgeMediaOnlyForShortAsks(t *testing.T) {
	r, _ := newJudgeRouter(t, "image")
	d := r.Decide(&Task{Prompt: "Generate a flat-style illustration of a cat", Judged: "image"})
	if d.Kind != KindImage {
		t.Fatalf("短请求判为生图应进媒体渠道，got %s", d.Kind)
	}
	d = r.Decide(&Task{Prompt: "Generate a flat-style illustration of a cat", Judged: "image", Tools: []byte(`[{"type":"function"}]`)})
	if d.Kind != KindText {
		t.Fatalf("带工具的请求不能被判成媒体，got %s", d.Kind)
	}
}
