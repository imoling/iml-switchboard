package admin

import (
	"testing"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

// 记录分页：带 page 回 {items,total}；来源筛选认应用名；只看失败在后端筛
func TestCallsPaging(t *testing.T) {
	ts, r := newServer(t)
	now := time.Now()
	for i, h := range []string{"codex", "codex", "workbuddy"} {
		c := &core.Call{ID: core.NewID("c"), TS: now.Add(time.Duration(i) * time.Second), Source: "facade", Host: h, Kind: core.KindText, Lane: core.LaneAPI, Model: "gpt-6.1-sol", OK: i != 1}
		if err := r.Ledger().Insert(c); err != nil {
			t.Fatal(err)
		}
	}
	_, m, _ := do(t, ts, "GET", "/api/calls?page=1&size=2", nil)
	if items, _ := m["items"].([]any); len(items) != 2 || m["total"].(float64) != 3 {
		t.Fatalf("第一页应 2 条、共 3 条：%v", m)
	}
	_, m, _ = do(t, ts, "GET", "/api/calls?page=2&size=2", nil)
	if items, _ := m["items"].([]any); len(items) != 1 {
		t.Fatalf("第二页应 1 条：%v", m)
	}
	_, m, _ = do(t, ts, "GET", "/api/calls?page=1&source=codex", nil)
	if m["total"].(float64) != 2 {
		t.Fatalf("来源选 Codex 应 2 条：%v", m["total"])
	}
	_, m, _ = do(t, ts, "GET", "/api/calls?page=1&failed=1", nil)
	if m["total"].(float64) != 1 {
		t.Fatalf("只看失败应 1 条：%v", m["total"])
	}
}
