package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/imoling/iml-switchboard/internal/config"
)

// Router 是调度核心：决策、执行、回退、记账。
type Router struct {
	cfg        *config.Store
	ledger     *Ledger
	reg        *Registry
	quota      *QuotaManager
	sticky     *Sticky
	drivers    map[LaneID]Driver
	mu         sync.RWMutex
	mode       Mode
	paused     bool
	onCall     func(*Call)
	onAttempt  func(*Task, Target, Attempt)
	onBounce   func(*Task, error)
	onNotice   func(title, body, level string)
	loc        *time.Location
	clsCache   classifyCache
	smartJudge SmartJudge
	smartCache classifyCache
	inflight   atomic.Int64
	// baselineDefault 给出应用接管前自己用的型号，算应用原价用；由 app 层接到接入面
	baselineDefault func(host string) string
	// appBilling 给自己按量计价的应用（WorkBuddy 按积分）换算这次请求的计价型号；不是这类应用返回空
	appBilling func(host, hostModel string) string
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
func (r *Router) OnCall(fn func(*Call)) { r.onCall = fn }

// OnAttempt 在把请求交给某个候选之前调用；换候选时会再调一次，OnCall 要等做完才调。
func (r *Router) OnAttempt(fn func(*Task, Target, Attempt)) { r.onAttempt = fn }

// OnBounce 在兼容入口发现谁都装不下、原样退回应用时调用；这类请求不进记录。
func (r *Router) OnBounce(fn func(*Task, error)) { r.onBounce = fn }

// Bounce 见 OnBounce。
func (r *Router) Bounce(t *Task, err error) {
	if r.onBounce != nil && err != nil {
		r.onBounce(t, err)
	}
}

// Attempt 是一次交给候选时的上下文，给界面演「谁接的活、是不是换人、判官有没有改判」。
type Attempt struct {
	N        int    // 第几个候选，从 1 起；大于 1 就是换人
	Tier     Tier   // 这个候选所在的档
	RuleTier Tier   // 智能分派改了档时，规则原本判的档；没改为空
	Judge    string // 改判的判官
	TaskType string
	Sticky   bool // 会话粘住了上次的模型
}

func (r *Router) OnNotice(fn func(title, body, level string)) { r.onNotice = fn }

// Mode / SetMode / Paused / SetPaused 是菜单栏的两个开关。
func (r *Router) Mode() Mode     { r.mu.RLock(); defer r.mu.RUnlock(); return r.mode }
func (r *Router) SetMode(m Mode) { r.mu.Lock(); r.mode = m; r.mu.Unlock() }

// InFlight 是正在执行的请求数；重启后台前用它提醒会中断多少个。
func (r *Router) InFlight() int         { return int(r.inflight.Load()) }
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
	prompt0, mediaTier0 := t.Prompt, t.MediaTier
	if hit.MediaBest && t.MediaTier == "" {
		t.MediaTier = "best"
	}

	// 2. 模态
	tt, matched := config.TaskType{}, false
	autoMedia := false // 媒体是规则或判档员认出来的，不是调用方点名要的：没配画师时可以退回文字模型
	if t.Kind == "" || t.Kind == KindText {
		ask := StripInjected(t.Prompt)
		tt, matched = MatchTaskType(cfg.Routing.TaskTable, ask, sig)
		if matched && tt.Kind != "" && !IsMediaAsk(ask) {
			d.Reason = append(d.Reason, "长文本里出现媒体词，不当媒体任务")
			tt, matched = config.TaskType{}, false
		}
		if matched && tt.Kind != "" && IsQuoted(t.Prompt) {
			// WorkBuddy 每轮都会把用户原话包在 <session> 里另发一次辅助请求；按它出图就会一张变两张
			d.Reason = append(d.Reason, "应用转述的会话内容，不当媒体任务")
			tt, matched = config.TaskType{}, false
		}
		if matched && tt.Kind != "" && !t.FreshTurn() {
			d.Reason = append(d.Reason, "工具结果回来的后续回合，不再出图")
			tt, matched = config.TaskType{}, false
		}
		if !matched || tt.Type == "qa_short" {
			switch t.Judged {
			case "image", "video", "animation":
				// 带工具的智能体回合也认：接管后宿主的模型拿不到画师，只会自己写 SVG
				if IsMediaAsk(ask) && t.FreshTurn() && !IsQuoted(t.Prompt) {
					tt, matched = config.TaskType{Type: t.Judged, Kind: t.Judged}, true
					for _, row := range cfg.Routing.TaskTable {
						if row.Type == t.Judged {
							tt = row
						}
					}
					d.Reason = append(d.Reason, "规则拿不准，低档模型判为"+kindLabel(Kind(t.Judged)))
				}
			}
		}
		if matched && tt.Kind != "" {
			t.Kind = Kind(tt.Kind)
			d.Kind = t.Kind
			autoMedia = true
			t.Prompt = ask // 媒体提示词、产物文件名、媒体库标题都用剥干净的这句
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
		r.pinMedia(cfg, t, d)
		if !autoMedia || len(live(d.Candidates)) > 0 || t.Kind == KindAnimation {
			return d
		}
		// 没配出图、出视频账号：这句话照常交给文字模型，不报错
		d.Reason = append(d.Reason, "没有可用的"+kindLabel(t.Kind)+"渠道，交给文字模型")
		t.Kind, t.Prompt, t.MediaTier = KindText, prompt0, mediaTier0
		d.Kind, d.TaskType, d.MediaTier, d.Candidates = KindText, "", "", nil
		tt, matched = config.TaskType{}, false
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
		d.TaskType = HeuristicType(t.HasTools(), sig)
		if tier == "" {
			tier = ht
		}
		d.Reason = append(d.Reason, why)
	}
	if hit.Word == "" && explicitTier == "" && (d.TaskType == "qa_short" || IsHeuristic(d.TaskType)) {
		d.Unsure = true
		if jt := Tier(t.Judged); tierRank(jt) > 0 {
			d.Reason = append(d.Reason, "规则拿不准，低档模型判为"+TierLabel(jt))
			tier = jt
			d.TaskType = "judged"
		}
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

	// 6. 会话粘性：粘住的目标排在候选最前，后面仍接这一档的正常候选，它出错时能顺序回退
	stickyOn := false
	for _, cr := range cfg.Routing.ContextRules {
		if cr.ID == "sticky_session" && cr.Enabled {
			stickyOn = true
		}
	}
	var stickyTgt *Target
	if (stickyOn || keepCurrent) && t.SessionID != "" && explicitTier == "" {
		if tgt, prevTier, ok := r.sticky.Get(t.SessionID); ok {
			subLane := tgt.Lane != LaneAPI && tgt.Lane != LaneMedia
			usable := !(t.HasTools() && subLane) // 带工具的请求不能粘到订阅渠道上
			if fit, _ := r.fits(tgt.Model, t.EstTokens); !fit {
				usable = false // 对话越来越长，原来的模型装不下了：重新选
				d.Reason = append(d.Reason, "对话变长，原模型装不下，重新选")
			}
			if cooling, _, why := r.quota.Cooling(cooldownKey(tgt)); usable && cooling {
				usable = false // 上一轮刚被限流或额度用完：不再粘它，交给正常排序
				r.sticky.Forget(t.SessionID)
				d.Reason = append(d.Reason, "原模型"+why+"，重新选")
			}
			if usable && (tierRank(prevTier) >= tierRank(tier) || keepCurrent) {
				d.Sticky = true
				d.Tier = prevTier
				tier = prevTier
				stickyTgt = &tgt
				d.Reason = append(d.Reason, "会话粘性：沿用当前模型")
			}
		}
	}

	// 7. 候选排序
	prefer := tt.Prefer
	if len(prefer) == 0 {
		prefer = []string{"claude_sub", "api", "codex_sub", "agy_sub"}
		if tier == TierLow {
			prefer = []string{"api", "codex_sub", "agy_sub", "claude_sub"}
		}
	}
	if tier == TierHigh || tier == TierTop {
		// 高档和顶配先用 Claude 订阅的 Opus / Fable，任务表里的「按量优先」只对低中档有意义
		rest := []string{}
		for _, p := range prefer {
			if p != "claude_sub" {
				rest = append(rest, p)
			}
		}
		prefer = append([]string{"claude_sub"}, rest...)
	}
	if hit.SubOnly {
		prefer = []string{"claude_sub", "codex_sub", "agy_sub"}
		d.Reason = append(d.Reason, "显式要求只用订阅")
	} else {
		// prefer 是优先顺序而不是白名单：没列到的渠道排在后面当回退
		for _, lane := range []string{"claude_sub", "api", "codex_sub", "agy_sub"} {
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
	d.Auto = explicitTier == "" && t.Model == "" && t.Lane == "" && !hit.SubOnly
	if d.Auto {
		// 自动分档：这一档都用不了（不能看图、带工具、被限流、额度用完）时依次往上提，最多到高档；顶配只由显式指令和质量模式触发
		added := false
		for _, up := range []Tier{TierMid, TierHigh} {
			if tierRank(up) <= tierRank(d.Tier) {
				continue
			}
			var ignore []string
			for _, c := range r.textCandidates(cfg, t, up, preferFor(up, prefer), sig.HasImage, &ignore) {
				if c.Skipped != "" || hasTarget(d.Candidates, c) {
					continue
				}
				c.Tier = up
				d.Candidates = append(d.Candidates, c)
				added = true
			}
		}
		if added {
			d.Reason = append(d.Reason, TierLabel(d.Tier)+"都用不了时往上提档")
		}
	}
	if t.HasTools() && len(live(d.Candidates)) == 0 {
		// 带工具只能走按量付费；这一档没配按量付费模型时往下找，总比整轮报错强
		for _, down := range []Tier{TierMid, TierLow} {
			if tierRank(down) >= tierRank(d.Tier) {
				continue
			}
			var ignore []string
			alt := r.textCandidates(cfg, t, down, prefer, sig.HasImage, &ignore)
			for i := range alt {
				alt[i].Tier = down
			}
			if len(live(alt)) > 0 {
				d.Reason = append(d.Reason, fmt.Sprintf("请求带工具，%s没有可用的按量付费模型，改用%s", TierLabel(d.Tier), TierLabel(down)))
				d.Candidates = append(alt, d.Candidates...)
				break
			}
		}
	}
	if stickyTgt != nil {
		d.Candidates = withStickyFirst(*stickyTgt, d.Candidates)
	}
	r.pinChoice(t, d)
	if len(live(d.Candidates)) == 0 && t.dropToolImages() {
		d = r.Decide(t)
		d.Reason = append(d.Reason, "工具返回的图片没有能看图的模型，改成文字提示")
	}
	return d
}

// cooldownKey 是目标对应的冷却键：按量付费按账号记，订阅按渠道记。
func cooldownKey(tgt Target) LaneID {
	if tgt.Lane == LaneAPI {
		return LaneID("acct:" + tgt.AccountID)
	}
	return tgt.Lane
}

// withStickyFirst 把粘住的目标放到候选最前面，正常排序里的同一条去掉。
func withStickyFirst(st Target, cands []Target) []Target {
	out := []Target{st}
	for _, c := range cands {
		if c.Lane == st.Lane && c.AccountID == st.AccountID && c.Model == st.Model {
			continue
		}
		out = append(out, c)
	}
	return out
}

// dropToolImages 去掉工具返回的图片，换成一句话告诉模型，免得它以为工具坏了。返回有没有去掉。
func (t *Task) dropToolImages() bool {
	dropped := false
	for i := range t.Messages {
		if t.Messages[i].ToolImage {
			t.Messages[i].ToolImage, t.Messages[i].Images = false, nil
			t.Messages[i].Content = "工具返回了图片，但当前模型看不了图；工具本身是正常的"
			dropped = true
		}
	}
	return dropped
}

// accountWithModel 找配了这个型号的按量付费账号；档位里没配的，按型号的供应商匹配。找不到返回 nil。
func (r *Router) accountWithModel(model string) *config.APIAccount {
	if acc := r.accountListing(model); acc != nil {
		return acc
	}
	cfg := r.cfg.Get()
	if info, ok := r.reg.Lookup(model); ok {
		for i := range cfg.API {
			if cfg.API[i].Enabled && cfg.API[i].Provider == info.Provider {
				return &cfg.API[i]
			}
		}
	}
	return nil
}

// accountListing 找档位里配了、或自定义服务报了这个型号的账号；只认名字完全相同的。
func (r *Router) accountListing(model string) *config.APIAccount {
	cfg := r.cfg.Get()
	for i := range cfg.API {
		acc := &cfg.API[i]
		if !acc.Enabled {
			continue
		}
		for _, ms := range acc.Models {
			for _, m := range ms {
				if m == model {
					return acc
				}
			}
		}
		for _, d := range acc.Discovered {
			if d.ID == model {
				return acc
			}
		}
	}
	return nil
}

// pinChoice 让调用方点名的渠道或模型排在最前：点了渠道只留该渠道，点了模型把它提到首位（没有就临时构造一个候选）。
func (r *Router) pinChoice(t *Task, d *Decision) {
	if t.Lane == "" && t.Model == "" {
		return
	}
	var pinned, rest []Target
	for _, c := range d.Candidates {
		switch {
		case t.Model != "" && c.Model == t.Model && (t.Lane == "" || c.Lane == t.Lane):
			pinned = append(pinned, c)
		case t.Lane != "" && c.Lane == t.Lane:
			pinned = append(pinned, c)
		default:
			rest = append(rest, c)
		}
	}
	if len(pinned) == 0 && t.Model != "" {
		lane := t.Lane
		if lane == "" && r.accountListing(t.Model) != nil {
			// 账号里有这个型号就发给账号：自定义服务的 gpt-6.1-sol 和 ChatGPT 订阅里的同名，不能按厂商猜成订阅
			lane = LaneAPI
		} else if lane == "" {
			lane = LaneAPI
			if info, ok := r.reg.Lookup(t.Model); ok {
				switch info.Provider {
				case "anthropic":
					lane = LaneClaudeSub
				case "openai":
					lane = LaneCodexSub
				case "google":
					lane = LaneAgySub
				}
			}
		}
		tgt := Target{Lane: lane, Provider: providerOf(lane), Model: t.Model, EstCostUSD: r.reg.EstimateText(t.Model, t.EstTokens)}
		if lane != LaneAPI && lane != LaneMedia && t.HasTools() {
			tgt.Skipped = "带工具" // 订阅渠道接不了工具；不跳过的话会去调命令行，一直等到超时
		}
		if lane == LaneAPI {
			// 找哪个账号有这个型号：先看档位里配没配，再看型号的供应商和账号对不对得上
			if acc := r.accountWithModel(t.Model); acc != nil {
				tgt.AccountID, tgt.Provider = acc.ID, acc.Provider
			}
		}
		pinned = append(pinned, tgt)
	}
	if len(pinned) == 0 {
		d.Reason = append(d.Reason, "点名的渠道不可用，按默认顺序")
		return
	}
	if t.Lane != "" {
		d.Reason = append(d.Reason, "调用方指定渠道 "+string(t.Lane))
		d.Candidates = pinned
		return
	}
	d.Reason = append(d.Reason, "调用方指定模型 "+t.Model)
	d.Candidates = append(pinned, rest...)
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
// 这一档有手排的候选表就完全照表；表里的条目都失效了（账号删了、渠道关了）再按内置规则排。
func (r *Router) textCandidates(cfg *config.Config, t *Task, tier Tier, prefer []string, needVision bool, reason *[]string) []Target {
	var out []Target
	subAllowed := !t.HasTools()
	if !subAllowed {
		*reason = append(*reason, "请求带工具，跳过订阅渠道")
	}
	if picks, scope := tierPicks(cfg, t.Host, tier); len(picks) > 0 {
		out = r.pickedCandidates(cfg, t, tier, picks, needVision, subAllowed, slices.Contains(prefer, "api"))
		if len(out) > 0 {
			*reason = append(*reason, TierLabel(tier)+"按"+scope+"手排的候选表")
		}
	}
	if len(out) == 0 {
		out = r.preferCandidates(cfg, t, tier, prefer, needVision, subAllowed)
	}
	tooLong := 0
	for i := range out {
		if out[i].Skipped != "" {
			continue
		}
		if ok, window := r.fits(out[i].Model, t.EstTokens); !ok {
			out[i].Skipped = fmt.Sprintf("装不下（上限 %d 万 token）", window/10000)
			tooLong++
		}
	}
	if tier == TierTop && len(live(out)) == 0 {
		// 顶配只有 Claude 订阅有；都不可用时退到高档的候选
		*reason = append(*reason, "顶配不可用，退到高档")
		return r.textCandidates(cfg, t, TierHigh, prefer, needVision, reason)
	}
	if tooLong > 0 && len(live(out)) == 0 && tierRank(tier) < tierRank(TierHigh) {
		// 这一档的模型都装不下：往上找窗口更大的
		up := map[Tier]Tier{TierLow: TierMid, TierMid: TierHigh}[tier]
		*reason = append(*reason, fmt.Sprintf("内容约 %d 万 token，%s装不下，改用%s", (t.EstTokens+5000)/10000, TierLabel(tier), TierLabel(up)))
		return r.textCandidates(cfg, t, up, prefer, needVision, reason)
	}
	return out
}

// pickedCandidates 按用户手排的候选表展开：只认渠道现在在这一档还提供的条目，顺序完全照表，不再把免费型号往前挪。
func (r *Router) pickedCandidates(cfg *config.Config, t *Task, tier Tier, picks []config.TierPick, needVision, subAllowed, allowAPI bool) []Target {
	var out []Target
	for _, p := range picks {
		if p.Lane == "api" {
			if !allowAPI {
				continue
			}
			for _, acc := range cfg.API {
				if acc.ID == p.Account && acc.Enabled && slices.Contains(acc.Models[string(tier)], p.Model) {
					out = append(out, r.apiTarget(t, acc, p.Model, needVision))
				}
			}
			continue
		}
		if tgt, ok := r.subTarget(cfg, p.Lane, tier, subAllowed); ok {
			out = append(out, tgt)
		}
	}
	return out
}

// preferCandidates 按渠道偏好顺序展开内置排法：订阅渠道各出这一档的型号，按量付费账号各出这一档的型号列表，免费的排前面。
func (r *Router) preferCandidates(cfg *config.Config, t *Task, tier Tier, prefer []string, needVision, subAllowed bool) []Target {
	var out []Target
	for _, lane := range prefer {
		switch lane {
		case "claude_sub", "codex_sub", "agy_sub":
			if tgt, ok := r.subTarget(cfg, lane, tier, subAllowed); ok {
				out = append(out, tgt)
			}
		case "api":
			// 免费型号排在收费型号前面：同档位里先用不花钱的
			apiStart := len(out)
			for _, acc := range cfg.API {
				if !acc.Enabled {
					continue
				}
				for _, m := range acc.Models[string(tier)] {
					out = append(out, r.apiTarget(t, acc, m, needVision))
				}
			}
			sort.SliceStable(out[apiStart:], func(i, j int) bool {
				fi := r.reg.IsFree(out[apiStart+i].Model)
				fj := r.reg.IsFree(out[apiStart+j].Model)
				return fi && !fj
			})
		}
	}
	return out
}

// subTarget 把一条订阅渠道展开成这一档的候选并标好跳过原因；渠道没开或这一档没配型号时返回 false。
func (r *Router) subTarget(cfg *config.Config, lane string, tier Tier, subAllowed bool) (Target, bool) {
	lid := LaneID(lane)
	sl, ok := cfg.Sub[lane]
	if !ok || !sl.Enabled {
		return Target{}, false
	}
	model := sl.Models[string(tier)]
	if model == "" {
		return Target{}, false // 顶配只有 Claude 订阅有
	}
	tgt := Target{Lane: lid, Provider: providerOf(lid), Model: model, EstCostUSD: 0}
	if !subAllowed {
		tgt.Skipped = "带工具"
	} else if _, ok := r.drivers[lid]; !ok {
		tgt.Skipped = "未接入"
		tgt.NoService = true
	} else {
		h := r.drivers[lid].Health(context.Background())
		r.quota.Observe(lid, h.Quota)
		if h.Status == "missing" || h.Status == "error" || h.Status == "disabled" || h.SignedOut {
			tgt.Skipped = h.Detail
			tgt.NoService = h.Status != "error"
		} else if ok, why := r.quota.Healthy(lid, tier, sl.Models); !ok {
			tgt.Skipped = why
		}
	}
	return tgt, true
}

// apiTarget 把按量付费账号的一个型号展开成候选并标好跳过原因。
func (r *Router) apiTarget(t *Task, acc config.APIAccount, m string, needVision bool) Target {
	info, _ := r.reg.Lookup(m)
	tgt := Target{Lane: LaneAPI, Provider: acc.Provider, Model: m, AccountID: acc.ID, EstCostUSD: r.reg.EstimateText(m, t.EstTokens)}
	if needVision && !info.Vision {
		tgt.Skipped = "不能看图"
	}
	if !acc.AllowPaid && len(r.reg.FreeModels(acc.Provider)) > 0 && !info.Free {
		tgt.Skipped = "只用免费型号"
	}
	if cooling, _, why := r.quota.Cooling(LaneID("acct:" + acc.ID)); cooling {
		tgt.Skipped = why
	}
	return tgt
}

// defaultPrefer 是没有任务表偏好时的渠道顺序：低档先按量付费，中高档先 Claude 订阅。
func defaultPrefer(tier Tier) []string {
	if tier == TierLow {
		return []string{"api", "codex_sub", "agy_sub", "claude_sub"}
	}
	return []string{"claude_sub", "api", "codex_sub", "agy_sub"}
}

// tierPicks 找一档该用哪张候选表：宿主自己的优先，其次全局的；都没有返回空。scope 是给原因看的「宿主」「全局」。
func tierPicks(cfg *config.Config, host string, tier Tier) ([]config.TierPick, string) {
	if host != "" {
		if picks := cfg.Integrations[host].Tiers[string(tier)]; len(picks) > 0 {
			return picks, "这个应用"
		}
	}
	if picks := cfg.Routing.Tiers[string(tier)]; len(picks) > 0 {
		return picks, "全局"
	}
	return nil, ""
}

// TierTable 给界面看某个宿主（host 为空是全局）一档的候选：chosen 是现在生效的顺序，avail 是这一档所有渠道提供的全部候选（内置排法的顺序），
// inherited 是这个宿主不手排时会用的顺序（全局的表或内置排法）。custom 表示 host 这一层自己排过。
// 按不带工具、不带附件的普通请求算，跳过原因（未接入、冷却中）照标，界面上作提示。
func (r *Router) TierTable(host string, tier Tier) (chosen, avail, inherited []Target, custom bool) {
	cfg := r.cfg.Get()
	t := &Task{Kind: KindText}
	avail = r.preferCandidates(cfg, t, tier, defaultPrefer(tier), false, true)
	inherited = avail
	if host != "" {
		if picks := cfg.Routing.Tiers[string(tier)]; len(picks) > 0 {
			if g := r.pickedCandidates(cfg, t, tier, picks, false, true, true); len(g) > 0 {
				inherited = g
			}
		}
	}
	var own []config.TierPick
	if host != "" {
		own = cfg.Integrations[host].Tiers[string(tier)]
	} else {
		own = cfg.Routing.Tiers[string(tier)]
	}
	if len(own) > 0 {
		if chosen = r.pickedCandidates(cfg, t, tier, own, false, true, true); len(chosen) > 0 {
			return chosen, avail, inherited, true
		}
	}
	return inherited, avail, inherited, false
}

// contextHeadroom 是只用上下文窗口的多少：估算有误差，还要给回答留地方。
const contextHeadroom = 0.9

// fits 判断估算的输入放不放得进这个模型；注册表里没有窗口数据的模型一律放行。
func (r *Router) fits(model string, est int) (bool, int) {
	info, ok := r.reg.Lookup(model)
	if !ok || info.Context <= 0 {
		return true, 0
	}
	return float64(est) <= float64(info.Context)*contextHeadroom, info.Context
}

// MaxContext 是当前启用的文本模型里最大的上下文窗口；兼容入口用它在流式输出前判断「谁都装不下」。0 表示不知道。
func (r *Router) MaxContext() int {
	cfg := r.cfg.Get()
	best := 0
	see := func(m string) {
		if info, ok := r.reg.Lookup(m); ok && info.MediaKind == "" && info.Context > best {
			best = info.Context
		}
	}
	for lane, sl := range cfg.Sub {
		if _, ok := r.drivers[LaneID(lane)]; !ok || !sl.Enabled {
			continue
		}
		for _, m := range sl.Models {
			see(m)
		}
	}
	for _, acc := range cfg.API {
		if !acc.Enabled {
			continue
		}
		for _, ms := range acc.Models {
			for _, m := range ms {
				see(m)
			}
		}
	}
	return best
}

// FitsAny 判断估算的输入能不能放进任何一个启用的模型；不能时返回 ErrContextTooLong。
func (r *Router) FitsAny(est int) error {
	if max := r.MaxContext(); max > 0 && float64(est) > float64(max)*contextHeadroom {
		return &ErrContextTooLong{Tokens: est, MaxWindow: max}
	}
	return nil
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

// mediaCandidates 返回媒体任务的候选。用户手排过这一类的先后（分派 → 档位候选），又没指定档位，就照手排的；
// 否则先是所选档位，再是其它档位作为回退。
func (r *Router) mediaCandidates(cfg *config.Config, t *Task, mediaTier string) []Target {
	out := r.mediaBase(cfg, t, mediaTier)
	if order := cfg.Routing.MediaOrder[string(t.Kind)]; len(order) > 0 && t.MediaTier == "" && t.Kind != KindAnimation {
		pool := append(out, r.discoveredMedia(cfg, t.Kind, out)...)
		var picked []Target
		for _, id := range order {
			for _, c := range pool {
				if MediaID(c) == id && !hasTarget(picked, c) {
					picked = append(picked, c)
					break
				}
			}
		}
		if len(live(picked)) > 0 {
			return r.skipRefUnsupported(t, picked)
		}
	}
	// 排序：能用的付费渠道在前，ChatGPT 订阅出图当兜底，没账号的放最后只作提示
	rank := func(c Target) int {
		switch {
		case c.Skipped != "":
			return 2
		case c.Provider == "codex":
			return 1
		}
		return 0
	}
	out = r.skipRefUnsupported(t, out)
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

// MediaID 是媒体候选在手排表里的标识：账号（没有账号就是供应商）/型号。
func MediaID(c Target) string {
	k := c.AccountID
	if k == "" {
		k = c.Provider
	}
	return k + "/" + c.Model
}

// skipRefUnsupported：带参考图时，只能文生图的型号出的图跟参考图无关，钱照收：跳过，一个都不剩就直接报错
func (r *Router) skipRefUnsupported(t *Task, out []Target) []Target {
	if t.Kind == KindImage && len(t.ImageRefs()) > 0 {
		for i := range out {
			if out[i].Skipped == "" && RefImageUnsupported(out[i].Model) {
				out[i].Skipped = "不接参考图"
			}
		}
	}
	return out
}

// mediaBase 是各档配的型号加账号自己选的出图出视频型号，没排序。
func (r *Router) mediaBase(cfg *config.Config, t *Task, mediaTier string) []Target {
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
		if ready, why := r.mediaProviderReady(cfg, mt.Provider); !ready {
			tgt.Skipped = why
			tgt.NoService = true
		} else if acc := r.accountFor(cfg, mt.Provider, mt.AccountID); acc != nil && !acc.AllowPaid && len(r.reg.FreeModels(acc.Provider)) > 0 && !r.reg.IsFree(mt.Model) {
			tgt.Skipped = "只用免费型号"
		}
		out = append(out, tgt)
	}
	// 账号自己选了出图 / 出视频型号的按量付费账号，自动当作候选，不用改媒体档位配置；Agnes 默认用免费型号
	for _, acc := range cfg.API {
		if !acc.Enabled {
			continue
		}
		kind := "image"
		if t.Kind == KindVideo {
			kind = "video"
		}
		model := acc.MediaModel(kind, r.DefaultMediaModel(acc.Provider, kind))
		if model == "" {
			continue
		}
		if !acc.AllowPaid && len(r.reg.FreeModels(acc.Provider)) > 0 && !r.reg.IsFree(model) {
			continue
		}
		dup := false
		for _, c := range out {
			if c.Model == model {
				dup = true
			}
		}
		if !dup {
			usd, _, _ := r.reg.MediaCost(model, units)
			out = append(out, Target{Lane: LaneMedia, Provider: acc.Provider, Model: model, EstCostUSD: usd, AccountID: acc.ID})
		}
	}
	return out
}

func providerLabel(p string) string {
	switch p {
	case "winwork":
		return "WinWork"
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
	case "deepseek":
		return "DeepSeek"
	case "moonshot":
		return "Moonshot"
	case "minimax":
		return "MiniMax"
	case "google":
		return "Google AI"
	case "agnes":
		return "Agnes AI"
	}
	return p
}

// ProviderLabel 导出给接入面用。
func ProviderLabel(p string) string { return providerLabel(p) }

func (r *Router) mediaProviderReady(cfg *config.Config, provider string) (bool, string) {
	if provider == "codex" { // 订阅出图走本机 Codex：没装或没登录都画不了
		d, ok := r.drivers[LaneCodexSub]
		if !ok {
			return false, "没装 Codex"
		}
		if h := d.Health(context.Background()); h.Status == "missing" || h.SignedOut {
			return false, h.Detail
		}
		return true, ""
	}
	for _, a := range cfg.API {
		if a.Enabled && a.Provider == provider {
			return true, ""
		}
	}
	return false, "未添加 " + providerLabel(provider) + " 账号"
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
	kind, prompt := t.Kind, t.Prompt
	d := r.Decide(t)
	if d.Kind == KindText && (t.Smart || (d.Auto && r.cfg.Get().Routing.Smart.Enabled)) {
		r.smartPick(ctx, t, d) // 分派方式选了智能分派：自动分档的请求都请判官挑，替代低档判档员，不串行等两次
	} else if d.Kind == KindImage || d.Kind == KindVideo {
		r.withMediaContext(ctx, t, d)      // 「根据这个文案画一张」：先把上文并进提示词，判官挑型号时也看得到
		r.smartPickMedia(ctx, t, d, false) // 出图出视频不经过应用的模型菜单：打开智能分派就生效
	} else if d.Unsure && !d.Sticky && !r.cfg.Get().Settings.RulesOnly && t.Source != "classifier" && t.Model == "" && t.Lane == "" && StripInjected(prompt) != "" {
		label, c, model := r.classify(ctx, prompt)
		if label != "" {
			t.JudgeCostUSD = c
			t.Kind, t.Prompt, t.Judged = kind, prompt, label
			d = r.Decide(t)
			if model != "" {
				d.Reason = append(d.Reason, "判档用 "+r.reg.Display(model))
			}
		} else {
			d.Reason = append(d.Reason, "低档模型没判出来，按规则走")
		}
	}
	return r.Execute(ctx, t, d, emit)
}

// Execute 按决策执行。
func (r *Router) Execute(ctx context.Context, t *Task, d *Decision, emit func(Event)) (*Call, *Result, error) {
	r.inflight.Add(1)
	defer r.inflight.Add(-1)
	start := time.Now()
	cfg := r.cfg.Get()
	call := &Call{ID: t.ID, TS: start, Source: t.Source, Host: t.Host, SessionID: t.SessionID, Kind: t.Kind, TaskType: d.TaskType,
		Tier: d.Tier, MediaTier: d.MediaTier, Reason: d.Reason, Candidates: d.Candidates, JobID: ""}
	if cfg.Settings.SavePrompts {
		ask := t.Prompt
		if t.Ask != "" {
			ask = t.Ask
		}
		call.PromptExcerpt = excerpt(StripInjected(ask), 200)
	}
	var lastErr error
	var fallbackFrom string
	tried := 0
	// 按量付费月上限
	overBudget := false
	if spent, err := r.ledger.MonthCost(r.loc); err == nil && cfg.Settings.MonthlyAPILimitUSD > 0 && spent >= cfg.Settings.MonthlyAPILimitUSD {
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
		if r.onAttempt != nil {
			r.onAttempt(t, *c, r.attemptInfo(t, d, *c, tried))
		}
		res, err := drv.Run(ctx, t, *c, emit)
		if err == nil {
			r.finish(call, t, *c, res, start, fallbackFrom)
			if t.SessionID != "" && t.Kind == KindText {
				r.sticky.Set(t.SessionID, *c, call.Tier)
			}
			return call, res, nil
		}
		lastErr = err
		c.Skipped = oneLine(err)
		if d.Sticky {
			r.sticky.Forget(t.SessionID) // 粘住的目标出错了：下一轮重新选，成功的那条会重新记上
		}
		var rl *ErrRateLimited
		var pe *ErrProvider
		switch {
		case errors.As(err, &rl):
			r.quota.MarkCooldown(cooldownKey(*c), rl.RetryAfter, rl.Detail)
			if c.Lane != LaneAPI && c.Lane != LaneMedia {
				r.notify("额度用完", providerLabel(c.Provider)+" 订阅额度暂时用完，同档任务改走按量付费", "warn")
			}
			fallbackFrom = c.Model
		case errors.As(err, &pe) && pe.Account && c.Lane == LaneAPI:
			r.quota.MarkCooldown(LaneID("acct:"+c.AccountID), pe.Cooldown, pe.Summary)
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
		if d.Kind == KindText {
			if err := r.FitsAny(t.EstTokens); err != nil {
				lastErr = err
			}
		}
	}
	r.fail(call, t, Target{}, lastErr, start)
	return call, nil, lastErr
}

func (r *Router) attemptInfo(t *Task, d *Decision, c Target, n int) Attempt {
	a := Attempt{N: n, Tier: d.Tier, TaskType: d.TaskType, Sticky: d.Sticky}
	if c.Tier != "" {
		a.Tier = c.Tier
	}
	if t.Kind == KindText && t.Model != "" && c.Model == t.Model {
		if tier := r.TierOfModel(c); tier != "" {
			a.Tier = tier
		}
	}
	if d.Smart != nil && len(d.Smart.Options) > 0 && d.Tier != "" && a.Tier != d.Tier && strings.HasPrefix(c.Why, "智能分派") {
		a.RuleTier, a.Judge = d.Tier, r.judgeLabel()
	}
	return a
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
	if t.Kind == KindText && t.Model != "" && tgt.Model == t.Model {
		// 应用点名了型号：档位记这个型号在配置里所在的那一档，不记规则判的（规则判低档、点名的是高档型号时记成低档会误导）
		if tier := r.TierOfModel(tgt); tier != call.Tier {
			if tier != "" {
				call.Reason = append(call.Reason, "点名的型号在"+TierLabel(tier)+"，按"+TierLabel(tier)+"记")
			}
			call.Tier = tier
		}
	} else if tgt.Tier != "" && tgt.Tier != call.Tier {
		// 记实际用到的那一档：提档、降档后统计和「判低了」都按它算
		why := tgt.Why
		if why == "" {
			why = fmt.Sprintf("%s都用不了，改用%s", TierLabel(call.Tier), TierLabel(tgt.Tier))
		}
		call.Reason = append(call.Reason, why)
		call.Tier = tgt.Tier
	}
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
	call.HostModel = t.HostModel
	if t.Kind == KindText {
		in, out, cached := res.Usage.InputTokens, res.Usage.OutputTokens, res.Usage.CacheReadTokens
		if in == 0 && out == 0 { // 订阅命令行没报 token 数：按估的算
			in, out = t.EstTokens, int(float64(t.EstTokens)*0.6)+200
		}
		r.applyBaseline(call, in, out, cached)
	} else {
		call.BaselineCostUSD, call.BaselineModel = call.CostUSD, call.Model
	}
	call.LatencyMS = int(time.Since(start).Milliseconds())
	call.OK = true
	call.ResultPath = res.Path
	call.JobID = res.JobID
	call.ToolCalls = countToolCalls(res.ToolCalls)
	if r.cfg.Get().Settings.SavePrompts {
		call.ResultText = excerpt(res.Text, 2000)
		// 评估要用原话重做：只存单轮文本请求，超过上限的不存（截断后重做就不公平了）
		if t.Kind == KindText && len(t.Messages) <= 1 && runeLen(t.Prompt) <= evalFullLimit && runeLen(res.Text) <= evalFullLimit {
			call.PromptFull, call.ResultFull = t.Prompt, res.Text
		}
	}
	call.CostUSD += t.JudgeCostUSD // 判档的花费算在这次请求上，基线不变
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
	var tl *ErrContextTooLong
	call.TooLong = errors.As(err, &tl)
	call.CostUSD += t.JudgeCostUSD
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

// countToolCalls 数模型要应用去跑几个工具（OpenAI 格式数组）。
func countToolCalls(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil {
		return 0
	}
	return len(arr)
}

// evalFullLimit 是为评估保存完整提问和回答的字数上限。
const evalFullLimit = 20000

func runeLen(s string) int { return len([]rune(s)) }

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

// Decorate 给一条记录补上任务名和模型显示名（只用于输出）。
func (r *Router) Decorate(c *Call) *Call {
	if c == nil {
		return c
	}
	c.TaskName = r.TaskName(c.TaskType)
	c.ModelDisplay = r.reg.Display(c.Model)
	if c.BaselineModel != "" {
		c.BaselineDisplay = r.reg.Display(c.BaselineModel)
	}
	r.decorateBaselinePrice(c)
	return c
}

// TaskName 任务类型的中文名。
func (r *Router) TaskName(tt string) string {
	for _, t := range r.cfg.Get().Routing.TaskTable {
		if t.Type == tt {
			return t.Name
		}
	}
	if n, ok := HeuristicNames[tt]; ok {
		return n
	}
	if tt == "judged" {
		return "模型判档"
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
		if s.Escalations < 5 || ignored[s.TaskType] || IsHeuristic(s.TaskType) {
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

// accountFor 找供应商对应的按量付费账号；给了 id 就按 id 找。
func (r *Router) accountFor(cfg *config.Config, provider, id string) *config.APIAccount {
	for i := range cfg.API {
		a := &cfg.API[i]
		if id != "" && a.ID == id {
			return a
		}
		if id == "" && a.Provider == provider && a.Enabled {
			return a
		}
	}
	return nil
}

// DefaultMediaModel 是账号没选时的媒体型号：Agnes 用免费的 flash；其它供应商不自动出图，要用户在档位里选。
func (r *Router) DefaultMediaModel(provider, kind string) string {
	if provider != "agnes" {
		return ""
	}
	if kind == "video" {
		return "agnes-video-2.5-flash"
	}
	return "agnes-image-2.5-flash"
}

// preferFor 是某一档的渠道顺序：高档和顶配先用 Claude 订阅（跟 Decide 里的规则一样）。
func preferFor(tier Tier, prefer []string) []string {
	if tier != TierHigh && tier != TierTop {
		return prefer
	}
	out := []string{"claude_sub"}
	for _, p := range prefer {
		if p != "claude_sub" {
			out = append(out, p)
		}
	}
	return out
}

// hasTarget 判断候选里是不是已经有同一个渠道、账号、型号。
func hasTarget(ts []Target, c Target) bool {
	for _, x := range ts {
		if x.Lane == c.Lane && x.Model == c.Model && x.AccountID == c.AccountID {
			return true
		}
	}
	return false
}

// TierOfModel 是这个候选在配置里所在的档位：按量付费看账号分的档，订阅看各档配的型号；
// 同一个型号在好几档里取最高的；哪档都没有就按型号名认（Luna、mini、Haiku 低档，Terra、Sonnet 中档，Sol、pro、Opus 高档），
// 名字也认不出返回空（界面显示「指定」）。
func (r *Router) TierOfModel(c Target) Tier {
	cfg := r.cfg.Get()
	for _, tier := range []Tier{TierHigh, TierMid, TierLow} {
		if c.Lane == LaneAPI {
			for _, a := range cfg.API {
				if (c.AccountID == "" || a.ID == c.AccountID) && slices.Contains(a.Models[string(tier)], c.Model) {
					return tier
				}
			}
			continue
		}
		if sl, ok := cfg.Sub[string(c.Lane)]; ok && sl.Models[string(tier)] == c.Model {
			return tier
		}
	}
	return TierByName(c.Model)
}

// TierByName 按型号名猜档位，和兼容接口理解 Codex、Claude 自家型号名的规则一致。
func TierByName(model string) Tier {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "haiku"), strings.Contains(m, "luna"), strings.Contains(m, "mini"), strings.Contains(m, "nano"), strings.Contains(m, "flash"):
		return TierLow
	case strings.Contains(m, "opus"), strings.Contains(m, "fable"), strings.Contains(m, "-sol"), strings.Contains(m, "-pro"), strings.HasSuffix(m, "pro"):
		return TierHigh
	case strings.Contains(m, "sonnet"), strings.Contains(m, "terra"):
		return TierMid
	}
	return ""
}
