package sub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

// slowClaude 启动先睡 1 秒（模拟加载插件），再读完标准输入、记下来，然后输出成功夹具。
func slowClaude(t *testing.T) (bin, dir string) {
	t.Helper()
	dir = t.TempDir()
	out := filepath.Join(dir, "out.jsonl")
	_ = os.WriteFile(out, []byte(fixture(t, "claude_ok.jsonl")), 0o644)
	bin = filepath.Join(dir, "claude")
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 9.9.9; exit 0; fi\n" +
		"echo x >> \"" + dir + "/spawned\"\nsleep 1\n" +
		"cat > \"" + dir + "/in-$$\"\ncat \"" + out + "\"\n"
	_ = os.WriteFile(bin, []byte(script), 0o755)
	return bin, dir
}

func spawned(dir string) int {
	b, _ := os.ReadFile(filepath.Join(dir, "spawned"))
	return strings.Count(string(b), "x")
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 50; i++ {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("等不到预热进程")
}

func TestClaudePrewarm(t *testing.T) {
	needShell(t)
	bin, dir := slowClaude(t)
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	d := &ClaudeDriver{Bin: bin, Home: home, Prewarm: true}
	defer d.Close()
	task := func() *core.Task { return &core.Task{Kind: core.KindText, Prompt: "回复 OK"} }

	st := time.Now()
	if res, err := d.Run(context.Background(), task(), core.Target{Model: "haiku"}, nil); err != nil || res.Text != "OK" {
		t.Fatalf("冷启动失败：%v %+v", err, res)
	}
	if cold := time.Since(st); cold < time.Second {
		t.Fatalf("冷启动应至少 1 秒：%v", cold)
	}
	waitFor(t, func() bool { return d.warm().size() == 1 })
	time.Sleep(1200 * time.Millisecond) // 让预热进程把启动开销付完

	st = time.Now()
	if res, err := d.Run(context.Background(), task(), core.Target{Model: "haiku"}, nil); err != nil || res.Text != "OK" {
		t.Fatalf("用预热进程失败：%v %+v", err, res)
	}
	if warm := time.Since(st); warm > 700*time.Millisecond {
		t.Fatalf("预热后应明显更快：%v", warm)
	}
	// 每个进程只接一个任务：标准输入里只有一条消息
	ins, _ := filepath.Glob(filepath.Join(dir, "in-*"))
	for _, f := range ins {
		b, _ := os.ReadFile(f)
		if n := strings.Count(strings.TrimSpace(string(b)), "\n") + 1; len(b) > 0 && n != 1 {
			t.Errorf("一个进程收到了 %d 条消息", n)
		}
	}

	// 换了模型就不是同一组参数，不能拿别的模型的预热进程
	before := spawned(dir)
	if _, err := d.Run(context.Background(), task(), core.Target{Model: "sonnet"}, nil); err != nil {
		t.Fatal(err)
	}
	if spawned(dir) == before {
		t.Fatal("换模型应该重新启动")
	}

	// 带系统提示的任务不预热：参数里有系统提示，预热时还不知道
	waitFor(t, func() bool { return d.warm().size() >= 1 })
	n := d.warm().size()
	if _, err := d.Run(context.Background(), &core.Task{Kind: core.KindText, Prompt: "x", System: "你是翻译"}, core.Target{Model: "haiku"}, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if d.warm().size() > n {
		t.Fatal("带系统提示的任务不该补预热进程")
	}

	d.Close()
	if d.warm().size() != 0 {
		t.Fatal("关闭后不该留预热进程")
	}
}

// hangingAgy 出了结果也不退出，模拟 agy 读完输入后挂着的情况。
func TestAgyStopsAfterResult(t *testing.T) {
	needShell(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "agy")
	script := "#!/bin/sh\nread line\n" +
		`echo '{"event":"result","result":{"status":"SUCCESS","response":"OK"}}'` + "\nsleep 30\n"
	_ = os.WriteFile(bin, []byte(script), 0o755)
	d := &AntigravityDriver{Bin: bin}
	st := time.Now()
	res, err := d.Run(context.Background(), &core.Task{Kind: core.KindText, Prompt: "x"}, core.Target{Model: "gemini-3.8-flash-low"}, nil)
	if err != nil || strings.TrimSpace(res.Text) != "OK" {
		t.Fatalf("%v %+v", err, res)
	}
	if el := time.Since(st); el > 3*time.Second {
		t.Fatalf("看到结果后应很快收工，用了 %v", el)
	}
}
