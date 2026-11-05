package media

import (
	"strings"

	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/lanes/api"
)

// Price 是媒体模型的单价：图片按张、视频按秒。USD 与 CNY 只会有一个非零，另一个按汇率折算。
type Price struct {
	Key  string // 匹配用的模型关键字（已归一化）
	Unit string // image / second
	USD  float64
	CNY  float64
}

// priceTable 按「更长的关键字优先」排列，归一化后用子串匹配。
var priceTable = []Price{
	// gpt-image 按 1024×1024 一张算（2026-11-05 官方出图指南）。2.5 Flare / Sunburst 和 gpt-image-2 的 token 单价相同，按 gpt-image-2 的张价
	{Key: "gpt.image.2.5:low", Unit: "image", USD: 0.006},
	{Key: "gpt.image.2.5:medium", Unit: "image", USD: 0.053},
	{Key: "gpt.image.2.5:high", Unit: "image", USD: 0.211},
	{Key: "gpt.image.1.5:low", Unit: "image", USD: 0.009},
	{Key: "gpt.image.1.5:medium", Unit: "image", USD: 0.034},
	{Key: "gpt.image.1.5:high", Unit: "image", USD: 0.133},
	{Key: "gpt.image.1:low", Unit: "image", USD: 0.011},
	{Key: "gpt.image.1:medium", Unit: "image", USD: 0.042},
	{Key: "gpt.image.1:high", Unit: "image", USD: 0.167},
	{Key: "nano.banana.2.1", Unit: "image", USD: 0.045},
	{Key: "seedream.5.0.flash", Unit: "image", CNY: 0.12},
	{Key: "seedream.5.0.pro", Unit: "image", CNY: 0.30},
	{Key: "seedance.2.0.fast", Unit: "second", CNY: 0.80},
	{Key: "seedance.2.5", Unit: "second", CNY: 1.51},
	{Key: "seedance.2.0", Unit: "second", CNY: 0.99},
	{Key: "veo.3.1.lite", Unit: "second", USD: 0.05},
	{Key: "veo.3.1", Unit: "second", USD: 0.40},
	{Key: "wan3.0", Unit: "second", CNY: 0.6},
	{Key: "wan.3.0", Unit: "second", CNY: 0.6},
	{Key: "codex.imagegen", Unit: "image"},
}

// normalizeModel 把 - 和 _ 统一成 .，方便匹配带日期后缀、带厂商前缀的模型名。
func normalizeModel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("-", ".", "_", ".").Replace(s)
	return s
}

// PriceFor 查模型单价；quality 只对 gpt-image 系列有意义（low/medium/high，空则按模型名后缀，再默认 high）。
func PriceFor(model, quality string) (Price, bool) {
	n := normalizeModel(model)
	if strings.Contains(n, "gpt.image") {
		q := strings.ToLower(quality)
		if q == "" {
			switch {
			case strings.HasSuffix(n, ".low"), strings.Contains(n, ".low."):
				q = "low"
			default:
				q = "high"
			}
		}
		if q != "low" && q != "medium" {
			q = "high"
		}
		family := "gpt.image.2.5" // gpt-image-2 和 2.5 同价
		switch {
		case strings.Contains(n, "gpt.image.1.5"):
			family = "gpt.image.1.5"
		case strings.Contains(n, "gpt.image.1"):
			family = "gpt.image.1"
		}
		for _, p := range priceTable {
			if p.Key == family+":"+q {
				return p, true
			}
		}
	}
	for _, p := range priceTable {
		if strings.Contains(p.Key, ":") {
			continue
		}
		if strings.Contains(n, p.Key) {
			return p, true
		}
	}
	return Price{}, false
}

// UsageFor 按单价算用量：图片 units=张数，视频 units=秒数。
func UsageFor(model, quality string, units float64) core.Usage {
	p, ok := PriceFor(model, quality)
	u := core.Usage{Units: units, Unit: p.Unit}
	if !ok {
		return u
	}
	switch {
	case p.CNY > 0:
		u.Currency = "CNY"
		u.CostNative = p.CNY * units
		u.CostUSD = u.CostNative / api.USDPerCNY
	default:
		u.Currency = "USD"
		u.CostUSD = p.USD * units
		u.CostNative = u.CostUSD
	}
	return u
}
