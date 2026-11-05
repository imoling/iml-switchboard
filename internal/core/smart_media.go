package core

import (
	"context"
	"fmt"
	"sort"
)

// 出图、出视频的智能分派：只在「档位候选 → 生图 / 生视频」里勾选的型号里挑（没手排就是现在能用的），
// 判官的概率 × 顺位系数（排第一 1、第二 0.7、第三 0.5、再往后 0.4）：你排在前面的天然占优，
// 判官要明显更看好别的才换（第二个要高出约六成对四成）。
// 点名了型号、指定了档位、能用的只有一个、判官出错，都照你排的顺序走。

const mediaImageInstructions = `为这张图挑一个出图模型：能画好的前提下越便宜越好。` +
	`图标、示意图、草图、简单插画选便宜快的；海报、封面、写实照片、要在图里准确写字的选效果好的；` +
	`带参考图时要擅长照着参考图改。`

const mediaVideoInstructions = `为这段视频挑一个出视频模型：能做好的前提下越便宜越好。` +
	`简单的镜头、示意动画选便宜快的；写实、人物、长镜头、要高分辨率的选效果好的；带首帧图时要擅长图生视频。`

func rankWeight(i int) float64 {
	switch i {
	case 0:
		return 1
	case 1:
		return 0.7
	case 2:
		return 0.5
	}
	return 0.4
}

// describeMedia 是给判官看的一个出图、出视频型号：名字、在你顺序里第几、价钱。
func (r *Router) describeMedia(c Target, rank int, kind Kind) string {
	price := "价格未知"
	switch {
	case c.Provider == "codex":
		price = "订阅额度，不另花钱"
	case r.reg.IsFree(c.Model):
		price = "免费"
	case c.EstCostUSD > 0:
		unit := "每张"
		if kind == KindVideo {
			unit = "这一段"
		}
		price = fmt.Sprintf("%s约 $%.3f", unit, c.EstCostUSD)
	}
	return fmt.Sprintf("%s（%s，你排的第 %d 个）：%s", r.reg.Display(c.Model), providerLabel(c.Provider), rank, price)
}

func (r *Router) smartPickMedia(ctx context.Context, t *Task, d *Decision, force bool) {
	cfg := r.cfg.Get()
	note := func(s string) {
		d.Reason = append(d.Reason, s)
		d.Smart = &SmartPick{Note: s}
	}
	if !cfg.Routing.Smart.Enabled && !force {
		return // 没打开就照顺序，不在理由里多说一句
	}
	if t.Model != "" || t.MediaTier != "" {
		note("智能分派：指定了型号或档位，照指定的走")
		return
	}
	var pool []Target
	for _, c := range d.Candidates {
		if c.Skipped == "" {
			pool = append(pool, c)
		}
	}
	if len(pool) < 2 {
		note("智能分派：只有一个能用的" + kindLabel(d.Kind) + "型号，不用选")
		return
	}
	criteria := map[string]string{}
	for i, c := range pool {
		criteria[fmt.Sprint(i+1)] = r.describeMedia(c, i+1, d.Kind)
	}
	ins := mediaImageInstructions
	if d.Kind == KindVideo {
		ins = mediaVideoInstructions
	}
	signals := map[string]any{"kind": kindLabel(d.Kind), "has_reference_image": len(t.ImageRefs()) > 0}
	if d.Kind == KindVideo {
		signals["seconds"] = t.Seconds
	}
	probs, err := r.askJudge(ctx, cfg, t, StripInjected(t.Prompt), pool, signals, criteria, ins)
	if err != nil {
		note("智能分派：" + oneLine(err) + "，照你排的顺序")
		return
	}
	opts := make([]SmartOption, len(pool))
	jevTop := 0
	for i, c := range pool {
		p := probs[fmt.Sprint(i+1)]
		w := rankWeight(i)
		opts[i] = SmartOption{Model: c.Model, Display: r.reg.Display(c.Model), Lane: c.Lane, Provider: c.Provider, Prob: p, Weight: w, Score: p * w, Rank: i + 1}
		if p > opts[jevTop].Prob {
			jevTop = i
		}
	}
	order := make([]int, len(pool))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return opts[order[a]].Score > opts[order[b]].Score })
	var ranked []Target
	var shown []SmartOption
	for _, i := range order {
		ranked = append(ranked, pool[i])
		shown = append(shown, opts[i])
	}
	for _, c := range d.Candidates {
		if c.Skipped != "" {
			ranked = append(ranked, c)
		}
	}
	d.Candidates = ranked
	d.Smart = &SmartPick{Options: shown}
	best := opts[order[0]]
	if order[0] == jevTop {
		d.Reason = append(d.Reason, fmt.Sprintf("智能分派：判官选 %s（%.0f%%）", best.Display, best.Prob*100))
	} else {
		d.Reason = append(d.Reason, fmt.Sprintf("智能分派：判官首选 %s（%.0f%%），按你排的顺序加权后用 %s", opts[jevTop].Display, opts[jevTop].Prob*100, best.Display))
	}
}
