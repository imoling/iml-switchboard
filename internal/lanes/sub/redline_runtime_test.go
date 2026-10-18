package sub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/core"
)

// 红线 3、4 的运行时守卫：扫源码挡不住「"--" + "bare"」这种拼接，这里用假的 CLI 记下真正收到的参数和环境变量。
// 父进程故意带上全部按量付费变量，三个订阅驱动、各种请求形态都跑一遍。

// mustScrub 单独写一份，不复用 scrubbedEnv：有人从那份列表里删掉一项，这里照样会失败。
var mustScrub = []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "OPENAI_API_KEY", "OPENAI_BASE_URL", "GEMINI_API_KEY", "GOOGLE_API_KEY"}

type recorder struct {
	dir string
	bin string
}

func newRecorder(t *testing.T, name, fixtureName string) *recorder {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SB_FAKE_DIR", dir)
	t.Setenv("SB_FAKE_OUT", "")
	if fixtureName != "" {
		out := filepath.Join(dir, "out.jsonl")
		_ = os.WriteFile(out, []byte(fixture(t, fixtureName)), 0o644)
		t.Setenv("SB_FAKE_OUT", out)
	}
	// 假 CLI 就是测试二进制自己（见 fakecli_test.go），name 只用于报错时区分
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return &recorder{dir: dir, bin: exe}
}

func (r *recorder) runs(t *testing.T) (args, envs [][]string) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(r.dir, "args-*"))
	for _, f := range files {
		a, _ := os.ReadFile(f)
		e, _ := os.ReadFile(strings.Replace(f, "args-", "env-", 1))
		args = append(args, strings.Split(strings.TrimSpace(string(a)), "\n"))
		envs = append(envs, strings.Split(strings.TrimSpace(string(e)), "\n"))
	}
	return
}

func taskShapes(t *testing.T) []*core.Task {
	proj := t.TempDir()
	return []*core.Task{
		{Kind: core.KindText, Prompt: "回复 OK"},
		{Kind: core.KindText, Prompt: "回复 OK", System: "你是助手"},
		{Kind: core.KindText, Prompt: "在项目里加一个文件", ProjectDir: proj, Engine: "write"},
		{Kind: core.KindAnimation, Prompt: "写一个 Remotion 组件", ProjectDir: proj},
	}
}

func assertClean(t *testing.T, who string, args, envs [][]string) {
	t.Helper()
	if len(args) == 0 {
		t.Fatalf("%s：假 CLI 一次都没被调用", who)
	}
	for i := range args {
		for _, a := range args[i] {
			if bare := "--" + "bare"; a == bare || strings.HasPrefix(a, bare+"=") {
				t.Errorf("%s 第 %d 次调用带了 %s：%v", who, i+1, a, args[i])
			}
		}
		for _, kv := range envs[i] {
			name, _, _ := strings.Cut(kv, "=")
			for _, s := range mustScrub {
				if name == s {
					t.Errorf("%s 第 %d 次调用把 %s 传给了子进程", who, i+1, name)
				}
			}
		}
	}
}

func setLeakyEnv(t *testing.T) {
	for _, s := range mustScrub {
		t.Setenv(s, "leak")
	}
}

func TestRedLineRuntimeClaude(t *testing.T) {
	setLeakyEnv(t)
	rec := newRecorder(t, "claude", "claude_ok.jsonl")
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	d := &ClaudeDriver{Bin: rec.bin, Home: home}
	for _, task := range taskShapes(t) {
		_, _ = d.Run(context.Background(), task, core.Target{Model: "haiku"}, nil)
	}
	_, _ = d.SelfCheck(context.Background())
	args, envs := rec.runs(t)
	assertClean(t, "claude", args, envs)
}

func TestRedLineRuntimeCodex(t *testing.T) {
	setLeakyEnv(t)
	rec := newRecorder(t, "codex", "codex_ok.jsonl")
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	d := &CodexDriver{Bin: rec.bin, Home: home, Ephemeral: true}
	for _, task := range taskShapes(t) {
		_, _ = d.Run(context.Background(), task, core.Target{Model: "gpt-5.6-luna"}, nil)
	}
	_, _ = d.SelfCheck(context.Background())
	args, envs := rec.runs(t)
	assertClean(t, "codex", args, envs)
}

func TestRedLineRuntimeAntigravity(t *testing.T) {
	setLeakyEnv(t)
	rec := newRecorder(t, "agy", "")
	d := &AntigravityDriver{Bin: rec.bin}
	for _, task := range taskShapes(t) {
		_, _ = d.Run(context.Background(), task, core.Target{Model: "gemini-3.8-flash-low"}, nil)
	}
	args, envs := rec.runs(t)
	assertClean(t, "agy", args, envs)
}
