package core

import (
	"context"
	"crypto/sha256"
	"regexp"
	"strings"
	"sync"
	"time"
)

// 规则只认关键词，「CI 里这条测试十次挂一次」这种没有关键词的难题会落到短问答。
// 规则拿不准时（落到短问答或启发式兜底），先请低档模型判一次档，判不出来就照规则走。
// 盲测集（testdata/holdout.yaml）上：规则 65%，低档模型批量判 60/60。

const (
	classifyTimeout = 10 * time.Second
	classifyExcerpt = 1500 // 只给判档员看开头这么多字
	classifyCacheN  = 256
)

const classifyPrompt = `你是任务分档员。编程助手会把子任务派给不同价位的模型，你只判断这条任务该用哪一档，不回答任务本身。

low：翻译、改写、润色、摘要、归类、抽取、格式转换、查一个命令或一个事实
mid：写一般的代码（函数、脚本、组件、测试）、代码评审、解释技术原理、比较方案、排查常见小问题
high：疑难排查（偶发、反复失败、查了很久、性能退化、泄漏）、架构与系统设计、并发、安全、分布式、需要深入推理和权衡
image：真的要生成一张图片；video：真的要生成一段视频；animation：真的要做一段动效视频（片头、转场、数据动画）
修图片接口、视频播放器卡顿、CSS 动画掉帧这类是写代码，按 mid 或 high 判。拿不准时宁高勿低。

只输出一个词：low、mid、high、image、video、animation 之一。

任务：
`

var reLabel = regexp.MustCompile(`\b(low|mid|high|image|video|animation)\b`)

// ParseLabel 从判档员的回答里取第一个合法档位，取不到返回空。
func ParseLabel(s string) string {
	return reLabel.FindString(strings.ToLower(s))
}

type classifyCache struct {
	mu    sync.Mutex
	m     map[[32]byte]string
	order [][32]byte
}

func (c *classifyCache) get(k [32]byte) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v, ok
}

func (c *classifyCache) put(k [32]byte, v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[[32]byte]string{}
	}
	if _, ok := c.m[k]; !ok {
		c.order = append(c.order, k)
		if len(c.order) > classifyCacheN {
			delete(c.m, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.m[k] = v
}

// classify 请低档模型判档。不进账本的「最近请求」，花费加到原请求上；失败、超时、乱答都返回空。
func (r *Router) classify(ctx context.Context, prompt string) (label string, costUSD float64, model string) {
	ask := excerpt(StripInjected(prompt), classifyExcerpt)
	key := sha256.Sum256([]byte(ask))
	if v, ok := r.clsCache.get(key); ok {
		return v, 0, ""
	}
	ctx, cancel := context.WithTimeout(ctx, classifyTimeout)
	defer cancel()
	t := &Task{ID: NewID("cls"), Prompt: classifyPrompt + ask, Tier: TierLow, Kind: KindText, Source: "classifier", MaxTokens: 8}
	cfg := r.cfg.Get()
	// 不走 Decide：判档提示词里本身就有「架构」「视频」这些词，会被任务表认成别的类型，渠道顺序跟着变
	var why []string
	cands := r.textCandidates(cfg, t, TierLow, []string{"api", "codex_sub", "agy_sub", "claude_sub"}, false, &why)
	overBudget := false
	if cfg.Settings.MonthlyAPILimitUSD > 0 {
		if spent, err := r.ledger.MonthCost(r.loc); err == nil && spent >= cfg.Settings.MonthlyAPILimitUSD {
			overBudget = true
		}
	}
	tried := 0
	for _, c := range cands {
		if c.Skipped != "" || (overBudget && c.Lane == LaneAPI) {
			continue
		}
		drv, ok := r.drivers[c.Lane]
		if !ok {
			continue
		}
		if tried++; tried > 2 || ctx.Err() != nil {
			break
		}
		res, err := drv.Run(ctx, t, c, nil)
		if err != nil {
			continue
		}
		if l := ParseLabel(res.Text); l != "" {
			r.clsCache.put(key, l)
			m := c.Model
			if res.Model != "" {
				m = res.Model
			}
			return l, res.Usage.CostUSD, m
		}
	}
	return "", 0, ""
}
