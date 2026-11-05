package core

import (
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
)

// 规则认出要视频、但一个出视频账号都没配：交给文字模型，不报错
func TestAutoMediaFallsBackToTextWithoutProvider(t *testing.T) {
	r, _, _ := newTestRouter(t)
	d := r.Decide(&Task{Prompt: "做一个 5 秒的海浪视频"})
	if d.Kind != KindText || len(live(d.Candidates)) == 0 {
		t.Fatalf("没配影师应交给文字模型：%s %v", d.Kind, d.Reason)
	}
	if !strings.Contains(strings.Join(d.Reason, "；"), "交给文字模型") {
		t.Fatalf("理由里要写明为什么没出视频：%v", d.Reason)
	}
	// 调用方点名要视频（generate_video）就照常报错，不偷偷换成文字
	d = r.Decide(&Task{Prompt: "海浪", Kind: KindVideo})
	if d.Kind != KindVideo {
		t.Fatalf("点名要视频不该改成文字：%s", d.Kind)
	}
}

// 带参考图时，只能文生图的型号要跳过：出一张跟参考图无关的图还照收钱
func TestRefImagesSkipTextOnlyModels(t *testing.T) {
	r, _, _ := newTestRouter(t)
	_ = r.Config().Update(func(c *config.Config) error {
		c.API = append(c.API, config.APIAccount{ID: "openai-1", Provider: "openai", Enabled: true})
		c.Media.Image = map[string]config.MediaTarget{"default": {Provider: "openai", Model: "dall-e-3"}, "best": {Provider: "openai", Model: "gpt-image-2.5"}}
		return nil
	})
	d := r.Decide(&Task{Prompt: "改成像素画", Kind: KindImage, RefImages: []string{"/tmp/a.png"}})
	first := live(d.Candidates)
	if len(first) == 0 || first[0].Model != "gpt-image-2.5" {
		t.Fatalf("带参考图应跳过 dall-e-3：%+v", d.Candidates)
	}
	d = r.Decide(&Task{Prompt: "画一只猫", Kind: KindImage})
	if live(d.Candidates)[0].Model != "dall-e-3" {
		t.Fatalf("没有参考图时照常用默认档：%+v", d.Candidates)
	}
}
