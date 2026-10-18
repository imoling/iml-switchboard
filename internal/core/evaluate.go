package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Evaluation 是一轮影子评估的结果：抽样低档请求，用高档重做，再让高档模型盲评。
type Evaluation struct {
	ID         string        `json:"id"`
	Status     string        `json:"status"`
	StartedAt  time.Time     `json:"started_at"`
	FinishedAt *time.Time    `json:"finished_at,omitempty"`
	Sample     int           `json:"sample"`
	CostUSD    float64       `json:"cost_usd"`
	Summary    EvalSummary   `json:"summary"`
	ByTask     []EvalTaskRow `json:"by_task"` // 始终非 nil，界面直接 .map
	Error      string        `json:"error,omitempty"`
	Note       string        `json:"note,omitempty"`
}

// EvalSummary 是三类结果计数。
type EvalSummary struct {
	LowBetter  int `json:"low_better"`
	Tie        int `json:"tie"`
	HighBetter int `json:"high_better"`
}

// EvalTaskRow 是按任务类型的结果与建议。
type EvalTaskRow struct {
	TaskType       string  `json:"task_type"`
	Name           string  `json:"name"`
	N              int     `json:"n"`
	LowBetter      int     `json:"low_better"`
	Tie            int     `json:"tie"`
	HighBetter     int     `json:"high_better"`
	Suggestion     string  `json:"suggestion,omitempty"`
	ToTier         Tier    `json:"to_tier,omitempty"`
	WeeklyDeltaUSD float64 `json:"weekly_delta_usd,omitempty"`
}

// RunEvaluation 执行一轮评估。裁判走高档（订阅优先，不花钱时费用为 0）。
// 每条样本两次调用：高档重做 + 裁判。sample 默认 20，上限 100。
func (r *Router) RunEvaluation(ctx context.Context, ev *Evaluation, onUpdate func(*Evaluation)) {
	if ev.Sample <= 0 {
		ev.Sample = 20
	}
	if ev.Sample > 100 {
		ev.Sample = 100
	}
	samples, err := r.ledger.Sample(30, ev.Sample, []string{"low", "mid"})
	if err != nil || len(samples) == 0 {
		ev.Status = "failed"
		ev.Error = "没有可抽样的记录：先用一阵子，再来评估"
		now := time.Now()
		ev.FinishedAt = &now
		onUpdate(ev)
		return
	}
	ev.Sample = len(samples)
	byTask := map[string]*EvalTaskRow{}
	failed, judged := 0, 0
	var lastErr error
	for i, s := range samples {
		if ctx.Err() != nil {
			break
		}
		row := byTask[s.TaskType]
		if row == nil {
			row = &EvalTaskRow{TaskType: s.TaskType, Name: r.TaskName(s.TaskType)}
			byTask[s.TaskType] = row
		}
		row.N++
		// 高档重做
		prompt, lowAnswer := s.PromptFull, s.ResultFull
		if prompt == "" { // 老记录：抽样时已确认摘录没被截断
			prompt, lowAnswer = s.PromptExcerpt, s.ResultText
		}
		hi := &Task{Prompt: prompt, Tier: TierHigh, Source: "evaluation", Kind: KindText}
		hiCall, hiRes, err := r.Run(ctx, hi, nil)
		if err != nil {
			failed++
			lastErr = err
			continue
		}
		ev.CostUSD += hiCall.CostUSD
		// 盲评：随机交换 A/B
		a, b := lowAnswer, hiRes.Text
		swapped := i%2 == 1
		if swapped {
			a, b = b, a
		}
		judgePrompt := fmt.Sprintf("你是评审。下面是同一个问题的两个回答，只输出一个字：A、B 或 平。\n\n问题：\n%s\n\n回答 A：\n%s\n\n回答 B：\n%s\n\n哪个更好？", prompt, a, b)
		judge := &Task{Prompt: judgePrompt, Tier: TierHigh, Source: "evaluation", Kind: KindText}
		jCall, jRes, err := r.Run(ctx, judge, nil)
		if err != nil {
			failed++
			lastErr = err
			continue
		}
		judged++
		ev.CostUSD += jCall.CostUSD
		verdict := strings.TrimSpace(jRes.Text)
		lowWins := strings.HasPrefix(verdict, "A") != swapped
		switch {
		case strings.HasPrefix(verdict, "平") || strings.Contains(verdict, "tie"):
			row.Tie++
			ev.Summary.Tie++
		case lowWins:
			row.LowBetter++
			ev.Summary.LowBetter++
		default:
			row.HighBetter++
			ev.Summary.HighBetter++
		}
		onUpdate(ev)
	}
	ev.ByTask = ev.ByTask[:0]
	for _, row := range byTask {
		if row.N >= 3 && float64(row.HighBetter)/float64(row.N) >= 0.4 {
			cur := r.tierOf(row.TaskType)
			if cur == TierLow {
				row.ToTier = TierMid
			} else if cur == TierMid {
				row.ToTier = TierHigh
			}
			if row.ToTier != "" {
				row.Suggestion = "调到" + TierLabel(row.ToTier)
				rep := map[Tier]string{TierMid: "sonnet", TierHigh: "opus"}[row.ToTier]
				row.WeeklyDeltaUSD = float64(row.N) * r.reg.EstimateText(rep, 2000)
			}
		}
		ev.ByTask = append(ev.ByTask, *row)
	}
	now := time.Now()
	ev.FinishedAt = &now
	switch {
	case judged == 0 && lastErr != nil:
		ev.Status = "failed"
		ev.Error = fmt.Sprintf("%d 条样本都没评成：%s", failed, oneLine(lastErr))
	case failed > 0:
		ev.Status = "done"
		ev.Note = fmt.Sprintf("%d 条没评成，最后一个错误：%s", failed, oneLine(lastErr))
	default:
		ev.Status = "done"
	}
	onUpdate(ev)
}

func (r *Router) tierOf(taskType string) Tier {
	for _, t := range r.cfg.Get().Routing.TaskTable {
		if t.Type == taskType {
			return Tier(t.Tier)
		}
	}
	return ""
}
