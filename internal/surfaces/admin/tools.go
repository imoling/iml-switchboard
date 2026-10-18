package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/imoling/iml-switchboard/internal/config"
	"github.com/imoling/iml-switchboard/internal/core"
)

// RunTool 执行一个 MCP 工具。返回给宿主的文字永远用标准措辞，不走主题词表。
func RunTool(ctx context.Context, r *core.Router, jobs *core.MemJobStore, req ToolRequest) (string, error) {
	var a struct {
		Prompt    string `json:"prompt"`
		Spec      string `json:"spec"`
		Tier      string `json:"tier"`
		Size      string `json:"size"`
		OutDir    string `json:"out_dir"`
		Seconds   int    `json:"seconds"`
		ImagePath string `json:"image_path"`
		Engine    string `json:"engine"`
		JobID     string `json:"job_id"`
	}
	_ = json.Unmarshal(req.Args, &a)
	host := req.Host
	if host == "" {
		host = "mcp"
	}
	reg := r.Registry()
	switch req.Name {
	case "ask_model":
		tier := core.Tier(a.Tier)
		if tier == "" {
			tier = core.TierLow
		}
		t := &core.Task{Prompt: a.Prompt, Kind: core.KindText, Tier: tier, Source: "mcp", Host: host, SessionID: req.SessionID}
		call, res, err := r.Run(ctx, t, nil)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(res.Text) + "\n\n（" + reg.Display(call.Model) + " · " + fmtCost(r.Config().Get().Settings, call) + "）", nil
	case "generate_image":
		t := &core.Task{Prompt: a.Prompt, Kind: core.KindImage, MediaTier: a.Tier, Size: a.Size, OutDir: a.OutDir, ProjectDir: req.ProjectDir, Source: "mcp", Host: host}
		call, res, err := r.Run(ctx, t, nil)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("已生成 %s（%s · %s）", res.Path, reg.Display(call.Model), fmtCost(r.Config().Get().Settings, call)), nil
	case "generate_video", "make_animation":
		kind := core.KindVideo
		prompt := a.Prompt
		if req.Name == "make_animation" {
			kind = core.KindAnimation
			prompt = a.Spec
			if a.Seconds == 0 {
				a.Seconds = 8
			}
		} else if a.Seconds == 0 {
			a.Seconds = 5
		}
		t := &core.Task{Prompt: prompt, Kind: kind, MediaTier: a.Tier, Seconds: a.Seconds, ImagePath: a.ImagePath, Engine: a.Engine, OutDir: a.OutDir, ProjectDir: req.ProjectDir, Source: "mcp", Host: host}
		// MCP 调用方最多等 90 秒
		wctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		call, res, err := r.Run(wctx, t, nil)
		if err != nil {
			return "", err
		}
		if res.Path != "" {
			return fmt.Sprintf("已生成 %s（%s · %s）", res.Path, reg.Display(call.Model), fmtCost(r.Config().Get().Settings, call)), nil
		}
		return fmt.Sprintf("任务 %s 进行中，用 get_job 查询（%s）", res.JobID, reg.Display(call.Model)), nil
	case "get_job":
		j, ok := jobs.Get(a.JobID)
		if !ok {
			return "", fmt.Errorf("任务不存在：%s", a.JobID)
		}
		switch j.Status {
		case "done":
			return fmt.Sprintf("已完成 %s（%s）", j.ResultPath, reg.Display(j.Model)), nil
		case "failed", "timeout", "canceled":
			return fmt.Sprintf("生成失败：%s", j.Error), nil
		default:
			eta := ""
			if j.ETASeconds > 0 {
				eta = fmt.Sprintf("，约 %d 秒", j.ETASeconds)
			}
			return fmt.Sprintf("进行中 %.0f%%%s", j.Progress*100, eta), nil
		}
	}
	return "", fmt.Errorf("不认识的工具：%s", req.Name)
}

func fmtCost(st config.Settings, c *core.Call) string {
	if c.Lane != core.LaneAPI && c.Lane != core.LaneMedia {
		return "订阅"
	}
	return st.MoneyNative(c.CostUSD, c.CostNative, c.Currency)
}
