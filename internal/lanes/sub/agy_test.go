package sub

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/core"
)

func TestAgyParser(t *testing.T) {
	f, err := os.Open("../../../testdata/cli_streams/agy_ok.jsonl")
	if err != nil {
		t.Skip("没有夹具")
	}
	defer f.Close()
	var deltas []string
	p := &agyParser{emit: func(e core.Event) {
		if e.Type == "text_delta" {
			deltas = append(deltas, e.Text)
		}
	}}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p.feed(sc.Text())
	}
	got := p.done()
	if got.Text != "收到" || got.Status != "SUCCESS" || !got.SawResult {
		t.Fatalf("%+v", got)
	}
	if got.Usage.InputTokens != 11815 || got.Usage.OutputTokens != 119+118 {
		t.Fatalf("用量解析错误：%+v", got.Usage)
	}
	if strings.Join(deltas, "") != "收到\n" {
		t.Fatalf("流式片段：%q", deltas)
	}
}

func TestAgyArgs(t *testing.T) {
	d := NewAntigravityDriver()
	args := d.BuildArgs(&core.Task{Prompt: "你好", Kind: core.KindText}, core.Target{Model: "gemini-3.8-flash-low"})
	joined := strings.Join(args, " ")
	for _, want := range []string{"--input-format stream-json", "--output-format stream-json", "--model gemini-3.8-flash-low", "--mode plan"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("缺少 %q：%s", want, joined)
		}
	}
	if args[0] != "-p" || args[1] != "" || strings.Contains(joined, "你好") {
		t.Fatalf("-p 要带空值，提示词走标准输入：%q", args)
	}
	if got := AgyStdinPayload(&core.Task{Prompt: "你好", Kind: core.KindText}); got != `{"event":"user","message":{"content":"你好","role":"user"}}`+"\n" {
		t.Fatalf("标准输入格式不对：%q", got)
	}
	args = d.BuildArgs(&core.Task{Prompt: "改代码", Kind: core.KindAnimation, ProjectDir: "/tmp/x", Engine: "remotion"}, core.Target{Model: "claude-sonnet-4-6"})
	if !strings.Contains(strings.Join(args, " "), "--mode accept-edits") {
		t.Fatalf("写文件任务应用 accept-edits：%v", args)
	}
}

func TestParseCodexRateLimits(t *testing.T) {
	raw := []byte(`{"rateLimits":{"limitId":"codex","primary":{"usedPercent":12.5,"windowDurationMins":300,"resetsAt":1794861817},"secondary":{"usedPercent":40,"windowDurationMins":10080,"resetsAt":1794861817},"planType":"plus"}}`)
	ws, plan, err := ParseCodexRateLimits(raw)
	if err != nil || plan != "plus" || len(ws) != 2 || ws[0].Window != "5h" || ws[0].Pct != 12.5 || ws[1].Window != "week" {
		t.Fatalf("%+v %q %v", ws, plan, err)
	}
	ws, plan, _ = ParseCodexRateLimits([]byte(`{"rateLimits":{"primary":{"usedPercent":0,"windowDurationMins":43200},"planType":"free"}}`))
	if plan != "free" || ws[0].Window != "month" || PlanLabel(plan) != "免费版" {
		t.Fatalf("%+v %q", ws, plan)
	}
}

func TestParseAgyUsage(t *testing.T) {
	txt := "Gemini Models\tWeekly Limit Remaining\t99%\t2026-10-25T03:09:58Z\nGemini Models\tFive Hour Limit Remaining\t97%\t2026-10-18T08:09:58Z\nClaude and GPT models\tWeekly Limit Remaining\t100%\t2026-10-25T04:06:45Z\nClaude and GPT models\tFive Hour Limit Remaining\t60%\t2026-10-18T09:06:45Z\n"
	ws := ParseAgyUsage(txt)
	if len(ws) != 4 {
		t.Fatalf("%+v", ws)
	}
	got := map[string]float64{}
	for _, w := range ws {
		got[w.Window] = w.Pct
	}
	if got["week"] != 1 || got["5h"] != 3 || got["week_claude"] != 0 || got["5h_claude"] != 40 {
		t.Fatalf("剩余应换算成已用：%+v", got)
	}
	if ws[0].ResetsAt.IsZero() {
		t.Fatal("应解析重置时间")
	}
}
