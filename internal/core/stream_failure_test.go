package core

import (
	"context"
	"errors"
	"testing"
)

type partialFailureDriver struct{}

func (*partialFailureDriver) ID() LaneID                    { return LaneAPI }
func (*partialFailureDriver) Health(context.Context) Health { return Health{Status: "ok"} }
func (*partialFailureDriver) Run(_ context.Context, _ *Task, _ Target, emit func(Event)) (*Result, error) {
	if emit != nil {
		emit(Event{Type: "text_delta", Text: "半截回答"})
	}
	return nil, errors.New("连接中断")
}

func TestStreamFailureDoesNotAppendFallback(t *testing.T) {
	r, fallback, _ := newTestRouter(t)
	r.Register(&partialFailureDriver{})
	task := &Task{ID: NewID("call"), Kind: KindText, Source: "test"}
	d := &Decision{Kind: KindText, Tier: TierLow, Candidates: []Target{
		{Lane: LaneAPI, Provider: "zhipu", AccountID: "zhipu-1", Model: "glm-5.3-flash"},
		{Lane: LaneClaudeSub, Provider: "anthropic", Model: "haiku"},
	}}
	var text string
	call, _, err := r.Execute(context.Background(), task, d, func(e Event) { text += e.Text })
	if err == nil || call.OK || fallback.calls != 0 || text != "半截回答" {
		t.Fatalf("断流应记失败且不能拼接回退回答：call=%+v err=%v fallback=%d text=%q", call, err, fallback.calls, text)
	}
	stored, err := r.ledger.Get(call.ID)
	if err != nil || stored.OK || stored.Model != "glm-5.3-flash" {
		t.Fatalf("应记录实际失败型号：%+v %v", stored, err)
	}
}
