package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
)

type fakeDriver struct {
	id     LaneID
	fail   error
	calls  int
	health Health
}

func (f *fakeDriver) ID() LaneID { return f.id }
func (f *fakeDriver) Run(ctx context.Context, t *Task, tg Target, emit func(Event)) (*Result, error) {
	f.calls++
	if f.fail != nil {
		return nil, f.fail
	}
	return &Result{Text: "ok from " + tg.Model, Model: tg.Model, Usage: Usage{InputTokens: 100, OutputTokens: 50, CostUSD: 0.001}}, nil
}
func (f *fakeDriver) Health(ctx context.Context) Health {
	if f.health.Status == "" {
		return Health{Status: "ok"}
	}
	return f.health
}

func newTestRouter(t *testing.T) (*Router, *fakeDriver, *fakeDriver) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("IMLS_DATA_DIR", dir)
	cs, err := config.Open()
	if err != nil {
		t.Fatal(err)
	}
	_ = cs.Update(func(c *config.Config) error {
		c.API = []config.APIAccount{{ID: "zhipu-1", Provider: "zhipu", Enabled: true, Currency: "CNY",
			Models: map[string][]string{"low": {"glm-5.3-flash"}, "mid": {"glm-5.3"}, "high": {}}}}
		return nil
	})
	l, err := OpenLedger(filepath.Join(dir, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	reg := NewRegistry(1.0 / 7.1)
	r := NewRouter(cs, l, reg, NewQuotaManager(l, func() float64 { return 70 }))
	claude := &fakeDriver{id: LaneClaudeSub}
	api := &fakeDriver{id: LaneAPI}
	r.Register(claude)
	r.Register(api)
	r.Register(&fakeDriver{id: LaneCodexSub})
	return r, claude, api
}

func TestDecideAndRun(t *testing.T) {
	r, claude, api := newTestRouter(t)
	call, res, err := r.Run(context.Background(), &Task{Prompt: "把这段话翻译成英文：你好", Source: "cli"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if call.Lane != LaneAPI || call.Tier != TierLow || api.calls != 1 || claude.calls != 0 {
		t.Fatalf("低档应走按量付费：%+v", call)
	}
	if res.Text == "" || call.BaselineCostUSD <= call.CostUSD {
		t.Fatalf("基线应高于实际：%+v", call)
	}
	call, _, err = r.Run(context.Background(), &Task{Prompt: "这个系统的架构怎么设计", Source: "cli"}, nil)
	if err != nil || call.Lane != LaneClaudeSub || call.Model != "opus" {
		t.Fatalf("高档应走 Claude 订阅 opus：%+v %v", call, err)
	}
	call, _, _ = r.Run(context.Background(), &Task{Prompt: "/fable 这个系统的架构怎么设计", Source: "cli"}, nil)
	if call.Tier != TierTop || call.Model != "fable" {
		t.Fatalf("显式 /fable 应走顶配：%+v", call)
	}
}

func TestFallbackOnRateLimit(t *testing.T) {
	r, claude, api := newTestRouter(t)
	claude.fail = &ErrRateLimited{Lane: LaneClaudeSub, Detail: "resets in 1h"}
	// code_small 的偏好是 api 优先，这里用架构设计（claude 优先）测回退
	_ = r.cfg.Update(func(c *config.Config) error {
		c.API[0].Models["high"] = []string{"glm-5.3"}
		return nil
	})
	call, _, err := r.Run(context.Background(), &Task{Prompt: "这个系统的架构怎么设计", Source: "cli"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if call.Lane != LaneAPI || call.FallbackFrom != "opus" || api.calls != 1 {
		t.Fatalf("应从 opus 降级到按量付费：%+v", call)
	}
	if cooling, _, _ := r.quota.Cooling(LaneClaudeSub); !cooling {
		t.Fatal("Claude 订阅应进入冷却")
	}
	d := r.Decide(&Task{Prompt: "这个系统的架构怎么设计"})
	if d.Candidates[0].Skipped == "" {
		t.Fatalf("冷却中的候选应被标注：%+v", d.Candidates)
	}
}

func TestToolsSkipSubscription(t *testing.T) {
	r, claude, api := newTestRouter(t)
	_ = r.cfg.Update(func(c *config.Config) error { c.API[0].Models["high"] = []string{"glm-5.3"}; return nil })
	call, _, err := r.Run(context.Background(), &Task{Prompt: "这个系统的架构怎么设计", Tools: []byte(`[{"type":"function"}]`), Source: "facade"}, nil)
	if err != nil || call.Lane != LaneAPI || claude.calls != 0 || api.calls != 1 {
		t.Fatalf("带工具不能走订阅：%+v %v", call, err)
	}
}

func TestNonRetryableStops(t *testing.T) {
	r, claude, api := newTestRouter(t)
	claude.fail = &ErrProvider{Summary: "内容未通过审核", Retryable: false}
	_ = r.cfg.Update(func(c *config.Config) error { c.API[0].Models["high"] = []string{"glm-5.3"}; return nil })
	_, _, err := r.Run(context.Background(), &Task{Prompt: "这个系统的架构怎么设计", Source: "cli"}, nil)
	if err == nil || api.calls != 0 {
		t.Fatalf("不可重试的错误不应换候选：%v %d", err, api.calls)
	}
	calls, _ := r.ledger.List(ListFilter{})
	if len(calls) != 1 || calls[0].OK {
		t.Fatalf("失败也要记账：%+v", calls)
	}
}

func TestLedgerAggregateAndEscalate(t *testing.T) {
	r, _, _ := newTestRouter(t)
	for i := 0; i < 6; i++ {
		call, _, _ := r.Run(context.Background(), &Task{Prompt: "把这段话翻译成英文：你好", Source: "cli"}, nil)
		if _, n, _ := r.Escalate(call.ID); i == 5 && n != 6 {
			t.Fatalf("判低计数错误 %d", n)
		}
	}
	ov, err := r.ledger.Aggregate(7, r.loc)
	if err != nil || ov.Calls != 6 || ov.EscalationRate != 100 || ov.EscalationHot != "rewrite" {
		t.Fatalf("%+v %v", ov, err)
	}
	sug, _ := r.Suggestions()
	if len(sug) != 1 || sug[0].ToTier != TierMid {
		t.Fatalf("应建议 rewrite 升到中档：%+v", sug)
	}
	if err := r.ApplySuggestion("rewrite", "accept"); err != nil {
		t.Fatal(err)
	}
	for _, tt := range r.cfg.Get().Routing.TaskTable {
		if tt.Type == "rewrite" && tt.Tier != "mid" {
			t.Fatal("采纳后应为中档")
		}
	}
	_ = os.Remove(filepath.Join(config.DataDir(), "x"))
}

func TestErrors(t *testing.T) {
	var e error = &ErrRateLimited{Lane: LaneClaudeSub, Detail: "x"}
	var rl *ErrRateLimited
	if !errors.As(e, &rl) {
		t.Fatal("errors.As")
	}
}

func TestPinnedLaneAndModel(t *testing.T) {
	r, claude, api := newTestRouter(t)
	_ = r.cfg.Update(func(c *config.Config) error { c.API[0].Models["high"] = []string{"glm-5.3"}; return nil })
	call, _, err := r.Run(context.Background(), &Task{Prompt: "这个系统的架构怎么设计", Lane: LaneAPI, Source: "quick"}, nil)
	if err != nil || call.Lane != LaneAPI || claude.calls != 0 {
		t.Fatalf("点名按量付费应只走按量付费：%+v %v", call, err)
	}
	call, _, err = r.Run(context.Background(), &Task{Prompt: "翻译：hi", Model: "sonnet", Source: "quick"}, nil)
	if err != nil || call.Model != "sonnet" || api.calls != 1 {
		t.Fatalf("点名模型应优先：%+v %v", call, err)
	}
}
