package core

import "testing"

// 写作、自动化、记忆整理三类内置规则，以及启发式兜底的细分名字。
func TestBuiltinWritingAutomationRules(t *testing.T) {
	r, _, _ := newTestRouter(t)
	cases := []struct {
		prompt, want string
		tier         Tier
	}{
		{"补充两三页，关于WorkBuddy爆火现象和分析", "writing", TierMid},
		{"做个三页示例ppt", "writing", TierMid},
		{"帮我写一篇公众号文章，讲讲这周的产品更新", "writing", TierMid},
		{"Automation: ManagerZone 每日友谊赛检查\nAutomation ID: managerzone-2\n检查今天的比赛并汇报", "automation", TierMid},
		{"Analyze this rollout and produce JSON with `raw_memory`, `rollout_summary` fields.", "housekeeping", TierLow},
	}
	for _, c := range cases {
		d := r.Decide(&Task{Prompt: c.prompt, Source: "facade"})
		if d.TaskType != c.want || d.Tier != c.tier {
			t.Errorf("%q → %s/%s，想要 %s/%s（%v）", c.prompt, d.TaskType, d.Tier, c.want, c.tier, d.Reason)
		}
	}
	// 「报错」不算写作
	if d := r.Decide(&Task{Prompt: "这个报告导出功能报错了，栈在下面", Source: "facade"}); d.TaskType == "writing" {
		t.Errorf("带报错的不该算写作：%+v", d.Reason)
	}
}

func TestHeuristicTypeNames(t *testing.T) {
	r, _, _ := newTestRouter(t)
	long := "请结合上面的材料给出判断，并说明理由；注意必须覆盖所有要求、不能省略任何一条、输出按模板。" + big(2000)
	d := r.Decide(&Task{Prompt: long, Source: "facade"})
	if !IsHeuristic(d.TaskType) || d.TaskType != "heuristic_long" {
		t.Fatalf("长消息应是 heuristic_long：%s %v", d.TaskType, d.Reason)
	}
	d = r.Decide(&Task{Prompt: long, Source: "facade", Tools: []byte(`[{"type":"function"}]`)})
	if d.TaskType != "heuristic_tools" || r.TaskName(d.TaskType) != "带工具的回合" {
		t.Fatalf("带工具的回合：%s %s", d.TaskType, r.TaskName(d.TaskType))
	}
	if r.TaskName("heuristic") != "未归类" || r.TaskName("heuristic_short") != "短消息" {
		t.Fatal("启发式名字表不对")
	}
}
