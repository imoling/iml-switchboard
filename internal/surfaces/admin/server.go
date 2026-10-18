// Package admin 是给界面和命令行用的本机接口：/api/*。只监听 127.0.0.1，要求请求头 X-IMLS-Client。
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/imoling/iml-switchboard/internal/config"
	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/themes"
)

// Version 由 main 注入。
var Version = "dev"

// AccountOps 由按量付费渠道提供：验证密钥、列模型、默认模型表。
type AccountOps interface {
	Verify(ctx context.Context, provider, baseURL, apiKey string) (models []string, err error)
	Preset(provider string) (baseURL, currency string, tiers map[string][]string, needsProxy bool, ok bool)
	Providers() []string
	Reload(cfg *config.Config)
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
	h("GET /api/rules/suggestions", s.suggestions)
	h("POST /api/rules/suggestions/{task}", s.applySuggestion)
	h("GET /api/rules/export", s.exportRules)
	h("POST /api/rules/import", s.importRules)
	h("GET /api/accounts", s.accounts)
	h("GET /api/providers", s.providers)
	h("POST /api/accounts", s.addAccount)
	h("POST /api/accounts/detect", s.accounts)
	h("PUT /api/accounts/{id}", s.updateAccount)
	h("DELETE /api/accounts/{id}", s.deleteAccount)
	h("POST /api/accounts/{id}/verify", s.verifyAccount)
	h("GET /api/media", s.media)
	h("GET /api/jobs/{id}", s.job)
	h("POST /api/jobs/{id}/cancel", s.jobCancel)
	h("POST /api/jobs/{id}/retry", s.jobRetry)
	h("POST /api/jobs/{id}/revise", s.jobRevise)
	h("GET /api/integrations", s.integrations)
	h("POST /api/integrations/{id}/connect", s.integrationConnect)
	h("POST /api/integrations/{id}/disconnect", s.integrationDisconnect)
	h("PUT /api/integrations/{id}", s.integrationUpdate)
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
	}
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
		delta = float64(ov.Calls-ov.PrevCalls) / float64(ov.PrevCalls) * 100
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
		metric := fmt.Sprintf("%d 次", t.Count)
		if t.CostUSD > 0 {
			metric = fmt.Sprintf("$%.2f", t.CostUSD)
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
		if h := d.Health(context.Background()); h.Status == "missing" {
			continue
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
	if b := q.Get("before"); b != "" {
		f.Before, _ = time.Parse(time.RFC3339Nano, b)
	}
	return s.d.Router.Ledger().List(f)
}

func (s *Server) call(w http.ResponseWriter, r *http.Request) (any, error) {
	c, err := s.d.Router.Ledger().Get(r.PathValue("id"))
	if err != nil {
		return nil, notFound("记录不存在")
	}
	return c, nil
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
	out := map[string]any{"call": call}
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
	return map[string]any{"task_table": rows, "explicit": explicit, "context_rules": ctxRules, "sticky": cfg.Routing.Sticky}
}

func (s *Server) rules(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.rulesBody(s.d.Router.Config().Get()), nil
}

type uiTaskRow struct {
	TaskType string   `json:"task_type"`
	Name     string   `json:"name"`
	How      string   `json:"how"`
	Tier     string   `json:"tier"`
	Lanes    []string `json:"lanes"`
	Kind     string   `json:"kind"`
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
		TaskTable    []uiTaskRow  `json:"task_table"`
		Explicit     []uiExplicit `json:"explicit"`
		ContextRules []uiContext  `json:"context_rules"`
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	err := s.d.Router.Config().Update(func(c *config.Config) error {
		if in.TaskTable != nil {
			byType := map[string]config.TaskType{}
			for _, t := range c.Routing.TaskTable {
				byType[t.Type] = t
			}
			var table []config.TaskType
			for i, row := range in.TaskTable {
				t, ok := byType[row.TaskType]
				if !ok {
					// 新建的行：用说明里的词做关键词
					t = config.TaskType{Type: row.TaskType, Name: row.TaskType, Order: 100 + i, Match: config.Match{Keywords: keywordsFromHow(row.How)}}
					if row.Name != "" {
						t.Name = row.Name
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
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	t := &core.Task{Prompt: in.Prompt, EstTokens: in.EstTokens, SessionID: in.SessionID, Source: "try"}
	if in.HasTools {
		t.Tools = json.RawMessage(`[{"type":"function"}]`)
	}
	d := s.d.Router.Decide(t)
	return s.decorate(d), nil
}

// decorate 给决策加上展示名和档位中文。
func (s *Server) decorate(d *core.Decision) map[string]any {
	reg := s.d.Router.Registry()
	cands := []map[string]any{}
	for _, c := range d.Candidates {
		cands = append(cands, map[string]any{"lane": c.Lane, "provider": c.Provider, "model": c.Model, "display": reg.Display(c.Model), "est_cost_usd": c.EstCostUSD, "skipped": c.Skipped, "account_id": c.AccountID})
	}
	return map[string]any{"task_type": d.TaskType, "task_name": s.d.Router.TaskName(d.TaskType), "kind": d.Kind, "tier": d.Tier, "tier_label": core.TierLabel(d.Tier),
		"media_tier": d.MediaTier, "candidates": cands, "reason": d.Reason, "sticky": d.Sticky}
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
	Label           string              `json:"label"`
	Status          string              `json:"status"`
	Detail          string              `json:"detail"`
	MonthCostUSD    float64             `json:"month_cost_usd"`
	MonthCostNative float64             `json:"month_cost_native"`
	Currency        string              `json:"currency"`
	Quota           []core.QuotaWindow  `json:"quota,omitempty"`
	Tiers           map[string]any      `json:"tiers,omitempty"`
	TierOptions     map[string][]string `json:"tier_options,omitempty"`
	Labels          map[string]string   `json:"labels,omitempty"`
	Models          []string            `json:"models"`
	NeedsProxy      bool                `json:"needs_proxy,omitempty"`
	Enabled         bool                `json:"enabled"`
	InstallHint     string              `json:"install_hint,omitempty"`
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
			s.d.Router.Quota().Observe(sb.lane, h.Quota)
			v.Status = h.Status
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
		out = append(out, v)
	}
	for _, a := range cfg.API {
		v := accountView{ID: a.ID, Kind: "api", Provider: a.Provider, Label: labelOr(a.Label, core.ProviderLabel(a.Provider)), Enabled: a.Enabled, Currency: a.Currency, NeedsProxy: a.NeedsProxy, Status: "ok", Detail: ""}
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
		// 预设里有但还没分到任何档位的型号也列出来，方便在档位里选
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
		if c, ok := costs[a.Provider]; ok {
			v.MonthCostUSD, v.MonthCostNative = c[0], c[1]
		}
		if !a.Enabled {
			v.Status = "disabled"
		} else if cooling, _, why := s.d.Router.Quota().Cooling(core.LaneID("acct:" + a.ID)); cooling {
			v.Status = "error"
			v.Detail = why
		} else if _, err := s.d.Secrets.Get(a.ID); err != nil {
			v.Status = "error"
			v.Detail = "没有找到密钥，重新填写"
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
		baseURL = in.BaseURL
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if _, err := s.d.Accounts.Verify(ctx, in.Provider, baseURL, in.APIKey); err != nil {
		return nil, err
	}
	id := in.Provider + "-" + core.NewID("")[1:7]
	if err := s.d.Secrets.Set(id, in.APIKey); err != nil {
		return nil, err
	}
	acc := config.APIAccount{ID: id, Provider: in.Provider, Label: in.Label, BaseURL: baseURL, Currency: currency, Enabled: true, Models: tiers, NeedsProxy: needsProxy}
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
		Enabled *bool          `json:"enabled"`
		Tiers   map[string]any `json:"tiers"` // 订阅：tier → 型号；按量付费：tier → 首选型号（字符串）或完整列表
	}
	if err := decode(r, &in); err != nil {
		return nil, err
	}
	err := s.d.Router.Config().Update(func(c *config.Config) error {
		if sl, ok := c.Sub[id]; ok {
			if in.Enabled != nil {
				sl.Enabled = *in.Enabled
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
		if err != nil {
			return nil, errors.New("没有找到密钥，重新填写")
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if _, err := s.d.Accounts.Verify(ctx, a.Provider, a.BaseURL, key); err != nil {
			s.d.Router.Quota().MarkCooldown(core.LaneID("acct:"+id), time.Hour, err.Error())
			return nil, err
		}
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

func (s *Server) integrations(w http.ResponseWriter, r *http.Request) (any, error) {
	return s.d.Integr.List(), nil
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

// --- 设置 ---

func (s *Server) settings(w http.ResponseWriter, r *http.Request) (any, error) {
	st := s.d.Router.Config().Get().Settings
	return st, nil
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
	return s.d.Router.Config().Get().Settings, nil
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
	ev := &core.Evaluation{ID: core.NewID("eval"), Status: "running", StartedAt: time.Now(), Sample: in.Sample}
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
		for _, k := range []string{"cheap", "default", "best", "free"} {
			if k == d.MediaTier {
				continue
			}
			if mt, ok := table[k]; ok {
				usd, native, cur := reg.MediaCost(mt.Model, units)
				alts = append(alts, map[string]any{"media_tier": k, "label": mediaTierLabel(k), "model": mt.Model, "display": reg.Display(mt.Model), "provider": mt.Provider, "est_cost_usd": usd, "est_cost_native": native, "currency": cur})
			}
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
	out := map[string]any{"call": call}
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
