// Package admin 是给界面和命令行用的本机接口：/api/*。只监听 127.0.0.1，要求请求头 X-IMLS-Client。
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/imoling/iml-switchboard/internal/config"
	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/lanes/laya"
	"github.com/imoling/iml-switchboard/internal/themes"
)

// Version 由 main 注入。
var Version = "dev"

// AccountOps 由按量付费渠道提供：验证密钥、列模型、默认模型表。
type AccountOps interface {
	Verify(ctx context.Context, provider, baseURL, apiKey string) (models []string, err error)
	// ListModels 拉自定义服务的型号列表；同时就验证了地址和密钥
	ListModels(ctx context.Context, baseURL, apiKey string) ([]config.DiscoveredModel, error)
	Preset(provider string) (baseURL, currency string, tiers map[string][]string, needsProxy bool, ok bool)
	Providers() []string
	Reload(cfg *config.Config)
	// Discover 找本机能自动接的账号（目前是 WinWork）；找到新的返回真
	Discover() bool
}

// MediaOps 由媒体渠道提供：任务的取消、重试、改一句重做。
type MediaOps interface {
	Cancel(ctx context.Context, jobID string) (*core.Job, error)
	Retry(ctx context.Context, jobID string, model string) (*core.Job, error)
	Revise(ctx context.Context, jobID string, instruction string) (*core.Job, error)
}

// IntegrationOps 由 integrations 包提供。
type IntegrationOps interface {
	List() []map[string]any
	Connect(id string, opts map[string]bool) (map[string]any, error)
	Disconnect(id string) (map[string]any, error)
	Update(id string, opts map[string]bool) (map[string]any, error)
	// HostModels 和 SetHostModels 管宿主菜单里列出的型号表（Codex、WorkBuddy、pi 等）；ids 为空表示回到默认
	HostModels(id string) (any, error)
	SetHostModels(id string, ids []string) (any, error)
}

// Deps 是接口层需要的全部依赖。
type Deps struct {
	Router        *core.Router
	Jobs          *core.MemJobStore
	Bus           *core.Bus
	Accounts      AccountOps
	Media         MediaOps
	Integr        IntegrationOps
	Secrets       core.Secrets
	StartedAt     time.Time
	NodeAvailable func() bool
	Build         string                                             // 程序文件指纹，见 app.BuildID
	Quit          func()                                             // 让后台自己退出；客户端「重启」先调它，再拉起安装包里的那份
	Update        func(ctx context.Context, force bool) (any, error) // 查新版本；nil 表示不支持
	Laya          *laya.Manager                                      // 本机 Laya；nil 表示不支持
}

// Server 是 /api 处理器。
type Server struct {
	d          Deps
	mu         sync.Mutex
	projection bool
	refreshing bool
	evals      map[string]*core.Evaluation
}

// New 创建处理器。
func New(d Deps) *Server {
	s := &Server{d: d, evals: map[string]*core.Evaluation{}}
	if raws, err := d.Router.Ledger().ListEvaluations(); err == nil {
		for _, raw := range raws {
			var e core.Evaluation
			if json.Unmarshal(raw, &e) == nil {
				if e.Status == "running" {
					e.Status = "failed"
					e.Error = "服务重启，评估中断"
				}
				s.evals[e.ID] = &e
			}
		}
	}
	return s
}

// Projection 返回投屏模式状态。
func (s *Server) Projection() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.projection }

// Routes 注册到 mux。
func (s *Server) Routes(mux *http.ServeMux) {
	h := func(pattern string, fn func(http.ResponseWriter, *http.Request) (any, error)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-IMLS-Client") == "" && !strings.HasPrefix(r.URL.Path, "/api/events") {
				writeErr(w, 403, "缺少客户端标识")
				return
			}
			v, err := fn(w, r)
			if err != nil {
				code := 400
				var he *httpError
				if errors.As(err, &he) {
					code = he.code
				}
				writeErr(w, code, err.Error())
				return
			}
			if v != nil {
				writeJSON(w, v)
			}
		})
	}
	h("GET /api/status", s.status)
	h("POST /api/quit", s.quit)
	h("GET /api/update", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.update(r, false) })
	h("POST /api/update/check", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.update(r, true) })
	h("POST /api/pause", s.pause)
	h("POST /api/mode", s.mode)
	h("POST /api/projection", s.projectionToggle)
	h("GET /api/overview", s.overview)
	h("POST /api/quota/refresh", s.quotaRefresh)
	h("GET /api/calls", s.calls)
	h("GET /api/calls/{id}", s.call)
	h("POST /api/calls/{id}/escalate", s.escalate)
	h("POST /api/calls/{id}/redo", s.redo)
	h("GET /api/rules", s.rules)
	h("PUT /api/rules", s.putRules)
	h("POST /api/rules/try", s.tryRule)
	h("GET /api/smart", s.smartGet)
	h("PUT /api/smart", s.smartPut)
	h("POST /api/smart/test", s.smartTest)
	h("POST /api/smart/laya/{action}", s.smartLaya)
	h("GET /api/media/models", s.mediaModels)
	h("GET /api/rules/suggestions", s.suggestions)
	h("POST /api/rules/suggestions/{task}", s.applySuggestion)
	h("GET /api/rules/export", s.exportRules)
	h("POST /api/rules/import", s.importRules)
	h("GET /api/accounts", func(w http.ResponseWriter, r *http.Request) (any, error) {
		s.d.Accounts.Discover() // 只看几个文件在不在：WinWork 后装、后登录的，打开账号页就出现
		return s.accountViews(), nil
	})
	h("GET /api/providers", s.providers)
	h("POST /api/accounts", s.addAccount)
	h("POST /api/accounts/detect", func(w http.ResponseWriter, r *http.Request) (any, error) {
		s.d.Accounts.Discover()
		return s.accountViews(), nil
	})
	h("PUT /api/accounts/{id}", s.updateAccount)
	h("DELETE /api/accounts/{id}", s.deleteAccount)
	h("POST /api/accounts/{id}/verify", s.verifyAccount)
	h("GET /api/media", s.media)
	h("GET /api/jobs/{id}", s.job)
	h("POST /api/jobs/{id}/cancel", s.jobCancel)
	h("DELETE /api/jobs/{id}", s.jobDelete)
	h("DELETE /api/calls/{id}", s.callDelete)
	h("POST /api/jobs/{id}/retry", s.jobRetry)
	h("POST /api/jobs/{id}/revise", s.jobRevise)
	h("GET /api/integrations", s.integrations)
	h("POST /api/integrations/{id}/connect", s.integrationConnect)
	h("POST /api/integrations/{id}/disconnect", s.integrationDisconnect)
	h("PUT /api/integrations/{id}", s.integrationUpdate)
	h("GET /api/integrations/{id}/models", s.integrationModels)
	h("PUT /api/integrations/{id}/models", s.integrationSetModels)
	h("GET /api/integrations/{id}/tiers", s.integrationTiers)
	h("PUT /api/integrations/{id}/tiers", s.integrationSetTiers)
	h("PUT /api/integrations/{id}/baseline", s.integrationSetBaseline)
	h("GET /api/settings", s.settings)
	h("PUT /api/settings", s.putSettings)
	h("GET /api/themes", func(w http.ResponseWriter, r *http.Request) (any, error) { return themes.All(), nil })
	h("GET /api/evaluations", s.evaluations)
	h("POST /api/evaluations", s.startEvaluation)
	h("GET /api/evaluations/{id}", s.evaluation)
	h("POST /api/evaluations/{id}/accept", s.evaluationAccept)
	h("POST /api/quick/preview", s.quickPreview)
	h("GET /api/onboarding", s.onboarding)
	h("POST /api/onboarding/done", s.onboardingDone)
	h("POST /api/internal/tool", s.internalTool)
	mux.HandleFunc("POST /api/quick/run", s.quickRun)
	mux.HandleFunc("GET /api/events", s.events)
}

type httpError struct {
	code int
	msg  string
}

func (e *httpError) Error() string { return e.msg }
func notFound(msg string) error    { return &httpError{404, msg} }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func decode(r *http.Request, into any) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	return json.Unmarshal(b, into)
}

// --- 状态 ---

func (s *Server) statusBody() map[string]any {
	cfg := s.d.Router.Config().Get()
	theme := cfg.Settings.Theme
	if s.Projection() {
		theme = "standard"
	}
	node := false
	if s.d.NodeAvailable != nil {
		node = s.d.NodeAvailable()
	}
	return map[string]any{
		"version": Version, "uptime_s": int(time.Since(s.d.StartedAt).Seconds()),
		"mode": s.d.Router.Mode(), "theme": theme, "configured_theme": cfg.Settings.Theme,
		"paused": s.d.Router.Paused(), "projection": s.Projection(), "node_available": node,
		"data_dir": config.DataDir(), "app_title": themes.Word(theme, "app.title"),
		"currency": currencyOf(cfg.Settings), "usd_per_cny": cfg.Settings.USDPerCNY,
		"build": s.d.Build, "busy": s.busy(),
	}
}

// busy 是重启会打断的活：正在执行的请求，加上还没结束的媒体任务。
func (s *Server) busy() int {
	n := s.d.Router.InFlight()
	if s.d.Jobs != nil {
		n += s.d.Jobs.Active()
	}
	return n
}

func (s *Server) update(r *http.Request, force bool) (any, error) {
	if s.d.Update == nil {
		return nil, errors.New("这个后台不支持检查更新")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	return s.d.Update(ctx, force)
}

// quit 先回话再退出，不然客户端收不到结果。
func (s *Server) quit(w http.ResponseWriter, r *http.Request) (any, error) {
	if s.d.Quit == nil {
		return nil, errors.New("这个后台不支持从界面重启")
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		s.d.Quit()
	}()
	return map[string]any{"ok": true}, nil
}

func currencyOf(st config.Settings) string {
	if st.CNY() {
		return "CNY"
	}
	return "USD"
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.statusBody(), nil
}

func (s *Server) pause(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Paused bool `json:"paused"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	s.d.Router.SetPaused(in.Paused)
	s.d.Bus.Publish("status", s.statusBody())
	return s.statusBody(), nil
}

func (s *Server) mode(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Mode string `json:"mode"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	switch core.Mode(in.Mode) {
	case core.ModeSave, core.ModeBalanced, core.ModeQuality:
		s.d.Router.SetMode(core.Mode(in.Mode))
	default:
		return nil, errors.New("调度模式只能是省钱、均衡、质量")
	}
	s.d.Bus.Publish("status", s.statusBody())
	return s.statusBody(), nil
}

func (s *Server) projectionToggle(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		On bool `json:"on"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.projection = in.On
	s.mu.Unlock()
	s.d.Bus.Publish("status", s.statusBody())
	return s.statusBody(), nil
}

// --- 额度探测 ---

// maybeRefreshQuota 在后台让 Claude 做一次极小调用拿真实额度；30 分钟内不重复。
func (s *Server) maybeRefreshQuota(force bool) {
	q := s.d.Router.Quota()
	if !force && time.Since(q.ObservedAt(core.LaneClaudeSub)) < 30*time.Minute {
		return
	}
	s.mu.Lock()
	if s.refreshing {
		s.mu.Unlock()
		return
	}
	s.refreshing = true
	s.mu.Unlock()
	go func() {
		defer func() { s.mu.Lock(); s.refreshing = false; s.mu.Unlock() }()
		drivers := map[core.LaneID]core.Driver{}
		for _, lane := range []core.LaneID{core.LaneClaudeSub, core.LaneCodexSub, core.LaneAgySub} {
			if d, ok := s.d.Router.Driver(lane); ok {
				drivers[lane] = d
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		q.Refresh(ctx, drivers, force)
		s.d.Bus.Publish("quota", map[string]any{"quotas": s.quotas(s.d.Router.Config().Get())})
	}()
}

func (s *Server) quotaRefresh(w http.ResponseWriter, r *http.Request) (any, error) {
	s.maybeRefreshQuota(true)
	return map[string]any{"started": true, "quotas": s.quotas(s.d.Router.Config().Get())}, nil
}

// --- 概览与记录 ---

func (s *Server) overview(w http.ResponseWriter, r *http.Request) (any, error) {
	days := 7
	switch r.URL.Query().Get("range") {
	case "today":
		days = 1
	case "30d":
		days = 30
	}
	ov, err := s.d.Router.Ledger().Aggregate(days, time.Local)
	if err != nil {
		return nil, err
	}
	cfg := s.d.Router.Config().Get()
	s.maybeRefreshQuota(false)
	quotas := s.quotas(cfg)
	pct := func(m map[string]int, keys ...string) map[string]int {
		total := 0
		for _, v := range m {
			total += v
		}
		out := map[string]int{}
		for _, k := range keys {
			if total > 0 {
				out[k] = int(float64(m[k])/float64(total)*100 + 0.5)
			} else {
				out[k] = 0
			}
		}
		return out
	}
	delta := 0.0
	if ov.PrevCalls > 0 {
		delta = math.Round(float64(ov.Calls-ov.PrevCalls) / float64(ov.PrevCalls) * 100) // 整数就够，小数点后十几位没人看
	}
	return map[string]any{
		"calls": ov.Calls, "prev_calls": ov.PrevCalls, "calls_delta_pct": delta, "api_cost_usd": ov.APICostUSD, "baseline_cost_usd": ov.BaselineCostUSD,
		"saved_usd": ov.SavedUSD, "saved_pct": ov.SavedPct, "escalation_rate": ov.EscalationRate, "escalation_hot_task": ov.EscalationHot,
		"escalation_hot_name": s.d.Router.TaskName(ov.EscalationHot),
		"tier_dist":           pct(ov.TierDist, "low", "mid", "high", "top", "media"), "lane_dist": pct(ov.LaneDist, "sub", "api", "media"), "tier_counts": ov.TierDist, "lane_counts": ov.LaneDist, "daily": ov.Daily, "quotas": quotas,
		"budget":    map[string]float64{"month_usd": ov.MonthAPICostUSD, "limit_usd": cfg.Settings.MonthlyAPILimitUSD},
		"top_tasks": s.namedTasks(ov.TopTasks),
	}, nil
}

func (s *Server) namedTasks(ts []core.TopTask) []map[string]any {
	out := []map[string]any{}
	for _, t := range ts {
		metric := config.Count(t.Count) + " 次"
		if t.CostUSD > 0 {
			metric = s.d.Router.Config().Get().Settings.Money(t.CostUSD)
		}
		out = append(out, map[string]any{"task_type": t.TaskType, "name": s.d.Router.TaskName(t.TaskType), "lane": t.Lane, "count": t.Count, "cost_usd": t.CostUSD, "metric": metric})
	}
	return out
}

type quotaRow struct {
	Estimated  bool       `json:"estimated,omitempty"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
	AccountID  string     `json:"account_id"`
	Label      string     `json:"label"`
	Provider   string     `json:"provider"`
	Window     string     `json:"window"`
	WindowKey  string     `json:"window_key"`
	Pct        float64    `json:"pct"`
	ResetsAt   time.Time  `json:"resets_at"`
	Sub        bool       `json:"sub,omitempty"`
}

func (s *Server) quotas(cfg *config.Config) []quotaRow {
	out := []quotaRow{}
	for _, lane := range []core.LaneID{core.LaneClaudeSub, core.LaneCodexSub, core.LaneAgySub} {
		sl, ok := cfg.Sub[string(lane)]
		if !ok || !sl.Enabled {
			continue
		}
		d, ok := s.d.Router.Driver(lane)
		if !ok {
			continue
		}
		if h := d.Health(context.Background()); h.Status == "missing" || h.SignedOut {
			continue // 没装或没登录：没有额度可看
		} else {
			s.d.Router.Quota().Observe(lane, h.Quota)
		}
		label := map[core.LaneID]string{core.LaneClaudeSub: "Claude 订阅", core.LaneCodexSub: "ChatGPT 订阅", core.LaneAgySub: "Google AI 订阅"}[lane]
		for _, wnd := range s.d.Router.Quota().Windows(lane, sl.Models) {
			row := quotaRow{AccountID: string(lane), Label: label, Provider: providerOfLane(lane), Window: wnd.Label, WindowKey: wnd.Window, Pct: wnd.Pct, ResetsAt: wnd.ResetsAt, Sub: wnd.Window == "week_fable", Estimated: wnd.Estimated}
			if !wnd.ObservedAt.IsZero() {
				t := wnd.ObservedAt
				row.ObservedAt = &t
			}
			// Fable 的用量只有 CLI 自报才准；只凭本应用的记录估不出来，估算行不显示
			if row.Sub && wnd.Estimated {
				continue
			}
			if row.Window == "" {
				row.Window = map[string]string{"5h": "5 小时窗口", "week": "本周", "month": "本月"}[wnd.Window]
			}
			if row.Sub {
				row.Label, row.Window = "其中 Fable", "本周"
			}
			if row.Estimated {
				row.Window += "（估算）"
			}
			out = append(out, row)
		}
	}
	return out
}

func providerOfLane(l core.LaneID) string {
	switch l {
	case core.LaneClaudeSub:
		return "anthropic"
	case core.LaneCodexSub:
		return "openai"
	}
	return "google"
}

func (s *Server) calls(w http.ResponseWriter, r *http.Request) (any, error) {
	q := r.URL.Query()
	f := core.ListFilter{Source: q.Get("source"), Lane: q.Get("lane"), Tier: q.Get("tier"), Query: q.Get("q"), Host: q.Get("host"), Kind: q.Get("kind")}
	f.Limit, _ = strconv.Atoi(q.Get("limit"))
	f.OnlyFailed = q.Get("failed") == "1"
	if b := q.Get("before"); b != "" {
		f.Before, _ = time.Parse(time.RFC3339Nano, b)
	}
	// 带 page 时分页：每页 size 条（默认 50，最多 200），回 {items, total, page, size}；不带照旧回数组
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("size"))
	if page > 0 {
		if size <= 0 || size > 200 {
			size = 50
		}
		f.Limit, f.Offset = size, (page-1)*size
	}
	calls, err := s.d.Router.Ledger().List(f)
	if err != nil {
		return nil, err
	}
	for _, c := range calls {
		s.d.Router.Decorate(c)
	}
	if page <= 0 {
		return calls, nil
	}
	total, err := s.d.Router.Ledger().Count(f)
	return map[string]any{"items": calls, "total": total, "page": page, "size": size}, err
}

func (s *Server) call(w http.ResponseWriter, r *http.Request) (any, error) {
	c, err := s.d.Router.Ledger().Get(r.PathValue("id"))
	if err != nil {
		return nil, notFound("记录不存在")
	}
	return s.d.Router.Decorate(c), nil
}

func (s *Server) escalate(w http.ResponseWriter, r *http.Request) (any, error) {
	tt, n, err := s.d.Router.Escalate(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "task_type": tt, "escalations_for_task": n}, nil
}

func (s *Server) redo(w http.ResponseWriter, r *http.Request) (any, error) {
	orig, err := s.d.Router.Ledger().Get(r.PathValue("id"))
	if err != nil {
		return nil, notFound("记录不存在")
	}
	if orig.PromptExcerpt == "" {
		return nil, errors.New("这条记录没有保存请求内容，没法重做")
	}
	var in struct {
		Tier  string `json:"tier"`
		Model string `json:"model"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	t := &core.Task{Prompt: orig.PromptExcerpt, Kind: orig.Kind, Source: "redo", Host: orig.Host, ProjectDir: projectDirOf(orig.ResultPath)}
	if in.Tier != "" {
		t.Tier = core.Tier(in.Tier)
	} else if orig.Kind == core.KindText {
		// 默认升一档；已经是高档就上顶配
		switch orig.Tier {
		case core.TierLow:
			t.Tier = core.TierMid
		case core.TierMid:
			t.Tier = core.TierHigh
		default:
			t.Tier = core.TierTop
		}
	}
	if in.Model != "" {
		t.Model = in.Model
	}
	if orig.Kind != core.KindText && in.Model == "" && in.Tier == "" {
		t.MediaTier = "best"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	call, res, err := s.d.Router.Run(ctx, t, nil)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"call": s.d.Router.Decorate(call)}
	if res != nil {
		out["text"] = res.Text
		out["path"] = res.Path
		out["job_id"] = res.JobID
	}
	return out, nil
}

func projectDirOf(p string) string {
	if i := strings.LastIndex(p, "/assets/"); i > 0 {
		return p[:i]
	}
	return ""
}

// --- 规则 ---

func laneGroups(prefer []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, p := range prefer {
		g := "api"
		if strings.HasSuffix(p, "_sub") {
			g = "sub"
		}
		if !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	if len(out) == 0 {
		out = append(out, "sub", "api")
	}
	return out
}

func explicitEffect(e config.Explicit) string {
	switch {
	case e.Tier != "":
		return core.TierLabel(core.Tier(e.Tier))
	case e.Effect == "sub_only":
		return "只用订阅"
	case e.Effect == "media_best":
		return "媒体用最好档"
	}
	return e.Effect
}

// rulesBody 按界面的契约输出：task_type 是行的 id，how 是识别说明，lanes 只分订阅 / 按量付费两组。
func (s *Server) rulesBody(cfg *config.Config) map[string]any {
	rows := []map[string]any{}
	stats, _ := s.d.Router.Ledger().EscalationStats(7)
	byType := map[string]core.EscalationStat{}
	for _, st := range stats {
		byType[st.TaskType] = st
	}
	defaults := map[string]bool{}
	for _, t := range config.Default().Routing.TaskTable {
		defaults[t.Type] = true
	}
	for _, t := range cfg.Routing.TaskTable {
		st := byType[t.Type]
		rows = append(rows, map[string]any{"task_type": t.Type, "name": t.Name, "how": t.Hint, "tier": t.Tier, "kind": t.Kind, "media_tier": t.MediaTier,
			"lanes": laneGroups(t.Prefer), "prefer": t.Prefer, "order": t.Order, "count_7d": st.Total, "escalations_7d": st.Escalations, "builtin": defaults[t.Type]})
	}
	explicit := []map[string]any{}
	for _, e := range cfg.Routing.Explicit {
		explicit = append(explicit, map[string]any{"id": e.ID, "triggers": e.Words, "effect": explicitEffect(e), "tier": e.Tier, "raw_effect": e.Effect, "builtin": e.Builtin, "enabled": e.Enabled})
	}
	ctxRules := []map[string]any{}
	for _, c := range cfg.Routing.ContextRules {
		ctxRules = append(ctxRules, map[string]any{"id": c.ID, "title": c.Title, "off_effect": c.Note, "enabled": c.Enabled})
	}
	return map[string]any{"task_table": rows, "explicit": explicit, "context_rules": ctxRules, "sticky": cfg.Routing.Sticky, "tiers": s.tierTables(cfg, ""), "media_order": s.mediaOrderTables(cfg)}
}

// mediaOrderTables 是出图、出视频的先后表：生效的在前（on），其余能选的跟在后面；custom 表示手排过。
func (s *Server) mediaOrderTables(cfg *config.Config) map[string]any {
	reg := s.d.Router.Registry()
	tierOf := map[string]string{}
	for _, tb := range []map[string]config.MediaTarget{cfg.Media.Image, cfg.Media.Video} {
		for k, mt := range tb {
			if key := mt.Provider + "/" + mt.Model; tierOf[key] == "" {
				tierOf[key] = mediaTierLabel(k) // 按供应商加型号认：网关里同名的型号不是这一档配的那个
			}
		}
	}
	accLabel := map[string]string{}
	for _, a := range cfg.API {
		accLabel[a.ID] = a.Label
	}
	out := map[string]any{}
	for _, kind := range []core.Kind{core.KindImage, core.KindVideo} {
		pool := s.d.Router.MediaModels(kind)
		order := cfg.Routing.MediaOrder[string(kind)]
		var on []string
		if len(order) > 0 {
			on = order
		} else {
			for _, c := range s.d.Router.MediaOrderNow(kind) {
				if c.Skipped == "" {
					on = append(on, core.MediaID(c))
				}
			}
		}
		pick := func(c core.Target, enabled bool) map[string]any {
			group := accLabel[c.AccountID]
			if group == "" {
				group = core.ProviderLabel(c.Provider)
			}
			note := c.Skipped
			if note == "" {
				note = tierOf[c.Provider+"/"+c.Model]
			}
			return map[string]any{"id": core.MediaID(c), "lane": core.LaneMedia, "account_id": c.AccountID, "model": c.Model, "name": reg.Display(c.Model), "group": group, "icon": c.Provider, "on": enabled, "note": note}
		}
		picks := []map[string]any{}
		used := map[string]bool{}
		for _, id := range on {
			for _, c := range pool {
				if core.MediaID(c) == id && !used[id] && !c.NoService { // 勾着但没了模型服务的也不列：保存时顺手从顺序里去掉
					used[id] = true
					picks = append(picks, pick(c, true))
				}
			}
		}
		for _, c := range pool {
			if id := core.MediaID(c); !used[id] && !c.NoService {
				used[id] = true
				picks = append(picks, pick(c, false))
			}
		}
		out[string(kind)] = map[string]any{"custom": len(order) > 0, "picks": picks}
	}
	return out
}

// tierTables 是某个宿主（host 为空是全局）低中高三档的候选表：生效顺序在前（on 为真），这一档其余可选的候选跟在后面；
// inherited 是这个宿主不手排时会用的顺序，界面上「跟全局」要还原成它。
func (s *Server) tierTables(cfg *config.Config, host string) map[string]any {
	out := map[string]any{}
	for _, tier := range []core.Tier{core.TierLow, core.TierMid, core.TierHigh} {
		chosen, avail, inherited, custom := s.d.Router.TierTable(host, tier)
		seen := map[string]bool{}
		picks := []map[string]any{}
		add := func(tg core.Target, on bool) {
			id := pickOf(tg).ID()
			if seen[id] {
				return
			}
			seen[id] = true
			group, icon := subGroup[tg.Lane], tg.Provider
			if tg.Lane == core.LaneAPI {
				for _, acc := range cfg.API {
					if acc.ID == tg.AccountID {
						group = acc.Label
						if group == "" {
							group = core.ProviderLabel(acc.Provider)
						}
						if acc.Icon != "" {
							icon = acc.Icon
						}
					}
				}
			}
			picks = append(picks, map[string]any{"id": id, "lane": tg.Lane, "account_id": tg.AccountID, "model": tg.Model,
				"name": s.d.Router.Registry().Display(tg.Model), "group": group, "icon": icon, "on": on, "note": tg.Skipped})
		}
		for _, tg := range chosen {
			if !tg.NoService {
				add(tg, true)
			}
		}
		for _, tg := range avail {
			if !tg.NoService { // 没有模型服务的不列：勾了也用不上
				add(tg, false)
			}
		}
		inh := []string{}
		for _, tg := range inherited {
			inh = append(inh, pickOf(tg).ID())
		}
		out[string(tier)] = map[string]any{"custom": custom, "picks": picks, "inherited": inh}
	}
	return out
}

// parseTierPicks 把界面交来的候选标识表整理成配置条目：只认这一档现有的条目，乱填的丢掉；档位只认低中高。
func (s *Server) parseTierPicks(host string, in map[string][]string) (map[string][]config.TierPick, error) {
	out := map[string][]config.TierPick{}
	for tier, ids := range in {
		if tier != "low" && tier != "mid" && tier != "high" {
			return nil, &httpError{400, "档位只能是 low、mid、high"}
		}
		_, avail, _, _ := s.d.Router.TierTable(host, core.Tier(tier))
		known := map[string]bool{}
		for _, tg := range avail {
			known[pickOf(tg).ID()] = true
		}
		var list []config.TierPick
		for _, id := range ids {
			if p, ok := config.ParseTierPick(id); ok && known[id] {
				list = append(list, p)
			}
		}
		out[tier] = list
	}
	return out, nil
}

func (s *Server) baselineBody(host string) map[string]any {
	b := s.d.Router.AppBaselineOf(host)
	reg := s.d.Router.Registry()
	return map[string]any{"model": b.Model, "display": reg.Display(b.Model), "default_model": b.Default, "default_display": reg.Display(b.Default), "price": b.Price, "source": b.Source}
}

// integrationSetBaseline 改一个应用的原价型号和单价，并按新设置重算它的历史记录。
// model 为空回到默认（接管前的型号）；price 为空或全是 0 用内置价目表。
func (s *Server) integrationSetBaseline(w http.ResponseWriter, r *http.Request) (any, error) {
	id := r.PathValue("id")
	if !s.knownHost(id) {
		return nil, &httpError{404, "没有这个应用"}
	}
	var in struct {
		Model string        `json:"model"`
		Price *config.Price `json:"price"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if p := in.Price; p != nil {
		if p.InputPerM < 0 || p.OutputPerM < 0 || p.CacheReadPerM < 0 {
			return nil, &httpError{400, "单价不能是负数"}
		}
		if p.Currency != "" && p.Currency != "USD" && p.Currency != "CNY" {
			return nil, &httpError{400, "币种只能是 USD 或 CNY"}
		}
		if p.InputPerM == 0 && p.OutputPerM == 0 {
			in.Price = nil
		}
	}
	err := s.d.Router.Config().Update(func(c *config.Config) error {
		if c.Integrations == nil {
			c.Integrations = map[string]config.Integration{}
		}
		it := c.Integrations[id]
		it.BaselineModel, it.BaselinePrice = strings.TrimSpace(in.Model), in.Price
		c.Integrations[id] = it
		return nil
	})
	if err != nil {
		return nil, err
	}
	n, err := s.d.Router.RecomputeBaselines(id)
	if err != nil {
		return nil, err
	}
	out := s.baselineBody(id)
	out["recomputed"] = n
	return out, nil
}

// integrationTiers 是一个宿主自己的档位候选表。
func (s *Server) integrationTiers(w http.ResponseWriter, r *http.Request) (any, error) {
	id := r.PathValue("id")
	if !s.knownHost(id) {
		return nil, &httpError{404, "没有这个应用"}
	}
	return s.tierTables(s.d.Router.Config().Get(), id), nil
}

// integrationSetTiers 保存宿主自己的档位候选表；某档给空表示跟全局。
func (s *Server) integrationSetTiers(w http.ResponseWriter, r *http.Request) (any, error) {
	id := r.PathValue("id")
	if !s.knownHost(id) {
		return nil, &httpError{404, "没有这个应用"}
	}
	var in map[string][]string
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	picks, err := s.parseTierPicks(id, in)
	if err != nil {
		return nil, err
	}
	err = s.d.Router.Config().Update(func(c *config.Config) error {
		if c.Integrations == nil {
			c.Integrations = map[string]config.Integration{}
		}
		it := c.Integrations[id]
		for tier, list := range picks {
			if len(list) == 0 {
				delete(it.Tiers, tier)
				continue
			}
			if it.Tiers == nil {
				it.Tiers = map[string][]config.TierPick{}
			}
			it.Tiers[tier] = list
		}
		if len(it.Tiers) == 0 {
			it.Tiers = nil
		}
		c.Integrations[id] = it
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.tierTables(s.d.Router.Config().Get(), id), nil
}

// knownHost 判断是不是接入页列出的宿主。
func (s *Server) knownHost(id string) bool { return slices.Contains(config.HostIDs, id) }

// subGroup 是候选表里订阅条目的来源名，和「模型与账号」里的叫法一致。
var subGroup = map[core.LaneID]string{core.LaneClaudeSub: "Claude 订阅", core.LaneCodexSub: "ChatGPT 订阅", core.LaneAgySub: "Google AI 订阅"}

func pickOf(tg core.Target) config.TierPick {
	if tg.Lane == core.LaneAPI {
		return config.TierPick{Lane: "api", Account: tg.AccountID, Model: tg.Model}
	}
	return config.TierPick{Lane: string(tg.Lane)}
}

func (s *Server) rules(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.rulesBody(s.d.Router.Config().Get()), nil
}

type uiTaskRow struct {
	TaskType  string   `json:"task_type"`
	Name      string   `json:"name"`
	How       string   `json:"how"`
	Tier      string   `json:"tier"`
	Lanes     []string `json:"lanes"`
	Kind      string   `json:"kind"`       // 空是文字；image video 交给画师、影师。只有用户新建的行能改
	MediaTier string   `json:"media_tier"` // 媒体行用哪一档：cheap default best
}

type uiExplicit struct {
	ID        string   `json:"id"`
	Triggers  []string `json:"triggers"`
	Tier      string   `json:"tier"`
	RawEffect string   `json:"raw_effect"`
	Effect    string   `json:"effect"`
	Enabled   bool     `json:"enabled"`
	Builtin   bool     `json:"builtin"`
}

type uiContext struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

func preferFromLanes(lanes []string, prev []string) []string {
	var out []string
	for _, g := range lanes {
		switch g {
		case "sub":
			added := false
			for _, p := range prev {
				if strings.HasSuffix(p, "_sub") {
					out = append(out, p)
					added = true
				}
			}
			if !added {
				out = append(out, "claude_sub", "codex_sub")
			}
		case "api":
			out = append(out, "api")
		}
	}
	return out
}

func keywordsFromHow(how string) []string {
	var out []string
	for _, k := range strings.FieldsFunc(how, func(r rune) bool { return strings.ContainsRune("、，,;； /「」", r) }) {
		k = strings.TrimSpace(k)
		if len([]rune(k)) >= 2 {
			out = append(out, k)
		}
	}
	return out
}

func (s *Server) putRules(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		TaskTable    []uiTaskRow         `json:"task_table"`
		Explicit     []uiExplicit        `json:"explicit"`
		ContextRules []uiContext         `json:"context_rules"`
		TierPicks    map[string][]string `json:"tier_picks"`  // 档位 → 候选标识，按顺序；空表示回到内置排法
		MediaPicks   map[string][]string `json:"media_picks"` // image / video → 候选标识，按顺序；空表示回到内置排法（GET 回的 media_order 是表，名字错开）
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	// 候选表只能挑这一档现有的条目：乱填的丢掉
	tierPicks, err := s.parseTierPicks("", in.TierPicks)
	if err != nil {
		return nil, err
	}
	mediaOrder := map[string][]string{}
	for kind, ids := range in.MediaPicks {
		if kind != "image" && kind != "video" {
			return nil, &httpError{400, "媒体顺序只认 image 和 video"}
		}
		known := map[string]bool{}
		for _, c := range s.d.Router.MediaModels(core.Kind(kind)) {
			known[core.MediaID(c)] = true
		}
		keep := []string{}
		for _, id := range ids {
			if known[id] && !slices.Contains(keep, id) {
				keep = append(keep, id)
			}
		}
		mediaOrder[kind] = keep
	}
	err = s.d.Router.Config().Update(func(c *config.Config) error {
		for kind, list := range mediaOrder {
			if c.Routing.MediaOrder == nil {
				c.Routing.MediaOrder = map[string][]string{}
			}
			if len(list) == 0 {
				delete(c.Routing.MediaOrder, kind)
			} else {
				c.Routing.MediaOrder[kind] = list
			}
		}
		for tier, list := range tierPicks {
			if c.Routing.Tiers == nil {
				c.Routing.Tiers = map[string][]config.TierPick{}
			}
			if len(list) == 0 {
				delete(c.Routing.Tiers, tier)
			} else {
				c.Routing.Tiers[tier] = list
			}
		}
		if in.TaskTable != nil {
			byType := map[string]config.TaskType{}
			for _, t := range c.Routing.TaskTable {
				byType[t.Type] = t
			}
			builtin := map[string]bool{}
			for _, t := range config.Default().Routing.TaskTable {
				builtin[t.Type] = true
			}
			var table []config.TaskType
			for _, row := range in.TaskTable {
				t, ok := byType[row.TaskType]
				if !ok {
					// 新建的行：用说明里的词做关键词，排在内置行前面，用户自己写的先看
					t = config.TaskType{Type: row.TaskType, Name: row.TaskType, Order: config.CustomOrder, Match: config.Match{Keywords: keywordsFromHow(row.How)}}
					if row.Name != "" {
						t.Name = row.Name
					}
				}
				if !builtin[t.Type] && row.Kind != t.Kind && (row.Kind == "" || row.Kind == "image" || row.Kind == "video") {
					wasMedia := t.Kind != ""
					t.Kind = row.Kind
					switch {
					case t.Kind != "" && len(t.Match.Exclude) == 0:
						t.Match.Exclude = config.MediaTechWords() // 「图片接口报 500」不是要出图
					case t.Kind == "" && wasMedia && slices.Equal(t.Match.Exclude, config.MediaTechWords()):
						t.Match.Exclude = nil
					}
					if t.Kind == "" && t.Tier == "" {
						t.Tier = "low"
					}
				}
				if t.Kind == "image" || t.Kind == "video" {
					switch row.MediaTier {
					case "cheap", "default", "best":
						t.MediaTier = row.MediaTier
					}
					if t.MediaTier == "" {
						t.MediaTier = "default"
					}
				}
				if row.How != "" && row.How != t.Hint {
					t.Hint = row.How
					if !ok || len(t.Match.Keywords) > 0 {
						t.Match.Keywords = keywordsFromHow(row.How)
					}
				}
				if t.Kind == "" && row.Tier != "" {
					t.Tier = row.Tier
				}
				if len(row.Lanes) > 0 {
					t.Prefer = preferFromLanes(row.Lanes, t.Prefer)
				}
				table = append(table, t)
			}
			c.Routing.TaskTable = table
		}
		if in.Explicit != nil {
			have := map[string]bool{}
			var list []config.Explicit
			for _, e := range in.Explicit {
				have[e.ID] = true
				ex := config.Explicit{ID: e.ID, Words: e.Triggers, Tier: e.Tier, Effect: e.RawEffect, Enabled: e.Enabled, Builtin: e.Builtin}
				if ex.ID == "" {
					ex.ID = core.NewID("ex")
				}
				if ex.Tier == "" && ex.Effect == "" {
					switch e.Effect {
					case "只用订阅":
						ex.Effect = "sub_only"
					case "媒体用最好档":
						ex.Effect = "media_best"
					case "低档", "中档", "高档", "顶配":
						ex.Tier = map[string]string{"低档": "low", "中档": "mid", "高档": "high", "顶配": "top"}[e.Effect]
					default:
						ex.Tier = "high"
					}
				}
				list = append(list, ex)
			}
			for _, e := range c.Routing.Explicit {
				if e.Builtin && !have[e.ID] {
					e.Enabled = false
					list = append(list, e)
				}
			}
			c.Routing.Explicit = list
		}
		if in.ContextRules != nil {
			for _, u := range in.ContextRules {
				for i := range c.Routing.ContextRules {
					if c.Routing.ContextRules[i].ID == u.ID {
						c.Routing.ContextRules[i].Enabled = u.Enabled
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.rulesBody(s.d.Router.Config().Get()), nil
}

func (s *Server) tryRule(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Prompt    string `json:"prompt"`
		HasTools  bool   `json:"has_tools"`
		EstTokens int    `json:"est_tokens"`
		SessionID string `json:"session_id"`
		Smart     bool   `json:"smart"` // 按智能分派试：会真的请 Jev 选一次
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	t := &core.Task{Prompt: in.Prompt, EstTokens: in.EstTokens, SessionID: in.SessionID, Source: "try"}
	if in.HasTools {
		t.Tools = json.RawMessage(`[{"type":"function"}]`)
	}
	start := time.Now()
	if in.Smart {
		out := s.decorate(s.d.Router.DecideSmart(r.Context(), t, false))
		out["latency_ms"] = time.Since(start).Milliseconds()
		return out, nil
	}
	d := s.d.Router.Decide(t)
	out := s.decorate(d)
	out["latency_ms"] = time.Since(start).Milliseconds() // 规则只在本机比对，通常是 0
	return out, nil
}

// decorate 给决策加上展示名和档位中文。
func (s *Server) decorate(d *core.Decision) map[string]any {
	reg := s.d.Router.Registry()
	cands := []map[string]any{}
	for _, c := range d.Candidates {
		cands = append(cands, map[string]any{"lane": c.Lane, "provider": c.Provider, "model": c.Model, "display": reg.Display(c.Model), "est_cost_usd": c.EstCostUSD, "skipped": c.Skipped, "account_id": c.AccountID})
	}
	reason := d.Reason
	if d.Unsure && !d.Sticky && !s.d.Router.Config().Get().Settings.RulesOnly {
		reason = append(append([]string(nil), reason...), "规则拿不准，执行时先请低档模型判档")
	}
	return map[string]any{"task_type": d.TaskType, "task_name": s.d.Router.TaskName(d.TaskType), "kind": d.Kind, "tier": d.Tier, "tier_label": core.TierLabel(d.Tier),
		"media_tier": d.MediaTier, "candidates": cands, "reason": reason, "sticky": d.Sticky, "unsure": d.Unsure, "smart": d.Smart}
}

// mediaModels 列出能点名的出图（kind=image）或出视频（kind=video）型号：MCP 工具说明、快速面板都用它。
func (s *Server) mediaModels(w http.ResponseWriter, r *http.Request) (any, error) {
	kind := core.Kind(r.URL.Query().Get("kind"))
	if kind != core.KindImage && kind != core.KindVideo {
		return nil, &httpError{400, "kind 只能是 image 或 video"}
	}
	reg := s.d.Router.Registry()
	out := []map[string]any{}
	for _, c := range s.d.Router.MediaModels(kind) {
		out = append(out, map[string]any{"model": c.Model, "display": reg.Display(c.Model), "provider": c.Provider, "account_id": c.AccountID, "est_cost_usd": c.EstCostUSD, "skipped": c.Skipped})
	}
	return out, nil
}

// SmartKeyID 是 Jev 密钥在钥匙串里的名字；账号 id 都是「供应商-随机串」，撞不上。
const SmartKeyID = "smart:jev"

func (s *Server) smartBody() map[string]any {
	key, _ := s.d.Secrets.Get(SmartKeyID)
	tail := ""
	if n := len(key); n >= 4 {
		tail = key[n-4:]
	}
	sm := s.d.Router.Config().Get().Routing.Smart
	out := map[string]any{"enabled": sm.Enabled, "has_key": key != "", "key_tail": tail,
		"judge": sm.JudgeOrDefault(), "judge_account": sm.JudgeAccount, "judge_model": sm.JudgeModel, "judge_models": s.judgeModels()}
	if s.d.Laya != nil {
		out["laya"] = s.d.Laya.Status()
	}
	return out
}

// judgeModels 是能当判官的型号：启用的按量付费账号分进档位的文本型号，低档在前（快、便宜）。
func (s *Server) judgeModels() []map[string]any {
	reg := s.d.Router.Registry()
	out := []map[string]any{}
	seen := map[string]bool{}
	for _, tier := range []string{"low", "mid", "high"} {
		for _, acc := range s.d.Router.Config().Get().API {
			if !acc.Enabled {
				continue
			}
			label := acc.Label
			if label == "" {
				label = core.ProviderLabel(acc.Provider)
			}
			for _, m := range acc.Models[tier] {
				if k := acc.ID + "/" + m; !seen[k] {
					seen[k] = true
					out = append(out, map[string]any{"account": acc.ID, "account_label": label, "provider": acc.Provider, "model": m, "display": reg.Display(m), "tier": tier})
				}
			}
		}
	}
	return out
}

// smartLaya 管本机 Laya：install（装运行环境并下载模型，后台跑）、start、stop、remove。
func (s *Server) smartLaya(w http.ResponseWriter, r *http.Request) (any, error) {
	if s.d.Laya == nil {
		return nil, &httpError{400, "这台电脑上不支持本机 Laya"}
	}
	setAuto := func(on bool) {
		_ = s.d.Router.Config().Update(func(c *config.Config) error { c.Routing.Smart.LayaAutostart = on; return nil })
	}
	var err error
	switch r.PathValue("action") {
	case "install":
		err = s.d.Laya.Install(nil)
	case "start":
		if err = s.d.Laya.Start(); err == nil {
			setAuto(true)
		}
	case "stop":
		err = s.d.Laya.Stop()
		setAuto(false)
	case "remove":
		if err = s.d.Laya.Remove(); err == nil {
			setAuto(false)
		}
	default:
		return nil, notFound("不认识的操作")
	}
	if err != nil {
		return nil, &httpError{400, err.Error()}
	}
	return s.smartBody(), nil
}

func (s *Server) smartGet(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.smartBody(), nil
}

// smartPut 改智能分派：enabled 开关；key 非空就换密钥；clear_key 删掉密钥。
func (s *Server) smartPut(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Enabled      *bool   `json:"enabled"`
		Key          string  `json:"key"`
		ClearKey     bool    `json:"clear_key"`
		Judge        *string `json:"judge"`
		JudgeAccount *string `json:"judge_account"`
		JudgeModel   *string `json:"judge_model"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if k := strings.TrimSpace(in.Key); k != "" {
		if err := s.d.Secrets.Set(SmartKeyID, k); err != nil {
			return nil, err
		}
	}
	if in.ClearKey {
		_ = s.d.Secrets.Delete(SmartKeyID)
	}
	if in.Judge != nil && *in.Judge != "laya" && *in.Judge != "jev" && *in.Judge != "model" {
		return nil, &httpError{400, "判官只能是 laya、jev 或 model"}
	}
	if err := s.d.Router.Config().Update(func(c *config.Config) error {
		sm := &c.Routing.Smart
		if in.Enabled != nil {
			sm.Enabled = *in.Enabled
		}
		if in.Judge != nil {
			sm.Judge = *in.Judge
		}
		if in.JudgeAccount != nil {
			sm.JudgeAccount = *in.JudgeAccount
		}
		if in.JudgeModel != nil {
			sm.JudgeModel = *in.JudgeModel
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return s.smartBody(), nil
}

// smartTest 用一句合成的话请 Jev 选一次，不看开关：填完密钥点「测试」用。
func (s *Server) smartTest(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Prompt   string `json:"prompt"`
		HasTools bool   `json:"has_tools"`
	}
	_ = decode(r, &in)
	if strings.TrimSpace(in.Prompt) == "" {
		in.Prompt = "帮我写一个函数，把 CSV 里的日期列统一成 YYYY-MM-DD"
	}
	t := &core.Task{Prompt: in.Prompt, Source: "try"}
	if in.HasTools {
		t.Tools = json.RawMessage(`[{"type":"function"}]`)
	}
	start := time.Now()
	d := s.d.Router.DecideSmart(r.Context(), t, true)
	out := s.decorate(d)
	out["latency_ms"] = time.Since(start).Milliseconds()
	return out, nil
}

func (s *Server) suggestions(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.d.Router.Suggestions()
}

func (s *Server) applySuggestion(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Action string `json:"action"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if err := s.d.Router.ApplySuggestion(r.PathValue("task"), in.Action); err != nil {
		return nil, err
	}
	return s.rulesBody(s.d.Router.Config().Get()), nil
}

func (s *Server) exportRules(w http.ResponseWriter, r *http.Request) (any, error) {
	b, err := yaml.Marshal(s.d.Router.Config().Get().Routing)
	if err != nil {
		return nil, err
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="switchboard-rules.yaml"`)
	_, _ = w.Write(b)
	return nil, nil
}

func (s *Server) importRules(w http.ResponseWriter, r *http.Request) (any, error) {
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var routing config.Routing
	if err := yaml.Unmarshal(b, &routing); err != nil {
		return nil, errors.New("规则文件无法解析：" + err.Error())
	}
	err = s.d.Router.Config().Update(func(c *config.Config) error { c.Routing = routing; return nil })
	if err != nil {
		return nil, err
	}
	return s.rulesBody(s.d.Router.Config().Get()), nil
}

// --- 账号 ---

type accountView struct {
	ID              string              `json:"id"`
	Kind            string              `json:"kind"`
	Provider        string              `json:"provider"`
	Icon            string              `json:"icon,omitempty"` // 自定义服务手动选的图标；空表示界面按型号名认
	Label           string              `json:"label"`
	BaseURL         string              `json:"base_url,omitempty"` // 按量付费：接口地址，改地址时回填
	Status          string              `json:"status"`
	Detail          string              `json:"detail"`
	Plan            string              `json:"plan,omitempty"`
	Local           bool                `json:"local,omitempty"` // 本机登录的客户端（WinWork）：自动出现、不计费，界面上和订阅排在一起
	PlanOptions     []string            `json:"plan_options,omitempty"`
	PlanManual      bool                `json:"plan_manual,omitempty"`
	MonthCostUSD    float64             `json:"month_cost_usd"`
	MonthCostNative float64             `json:"month_cost_native"`
	Currency        string              `json:"currency"`
	Quota           []core.QuotaWindow  `json:"quota,omitempty"`
	Tiers           map[string]any      `json:"tiers,omitempty"`
	TierOptions     map[string][]string `json:"tier_options,omitempty"`
	Labels          map[string]string   `json:"labels,omitempty"`
	Media           []mediaItem         `json:"media,omitempty"`
	FreeModels      []string            `json:"free_models,omitempty"`   // 平台当前免费的型号；有它才显示计费开关
	MediaChoice     map[string]string   `json:"media_choice,omitempty"`  // image / video → 这个账号选用的型号，空串表示不用
	MediaOptions    map[string][]string `json:"media_options,omitempty"` // image / video → 可选型号
	AllowPaid       *bool               `json:"allow_paid,omitempty"`
	Models          []string            `json:"models"`
	NeedsProxy      bool                `json:"needs_proxy,omitempty"`
	Enabled         bool                `json:"enabled"`
	InstallHint     string              `json:"install_hint,omitempty"`
}

// planOptions 是命令行读不到套餐时给用户自己选的名单。
var planOptions = map[core.LaneID][]string{
	core.LaneAgySub:    {"免费版", "Google AI Pro", "Google AI Ultra"},
	core.LaneClaudeSub: {"免费版", "Pro", "Max 5x", "Max 20x", "Team", "企业版"},
	core.LaneCodexSub:  {"免费版", "Plus", "Pro", "Team", "企业版"},
}

func (s *Server) accountViews() []accountView {
	cfg := s.d.Router.Config().Get()
	costs, _ := s.d.Router.Ledger().MonthCostByAccount(time.Local)
	out := []accountView{}
	subs := []struct {
		lane   core.LaneID
		label  string
		detail string
		prov   string
		hint   string
	}{
		{core.LaneClaudeSub, "Claude", "用本机 claude 命令的登录，消耗你的订阅额度", "anthropic", ""},
		{core.LaneCodexSub, "ChatGPT", "用本机 codex 命令的登录；含订阅出图", "openai", ""},
		{core.LaneAgySub, "Google AI", "用本机 agy 命令的登录，消耗 Google AI 订阅额度", "google", "安装 Antigravity 并在终端运行一次 agy 登录"},
	}
	for _, sb := range subs {
		sl := cfg.Sub[string(sb.lane)]
		v := accountView{ID: string(sb.lane), Kind: "subscription", Provider: sb.prov, Label: sb.label, Detail: sb.detail, Enabled: sl.Enabled, Status: "missing", Currency: "USD", InstallHint: sb.hint}
		tiers := map[string]any{}
		v.Labels = map[string]string{}
		for k, m := range sl.Models {
			tiers[k] = m
			v.Models = append(v.Models, m)
			v.Labels[m] = s.d.Router.Registry().Display(m)
		}
		v.Tiers = tiers
		if d, ok := s.d.Router.Driver(sb.lane); ok {
			h := d.Health(context.Background())
			if h.SignedOut {
				continue // 没登录就没有额度可用，不占一行
			}
			s.d.Router.Quota().Observe(sb.lane, h.Quota)
			v.Status = h.Status
			if h.Plan != "" {
				s.d.Router.Quota().ObservePlan(sb.lane, h.Plan)
			}
			v.Plan = h.Plan
			if v.Plan == "" {
				v.Plan = s.d.Router.Quota().Plan(sb.lane)
			}
			if v.Plan == "" {
				v.Plan = sl.Plan
				v.PlanManual = true
				v.PlanOptions = planOptions[sb.lane]
			}
			if h.Detail != "" {
				v.Detail = h.Detail
			}
			if h.Status == "ok" {
				v.Quota = s.d.Router.Quota().Windows(sb.lane, sl.Models)
				for _, q := range v.Quota {
					if q.Window == "5h" && q.Pct >= cfg.Settings.SubscriptionYieldPct {
						v.Status = "warn"
						v.Detail = fmt.Sprintf("额度 %.0f%%", q.Pct)
					}
				}
				if cooling, until, _ := s.d.Router.Quota().Cooling(sb.lane); cooling {
					v.Status = "cooldown"
					v.Detail = "额度暂时用完：约 " + until.Sub(time.Now()).Round(time.Minute).String() + " 后恢复"
				}
			}
		}
		if !sl.Enabled {
			v.Status = "disabled"
		}
		if sb.lane == core.LaneCodexSub && sl.ImageGen {
			v.Media = []mediaItem{{Kind: "image", Model: "codex-imagegen", Display: "ChatGPT 订阅出图"}}
		}
		out = append(out, v)
	}
	for _, a := range cfg.API {
		v := accountView{ID: a.ID, Kind: "api", Provider: a.Provider, Icon: a.Icon, Label: labelOr(a.Label, core.ProviderLabel(a.Provider)), BaseURL: a.BaseURL, Enabled: a.Enabled, Currency: a.Currency, NeedsProxy: a.NeedsProxy, Status: "ok", Detail: "", Plan: "按量付费"}
		if a.Provider == "winwork" {
			// 走的是它网关的接口，所以在按量付费这条通路上；但它是本机登录的客户端，不计费，界面上和订阅排在一起
			v.Local, v.Plan = true, "不计费"
		}
		tiers := map[string]any{}
		v.TierOptions = map[string][]string{}
		v.Labels = map[string]string{}
		seen := map[string]bool{}
		var displays []string
		for _, k := range []string{"low", "mid", "high"} {
			ms := a.Models[k]
			first := ""
			if len(ms) > 0 {
				first = ms[0]
			}
			tiers[k] = first
			v.TierOptions[k] = append([]string{}, ms...)
			for _, m := range ms {
				if !seen[m] {
					seen[m] = true
					v.Models = append(v.Models, m)
					v.Labels[m] = s.d.Router.Registry().Display(m)
					displays = append(displays, v.Labels[m])
				}
			}
		}
		// 预设里有但还没分到任何档位的型号也列出来，方便在档位里选；自定义服务用它自己报的型号
		for _, d := range a.Discovered {
			if core.GuessMediaKind(d.ID) != "" {
				continue // 出图、出视频型号放在媒体那里选，不进文本档位
			}
			if !seen[d.ID] {
				seen[d.ID] = true
				v.Models = append(v.Models, d.ID)
				v.Labels[d.ID] = labelOr(d.Name, d.ID)
			}
		}
		if p, ok := presetModels(s.d.Accounts, a.Provider); ok {
			for _, m := range p {
				if !seen[m] {
					seen[m] = true
					v.Models = append(v.Models, m)
					v.Labels[m] = s.d.Router.Registry().Display(m)
				}
			}
		}
		v.Tiers = tiers
		v.Media = s.mediaItemsFor(a)
		if len(v.Media) > 0 {
			v.MediaChoice = map[string]string{}
			v.MediaOptions = map[string][]string{}
			for _, m := range v.Media {
				v.MediaOptions[m.Kind] = append(v.MediaOptions[m.Kind], m.Model)
				v.Labels[m.Model] = m.Display
			}
			for _, k := range []string{"image", "video"} {
				if len(v.MediaOptions[k]) > 0 {
					v.MediaChoice[k] = a.MediaModel(k, s.d.Router.DefaultMediaModel(a.Provider, k))
				}
			}
		}
		// 本机登录的客户端整个不计费，不再逐个标「免费」，也没有「只用免费型号」可选
		if fm := s.d.Router.Registry().FreeModels(a.Provider); len(fm) > 0 && !v.Local {
			v.FreeModels = fm
			ap := a.AllowPaid
			v.AllowPaid = &ap
		}
		if c, ok := costs[a.Provider]; ok {
			v.MonthCostUSD, v.MonthCostNative = c[0], c[1]
		}
		if !a.Enabled {
			v.Status = "disabled"
		} else if cooling, _, why := s.d.Router.Quota().Cooling(core.LaneID("acct:" + a.ID)); cooling {
			v.Status = "error"
			v.Detail = why
		} else if _, err := s.d.Secrets.Get(a.ID); err != nil {
			var notHere *core.ErrKeyNotHere
			if errors.As(err, &notHere) {
				v.Status, v.Detail = "warn", notHere.Reason
			} else {
				v.Status, v.Detail = "error", "没有找到密钥，重新填写"
			}
		}
		if v.Detail == "" && v.Local {
			v.Detail = "用本机 WinWork 的登录"
		}
		if v.Detail == "" && len(displays) == 0 && len(a.Discovered) > 0 {
			v.Detail = fmt.Sprintf("服务有 %d 个型号，还没分到档位", len(a.Discovered))
		}
		if v.Detail == "" {
			v.Detail = strings.Join(displays, "、")
			if a.NeedsProxy {
				v.Detail += "；需要代理"
			}
		}
		out = append(out, v)
	}
	return out
}

// presetModels 返回预设里该供应商的全部型号 id。
func presetModels(ops AccountOps, provider string) ([]string, bool) {
	_, _, tiers, _, ok := ops.Preset(provider)
	if !ok {
		return nil, false
	}
	var out []string
	for _, k := range []string{"low", "mid", "high"} {
		out = append(out, tiers[k]...)
	}
	return out, true
}

type mediaItem struct {
	Free    bool   `json:"free,omitempty"`
	Kind    string `json:"kind"` // image video
	Model   string `json:"model"`
	Display string `json:"display"`
}

func (s *Server) mediaItems(provider string) []mediaItem {
	var out []mediaItem
	for _, m := range s.d.Router.Registry().MediaByProvider(provider) {
		kind := "image"
		if m.IsVideo() {
			kind = "video"
		}
		out = append(out, mediaItem{Kind: kind, Model: m.ID, Display: m.Display, Free: m.Free})
	}
	return out
}

// mediaItemsFor 是账号能选的出图、出视频型号：预设厂商从型号表里找，自定义服务从它报的型号里按名字认。
func (s *Server) mediaItemsFor(a config.APIAccount) []mediaItem {
	if a.Provider != "custom" {
		return s.mediaItems(a.Provider)
	}
	var out []mediaItem
	for _, d := range a.Discovered {
		if k := core.GuessMediaKind(d.ID); k != "" {
			out = append(out, mediaItem{Kind: k, Model: d.ID, Display: labelOr(d.Name, d.ID)})
		}
	}
	return out
}

func labelOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (s *Server) accounts(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.accountViews(), nil
}

func (s *Server) providers(w http.ResponseWriter, r *http.Request) (any, error) {
	out := []map[string]any{}
	for _, p := range s.d.Accounts.Providers() {
		_, cur, _, proxy, _ := s.d.Accounts.Preset(p)
		out = append(out, map[string]any{"id": p, "label": core.ProviderLabel(p), "currency": cur, "needs_proxy": proxy})
	}
	return out, nil
}

func (s *Server) addAccount(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Provider string `json:"provider"`
		APIKey   string `json:"api_key"`
		BaseURL  string `json:"base_url"`
		Label    string `json:"label"`
		Icon     string `json:"icon"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	in.APIKey = strings.TrimSpace(in.APIKey)
	if in.Provider == "" || in.APIKey == "" {
		return nil, errors.New("供应商和密钥都要填")
	}
	baseURL, currency, tiers, needsProxy, ok := s.d.Accounts.Preset(in.Provider)
	if !ok {
		return nil, errors.New("不认识的供应商：" + in.Provider)
	}
	if in.BaseURL != "" {
		baseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var discovered []config.DiscoveredModel
	if in.Provider == "custom" {
		if baseURL == "" {
			return nil, errors.New("自定义服务要填接口地址")
		}
		ms, err := s.d.Accounts.ListModels(ctx, baseURL, in.APIKey)
		if err != nil {
			return nil, err
		}
		discovered = ms
		if strings.TrimSpace(in.Label) == "" {
			in.Label = hostOf(baseURL)
		}
	} else if _, err := s.d.Accounts.Verify(ctx, in.Provider, baseURL, in.APIKey); err != nil {
		return nil, err
	}
	id := in.Provider + "-" + core.NewID("")[1:7]
	if err := s.d.Secrets.Set(id, in.APIKey); err != nil {
		return nil, err
	}
	acc := config.APIAccount{ID: id, Provider: in.Provider, Label: strings.TrimSpace(in.Label), BaseURL: baseURL, Currency: currency, Enabled: true, Models: tiers, NeedsProxy: needsProxy, Discovered: discovered, Icon: in.Icon}
	if err := s.d.Router.Config().Update(func(c *config.Config) error { c.API = append(c.API, acc); return nil }); err != nil {
		return nil, err
	}
	s.d.Accounts.Reload(s.d.Router.Config().Get())
	for _, v := range s.accountViews() {
		if v.ID == id {
			return v, nil
		}
	}
	return nil, errors.New("添加后没有找到账号")
}

func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request) (any, error) {
	id := r.PathValue("id")
	var in struct {
		Enabled   *bool             `json:"enabled"`
		Tiers     map[string]any    `json:"tiers"`      // 订阅：tier → 型号；按量付费：tier → 首选型号（字符串）或完整列表
		Plan      *string           `json:"plan"`       // 订阅：手填套餐名
		AllowPaid *bool             `json:"allow_paid"` // 按量付费：是否允许用收费型号
		Media     map[string]string `json:"media"`      // 按量付费：image / video → 型号，空串表示不用
		Icon      *string           `json:"icon"`       // 自定义服务：换图标，空串表示按型号名自动认
		APIKey    *string           `json:"api_key"`    // 按量付费：换密钥；先验证再存
		BaseURL   *string           `json:"base_url"`   // 按量付费：换接口地址（自定义服务、OpenRouter）
		Label     *string           `json:"label"`      // 按量付费：改名字
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	// 换密钥或地址：先用新值去验证，过了才落盘；自定义服务顺便把型号列表换新
	var discovered []config.DiscoveredModel
	var newBase string
	if in.APIKey != nil || in.BaseURL != nil {
		var acc *config.APIAccount
		for _, a := range s.d.Router.Config().Get().API {
			if a.ID == id {
				a := a
				acc = &a
			}
		}
		if acc == nil {
			return nil, errors.New("不是按量付费账号")
		}
		key := ""
		if in.APIKey != nil && strings.TrimSpace(*in.APIKey) != "" {
			key = strings.TrimSpace(*in.APIKey)
		} else if k, err := s.d.Secrets.Get(id); err == nil {
			key = k
		} else {
			return nil, errors.New("没有找到密钥，重新填写")
		}
		newBase = acc.BaseURL
		if in.BaseURL != nil {
			newBase = strings.TrimRight(strings.TrimSpace(*in.BaseURL), "/")
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if acc.Provider == "custom" {
			if newBase == "" {
				return nil, errors.New("自定义服务要填接口地址")
			}
			ms, err := s.d.Accounts.ListModels(ctx, newBase, key)
			if err != nil {
				return nil, err
			}
			discovered = ms
		} else if _, err := s.d.Accounts.Verify(ctx, acc.Provider, newBase, key); err != nil {
			return nil, err
		}
		if in.APIKey != nil && strings.TrimSpace(*in.APIKey) != "" {
			if err := s.d.Secrets.Set(id, key); err != nil {
				return nil, err
			}
		}
	}
	err := s.d.Router.Config().Update(func(c *config.Config) error {
		if sl, ok := c.Sub[id]; ok {
			if in.Enabled != nil {
				sl.Enabled = *in.Enabled
			}
			if in.Plan != nil {
				sl.Plan = *in.Plan
			}
			if in.Tiers != nil {
				models := map[string]string{}
				for k, v := range in.Tiers {
					if str, ok := v.(string); ok && str != "" {
						models[k] = str
					}
				}
				if len(models) > 0 {
					sl.Models = models
				}
			}
			c.Sub[id] = sl
			return nil
		}
		for i := range c.API {
			if c.API[i].ID == id {
				if in.Enabled != nil {
					c.API[i].Enabled = *in.Enabled
				}
				if in.AllowPaid != nil {
					c.API[i].AllowPaid = *in.AllowPaid
					config.ApplyFreeOnly(&c.API[i], s.d.Router.Registry().IsFree)
				}
				if in.Icon != nil {
					c.API[i].Icon = *in.Icon
				}
				if in.Label != nil {
					c.API[i].Label = strings.TrimSpace(*in.Label)
				}
				if in.BaseURL != nil {
					c.API[i].BaseURL = newBase
				}
				if discovered != nil {
					c.API[i].Discovered = discovered
				}
				if in.Media != nil {
					if c.API[i].Media == nil {
						c.API[i].Media = map[string]string{}
					}
					for k, m := range in.Media {
						c.API[i].Media[k] = m
					}
				}
				if in.Tiers != nil {
					if c.API[i].Models == nil {
						c.API[i].Models = map[string][]string{}
					}
					for k, v := range in.Tiers {
						switch val := v.(type) {
						case string:
							cur := c.API[i].Models[k]
							if val == "" {
								c.API[i].Models[k] = nil
								continue
							}
							next := []string{val}
							for _, m := range cur {
								if m != val {
									next = append(next, m)
								}
							}
							c.API[i].Models[k] = next
						case []any:
							var list []string
							for _, x := range val {
								if m, ok := x.(string); ok {
									list = append(list, m)
								}
							}
							c.API[i].Models[k] = list
						}
					}
				}
				return nil
			}
		}
		return notFound("账号不存在")
	})
	if err != nil {
		return nil, err
	}
	s.d.Accounts.Reload(s.d.Router.Config().Get())
	for _, v := range s.accountViews() {
		if v.ID == id {
			return v, nil
		}
	}
	return nil, notFound("账号不存在")
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) (any, error) {
	id := r.PathValue("id")
	err := s.d.Router.Config().Update(func(c *config.Config) error {
		for i := range c.API {
			if c.API[i].ID == id {
				c.API = append(c.API[:i], c.API[i+1:]...)
				return nil
			}
		}
		return notFound("账号不存在")
	})
	if err != nil {
		return nil, err
	}
	_ = s.d.Secrets.Delete(id)
	s.d.Accounts.Reload(s.d.Router.Config().Get())
	return map[string]bool{"ok": true}, nil
}

func (s *Server) verifyAccount(w http.ResponseWriter, r *http.Request) (any, error) {
	id := r.PathValue("id")
	cfg := s.d.Router.Config().Get()
	for _, a := range cfg.API {
		if a.ID != id {
			continue
		}
		key, err := s.d.Secrets.Get(id)
		var notHere *core.ErrKeyNotHere
		if errors.As(err, &notHere) {
			return nil, notHere
		}
		if err != nil {
			return nil, errors.New("没有找到密钥，重新填写")
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if a.Provider == "custom" {
			// 自定义服务重新验证顺便把型号列表换新：服务那边加了型号，这里点一下就有
			ms, err := s.d.Accounts.ListModels(ctx, a.BaseURL, key)
			if err != nil {
				s.d.Router.Quota().MarkCooldown(core.LaneID("acct:"+id), time.Hour, err.Error())
				return nil, err
			}
			_ = s.d.Router.Config().Update(func(c *config.Config) error {
				for i := range c.API {
					if c.API[i].ID == id {
						c.API[i].Discovered = ms
					}
				}
				return nil
			})
			s.d.Accounts.Reload(s.d.Router.Config().Get())
		} else if _, err := s.d.Accounts.Verify(ctx, a.Provider, a.BaseURL, key); err != nil {
			s.d.Router.Quota().MarkCooldown(core.LaneID("acct:"+id), time.Hour, err.Error())
			return nil, err
		}
		s.d.Router.Quota().ClearCooldown(core.LaneID("acct:" + id))
		for _, v := range s.accountViews() {
			if v.ID == id {
				return v, nil
			}
		}
	}
	return nil, notFound("账号不存在")
}

// --- 媒体与任务 ---

func (s *Server) media(w http.ResponseWriter, r *http.Request) (any, error) {
	kind := core.Kind(r.URL.Query().Get("kind"))
	if kind == "all" {
		kind = ""
	}
	jobs := []*core.Job{}
	for _, j := range s.d.Jobs.List(kind) {
		if j.Status != "done" {
			jobs = append(jobs, j)
		}
	}
	items := []*core.Call{}
	for _, k := range []string{"image", "video", "animation"} {
		if kind != "" && string(kind) != k {
			continue
		}
		cs, _ := s.d.Router.Ledger().List(core.ListFilter{Kind: k, Limit: 200, OnlyOK: true})
		for _, c := range cs {
			if c.ResultPath != "" {
				items = append(items, c)
			}
		}
	}
	return map[string]any{"jobs": jobs, "items": items}, nil
}

func (s *Server) job(w http.ResponseWriter, r *http.Request) (any, error) {
	j, ok := s.d.Jobs.Get(r.PathValue("id"))
	if !ok {
		return nil, notFound("任务不存在")
	}
	return j, nil
}

func (s *Server) jobDelete(w http.ResponseWriter, r *http.Request) (any, error) {
	s.d.Jobs.Delete(r.PathValue("id"))
	return map[string]any{"ok": true}, nil
}

func (s *Server) callDelete(w http.ResponseWriter, r *http.Request) (any, error) {
	if err := s.d.Router.Ledger().DeleteCall(r.PathValue("id")); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

func (s *Server) jobCancel(w http.ResponseWriter, r *http.Request) (any, error) {
	if s.d.Media == nil {
		return nil, errors.New("媒体渠道未启用")
	}
	return s.d.Media.Cancel(r.Context(), r.PathValue("id"))
}

func (s *Server) jobRetry(w http.ResponseWriter, r *http.Request) (any, error) {
	if s.d.Media == nil {
		return nil, errors.New("媒体渠道未启用")
	}
	var in struct {
		Model string `json:"model"`
	}
	_ = decode(r, &in)
	return s.d.Media.Retry(context.Background(), r.PathValue("id"), in.Model)
}

func (s *Server) jobRevise(w http.ResponseWriter, r *http.Request) (any, error) {
	if s.d.Media == nil {
		return nil, errors.New("媒体渠道未启用")
	}
	var in struct {
		Instruction string `json:"instruction"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Instruction) == "" {
		return nil, errors.New("写一句要怎么改")
	}
	return s.d.Media.Revise(context.Background(), r.PathValue("id"), in.Instruction)
}

// --- 接入 ---

// hostTagged 是请求里带宿主标记的接入：MCP 靠 IMLS_HOST，兼容入口靠请求头或密钥后缀。
// pi、OpenClaw、DeepSeek Harness 早先接入时写的密钥没有后缀，重新接入一次才计得上。
var hostTagged = map[string]bool{"claude_code": true, "claude_desktop": true, "codex": true, "gemini_cli": true, "workbuddy": true, "hermes": true, "doubao_work": true,
	"pi": true, "openclaw": true, "deepseek_harness": true}

func (s *Server) integrations(w http.ResponseWriter, r *http.Request) (any, error) {
	list := s.d.Integr.List()
	act, err := s.d.Router.Ledger().HostActivity(time.Now().AddDate(0, 0, -7))
	if err != nil {
		return list, nil
	}
	// 豆包工作的连接器在它自己的账号里，本机看不到配置；它派过活就算接上了
	ever, _ := s.d.Router.Ledger().HostActivity(time.Time{})
	for _, it := range list {
		id, _ := it["id"].(string)
		it["baseline"] = s.baselineBody(id)
		if !hostTagged[id] {
			continue
		}
		a := act[id]
		it["calls_7d"] = a.Calls
		if !a.Last.IsZero() {
			it["last_call"] = a.Last
		}
		if id == "doubao_work" && ever[id].Calls > 0 {
			it["status"] = "connected"
		}
	}
	return list, nil
}

func (s *Server) integrationConnect(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Options map[string]bool `json:"options"`
	}
	_ = decode(r, &in)
	return s.d.Integr.Connect(r.PathValue("id"), in.Options)
}

func (s *Server) integrationDisconnect(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.d.Integr.Disconnect(r.PathValue("id"))
}

func (s *Server) integrationUpdate(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Options map[string]bool `json:"options"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	return s.d.Integr.Update(r.PathValue("id"), in.Options)
}

func (s *Server) integrationModels(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.d.Integr.HostModels(r.PathValue("id"))
}

// integrationSetModels 保存型号表。写进宿主失败时表已经存下，连同错误一起告诉界面
func (s *Server) integrationSetModels(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Models []string `json:"models"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	return s.d.Integr.SetHostModels(r.PathValue("id"), in.Models)
}

// --- 设置 ---

func (s *Server) settings(w http.ResponseWriter, r *http.Request) (any, error) {
	return settingsBody(s.d.Router.Config().Get().Settings), nil
}

// settingsBody 在设置之外附带只读的数据目录，界面要显示它。
func settingsBody(st config.Settings) map[string]any {
	b, _ := json.Marshal(st)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	out["data_dir"] = config.DataDir()
	return out
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) (any, error) {
	var in map[string]json.RawMessage
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	err := s.d.Router.Config().Update(func(c *config.Config) error {
		b, _ := json.Marshal(c.Settings)
		var merged map[string]json.RawMessage
		_ = json.Unmarshal(b, &merged)
		for k, v := range in {
			merged[k] = v
		}
		mb, _ := json.Marshal(merged)
		var st config.Settings
		if err := json.Unmarshal(mb, &st); err != nil {
			return err
		}
		if !themes.Valid(st.Theme) {
			return errors.New("主题不存在")
		}
		if st.TrayIcon != "" && !slices.Contains(config.TrayIcons, st.TrayIcon) {
			return errors.New("没有这个菜单栏图标样式")
		}
		c.Settings = st
		return nil
	})
	if err != nil {
		return nil, err
	}
	if _, ok := in["default_mode"]; ok {
		s.d.Router.SetMode(core.Mode(s.d.Router.Config().Get().Settings.DefaultMode))
	}
	s.d.Bus.Publish("status", s.statusBody())
	return settingsBody(s.d.Router.Config().Get().Settings), nil
}

// --- 评估 ---

func (s *Server) evaluations(w http.ResponseWriter, r *http.Request) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*core.Evaluation{}
	for _, e := range s.evals {
		out = append(out, e)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].StartedAt.After(out[i].StartedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func (s *Server) startEvaluation(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Sample int `json:"sample"`
	}
	_ = decode(r, &in)
	ev := &core.Evaluation{ID: core.NewID("eval"), Status: "running", StartedAt: time.Now(), Sample: in.Sample, ByTask: []core.EvalTaskRow{}}
	s.mu.Lock()
	s.evals[ev.ID] = ev
	s.mu.Unlock()
	save := func(e *core.Evaluation) {
		_ = s.d.Router.Ledger().SaveEvaluation(e.ID, e.StartedAt, e)
		s.d.Bus.Publish("evaluation", e)
	}
	save(ev)
	go s.d.Router.RunEvaluation(context.Background(), ev, save)
	return ev, nil
}

func (s *Server) evaluation(w http.ResponseWriter, r *http.Request) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.evals[r.PathValue("id")]
	if !ok {
		return nil, notFound("评估不存在")
	}
	return e, nil
}

func (s *Server) evaluationAccept(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		TaskType string `json:"task_type"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	if err := s.d.Router.ApplySuggestion(in.TaskType, "accept"); err != nil {
		return nil, err
	}
	return s.rulesBody(s.d.Router.Config().Get()), nil
}

// --- 快速面板 ---

func (s *Server) quickPreview(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Prompt    string `json:"prompt"`
		ImagePath string `json:"image_path"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	t := &core.Task{Prompt: in.Prompt, ImagePath: in.ImagePath, Source: "quick"}
	d := s.d.Router.Decide(t)
	out := s.decorate(d)
	// 备选：媒体给其它档位；文本给其它档位的首选
	alts := []map[string]any{}
	cfg := s.d.Router.Config().Get()
	reg := s.d.Router.Registry()
	switch d.Kind {
	case core.KindImage, core.KindVideo:
		table := cfg.Media.Image
		units := 1.0
		if d.Kind == core.KindVideo {
			table = cfg.Media.Video
			units = 5
		}
		// 能用的型号都列出来（各档配的、账号报上来的同类型号），按 Tab 挨个换；第一个是这次会用的，不重复列
		first := ""
		for _, c := range d.Candidates {
			if c.Skipped == "" {
				first = c.Model
				break
			}
		}
		tierOf := map[string]string{}
		for _, k := range []string{"cheap", "default", "best", "free"} {
			if mt, ok := table[k]; ok && tierOf[mt.Model] == "" {
				tierOf[mt.Model] = k
			}
		}
		for _, c := range s.d.Router.MediaModels(d.Kind) {
			if c.Skipped != "" || c.Model == first {
				continue
			}
			usd, native, cur := reg.MediaCost(c.Model, units)
			alt := map[string]any{"label": "其他型号", "model": c.Model, "display": reg.Display(c.Model), "provider": c.Provider, "lane": core.LaneMedia, "est_cost_usd": usd, "est_cost_native": native, "currency": cur}
			if k := tierOf[c.Model]; k != "" {
				alt["media_tier"], alt["label"] = k, mediaTierLabel(k)
			}
			alts = append(alts, alt)
		}
	case core.KindText:
		for _, tier := range []core.Tier{core.TierLow, core.TierMid, core.TierHigh, core.TierTop} {
			if tier == d.Tier {
				continue
			}
			t2 := &core.Task{Prompt: in.Prompt, Tier: tier, Source: "quick"}
			d2 := s.d.Router.Decide(t2)
			for _, c := range d2.Candidates {
				if c.Skipped == "" {
					alts = append(alts, map[string]any{"tier": tier, "label": core.TierLabel(tier), "model": c.Model, "display": reg.Display(c.Model), "provider": c.Provider, "lane": c.Lane, "est_cost_usd": c.EstCostUSD})
					break
				}
			}
		}
	}
	out["alternatives"] = alts
	return out, nil
}

func mediaTierLabel(k string) string {
	switch k {
	case "cheap":
		return "便宜档"
	case "default":
		return "默认档"
	case "best":
		return "最好档"
	case "free":
		return "订阅出图"
	}
	return k
}

func (s *Server) quickRun(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-IMLS-Client") == "" {
		writeErr(w, 403, "缺少客户端标识")
		return
	}
	var in struct {
		Prompt     string `json:"prompt"`
		ImagePath  string `json:"image_path"`
		Tier       string `json:"tier"`
		MediaTier  string `json:"media_tier"`
		Model      string `json:"model"`
		Lane       string `json:"lane"`
		Kind       string `json:"kind"`
		Seconds    int    `json:"seconds"`
		ProjectDir string `json:"project_dir"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "不支持流式输出")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(200)
	send := func(typ string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, b)
		fl.Flush()
	}
	t := &core.Task{Prompt: in.Prompt, ImagePath: in.ImagePath, Tier: core.Tier(in.Tier), MediaTier: in.MediaTier, Model: in.Model, Lane: core.LaneID(in.Lane), Kind: core.Kind(in.Kind), Seconds: in.Seconds, Source: "quick", ProjectDir: in.ProjectDir, Stream: true}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	call, res, err := s.d.Router.Run(ctx, t, func(e core.Event) {
		switch e.Type {
		case "text_delta":
			send("text_delta", map[string]string{"text": e.Text})
		case "media":
			send("media", map[string]string{"path": e.Path})
		case "job":
			send("job", map[string]any{"job": e.Job})
		case "status":
			send("status", map[string]string{"text": e.Text})
		}
	})
	if err != nil {
		send("error", map[string]string{"message": err.Error()})
		return
	}
	out := map[string]any{"call": s.d.Router.Decorate(call)}
	if res != nil {
		out["text"], out["path"], out["job_id"] = res.Text, res.Path, res.JobID
		out["display"] = s.d.Router.Registry().Display(call.Model)
	}
	send("done", out)
}

// --- 事件流 ---

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "不支持流式输出")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(200)
	ch, cancel := s.d.Bus.Subscribe()
	defer cancel()
	b, _ := json.Marshal(s.statusBody())
	fmt.Fprintf(w, "event: status\ndata: %s\n\n", b)
	fl.Flush()
	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case env := <-ch:
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", env.Type, env.Data)
			fl.Flush()
		}
	}
}

// --- 引导 ---

func (s *Server) onboarding(w http.ResponseWriter, r *http.Request) (any, error) {
	cfg := s.d.Router.Config().Get()
	cli := false
	for _, lane := range []core.LaneID{core.LaneClaudeSub, core.LaneCodexSub} {
		if d, ok := s.d.Router.Driver(lane); ok && d.Health(context.Background()).Status == "ok" {
			cli = true
		}
	}
	calls, _ := s.d.Router.Ledger().List(core.ListFilter{Limit: 1})
	connected := false
	if s.d.Integr != nil {
		for _, it := range s.d.Integr.List() {
			if it["id"] == "claude_code" && it["status"] == "connected" {
				connected = true
			}
		}
	}
	return map[string]any{"done": cfg.Settings.OnboardingDone, "steps": map[string]bool{"cli_detected": cli, "api_added": len(cfg.API) > 0, "claude_connected": connected, "tried": len(calls) > 0}}, nil
}

func (s *Server) onboardingDone(w http.ResponseWriter, r *http.Request) (any, error) {
	err := s.d.Router.Config().Update(func(c *config.Config) error { c.Settings.OnboardingDone = true; return nil })
	return map[string]bool{"ok": err == nil}, err
}

// --- MCP 转发 ---

// ToolRequest 是 imls mcp 转发过来的工具调用。
type ToolRequest struct {
	Name       string          `json:"name"`
	Args       json.RawMessage `json:"args"`
	ProjectDir string          `json:"project_dir"`
	Host       string          `json:"host"`
	SessionID  string          `json:"session_id"`
}

func (s *Server) internalTool(w http.ResponseWriter, r *http.Request) (any, error) {
	var req ToolRequest
	if err := decode(r, &req); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	text, err := RunTool(ctx, s.d.Router, s.d.Jobs, req)
	if err != nil {
		return map[string]any{"text": err.Error(), "is_error": true}, nil
	}
	return map[string]any{"text": text}, nil
}

// hostOf 取地址里的主机名，自定义服务没填名称时用它当名称。
func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}
