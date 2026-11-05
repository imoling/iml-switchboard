package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
)

// 假 Jev：按描述里的关键字给概率，记被问了几次
type fakeJudge struct {
	calls    int
	prefer   string // 描述里含这个字的给 0.8，其余平分
	err      error
	criteria map[string]string
	task     string
}

func (f *fakeJudge) judge(ctx context.Context, task string, sig map[string]any, criteria map[string]string, ins string) (map[string]float64, error) {
	f.calls++
	f.criteria, f.task = criteria, task
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]float64{}
	hit := ""
	for id, d := range criteria {
		if strings.Contains(d, f.prefer) && hit == "" {
			hit = id
		}
	}
	rest := 0.2 / float64(max(len(criteria)-1, 1))
	for id := range criteria {
		out[id] = rest
	}
	if hit != "" {
		out[hit] = 0.8
	}
	return out, nil
}

func smartRouter(t *testing.T, f *fakeJudge) *Router {
	r, _, _ := newTestRouter(t)
	_ = r.cfg.Update(func(c *config.Config) error {
		c.Routing.Smart.Enabled = true
		c.API[0].Models = map[string][]string{"low": {"glm-5.3-flash"}, "mid": {"glm-5.3"}, "high": {}}
		return nil
	})
	r.SetSmartJudge(f.judge)
	return r
}

func TestSmartPicksByJevAndWeights(t *testing.T) {
	f := &fakeJudge{prefer: "GLM-5.3（中档）"}
	r := smartRouter(t, f)
	tools := []byte(`[{"type":"function"}]`)
	task := &Task{Prompt: "把这段话翻译成英文：你好", Source: "facade", Host: "codex", Tools: tools, Smart: true,
		Messages: []Message{{Role: "user", Content: "把这段话翻译成英文：你好"}}}
	call, _, err := r.Run(context.Background(), task, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Jev 偏中档 0.8，规则判低档：0.8 × 0.7 = 0.56 仍高于低档 0.2 × 1
	if call.Model != "glm-5.3" || call.Tier != TierMid || f.calls != 1 {
		t.Fatalf("应按 Jev 加权后用中档：%s %s 调了 %d 次 %v", call.Model, call.Tier, f.calls, call.Reason)
	}
	// 理由里写的是实际在判的那位：默认本机 Laya，不能一律写成 Jev
	if why := strings.Join(call.Reason, "；"); !strings.Contains(why, "智能分派：Laya ") || strings.Contains(why, "Jev") {
		t.Fatalf("理由应写 Laya：%v", call.Reason)
	}
	// 带工具时订阅渠道不进候选（硬条件先筛，不交给 Jev）
	for _, d := range f.criteria {
		if strings.Contains(d, "订阅") {
			t.Fatalf("带工具的请求不该把订阅给 Jev 选：%v", f.criteria)
		}
	}
	// 同一句、同一组候选：缓存命中，不再问
	task2 := &Task{Prompt: "把这段话翻译成英文：你好", Source: "facade", Host: "codex", Tools: tools, Smart: true,
		Messages: []Message{{Role: "user", Content: "把这段话翻译成英文：你好"}}}
	if _, _, err := r.Run(context.Background(), task2, nil); err != nil || f.calls != 1 {
		t.Fatalf("缓存命中不该再问 Jev：%d %v", f.calls, err)
	}
	// 工具回传的后续回合不问
	task3 := &Task{Prompt: "另一句话：总结一下", Source: "facade", Host: "codex", Tools: tools, Smart: true,
		Messages: []Message{{Role: "user", Content: "另一句话：总结一下"}, {Role: "assistant", ToolCalls: []byte(`[{}]`)}, {Role: "tool", Content: "ok"}}}
	if _, _, err := r.Run(context.Background(), task3, nil); err != nil || f.calls != 1 {
		t.Fatalf("工具回传的回合不该问 Jev：%d %v", f.calls, err)
	}
}

func TestSmartFallsBack(t *testing.T) {
	f := &fakeJudge{err: errors.New("Jev 超时没回")}
	r := smartRouter(t, f)
	d := r.DecideSmart(context.Background(), &Task{Prompt: "把这段话翻译成英文：你好"}, false)
	if d.Smart == nil || !strings.Contains(d.Smart.Note, "按规则走") || d.Candidates[0].Model != "glm-5.3-flash" {
		t.Fatalf("判官出错应按规则走：%+v %v", d.Smart, d.Candidates)
	}
	// 没打开：不问
	_ = r.cfg.Update(func(c *config.Config) error { c.Routing.Smart.Enabled = false; return nil })
	f.err, f.calls = nil, 0
	d = r.DecideSmart(context.Background(), &Task{Prompt: "随便一句别的"}, false)
	if f.calls != 0 || d.Smart == nil || !strings.Contains(d.Smart.Note, "没打开") {
		t.Fatalf("没打开不该问 Jev：%d %+v", f.calls, d.Smart)
	}
	// 指定档位：不问
	_ = r.cfg.Update(func(c *config.Config) error { c.Routing.Smart.Enabled = true; return nil })
	d = r.DecideSmart(context.Background(), &Task{Prompt: "再换一句", Tier: TierMid}, false)
	if f.calls != 0 {
		t.Fatalf("指定了档位不该问 Jev：%d", f.calls)
	}
	// 媒体请求：不问
	d = r.DecideSmart(context.Background(), &Task{Prompt: "帮我画一张海浪的图"}, false)
	if f.calls != 0 || d.Kind != KindImage {
		t.Fatalf("媒体请求直接走媒体渠道：%d %s", f.calls, d.Kind)
	}
}

// 判官型号：按量付费渠道里挑一个快的，让它给候选打分
type scoreDriver struct {
	fakeDriver
	judged int
}

func (d *scoreDriver) Run(ctx context.Context, t *Task, tg Target, emit func(Event)) (*Result, error) {
	if t.Source == "classifier" && strings.Contains(t.Prompt, "模型调度员") {
		d.judged++
		// 给描述里带「GLM-5.3（中档）」的那个编号打高分
		id := "1"
		for _, line := range strings.Split(t.Prompt, "\n") {
			if strings.Contains(line, "GLM-5.3（中档）") {
				id = strings.SplitN(line, ".", 2)[0]
			}
		}
		return &Result{Text: "好的：{\"" + id + "\": 0.9, \"9\": 0.1}", Usage: Usage{CostUSD: 0.0001}}, nil
	}
	return d.fakeDriver.Run(ctx, t, tg, emit)
}

func TestSmartModelJudge(t *testing.T) {
	r, _, _ := newTestRouter(t)
	d := &scoreDriver{fakeDriver: fakeDriver{id: LaneAPI}}
	r.Register(d)
	_ = r.cfg.Update(func(c *config.Config) error {
		c.Routing.Smart = config.SmartDispatch{Enabled: true, Judge: "model", JudgeAccount: "zhipu-1", JudgeModel: "glm-5.3-flash"}
		c.API[0].Models = map[string][]string{"low": {"glm-5.3-flash"}, "mid": {"glm-5.3"}, "high": {}}
		return nil
	})
	tools := []byte(`[{"type":"function"}]`)
	call, _, err := r.Run(context.Background(), &Task{Prompt: "解释一下这个正则为什么会回溯", Source: "facade", Host: "codex", Tools: tools, Smart: true,
		Messages: []Message{{Role: "user", Content: "解释一下这个正则为什么会回溯"}}}, nil)
	if err != nil || d.judged != 1 || call.Model != "glm-5.3" {
		t.Fatalf("判官型号应选中档 glm-5.3：%v %d %s %v", err, d.judged, call.Model, call.Reason)
	}
}

func TestParseScores(t *testing.T) {
	p, err := parseScores("分数如下 {\"1\": 2, \"2\": 6, \"x\": 9}", []string{"1", "2", "3"})
	if err != nil || p["2"] < 0.749 || p["2"] > 0.751 || p["3"] != 0 {
		t.Fatalf("应归一化、缺的记 0：%v %v", p, err)
	}
	if _, err := parseScores("我觉得第二个好", []string{"1", "2"}); err == nil {
		t.Fatal("没有 JSON 应报错")
	}
}

// 分派方式选了智能分派：自动分档的请求不用点名 router-smart 也请判官挑；指定了档位的照指定的走
func TestSmartAppliesToAuto(t *testing.T) {
	f := &fakeJudge{prefer: "GLM-5.3（中档）"}
	r := smartRouter(t, f)
	tools := []byte(`[{"type":"function"}]`)
	call, _, err := r.Run(context.Background(), &Task{Prompt: "把这段话翻译成英文：早上好", Source: "facade", Host: "codex", Tools: tools,
		Messages: []Message{{Role: "user", Content: "把这段话翻译成英文：早上好"}}}, nil)
	if err != nil || f.calls != 1 || call.Model != "glm-5.3" {
		t.Fatalf("自动分档也应请判官：%v 调了 %d 次 %s", err, f.calls, call.Model)
	}
	if _, _, err := r.Run(context.Background(), &Task{Prompt: "把这段话翻译成英文：晚安", Source: "mcp", Tier: TierLow, Tools: tools,
		Messages: []Message{{Role: "user", Content: "把这段话翻译成英文：晚安"}}}, nil); err != nil || f.calls != 1 {
		t.Fatalf("指定了档位不该问判官：%v %d", err, f.calls)
	}
}
