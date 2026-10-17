package media

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

//go:embed all:remotion_template
var remotionTemplate embed.FS

// CommandRunner 执行一条外部命令，返回 stdout / stderr。注入它可以在测试里替换 npm 与 remotion。
type CommandRunner func(ctx context.Context, dir string, env []string, name string, args ...string) (stdout, stderr string, err error)

// proxyEnv 是 npm install 前要删掉的代理变量（本机代理会把下载卡到几十 KB/s）。
var proxyEnv = []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"}

// defaultRunner 用真实子进程执行命令。
func defaultRunner(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	return out.String(), errb.String(), err
}

// Remotion 负责 make_animation：复制模板工程、让 Claude 改代码、npm 安装、渲染、失败回喂修复。
type Remotion struct {
	// Claude 是写代码用的订阅驱动。
	Claude core.Driver
	// Root 是所有动画工程的根目录。
	Root string
	// Exec 是命令执行器；nil 用真实子进程。
	Exec CommandRunner
	// LookPath 查找可执行文件；nil 用 exec.LookPath。
	LookPath func(string) (string, error)
	// MaxFixRounds 是渲染失败后让 Claude 修复的最多轮数，零值为 2。
	MaxFixRounds int
	// Now 便于测试注入时间。
	Now func() time.Time
}

func (r *Remotion) exec() CommandRunner {
	if r.Exec != nil {
		return r.Exec
	}
	return defaultRunner
}

func (r *Remotion) lookPath(name string) (string, error) {
	if r.LookPath != nil {
		return r.LookPath(name)
	}
	return exec.LookPath(name)
}

func (r *Remotion) root() (string, error) {
	if r.Root != "" {
		return r.Root, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "iML Switchboard", "animations"), nil
}

func (r *Remotion) fixRounds() int {
	if r.MaxFixRounds > 0 {
		return r.MaxFixRounds
	}
	return 2
}

// Precheck 检查本机依赖：Node 与 Claude 驱动。Node 不存在时返回不可重试的错误。
func (r *Remotion) Precheck() error {
	if _, err := r.lookPath("node"); err != nil {
		return &core.ErrProvider{Summary: "动画需要本机安装 Node：装好 node 与 npm 后重试", Raw: err.Error(), Retryable: false}
	}
	if _, err := r.lookPath("npm"); err != nil {
		return &core.ErrProvider{Summary: "动画需要本机安装 Node：找不到 npm", Raw: err.Error(), Retryable: false}
	}
	if r.Claude == nil {
		return &core.ErrProvider{Summary: "动画需要 Claude 渠道写代码：先登录 claude", Retryable: false}
	}
	return nil
}

// Prepare 把模板工程复制到 <root>/<jobID>/ 并返回目录；目录已存在时不覆盖已有文件。
func (r *Remotion) Prepare(jobID string) (string, error) {
	root, err := r.root()
	if err != nil {
		return "", &core.ErrProvider{Summary: "无法确定动画工程目录", Raw: err.Error()}
	}
	dir := filepath.Join(root, jobID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", &core.ErrProvider{Summary: "无法创建动画工程目录：" + dir, Raw: err.Error()}
	}
	err = fs.WalkDir(remotionTemplate, "remotion_template", func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("remotion_template", p)
		if rel == "." {
			return nil
		}
		dst := filepath.Join(dir, rel)
		if de.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if _, err := os.Stat(dst); err == nil {
			return nil
		}
		b, err := remotionTemplate.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		return "", &core.ErrProvider{Summary: "复制动画模板失败", Raw: err.Error()}
	}
	return dir, nil
}

// codePrompt 生成让 Claude 改 src/Main.tsx 的提示词。
func codePrompt(spec string, seconds int) string {
	return fmt.Sprintf(`这是一个 Remotion 工程。根据下面的描述修改 src/Main.tsx，做成一段动画。

要求：
- 时长 %d 秒、30fps、1920x1080；把 DURATION_SECONDS 改成 %d
- 只改 src/Main.tsx，不要动其它文件，不要新增依赖
- 保持具名导出 Main 与 DURATION_SECONDS
- 只用 remotion、react 自带的能力（AbsoluteFill、interpolate、spring、Sequence 等），不用外部字体和网络资源
- 改完不用解释，简短回复「已修改」

描述：
%s`, seconds, seconds, spec)
}

// fixPrompt 生成渲染失败后的修复提示词。
func fixPrompt(stderr string) string {
	if len(stderr) > 6000 {
		stderr = stderr[len(stderr)-6000:]
	}
	return "刚才 `npx remotion render Main out.mp4` 失败了，错误输出如下。请只修改 src/Main.tsx 修好它，不要动其它文件，改完简短回复「已修复」。\n\n```\n" + stderr + "\n```"
}

// revisePrompt 生成修改指令的提示词。
func revisePrompt(instruction string, seconds int) string {
	return fmt.Sprintf("在现有 src/Main.tsx 的基础上按下面的指令修改动画。时长仍为 %d 秒、30fps、1920x1080；只改 src/Main.tsx；改完简短回复「已修改」。\n\n指令：\n%s", seconds, instruction)
}

// askClaude 让 Claude 驱动在工程目录里改代码。
func (r *Remotion) askClaude(ctx context.Context, dir, prompt string) error {
	task := &core.Task{Kind: core.KindAnimation, Engine: "remotion", ProjectDir: dir, Prompt: prompt, Source: "media", CreatedAt: time.Now()}
	_, err := r.Claude.Run(ctx, task, core.Target{Lane: core.LaneClaudeSub, Provider: "claude", Model: "sonnet"}, nil)
	return err
}

func (r *Remotion) npmInstall(ctx context.Context, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "remotion")); err == nil {
		return nil
	}
	env := stripEnv(os.Environ(), proxyEnv)
	_, stderr, err := r.exec()(ctx, dir, env, "npm", "install", "--no-audit", "--no-fund", "--loglevel=error")
	if err != nil {
		return &core.ErrProvider{Summary: "动画依赖安装失败：npm install 出错", Raw: stderr + "\n" + err.Error(), Retryable: true}
	}
	return nil
}

func (r *Remotion) render(ctx context.Context, dir string) (string, error) {
	_, stderr, err := r.exec()(ctx, dir, os.Environ(), "npx", "remotion", "render", "Main", "out.mp4")
	if err != nil {
		return stderr + "\n" + err.Error(), err
	}
	return "", nil
}

// Build 走完整流程：复制模板 → Claude 改代码 → npm install → 渲染（失败回喂修复，最多两轮）。返回 out.mp4 路径。
func (r *Remotion) Build(ctx context.Context, jobID string, task *core.Task, progress func(pct float64, status string)) (string, string, error) {
	if progress == nil {
		progress = func(float64, string) {}
	}
	if err := r.Precheck(); err != nil {
		return "", "", err
	}
	dir, err := r.Prepare(jobID)
	if err != nil {
		return "", "", err
	}
	progress(0.1, "running")
	if err := r.askClaude(ctx, dir, codePrompt(task.Prompt, secondsOf(task))); err != nil {
		return "", dir, err
	}
	progress(0.4, "running")
	out, err := r.installAndRender(ctx, dir, progress)
	return out, dir, err
}

// Revise 在已有目录上按指令再跑一轮改代码与渲染。
func (r *Remotion) Revise(ctx context.Context, dir, instruction string, task *core.Task, progress func(pct float64, status string)) (string, error) {
	if progress == nil {
		progress = func(float64, string) {}
	}
	if err := r.Precheck(); err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(dir, "src", "Main.tsx")); err != nil {
		return "", &core.ErrProvider{Summary: "动画工程目录已不存在：" + dir, Raw: err.Error()}
	}
	progress(0.1, "running")
	if err := r.askClaude(ctx, dir, revisePrompt(instruction, secondsOf(task))); err != nil {
		return "", err
	}
	progress(0.4, "running")
	return r.installAndRender(ctx, dir, progress)
}

func (r *Remotion) installAndRender(ctx context.Context, dir string, progress func(float64, string)) (string, error) {
	if err := r.npmInstall(ctx, dir); err != nil {
		return "", err
	}
	progress(0.6, "running")
	var lastErr string
	for round := 0; round <= r.fixRounds(); round++ {
		stderr, err := r.render(ctx, dir)
		if err == nil {
			out := filepath.Join(dir, "out.mp4")
			if _, err := os.Stat(out); err != nil {
				return "", &core.ErrProvider{Summary: "动画渲染结束但没有产物文件", Raw: stderr, Retryable: true}
			}
			return out, nil
		}
		lastErr = stderr
		if ctx.Err() != nil {
			return "", &core.ErrProvider{Summary: "动画渲染被中断", Raw: stderr, Retryable: true}
		}
		if round == r.fixRounds() {
			break
		}
		progress(0.6+0.1*float64(round+1), "running")
		if err := r.askClaude(ctx, dir, fixPrompt(stderr)); err != nil {
			return "", err
		}
	}
	return "", &core.ErrProvider{Summary: "动画渲染失败：自动修复两轮仍未通过", Raw: lastErr, Retryable: true}
}

// stripEnv 删掉指定变量。
func stripEnv(env []string, names []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, n := range names {
			if k == n {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// runAnimationJob 在后台跑动画任务；instruction 非空表示在已有目录上修改。
func (d *MediaDriver) runAnimationJob(job *core.Job, task *core.Task, outPath, instruction string, emit func(core.Event), done chan struct{}) {
	defer d.finish(job.ID, done)
	ctx, cancel := context.WithTimeout(context.Background(), d.opt.JobTimeout*3)
	defer cancel()
	d.mu.Lock()
	d.active[job.ID] = &activeJob{cancel: cancel}
	d.mu.Unlock()

	progress := func(pct float64, status string) {
		if ctx.Err() != nil {
			return
		}
		d.update(job.ID, emit, func(j *core.Job) {
			j.Status = status
			if pct > j.Progress {
				j.Progress = pct
			}
		})
	}
	var mp4, dir string
	var err error
	if instruction == "" {
		mp4, dir, err = d.remotion.Build(ctx, job.ID, task, progress)
	} else {
		dir = job.CodeDir
		mp4, err = d.remotion.Revise(ctx, dir, instruction, task, progress)
	}
	if err != nil {
		if ctx.Err() == context.Canceled {
			return // 用户取消，状态已由 Cancel 写好
		}
		msg, _ := classifyJobError(err)
		d.update(job.ID, emit, func(j *core.Job) {
			j.Status = "failed"
			j.Error = msg
			j.CodeDir = dir
		})
		return
	}
	if err := copyFile(mp4, outPath); err != nil {
		d.update(job.ID, emit, func(j *core.Job) {
			j.Status = "failed"
			j.Error = "复制动画产物失败：" + firstLine(err.Error())
			j.CodeDir = dir
		})
		return
	}
	d.update(job.ID, emit, func(j *core.Job) {
		j.Status = "done"
		j.Progress = 1
		j.ResultPath = outPath
		j.CodeDir = dir
	})
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}
