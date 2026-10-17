package core

import (
	"sync"
	"time"
)

// QuotaManager 记录订阅渠道的冷却与窗口用量，决定它能不能接活。
type QuotaManager struct {
	mu        sync.Mutex
	cooldown  map[LaneID]cool
	ledger    *Ledger
	yieldPct  func() float64 // 让出线
	// 订阅渠道的 5 小时窗口「满额」估算值：按 quota_units（美元等值）计。
	// 没有官方接口可查，用经验值：Max 5x ≈ $35 / 5h，Plus ≈ $8 / 5h。界面上注明是估算。
	windowCap map[LaneID]float64
	weekCap   map[LaneID]float64
	fableWeekCap float64
}

type cool struct {
	until  time.Time
	detail string
}

// NewQuotaManager 创建额度管家。
func NewQuotaManager(l *Ledger, yieldPct func() float64) *QuotaManager {
	q := &QuotaManager{cooldown: map[LaneID]cool{}, ledger: l, yieldPct: yieldPct,
		windowCap: map[LaneID]float64{LaneClaudeSub: 35, LaneCodexSub: 8, LaneAgySub: 10},
		weekCap:   map[LaneID]float64{LaneClaudeSub: 180, LaneCodexSub: 40, LaneAgySub: 50},
		fableWeekCap: 90}
	var saved map[LaneID]cool
	if l != nil && l.KVGet("cooldown", &saved) {
		for k, v := range saved {
			if v.until.After(time.Now()) {
				q.cooldown[k] = v
			}
		}
	}
	return q
}

// MarkCooldown 把渠道标为冷却。
func (q *QuotaManager) MarkCooldown(lane LaneID, d time.Duration, detail string) {
	if d <= 0 {
		d = 30 * time.Minute
	}
	q.mu.Lock()
	q.cooldown[lane] = cool{until: time.Now().Add(d), detail: detail}
	cp := map[LaneID]cool{}
	for k, v := range q.cooldown {
		cp[k] = v
	}
	q.mu.Unlock()
	if q.ledger != nil {
		_ = q.ledger.KVSet("cooldown", cp)
	}
}

// Cooling 返回渠道是否在冷却中及恢复时间。
func (q *QuotaManager) Cooling(lane LaneID) (bool, time.Time, string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	c, ok := q.cooldown[lane]
	if !ok || time.Now().After(c.until) {
		delete(q.cooldown, lane)
		return false, time.Time{}, ""
	}
	return true, c.until, c.detail
}

// Windows 估算渠道的额度窗口占比。
func (q *QuotaManager) Windows(lane LaneID, models map[string]string) []QuotaWindow {
	if q.ledger == nil {
		return nil
	}
	now := time.Now()
	out := []QuotaWindow{}
	if cap := q.windowCap[lane]; cap > 0 {
		start := now.Truncate(5 * time.Hour)
		u, _, _ := q.ledger.QuotaWindowUsage(lane, start, "")
		out = append(out, QuotaWindow{Window: "5h", Label: "5 小时窗口", Pct: clamp(u / cap * 100), ResetsAt: start.Add(5 * time.Hour)})
	}
	if cap := q.weekCap[lane]; cap > 0 {
		wd := int(now.Weekday())
		if wd == 0 {
			wd = 7
		}
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(wd - 1))
		u, _, _ := q.ledger.QuotaWindowUsage(lane, start, "")
		out = append(out, QuotaWindow{Window: "week", Label: "本周", Pct: clamp(u / cap * 100), ResetsAt: start.AddDate(0, 0, 7)})
		if lane == LaneClaudeSub {
			if fm, ok := models["top"]; ok {
				fu, _, _ := q.ledger.QuotaWindowUsage(lane, start, fm)
				out = append(out, QuotaWindow{Window: "week_fable", Label: "其中 Fable", Pct: clamp(fu / q.fableWeekCap * 100), ResetsAt: start.AddDate(0, 0, 7)})
			}
		}
	}
	return out
}

// Healthy 判断订阅渠道现在能不能接某个档位：冷却中不行；过了让出线只接高档和顶配。
func (q *QuotaManager) Healthy(lane LaneID, tier Tier, models map[string]string) (bool, string) {
	if cooling, until, _ := q.Cooling(lane); cooling {
		return false, "额度暂时用完，约 " + until.Sub(time.Now()).Round(time.Minute).String() + " 后恢复"
	}
	for _, w := range q.Windows(lane, models) {
		if w.Window == "5h" && w.Pct >= q.yieldPct() && (tier == TierLow || tier == TierMid) {
			return false, "额度已过让出线，低档和中档改走按量付费"
		}
	}
	return true, ""
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
