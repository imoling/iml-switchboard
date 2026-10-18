// Package sub 实现订阅渠道：通过本机未修改的官方 CLI 子进程消耗套餐额度。
//
// 红线：不读取任何凭据文件内容；启动子进程前清掉按量付费相关环境变量；永远不传绕过凭据的参数。
package sub

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DefaultTimeout 是子进程默认超时。
const DefaultTimeout = 10 * time.Minute

// scrubbedEnv 是启动任何官方 CLI 前必须删掉的环境变量：
// 它们会让 CLI 改走按量付费接口或第三方网关，违背「订阅额度只能由官方 CLI 消耗」的约定。
var scrubbedEnv = []string{
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_BASE_URL",
	"CLAUDE_CODE_USE_BEDROCK",
	"CLAUDE_CODE_USE_VERTEX",
	"GEMINI_API_KEY",
	"GOOGLE_API_KEY",
	"OPENAI_API_KEY",
	"OPENAI_BASE_URL",
	// 嵌套在 Claude Code 里运行时，这两个变量会让子进程以为自己是被嵌套调用的实例。
	"CLAUDECODE",
	"CLAUDE_CODE_ENTRYPOINT",
}

// InnerEnv 是给订阅子进程打的标记。
const InnerEnv = "IMLS_INNER"

// cleanEnv 返回清洗后的环境变量列表。
func cleanEnv(base []string) []string {
	out := make([]string, 0, len(base))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, s := range scrubbedEnv {
			if name == s {
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

// procSpec 描述一次子进程调用。
type procSpec struct {
	Bin     string
	Args    []string
	Dir     string
	Stdin   string
	Timeout time.Duration
	// OnLine 收到 stdout 的每一行（不含换行）。
	OnLine func(line string)
	// Env 非空时替代 os.Environ() 作为清洗前的基础环境（测试用）。
	Env []string
}

// procResult 是子进程的运行结果。
type procResult struct {
	ExitCode int
	Stderr   string
	TimedOut bool
	Canceled bool
	// Err 是启动或等待阶段的错误；退出码非 0 不算 Err。
	Err error
}

// started 是已经启动、还没交任务的子进程。预热池里放的就是它：启动开销提前付掉，任务来了直接写标准输入。
type started struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *os.File // 自己建的管道：后台等进程退出时不会提前关掉它，读完最后一行之前数据不丢
	stderr *bytes.Buffer
	exited chan struct{}
	err    error // Wait 的结果，exited 关闭后可读
}

// startProc 启动子进程：清洗环境、打上 InnerEnv 标记，标准输入留着等任务。
func startProc(spec procSpec) (*started, error) {
	cmd := exec.Command(spec.Bin, spec.Args...)
	cmd.Dir = spec.Dir
	base := spec.Env
	if base == nil {
		base = os.Environ()
	}
	// InnerEnv 标记「这是 Switchboard 拉起的子进程」：子进程里若又拉起 imls mcp，它会直接退出，不形成循环
	cmd.Env = append(cleanEnv(base), InnerEnv+"=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = w
	ownGroup(cmd)
	// 进程退出后最多再等 2 秒就关掉错误输出的管道：防止它的子进程还握着管道，让 Wait 一直等下去
	cmd.WaitDelay = 2 * time.Second
	s := &started{cmd: cmd, stdin: stdin, stdout: r, stderr: &bytes.Buffer{}, exited: make(chan struct{})}
	cmd.Stderr = s.stderr
	if err := cmd.Start(); err != nil {
		r.Close()
		w.Close()
		return nil, err
	}
	w.Close() // 子进程拿着写端；它退出后读端才会读到结尾
	go func() {
		s.err = cmd.Wait()
		close(s.exited)
	}()
	return s, nil
}

// alive 表示进程还在等任务。
func (s *started) alive() bool {
	select {
	case <-s.exited:
		return false
	default:
		return true
	}
}

// kill 结束进程（连同它的子进程）并回收。
func (s *started) kill() {
	_ = s.stdin.Close()
	if s.alive() {
		killTree(s.cmd)
	}
	<-s.exited
	s.stdout.Close()
}

// finish 把任务写进标准输入，逐行读输出直到结束。done 非空时，它返回 true 就不再等进程自己退出（有的 CLI 读完输入还会挂着）。
func (s *started) finish(ctx context.Context, input string, timeout time.Duration, onLine func(string), done func() bool) procResult {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	go func() {
		select {
		case <-ctx.Done():
			killTree(s.cmd)
		case <-s.exited:
		}
	}()
	if input != "" {
		_, _ = io.WriteString(s.stdin, input)
	}
	_ = s.stdin.Close()

	stoppedEarly := false
	sc := bufio.NewScanner(s.stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		if onLine != nil {
			onLine(sc.Text())
		}
		if done != nil && done() {
			stoppedEarly = true
			break
		}
	}
	if stoppedEarly {
		// 结果已经拿到：给一秒自己退出，不退就结束它
		select {
		case <-s.exited:
		case <-time.After(time.Second):
			killTree(s.cmd)
		}
	}
	<-s.exited
	s.stdout.Close()

	res := procResult{Stderr: s.stderr.String()}
	if stoppedEarly {
		return res
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		res.TimedOut = errors.Is(ctxErr, context.DeadlineExceeded)
		res.Canceled = errors.Is(ctxErr, context.Canceled)
	}
	if s.err != nil {
		var ee *exec.ExitError
		if errors.As(s.err, &ee) {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = -1
			res.Err = s.err
		}
	}
	return res
}

// runProc 带上下文取消、超时、stderr 收集、环境清洗地运行子进程，并逐行回调 stdout。
func runProc(ctx context.Context, spec procSpec) procResult {
	s, err := startProc(spec)
	if err != nil {
		return procResult{ExitCode: -1, Err: err}
	}
	return s.finish(ctx, spec.Stdin, spec.Timeout, spec.OnLine, nil)
}

// lookBin 查找可执行文件；找不到返回空串。
func lookBin(name string) string {
	if name == "" {
		return ""
	}
	if strings.Contains(name, "/") {
		if _, err := os.Stat(name); err == nil {
			return name
		}
		return ""
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// dirExists 只 stat 目录是否存在，不读取任何内容。
func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// fileExists 只 stat 文件是否存在，不读取任何内容。
func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// versionOf 运行 `<bin> --version` 并返回首行；失败返回空串。不会消耗额度。
func versionOf(ctx context.Context, bin string) string {
	var first string
	res := runProc(ctx, procSpec{Bin: bin, Args: []string{"--version"}, Timeout: 15 * time.Second, OnLine: func(l string) {
		if first == "" && strings.TrimSpace(l) != "" {
			first = strings.TrimSpace(l)
		}
	}})
	if res.Err != nil || res.ExitCode != 0 {
		return ""
	}
	return first
}

// firstLine 取文本第一行非空内容，供一行人话的错误摘要使用。
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			if len(l) > 160 {
				l = l[:160] + "…"
			}
			return l
		}
	}
	return ""
}
