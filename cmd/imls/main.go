// imls 是 Switchboard 的单文件入口：serve / mcp / route / stats / doctor / install。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/imoling/iml-switchboard/internal/app"
	"github.com/imoling/iml-switchboard/internal/autostart"
	"github.com/imoling/iml-switchboard/internal/config"
	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/surfaces/admin"
	"github.com/imoling/iml-switchboard/internal/surfaces/mcp"
)

var version = "0.1.0"

func main() {
	admin.Version = version
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(len(os.Args) > 2 && os.Args[2] == "--background")
	case "mcp":
		err = mcp.Serve(context.Background(), app.BaseURL, version)
	case "route":
		err = route(os.Args[2:])
	case "stats":
		err = stats()
	case "doctor":
		err = doctor()
	case "golden":
		err = golden()
	case "install":
		exe, _ := os.Executable()
		err = autostart.Install(exe, filepath.Join(config.DataDir(), "logs"))
		if err == nil {
			fmt.Println("已注册开机自启")
		}
	case "uninstall":
		err = autostart.Uninstall()
	case "version", "--version", "-v":
		fmt.Println("Switchboard", version)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `用法：
  imls serve [--background] 常驻后台，监听 127.0.0.1:18081；--background 时不占命令行窗口，日志写进数据目录
  imls mcp                  作为 MCP 小程序被宿主拉起
  imls route "任务"          只打印路由决策
  imls route --run "任务"    真正执行
  imls stats                本周账本摘要
  imls doctor               检查本机 CLI、Node、账号
  imls golden               把近 90 天「判低了」的请求导出成路由金标候选
  imls install | uninstall  注册 / 取消开机自启`)
}

// serve 常驻。background 时脱离命令行窗口，输出写进数据目录下的 logs/serve.log（开机自启、宿主拉起都走这条）。
func serve(background bool) error {
	if background {
		hideConsole()
		logDir := filepath.Join(config.DataDir(), "logs")
		if err := os.MkdirAll(logDir, 0o755); err == nil {
			if f, err := os.OpenFile(filepath.Join(logDir, "serve.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
				os.Stdout, os.Stderr = f, f
			}
		}
	}
	a, err := app.New(version)
	if err != nil {
		return err
	}
	defer a.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(os.Stderr, "Switchboard %s · %s · 数据目录 %s\n", version, app.BaseURL, config.DataDir())
	return a.Serve(ctx)
}

func route(args []string) error {
	run := false
	var parts []string
	for _, a := range args {
		if a == "--run" {
			run = true
			continue
		}
		parts = append(parts, a)
	}
	prompt := strings.Join(parts, " ")
	if prompt == "" {
		return fmt.Errorf("要路由什么？imls route \"任务\"")
	}
	a, err := app.New(version)
	if err != nil {
		return err
	}
	defer a.Close()
	cwd, _ := os.Getwd()
	t := &core.Task{Prompt: prompt, Source: "cli", ProjectDir: cwd}
	d := a.Router.Decide(t)
	fmt.Printf("任务类型：%s（%s）\n", a.Router.TaskName(d.TaskType), d.TaskType)
	if d.Kind == core.KindText {
		fmt.Printf("档位：%s\n", core.TierLabel(d.Tier))
	} else {
		fmt.Printf("模态：%s · %s\n", d.Kind, d.MediaTier)
	}
	for _, r := range d.Reason {
		fmt.Println("  · " + r)
	}
	if d.Unsure && !d.Sticky && !a.Router.Config().Get().Settings.RulesOnly {
		fmt.Println("  · 规则拿不准，执行时先请低档模型判档（加 --run 看实际结果）")
	}
	fmt.Println("候选：")
	for _, c := range d.Candidates {
		mark := "✓"
		if c.Skipped != "" {
			mark = "跳过（" + c.Skipped + "）"
		}
		fmt.Printf("  %-12s %-36s %-9s %s\n", c.Lane, c.Model, a.Cfg.Get().Settings.Money(c.EstCostUSD), mark)
	}
	if !run {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	streamed := false
	// 重新建任务走 Run：预览用的 Decide 改过 t，而且判档只在 Run 里做
	t = &core.Task{Prompt: prompt, Source: "cli", ProjectDir: cwd}
	call, res, err := a.Router.Run(ctx, t, func(e core.Event) {
		if e.Type == "text_delta" {
			streamed = true
			fmt.Print(e.Text)
		}
	})
	if err != nil {
		return err
	}
	if streamed {
		fmt.Println()
	} else if res.Text != "" {
		fmt.Println(res.Text)
	}
	if res.Path != "" {
		fmt.Println("产物：", res.Path)
	}
	if res.JobID != "" {
		fmt.Println("任务：", res.JobID)
	}
	for _, r := range call.Reason {
		if strings.Contains(r, "判") && strings.Contains(r, "低档模型") {
			fmt.Println("\n" + r)
		}
	}
	st := a.Cfg.Get().Settings
	fmt.Printf("\n—— %s · %s · %s（全用 Opus 约 %s）\n", call.Lane, a.Router.Registry().Display(call.Model), st.MoneyNative(call.CostUSD, call.CostNative, call.Currency), st.Money(call.BaselineCostUSD))
	return nil
}

func stats() error {
	a, err := app.New(version)
	if err != nil {
		return err
	}
	defer a.Close()
	ov, err := a.Ledger.Aggregate(7, time.Local)
	if err != nil {
		return err
	}
	st := a.Cfg.Get().Settings
	fmt.Printf("近 7 天：%d 次 · 按量花费 %s · 比全用 Opus 省 %s（%.0f%%）· 手动升档率 %.1f%%\n", ov.Calls, st.Money(ov.APICostUSD), st.Money(ov.SavedUSD), ov.SavedPct, ov.EscalationRate)
	b, _ := json.MarshalIndent(map[string]any{"tier_dist": ov.TierDist, "lane_dist": ov.LaneDist, "top_tasks": ov.TopTasks}, "", "  ")
	fmt.Println(string(b))
	return nil
}

func doctor() error {
	a, err := app.New(version)
	if err != nil {
		return err
	}
	defer a.Close()
	ctx := context.Background()
	for _, lane := range []core.LaneID{core.LaneClaudeSub, core.LaneCodexSub, core.LaneAgySub, core.LaneAPI, core.LaneMedia} {
		d, ok := a.Router.Driver(lane)
		if !ok {
			continue
		}
		h := d.Health(ctx)
		fmt.Printf("%-12s %-9s %s\n", lane, h.Status, h.Detail)
	}
	for _, c := range []struct {
		name  string
		check func(context.Context) (string, []string)
	}{{"claude_args", a.Claude.SelfCheck}, {"codex_args", a.Codex.SelfCheck}} {
		ver, problems := c.check(ctx)
		switch {
		case ver == "":
		case len(problems) == 0:
			fmt.Printf("%-12s ok        %s 用到的参数都在\n", c.name, ver)
		default:
			fmt.Printf("%-12s warn      %s\n", c.name, strings.Join(problems, "；"))
		}
	}
	if a.Secrets.Plain() {
		fmt.Printf("密钥         warn      系统凭据库不可用，按量付费密钥存在数据目录的 secrets.local 里（明文，只有当前用户能读）\n")
	}
	if p := proxyWithoutLocal(); p != "" {
		fmt.Printf("代理         warn      设了 %s 但没排除本机，pi、OpenClaw 这些宿主连不上 Switchboard；加上 NO_PROXY=127.0.0.1,localhost\n", p)
	}
	if _, err := exec.LookPath("node"); err != nil {
		fmt.Println("node         missing   动画需要本机安装 Node")
	} else {
		fmt.Println("node         ok")
	}
	cfg := a.Cfg.Get()
	fmt.Printf("按量付费账号 %d 个 · 配置 %s\n", len(cfg.API), a.Cfg.Path())
	if _, err := os.Stat(filepath.Join(config.DataDir(), "ledger.db")); err == nil {
		fmt.Println("账本         ok")
	}
	if autostart.Installed() {
		fmt.Println("开机自启     已注册")
	}
	return nil
}

// golden 把「判低了」的记录导出成 internal/core/testdata/golden.yaml 的格式，想要的档位按高一档填。
// 摘录只存了前 200 字，贴进金标前要人工对一下：截断后规则看到的信号可能和原话不一样。
func golden() error {
	a, err := app.New(version)
	if err != nil {
		return err
	}
	defer a.Close()
	calls, err := a.Router.Ledger().List(core.ListFilter{Kind: string(core.KindText), Since: time.Now().AddDate(0, 0, -90), Limit: 100000})
	if err != nil {
		return err
	}
	up := map[core.Tier]string{core.TierLow: "mid", core.TierMid: "high", core.TierHigh: "high", core.TierTop: "high"}
	n := 0
	fmt.Println("# imls golden 导出：判低了的请求，want 按高一档填；人工复核后贴进 internal/core/testdata/golden.yaml")
	for _, c := range calls {
		if !c.UserEscalated || c.PromptExcerpt == "" || c.Source == "evaluation" {
			continue
		}
		var b strings.Builder
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(strings.TrimSuffix(c.PromptExcerpt, "…"))
		fmt.Printf("- {p: %s, want: %s}  # 原判 %s（%s）\n", strings.TrimSpace(b.String()), up[c.Tier], core.TierLabel(c.Tier), c.TaskType)
		n++
	}
	if n == 0 {
		fmt.Fprintln(os.Stderr, "近 90 天没有「判低了」的记录；关掉「保存提示词」时也导不出原话")
	}
	return nil
}

// proxyWithoutLocal 返回「设了代理却没把本机排除在外」时的那个变量名：宿主会把发往 127.0.0.1 的请求也交给代理，直接连不上。
func proxyWithoutLocal() string {
	noProxy := strings.ToLower(os.Getenv("NO_PROXY") + "," + os.Getenv("no_proxy"))
	if strings.Contains(noProxy, "127.0.0.1") || strings.Contains(noProxy, "localhost") || strings.Contains(noProxy, "*") {
		return ""
	}
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "ALL_PROXY", "all_proxy"} {
		if os.Getenv(k) != "" {
			return k
		}
	}
	return ""
}
