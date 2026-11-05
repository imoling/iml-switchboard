package core

import (
	"strings"

	"github.com/imoling/iml-switchboard/internal/config"
)

// 应用原价：不经过 Switchboard、由应用自己调模型，这条请求要花的钱。省下的钱就是应用原价减实际花费。
// 型号按这个顺序找：应用自己的计价（WorkBuddy 的积分；应用设置里手填了单价就用手填的）→ 应用这次点名的真实型号
// → 应用设置里填的 → 接管前它自己用的 → Opus。
// 找到的型号没有价就记「未设单价」，原价按实际花费算，不算省也不算亏；不替它编价格。

// SetAppBilling 接上「应用自己的计价型号」：WorkBuddy 这种按积分收费的，原价按它的积分折算，不按型号的官方价。
func (r *Router) SetAppBilling(fn func(host, hostModel string) string) { r.appBilling = fn }

// SetBaselineDefault 接上「应用接管前用的型号」。
func (r *Router) SetBaselineDefault(fn func(host string) string) { r.baselineDefault = fn }

// AppBaseline 是一个应用的原价设置：型号、单价、单价从哪来（app 填的、内置价目表，或没有）。
type AppBaseline struct {
	Model   string        `json:"model"`
	Default string        `json:"default_model"` // 不填时用的型号
	Price   *config.Price `json:"price,omitempty"`
	Source  string        `json:"source"` // custom builtin none
}

// AppBaselineOf 返回应用当前的原价设置。
func (r *Router) AppBaselineOf(host string) AppBaseline {
	cfg := r.cfg.Get()
	def := "opus"
	if r.baselineDefault != nil && host != "" {
		if m := r.baselineDefault(host); m != "" {
			def = m
		}
	}
	it := cfg.Integrations[host]
	b := AppBaseline{Model: def, Default: def}
	if it.BaselineModel != "" {
		b.Model = it.BaselineModel
	}
	switch info, ok := r.reg.Priced(b.Model); {
	case it.BaselinePrice != nil && (it.BaselinePrice.InputPerM > 0 || it.BaselinePrice.OutputPerM > 0):
		b.Price, b.Source = it.BaselinePrice, "custom"
	case ok && info.Currency == "CNY" && r.reg.USDPerCNY() > 0:
		k := 1 / r.reg.USDPerCNY() // 人民币计价的（WorkBuddy 积分折算）按人民币显示
		b.Price, b.Source = &config.Price{InputPerM: info.InputPerM * k, OutputPerM: info.OutputPerM * k, CacheReadPerM: info.CacheReadPerM * k, Currency: "CNY"}, "builtin"
	case ok:
		b.Price, b.Source = &config.Price{InputPerM: info.InputPerM, OutputPerM: info.OutputPerM, CacheReadPerM: info.CacheReadPerM, Currency: "USD"}, "builtin"
	default:
		b.Source = "none"
	}
	return b
}

func (r *Router) priceUSD(p *config.Price, in, out, cached int) float64 {
	fresh := max(in-cached, 0)
	usd := float64(fresh)/1e6*p.InputPerM + float64(cached)/1e6*p.CacheReadPerM + float64(out)/1e6*p.OutputPerM
	if strings.EqualFold(p.Currency, "CNY") {
		usd *= r.reg.USDPerCNY()
	}
	return usd
}

// BaselineFor 算一条文字请求的应用原价：返回按哪个型号算、多少美元、有没有价。
func (r *Router) BaselineFor(host, hostModel string, in, out, cached int) (string, float64, bool) {
	if r.appBilling != nil {
		if m := r.appBilling(host, hostModel); m != "" {
			if b := r.AppBaselineOf(host); b.Source == "custom" {
				return b.Model, r.priceUSD(b.Price, in, out, cached), true
			}
			info, ok := r.reg.Priced(m)
			if !ok {
				return m, 0, false
			}
			return m, r.priceUSD(&config.Price{InputPerM: info.InputPerM, OutputPerM: info.OutputPerM, CacheReadPerM: info.CacheReadPerM}, in, out, cached), true
		}
	}
	if m := hostModel; m != "" && !strings.HasPrefix(strings.ToLower(m), "router-") {
		// 应用这次点名了真实型号（Codex 内部小请求点名 gpt-5.6-luna、用户在菜单里选了 GPT-6.1 Sol）：它自己调也是这个型号
		if info, ok := r.reg.Priced(m); ok {
			return m, r.priceUSD(&config.Price{InputPerM: info.InputPerM, OutputPerM: info.OutputPerM, CacheReadPerM: info.CacheReadPerM}, in, out, cached), true
		}
	}
	b := r.AppBaselineOf(host)
	if b.Price == nil {
		return b.Model, 0, false
	}
	return b.Model, r.priceUSD(b.Price, in, out, cached), true
}

func (r *Router) applyBaseline(c *Call, in, out, cached int) {
	model, usd, ok := r.BaselineFor(c.Host, c.HostModel, in, out, cached)
	c.BaselineModel, c.BaselineUnpriced = model, !ok
	c.BaselineCostUSD = usd
	if !ok {
		c.BaselineCostUSD = c.CostUSD // 没有价：不算省也不算亏
	}
}

// RecomputeBaselines 按现在的原价设置重算文字记录的应用原价；host 为空表示全部应用。返回改了几条。
func (r *Router) RecomputeBaselines(host string) (int, error) {
	return r.ledger.RewriteBaselines(host, func(c *Call) {
		r.applyBaseline(c, c.InputTokens, c.OutputTokens, c.CacheReadTokens)
	})
}

// decorateBaselinePrice 填上原价用的单价，记录表里「用量 × 单价 ≈ 原价」一眼能对上。
func (r *Router) decorateBaselinePrice(c *Call) {
	if c.Kind != KindText {
		if c.Units > 0 && c.BaselineCostUSD > 0 {
			c.BaselinePerUnitUSD = c.BaselineCostUSD / c.Units
		}
		return
	}
	if c.BaselineUnpriced || c.BaselineModel == "" {
		return
	}
	if b := r.AppBaselineOf(c.Host); b.Source == "custom" && b.Model == c.BaselineModel {
		c.BaselinePrice = b.Price
		return
	}
	info, ok := r.reg.Priced(c.BaselineModel)
	if !ok {
		return
	}
	p := &config.Price{InputPerM: info.InputPerM, OutputPerM: info.OutputPerM, CacheReadPerM: info.CacheReadPerM, Currency: "USD"}
	if info.Currency == "CNY" && r.reg.USDPerCNY() > 0 {
		k := 1 / r.reg.USDPerCNY()
		p = &config.Price{InputPerM: info.InputPerM * k, OutputPerM: info.OutputPerM * k, CacheReadPerM: info.CacheReadPerM * k, Currency: "CNY"}
	}
	c.BaselinePrice = p
}
