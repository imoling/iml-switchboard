package core

import (
	"github.com/imoling/iml-switchboard/internal/config"
	"strings"
	"testing"
)

func TestIsMediaAsk(t *testing.T) {
	if !IsMediaAsk("做一个 8 秒的标题动画") {
		t.Fatal("短句应算媒体请求")
	}
	long := "<system-reminder>" + strings.Repeat("技能：动画、视频、海报。", 50) + "</system-reminder>只回复 ok"
	if !IsMediaAsk(long) {
		t.Fatal("去掉提醒块后只剩一句，应算媒体请求判定范围")
	}
	if got := StripInjected(long); got != "只回复 ok" {
		t.Fatalf("注入块应被整段去掉，得到 %q", got)
	}
	if IsMediaAsk(strings.Repeat("项目里有很多动画和视频素材。", 60)) {
		t.Fatal("几百字的上下文不该算媒体请求")
	}
}

func TestNaturalMediaPhrases(t *testing.T) {
	table := config.Default().Routing.TaskTable
	cases := map[string]string{
		"做一个 5 秒的海浪视频": "video",
		"生成一段海浪的视频":    "video",
		"帮我画一张海浪的图":    "image",
		"来个春天主题的海报":    "image",
	}
	for prompt, want := range cases {
		tt, ok := MatchTaskType(table, prompt, Extract(prompt, 0, 0))
		if !ok || tt.Kind != want {
			t.Errorf("%q 应判为 %s，得到 %v %q", prompt, want, ok, tt.Kind)
		}
	}
	if tt, ok := MatchTaskType(table, "帮我看看这个视频脚本写得怎么样", Extract("帮我看看这个视频脚本写得怎么样", 0, 0)); ok && tt.Kind == "video" {
		t.Errorf("讨论视频脚本不该当成出视频")
	}
}
