package core

import (
	"encoding/json"
	"testing"
)

// 事件流的 data 必须带外层包装：界面读 e.data.call / e.data.job，发裸对象浮窗会崩成空白
func TestEventsAreWrapped(t *testing.T) {
	b := NewBus()
	ch, cancel := b.Subscribe()
	defer cancel()
	b.PublishCall(&Call{ID: "c1"})
	b.PublishJob(&Job{ID: "j1"})
	var call struct {
		Call struct {
			ID string `json:"id"`
		} `json:"call"`
	}
	env := <-ch
	if env.Type != "call" || json.Unmarshal(env.Data, &call) != nil || call.Call.ID != "c1" {
		t.Fatalf("call 事件格式不对：%s %s", env.Type, env.Data)
	}
	var job struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	env = <-ch
	if env.Type != "job" || json.Unmarshal(env.Data, &job) != nil || job.Job.ID != "j1" {
		t.Fatalf("job 事件格式不对：%s %s", env.Type, env.Data)
	}
}
