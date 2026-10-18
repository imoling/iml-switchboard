package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

// AntigravityDriver 用本机的 Antigravity CLI（agy）消耗 Google AI 订阅额度。
// 只负责启动未修改的官方 CLI 并解析它的 stream-json 输出，不读取任何登录信息。
type AntigravityDriver struct {
	Bin     string
	Timeout time.Duration
	Now     func() time.Time
	// Prewarm 为真时，只读任务用完一个进程就在后台预热下一个（后台服务里打开）。
	Prewarm bool

	pool     *warmPool
	mu       sync.Mutex
	cooldown time.Time
}

func (d *AntigravityDriver) warm() *warmPool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Prewarm && d.pool == nil {
		// 闲置上限要短于 --print-timeout：agy 从启动就开始计时
		d.pool = newWarmPool(5*time.Minute, 1)
	}
	return d.pool
}

// Close 关掉预热的进程。
func (d *AntigravityDriver) Close() { d.warm().close() }

// AgyStdinPayload 是写进 agy 标准输入的一条消息。格式是试出来的：event 必填，message 要是对象。
func AgyStdinPayload(task *core.Task) string {
	b, _ := json.Marshal(map[string]any{"event": "user", "message": map[string]string{"role": "user", "content": buildPrompt(task)}})
	return string(b) + "\n"
}

// NewAntigravityDriver 构造驱动。
func NewAntigravityDriver() *AntigravityDriver { return &AntigravityDriver{} }

// ID 返回渠道标识。
func (d *AntigravityDriver) ID() core.LaneID { return core.LaneAgySub }

func (d *AntigravityDriver) bin() string {
	if d.Bin != "" {
		return d.Bin
	}
	return "agy"
}

func (d *AntigravityDriver) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *AntigravityDriver) timeout() time.Duration {
	if d.Timeout > 0 {
		return d.Timeout
	}
	return 10 * time.Minute
}

// BuildArgs 组装参数：纯文本任务用 plan 模式（不改文件），需要写文件的任务用 accept-edits 并指定工作目录。
func (d *AntigravityDriver) BuildArgs(task *core.Task, target core.Target) []string {
	// 提示词从标准输入传；-p 必须带一个值，否则 agy 会把下一个参数当成提示词
	args := []string{"-p", "", "--input-format", "stream-json", "--output-format", "stream-json", "--disable-slash-commands",
		"--print-timeout", d.timeout().String()}
	if target.Model != "" {
		args = append(args, "--model", target.Model)
	}
	if needsWrite(task) {
		args = append(args, "--mode", "accept-edits")
	} else {
		args = append(args, "--mode", "plan")
	}
	return args
}

type agyLine struct {
	Event      string `json:"event"`
	StepUpdate struct {
		StepType  string `json:"step_type"`
		State     string `json:"state"`
		TextDelta string `json:"text_delta"`
	} `json:"step_update"`
	Result struct {
		Status   string   `json:"status"`
		Response string   `json:"response"`
		Error    string   `json:"error"`
		Message  string   `json:"message"`
		Usage    agyUsage `json:"usage"`
	} `json:"result"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

type agyUsage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	ThinkingTokens  int `json:"thinking_tokens"`
	CacheReadTokens int `json:"cache_read_tokens"`
}

// AgyParsed 是一次运行解析出的结果。
type AgyParsed struct {
	Text      string
	Status    string
	ErrorText string
	Usage     core.Usage
	RawResult string
	SawResult bool
}

type agyParser struct {
	emit func(core.Event)
	p    AgyParsed
	text strings.Builder
}

func (a *agyParser) feed(line string) {
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "{") {
		return
	}
	var ev agyLine
	if json.Unmarshal([]byte(line), &ev) != nil {
		return
	}
	switch ev.Event {
	case "step_update":
		if ev.StepUpdate.StepType == "agent_response" && ev.StepUpdate.TextDelta != "" {
			a.text.WriteString(ev.StepUpdate.TextDelta)
			if a.emit != nil {
				a.emit(core.Event{Type: "text_delta", Text: ev.StepUpdate.TextDelta})
			}
		}
	case "result":
		a.p.SawResult = true
		a.p.RawResult = line
		a.p.Status = ev.Result.Status
		a.p.ErrorText = firstNonEmpty(ev.Result.Error, ev.Result.Message)
		if ev.Result.Response != "" {
			a.text.Reset()
			a.text.WriteString(ev.Result.Response)
		}
		u := ev.Result.Usage
		a.p.Usage = core.Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens + u.ThinkingTokens, CacheReadTokens: u.CacheReadTokens, CostUSD: 0}
	case "error":
		a.p.ErrorText = firstNonEmpty(ev.Error, ev.Message, line)
	}
}

func (a *agyParser) done() AgyParsed {
	a.p.Text = strings.TrimSpace(a.text.String())
	return a.p
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// Run 执行任务。
func (d *AntigravityDriver) Run(ctx context.Context, task *core.Task, target core.Target, emit func(core.Event)) (*core.Result, error) {
	bin := lookBin(d.bin())
	if bin == "" {
		return nil, &core.ErrProvider{Summary: "Antigravity 不可用：本机没有找到 agy 命令", Retryable: true}
	}
	parser := &agyParser{emit: emit}
	spec := procSpec{Bin: bin, Args: d.BuildArgs(task, target)}
	var pool *warmPool
	if needsWrite(task) {
		spec.Dir = task.ProjectDir
	} else {
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
	pool.refill(spec)
	if p != nil {
		// agy 读完输入、出了结果也不一定自己退出：看到 result 就收工
		res = p.finish(ctx, AgyStdinPayload(task), d.timeout()+30*time.Second, parser.feed, func() bool { return parser.p.SawResult })
	}
	parsed := parser.done()
	if res.Err != nil {
		return nil, &core.ErrProvider{Summary: "Antigravity 启动失败：" + firstLine(res.Err.Error()), Raw: res.Stderr, Retryable: true}
	}
	if res.TimedOut {
		return nil, &core.ErrProvider{Summary: "Antigravity 执行超时：超过 " + d.timeout().String() + " 没有返回", Raw: res.Stderr, Retryable: true}
	}
	if res.Canceled {
		return nil, ctx.Err()
	}
	failed := res.ExitCode != 0 || (parsed.SawResult && parsed.Status != "SUCCESS") || (!parsed.SawResult && parsed.Text == "")
	if failed {
		all := res.Stderr + "\n" + parsed.ErrorText + "\n" + parsed.Text
		if isRateLimitText(all) || strings.Contains(strings.ToLower(all), "resource_exhausted") || strings.Contains(strings.ToLower(all), "quota") {
			now := d.now()
			ra, _ := parseRetryAfter(all, now)
			d.mu.Lock()
			d.cooldown = now.Add(ra)
			d.mu.Unlock()
			return nil, &core.ErrRateLimited{Lane: d.ID(), RetryAfter: ra, Detail: firstLine(firstNonEmpty(parsed.ErrorText, res.Stderr))}
		}
		msg := firstLine(firstNonEmpty(parsed.ErrorText, res.Stderr, parsed.Text))
		if msg == "" {
			msg = fmt.Sprintf("退出码 %d", res.ExitCode)
		}
		return nil, &core.ErrProvider{Summary: "Antigravity 执行失败：" + msg, Raw: strings.TrimSpace(all + "\n" + parsed.RawResult), Retryable: true}
	}
	out := &core.Result{Text: parsed.Text, Model: target.Model, Usage: parsed.Usage}
	if parsed.RawResult != "" {
		out.Raw = json.RawMessage(parsed.RawResult)
	}
	if needsWrite(task) {
		out.Path = task.ProjectDir
	}
	if emit != nil {
		emit(core.Event{Type: "done"})
	}
	return out, nil
}

// Health：有 agy 命令且 Antigravity 的应用目录存在即可用；只 stat 目录，不读任何文件。
func (d *AntigravityDriver) Health(ctx context.Context) core.Health {
	bin := lookBin(d.bin())
	if bin == "" {
		return core.Health{Status: "missing", Detail: "本机没有安装 Antigravity：从 antigravity.google 下载后在设置里开启命令行工具"}
	}
	home, _ := os.UserHomeDir()
	if !dirExists(filepath.Join(home, "Library", "Application Support", "Antigravity")) {
		return core.Health{Status: "warn", Detail: "找到 agy 命令，但还没有登录 Google 账号：先在终端运行一次 agy"}
	}
	d.mu.Lock()
	cd := d.cooldown
	d.mu.Unlock()
	if cd.After(d.now()) {
		return core.Health{Status: "cooldown", Detail: "额度暂时用完：约 " + cd.Sub(d.now()).Round(time.Minute).String() + " 后恢复", CooldownUntil: cd}
	}
	return core.Health{Status: "ok", Detail: "Antigravity " + versionOf(ctx, bin)}
}
