package core

import (
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
)

func TestGoldenRouting(t *testing.T) {
	table := config.Default().Routing.TaskTable
	cases := []struct {
		prompt string
		want   string
	}{
		{"Python 里怎么把 list 按某个字段分组", "qa_short"},
		{"把这段话翻译成英文：今天天气不错", "rewrite"},
		{"帮我总结一下这 40 条报错日志分成几类", "summarize"},
		{"给 README 配一张封面图", "image"},
		{"做一个 8 秒的标题动画", "animation"},
		{"做一个 5 秒的产品介绍视频", "video"},
		{"为什么这个组件每次输入都重新渲染\nTraceback (most recent call last):\n  File \"a.py\", line 3\nTypeError: x", "debug_hard"},
		{"这个项目的后端架构怎么设计比较好", "architecture"},
		{"review 一下这个函数\n```go\nfunc a() {}\n```", "review"},
		{"把这个函数改成支持批量传入\n```python\ndef f(x):\n    return x\n```", "code_small"},
		{"试了很多次还是不行，这个登录接口一直 500", "debug_stuck"},
	}
	for _, c := range cases {
		sig := Extract(c.prompt, 0, EstimateTokens(c.prompt))
		got, ok := MatchTaskType(table, c.prompt, sig)
		if !ok || got.Type != c.want {
			t.Errorf("%q → %q，想要 %q（命中 %v）", c.prompt, got.Type, c.want, ok)
		}
	}
}

func TestExplicit(t *testing.T) {
	rules := config.Default().Routing.Explicit
	h := FindExplicit(rules, "/fable 这个架构怎么设计")
	if h.Tier != TierTop || h.Prompt != "这个架构怎么设计" {
		t.Fatalf("got %+v", h)
	}
	h = FindExplicit(rules, "仔细想想这个问题")
	if h.Tier != TierTop {
		t.Fatalf("仔细想 应命中顶配，got %+v", h)
	}
	h = FindExplicit(rules, "/quick 今天几号")
	if h.Tier != TierLow {
		t.Fatalf("got %+v", h)
	}
	h = FindExplicit(rules, "随便聊聊 /sub")
	if !h.SubOnly {
		t.Fatalf("got %+v", h)
	}
}

func TestHeuristic(t *testing.T) {
	sig := Extract("为什么会这样\npanic: runtime error\ngoroutine 1 [running]:\n```go\nfunc main(){}\n```", 0, 100)
	tier, _ := Heuristic(sig)
	if tier != TierHigh {
		t.Fatalf("got %s", tier)
	}
}

func TestParseSeconds(t *testing.T) {
	for in, want := range map[string]int{"做一个 3 秒的标题动画": 3, "做个 12秒 视频": 12, "a 5s clip": 5, "没有时长": 0} {
		if got := ParseSeconds(in); got != want {
			t.Errorf("%q → %d，想要 %d", in, got, want)
		}
	}
}
