package sub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "cli_streams", name))
	if err != nil {
		t.Fatalf("读夹具失败：%v", err)
	}
	return string(b)
}

func TestClaudeArgsNeverContainForbiddenFlag(t *testing.T) {
	d := NewClaudeDriver()
	cases := []*core.Task{
		{Kind: core.KindText, Prompt: "hi"},
		{Kind: core.KindText, Prompt: "hi", System: "sys"},
		{Kind: core.KindAnimation, Prompt: "anim", Engine: "remotion", ProjectDir: "/tmp/x"},
		{Kind: core.KindText, Prompt: "p", Messages: []core.Message{{Role: "user", Content: "a"}, {Role: "assistant", Content: "b"}, {Role: "user", Content: "p"}}},
	}
	for _, task := range cases {
		args := d.BuildArgs(task, core.Target{Model: "haiku"})
		for _, a := range args {
			if a == forbiddenClaudeFlag || strings.Contains(a, forbiddenClaudeFlag) {
				t.Fatalf("参数里出现了被禁止的标志：%v", args)
			}
		}
	}
}

func TestClaudeArgsTextVsWrite(t *testing.T) {
	d := NewClaudeDriver()
	textArgs := d.BuildArgs(&core.Task{Kind: core.KindText, Prompt: "hi", System: "s"}, core.Target{Model: "opus"})
	text := strings.Join(textArgs, " ")
	for _, want := range []string{"-p --input-format stream-json", "--output-format stream-json", "--verbose", "--no-session-persistence", "--model opus", "--tools "} {
		if !strings.Contains(text, want) {
			t.Errorf("缺少 %q：%s", want, text)
		}
	}
	if strings.Contains(text, "hi") {
		t.Errorf("提示词不该出现在参数里（走标准输入）：%s", text)
	}
	// 文本子任务换掉默认系统提示，宿主自带的系统提示接在后面
	if i := slices.Index(textArgs, "--system-prompt"); i < 0 || !strings.HasPrefix(textArgs[i+1], subtaskSystem) || !strings.HasSuffix(textArgs[i+1], "\n\ns") {
		t.Errorf("系统提示不对：%q", textArgs)
	}
	if strings.Contains(text, "--permission-mode") || strings.Contains(text, "--append-system-prompt") {
		t.Errorf("纯文本任务不应有 permission-mode 或追加系统提示：%s", text)
	}
	write := strings.Join(d.BuildArgs(&core.Task{Kind: core.KindAnimation, Prompt: "x", ProjectDir: "/tmp/p", System: "s"}, core.Target{Model: "sonnet"}), " ")
	if strings.Contains(write, "--tools") || strings.Contains(write, "--system-prompt") {
		t.Errorf("写文件任务不应禁用工具或换掉默认系统提示：%s", write)
	}
	for _, want := range []string{"--permission-mode acceptEdits", "--max-turns 12", "--append-system-prompt s"} {
		if !strings.Contains(write, want) {
			t.Errorf("缺少 %q：%s", want, write)
		}
	}
	// 多轮消息拍平后作为一条 stream-json 消息走标准输入
	payload := StdinPayload(&core.Task{Kind: core.KindText, Prompt: "p", Messages: []core.Message{{Role: "user", Content: "a"}, {Role: "assistant", Content: "b"}, {Role: "user", Content: "p"}}})
	var msg struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct{ Text string } `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(payload), &msg); err != nil || msg.Type != "user" || len(msg.Message.Content) != 1 {
		t.Fatalf("标准输入不是一条用户消息：%q %v", payload, err)
	}
	if got := msg.Message.Content[0].Text; !strings.Contains(got, "[assistant]\nb") || !strings.HasSuffix(got, "当前请求：\np") {
		t.Errorf("多轮拍平不对：%q", got)
	}
}

func TestCleanEnv(t *testing.T) {
	in := []string{"ANTHROPIC_API_KEY=x", "ANTHROPIC_AUTH_TOKEN=y", "ANTHROPIC_BASE_URL=z", "CLAUDE_CODE_USE_BEDROCK=1", "CLAUDE_CODE_USE_VERTEX=1", "PATH=/bin", "HOME=/h"}
	out := cleanEnv(in)
	if len(out) != 2 {
		t.Fatalf("清洗后应剩 2 个，得到 %v", out)
	}
	for _, kv := range out {
		if strings.HasPrefix(kv, "ANTHROPIC") || strings.HasPrefix(kv, "CLAUDE_CODE_USE") {
			t.Fatalf("没删干净：%v", out)
		}
	}
}

func TestParseClaudeFixture(t *testing.T) {
	var deltas []string
	p := ParseClaudeStream(fixture(t, "claude_ok.jsonl"), func(e core.Event) {
		if e.Type == "text_delta" {
			deltas = append(deltas, e.Text)
		}
	})
	if p.Text != "OK" || p.ResultText != "OK" {
		t.Errorf("文本不对：%+v", p)
	}
	if strings.Join(deltas, "") != "OK" {
		t.Errorf("text_delta 不对：%v", deltas)
	}
	if p.IsError || !p.GotResult {
		t.Errorf("状态不对：%+v", p)
	}
	if p.Usage.InputTokens != 2 || p.Usage.OutputTokens != 4 || p.Usage.CacheReadTokens != 649 {
		t.Errorf("usage 不对：%+v", p.Usage)
	}
	if p.Usage.CostUSD != 0 || p.Usage.QuotaUnits < 0.001 || p.Usage.QuotaUnits > 0.002 {
		t.Errorf("订阅费用应为 0、额度占用取 total_cost_usd：%+v", p.Usage)
	}
	if p.Model != "claude-haiku-5-5" || p.SessionID != "170d6a85-c0b6-49e3-b249-cac923cc4780" {
		t.Errorf("model/session 不对：%+v", p)
	}
	if p.RateLimited {
		t.Errorf("status=allowed 不应判为限流")
	}
	if len(p.Quota) != 2 {
		t.Fatalf("应解析出两个额度窗口：%+v", p.Quota)
	}
	for _, q := range p.Quota {
		switch q.Window {
		case "5h":
			if q.Pct < 5.9 || q.Pct > 6.1 {
				t.Errorf("5h 利用率不对：%+v", q)
			}
		case "week":
			if q.Pct < 11.9 || q.Pct > 12.1 {
				t.Errorf("week 利用率不对：%+v", q)
			}
		default:
			t.Errorf("未知窗口：%+v", q)
		}
	}
}

func TestParseClaudeErrorAndRateLimitEvent(t *testing.T) {
	data := `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1792278000,"rateLimitType":"five_hour","unifiedWindows":{"five_hour":{"utilization":1,"resetsAt":1792278000}}}}
{"type":"result","subtype":"error_during_execution","is_error":true,"result":"You've hit your limit · resets 3pm (Asia/Shanghai)","usage":{"input_tokens":0,"output_tokens":0}}`
	p := ParseClaudeStream(data, nil)
	if !p.IsError || !p.RateLimited || p.RateResetsAt.Unix() != 1792278000 {
		t.Errorf("限流事件没解析出来：%+v", p)
	}
}

func TestParseCodexFixtures(t *testing.T) {
	ok := ParseCodexStream(fixture(t, "codex_ok.jsonl"), nil)
	if ok.Text != "OK" || !ok.Completed || ok.Failed {
		t.Errorf("成功夹具解析不对：%+v", ok)
	}
	if ok.Usage.InputTokens != 1234 || ok.Usage.CacheReadTokens != 1024 || ok.Usage.OutputTokens != 3 || ok.Usage.CostUSD != 0 {
		t.Errorf("usage 不对：%+v", ok.Usage)
	}
	if ok.ThreadID == "" {
		t.Errorf("thread_id 没解析")
	}

	rl := ParseCodexStream(fixture(t, "codex_rate_limited.jsonl"), nil)
	if !rl.Failed || len(rl.Errors) == 0 {
		t.Fatalf("限流夹具应判为失败：%+v", rl)
	}
	all := strings.Join(rl.Errors, "\n")
	if !isRateLimitText(all) {
		t.Errorf("应识别为限流：%s", all)
	}
	now := time.Date(2026, 10, 18, 4, 0, 0, 0, time.Local)
	ra, ok2 := parseRetryAfter(all, now)
	if !ok2 {
		t.Fatalf("应解析出重置时间：%s", all)
	}
	want := time.Date(2026, 10, 29, 8, 54, 0, 0, time.Local).Sub(now)
	if ra != want {
		t.Errorf("RetryAfter=%v，期望 %v", ra, want)
	}

	me := ParseCodexStream(fixture(t, "codex_model_error.jsonl"), nil)
	if !me.Failed || isRateLimitText(strings.Join(me.Errors, "\n")) {
		t.Errorf("模型错误不应判为限流：%+v", me)
	}
	if s := errorSummary(me.Errors); !strings.Contains(s, "not supported") || strings.HasPrefix(s, "{") {
		t.Errorf("错误摘要应是人话而不是 JSON：%q", s)
	}
}

func TestCodexArgs(t *testing.T) {
	d := NewCodexDriver()
	d.Home = t.TempDir() // 不读本机真实的 ~/.codex：那里可能已经接入了 Switchboard
	args, stdin := d.BuildArgs(&core.Task{Kind: core.KindText, Prompt: "回复 OK"}, core.Target{Model: "gpt-5.6-luna"})
	joined := strings.Join(args, " ")
	if joined != "exec --json --skip-git-repo-check -m gpt-5.6-luna --ephemeral -" {
		t.Errorf("参数不对：%s", joined)
	}
	if stdin != "回复 OK" {
		t.Errorf("stdin 不对：%q", stdin)
	}
	args, stdin = d.BuildArgs(&core.Task{Kind: core.KindImage, Prompt: "一只猫"}, core.Target{Model: CodexImageModel})
	if strings.Contains(strings.Join(args, " "), CodexImageModel) {
		t.Errorf("虚拟模型名不能传给 -m：%v", args)
	}
	if !strings.HasPrefix(stdin, "$imagegen 用 image_gen 生成一张图：一只猫") || !strings.HasSuffix(stdin, "保存后告诉我文件路径") {
		t.Errorf("出图提示词不对：%q", stdin)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 18, 10, 0, 0, 0, time.Local)
	cases := map[string]time.Duration{
		"try again in 2 hours 15 minutes":      2*time.Hour + 15*time.Minute,
		"Your limit will reset at 3pm":         5 * time.Hour,
		"resets 9am (Asia/Shanghai)":           23 * time.Hour,
		`"resetsAt":1892278000`:                time.Unix(1892278000, 0).Sub(now),
		"try again at Oct 29th, 2026 8:54 AM.": time.Date(2026, 10, 29, 8, 54, 0, 0, time.Local).Sub(now),
	}
	for in, want := range cases {
		got, ok := parseRetryAfter(in, now)
		if !ok || got != want {
			t.Errorf("%q → %v（ok=%v），期望 %v", in, got, ok, want)
		}
	}
	if got, ok := parseRetryAfter("something went wrong", now); ok || got != DefaultRetryAfter {
		t.Errorf("无法解析时应给默认 30 分钟：%v %v", got, ok)
	}
}

func TestRunProcWithFakeBinary(t *testing.T) {
	needShell(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "fake.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho line1\necho line2\necho err >&2\nif [ -n \"$ANTHROPIC_API_KEY\" ]; then echo LEAK; fi\nexit 3\n"), 0o755)
	var lines []string
	res := runProc(context.Background(), procSpec{Bin: script, Env: []string{"ANTHROPIC_API_KEY=leak", "PATH=/bin:/usr/bin"}, OnLine: func(l string) { lines = append(lines, l) }})
	if res.Err != nil || res.ExitCode != 3 || strings.TrimSpace(res.Stderr) != "err" {
		t.Fatalf("结果不对：%+v", res)
	}
	if strings.Join(lines, ",") != "line1,line2" {
		t.Fatalf("stdout 行不对（环境没清洗？）：%v", lines)
	}
	// 超时
	os.WriteFile(script, []byte("#!/bin/sh\nsleep 5\n"), 0o755)
	res = runProc(context.Background(), procSpec{Bin: script, Timeout: 200 * time.Millisecond})
	if !res.TimedOut {
		t.Fatalf("应超时：%+v", res)
	}
}

func TestClaudeRunWithFakeBinary(t *testing.T) {
	needShell(t)
	dir := t.TempDir()
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	data := fixture(t, "claude_ok.jsonl")
	os.WriteFile(filepath.Join(dir, "out.jsonl"), []byte(data), 0o644)
	script := filepath.Join(dir, "claude")
	os.WriteFile(script, []byte("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 9.9.9; exit 0; fi\nfor a in \"$@\"; do [ \"$a\" = \""+forbiddenClaudeFlag+"\" ] && exit 99; done\ncat \""+filepath.Join(dir, "out.jsonl")+"\"\n"), 0o755)
	d := &ClaudeDriver{Bin: script, Home: home}
	res, err := d.Run(context.Background(), &core.Task{Kind: core.KindText, Prompt: "回复 OK 两个字"}, core.Target{Model: "haiku"}, nil)
	if err != nil {
		t.Fatalf("Run 失败：%v", err)
	}
	if res.Text != "OK" || res.Model != "claude-haiku-5-5" || res.Usage.CostUSD != 0 {
		t.Errorf("结果不对：%+v", res)
	}
	h := d.Health(context.Background())
	if h.Status != "ok" || len(h.Quota) != 2 {
		t.Errorf("Health 应为 ok 且带额度窗口：%+v", h)
	}

	// 限流：退出码非 0 且文本含 usage limit
	os.WriteFile(script, []byte("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 9.9.9; exit 0; fi\necho '{\"type\":\"result\",\"is_error\":true,\"result\":\"Claude usage limit reached. Your limit will reset at 3pm\"}'\nexit 1\n"), 0o755)
	_, err = d.Run(context.Background(), &core.Task{Kind: core.KindText, Prompt: "x"}, core.Target{Model: "haiku"}, nil)
	var rl *core.ErrRateLimited
	if !errors.As(err, &rl) || rl.Lane != core.LaneClaudeSub || rl.RetryAfter <= 0 {
		t.Fatalf("应返回 ErrRateLimited：%v", err)
	}
	if h := d.Health(context.Background()); h.Status != "cooldown" {
		t.Errorf("限流后 Health 应为 cooldown：%+v", h)
	}

	// 普通失败
	os.WriteFile(script, []byte("#!/bin/sh\necho 'boom happened' >&2\nexit 2\n"), 0o755)
	d2 := &ClaudeDriver{Bin: script, Home: home}
	_, err = d2.Run(context.Background(), &core.Task{Kind: core.KindText, Prompt: "x"}, core.Target{Model: "haiku"}, nil)
	var pe *core.ErrProvider
	if !errors.As(err, &pe) || !strings.HasPrefix(pe.Summary, "Claude 执行失败：boom happened") {
		t.Fatalf("应返回 ErrProvider：%v", err)
	}
}

func TestHealthMissing(t *testing.T) {
	c := &ClaudeDriver{Bin: "/nonexistent/claude-xyz"}
	if h := c.Health(context.Background()); h.Status != "missing" || h.Detail != "本机没有找到 claude 命令" {
		t.Errorf("Claude missing 不对：%+v", h)
	}
	x := &CodexDriver{Bin: "/nonexistent/codex-xyz"}
	if h := x.Health(context.Background()); h.Status != "missing" {
		t.Errorf("Codex missing 不对：%+v", h)
	}
	a := &AntigravityDriver{Bin: "/nonexistent/agy-xyz"}
	if h := a.Health(context.Background()); h.Status != "missing" {
		t.Errorf("Antigravity missing 不对：%+v", h)
	}
	if _, err := a.Run(context.Background(), &core.Task{}, core.Target{}, nil); err == nil {
		t.Errorf("Antigravity Run 应返回错误")
	}
}

func TestCodexRunWithFakeBinary(t *testing.T) {
	needShell(t)
	dir := t.TempDir()
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".codex", "generated_images"), 0o755)
	os.WriteFile(filepath.Join(home, ".codex", "auth.json"), []byte("{}"), 0o600)
	okData := fixture(t, "codex_ok.jsonl")
	os.WriteFile(filepath.Join(dir, "ok.jsonl"), []byte(okData), 0o644)
	script := filepath.Join(dir, "codex")
	// 假 codex：把 stdin 记下来，输出成功夹具；若 stdin 含 $imagegen 则生成一张图
	os.WriteFile(script, []byte(`#!/bin/sh
if [ "$1" = --version ]; then echo codex-cli 0.0.0; exit 0; fi
IN=$(cat)
echo "$IN" > "`+dir+`/stdin.txt"
case "$IN" in *imagegen*) printf 'PNG' > "`+filepath.Join(home, ".codex", "generated_images", "a.png")+`";; esac
cat "`+filepath.Join(dir, "ok.jsonl")+`"
`), 0o755)
	d := &CodexDriver{Bin: script, Home: home, Ephemeral: true}
	res, err := d.Run(context.Background(), &core.Task{Kind: core.KindText, Prompt: "回复 OK"}, core.Target{Model: "gpt-5.6-luna"}, nil)
	if err != nil {
		t.Fatalf("Run 失败：%v", err)
	}
	if res.Text != "OK" || res.Usage.InputTokens != 1234 {
		t.Errorf("结果不对：%+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "stdin.txt")); strings.TrimSpace(string(b)) != "回复 OK" {
		t.Errorf("提示词应从 stdin 传：%q", b)
	}
	if h := d.Health(context.Background()); h.Status != "ok" {
		t.Errorf("Health 应为 ok：%+v", h)
	}
	// 出图
	out := t.TempDir()
	res, err = d.Run(context.Background(), &core.Task{Kind: core.KindImage, Prompt: "一只猫", OutDir: out}, core.Target{Model: CodexImageModel}, nil)
	if err != nil {
		t.Fatalf("出图失败：%v", err)
	}
	if !strings.HasPrefix(res.Path, out) || !strings.HasSuffix(res.Path, ".png") || res.Usage.Unit != "image" {
		t.Errorf("出图产物不对：%+v", res)
	}
	// 限流夹具
	os.WriteFile(filepath.Join(dir, "ok.jsonl"), []byte(fixture(t, "codex_rate_limited.jsonl")), 0o644)
	_, err = d.Run(context.Background(), &core.Task{Kind: core.KindText, Prompt: "x"}, core.Target{}, nil)
	var rl *core.ErrRateLimited
	if !errors.As(err, &rl) || rl.Lane != core.LaneCodexSub {
		t.Fatalf("应返回 ErrRateLimited：%v", err)
	}
	if rl.RetryAfter <= 0 {
		t.Errorf("RetryAfter 应为正：%v", rl.RetryAfter)
	}
}

func TestClaudePlanLabel(t *testing.T) {
	cases := []struct{ org, tier, seat, want string }{
		{"claude_max", "default_claude_max_20x", "", "Max 20x"},
		{"claude_max", "default_claude_max_5x", "", "Max 5x"},
		{"claude_pro", "", "", "Pro"},
		{"claude_free", "", "", "免费版"},
		{"claude_team", "", "premium", "Team 高级席位"},
		{"", "", "", ""},
	}
	for _, c := range cases {
		if got := ClaudePlanLabel(c.org, c.tier, c.seat); got != c.want {
			t.Errorf("%s/%s: got %q want %q", c.org, c.tier, got, c.want)
		}
	}
}
