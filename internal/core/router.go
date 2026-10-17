package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/imoling/iml-switchboard/internal/config"
)

// Router 是调度核心：决策、执行、回退、记账。
type Router struct {
	cfg      *config.Store
	ledger   *Ledger
	reg      *Registry
	quota    *QuotaManager
	sticky   *Sticky
	drivers  map[LaneID]Driver
	mu       sync.RWMutex
	mode     Mode
	paused   bool
	onCall   func(*Call)
	onNotice func(title, body, level string)
	loc      *time.Location
}

// NewRouter 创建调度器。
func NewRouter(cfg *config.Store, l *Ledger, reg *Registry, q *QuotaManager) *Router {
	r := &Router{cfg: cfg, ledger: l, reg: reg, quota: q, sticky: NewSticky(), drivers: map[LaneID]Driver{}, loc: time.Local}
	r.mode = Mode(cfg.Get().Settings.DefaultMode)
	return r
}

// Register 挂一个渠道驱动。
func (r *Router) Register(d Driver) { r.drivers[d.ID()] = d }

// Driver 取驱动。
func (r *Router) Driver(id LaneID) (Driver, bool) { d, ok := r.drivers[id]; return d, ok }

// OnCall / OnNotice 设置回调（事件流、通知）。
func (r *Router) OnCall(fn func(*Call))                       { r.onCall = fn }
func (r *Router) OnNotice(fn func(title, body, level string)) { r.onNotice = fn }

// Mode / SetMode / Paused / SetPaused 是菜单栏的两个开关。
func (r *Router) Mode() Mode            { r.mu.RLock(); defer r.mu.RUnlock(); return r.mode }
func (r *Router) SetMode(m Mode)        { r.mu.Lock(); r.mode = m; r.mu.Unlock() }
func (r *Router) Paused() bool          { r.mu.RLock(); defer r.mu.RUnlock(); return r.paused }
func (r *Router) SetPaused(p bool)      { r.mu.Lock(); r.paused = p; r.mu.Unlock() }
func (r *Router) Registry() *Registry   { return r.reg }
func (r *Router) Ledger() *Ledger       { return r.ledger }
func (r *Router) Quota() *QuotaManager  { return r.quota }
func (r *Router) Config() *config.Store { return r.cfg }

// NewID 生成短 id。
func NewID(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

// Decide 只做决策，不执行。界面的「试一句」和快速面板预览都用它。
func (r *Router) Decide(t *Task) *Decision {
	cfg := r.cfg.Get()
	d := &Decision{Kind: t.Kind, Reason: []string{}}
	if t.EstTokens == 0 {
		t.EstTokens = EstimateTokens(t.Prompt)
		for _, m := range t.Messages {
			t.EstTokens += EstimateTokens(m.Content)
		}
	}
	images := 0
	if t.ImagePath != "" {
		images++
	}
	for _, m := range t.Messages {
		images += len(m.Images)
	}
	sig := Extract(t.Prompt, images, t.EstTokens)

	// 1. 显式指令
	hit := FindExplicit(cfg.Routing.Explicit, t.Prompt)
	if hit.Word != "" {
		t.Prompt = hit.Prompt
	}
	explicitTier := hit.Tier
	if t.Tier != "" { // 调用方（MCP 参数、重做按钮）指定的档位优先级最高
		explicitTier = t.Tier
		d.Reason = append(d.Reason, "调用方指定"+TierLabel(t.Tier))
	} else if hit.Tier != "" {
		d.Reason = append(d.Reason, fmt.Sprintf("显式指令「%s」→ %s", hit.Word, TierLabel(hit.Tier)))
	}
	if hit.MediaBest && t.MediaTier == "" {
		t.MediaTier = "best"
	}

	// 2. 模态
	tt, matched := config.TaskType{}, false
	if t.Kind == "" || t.Kind == KindText {
		tt, matched = MatchTaskType(cfg.Routing.TaskTable, t.Prompt, sig)
		if matched && tt.Kind != "" {
			t.Kind = Kind(tt.Kind)
			d.Kind = t.Kind
		} else {
			t.Kind = KindText
			d.Kind = KindText
		}
	}
	if t.Kind != KindText {
		d.TaskType = string(t.Kind)
		if matched && tt.Type != "" {
			d.TaskType = tt.Type
		}
		d.MediaTier = t.MediaTier
		if d.MediaTier == "" {
			d.MediaTier = tt.MediaTier
		}
		if d.MediaTier == "" {
			d.MediaTier = "default"
		}
		if t.Kind == KindAnimation {
			d.MediaTier = ""
		}
		if (t.Kind == KindVideo || t.Kind == KindAnimation) && t.Seconds == 0 {
			if n := ParseSeconds(t.Prompt); n > 0 {
				t.Seconds = min(max(n, 2), 30)
				d.Reason = append(d.Reason, fmt.Sprintf("时长：%d 秒", t.Seconds))
			}
		}
		d.Reason = append(d.Reason, "模态："+kindLabel(t.Kind)+"，进入媒体渠道")
		d.Candidates = r.mediaCandidates(cfg, t, d.MediaTier)
		return d
	}

	// 3. 上下文规则
	forbidLow := false
	keepCurrent := false
	for _, cr := range cfg.Routing.ContextRules {
		if !cr.Enabled {
			continue
		}
		switch cr.ID {
		case "no_low_over_100k":
			if t.EstTokens > 100000 {
				forbidLow = true
				d.Reason = append(d.Reason, "上下文超过 100K，禁用低档")
			}
		case "keep_over_60k":
			if t.EstTokens > 60000 {
				keepCurrent = true
			}
		}
	}

	// 4. 任务类型表 / 5. 启发式
	tier := explicitTier
	if matched {
		d.TaskType = tt.Type
		if tier == "" {
			tier = Tier(tt.Tier)
		}
		d.Reason = append(d.Reason, fmt.Sprintf("命中「%s」：%s", tt.Name, tt.Hint))
	} else {
		ht, why := Heuristic(sig)
		d.TaskType = "heuristic"
		if tier == "" {
			tier = ht
		}
		d.Reason = append(d.Reason, why)
	}
	// 质量模式：高档升顶配；省钱模式：高档降中档（除非显式指定）
	if explicitTier == "" {
		switch r.Mode() {
		case ModeQuality:
			if tier == TierHigh {
				tier = TierTop
				d.Reason = append(d.Reason, "质量模式：困难任务用顶配")
			}
		case ModeSave:
			if tier == TierHigh {
				tier = TierMid
				d.Reason = append(d.Reason, "省钱模式：高档降为中档")
			}
		}
	}
	if forbidLow && tier == TierLow {
		tier = TierMid
	}
	d.Tier = tier

	// 6. 会话粘性
	stickyOn := false
	for _, cr := range cfg.Routing.ContextRules {
		if cr.ID == "sticky_session" && cr.Enabled {
			stickyOn = true
		}
	}
	if (stickyOn || keepCurrent) && t.SessionID != "" && explicitTier == "" {
		if tgt, prevTier, ok := r.sticky.Get(t.SessionID); ok {
			subLane := tgt.Lane != LaneAPI && tgt.Lane != LaneMedia
			usable := !(t.HasTools() && subLane) // 带工具的请求不能粘到订阅渠道上
			if usable && (tierRank(prevTier) >= tierRank(tier) || keepCurrent) {
				d.Sticky = true
				d.Tier = prevTier
				d.Candidates = []Target{tgt}
				d.Reason = append(d.Reason, "会话粘性：沿用当前模型")
				return d
			}
		}
	}

	// 7. 候选排序
	prefer := tt.Prefer
	if len(prefer) == 0 {
		prefer = []string{"claude_sub", "api", "codex_sub"}
		if tier == TierLow {
			prefer = []string{"api", "codex_sub", "claude_sub"}
		}
	}
	if hit.SubOnly {
		prefer = []string{"claude_sub", "codex_sub"}
		d.Reason = append(d.Reason, "显式要求只用订阅")
	} else {
		// prefer 是优先顺序而不是白名单：没列到的渠道排在后面当回退
		for _, lane := range []string{"claude_sub", "api", "codex_sub"} {
			found := false
			for _, p := range prefer {
				if p == lane {
					found = true
				}
			}
			if !found {
				prefer = append(prefer, lane)
			}
		}
	}
	d.Candidates = r.textCandidates(cfg, t, tier, prefer, sig.HasImage, &d.Reason)
	return d
}

func kindLabel(k Kind) string {
	switch k {
	case KindImage:
		return "生图"
	case KindVideo:
		return "生视频"
	case KindAnimation:
		return "动画"
	}
	return "文本"
}

// textCandidates 把档位和渠道偏好展开成具体候选，并标注跳过原因。
func (r *Router) textCandidates(cfg *config.Config, t *Task, tier Tier, prefer []string, needVision bool, reason *[]string) []Target {
	var out []Target
	subAllowed := !t.HasTools()
	if !subAllowed {
		*reason = append(*reason, "请求带工具，跳过订阅渠道")
	}
	for _, lane := range prefer {
		switch lane {
		case "claude_sub", "codex_sub", "agy_sub":
			lid := LaneID(lane)
			sl, ok := cfg.Sub[lane]
			if !ok || !sl.Enabled {
				continue
			}
			model := sl.Models[string(tier)]
			if model == "" {
				if tier == TierTop {
					continue // 其它订阅没有顶配
				}
				continue
			}
			tgt := Target{Lane: lid, Provider: providerOf(lid), Model: model, EstCostUSD: 0}
			if !subAllowed {
				tgt.Skipped = "带工具"
			} else if _, ok := r.drivers[lid]; !ok {
				tgt.Skipped = "未接入"
			} else {
				h := r.drivers[lid].Health(context.Background())
				r.quota.Observe(lid, h.Quota)
				if h.Status == "missing" || h.Status == "error" || h.Status == "disabled" {
					tgt.Skipped = h.Detail
				} else if ok, why := r.quota.Healthy(lid, tier, sl.Models); !ok {
					tgt.Skipped = why
				}
			}
			out = append(out, tgt)
		case "api":
			for _, acc := range cfg.API {
				if !acc.Enabled {
					continue
				}
				for _, m := range acc.Models[string(tier)] {
					info, _ := r.reg.Lookup(m)
					tgt := Target{Lane: LaneAPI, Provider: acc.Provider, Model: m, AccountID: acc.ID, EstCostUSD: r.reg.EstimateText(m, t.EstTokens)}
					if needVision && !info.Vision {
						tgt.Skipped = "不能看图"
					}
					if cooling, _, why := r.quota.Cooling(LaneID("acct:" + acc.ID)); cooling {
						tgt.Skipped = why
					}
					out = append(out, tgt)
				}
			}
		}
	}
	if tier == TierTop && len(live(out)) == 0 {
		// 顶配只有 Claude 订阅有；都不可用时退到高档的候选
		*reason = append(*reason, "顶配不可用，退到高档")
		return r.textCandidates(cfg, t, TierHigh, prefer, needVision, reason)
	}
	return out
}

func live(ts []Target) []Target {
	var out []Target
	for _, t := range ts {
		if t.Skipped == "" {
			out = append(out, t)
		}
	}
	return out
}

func providerOf(l LaneID) string {
	switch l {
	case LaneClaudeSub:
		return "anthropic"
	case LaneCodexSub:
		return "openai"
	case LaneAgySub:
		return "google"
	}
	return string(l)
}

// mediaCandidates 返回媒体任务的候选：先是所选档位，再是其它档位作为回退。
func (r *Router) mediaCandidates(cfg *config.Config, t *Task, mediaTier string) []Target {
	var table map[string]config.MediaTarget
	switch t.Kind {
	case KindImage:
		table = cfg.Media.Image
	case KindVideo:
		table = cfg.Media.Video
	case KindAnimation:
		coder := cfg.Media.Animation.CoderModel
		return []Target{{Lane: LaneMedia, Provider: "remotion", Model: coder, EstCostUSD: 0}}
	}
	order := []string{mediaTier}
	for _, k := range []string{"default", "cheap", "best", "free"} {
		if k != mediaTier {
			order = append(order, k)
		}
	}
	units := 1.0
	if t.Kind == KindVideo {
		units = float64(max(t.Seconds, 5))
	}
	var out []Target
	for i, k := range order {
		mt, ok := table[k]
		if !ok {
			continue
		}
		usd, _, _ := r.reg.MediaCost(mt.Model, units)
		tgt := Target{Lane: LaneMedia, Provider: mt.Provider, Model: mt.Model, EstCostUSD: usd, AccountID: mt.AccountID}
		if i > 0 {
			tgt.Skipped = "" // 回退候选，不算跳过
		}
		if !r.mediaProviderReady(cfg, mt.Provider) {
			tgt.Skipped = "未添加 " + providerLabel(mt.Provider) + " 账号"
		}
		out = append(out, tgt)
	}
	return out
}

func providerLabel(p string) string {
	switch p {
	case "ark":
		return "火山方舟"
	case "dashscope":
		return "阿里云百炼"
	case "zhipu":
		return "智谱"
	case "openai":
		return "OpenAI"
	case "openrouter":
		return "OpenRouter"
	case "302ai":
		return "302.ai"
	case "codex":
		return "ChatGPT"
	case "anthropic":
		return "Claude"
	}
	return p
}

// ProviderLabel 导出给接入面用。
func ProviderLabel(p string) string { return providerLabel(p) }

func (r *Router) mediaProviderReady(cfg *config.Config, provider string) bool {
	if provider == "codex" {
		d, ok := r.drivers[LaneCodexSub]
		return ok && d.Health(context.Background()).Status != "missing"
	}
	for _, a := range cfg.API {
		if a.Enabled && a.Provider == provider {
			return true
		}
	}
	return false
}

// Run 决策并执行，带回退与记账。emit 可为 nil。
func (r *Router) Run(ctx context.Context, t *Task, emit func(Event)) (*Call, *Result, error) {
	if t.ID == "" {
		t.ID = NewID("call")
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	if r.Paused() {
		return nil, nil, errors.New("调度已暂停")
	}
	d := r.Decide(t)
	return r.Execute(ctx, t, d, emit)
}

// Execute 按决策执行。
func (r *Router) Execute(ctx context.Context, t *Task, d *Decision, emit func(Event)) (*Call, *Result, error) {
	start := time.Now()
	cfg := r.cfg.Get()
	call := &Call{ID: t.ID, TS: start, Source: t.Source, Host: t.Host, SessionID: t.SessionID, Kind: t.Kind, TaskType: d.TaskType,
		Tier: d.Tier, MediaTier: d.MediaTier, Reason: d.Reason, Candidates: d.Candidates, JobID: ""}
	if cfg.Settings.SavePrompts {
		call.PromptExcerpt = excerpt(t.Prompt, 200)
	}
	var lastErr error
	var fallbackFrom string
	tried := 0
	// 按量付费月上限
	overBudget := false
	if ov, err := r.ledger.Aggregate(1, r.loc); err == nil && cfg.Settings.MonthlyAPILimitUSD > 0 && ov.MonthAPICostUSD >= cfg.Settings.MonthlyAPILimitUSD {
		overBudget = true
	}
	for i := range d.Candidates {
		c := &d.Candidates[i]
		if c.Skipped != "" {
			continue
		}
		if overBudget && (c.Lane == LaneAPI || (c.Lane == LaneMedia && c.EstCostUSD > 0)) {
			c.Skipped = "按量付费已到本月上限"
			continue
		}
		drv, ok := r.drivers[c.Lane]
		if !ok {
			c.Skipped = "渠道未启用"
			continue
		}
		tried++
		res, err := drv.Run(ctx, t, *c, emit)
		if err == nil {
			r.finish(call, t, *c, res, start, fallbackFrom)
			if t.SessionID != "" && t.Kind == KindText {
				r.sticky.Set(t.SessionID, *c, d.Tier)
			}
			return call, res, nil
		}
		lastErr = err
		c.Skipped = oneLine(err)
		var rl *ErrRateLimited
		var pe *ErrProvider
		switch {
		case errors.As(err, &rl):
			lane := c.Lane
			if c.Lane == LaneAPI {
				lane = LaneID("acct:" + c.AccountID)
			}
			r.quota.MarkCooldown(lane, rl.RetryAfter, rl.Detail)
			if c.Lane != LaneAPI && c.Lane != LaneMedia {
				r.notify("额度用完", providerLabel(c.Provider)+" 订阅额度暂时用完，同档任务改走按量付费", "warn")
			}
			fallbackFrom = c.Model
		case errors.As(err, &pe) && !pe.Retryable:
			r.fail(call, t, *c, err, start)
			return call, nil, err
		case ctx.Err() != nil:
			r.fail(call, t, *c, err, start)
			return call, nil, err
		default:
			fallbackFrom = c.Model
		}
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用的模型：" + skippedSummary(d.Candidates))
	} else {
		lastErr = fmt.Errorf("没有可用的模型：最后一个错误是 %s", oneLine(lastErr))
	}
	if tried == 0 && len(d.Candidates) > 0 {
		lastErr = errors.New("没有可用的模型：" + skippedSummary(d.Candidates))
	}
	r.fail(call, t, Target{}, lastErr, start)
	return call, nil, lastErr
}

func skippedSummary(cs []Target) string {
	var parts []string
	for _, c := range cs {
		if c.Skipped != "" {
			parts = append(parts, c.Model+"（"+c.Skipped+"）")
		}
	}
	if len(parts) == 0 {
		return "没有配置任何候选"
	}
	return strings.Join(parts, "，")
}

func (r *Router) finish(call *Call, t *Task, tgt Target, res *Result, start time.Time, fallbackFrom string) {
	call.Lane, call.Provider, call.Model = tgt.Lane, tgt.Provider, tgt.Model
	if res.Model != "" {
		call.Model = res.Model
	}
	call.FallbackFrom = fallbackFrom
	call.InputTokens, call.OutputTokens, call.CacheReadTokens = res.Usage.InputTokens, res.Usage.OutputTokens, res.Usage.CacheReadTokens
	call.Units, call.Unit = res.Usage.Units, res.Usage.Unit
	call.CostUSD, call.CostNative, call.Currency, call.QuotaUnits = res.Usage.CostUSD, res.Usage.CostNative, res.Usage.Currency, res.Usage.QuotaUnits
	// 订阅渠道的额度占用统一按「等值美元」记：各家 CLI 报的单位不同（Claude 报美元，Codex 只有 token 数），
	// 用注册表价格换算，窗口百分比才能横向比较。
	if tgt.Lane != LaneAPI && tgt.Lane != LaneMedia && t.Kind == KindText {
		if usd := r.reg.TextCost(call.Model, res.Usage.InputTokens, res.Usage.OutputTokens, res.Usage.CacheReadTokens); usd > 0 {
			call.QuotaUnits = usd
		}
	}
	if t.Kind == KindText {
		call.BaselineCostUSD = r.reg.Baseline(res.Usage.InputTokens, res.Usage.OutputTokens, res.Usage.CacheReadTokens)
		if call.BaselineCostUSD == 0 {
			call.BaselineCostUSD = r.reg.EstimateText("opus", t.EstTokens)
		}
	} else {
		call.BaselineCostUSD = call.CostUSD
	}
	call.LatencyMS = int(time.Since(start).Milliseconds())
	call.OK = true
	call.ResultPath = res.Path
	call.JobID = res.JobID
	if r.cfg.Get().Settings.SavePrompts {
		call.ResultText = excerpt(res.Text, 2000)
	}
	_ = r.ledger.Insert(call)
	if r.onCall != nil {
		r.onCall(call)
	}
}

func (r *Router) fail(call *Call, t *Task, tgt Target, err error, start time.Time) {
	call.Lane, call.Provider, call.Model = tgt.Lane, tgt.Provider, tgt.Model
	call.LatencyMS = int(time.Since(start).Milliseconds())
	call.OK = false
	call.Error = oneLine(err)
	_ = r.ledger.Insert(call)
	if r.onCall != nil {
		r.onCall(call)
	}
}

func (r *Router) notify(title, body, level string) {
	if r.onNotice != nil {
		r.onNotice(title, body, level)
	}
}

func oneLine(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

func excerpt(s string, n int) string {
	s = strings.TrimSpace(s)
	rs := []rune(s)
	if len(rs) > n {
		return string(rs[:n]) + "…"
	}
	return s
}

// Escalate 处理「判低了」。返回该任务类型近 7 天标记次数。
func (r *Router) Escalate(callID string) (string, int, error) {
	tt, n, err := r.ledger.Escalate(callID)
	if err == nil && n >= 5 {
		r.notify("规则建议", fmt.Sprintf("「%s」7 天内被标记判低 %d 次，到路由规则里看看要不要调高一档", r.TaskName(tt), n), "info")
	}
	return tt, n, err
}

// TaskName 任务类型的中文名。
func (r *Router) TaskName(tt string) string {
	for _, t := range r.cfg.Get().Routing.TaskTable {
		if t.Type == tt {
			return t.Name
		}
	}
	if tt == "heuristic" {
		return "未归类"
	}
	return tt
}

// Suggestion 是一条规则调整建议。
type Suggestion struct {
	TaskType       string  `json:"task_type"`
	Name           string  `json:"name"`
	Escalations    int     `json:"escalations"`
	FromTier       Tier    `json:"from_tier"`
	ToTier         Tier    `json:"to_tier"`
	WeeklyDeltaUSD float64 `json:"weekly_delta_usd"`
}

// Suggestions 从账本算出建议：7 天内判低 ≥ 5 次的任务类型建议升一档。
func (r *Router) Suggestions() ([]Suggestion, error) {
	stats, err := r.ledger.EscalationStats(7)
	if err != nil {
		return nil, err
	}
	cfg := r.cfg.Get()
	ignored := map[string]bool{}
	for _, i := range cfg.Routing.Ignored {
		ignored[i] = true
	}
	out := []Suggestion{}
	for _, s := range stats {
		if s.Escalations < 5 || ignored[s.TaskType] || s.TaskType == "heuristic" {
			continue
		}
		var row *config.TaskType
		for i := range cfg.Routing.TaskTable {
			if cfg.Routing.TaskTable[i].Type == s.TaskType {
				row = &cfg.Routing.TaskTable[i]
			}
		}
		if row == nil || row.Tier == "high" {
			continue
		}
		to := TierMid
		if row.Tier == "mid" {
			to = TierHigh
		}
		// 估算：每周次数 × （目标档代表模型 − 当前平均费用）
		rep := map[Tier]string{TierMid: "sonnet", TierHigh: "opus"}[to]
		est := r.reg.EstimateText(rep, 2000)
		delta := float64(s.Total) * (est - s.AvgCostUSD)
		if delta < 0 {
			delta = 0
		}
		out = append(out, Suggestion{TaskType: s.TaskType, Name: row.Name, Escalations: s.Escalations, FromTier: Tier(row.Tier), ToTier: to, WeeklyDeltaUSD: delta})
	}
	return out, nil
}

// ApplySuggestion 采纳或忽略。
func (r *Router) ApplySuggestion(taskType, action string) error {
	return r.cfg.Update(func(c *config.Config) error {
		if action == "ignore" {
			c.Routing.Ignored = append(c.Routing.Ignored, taskType)
			return nil
		}
		for i := range c.Routing.TaskTable {
			if c.Routing.TaskTable[i].Type == taskType {
				switch c.Routing.TaskTable[i].Tier {
				case "low":
					c.Routing.TaskTable[i].Tier = "mid"
				case "mid":
					c.Routing.TaskTable[i].Tier = "high"
				}
				return nil
			}
		}
		return errors.New("任务类型不存在")
	})
}
