package core

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// QuotaManager 记录订阅渠道的冷却与窗口用量，决定它能不能接活。
type QuotaManager struct {
	mu       sync.Mutex
	cooldown map[LaneID]cool
	ledger   *Ledger
	yieldPct func() float64 // 让出线
	// 订阅渠道的 5 小时窗口「满额」估算值：按 quota_units（美元等值）计。
	// 没有官方接口可查，用经验值：Max 5x ≈ $35 / 5h，Plus ≈ $8 / 5h。界面上注明是估算。
	windowCap    map[LaneID]float64
	weekCap      map[LaneID]float64
	fableWeekCap float64
	// observed 是官方 CLI 自己报告的窗口利用率（Claude 的 rate_limit_event），比估算准，15 分钟内优先用。
	observed map[LaneID]observedQuota
	probeMu  sync.Mutex
}

type observedQuota struct {
	Windows []QuotaWindow `json:"windows"`
	At      time.Time     `json:"at"`
}

// observedTTL 是自报数据的有效期：过了还没有新的调用，就回退到估算并标注。
const observedTTL = 6 * time.Hour

// Observe 记录渠道自报的额度窗口，并持久化，重启后还在。
func (q *QuotaManager) Observe(lane LaneID, windows []QuotaWindow) {
	if len(windows) == 0 {
		return
	}
	now := time.Now()
	q.mu.Lock()
	// 合并：这次没报的窗口（比如只有调用 Fable 才出现的 Fable 周窗口）保留上次的值和时间
	merged := make([]QuotaWindow, 0, len(windows)+2)
	seen := map[string]bool{}
	for _, w := range windows {
		w.ObservedAt = now
		merged = append(merged, w)
		seen[w.Window] = true
	}
	for _, w := range q.observed[lane].Windows {
		if !seen[w.Window] && now.Sub(w.ObservedAt) < 7*24*time.Hour {
			merged = append(merged, w)
		}
	}
	q.observed[lane] = observedQuota{Windows: merged, At: now}
	cp := map[LaneID]observedQuota{}
	for k, v := range q.observed {
		cp[k] = v
	}
	q.mu.Unlock()
	if q.ledger != nil {
		_ = q.ledger.KVSet("quota_observed", cp)
	}
}

// ObservePlan 记住渠道自报的套餐名，重启后还在；空值不覆盖。
func (q *QuotaManager) ObservePlan(lane LaneID, plan string) {
	if plan == "" || q.ledger == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	saved := map[LaneID]string{}
	q.ledger.KVGet("plan_observed", &saved)
	if saved[lane] == plan {
		return
	}
	saved[lane] = plan
	_ = q.ledger.KVSet("plan_observed", saved)
}

// Plan 返回上次记住的套餐名；没有就是空。
func (q *QuotaManager) Plan(lane LaneID) string {
	if q.ledger == nil {
		return ""
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	saved := map[LaneID]string{}
	q.ledger.KVGet("plan_observed", &saved)
	return saved[lane]
}

// ObservedAt 返回某渠道最近一次自报的时间；没有就是零值。
func (q *QuotaManager) ObservedAt(lane LaneID) time.Time {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.observed[lane].At
}

// Refresh 让能探测的渠道各做一次极小的调用来拿真实额度。force 为假时 10 分钟内不重复探测。
func (q *QuotaManager) Refresh(ctx context.Context, drivers map[LaneID]Driver, force bool) {
	q.probeMu.Lock()
	defer q.probeMu.Unlock()
	for lane, d := range drivers {
		p, ok := d.(QuotaProber)
		if !ok {
			continue
		}
		if !force && time.Since(q.ObservedAt(lane)) < 10*time.Minute {
			continue
		}
		if h := d.Health(ctx); h.Status != "ok" && h.Status != "warn" {
			continue
		}
		if ws, err := p.ProbeQuota(ctx); err == nil {
			q.Observe(lane, ws)
		}
		if deep, ok := d.(QuotaProberDeep); ok && force && q.fableStale(lane) {
			if ws, err := deep.ProbeQuotaDeep(ctx); err == nil {
				q.Observe(lane, ws)
			}
		}
	}
}

// fableStale 判断 Fable 周窗口是否超过 6 小时没更新。
func (q *QuotaManager) fableStale(lane LaneID) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, w := range q.observed[lane].Windows {
		if strings.Contains(strings.ToLower(w.Window), "fable") {
			return time.Since(w.ObservedAt) > 6*time.Hour
		}
	}
	return true
}

type cool struct {
	until  time.Time
	detail string
}

// NewQuotaManager 创建额度管家。
func NewQuotaManager(l *Ledger, yieldPct func() float64) *QuotaManager {
	q := &QuotaManager{cooldown: map[LaneID]cool{}, ledger: l, yieldPct: yieldPct,
		windowCap:    map[LaneID]float64{LaneClaudeSub: 35, LaneCodexSub: 8, LaneAgySub: 10},
		weekCap:      map[LaneID]float64{LaneClaudeSub: 180, LaneCodexSub: 40, LaneAgySub: 50},
		fableWeekCap: 90, observed: map[LaneID]observedQuota{}}
	var obs map[LaneID]observedQuota
	if l != nil && l.KVGet("quota_observed", &obs) {
		for k, v := range obs {
			q.observed[k] = v
		}
	}
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
	q.mu.Lock()
	obs, hasObs := q.observed[lane]
	q.mu.Unlock()
	if hasObs && now.Sub(obs.At) < observedTTL {
		sawFable := false
		for _, w := range obs.Windows {
			if w.ObservedAt.IsZero() {
				w.ObservedAt = obs.At
			}
			key := strings.ToLower(w.Window)
			switch {
			case strings.Contains(key, "fable"):
				w.Window, w.Label = "week_fable", "其中 Fable"
				sawFable = true
			case key == "5h" || key == "five_hour":
				w.Window, w.Label = "5h", "5 小时窗口"
			case key == "week" || key == "7d" || key == "seven_day":
				w.Window, w.Label = "week", "本周"
			case key == "month" || key == "monthly":
				w.Window, w.Label = "month", "本月"
			default:
				if w.Label == "" {
					w.Label = w.Window
				}
			}
			out = append(out, w)
		}
		rank := func(w string) int {
			order := map[string]int{"5h": 0, "week": 1, "week_fable": 2, "month": 3, "5h_claude": 4, "week_claude": 5}
			if r, ok := order[w]; ok {
				return r
			}
			return 9
		}
		sort.SliceStable(out, func(i, j int) bool { return rank(out[i].Window) < rank(out[j].Window) })
		if lane == LaneClaudeSub && !sawFable {
			if fm, ok := models["top"]; ok && q.ledger != nil {
				start := weekStart(now)
				fu, _, _ := q.ledger.QuotaWindowUsage(lane, start, fm)
				out = append(out, QuotaWindow{Window: "week_fable", Label: "其中 Fable", Pct: clamp(fu / q.fableWeekCap * 100), ResetsAt: start.AddDate(0, 0, 7), Estimated: true})
			}
		}
		return out
	}
	if cap := q.windowCap[lane]; cap > 0 {
		start := now.Truncate(5 * time.Hour)
		u, _, _ := q.ledger.QuotaWindowUsage(lane, start, "")
		out = append(out, QuotaWindow{Window: "5h", Label: "5 小时窗口", Pct: clamp(u / cap * 100), ResetsAt: start.Add(5 * time.Hour), Estimated: true})
	}
	if cap := q.weekCap[lane]; cap > 0 {
		wd := int(now.Weekday())
		if wd == 0 {
			wd = 7
		}
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(wd - 1))
		u, _, _ := q.ledger.QuotaWindowUsage(lane, start, "")
		out = append(out, QuotaWindow{Window: "week", Label: "本周", Pct: clamp(u / cap * 100), ResetsAt: start.AddDate(0, 0, 7), Estimated: true})
		if lane == LaneClaudeSub {
			if fm, ok := models["top"]; ok {
				fu, _, _ := q.ledger.QuotaWindowUsage(lane, start, fm)
				out = append(out, QuotaWindow{Window: "week_fable", Label: "其中 Fable", Pct: clamp(fu / q.fableWeekCap * 100), ResetsAt: start.AddDate(0, 0, 7), Estimated: true})
			}
		}
	}
	return out
}

func weekStart(now time.Time) time.Time {
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(wd - 1))
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
