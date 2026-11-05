package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/imoling/iml-switchboard/internal/config"
)

// 智能分派：分派方式选了智能分派（或应用点名 router-smart）时，自动分档的请求从能用的模型里请判官挑一个。
// 硬条件先筛：带工具只留按量付费、带图只留能看图的、装得下、没在冷却——这些由 textCandidates 标好，不交给 Jev。
// 软权重再乘：Jev 的概率 × 跟规则判的档位差几档（同档 1、差一档 0.7、差两档 0.4）× 调度模式的价格偏好。
// 顶配不进候选；工具回传的后续回合、会话粘住时沿用上次的选择；判官没回就按规则走。

// SmartJudge 问 Jev 一道选择题：task 是剥过注入块的原话（由实现方脱敏、截断），signals 是确定性信号，
// criteria 是选项 id → 描述。返回每个选项的概率。
type SmartJudge func(ctx context.Context, task string, signals map[string]any, criteria map[string]string, instructions string) (map[string]float64, error)

// SetSmartJudge 接上 Jev；nil 表示没配密钥。
func (r *Router) SetSmartJudge(fn SmartJudge) { r.smartJudge = fn }

// SmartOption 是智能分派里一个候选的打分明细。
type SmartOption struct {
	Model    string  `json:"model"`
	Display  string  `json:"display"`
	Tier     Tier    `json:"tier"`
	Lane     LaneID  `json:"lane"`
	Provider string  `json:"provider"`
	Prob     float64 `json:"prob"`           // Jev 给的概率
	Weight   float64 `json:"weight"`         // 档位系数 × 调度模式系数；出图出视频是顺位系数
	Rank     int     `json:"rank,omitempty"` // 出图出视频：在你排的顺序里第几个
	Score    float64 `json:"score"`
}

// SmartPick 是一次智能分派的结果；Note 非空表示没请 Jev（没打开、没密钥、沿用上次……）。
type SmartPick struct {
	Options []SmartOption `json:"options,omitempty"`
	Note    string        `json:"note,omitempty"`
}

const smartExcerpt = 1500

const smartInstructions = `为这条任务挑一个模型：能做好的前提下越便宜越好。` +
	`翻译、改写、摘要、归类、短问答选小模型；写一般的代码、解释原理、代码评审选中等的；` +
	`疑难排查、架构设计、并发与安全、长链推理选强的。请求带图时看图能力要够，带工具时要擅长调工具。`

// tierWeight 是候选档位离规则判的档位越远越打折：规则是先验，Jev 只在它附近挪。
func tierWeight(rule, cand Tier) float64 {
	d := tierRank(cand) - tierRank(rule)
	if d < 0 {
		d = -d
	}
	switch d {
	case 0:
		return 1
	case 1:
		return 0.7
	}
	return 0.4
}

// modeWeight 是调度模式的价格偏好：省钱偏低档，质量偏高档，均衡不偏。
func modeWeight(m Mode, t Tier) float64 {
	k := []float64{1, 0.85, 0.7}
	switch m {
	case ModeSave:
		return k[min(max(tierRank(t)-1, 0), 2)]
	case ModeQuality:
		return k[min(max(3-tierRank(t), 0), 2)]
	}
	return 1
}

func imageCount(t *Task) int {
	n := 0
	if t.ImagePath != "" {
		n++
	}
	for _, m := range t.Messages {
		n += len(m.Images)
	}
	return n
}

// smartPool 是这条请求能用的全部候选：低、中、高三档里没被跳过的，按应用自己的档位候选表取，去重，标上档位。
func (r *Router) smartPool(cfg *config.Config, t *Task) []Target {
	var pool []Target
	hasImage := imageCount(t) > 0
	for _, tier := range []Tier{TierLow, TierMid, TierHigh} {
		var ignore []string
		for _, c := range r.textCandidates(cfg, t, tier, preferFor(tier, defaultPrefer(tier)), hasImage, &ignore) {
			if c.Skipped != "" || hasTarget(pool, c) {
				continue
			}
			c.Tier = tier
			pool = append(pool, c)
		}
	}
	return pool
}

// describe 是给 Jev 看的一个候选：型号、档位、能力、价钱。
func (r *Router) describe(c Target) string {
	info, _ := r.reg.Lookup(c.Model)
	name := r.reg.Display(c.Model)
	var caps []string
	if info.Vision {
		caps = append(caps, "能看图")
	}
	if info.Context > 0 {
		caps = append(caps, fmt.Sprintf("上下文 %dK", info.Context/1000))
	}
	price := "订阅额度，不另花钱"
	if c.Lane == LaneAPI {
		switch {
		case info.Free:
			price = "免费"
		case info.InputPerM > 0 || info.OutputPerM > 0:
			price = fmt.Sprintf("每百万 token 输入 $%g、输出 $%g", info.InputPerM, info.OutputPerM)
		default:
			price = "价格未知"
		}
	}
	if len(caps) == 0 {
		return fmt.Sprintf("%s（%s）：%s", name, TierLabel(c.Tier), price)
	}
	return fmt.Sprintf("%s（%s）：%s；%s", name, TierLabel(c.Tier), strings.Join(caps, "、"), price)
}

// DecideSmart 是智能分派的决策，给「试一句」和测试按钮用；force 为真时不看开关（测试密钥用）。
func (r *Router) DecideSmart(ctx context.Context, t *Task, force bool) *Decision {
	t.Smart = true
	d := r.Decide(t)
	switch d.Kind {
	case KindText:
		r.smartPickWith(ctx, t, d, force)
	case KindImage, KindVideo:
		r.smartPickMedia(ctx, t, d, force)
	}
	return d
}

func (r *Router) smartPick(ctx context.Context, t *Task, d *Decision) {
	r.smartPickWith(ctx, t, d, false)
}

func (r *Router) smartPickWith(ctx context.Context, t *Task, d *Decision, force bool) {
	cfg := r.cfg.Get()
	skip := func(note string) {
		d.Reason = append(d.Reason, note)
		d.Smart = &SmartPick{Note: note}
	}
	switch {
	case !d.Auto:
		skip("智能分派：指定了档位或型号，照指定的走") // 只有点名 router-smart 又带了档位时才走到这里
		return
	case !cfg.Routing.Smart.Enabled && !force:
		skip("智能分派没打开，按规则走")
		return
	case r.smartJudge == nil && cfg.Routing.Smart.JudgeOrDefault() != "model":
		skip("智能分派没接好判官，按规则走")
		return
	case d.Sticky || !t.FreshTurn():
		skip("智能分派：同一会话沿用上次的选择")
		return
	}
	pool := r.smartPool(cfg, t)
	if len(pool) < 2 {
		skip("智能分派：只有一个能用的模型，不用选")
		return
	}
	ask := StripInjected(t.Prompt)
	criteria := map[string]string{}
	for i, c := range pool {
		criteria[fmt.Sprint(i+1)] = r.describe(c)
	}
	signals := map[string]any{"has_tools": t.HasTools(), "has_image": imageCount(t) > 0, "context_tokens": t.EstTokens, "rule_tier": TierLabel(d.Tier)}
	probs, err := r.askJudge(ctx, cfg, t, ask, pool, signals, criteria, smartInstructions)
	if err != nil {
		skip("智能分派：" + oneLine(err) + "，按规则走")
		return
	}
	mode := r.Mode()
	opts := make([]SmartOption, len(pool))
	for i, c := range pool {
		w := tierWeight(d.Tier, c.Tier) * modeWeight(mode, c.Tier)
		p := probs[fmt.Sprint(i+1)]
		opts[i] = SmartOption{Model: c.Model, Display: r.reg.Display(c.Model), Tier: c.Tier, Lane: c.Lane, Provider: c.Provider, Prob: p, Weight: w, Score: p * w}
	}
	order := make([]int, len(pool))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return opts[order[a]].Score > opts[order[b]].Score })
	jevTop := 0
	for i := range opts {
		if opts[i].Prob > opts[jevTop].Prob {
			jevTop = i
		}
	}
	best := opts[order[0]]
	var ranked []Target
	var shown []SmartOption
	for _, i := range order {
		c := pool[i]
		c.Why = "智能分派用" + TierLabel(c.Tier)
		ranked = append(ranked, c)
		shown = append(shown, opts[i])
	}
	// 筛掉的候选留在后面，报错时还能看到为什么不能用
	for _, c := range d.Candidates {
		if c.Skipped != "" && !hasTarget(ranked, c) {
			ranked = append(ranked, c)
		}
	}
	d.Candidates = ranked
	d.Smart = &SmartPick{Options: shown}
	if order[0] == jevTop {
		d.Reason = append(d.Reason, fmt.Sprintf("智能分派：Jev 选 %s（%.0f%%）", best.Display, best.Prob*100))
	} else {
		d.Reason = append(d.Reason, fmt.Sprintf("智能分派：Jev 首选 %s（%.0f%%），按档位加权后用 %s（%.0f%% × %.2f）", opts[jevTop].Display, opts[jevTop].Prob*100, best.Display, best.Prob*100, best.Weight))
	}
}

// askJudge 问判官，带缓存：同一句话、同一组候选不重复问。
func (r *Router) askJudge(ctx context.Context, cfg *config.Config, t *Task, ask string, pool []Target, signals map[string]any, criteria map[string]string, instructions string) (map[string]float64, error) {
	var idKey []string
	for _, c := range pool {
		idKey = append(idKey, c.AccountID+"/"+c.Model)
	}
	key := sha256.Sum256([]byte(excerpt(ask, smartExcerpt) + "\x00" + strings.Join(idKey, "|") + "\x00" + instructions))
	if v, ok := r.smartCache.get(key); ok {
		var probs map[string]float64
		if json.Unmarshal([]byte(v), &probs) == nil {
			return probs, nil
		}
	}
	judge := r.smartJudge
	if s := cfg.Routing.Smart; s.JudgeOrDefault() == "model" {
		judge = r.modelJudge(t, s.JudgeAccount, s.JudgeModel)
	}
	if judge == nil {
		return nil, errors.New("智能分派没接好判官")
	}
	p, err := judge(ctx, excerpt(ask, smartExcerpt), signals, criteria, instructions)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(p)
	r.smartCache.put(key, string(b))
	return p, nil
}

// modelJudge 用一个已有的按量付费型号当判官：让它给每个候选打合适度，归一化成概率。花的钱记到这次请求上。
func (r *Router) modelJudge(t *Task, account, model string) SmartJudge {
	return func(ctx context.Context, task string, signals map[string]any, criteria map[string]string, instructions string) (map[string]float64, error) {
		if model == "" {
			return nil, errors.New("还没选判官型号")
		}
		drv, ok := r.drivers[LaneAPI]
		if !ok {
			return nil, errors.New("按量付费渠道没启用")
		}
		ids := make([]string, 0, len(criteria))
		for id := range criteria {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(a, b int) bool { return len(ids[a]) < len(ids[b]) || len(ids[a]) == len(ids[b]) && ids[a] < ids[b] })
		var sb strings.Builder
		sb.WriteString("你是模型调度员，只打分，不回答任务本身。" + instructions + "\n\n候选：\n")
		for _, id := range ids {
			sb.WriteString(id + ". " + criteria[id] + "\n")
		}
		sig, _ := json.Marshal(signals)
		sb.WriteString("\n请求的信号：" + string(sig) + "\n\n任务：\n" + task + "\n\n")
		sb.WriteString(`只输出一个 JSON 对象，键是候选编号，值是 0 到 1 的合适度，例如 {"1":0.1,"2":0.7}，不要别的文字。`)
		ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		jt := &Task{ID: NewID("smart"), Prompt: sb.String(), Kind: KindText, Source: "classifier", MaxTokens: 200}
		res, err := drv.Run(ctx, jt, Target{Lane: LaneAPI, Provider: r.accountProvider(account), AccountID: account, Model: model}, nil)
		if err != nil {
			return nil, fmt.Errorf("判官型号出错：%s", oneLine(err))
		}
		t.JudgeCostUSD += res.Usage.CostUSD
		return parseScores(res.Text, ids)
	}
}

var reJSONObj = regexp.MustCompile(`(?s)\{.*\}`)

// parseScores 从判官的回答里取 JSON 打分，归一化成加起来为 1 的概率；缺的候选记 0。
func parseScores(text string, ids []string) (map[string]float64, error) {
	raw := reJSONObj.FindString(text)
	var m map[string]float64
	if raw == "" || json.Unmarshal([]byte(raw), &m) != nil {
		return nil, errors.New("判官型号没按格式回答")
	}
	sum := 0.0
	out := map[string]float64{}
	for _, id := range ids {
		v := m[id]
		if v < 0 || v != v {
			v = 0
		}
		out[id] = v
		sum += v
	}
	if sum <= 0 {
		return nil, errors.New("判官型号没给出有效的分数")
	}
	for id := range out {
		out[id] /= sum
	}
	return out, nil
}

func (r *Router) accountProvider(id string) string {
	for _, a := range r.cfg.Get().API {
		if a.ID == id {
			return a.Provider
		}
	}
	return ""
}
