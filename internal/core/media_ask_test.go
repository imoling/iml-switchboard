package core

import (
	"github.com/imoling/iml-switchboard/internal/config"
	"strings"
	"testing"
)

// WorkBuddy 的提醒块带属性，也要剥掉
func TestStripInjectedWithAttributes(t *testing.T) {
	long := `<system-reminder data-role="user-context">` + strings.Repeat("工作区信息 ", 200) + "</system-reminder>\n生成图片：一枝红杏出墙来"
	if got := StripInjected(long); got != "生成图片：一枝红杏出墙来" {
		t.Fatalf("带属性的提醒块没剥干净：%q", got)
	}
	if !IsMediaAsk(long) {
		t.Fatal("剥掉提醒块后应是一句直接的出图请求")
	}
	if got := StripInjected(`<system-reminder data-role="user-context"> <craft_mode> You are now in Agent mode. Continue wi`); got != "" {
		t.Fatalf("截断的提醒块要整段去掉：%q", got)
	}
	if got := StripInjected("<user_query>你好，你是什么模型</user_query>"); got != "你好，你是什么模型" {
		t.Fatalf("user_query 标签要拆掉：%q", got)
	}
	if got := StripInjected("<session>\n生成图片：一枝红杏出墙来\n</session>"); got != "生成图片：一枝红杏出墙来" {
		t.Fatalf("包装标签要拆掉：%q", got)
	}
}

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
