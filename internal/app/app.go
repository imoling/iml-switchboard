// Package app 把配置、账本、调度器、渠道驱动和三个接入面装配起来。
package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/imoling/iml-switchboard/internal/config"
	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/integrations"
	"github.com/imoling/iml-switchboard/internal/lanes/api"
	"github.com/imoling/iml-switchboard/internal/lanes/jev"
	"github.com/imoling/iml-switchboard/internal/lanes/laya"
	"github.com/imoling/iml-switchboard/internal/lanes/media"
	"github.com/imoling/iml-switchboard/internal/lanes/sub"
	"github.com/imoling/iml-switchboard/internal/surfaces/admin"
	"github.com/imoling/iml-switchboard/internal/surfaces/facade"
	mcpsurface "github.com/imoling/iml-switchboard/internal/surfaces/mcp"
	"github.com/imoling/iml-switchboard/internal/update"
)

// Addr 是后台监听地址，只在本机。环境变量 IMLS_ADDR 可覆盖（测试用）。
var Addr = func() string {
	if a := os.Getenv("IMLS_ADDR"); a != "" {
		return a
	}
	return "127.0.0.1:18081"
}()

// BaseURL 是后台地址。
var BaseURL = "http://" + Addr

// App 是装配好的应用。
type App struct {
	Cfg     *config.Store
	Ledger  *core.Ledger
	Router  *core.Router
	Jobs    *core.MemJobStore
	Bus     *core.Bus
	Secrets *core.KeychainSecrets
	API     *api.APIDriver
	Media   *media.MediaDriver
	Claude  *sub.ClaudeDriver
	Codex   *sub.CodexDriver
	Agy     *sub.AntigravityDriver
	Admin   *admin.Server
	Laya    *laya.Manager
	Facade  *facade.Server
	Integr  *integrations.Manager
	Version string
	exe     string
	updates *update.Checker
	quitMu  sync.Mutex
	quit    context.CancelFunc
}

// New 装配。
func New(version string) (*App, error) {
	cfg, err := config.Open()
	if err != nil {
		return nil, err
	}
	dir := config.DataDir()
	for _, sub := range []string{"media", "animations", "logs"} {
		_ = os.MkdirAll(filepath.Join(dir, sub), 0o755)
	}
	ledger, err := core.OpenLedger(filepath.Join(dir, "ledger.db"))
	if err != nil {
		return nil, err
	}
	a := &App{Cfg: cfg, Ledger: ledger, Version: version}
	a.updates = &update.Checker{Current: version}
	// 老记录的摘录里带着宿主注入的内容，升级后清一次
	var done bool
	if !ledger.KVGet("migrate:clean_excerpts_v4", &done) || !done {
		if _, err := ledger.CleanExcerpts(core.StripInjected); err == nil {
			_ = ledger.KVSet("migrate:clean_excerpts_v4", true)
		}
	}
	a.exe, _ = os.Executable()
	a.Secrets = core.NewSecrets(dir)
	a.Bus = core.NewBus()
	reg := core.NewRegistry(cfg.Get().Settings.USDPerCNY)
	quota := core.NewQuotaManager(ledger, func() float64 { return cfg.Get().Settings.SubscriptionYieldPct })
	a.Router = core.NewRouter(cfg, ledger, reg, quota)
	a.Jobs = core.NewJobStore(ledger, func(j *core.Job) {
		a.Bus.PublishJob(j)
		if j.Status == "done" || j.Status == "failed" || j.Status == "timeout" {
			_ = ledger.UpdateResult(j.CallID, j.ResultPath, j.CostUSD, j.Status == "done", j.Error)
			if cfg.Get().Settings.Notifications.Media {
				if j.Status == "done" {
					a.Bus.Publish("notice", core.Notice{Title: kindTitle(j.Kind) + "已生成", Body: j.PromptExcerpt + " · " + reg.Display(j.Model), Level: "info"})
				} else {
					a.Bus.Publish("notice", core.Notice{Title: kindTitle(j.Kind) + "生成失败", Body: j.Error, Level: "error"})
				}
			}
		}
	})
	client := a.httpClient()
	api.USDPerCNY = 1 / cfg.Get().Settings.USDPerCNY

	a.Claude = sub.NewClaudeDriver()
	a.Codex = sub.NewCodexDriver()
	// 出图走订阅渠道的低档：用户自己的 Codex 配置里写的模型，免费套餐可能用不了
	a.Codex.ImageModel = func() string { return cfg.Get().Sub["codex_sub"].Models["low"] }
	ensureWinWorkAccount(cfg)
	a.API = api.NewAPIDriver(a.apiAccounts(cfg.Get()), client)
	a.Media = media.New(a.mediaOptions(client))
	a.Router.Register(a.Claude)
	a.Router.Register(a.Codex)
	a.Agy = sub.NewAntigravityDriver()
	a.Router.Register(a.Agy)
	a.Router.Register(a.API)
	a.Router.Register(a.Media)
	a.Router.OnCall(func(c *core.Call) { a.Bus.PublishCall(a.Router.Decorate(c)); a.checkBudget(c) })
	a.Router.OnAttempt(func(t *core.Task, c core.Target) {
		a.Bus.PublishCallStart(core.CallStart{ID: t.ID, Host: t.Host, Lane: c.Lane, Provider: c.Provider})
	})
	a.Router.OnNotice(func(title, body, level string) {
		if cfg.Get().Settings.Notifications.Quota {
			a.Bus.Publish("notice", core.Notice{Title: title, Body: body, Level: level})
		}
	})

	// 有免费型号的供应商（Agnes）默认只用免费型号；把早先配置里的收费型号摘掉
	_ = cfg.Update(func(c *config.Config) error {
		for i := range c.API {
			if free := a.Router.Registry().FreeModels(c.API[i].Provider); len(free) > 0 {
				config.ApplyFreeOnly(&c.API[i], a.Router.Registry().IsFree)
				if !c.API[i].AllowPaid && len(c.API[i].Models["mid"]) == 0 {
					best, bestCtx := "", 0
					for _, m := range free {
						if info, ok := a.Router.Registry().Lookup(m); ok && info.MediaKind == "" && info.Context > bestCtx {
							best, bestCtx = m, info.Context
						}
					}
					if best != "" {
						c.API[i].Models["mid"] = []string{best}
					}
				}
			}
		}
		return nil
	})
	a.Integr = integrations.New(cfg, a.exe, BaseURL, func() string { return cfg.Get().Settings.FacadeKey })
	a.Router.SetBaselineDefault(a.Integr.OriginalModel)
	a.Router.SetAppBilling(a.workbuddyBilling)
	a.Laya = &laya.Manager{Dir: filepath.Join(dir, "laya"), Env: envWithoutProxy}
	a.Router.SetSmartJudge(a.jevJudge(client))
	if s := cfg.Get().Routing.Smart; s.JudgeOrDefault() == "laya" && s.LayaAutostart {
		go func() { _ = a.Laya.Start() }() // 上次开着：接着跑（还在跑就直接认领）
	}
	// 点名型号的老记录档位记的是规则判的，按型号所在的档改正一次
	if !ledger.KVGet("migrate:pinned_tier_v1", &done) || !done {
		if _, err := ledger.RewriteTiers(func(c *core.Call) core.Tier {
			return a.Router.TierOfModel(core.Target{Lane: c.Lane, Model: c.Model, AccountID: accountOfCall(c)})
		}); err == nil {
			_ = ledger.KVSet("migrate:pinned_tier_v1", true)
		}
	}
	if !ledger.KVGet("migrate:router_smart_v1", &done) || !done {
		a.Integr.AddRouterModel("router-smart", "router-auto") // 「智能分派」补进各应用的模型列表
		_ = ledger.KVSet("migrate:router_smart_v1", true)
	}
	a.workbuddyBilling("workbuddy", "") // 先把 WorkBuddy 的积分价登记进价目表，应用页一打开就有
	// 省下的钱从「全用 Opus」改成按应用原价算：历史记录按现在的设置重算一次
	// 内置价目表改过价也重算一次
	if key := "migrate:app_baseline:" + core.PriceTableVersion; !ledger.KVGet(key, &done) || !done {
		if _, err := a.Router.RecomputeBaselines(""); err == nil {
			_ = ledger.KVSet(key, true)
		}
	}
	a.Integr.ModelName = a.Router.Registry().Display
	a.Integr.Lookup = a.Router.Registry().Lookup
	a.Integr.ModelContext = func(id string) int {
		info, _ := a.Router.Registry().Lookup(id)
		return info.Context
	}
	a.Admin = admin.New(admin.Deps{Router: a.Router, Jobs: a.Jobs, Bus: a.Bus, Accounts: &accountOps{a: a}, Media: &mediaOps{a: a}, Integr: a.Integr, Secrets: secretsWithWinWork{a.Secrets}, StartedAt: time.Now(),
		NodeAvailable: func() bool { _, err := exec.LookPath("node"); return err == nil },
		Build:         BuildID(), Quit: a.Quit, Laya: a.Laya,
		Update: func(ctx context.Context, force bool) (any, error) { return a.updates.Check(ctx, force) }})
	a.Facade = facade.New(a.Router, func() string { return cfg.Get().Settings.FacadeKey })
	a.Facade.MediaURL = mediaURL(filepath.Join(dir, "media"))
	return a, nil
}

// mediaOptions 组装媒体驱动的依赖；没有项目目录时产物落到数据目录的 media/。
func (a *App) mediaOptions(client *http.Client) media.Options {
	dir := config.DataDir()
	return media.Options{Jobs: a.Jobs, Accounts: a.API, Codex: a.Codex, Claude: a.Claude, Client: client, AnimRoot: filepath.Join(dir, "animations"),
		OutDir: func(t *core.Task) (string, error) {
			if t.OutDir == "" && t.ProjectDir == "" {
				return filepath.Join(dir, "media"), nil
			}
			return media.DefaultOutDir(t)
		}}
}

func kindTitle(k core.Kind) string {
	switch k {
	case core.KindVideo:
		return "视频"
	case core.KindAnimation:
		return "动画"
	}
	return "图片"
}

// httpClient 只给需要代理的海外服务走设置里的代理。
func (a *App) httpClient() *http.Client {
	proxied := []string{"openrouter.ai", "api.openai.com", "api.anthropic.com", "googleapis.com", "fal.run", "replicate.com", "github.com", "typesafe.ai"}
	tr := &http.Transport{
		Proxy: func(r *http.Request) (*url.URL, error) {
			p := strings.TrimSpace(a.Cfg.Get().Settings.Proxy)
			for _, h := range proxied {
				if strings.HasSuffix(r.URL.Host, h) {
					if p == "" {
						// 设置里没填代理就用系统的（HTTPS_PROXY 这些）：这几家国内直连不通，Jev 直连回 451
						return http.ProxyFromEnvironment(r)
					}
					if !strings.Contains(p, "://") {
						p = "http://" + p
					}
					return url.Parse(p)
				}
			}
			return nil, nil
		},
		DialContext:         (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
		TLSHandshakeTimeout: 15 * time.Second,
		MaxIdleConns:        20,
	}
	return &http.Client{Transport: tr, Timeout: 0}
}

// apiAccounts 把配置里的账号变成驱动需要的结构；密钥按需从钥匙串取。
func (a *App) apiAccounts(c *config.Config) []api.Account {
	var out []api.Account
	for _, acc := range c.API {
		if !acc.Enabled {
			continue
		}
		// 自定义服务的型号注册表里没有：用它自己报的名字登记，价格不知道就记 0。
		// 默认当能看图：现在的兼容服务大多能看，真不能看时上游报错，请求会换下一个候选
		for _, d := range acc.Discovered {
			if !a.Router.Registry().Has(d.ID) {
				info := core.ModelInfo{ID: d.ID, Provider: acc.Provider, Display: labelOrID(d.Name, d.ID), Vision: true, Tools: true}
				info.MediaKind = core.GuessMediaKind(d.ID)
				a.Router.Registry().Upsert(info)
			}
		}
		p, ok := api.PresetByID(acc.Provider)
		if !ok {
			p = api.Preset{ID: acc.Provider, BaseURL: acc.BaseURL, Currency: acc.Currency, Models: map[string]api.ModelPrice{}}
		}
		id := acc.ID
		ac := p.Account(id, func() (string, error) { return a.Secrets.Get(id) })
		if acc.Provider == "winwork" {
			// 网关地址和令牌都在 WinWork 自己的配置里，现读；不进钥匙串
			if base, _, _ := api.WinWorkGateway(); base != "" {
				ac.BaseURL = base
			}
			ac.APIKeyGetter = func() (string, error) { _, tok, err := api.WinWorkGateway(); return tok, err }
			ac.BeforeRequest = api.EnsureWinWork
		}
		if acc.BaseURL != "" {
			ac.BaseURL = acc.BaseURL
		}
		if acc.Currency != "" {
			ac.Currency = acc.Currency
		}
		out = append(out, ac)
	}
	return out
}

func (a *App) checkBudget(c *core.Call) {
	cfg := a.Cfg.Get()
	if !cfg.Settings.Notifications.Budget || cfg.Settings.MonthlyAPILimitUSD <= 0 || c.CostUSD == 0 {
		return
	}
	ov, err := a.Ledger.Aggregate(1, time.Local)
	if err != nil {
		return
	}
	limit := cfg.Settings.MonthlyAPILimitUSD
	if ov.MonthAPICostUSD >= limit && ov.MonthAPICostUSD-c.CostUSD < limit {
		a.Bus.Publish("notice", core.Notice{Title: "按量付费已到本月上限", Body: "只用订阅；媒体生成已暂停，到设置里可以调高", Level: "warn"})
	}
}

// Handler 组装 HTTP 路由。
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	a.Admin.Routes(mux)
	a.Facade.Routes(mux)
	mcpH := mcpsurface.HTTPHandler(BaseURL, a.Version, func() string { return a.Cfg.Get().Settings.FacadeKey })
	mux.Handle("GET /media/{name}", mediaHandler(filepath.Join(config.DataDir(), "media")))
	mux.Handle("POST /mcp", mcpH)
	mux.Handle("GET /mcp", mcpH)
	mux.Handle("DELETE /mcp", mcpH)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Switchboard %s\n", a.Version)
	})
	return cors(mux)
}

// cors 只放行 Tauri 窗口和本机开发服务器的跨源请求。
func cors(next http.Handler) http.Handler {
	allowed := map[string]bool{"tauri://localhost": true, "http://tauri.localhost": true, "https://tauri.localhost": true, "http://localhost:1420": true, "http://127.0.0.1:1420": true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" && allowed[o] {
			w.Header().Set("Access-Control-Allow-Origin", o)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-IMLS-Client, Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			if r.Method == http.MethodOptions {
				w.WriteHeader(204)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Quit 让 Serve 正常收尾退出；不在常驻时什么也不做。
func (a *App) Quit() {
	a.quitMu.Lock()
	defer a.quitMu.Unlock()
	if a.quit != nil {
		a.quit()
	}
}

// Serve 启动后台直到 ctx 结束。
func (a *App) Serve(ctx context.Context) error {
	ln, err := net.Listen("tcp", Addr)
	if err != nil {
		return errors.New("后台已经在运行，或端口 18081 被占用")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	a.quitMu.Lock()
	a.quit = cancel
	a.quitMu.Unlock()
	srv := &http.Server{Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	// 常驻时才预热：命令行一次性调用跑完就退出，预热了也白费
	a.Claude.Prewarm, a.Agy.Prewarm = true, true
	a.startMonthlyEvaluation(ctx)
	a.startSelfCheck(ctx)
	a.startHousekeeping(ctx)
	a.startUpdateCheck(ctx)
	go a.Integr.SyncCodexCatalog()
	err = srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// fullTextKeep 是为评估保存完整原话的天数，和评估的抽样范围一致；过期只清原话，摘录和费用照留。
const fullTextKeep = 30 * 24 * time.Hour

// startUpdateCheck 启动两分钟后、之后每天一次查新版本；同一个版本只通知一次。
func (a *App) startUpdateCheck(ctx context.Context) {
	a.updates.Client = a.httpClient()
	go func() {
		wait := 2 * time.Minute
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			wait = 24 * time.Hour
			info, err := a.updates.Check(ctx, false)
			if err != nil || !info.HasUpdate {
				continue
			}
			var told string
			if a.Ledger.KVGet("update:notified", &told) && told == info.Latest {
				continue
			}
			_ = a.Ledger.KVSet("update:notified", info.Latest)
			a.Bus.Publish("notice", core.Notice{Title: "有新版本 " + info.Latest, Body: "到设置里下载安装包", Level: "info"})
		}
	}()
}

// startHousekeeping 启动时和之后每天清一次过期的完整原话。
func (a *App) startHousekeeping(ctx context.Context) {
	go func() {
		for {
			_, _ = a.Ledger.PruneFull(time.Now().Add(-fullTextKeep))
			select {
			case <-ctx.Done():
				return
			case <-time.After(24 * time.Hour):
			}
		}
	}()
}

// startSelfCheck 启动一分钟后、之后每天一次，检查官方 CLI 的参数还对不对；同一个问题只通知一次。
func (a *App) startSelfCheck(ctx context.Context) {
	go func() {
		wait := time.Minute
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			wait = 24 * time.Hour
			for _, c := range []struct {
				lane  core.LaneID
				name  string
				check func(context.Context) (string, []string)
			}{{core.LaneClaudeSub, "Claude", a.Claude.SelfCheck}, {core.LaneCodexSub, "ChatGPT", a.Codex.SelfCheck}} {
				ver, problems := c.check(ctx)
				key := "selfcheck:" + string(c.lane)
				sig := ver + "|" + strings.Join(problems, "|")
				var last string
				if a.Ledger.KVGet(key, &last) && last == sig {
					continue
				}
				_ = a.Ledger.KVSet(key, sig)
				if len(problems) > 0 {
					a.Bus.Publish("notice", core.Notice{Title: c.name + " 订阅可能用不了", Body: problems[0] + "；先更新 Switchboard", Level: "warn"})
				}
			}
		}
	}()
}

// startMonthlyEvaluation 每月 1 日凌晨自动评估（设置里开了才跑）。
func (a *App) startMonthlyEvaluation(ctx context.Context) {
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				if !a.Cfg.Get().Settings.AutoEvaluateMonthly || now.Day() != 1 || now.Hour() != 3 {
					continue
				}
				var last string
				if a.Ledger.KVGet("last_auto_eval", &last) && last == now.Format("2006-01") {
					continue
				}
				_ = a.Ledger.KVSet("last_auto_eval", now.Format("2006-01"))
				ev := &core.Evaluation{ID: core.NewID("eval"), Status: "running", StartedAt: now, Sample: 100}
				a.Router.RunEvaluation(ctx, ev, func(e *core.Evaluation) { _ = a.Ledger.SaveEvaluation(e.ID, e.StartedAt, e) })
			}
		}
	}()
}

// Close 释放资源。
func (a *App) Close() {
	a.Claude.Close()
	a.Agy.Close()
	_ = a.Ledger.Close()
}

// ---- admin 依赖的两个适配器 ----

type accountOps struct{ a *App }

func (o *accountOps) Verify(ctx context.Context, provider, baseURL, apiKey string) ([]string, error) {
	p, ok := api.PresetByID(provider)
	if !ok {
		return nil, errors.New("不认识的供应商：" + provider)
	}
	if provider == "winwork" {
		base, _, err := api.WinWorkGateway()
		if err != nil {
			return nil, err
		}
		p.BaseURL = base
	}
	if baseURL != "" {
		p.BaseURL = baseURL
	}
	// 没有探测型号的预设（只做媒体的火山方舟）跳过联网验证，首次出图时再报错
	if p.ProbeModel != "" {
		if err := p.Verify(ctx, o.a.httpClient(), apiKey); err != nil {
			return nil, err
		}
	}
	var ms []string
	for m := range p.Models {
		ms = append(ms, m)
	}
	return ms, nil
}

// 各供应商在三个档位的默认候选。
var defaultTiers = map[string]map[string][]string{
	"zhipu":      {"low": {"glm-5.3-flash"}, "mid": {"glm-5.3"}, "high": {}},
	"dashscope":  {"low": {"qwen3.8-flash"}, "mid": {"qwen3.7-plus"}, "high": {"qwen3.8-max"}},
	"deepseek":   {"low": {"deepseek-flash"}, "mid": {"deepseek-v4-pro"}, "high": {}},
	"moonshot":   {"low": {}, "mid": {"kimi-k3"}, "high": {}},
	"minimax":    {"low": {"minimax-m3"}, "mid": {}, "high": {}},
	"openrouter": {"low": {}, "mid": {"anthropic/claude-sonnet-5.5"}, "high": {"anthropic/claude-opus-5.5"}},
	"anthropic":  {"low": {"claude-haiku-5-5"}, "mid": {"claude-sonnet-5-5"}, "high": {"claude-opus-5-5"}},
	"agnes":      {"low": {"agnes-3.0-flash", "agnes-2.5-flash"}, "mid": {"agnes-3.0-flash", "agnes-2.5-pro-beta"}, "high": {"agnes-2.5-pro"}},
	"winwork":    {"low": {"winwork-ds-v4.1-flash", "winwork-qw-3.8-flash"}, "mid": {"winwork-ds-v4-pro-0813", "winwork-gk-4.6"}, "high": {"winwork-snt-5.5", "winwork-os-5.5"}},
	"ark":        {"low": {}, "mid": {}, "high": {}},
	"302ai":      {"low": {}, "mid": {}, "high": {}},
}

func (o *accountOps) Preset(provider string) (string, string, map[string][]string, bool, bool) {
	p, ok := api.PresetByID(provider)
	if !ok {
		return "", "", nil, false, false
	}
	tiers := defaultTiers[provider]
	if tiers == nil {
		tiers = map[string][]string{"low": {}, "mid": {}, "high": {}}
	}
	return p.BaseURL, p.Currency, tiers, p.NeedsProxy, true
}

func (o *accountOps) ListModels(ctx context.Context, baseURL, apiKey string) ([]config.DiscoveredModel, error) {
	ms, err := api.ListModels(ctx, o.a.httpClient(), baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	out := make([]config.DiscoveredModel, 0, len(ms))
	for _, m := range ms {
		out = append(out, config.DiscoveredModel{ID: m.ID, Name: m.Name})
	}
	return out, nil
}

func (o *accountOps) Providers() []string {
	var out []string
	for _, p := range api.Presets {
		if p.ID == "winwork" {
			continue // 不用填密钥，装了 WinWork 就自动出现
		}
		out = append(out, p.ID)
	}
	return out
}

// Discover 找本机能自动接的账号；找到新的就重建驱动。
func (o *accountOps) Discover() bool {
	if !ensureWinWorkAccount(o.a.Cfg) {
		return false
	}
	o.Reload(o.a.Cfg.Get())
	return true
}

// ensureWinWorkAccount 在装了 WinWork 时把它的网关记成一个按量付费账号（不计费）；已有就不动。返回是否新加。
func ensureWinWorkAccount(cfg *config.Store) bool {
	if !api.WinWorkInstalled() {
		return false
	}
	for _, acc := range cfg.Get().API {
		if acc.Provider == "winwork" {
			return false
		}
	}
	tiers := map[string][]string{}
	for k, v := range defaultTiers["winwork"] {
		tiers[k] = append([]string{}, v...)
	}
	return cfg.Update(func(c *config.Config) error {
		c.API = append(c.API, config.APIAccount{ID: "winwork", Provider: "winwork", Label: "WinWork", Currency: "CNY", Enabled: true, AllowPaid: true, Models: tiers})
		return nil
	}) == nil
}

// secretsWithWinWork 让「验证账号」也能拿到 WinWork 的令牌：它不在钥匙串里，从 WinWork 的配置现读。
type secretsWithWinWork struct{ core.Secrets }

func (s secretsWithWinWork) Get(name string) (string, error) {
	if name == "winwork" {
		_, tok, err := api.WinWorkGateway()
		return tok, err
	}
	return s.Secrets.Get(name)
}

func (o *accountOps) Reload(cfg *config.Config) {
	o.a.API = api.NewAPIDriver(o.a.apiAccounts(cfg), o.a.httpClient())
	o.a.Router.Register(o.a.API)
	o.a.Media = media.New(o.a.mediaOptions(o.a.httpClient()))
	o.a.Router.Register(o.a.Media)
}

type mediaOps struct{ a *App }

// 取消与重试由媒体驱动实现；用接口断言是为了让两边可以独立演进。
type canceler interface {
	Cancel(ctx context.Context, jobID string) (*core.Job, error)
}
type retrier interface {
	Retry(ctx context.Context, jobID string, model string) (*core.Job, error)
}

func (o *mediaOps) Cancel(ctx context.Context, id string) (*core.Job, error) {
	if c, ok := any(o.a.Media).(canceler); ok {
		return c.Cancel(ctx, id)
	}
	return o.a.Jobs.Update(id, func(j *core.Job) {
		if j.Status != "done" {
			j.Status = "canceled"
			j.Error = "已取消"
		}
	})
}

func (o *mediaOps) Retry(ctx context.Context, id, model string) (*core.Job, error) {
	if r, ok := any(o.a.Media).(retrier); ok {
		return r.Retry(ctx, id, model)
	}
	return nil, errors.New("这个版本还不支持换模型重做")
}

func (o *mediaOps) Revise(ctx context.Context, id, instruction string) (*core.Job, error) {
	go func() {
		_, _ = o.a.Media.ReviseAnimation(context.Background(), id, instruction, nil)
	}()
	time.Sleep(200 * time.Millisecond)
	j, ok := o.a.Jobs.Get(id)
	if !ok {
		return nil, errors.New("任务不存在")
	}
	return j, nil
}

func labelOrID(name, id string) string {
	if name != "" {
		return name
	}
	return id
}

// workbuddyBilling 给 WorkBuddy 的请求换算计价型号：点名的是它自带的模型就按那个模型的倍率，否则按它的「均衡」档。
// 每个倍率登记成一个 workbuddy:<id> 型号，单价是 306 积分 × 倍率 × ¥0.05 每百万 token，输入、输出、缓存同价。
func (a *App) workbuddyBilling(host, hostModel string) string {
	if host != "workbuddy" {
		return ""
	}
	models := a.Integr.WorkBuddyModels()
	reg := a.Router.Registry()
	for _, m := range models {
		id := "workbuddy:" + m.ID
		usd := integrations.WorkBuddyCNYPerM(m.Mult) * reg.USDPerCNY()
		name := m.Name
		if name == "" {
			name = m.ID
		}
		reg.Upsert(core.ModelInfo{ID: id, Provider: "workbuddy", Display: fmt.Sprintf("WorkBuddy %s（×%.2f 积分）", name, m.Mult), InputPerM: usd, OutputPerM: usd, CacheReadPerM: usd, Currency: "CNY"})
	}
	if _, ok := models[hostModel]; ok {
		return "workbuddy:" + hostModel
	}
	return "workbuddy:" + integrations.WorkBuddyDefaultModel
}

// jevJudge 把智能分派接到 Jev 协议：云端的 Jev，或本机的 Laya（laya-serve 说同一套协议）。
// 原话脱敏、只留开头，连同工具、看图这些信号一起问一道选择题。密钥现取，不缓存。
func (a *App) jevJudge(client *http.Client) core.SmartJudge {
	cloud := &jev.Client{HTTP: client, Key: func() (string, error) { return a.Secrets.Get(admin.SmartKeyID) }}
	// 本机不走代理；空闲卸载后第一次要重新加载权重，给宽一点
	local := &jev.Client{Name: "Laya", HTTP: &http.Client{Transport: &http.Transport{Proxy: nil}}, Endpoint: laya.Endpoint(), Key: a.Laya.Key, Timeout: 8 * time.Second}
	return func(ctx context.Context, task string, signals map[string]any, criteria map[string]string, instructions string) (map[string]float64, error) {
		jc := cloud
		if a.Cfg.Get().Routing.Smart.JudgeOrDefault() == "laya" {
			if !a.Laya.Running() {
				return nil, errors.New("本机 Laya 没在运行")
			}
			jc = local
		}
		state := map[string]any{"task": jev.Redact(task, 1500)}
		for k, v := range signals {
			state[k] = v
		}
		ans, err := jc.Choose(ctx, state, criteria, instructions)
		if err != nil {
			return nil, err
		}
		return ans.Probabilities, nil
	}
}

// envWithoutProxy 是给 Laya 子进程的环境：去掉代理。国内直连 PyPI、Hugging Face 每秒几 MB，走代理反而只有几十 KB。
func envWithoutProxy() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY":
			continue
		}
		out = append(out, kv)
	}
	return out
}

// accountOfCall 从记录的候选里找出实际用的那个账号（记录本身不存账号 id）。
func accountOfCall(c *core.Call) string {
	for _, t := range c.Candidates {
		if t.Model == c.Model && t.Lane == c.Lane && t.Skipped == "" {
			return t.AccountID
		}
	}
	return ""
}
