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
	"time"

	"github.com/imoling/iml-switchboard/internal/config"
	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/integrations"
	"github.com/imoling/iml-switchboard/internal/lanes/api"
	"github.com/imoling/iml-switchboard/internal/lanes/media"
	"github.com/imoling/iml-switchboard/internal/lanes/sub"
	"github.com/imoling/iml-switchboard/internal/surfaces/admin"
	"github.com/imoling/iml-switchboard/internal/surfaces/facade"
	mcpsurface "github.com/imoling/iml-switchboard/internal/surfaces/mcp"
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
	Admin   *admin.Server
	Facade  *facade.Server
	Integr  *integrations.Manager
	Version string
	exe     string
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
	a.exe, _ = os.Executable()
	a.Secrets = core.NewSecrets(dir)
	a.Bus = core.NewBus()
	reg := core.NewRegistry(cfg.Get().Settings.USDPerCNY)
	quota := core.NewQuotaManager(ledger, func() float64 { return cfg.Get().Settings.SubscriptionYieldPct })
	a.Router = core.NewRouter(cfg, ledger, reg, quota)
	a.Jobs = core.NewJobStore(ledger, func(j *core.Job) {
		a.Bus.Publish("job", j)
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
	a.API = api.NewAPIDriver(a.apiAccounts(cfg.Get()), client)
	a.Media = media.New(a.mediaOptions(client))
	a.Router.Register(a.Claude)
	a.Router.Register(a.Codex)
	a.Router.Register(sub.NewAntigravityDriver())
	a.Router.Register(a.API)
	a.Router.Register(a.Media)
	a.Router.OnCall(func(c *core.Call) { a.Bus.Publish("call", a.Router.Decorate(c)); a.checkBudget(c) })
	a.Router.OnNotice(func(title, body, level string) {
		if cfg.Get().Settings.Notifications.Quota {
			a.Bus.Publish("notice", core.Notice{Title: title, Body: body, Level: level})
		}
	})

	a.Integr = integrations.New(cfg, a.exe, BaseURL, func() string { return cfg.Get().Settings.FacadeKey })
	a.Admin = admin.New(admin.Deps{Router: a.Router, Jobs: a.Jobs, Bus: a.Bus, Accounts: &accountOps{a: a}, Media: &mediaOps{a: a}, Integr: a.Integr, Secrets: a.Secrets, StartedAt: time.Now(),
		NodeAvailable: func() bool { _, err := exec.LookPath("node"); return err == nil }})
	a.Facade = facade.New(a.Router, func() string { return cfg.Get().Settings.FacadeKey })
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
	proxied := []string{"openrouter.ai", "api.openai.com", "api.anthropic.com", "googleapis.com", "fal.run", "replicate.com"}
	tr := &http.Transport{
		Proxy: func(r *http.Request) (*url.URL, error) {
			p := strings.TrimSpace(a.Cfg.Get().Settings.Proxy)
			if p == "" {
				return nil, nil
			}
			for _, h := range proxied {
				if strings.HasSuffix(r.URL.Host, h) {
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
		p, ok := api.PresetByID(acc.Provider)
		if !ok {
			p = api.Preset{ID: acc.Provider, BaseURL: acc.BaseURL, Currency: acc.Currency, Models: map[string]api.ModelPrice{}}
		}
		id := acc.ID
		ac := p.Account(id, func() (string, error) { return a.Secrets.Get(id) })
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

// Serve 启动后台直到 ctx 结束。
func (a *App) Serve(ctx context.Context) error {
	ln, err := net.Listen("tcp", Addr)
	if err != nil {
		return errors.New("后台已经在运行，或端口 18081 被占用")
	}
	srv := &http.Server{Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	a.startMonthlyEvaluation(ctx)
	err = srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
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
func (a *App) Close() { _ = a.Ledger.Close() }

// ---- admin 依赖的两个适配器 ----

type accountOps struct{ a *App }

func (o *accountOps) Verify(ctx context.Context, provider, baseURL, apiKey string) ([]string, error) {
	p, ok := api.PresetByID(provider)
	if !ok {
		return nil, errors.New("不认识的供应商：" + provider)
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
	"deepseek":   {"low": {"deepseek-v4.1-flash"}, "mid": {"deepseek-v4-pro"}, "high": {}},
	"moonshot":   {"low": {}, "mid": {"kimi-k3"}, "high": {}},
	"minimax":    {"low": {"minimax-m3"}, "mid": {}, "high": {}},
	"openrouter": {"low": {}, "mid": {"anthropic/claude-sonnet-5.5"}, "high": {"anthropic/claude-opus-5.5"}},
	"anthropic":  {"low": {"claude-haiku-5-5"}, "mid": {"claude-sonnet-5-5"}, "high": {"claude-opus-5-5"}},
	"agnes":      {"low": {"agnes-3.0-flash", "agnes-2.5-flash"}, "mid": {"agnes-2.5-pro-beta"}, "high": {"agnes-2.5-pro"}},
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

func (o *accountOps) Providers() []string {
	var out []string
	for _, p := range api.Presets {
		out = append(out, p.ID)
	}
	return out
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
