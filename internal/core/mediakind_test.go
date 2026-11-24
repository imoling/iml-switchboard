package core

import (
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
)

func TestGuessMediaKind(t *testing.T) {
	for id, want := range map[string]string{"gpt-image-2": "image", "gpt-image-2.5-flare": "image", "dall-e-3": "image", "sora-2": "video", "veo-3": "video",
		"gpt-6.1-sol": "", "gpt-4o-audio-preview": "", "gpt-6-chat-thinking": ""} {
		if got := GuessMediaKind(id); got != want {
			t.Errorf("%s：%q，应为 %q", id, got, want)
		}
	}
}

// 动效视频只在点名调 make_animation 时做：规则表里留着老的动画行、判档员答 animation，对话都照常交给文字模型
func TestAnimationNeverAutoRouted(t *testing.T) {
	r, _, _ := newTestRouter(t)
	_ = r.Config().Update(func(c *config.Config) error {
		c.Routing.TaskTable = append([]config.TaskType{{Type: "my_anim", Name: "动画", Kind: "animation", Match: config.Match{Keywords: []string{"片头"}}}}, c.Routing.TaskTable...)
		return nil
	})
	for _, task := range []*Task{
		{Prompt: "做一段片头，Logo 从中间放大淡入", Source: "test"},
		{Prompt: "做个东西让数字滚起来", Source: "test", Judged: "animation"},
	} {
		if d := r.Decide(task); d.Kind != KindText {
			t.Errorf("%q 应交给文字模型，得到 %s（%v）", task.Prompt, d.Kind, d.Reason)
		}
	}
	if d := r.Decide(&Task{Prompt: "Logo 放大淡入", Kind: KindAnimation, Source: "mcp"}); d.Kind != KindAnimation {
		t.Errorf("点名 make_animation 仍应做动效视频，得到 %s", d.Kind)
	}
}
