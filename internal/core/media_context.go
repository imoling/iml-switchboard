package core

import (
	"context"
	"strings"
	"time"
)

// 对话里说「根据这个文案画一张」时，出图模型只看得到这一句：上文的人物、场景、文案都丢了，画出来的东西对不上。
// 出图出视频前先把上文并进提示词：请低档模型改写成一段自足的提示词；改写不成就把上文原样拼在前面。

const (
	mediaCtxRunes     = 8000 // 上文最多带这么多字，从最近的往前取
	mediaCtxFallback  = 3000 // 改写失败时直接拼上文，拼这么多
	mediaPromptRunes  = 2000 // 改写结果的上限：出图接口的提示词有长度限制
	mediaCtxTimeout   = 40 * time.Second
	mediaRewriteHeads = "你在替出图模型写提示词。下面是一段对话的上文和用户最后的请求。\n" +
		"把请求改写成一段能直接交给%s模型的完整提示词：请求里指代的上文内容（人物外貌、场景、分镜、台词、风格）要具体写进去；" +
		"请求本身已经完整、跟上文无关时原样输出。只输出提示词本身，不加解释和标题，不超过 800 字；用户用中文就写中文。\n\n"
)

// mediaContext 是最后一条用户消息之前的对话正文（不含工具往来），从近往远取，最多 limit 字；没有上文返回空。
func mediaContext(t *Task, limit int) string {
	last := -1
	for i := len(t.Messages) - 1; i >= 0; i-- {
		if t.Messages[i].Role == "user" {
			last = i
			break
		}
	}
	var parts []string
	n := 0
	for i := last - 1; i >= 0 && n < limit; i-- {
		m := t.Messages[i]
		if (m.Role != "user" && m.Role != "assistant") || m.ToolCallID != "" || m.ToolImage {
			continue
		}
		text := strings.TrimSpace(StripInjected(m.Content))
		if text == "" {
			continue
		}
		who := "用户"
		if m.Role == "assistant" {
			who = "助手"
		}
		r := []rune(text)
		if left := limit - n; len(r) > left {
			r = r[len(r)-left:]
		}
		n += len(r)
		parts = append(parts, who+"："+string(r))
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "\n\n")
}

// withMediaContext 在出图出视频前把对话上文并进 t.Prompt；原话留在 t.Ask 里记账。没有上文时什么都不做。
func (r *Router) withMediaContext(ctx context.Context, t *Task, d *Decision) {
	if d.Kind != KindImage && d.Kind != KindVideo {
		return
	}
	history := mediaContext(t, mediaCtxRunes)
	if history == "" {
		return
	}
	ask := StripInjected(t.Prompt)
	what := "出图"
	if d.Kind == KindVideo {
		what = "出视频"
	}
	ctx, cancel := context.WithTimeout(ctx, mediaCtxTimeout)
	defer cancel()
	q := &Task{ID: NewID("mctx"), Kind: KindText, Tier: TierLow, Source: "classifier", MaxTokens: 1500,
		Prompt: strings.Replace(mediaRewriteHeads, "%s", what, 1) + "【上文】\n" + history + "\n\n【请求】\n" + ask}
	cfg := r.cfg.Get()
	var why []string
	tried := 0
	for _, c := range r.textCandidates(cfg, q, TierLow, []string{"api", "codex_sub", "agy_sub", "claude_sub"}, false, &why) {
		if c.Skipped != "" {
			continue
		}
		drv, ok := r.drivers[c.Lane]
		if !ok {
			continue
		}
		if tried++; tried > 2 || ctx.Err() != nil {
			break
		}
		res, err := drv.Run(ctx, q, c, nil)
		if err != nil {
			continue
		}
		out := strings.TrimSpace(res.Text)
		if out == "" {
			continue
		}
		if rs := []rune(out); len(rs) > mediaPromptRunes {
			out = string(rs[:mediaPromptRunes])
		}
		m := c.Model
		if res.Model != "" {
			m = res.Model
		}
		t.Ask, t.Prompt = ask, out
		t.JudgeCostUSD += res.Usage.CostUSD
		d.Reason = append(d.Reason, "提示词带上了对话上文（"+r.reg.Display(m)+" 改写）")
		return
	}
	t.Ask, t.Prompt = ask, "参考下面的上文完成请求。\n【上文】\n"+mediaContext(t, mediaCtxFallback)+"\n\n【请求】\n"+ask
	d.Reason = append(d.Reason, "提示词带上了对话上文")
}
