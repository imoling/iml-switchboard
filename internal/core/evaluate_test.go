package core

import (
	"context"
	"strings"
	"testing"
	"time"
)

// recDriver 记下每次收到的提示词；裁判请求一律答「平」。
type recDriver struct {
	fakeDriver
	prompts []string
}

func (d *recDriver) Run(ctx context.Context, t *Task, tg Target, emit func(Event)) (*Result, error) {
	d.prompts = append(d.prompts, t.Prompt)
	if strings.HasPrefix(t.Prompt, "你是评审") {
		return &Result{Text: "平", Model: tg.Model}, nil
	}
	return &Result{Text: "高档的回答", Model: tg.Model}, nil
}

func TestEvaluationUsesFullPrompt(t *testing.T) {
	r, _, _ := newTestRouter(t)
	rec := &recDriver{fakeDriver: fakeDriver{id: LaneClaudeSub}}
	r.Register(rec)
	long := "请评审下面这段很长的需求：" + strings.Repeat("细节。", 150) + "最后一句只在原话里"
	now := time.Now()
	ins := func(c *Call) {
		c.TS, c.Kind, c.OK, c.Lane, c.Tier = now, KindText, true, LaneAPI, TierLow
		if err := r.ledger.Insert(c); err != nil {
			t.Fatal(err)
		}
	}
	// 新记录：带完整原话，摘录被截断
	ins(&Call{ID: "full", Source: "mcp", TaskType: "qa_short", PromptExcerpt: excerpt(long, 200), ResultText: "低档的回答", PromptFull: long, ResultFull: "低档的回答"})
	// 老记录：摘录被截断，没有原话，不能抽
	ins(&Call{ID: "cut", Source: "mcp", TaskType: "qa_short", PromptExcerpt: excerpt(long, 200), ResultText: "x"})
	// 多轮对话（兼容入口）的老记录，不能抽
	ins(&Call{ID: "facade", Source: "facade", TaskType: "qa_short", PromptExcerpt: "短问题", ResultText: "x"})
	_, _ = r.ledger.db.Exec(`UPDATE calls SET prompt_full=NULL, result_full=NULL WHERE id IN ('cut','facade')`)

	got, err := r.ledger.Sample(30, 10, []string{"low", "mid"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "full" {
		ids := []string{}
		for _, c := range got {
			ids = append(ids, c.ID)
		}
		t.Fatalf("只该抽到 full，抽到 %v", ids)
	}

	ev := &Evaluation{ID: "ev1", Sample: 10}
	r.RunEvaluation(context.Background(), ev, func(*Evaluation) {})
	if ev.Status != "done" || ev.Summary.Tie != 1 {
		t.Fatalf("评估结果不对：%+v", ev)
	}
	var redo, judge string
	for _, p := range rec.prompts {
		if strings.HasPrefix(p, "你是评审") {
			judge = p
		} else if strings.HasPrefix(p, "请评审") {
			redo = p
		}
	}
	if !strings.Contains(redo, "最后一句只在原话里") {
		t.Error("高档重做拿到的是截断的摘录")
	}
	if !strings.Contains(judge, "最后一句只在原话里") || !strings.Contains(judge, "低档的回答") {
		t.Error("裁判没看到完整的问题或低档的回答")
	}
}

func TestPruneFullAndMonthCost(t *testing.T) {
	r, _, _ := newTestRouter(t)
	old, recent := time.Now().AddDate(0, 0, -40), time.Now()
	for _, c := range []*Call{
		{ID: "old", TS: old, Kind: KindText, OK: true, Lane: LaneAPI, Tier: TierLow, CostUSD: 1, PromptExcerpt: "旧", PromptFull: "旧的完整原话", ResultFull: "旧回答"},
		{ID: "new", TS: recent, Kind: KindText, OK: true, Lane: LaneAPI, Tier: TierLow, CostUSD: 2, PromptExcerpt: "新", PromptFull: "新的完整原话", ResultFull: "新回答"},
	} {
		if err := r.ledger.Insert(c); err != nil {
			t.Fatal(err)
		}
	}
	n, err := r.ledger.PruneFull(time.Now().AddDate(0, 0, -30))
	if err != nil || n != 1 {
		t.Fatalf("应只清掉一条：%d %v", n, err)
	}
	o, _ := r.ledger.Get("old")
	nw, _ := r.ledger.Get("new")
	if o.PromptFull != "" || o.PromptExcerpt != "旧" || o.CostUSD != 1 || nw.PromptFull != "新的完整原话" {
		t.Fatalf("清理范围不对：%+v / %+v", o, nw)
	}
	// 清过的老记录不能被当成「只有摘录的老记录」再抽出来
	got, _ := r.ledger.Sample(60, 10, []string{"low"})
	for _, c := range got {
		if c.ID == "old" {
			t.Fatal("清过原话的记录不该再被抽样")
		}
	}
	if old.Month() == recent.Month() {
		if v, _ := r.ledger.MonthCost(time.Local); v != 3 {
			t.Fatalf("本月花费：%v", v)
		}
	} else if v, _ := r.ledger.MonthCost(time.Local); v != 2 {
		t.Fatalf("本月花费：%v", v)
	}
}
