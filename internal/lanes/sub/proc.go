// Package sub 实现订阅渠道：通过本机未修改的官方 CLI 子进程消耗套餐额度。
//
// 红线：不读取任何凭据文件内容；启动子进程前清掉按量付费相关环境变量；永远不传绕过凭据的参数。
package sub

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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

// runProc 带上下文取消、超时、stderr 收集、环境清洗地运行子进程，并逐行回调 stdout。
func runProc(ctx context.Context, spec procSpec) procResult {
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, spec.Bin, spec.Args...)
	cmd.Dir = spec.Dir
	base := spec.Env
	if base == nil {
		base = os.Environ()
	}
	cmd.Env = cleanEnv(base)
	if spec.Stdin != "" {
		cmd.Stdin = strings.NewReader(spec.Stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return procResult{ExitCode: -1, Err: err}
	}
	if err := cmd.Start(); err != nil {
		return procResult{ExitCode: -1, Err: err}
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		if spec.OnLine != nil {
			spec.OnLine(sc.Text())
		}
	}
	waitErr := cmd.Wait()

	res := procResult{Stderr: stderr.String()}
	if ctxErr := ctx.Err(); ctxErr != nil {
		res.TimedOut = errors.Is(ctxErr, context.DeadlineExceeded)
		res.Canceled = errors.Is(ctxErr, context.Canceled)
	}
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = -1
			res.Err = waitErr
		}
	}
	return res
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
