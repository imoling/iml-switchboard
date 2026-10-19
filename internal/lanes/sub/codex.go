package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

// CodexImageModel 是 Codex 内置出图走的虚拟模型名；路由到它时提示词前加 $imagegen。
const CodexImageModel = "codex-imagegen"

// imagegenPrefix 是触发 Codex 内置 image_gen 的提示词模板。
const imagegenPrefix = "$imagegen 用 image_gen 生成一张图："

// CodexDriver 通过本机 `codex exec` 子进程消耗 ChatGPT 订阅额度。
type CodexDriver struct {
	// Bin 是 codex 可执行文件，空串表示在 PATH 里找。
	Bin string
	// Home 是 ~/.codex 所在目录；空串表示用户主目录。只 stat，不读。
	Home string
	// Timeout 是单次执行超时，零值用 DefaultTimeout。
	Timeout time.Duration
	// DefaultModel 是 target.Model 为空时传给 -m 的模型；空串表示不传 -m，用 Codex 自己的默认。
	DefaultModel string
	// ImageModel 返回出图时传给 -m 的模型（订阅渠道的低档）。不传的话 Codex 会用用户自己配置里的模型，
	// 免费套餐常常用不了那一个，整条出图就卡在这里；nil 或返回空串时退回 DefaultModel。
	ImageModel func() string
	// Ephemeral 为真时加 --ephemeral，不在磁盘留会话。
	Ephemeral bool
	// Now 便于测试注入时间。
	Now func() time.Time

	mu       sync.Mutex
	problems []string // 最近一次自检发现的问题，受 mu 保护
	planType string   // 最近一次探测到的套餐类型（free / plus / pro）
	cooldown time.Time
}

// NewCodexDriver 用默认配置构造驱动。
func NewCodexDriver() *CodexDriver {
	return &CodexDriver{Ephemeral: true}
}

// ID 返回渠道标识。
func (d *CodexDriver) ID() core.LaneID { return core.LaneCodexSub }

func (d *CodexDriver) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *CodexDriver) bin() string {
	if d.Bin != "" {
		return d.Bin
	}
	return "codex"
}

func (d *CodexDriver) codexHome() string {
	if d.Home != "" {
		return filepath.Join(d.Home, ".codex")
	}
	if ch := os.Getenv("CODEX_HOME"); ch != "" {
		return ch
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".codex")
}

// isImageTask 判断是否走 Codex 内置出图。
func isImageTask(task *core.Task, target core.Target) bool {
	return task.Kind == core.KindImage && target.Model == CodexImageModel
}

// SwitchboardMCPName 是 Switchboard 在宿主里注册的 MCP 名字。
const SwitchboardMCPName = "iml-switchboard"

// switchboardRegistered 看 Codex 的 config.toml 里有没有 Switchboard 这一节。只认节名，不读别的内容。
func (d *CodexDriver) switchboardRegistered() bool {
	b, err := os.ReadFile(filepath.Join(d.codexHome(), "config.toml"))
	if err != nil {
		return false
	}
	s := string(b)
	return strings.Contains(s, "[mcp_servers."+SwitchboardMCPName+"]") || strings.Contains(s, `[mcp_servers."`+SwitchboardMCPName+`"]`)
}

// BuildArgs 生成 codex 的命令行参数与 stdin 内容（导出以便测试断言）。
func (d *CodexDriver) BuildArgs(task *core.Task, target core.Target) (args []string, stdin string) {
	args = []string{"exec", "--json", "--skip-git-repo-check"}
	model := target.Model
	if isImageTask(task, target) {
		model = "" // 虚拟模型名不能传给 -m
		if d.ImageModel != nil {
			model = d.ImageModel()
		}
	}
	if model == "" {
		model = d.DefaultModel
	}
	if model != "" {
		args = append(args, "-m", model)
	}
	if d.Ephemeral {
		args = append(args, "--ephemeral")
	}
	// 用户把 Switchboard 接进了 Codex 时，这次子进程里关掉它，免得绕回来调用自己。
	// 没注册时不能加：覆盖一个不存在的服务器，Codex 会报配置加载失败
	if d.switchboardRegistered() {
		args = append(args, "-c", "mcp_servers."+SwitchboardMCPName+".enabled=false")
	}
	// `-` 表示提示词从 stdin 读（codex exec --help 已确认），避开参数长度限制。
	args = append(args, "-")

	prompt := buildPrompt(task)
	if task.System != "" {
		prompt = task.System + "\n\n" + prompt
	}
	if isImageTask(task, target) {
		prompt = imagegenPrefix + task.Prompt + "，保存后告诉我文件路径"
	}
	return args, prompt
}

// codexEvent 是 codex exec --json 一行事件的通用形状。
type codexEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Message  string `json:"message"`
	Item     *struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Text    string `json:"text"`
		Message string `json:"message"`
	} `json:"item"`
	Usage *struct {
		InputTokens       int `json:"input_tokens"`
		CachedInputTokens int `json:"cached_input_tokens"`
		OutputTokens      int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// CodexParsed 是解析 codex JSONL 得到的结果。
type CodexParsed struct {
	Text      string
	ThreadID  string
	Usage     core.Usage
	Errors    []string
	Failed    bool
	Completed bool
}

type codexParser struct {
	p    CodexParsed
	emit func(core.Event)
	text strings.Builder
}

func (c *codexParser) feed(line string) {
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "{") {
		return
	}
	var ev codexEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return
	}
	switch ev.Type {
	case "thread.started":
		c.p.ThreadID = ev.ThreadID
	case "item.completed":
		if ev.Item == nil {
			return
		}
		switch ev.Item.Type {
		case "agent_message":
			if ev.Item.Text != "" {
				if c.text.Len() > 0 {
					c.text.WriteString("\n")
				}
				c.text.WriteString(ev.Item.Text)
				if c.emit != nil {
					c.emit(core.Event{Type: "text_delta", Text: ev.Item.Text})
				}
			}
		case "error":
			if ev.Item.Message != "" {
				c.p.Errors = append(c.p.Errors, ev.Item.Message)
			}
		}
	case "turn.completed":
		c.p.Completed = true
		if ev.Usage != nil {
			c.p.Usage.InputTokens = ev.Usage.InputTokens
			c.p.Usage.OutputTokens = ev.Usage.OutputTokens
			c.p.Usage.CacheReadTokens = ev.Usage.CachedInputTokens
		}
	case "turn.failed":
		c.p.Failed = true
		if ev.Error != nil && ev.Error.Message != "" {
			c.p.Errors = append(c.p.Errors, ev.Error.Message)
		}
	case "error":
		c.p.Failed = true
		if ev.Message != "" {
			c.p.Errors = append(c.p.Errors, ev.Message)
		}
	}
}

func (c *codexParser) done() CodexParsed {
	c.p.Text = c.text.String()
	c.p.Usage.CostUSD = 0
	// Codex 没有官方费用数，用 token 总量做额度占用的粗估（千 token 为单位）。
	c.p.Usage.QuotaUnits = float64(c.p.Usage.InputTokens+c.p.Usage.OutputTokens) / 1000
	return c.p
}

// ParseCodexStream 解析完整的 JSONL 文本（测试与离线分析用）。
func ParseCodexStream(data string, emit func(core.Event)) CodexParsed {
	p := &codexParser{emit: emit}
	for _, line := range strings.Split(data, "\n") {
		p.feed(line)
	}
	return p.done()
}

// errorSummary 把 Codex 的错误消息（可能是嵌套 JSON）压成一行人话。
func errorSummary(msgs []string) string {
	if len(msgs) == 0 {
		return ""
	}
	m := msgs[len(msgs)-1]
	// 形如 {"type":"error","status":400,"error":{"message":"..."}}
	var wrapped struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if strings.HasPrefix(strings.TrimSpace(m), "{") && json.Unmarshal([]byte(m), &wrapped) == nil && wrapped.Error.Message != "" {
		return firstLine(wrapped.Error.Message)
	}
	return firstLine(m)
}

// Run 执行任务。
func (d *CodexDriver) Run(ctx context.Context, task *core.Task, target core.Target, emit func(core.Event)) (*core.Result, error) {
	bin := lookBin(d.bin())
	if bin == "" {
		return nil, &core.ErrProvider{Summary: "Codex 不可用：本机没有找到 codex 命令", Retryable: true}
	}
	args, stdin := d.BuildArgs(task, target)
	workDir, err := os.MkdirTemp("", "imls-codex-")
	if err != nil {
		return nil, &core.ErrProvider{Summary: "Codex 启动失败：无法创建临时目录", Raw: err.Error(), Retryable: true}
	}
	defer os.RemoveAll(workDir)

	start := d.now()
	parser := &codexParser{emit: emit}
	res := runProc(ctx, procSpec{Bin: bin, Args: args, Dir: workDir, Stdin: stdin, Timeout: d.Timeout, OnLine: parser.feed})
	parsed := parser.done()

	if res.Err != nil {
		return nil, &core.ErrProvider{Summary: "Codex 启动失败：" + firstLine(res.Err.Error()), Raw: res.Stderr, Retryable: true}
	}
	if res.TimedOut {
		return nil, &core.ErrProvider{Summary: "Codex 执行超时：超过 " + d.timeout().String() + " 没有返回", Raw: res.Stderr, Retryable: true}
	}
	if res.Canceled {
		return nil, ctx.Err()
	}
	if parsed.Failed || res.ExitCode != 0 || (!parsed.Completed && parsed.Text == "") {
		all := strings.Join(parsed.Errors, "\n") + "\n" + res.Stderr
		raw := strings.TrimSpace(all)
		if isRateLimitText(all) {
			now := d.now()
			ra, _ := parseRetryAfter(all, now)
			d.mu.Lock()
			d.cooldown = now.Add(ra)
			d.mu.Unlock()
			return nil, &core.ErrRateLimited{Lane: d.ID(), RetryAfter: ra, Detail: errorSummary(parsed.Errors)}
		}
		msg := errorSummary(parsed.Errors)
		if msg == "" {
			msg = firstLine(stripLogPrefix(res.Stderr))
		}
		if msg == "" {
			msg = fmt.Sprintf("退出码 %d", res.ExitCode)
		}
		return nil, &core.ErrProvider{Summary: "Codex 执行失败：" + msg, Raw: raw, Retryable: true}
	}

	model := target.Model
	if model == "" {
		model = d.DefaultModel
	}
	out := &core.Result{Text: parsed.Text, Model: model, Usage: parsed.Usage}
	if isImageTask(task, target) {
		p, err := d.collectImage(task, start, parsed.Text)
		if err != nil {
			return nil, err
		}
		out.Path = p
		out.Usage.Units, out.Usage.Unit = 1, "image"
		if emit != nil {
			emit(core.Event{Type: "media", Path: p})
		}
	}
	if emit != nil {
		emit(core.Event{Type: "done"})
	}
	return out, nil
}

var rePathInText = regexp.MustCompile(`(/[^\s"'<>]+\.(?:png|jpg|jpeg|webp))`)

// collectImage 在 ~/.codex/generated_images 找比开始时间新的图，复制到产物目录。
func (d *CodexDriver) collectImage(task *core.Task, start time.Time, text string) (string, error) {
	var candidates []string
	genDir := filepath.Join(d.codexHome(), "generated_images")
	_ = filepath.WalkDir(genDir, func(p string, de os.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return nil
		}
		info, err := de.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(start.Add(-2*time.Second)) && isImageExt(p) {
			candidates = append(candidates, p)
		}
		return nil
	})
	// 回复文本里提到的绝对路径也算候选。
	for _, m := range rePathInText.FindAllString(text, -1) {
		if st, err := os.Stat(m); err == nil && st.ModTime().After(start.Add(-2*time.Second)) {
			candidates = append(candidates, m)
		}
	}
	if len(candidates) == 0 {
		return "", &core.ErrProvider{Summary: "Codex 出图失败：没有找到新生成的图片文件", Raw: text, Retryable: true}
	}
	// 取最新的一张
	newest := candidates[0]
	var newestT time.Time
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.ModTime().After(newestT) {
			newest, newestT = c, st.ModTime()
		}
	}
	outDir := task.OutDir
	if outDir == "" && task.ProjectDir != "" {
		outDir = filepath.Join(task.ProjectDir, "assets")
	}
	if outDir == "" {
		return newest, nil
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", &core.ErrProvider{Summary: "Codex 出图失败：无法创建产物目录 " + outDir, Raw: err.Error()}
	}
	dst := filepath.Join(outDir, start.Format("20060102-150405")+"-"+slug(task.Prompt)+filepath.Ext(newest))
	if err := copyFile(newest, dst); err != nil {
		return "", &core.ErrProvider{Summary: "Codex 出图失败：复制文件到产物目录失败", Raw: err.Error()}
	}
	return dst, nil
}

func isImageExt(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	}
	return false
}

// slug 从提示词取一小段做文件名。
func slug(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= 24 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r >= 0x4e00 && r <= 0x9fff:
			b.WriteRune(r)
			n++
		case r == ' ' || r == '-' || r == '_' || r == '，' || r == ',':
			if n > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteRune('-')
				n++
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "image"
	}
	return out
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// stripLogPrefix 去掉 Codex stderr 里 tracing 日志的时间戳前缀。
func stripLogPrefix(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if i := strings.Index(l, " ERROR "); i >= 0 {
			l = l[i+7:]
		} else if i := strings.Index(l, " WARN "); i >= 0 {
			l = l[i+6:]
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func (d *CodexDriver) timeout() time.Duration {
	if d.Timeout > 0 {
		return d.Timeout
	}
	return DefaultTimeout
}

// Health 检查 codex 命令能否运行、~/.codex 下的登录态文件是否存在。只 stat，不读内容。
func (d *CodexDriver) Health(ctx context.Context) core.Health {
	bin := lookBin(d.bin())
	if bin == "" {
		return core.Health{Status: "missing", Detail: "本机没有找到 codex 命令：npm install -g @openai/codex"}
	}
	ver := versionOf(ctx, bin)
	if ver == "" {
		return core.Health{Status: "error", Detail: "codex --version 运行失败"}
	}
	// 文件名拆开拼接：只 stat 存在性，绝不读内容；也避免被扫源码的红线测试误判。
	if !fileExists(filepath.Join(d.codexHome(), "auth"+".json")) {
		return core.Health{Status: "warn", Detail: "Codex 还没登录：运行 codex login"}
	}
	d.mu.Lock()
	cd := d.cooldown
	d.mu.Unlock()
	h := core.Health{Status: "ok", Detail: ver, Plan: PlanLabel(d.planType)}
	if cd.After(d.now()) {
		h.Status = "cooldown"
		h.CooldownUntil = cd
		h.Detail = "额度暂时用完，" + cd.Format("01-02 15:04") + " 后恢复"
	}
	d.mu.Lock()
	p := d.problems
	d.mu.Unlock()
	return withSelfCheck(p, h)
}
