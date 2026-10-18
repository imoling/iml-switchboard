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
	for _, want := range []string{"-p 你好", "--output-format stream-json", "--model gemini-3.8-flash-low", "--mode plan"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("缺少 %q：%s", want, joined)
		}
	}
	args = d.BuildArgs(&core.Task{Prompt: "改代码", Kind: core.KindAnimation, ProjectDir: "/tmp/x", Engine: "remotion"}, core.Target{Model: "claude-sonnet-4-6"})
	if !strings.Contains(strings.Join(args, " "), "--mode accept-edits") {
		t.Fatalf("写文件任务应用 accept-edits：%v", args)
	}
}
