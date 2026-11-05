package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
)

type promptDriver struct {
	fakeDriver
	got string
}

func (p *promptDriver) Run(ctx context.Context, t *Task, tg Target, emit func(Event)) (*Result, error) {
	p.got = t.Prompt
	return &Result{Path: "/tmp/x.png", Model: tg.Model}, nil
}

func comicTask() *Task {
	ask := "现在根据这个文案生成一张漫画图"
	return &Task{Prompt: ask, Source: "facade", Host: "codex", Messages: []Message{
		{Role: "user", Content: "给我生成一个漫画的文案，关于美少女的故事"},
		{Role: "assistant", Content: "《高冷校花看不清》第1页：苏晚晴走进校门，黑长直，高二(3)班的冰山校花"},
		{Role: "user", Content: ask},
	}}
}

// 「根据这个文案画一张」：出图模型要看得到上文，记录里仍记用户原话
func TestMediaPromptCarriesContext(t *testing.T) {
	r := mediaRouter(t)
	_ = r.cfg.Update(func(c *config.Config) error { c.Settings.SavePrompts = true; return nil })
	md := &promptDriver{fakeDriver: fakeDriver{id: LaneMedia}}
	r.Register(md)
	call, _, err := r.Run(context.Background(), comicTask(), nil)
	if err != nil || call.Kind != KindImage {
		t.Fatalf("应走出图：%+v %v", call, err)
	}
	if md.got == "现在根据这个文案生成一张漫画图" || md.got == "" {
		t.Fatalf("出图提示词应改写成带上文的：%q", md.got)
	}
	if call.PromptExcerpt != "现在根据这个文案生成一张漫画图" {
		t.Fatalf("记录里应记原话：%q", call.PromptExcerpt)
	}
	if !strings.Contains(strings.Join(call.Reason, "；"), "上文") {
		t.Fatalf("理由里应写明带了上文：%v", call.Reason)
	}
}

// 改写用的低档模型都不可用：直接把上文拼进提示词，不能丢
func TestMediaPromptContextFallback(t *testing.T) {
	r := mediaRouter(t)
	boom := errors.New("down")
	for _, id := range []LaneID{LaneAPI, LaneClaudeSub, LaneCodexSub} {
		r.Register(&fakeDriver{id: id, fail: boom})
	}
	md := &promptDriver{fakeDriver: fakeDriver{id: LaneMedia}}
	r.Register(md)
	if _, _, err := r.Run(context.Background(), comicTask(), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md.got, "苏晚晴") || !strings.Contains(md.got, "现在根据这个文案生成一张漫画图") {
		t.Fatalf("改写不成也要带上文和原话：%q", md.got)
	}
}

// 没有上文（应用直接调出图工具）：提示词原样交给出图模型
func TestMediaPromptNoContextUntouched(t *testing.T) {
	r := mediaRouter(t)
	md := &promptDriver{fakeDriver: fakeDriver{id: LaneMedia}}
	r.Register(md)
	if _, _, err := r.Run(context.Background(), &Task{Prompt: "画一只海边的猫", Kind: KindImage, Source: "mcp"}, nil); err != nil {
		t.Fatal(err)
	}
	if md.got != "画一只海边的猫" {
		t.Fatalf("没有上文不该改：%q", md.got)
	}
}
