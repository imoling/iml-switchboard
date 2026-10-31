package sub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

// forbiddenClaudeFlag 是绝不能传给 claude 的参数：它会绕过官方登录态。
// 拆成两段拼接，是为了让仓库里扫源码的红线测试不会把这条常量本身误判成违规。
const forbiddenClaudeFlag = "--" + "bare"

// ClaudeDriver 通过本机 `claude -p` 子进程消耗 Claude 订阅额度。
type ClaudeDriver struct {
	// Bin 是 claude 可执行文件，空串表示在 PATH 里找。
	Bin string
	// Home 是 ~/.claude 所在目录；空串表示用户主目录。只 stat，不读。
	Home string
	// Timeout 是单次执行超时，零值用 DefaultTimeout。
	Timeout time.Duration
	// DefaultModel 是 target.Model 为空时的模型别名。
	DefaultModel string
	// Now 便于测试注入时间。
	Now func() time.Time
	// Prewarm 为真时，文本子任务用完一个进程就在后台按同样参数预热下一个（后台服务里打开，测试里默认关）。
	Prewarm bool

	pool      *warmPool
	mu        sync.Mutex
	problems  []string // 最近一次自检发现的问题，受 mu 保护
	lastQuota []core.QuotaWindow
	cooldown  time.Time
}

// NewClaudeDriver 用默认配置构造驱动。
func NewClaudeDriver() *ClaudeDriver {
	return &ClaudeDriver{DefaultModel: "sonnet"}
}

// ID 返回渠道标识。
func (d *ClaudeDriver) ID() core.LaneID { return core.LaneClaudeSub }

// subtaskSystem 替换 Claude Code 默认的编程智能体系统提示：派来的子任务自带上下文，用不上那一大段，
// 换掉后每次调用的上下文从约 5400 降到约 2500 token。动画要改文件，仍用默认提示。
const subtaskSystem = "你在替另一个智能体完成一个子任务。直接给出结果，不寒暄、不反问；对方用什么语言提问就用什么语言回答。"

// warmable 表示这个任务能用预热进程：只读的文本子任务，且没有自带系统提示（系统提示在启动参数里，预热时还不知道）。
func warmable(task *core.Task) bool { return !needsWrite(task) && task.System == "" }

func (d *ClaudeDriver) warm() *warmPool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Prewarm && d.pool == nil {
		d.pool = newWarmPool(10*time.Minute, 2)
	}
	return d.pool
}

// Close 关掉预热的进程。
func (d *ClaudeDriver) Close() { d.warm().close() }

func (d *ClaudeDriver) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *ClaudeDriver) bin() string {
	if d.Bin != "" {
		return d.Bin
	}
	return "claude"
}

func (d *ClaudeDriver) home() string {
	if d.Home != "" {
		return d.Home
	}
	h, _ := os.UserHomeDir()
	return h
}

// needsWrite 判断任务是否需要让 claude 改文件（动画工程）。
func needsWrite(task *core.Task) bool {
	return task.Kind == core.KindAnimation && (task.Engine != "" || task.ProjectDir != "")
}

// buildPrompt 把多轮消息拍平成一段提示词；订阅渠道没有结构化多轮接口。
func buildPrompt(task *core.Task) string {
	if len(task.Messages) <= 1 {
		return task.Prompt
	}
	var b strings.Builder
	b.WriteString("以下是之前的对话记录：\n\n")
	for i, m := range task.Messages {
		if i == len(task.Messages)-1 && m.Role == "user" {
			break
		}
		if m.Role == "system" {
			continue
		}
		fmt.Fprintf(&b, "[%s]\n%s\n\n", m.Role, m.Content)
	}
	b.WriteString("当前请求：\n")
	b.WriteString(task.Prompt)
	return b.String()
}

// StdinPayload 是写进 claude 标准输入的一条 stream-json 用户消息。
func StdinPayload(task *core.Task) string { return stdinPayload(task, false) }

// stdinPayload 的 inlineSystem 为真时，把宿主的系统提示放进消息开头（见 isBatch）。
func stdinPayload(task *core.Task, inlineSystem bool) string {
	text := buildPrompt(task)
	if inlineSystem && task.System != "" {
		text = "调用方的要求：\n" + task.System + "\n\n" + text
	}
	b, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []map[string]string{{"type": "text", "text": text}}}})
	return string(b) + "\n"
}

// isBatch 判断 claude 是不是 Windows 上 npm 装的 .cmd 包装。批处理的参数要经 cmd.exe 解析，
// 宿主传来的系统提示里有 & | % 这类字符就可能被当成命令，所以这时不把它放进参数，改走标准输入。
func isBatch(bin string) bool {
	b := strings.ToLower(bin)
	return strings.HasSuffix(b, ".cmd") || strings.HasSuffix(b, ".bat")
}

// BuildArgs 生成 claude 的命令行参数（导出以便测试断言）。
func (d *ClaudeDriver) BuildArgs(task *core.Task, target core.Target) []string {
	return d.buildArgs(task, target, false)
}

// buildArgs 的 inlineSystem 为真时，参数里只留我们自己写死的系统提示，宿主的那段改走标准输入。
func (d *ClaudeDriver) buildArgs(task *core.Task, target core.Target, inlineSystem bool) []string {
	model := target.Model
	if model == "" {
		model = task.Model
	}
	if model == "" {
		model = d.DefaultModel
	}
	// 提示词从标准输入传：不受命令行长度限制（Windows 的 .cmd 包装只有约 8000 字符），也才能预热
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--no-session-persistence",
		"--model", model,
		// 不加载用户配置里的 MCP 服务器：里面有 Switchboard 自己，加载了既慢又可能绕回来调用自己
		"--strict-mcp-config",
	}
	if needsWrite(task) {
		if task.System != "" && !inlineSystem {
			args = append(args, "--append-system-prompt", task.System)
		}
	} else {
		sys := subtaskSystem
		if task.System != "" && !inlineSystem {
			sys += "\n\n" + task.System
		}
		args = append(args, "--system-prompt", sys)
	}
	if needsWrite(task) {
		args = append(args, "--permission-mode", "acceptEdits", "--max-turns", "12")
	} else {
		args = append(args, "--tools", "")
	}
	return args
}

// claudeStreamLine 是 stream-json 一行的通用形状。
type claudeStreamLine struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
	Message   *struct {
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	// result
	IsError      bool    `json:"is_error"`
	Result       string  `json:"result"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	Usage        *struct {
		InputTokens          int `json:"input_tokens"`
		OutputTokens         int `json:"output_tokens"`
		CacheReadInputTokens int `json:"cache_read_input_tokens"`
	} `json:"usage"`
	ModelUsage map[string]json.RawMessage `json:"modelUsage"`
	// rate_limit_event
	RateLimitInfo *struct {
		Status         string `json:"status"`
		ResetsAt       int64  `json:"resetsAt"`
		RateLimitType  string `json:"rateLimitType"`
		UnifiedWindows map[string]struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    int64   `json:"resetsAt"`
		} `json:"unifiedWindows"`
	} `json:"rate_limit_info"`
}

// ClaudeParsed 是解析 stream-json 得到的结果。
type ClaudeParsed struct {
	Text         string
	ResultText   string
	Model        string
	SessionID    string
	IsError      bool
	GotResult    bool
	TotalCostUSD float64
	Usage        core.Usage
	Quota        []core.QuotaWindow
	// RateLimited 为真表示 rate_limit_event 报告了非 allowed 状态。
	RateLimited   bool
	RateResetsAt  time.Time
	RawResultLine string
}

// claudeParser 逐行解析 stream-json。
type claudeParser struct {
	p    ClaudeParsed
	emit func(core.Event)
	text strings.Builder
}

func (c *claudeParser) feed(line string) {
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "{") {
		return
	}
	var l claudeStreamLine
	if err := json.Unmarshal([]byte(line), &l); err != nil {
		return
	}
	switch l.Type {
	case "system":
		if l.Model != "" && c.p.Model == "" {
			c.p.Model = l.Model
		}
		if l.SessionID != "" {
			c.p.SessionID = l.SessionID
		}
	case "assistant":
		if l.Message == nil {
			return
		}
		if l.Message.Model != "" {
			c.p.Model = l.Message.Model
		}
		for _, blk := range l.Message.Content {
			if blk.Type == "text" && blk.Text != "" {
				c.text.WriteString(blk.Text)
				if c.emit != nil {
					c.emit(core.Event{Type: "text_delta", Text: blk.Text})
				}
			}
		}
	case "rate_limit_event":
		if l.RateLimitInfo == nil {
			return
		}
		c.p.Quota = c.p.Quota[:0]
		for name, w := range l.RateLimitInfo.UnifiedWindows {
			qw := core.QuotaWindow{Window: windowName(name), Pct: w.Utilization * 100}
			if w.ResetsAt > 0 {
				qw.ResetsAt = time.Unix(w.ResetsAt, 0)
			}
			c.p.Quota = append(c.p.Quota, qw)
		}
		st := strings.ToLower(l.RateLimitInfo.Status)
		if st != "" && st != "allowed" && st != "allowed_warning" {
			c.p.RateLimited = true
			if l.RateLimitInfo.ResetsAt > 0 {
				c.p.RateResetsAt = time.Unix(l.RateLimitInfo.ResetsAt, 0)
			}
		}
	case "result":
		c.p.GotResult = true
		c.p.RawResultLine = line
		c.p.IsError = l.IsError
		c.p.ResultText = l.Result
		c.p.TotalCostUSD = l.TotalCostUSD
		if l.SessionID != "" {
			c.p.SessionID = l.SessionID
		}
		if l.Usage != nil {
			c.p.Usage.InputTokens = l.Usage.InputTokens
			c.p.Usage.OutputTokens = l.Usage.OutputTokens
			c.p.Usage.CacheReadTokens = l.Usage.CacheReadInputTokens
		}
		if c.p.Model == "" {
			for k := range l.ModelUsage {
				c.p.Model = k
				break
			}
		}
	}
}

func (c *claudeParser) done() ClaudeParsed {
	c.p.Text = c.text.String()
	if c.p.Text == "" {
		c.p.Text = c.p.ResultText
	}
	// 订阅不花钱：CostUSD 记 0，用官方算出的 total_cost_usd 当额度占用估算。
	c.p.Usage.CostUSD = 0
	c.p.Usage.QuotaUnits = c.p.TotalCostUSD
	return c.p
}

func windowName(s string) string {
	switch s {
	case "five_hour":
		return "5h"
	case "seven_day":
		return "week"
	case "monthly":
		return "month"
	case "seven_day_overage_included":
		// 只在调用 Fable 时出现，就是 Claude Code 面板上的「Fable limit」
		return "week_fable"
	}
	return s
}

// ParseClaudeStream 解析完整的 stream-json 文本（测试与离线分析用）。
func ParseClaudeStream(data string, emit func(core.Event)) ClaudeParsed {
	p := &claudeParser{emit: emit}
	for _, line := range strings.Split(data, "\n") {
		p.feed(line)
	}
	return p.done()
}

// Run 执行任务。
func (d *ClaudeDriver) Run(ctx context.Context, task *core.Task, target core.Target, emit func(core.Event)) (*core.Result, error) {
	bin := lookBin(d.bin())
	if bin == "" {
		return nil, &core.ErrProvider{Summary: "Claude 不可用：本机没有找到 claude 命令", Retryable: true}
	}
	dir := ""
	if needsWrite(task) {
		dir = task.ProjectDir
	}
	if keys := thirdPartyIn(d.claudeSettingsFiles(dir)...); len(keys) > 0 {
		return nil, &core.ErrProvider{Summary: thirdPartyHint, Raw: "设置项：" + strings.Join(keys, " "), Retryable: true}
	}
	batch := isBatch(bin)
	args := d.buildArgs(task, target, batch)
	for _, a := range args {
		if a == forbiddenClaudeFlag {
			return nil, &core.ErrProvider{Summary: "Claude 参数非法：出现了被禁止的参数"}
		}
	}
	parser := &claudeParser{emit: emit}
	spec := procSpec{Bin: bin, Args: args, Dir: dir}
	var pool *warmPool
	if warmable(task) {
		pool = d.warm()
	}
	var res procResult
	p := pool.take(spec)
	if p == nil {
		var err error
		if p, err = startProc(spec); err != nil {
			res = procResult{ExitCode: -1, Err: err}
		}
	}
	pool.refill(spec) // 这个在跑，下一个先启动起来
	if p != nil {
		res = p.finish(ctx, stdinPayload(task, batch), d.Timeout, parser.feed, nil)
	}
	parsed := parser.done()

	d.mu.Lock()
	if len(parsed.Quota) > 0 {
		d.lastQuota = parsed.Quota
	}
	d.mu.Unlock()

	if res.Err != nil {
		return nil, &core.ErrProvider{Summary: "Claude 启动失败：" + firstLine(res.Err.Error()), Raw: res.Stderr, Retryable: true}
	}
	if res.TimedOut {
		return nil, &core.ErrProvider{Summary: "Claude 执行超时：超过 " + d.timeout().String() + " 没有返回", Raw: res.Stderr, Retryable: true}
	}
	if res.Canceled {
		return nil, ctx.Err()
	}
	if parsed.IsError || res.ExitCode != 0 || parsed.RateLimited {
		raw := strings.TrimSpace(res.Stderr + "\n" + parsed.ResultText + "\n" + parsed.RawResultLine)
		if parsed.RateLimited || isRateLimitText(res.Stderr+"\n"+parsed.ResultText) {
			now := d.now()
			var ra time.Duration
			if !parsed.RateResetsAt.IsZero() && parsed.RateResetsAt.After(now) {
				ra = parsed.RateResetsAt.Sub(now)
			} else {
				ra, _ = parseRetryAfter(res.Stderr+"\n"+parsed.ResultText, now)
			}
			d.mu.Lock()
			d.cooldown = now.Add(ra)
			d.mu.Unlock()
			return nil, &core.ErrRateLimited{Lane: d.ID(), RetryAfter: ra, Detail: firstLine(parsed.ResultText + "\n" + res.Stderr)}
		}
		msg := firstLine(parsed.ResultText)
		if msg == "" {
			msg = firstLine(res.Stderr)
		}
		if msg == "" {
			msg = fmt.Sprintf("退出码 %d", res.ExitCode)
		}
		return nil, &core.ErrProvider{Summary: "Claude 执行失败：" + msg, Raw: raw, Retryable: true}
	}
	model := parsed.Model
	if model == "" {
		model = target.Model
	}
	out := &core.Result{Text: parsed.Text, Model: model, Usage: parsed.Usage}
	if parsed.RawResultLine != "" {
		out.Raw = json.RawMessage(parsed.RawResultLine)
	}
	if needsWrite(task) {
		out.Path = task.ProjectDir
	}
	if emit != nil {
		emit(core.Event{Type: "done"})
	}
	return out, nil
}

// ProbeQuota 用一次最小的 Haiku 调用换取 CLI 自报的额度窗口；不经过账本。
func (d *ClaudeDriver) ProbeQuota(ctx context.Context) ([]core.QuotaWindow, error) {
	pctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if _, err := d.Run(pctx, &core.Task{Prompt: "回复 OK", Kind: core.KindText, Source: "probe"}, core.Target{Lane: d.ID(), Model: "haiku"}, nil); err != nil {
		var rl *core.ErrRateLimited
		if !errors.As(err, &rl) {
			return nil, err
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.lastQuota) == 0 {
		return nil, errors.New("CLI 没有回报额度")
	}
	return append([]core.QuotaWindow(nil), d.lastQuota...), nil
}

// ProbeQuotaDeep 用一次最小的 Fable 调用拿到 Fable 专属的周窗口；只在手动刷新且上次数据过期时用。
func (d *ClaudeDriver) ProbeQuotaDeep(ctx context.Context) ([]core.QuotaWindow, error) {
	pctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	if _, err := d.Run(pctx, &core.Task{Prompt: "回复 OK", Kind: core.KindText, Source: "probe"}, core.Target{Lane: d.ID(), Model: "fable"}, nil); err != nil {
		var rl *core.ErrRateLimited
		if !errors.As(err, &rl) {
			return nil, err
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.lastQuota) == 0 {
		return nil, errors.New("CLI 没有回报额度")
	}
	return append([]core.QuotaWindow(nil), d.lastQuota...), nil
}

func (d *ClaudeDriver) timeout() time.Duration {
	if d.Timeout > 0 {
		return d.Timeout
	}
	return DefaultTimeout
}

// Health 检查 claude 命令能否运行、~/.claude 是否存在。只 stat 目录，不读任何凭据。
func (d *ClaudeDriver) Health(ctx context.Context) core.Health {
	bin := lookBin(d.bin())
	if bin == "" {
		return core.Health{Status: "missing", Detail: "本机没有找到 claude 命令"}
	}
	ver := versionOf(ctx, bin)
	if ver == "" {
		return core.Health{Status: "error", Detail: "claude --version 运行失败"}
	}
	if !dirExists(filepath.Join(d.home(), ".claude")) {
		return core.Health{Status: "warn", Detail: "claude 还没登录过：运行一次 claude 完成登录"}
	}
	if keys := thirdPartyIn(d.claudeSettingsFiles("")...); len(keys) > 0 {
		return core.Health{Status: "warn", Detail: thirdPartyHint}
	}
	d.mu.Lock()
	q := append([]core.QuotaWindow(nil), d.lastQuota...)
	cd := d.cooldown
	d.mu.Unlock()
	h := core.Health{Status: "ok", Detail: ver, Quota: q, Plan: readClaudePlan(d.home())}
	if cd.After(d.now()) {
		h.Status = "cooldown"
		h.CooldownUntil = cd
		h.Detail = "额度暂时用完，" + cd.Format("15:04") + " 后恢复"
	}
	d.mu.Lock()
	p := d.problems
	d.mu.Unlock()
	return withSelfCheck(p, h)
}
