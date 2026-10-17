// imls 是 iML Switchboard 的单文件入口：serve / mcp / route / stats / doctor / install。
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
	"github.com/imoling/iml-switchboard/internal/config"
	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/launchd"
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
		err = serve()
	case "mcp":
		err = mcp.Serve(context.Background(), app.BaseURL, version)
	case "route":
		err = route(os.Args[2:])
	case "stats":
		err = stats()
	case "doctor":
		err = doctor()
	case "install":
		exe, _ := os.Executable()
		err = launchd.Install(exe, filepath.Join(config.DataDir(), "logs"))
		if err == nil {
			fmt.Println("已注册开机自启")
		}
	case "uninstall":
		err = launchd.Uninstall()
	case "version", "--version", "-v":
		fmt.Println("iML Switchboard", version)
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
  imls serve                常驻后台，监听 127.0.0.1:18081
  imls mcp                  作为 MCP 小程序被宿主拉起
  imls route "任务"          只打印路由决策
  imls route --run "任务"    真正执行
  imls stats                本周账本摘要
  imls doctor               检查本机 CLI、Node、账号
  imls install | uninstall  注册 / 取消开机自启`)
}

func serve() error {
	a, err := app.New(version)
	if err != nil {
		return err
	}
	defer a.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(os.Stderr, "iML Switchboard %s · %s · 数据目录 %s\n", version, app.BaseURL, config.DataDir())
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
	t := &core.Task{Prompt: prompt, Source: "cli"}
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
	fmt.Println("候选：")
	for _, c := range d.Candidates {
		mark := "✓"
		if c.Skipped != "" {
			mark = "跳过（" + c.Skipped + "）"
		}
		fmt.Printf("  %-12s %-36s $%.4f  %s\n", c.Lane, c.Model, c.EstCostUSD, mark)
	}
	if !run {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	call, res, err := a.Router.Execute(ctx, t, d, func(e core.Event) {
		if e.Type == "text_delta" {
			fmt.Print(e.Text)
		}
	})
	if err != nil {
		return err
	}
	if res.Text != "" && !t.Stream {
		fmt.Println(res.Text)
	}
	if res.Path != "" {
		fmt.Println("产物：", res.Path)
	}
	if res.JobID != "" {
		fmt.Println("任务：", res.JobID)
	}
	fmt.Printf("\n—— %s · %s · $%.4f（全用 Opus 约 $%.4f）\n", call.Lane, a.Router.Registry().Display(call.Model), call.CostUSD, call.BaselineCostUSD)
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
	fmt.Printf("近 7 天：%d 次 · 按量花费 $%.2f · 比全用 Opus 省 $%.2f（%.0f%%）· 手动升档率 %.1f%%\n", ov.Calls, ov.APICostUSD, ov.SavedUSD, ov.SavedPct, ov.EscalationRate)
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
	if launchd.Installed() {
		fmt.Println("开机自启     已注册")
	}
	return nil
}
