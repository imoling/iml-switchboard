package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/lanes/api"
)

// AccountLookup 是按量付费账号表的只读视图；*api.APIDriver 满足它。
type AccountLookup interface {
	Account(id string) (api.Account, bool)
	AccountByProvider(provider string) (api.Account, bool)
	Accounts() []api.Account
}

// Options 是媒体驱动的依赖注入点。
type Options struct {
	// Jobs 是任务存储，必填。
	Jobs core.JobStore
	// OutDir 是产物目录解析器；nil 时用 DefaultOutDir。
	OutDir func(task *core.Task) (string, error)
	// Accounts 是按量付费账号表，用来取密钥；可为 nil（只剩 Codex 免费档）。
	Accounts AccountLookup
	// Codex 是 Codex 订阅驱动，用于免费档出图。
	Codex core.Driver
	// Claude 是 Claude 订阅驱动，用于动画写代码。
	Claude core.Driver
	// Client 是 HTTP 客户端。
	Client *http.Client
	// AnimRoot 是动画工程根目录；空串用 ~/Library/Application Support/iML Switchboard/animations。
	AnimRoot string
	// ImageProviders / VideoProviders 按供应商名注入，优先于按账号自动构造（测试用）。
	ImageProviders map[string]ImageProvider
	VideoProviders map[string]VideoProvider
	// 轮询与超时参数；零值用默认（5 秒起退避到 30 秒，10 分钟超时）。
	PollBase   time.Duration
	PollMax    time.Duration
	JobTimeout time.Duration
	// Exec 是动画步骤用的命令执行器；nil 用真实子进程。
	Exec CommandRunner
	// Now 便于测试注入时间。
	Now func() time.Time
}

// MediaDriver 实现 core.Driver，内部按 Task.Kind 分发到图片 / 视频 / 动画。
type MediaDriver struct {
	opt      Options
	remotion *Remotion

	mu      sync.Mutex
	waiters map[string]chan struct{}
	// active 记录正在后台跑的任务，供 Cancel 使用。
	active map[string]*activeJob
}

// activeJob 是一个后台任务的句柄。
type activeJob struct {
	cancel context.CancelFunc
	video  VideoProvider
	ref    string
}

// New 构造媒体驱动。
func New(opt Options) *MediaDriver {
	if opt.PollBase <= 0 {
		opt.PollBase = 5 * time.Second
	}
	if opt.PollMax <= 0 {
		opt.PollMax = 30 * time.Second
	}
	if opt.JobTimeout <= 0 {
		opt.JobTimeout = 10 * time.Minute
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.OutDir == nil {
		opt.OutDir = DefaultOutDir
	}
	d := &MediaDriver{opt: opt, waiters: map[string]chan struct{}{}, active: map[string]*activeJob{}}
	d.remotion = &Remotion{Claude: opt.Claude, Root: opt.AnimRoot, Exec: opt.Exec, Now: opt.Now}
	return d
}

// ID 返回渠道标识。
func (d *MediaDriver) ID() core.LaneID { return core.LaneMedia }

// DefaultOutDir 解析产物目录：Task.OutDir 优先，其次 ProjectDir/assets，最后应用支持目录下的 media/。
func DefaultOutDir(task *core.Task) (string, error) {
	switch {
	case task.OutDir != "":
		if filepath.IsAbs(task.OutDir) || task.ProjectDir == "" {
			return task.OutDir, nil
		}
		return filepath.Join(task.ProjectDir, task.OutDir), nil
	case task.ProjectDir != "":
		return filepath.Join(task.ProjectDir, "assets"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "iML Switchboard", "media"), nil
}

// Slug 从提示词取一小段做文件名。
func Slug(s string) string {
	var b strings.Builder
	n := 0
	lastDash := false
	for _, r := range s {
		if n >= 24 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r >= 0x4e00 && r <= 0x9fff:
			b.WriteRune(r)
			n++
			lastDash = false
		default:
			if n > 0 && !lastDash {
				b.WriteRune('-')
				n++
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "media"
	}
	return out
}

// artifactPath 生成 <yyyyMMdd-HHmmss>-<slug>.<ext> 形式的产物路径。
func (d *MediaDriver) artifactPath(task *core.Task, ext string) (string, error) {
	dir, err := d.opt.OutDir(task)
	if err != nil {
		return "", &core.ErrProvider{Summary: "无法确定产物目录", Raw: err.Error()}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", &core.ErrProvider{Summary: "无法创建产物目录：" + dir, Raw: err.Error()}
	}
	name := d.opt.Now().Format("20060102-150405") + "-" + Slug(task.Prompt) + ext
	p := filepath.Join(dir, name)
	// 同秒重名时加短随机后缀
	if _, err := os.Stat(p); err == nil {
		p = filepath.Join(dir, strings.TrimSuffix(name, ext)+"-"+newID()[:4]+ext)
	}
	return p, nil
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// resolveAccount 按 target 找按量付费账号。
func (d *MediaDriver) resolveAccount(target core.Target) (api.Account, bool) {
	if d.opt.Accounts == nil {
		return api.Account{}, false
	}
	if target.AccountID != "" {
		if a, ok := d.opt.Accounts.Account(target.AccountID); ok {
			return a, true
		}
	}
	if target.Provider != "" {
		if a, ok := d.opt.Accounts.AccountByProvider(target.Provider); ok {
			return a, true
		}
	}
	return api.Account{}, false
}

// imageProvider 按 target 选图片供应商。
func (d *MediaDriver) imageProvider(target core.Target) (ImageProvider, error) {
	if p, ok := d.opt.ImageProviders[target.Provider]; ok {
		return p, nil
	}
	if target.Provider == "codex" || target.Model == "codex-imagegen" {
		return &CodexImage{Codex: d.opt.Codex}, nil
	}
	acct, ok := d.resolveAccount(target)
	if !ok {
		return nil, &core.ErrProvider{Summary: "没有可用的 " + target.Provider + " 账号：在设置里添加密钥", Retryable: true}
	}
	switch strings.ToLower(acct.Provider) {
	case "openrouter":
		return &OpenRouterImage{Account: acct, Client: d.opt.Client}, nil
	case "ark", "volcengine", "doubao":
		return &ArkImage{Account: acct, Client: d.opt.Client}, nil
	default:
		return &OpenAICompatImage{Account: acct, Client: d.opt.Client}, nil
	}
}

// videoProvider 按 target 选视频供应商。
func (d *MediaDriver) videoProvider(target core.Target) (VideoProvider, error) {
	if p, ok := d.opt.VideoProviders[target.Provider]; ok {
		return p, nil
	}
	acct, ok := d.resolveAccount(target)
	if !ok {
		return nil, &core.ErrProvider{Summary: "没有可用的 " + target.Provider + " 账号：在设置里添加密钥", Retryable: true}
	}
	switch strings.ToLower(acct.Provider) {
	case "openrouter":
		return &OpenRouterVideo{Account: acct, Client: d.opt.Client}, nil
	case "ark", "volcengine", "doubao":
		return &ArkVideo{Account: acct, Client: d.opt.Client}, nil
	default:
		return &OpenAICompatVideo{Account: acct, Client: d.opt.Client}, nil
	}
}

// Run 执行任务：图片同步返回；视频、动画走异步任务，ctx 到期前完成返回 Path，否则返回 JobID。
func (d *MediaDriver) Run(ctx context.Context, task *core.Task, target core.Target, emit func(core.Event)) (*core.Result, error) {
	switch task.Kind {
	case core.KindImage:
		return d.runImage(ctx, task, target, emit)
	case core.KindVideo:
		return d.runVideo(ctx, task, target, emit)
	case core.KindAnimation:
		return d.runAnimation(ctx, task, target, emit)
	}
	return nil, &core.ErrProvider{Summary: "媒体渠道不处理这种任务：" + string(task.Kind)}
}

func (d *MediaDriver) runImage(ctx context.Context, task *core.Task, target core.Target, emit func(core.Event)) (*core.Result, error) {
	p, err := d.imageProvider(target)
	if err != nil {
		return nil, err
	}
	quality := qualityFor(task, target)
	res, err := p.Generate(ctx, ImageRequest{Prompt: task.Prompt, Model: target.Model, Size: task.Size, Quality: quality, ImagePath: task.ImagePath})
	if err != nil {
		return nil, err
	}
	path, err := d.artifactPath(task, extForMime(res.Mime, res.URL))
	if err != nil {
		return nil, err
	}
	switch {
	case len(res.Data) > 0:
		if err := os.WriteFile(path, res.Data, 0o644); err != nil {
			return nil, &core.ErrProvider{Summary: "写入图片失败：" + path, Raw: err.Error()}
		}
	case res.URL != "":
		if err := download(ctx, d.opt.Client, p.Name(), res.URL, path, nil); err != nil {
			return nil, err
		}
	default:
		return nil, &core.ErrProvider{Summary: p.Name() + " 出图失败：没有返回图片", Retryable: true}
	}
	out := &core.Result{Path: path, Model: target.Model, Usage: UsageFor(target.Model, quality, 1), Raw: res.Raw}
	if emit != nil {
		emit(core.Event{Type: "media", Path: path})
		emit(core.Event{Type: "done"})
	}
	return out, nil
}

// qualityFor 按媒体档位推断 gpt-image 的 quality。
func qualityFor(task *core.Task, target core.Target) string {
	n := normalizeModel(target.Model)
	if !strings.Contains(n, "gpt.image") {
		return ""
	}
	switch {
	case strings.HasSuffix(n, ".low"):
		return "low"
	case strings.HasSuffix(n, ".high"):
		return "high"
	case strings.HasSuffix(n, ".medium"):
		return "medium"
	}
	switch task.MediaTier {
	case "cheap", "free":
		return "low"
	case "best":
		return "high"
	}
	return "medium"
}

func extForMime(m, url string) string {
	switch {
	case strings.Contains(m, "jpeg"):
		return ".jpg"
	case strings.Contains(m, "webp"):
		return ".webp"
	case strings.Contains(m, "png"):
		return ".png"
	}
	l := strings.ToLower(url)
	for _, e := range []string{".jpg", ".jpeg", ".webp", ".png"} {
		if strings.Contains(l, e) {
			if e == ".jpeg" {
				return ".jpg"
			}
			return e
		}
	}
	return ".png"
}

func (d *MediaDriver) runVideo(ctx context.Context, task *core.Task, target core.Target, emit func(core.Event)) (*core.Result, error) {
	job, done, err := d.startVideo(task, target, emit)
	if err != nil {
		return nil, err
	}
	return d.waitJob(ctx, job.ID, done, emit)
}

// startVideo 登记并在后台启动视频任务，立即返回。
func (d *MediaDriver) startVideo(task *core.Task, target core.Target, emit func(core.Event)) (*core.Job, chan struct{}, error) {
	if d.opt.Jobs == nil {
		return nil, nil, &core.ErrProvider{Summary: "视频任务不可用：没有接入任务存储"}
	}
	vp, err := d.videoProvider(target)
	if err != nil {
		return nil, nil, err
	}
	path, err := d.artifactPath(task, ".mp4")
	if err != nil {
		return nil, nil, err
	}
	job := d.newJob(task, target)
	if err := d.opt.Jobs.Put(job); err != nil {
		return nil, nil, &core.ErrProvider{Summary: "无法登记视频任务", Raw: err.Error()}
	}
	done := d.waiter(job.ID)
	go d.runVideoJob(job, vp, task, target, path, emit, done)
	return job, done, nil
}

func (d *MediaDriver) runAnimation(ctx context.Context, task *core.Task, target core.Target, emit func(core.Event)) (*core.Result, error) {
	job, done, err := d.startAnimation(task, target, emit)
	if err != nil {
		return nil, err
	}
	return d.waitJob(ctx, job.ID, done, emit)
}

// startAnimation 登记并在后台启动动画任务，立即返回。
func (d *MediaDriver) startAnimation(task *core.Task, target core.Target, emit func(core.Event)) (*core.Job, chan struct{}, error) {
	if d.opt.Jobs == nil {
		return nil, nil, &core.ErrProvider{Summary: "动画任务不可用：没有接入任务存储"}
	}
	if err := d.remotion.Precheck(); err != nil {
		return nil, nil, err
	}
	path, err := d.artifactPath(task, ".mp4")
	if err != nil {
		return nil, nil, err
	}
	job := d.newJob(task, target)
	if job.Provider == "" {
		job.Provider = "remotion"
	}
	if err := d.opt.Jobs.Put(job); err != nil {
		return nil, nil, &core.ErrProvider{Summary: "无法登记动画任务", Raw: err.Error()}
	}
	done := d.waiter(job.ID)
	go d.runAnimationJob(job, task, path, "", emit, done)
	return job, done, nil
}

// Cancel 取消一个进行中的任务：停止后台轮询、调供应商 Cancel（如有），状态改为 canceled。
func (d *MediaDriver) Cancel(ctx context.Context, jobID string) (*core.Job, error) {
	job, ok := d.opt.Jobs.Get(jobID)
	if !ok {
		return nil, &core.ErrProvider{Summary: "找不到任务：" + jobID}
	}
	switch job.Status {
	case "done", "failed", "canceled", "timeout":
		return nil, &core.ErrProvider{Summary: "任务已经结束，不能取消：" + job.Status}
	}
	d.mu.Lock()
	a := d.active[jobID]
	d.mu.Unlock()
	var provErr error
	if a != nil {
		a.cancel()
		if a.video != nil && a.ref != "" {
			provErr = a.video.Cancel(ctx, a.ref)
		}
	}
	now := d.opt.Now()
	updated, err := d.opt.Jobs.Update(jobID, func(j *core.Job) {
		j.Status = "canceled"
		j.Error = "已取消"
		if provErr != nil {
			j.Error = "已取消；供应商侧取消失败：" + firstLine(provErr.Error())
		}
		j.UpdatedAt = now
	})
	if err != nil {
		return nil, &core.ErrProvider{Summary: "更新任务状态失败", Raw: err.Error()}
	}
	return updated, nil
}

// Retry 用同一个 Task 换模型重跑，返回新任务。model 为空时取 job.RetryOptions[0]。
func (d *MediaDriver) Retry(ctx context.Context, jobID string, model string) (*core.Job, error) {
	job, ok := d.opt.Jobs.Get(jobID)
	if !ok {
		return nil, &core.ErrProvider{Summary: "找不到任务：" + jobID}
	}
	if job.Task == nil {
		return nil, &core.ErrProvider{Summary: "这个任务的原始请求已丢失（服务重启过），请重新发起"}
	}
	target := core.Target{Lane: core.LaneMedia, Provider: job.Provider, Model: model}
	if model == "" {
		if len(job.RetryOptions) == 0 {
			return nil, &core.ErrProvider{Summary: "没有可换的模型：指定一个模型再试"}
		}
		target = job.RetryOptions[0]
	} else {
		for _, opt := range job.RetryOptions {
			if opt.Model == model {
				target = opt
				break
			}
		}
	}
	task := *job.Task
	task.ID = ""
	task.CreatedAt = d.opt.Now()
	var newJob *core.Job
	var err error
	switch job.Kind {
	case core.KindVideo:
		newJob, _, err = d.startVideo(&task, target, nil)
	case core.KindAnimation:
		newJob, _, err = d.startAnimation(&task, target, nil)
	default:
		return nil, &core.ErrProvider{Summary: "只有视频和动画任务可以重试"}
	}
	if err != nil {
		return nil, err
	}
	return newJob, nil
}

// ReviseAnimation 在已有动画工程目录上按新指令再跑一轮改代码与渲染。
func (d *MediaDriver) ReviseAnimation(ctx context.Context, jobID, instruction string, emit func(core.Event)) (*core.Result, error) {
	job, ok := d.opt.Jobs.Get(jobID)
	if !ok {
		return nil, &core.ErrProvider{Summary: "找不到动画任务：" + jobID}
	}
	if job.Kind != core.KindAnimation {
		return nil, &core.ErrProvider{Summary: "这个任务不是动画，不能修改：" + jobID}
	}
	if job.Task == nil || job.CodeDir == "" {
		return nil, &core.ErrProvider{Summary: "这个动画的原始请求已丢失（服务重启过），请重新生成"}
	}
	if job.Status == "running" || job.Status == "submitted" {
		return nil, &core.ErrProvider{Summary: "动画还在生成中，等它完成再修改", Retryable: true}
	}
	path, err := d.artifactPath(job.Task, ".mp4")
	if err != nil {
		return nil, err
	}
	now := d.opt.Now()
	job, _ = d.opt.Jobs.Update(jobID, func(j *core.Job) {
		j.Status = "queued"
		j.Progress = 0
		j.Error = ""
		j.ResultPath = ""
		j.UpdatedAt = now
	})
	done := d.waiter(job.ID)
	go d.runAnimationJob(job, job.Task, path, instruction, emit, done)
	return d.waitJob(ctx, job.ID, done, emit)
}

func (d *MediaDriver) newJob(task *core.Task, target core.Target) *core.Job {
	now := d.opt.Now()
	excerpt := task.Prompt
	if r := []rune(excerpt); len(r) > 40 {
		excerpt = string(r[:40]) + "…"
	}
	id := task.ID
	if id == "" {
		id = "job_" + newID()
	} else {
		id = "job_" + id
	}
	return &core.Job{ID: id, Kind: task.Kind, Status: "queued", Provider: target.Provider, Model: target.Model,
		PromptExcerpt: excerpt, CallID: task.ID, CreatedAt: now, UpdatedAt: now, Task: task}
}

// Health 报告有哪些供应商可用，不发任何请求。
func (d *MediaDriver) Health(ctx context.Context) core.Health {
	var avail []string
	if d.opt.Codex != nil {
		avail = append(avail, "codex 免费出图")
	}
	if d.opt.Accounts != nil {
		for _, a := range d.opt.Accounts.Accounts() {
			if _, err := a.APIKey(); err == nil {
				avail = append(avail, a.Provider)
			}
		}
	}
	for name := range d.opt.ImageProviders {
		avail = append(avail, name)
	}
	if d.opt.Claude != nil {
		if err := d.remotion.Precheck(); err == nil {
			avail = append(avail, "remotion 动画")
		}
	}
	if len(avail) == 0 {
		return core.Health{Status: "disabled", Detail: "没有可用的媒体供应商：添加按量付费密钥或登录 Codex"}
	}
	return core.Health{Status: "ok", Detail: strings.Join(avail, "、")}
}

// classifyJobError 把供应商错误分成可重试与不可重试，并返回一行人话。
func classifyJobError(err error) (msg string, retryable bool) {
	if err == nil {
		return "", false
	}
	if api.IsModerationError(err) {
		return "内容未通过审核：换个描述再试", false
	}
	var pe *core.ErrProvider
	if errors.As(err, &pe) {
		return pe.Summary, pe.Retryable
	}
	var rl *core.ErrRateLimited
	if errors.As(err, &rl) {
		return rl.Error(), true
	}
	return firstLine(err.Error()), true
}
