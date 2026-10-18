package core

import (
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
