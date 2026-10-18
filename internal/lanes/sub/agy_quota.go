package sub

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

// ProbeQuota 用 agy 的 /usage 斜杠命令读真实额度。它只查用量，不调用模型。
func (d *AntigravityDriver) ProbeQuota(ctx context.Context) ([]core.QuotaWindow, error) {
	bin := lookBin(d.bin())
	if bin == "" {
		return nil, errors.New("本机没有找到 agy 命令")
	}
	var out strings.Builder
	spec := procSpec{Bin: bin, Args: []string{"-p", "/usage", "--output-format", "text", "--print-timeout", "40s"}, Timeout: 60 * time.Second,
		OnLine: func(line string) { out.WriteString(line); out.WriteString("\n") }}
	res := runProc(ctx, spec)
	if res.Err != nil {
		return nil, res.Err
	}
	ws := ParseAgyUsage(out.String())
	if len(ws) == 0 {
		return nil, errors.New("agy /usage 没有返回额度：" + firstLine(res.Stderr))
	}
	return ws, nil
}

// ParseAgyUsage 解析 `agy -p /usage` 的制表符表格：
// 组名 \t Weekly|Five Hour Limit Remaining \t 99% \t 2026-10-25T03:09:58Z
// Gemini 一组用标准窗口名；Claude 和 GPT 一组另起窗口名，界面上单独一行。
func ParseAgyUsage(text string) []core.QuotaWindow {
	var out []core.QuotaWindow
	for _, line := range strings.Split(text, "\n") {
		cols := strings.Split(strings.TrimSpace(line), "\t")
		if len(cols) < 3 {
			continue
		}
		group, kind, pctStr := strings.TrimSpace(cols[0]), strings.ToLower(cols[1]), strings.TrimSuffix(strings.TrimSpace(cols[2]), "%")
		remaining, err := strconv.ParseFloat(pctStr, 64)
		if err != nil {
			continue
		}
		w := core.QuotaWindow{Pct: 100 - remaining}
		switch {
		case strings.Contains(kind, "five hour"):
			w.Window = "5h"
		case strings.Contains(kind, "week"):
			w.Window = "week"
		default:
			continue
		}
		if !strings.HasPrefix(strings.ToLower(group), "gemini") {
			// Claude 与 GPT 共用一组额度
			w.Window += "_claude"
			if w.Window == "5h_claude" {
				w.Label = "Claude / GPT 5 小时窗口"
			} else {
				w.Label = "Claude / GPT 本周"
			}
		}
		if len(cols) >= 4 {
			if t, err := time.Parse(time.RFC3339, strings.TrimSpace(cols[3])); err == nil {
				w.ResetsAt = t
			}
		}
		out = append(out, w)
	}
	return out
}
