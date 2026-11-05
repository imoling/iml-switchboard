package core

import (
	"testing"
	"time"

	"github.com/imoling/iml-switchboard/internal/config"
)

// 应用原价按这个顺序找型号：这次点名的真实型号 → 应用设置 → 接管前的型号 → Opus；没有价就不算省
func TestAppBaseline(t *testing.T) {
	r, _, _ := newTestRouter(t)
	r.SetBaselineDefault(func(host string) string {
		if host == "codex" {
			return "gpt-9-unknown" // 价目表里没有
		}
		return ""
	})
	luna, _ := r.reg.Priced("gpt-5.6-luna")
	m, usd, ok := r.BaselineFor("codex", "gpt-5.6-luna", 1_000_000, 0, 0)
	if !ok || m != "gpt-5.6-luna" || usd != luna.InputPerM {
		t.Fatalf("点名 luna 应按 luna 算：%s %v %v", m, usd, ok)
	}
	if m, _, ok := r.BaselineFor("codex", "router-auto", 1000, 1000, 0); ok || m != "gpt-9-unknown" {
		t.Fatalf("路由型号按应用默认型号，gpt-9-unknown 没价应标未设单价：%s %v", m, ok)
	}
	// codex-auto-review 按账单反算出是 gpt-5.6-luna 的价
	if m, _, ok := r.BaselineFor("codex", "codex-auto-review", 1000, 1000, 0); !ok || m != "codex-auto-review" {
		t.Fatalf("Codex 自动审批应按估的价算：%s %v", m, ok)
	}
	if m, _, ok := r.BaselineFor("codex", "gpt-reserve", 1000, 1000, 0); ok || m != "gpt-9-unknown" {
		t.Fatalf("价目表里没有的名字落到应用默认：%s %v", m, ok)
	}
	// 自动发现的型号以 0 价登记过，不能拿来当有价
	r.reg.Upsert(ModelInfo{ID: "gpt-6", Provider: "custom"})
	if _, _, ok := r.BaselineFor("codex", "gpt-6", 1000, 1000, 0); ok {
		t.Fatal("0 价登记的型号不算有价")
	}
	// 没有应用信息的请求按 Opus
	if m, _, ok := r.BaselineFor("", "", 1000, 1000, 0); !ok || m != "opus" {
		t.Fatalf("没有应用时按 Opus：%s %v", m, ok)
	}
	// 应用设置里填了单价（人民币）
	_ = r.cfg.Update(func(c *config.Config) error {
		c.Integrations["codex"] = config.Integration{BaselinePrice: &config.Price{InputPerM: 10, OutputPerM: 30, Currency: "CNY"}}
		return nil
	})
	m, usd, ok = r.BaselineFor("codex", "router-auto", 1_000_000, 1_000_000, 0)
	if want := 40 * r.reg.USDPerCNY(); !ok || m != "gpt-9-unknown" || usd < want-1e-9 || usd > want+1e-9 {
		t.Fatalf("手填的人民币单价要折成美元：%s %v %v，想要 %v", m, usd, ok, want)
	}
}

// 没有价的记录原价记成实际花费；重算两次结果一样
func TestRecomputeBaselines(t *testing.T) {
	r, _, _ := newTestRouter(t)
	r.SetBaselineDefault(func(string) string { return "gpt-9-unknown" })
	now := time.Now()
	for i, hm := range []string{"router-auto", "gpt-5.6-luna"} {
		c := &Call{ID: NewID("c"), TS: now.Add(time.Duration(i) * time.Second), Host: "codex", HostModel: hm, Kind: KindText, Lane: LaneAPI, Model: "gpt-5.6-luna", OK: true,
			InputTokens: 2000, OutputTokens: 500, CostUSD: 0.001, BaselineCostUSD: 99}
		if err := r.ledger.Insert(c); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := r.RecomputeBaselines(""); err != nil || n != 2 {
		t.Fatalf("应改两条：%d %v", n, err)
	}
	if n, _ := r.RecomputeBaselines(""); n != 0 {
		t.Fatalf("第二次不该再改：%d", n)
	}
	calls, _ := r.ledger.List(ListFilter{Limit: 10})
	for _, c := range calls {
		switch c.HostModel {
		case "router-auto":
			if !c.BaselineUnpriced || c.BaselineCostUSD != c.CostUSD || c.BaselineModel != "gpt-9-unknown" {
				t.Fatalf("没价的记录原价应等于实际花费：%+v", c)
			}
		case "gpt-5.6-luna":
			if c.BaselineUnpriced || c.BaselineModel != "gpt-5.6-luna" || c.BaselineCostUSD <= 0 {
				t.Fatalf("点名 luna 的应按 luna 算：%+v", c)
			}
		}
	}
	ov, _ := r.ledger.Aggregate(7, time.Local)
	if ov.BaselineUnpriced != 1 {
		t.Fatalf("总览要报出没算进来的条数：%d", ov.BaselineUnpriced)
	}
}

// 价目表和官方价目页、Token Factory 账单对得上（2026-11-05）
func TestOpenAIPrices(t *testing.T) {
	r := NewRegistry(1.0 / 7.1)
	near := func(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }
	// Token Factory 用量页的三条
	for _, c := range []struct {
		model           string
		in, cached, out int
		want            float64
	}{{"gpt-5.6-luna", 18839, 0, 1090, 0.005076}, {"gpt-6-luna", 365, 0, 25, 0.000049}, {"codex-auto-review", 103800 + 399, 103800, 292, 0.0025062}} {
		if got := r.TextCost(c.model, c.in, c.out, c.cached); !near(got, c.want) && (got-c.want > 1e-6 || c.want-got > 1e-6) {
			t.Errorf("%s 应为 $%.7f，得到 $%.7f", c.model, c.want, got)
		}
	}
	for _, id := range []string{"gpt-5.5", "gpt-6.1-terra", "gpt-6-sol", "gpt-5.6", "gpt-6", "gpt-5.3-codex-spark", "gpt-5.2-2025-12-11"} {
		if _, ok := r.Priced(id); !ok {
			t.Errorf("%s 应有价", id)
		}
	}
}

// WorkBuddy 按它自己的积分计价：点名 gpt-6.1-sol 也不按 OpenAI 的价，按积分折算
func TestAppOwnBilling(t *testing.T) {
	r, _, _ := newTestRouter(t)
	cny := 7.65 * r.reg.USDPerCNY()
	r.reg.Upsert(ModelInfo{ID: "workbuddy:balanced-model", Provider: "workbuddy", Display: "WorkBuddy 均衡", InputPerM: cny, OutputPerM: cny, CacheReadPerM: cny, Currency: "CNY"})
	r.SetAppBilling(func(host, hm string) string {
		if host == "workbuddy" {
			return "workbuddy:balanced-model"
		}
		return ""
	})
	m, usd, ok := r.BaselineFor("workbuddy", "gpt-6.1-sol", 900_000, 100_000, 800_000)
	if !ok || m != "workbuddy:balanced-model" || usd < cny-1e-9 || usd > cny+1e-9 {
		t.Fatalf("100 万 token 按均衡档应是 ¥7.65：%s %v %v", m, usd, ok)
	}
	if m, _, _ := r.BaselineFor("codex", "gpt-6.1-sol", 1000, 1000, 0); m != "gpt-6.1-sol" {
		t.Fatalf("别的应用照旧按点名的型号：%s", m)
	}
	if b := r.AppBaselineOf("workbuddy"); b.Price != nil && b.Price.Currency != "" && b.Model == "workbuddy:balanced-model" && b.Price.Currency != "CNY" {
		t.Fatalf("积分折算的价按人民币显示：%+v", b.Price)
	}
}

// 回看评估是 Switchboard 自己发的请求：总览不算它的次数和原价，实际花的钱照记
func TestOverviewSkipsEvaluation(t *testing.T) {
	r, _, _ := newTestRouter(t)
	now := time.Now()
	for i, c := range []*Call{
		{Source: "facade", Host: "codex", BaselineCostUSD: 1, CostUSD: 0.2},
		{Source: EvaluationSource, BaselineCostUSD: 5, CostUSD: 0.3},
	} {
		c.ID, c.TS, c.Kind, c.Lane, c.OK, c.Tier = NewID("c"), now.Add(time.Duration(i)*time.Second), KindText, LaneAPI, true, TierLow
		if err := r.ledger.Insert(c); err != nil {
			t.Fatal(err)
		}
	}
	ov, err := r.ledger.Aggregate(1, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Calls != 1 || ov.BaselineCostUSD != 1 || ov.APICostUSD < 0.4999 || ov.APICostUSD > 0.5001 {
		t.Fatalf("评估只算花费：次数 %d 原价 %v 花费 %v", ov.Calls, ov.BaselineCostUSD, ov.APICostUSD)
	}
	if d := ov.SavedUSD - (ov.BaselineCostUSD - ov.APICostUSD); d > 1e-9 || d < -1e-9 {
		t.Fatalf("省下应等于原价减实际花费：%v", ov.SavedUSD)
	}
}
