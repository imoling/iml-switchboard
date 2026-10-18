package sub

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

// ProbeQuota 通过 codex app-server 的 account/rateLimits/read 读取真实额度。
// 这是官方 CLI 自带的本机接口，不发任何付费请求，也不碰登录文件。
func (d *CodexDriver) ProbeQuota(ctx context.Context) ([]core.QuotaWindow, error) {
	bin := lookBin(d.bin())
	if bin == "" {
		return nil, errors.New("本机没有找到 codex 命令")
	}
	pctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(pctx, bin, "app-server")
	cmd.Env = cleanEnv(os.Environ())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	send := func(v any) error {
		b, _ := json.Marshal(v)
		_, err := stdin.Write(append(b, '\n'))
		return err
	}
	reader := bufio.NewReader(stdout)
	waitID := func(id int) (json.RawMessage, error) {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return nil, err
			}
			var m struct {
				ID     *int            `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(line), &m) != nil || m.ID == nil || *m.ID != id {
				continue
			}
			if m.Error != nil {
				return nil, errors.New(m.Error.Message)
			}
			return m.Result, nil
		}
	}
	if err := send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "switchboard", "version": "0.1"}}}); err != nil {
		return nil, err
	}
	if _, err := waitID(1); err != nil {
		return nil, err
	}
	_ = send(map[string]any{"jsonrpc": "2.0", "method": "initialized"})
	if err := send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "account/rateLimits/read", "params": map[string]any{}}); err != nil {
		return nil, err
	}
	raw, err := waitID(2)
	if err != nil {
		return nil, err
	}
	windows, plan, err := ParseCodexRateLimits(raw)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.planType = plan
	d.mu.Unlock()
	return windows, nil
}

type codexRateWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins int     `json:"windowDurationMins"`
	ResetsAt           int64   `json:"resetsAt"`
}

// ParseCodexRateLimits 把 account/rateLimits/read 的结果转成额度窗口；返回套餐类型。
func ParseCodexRateLimits(raw json.RawMessage) ([]core.QuotaWindow, string, error) {
	var r struct {
		RateLimits struct {
			Primary   *codexRateWindow `json:"primary"`
			Secondary *codexRateWindow `json:"secondary"`
			PlanType  string           `json:"planType"`
		} `json:"rateLimits"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, "", err
	}
	var out []core.QuotaWindow
	for _, w := range []*codexRateWindow{r.RateLimits.Primary, r.RateLimits.Secondary} {
		if w == nil {
			continue
		}
		q := core.QuotaWindow{Window: windowByMinutes(w.WindowDurationMins), Pct: w.UsedPercent}
		if w.ResetsAt > 0 {
			q.ResetsAt = time.Unix(w.ResetsAt, 0)
		}
		out = append(out, q)
	}
	if len(out) == 0 {
		return nil, r.RateLimits.PlanType, errors.New("没有额度窗口")
	}
	return out, r.RateLimits.PlanType, nil
}

func windowByMinutes(m int) string {
	switch {
	case m <= 0:
		return "window"
	case m <= 360:
		return "5h"
	case m <= 60*24*8:
		return "week"
	default:
		return "month"
	}
}

// PlanLabel 把 planType 变成界面上的说法。
func PlanLabel(plan string) string {
	switch strings.ToLower(plan) {
	case "free":
		return "免费版"
	case "plus":
		return "Plus"
	case "pro":
		return "Pro"
	case "team":
		return "Team"
	case "enterprise", "business":
		return "企业版"
	case "":
		return ""
	}
	return plan
}
