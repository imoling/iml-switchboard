package admin

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/config"
	"github.com/imoling/iml-switchboard/internal/core"
)

// 界面新建一条交给画师的规则：存成媒体行、排在内置行前面、带上排除词；短句也能命中，不被「短问答」截走
func TestPutRulesCustomMediaRow(t *testing.T) {
	ts, r := newServer(t)
	_, m, _ := do(t, ts, "GET", "/api/rules", nil)
	rows := m["task_table"].([]any)
	row := map[string]any{"task_type": "拼豆图纸", "how": "拼豆、像素头像", "tier": "low", "lanes": []string{"api"}, "kind": "image", "media_tier": "best"}
	rows = append([]any{row}, rows...)
	if code, m, _ := do(t, ts, "PUT", "/api/rules", map[string]any{"task_table": rows}); code != 200 {
		t.Fatalf("保存失败：%d %v", code, m)
	}
	table := r.Config().Get().Routing.TaskTable
	got := table[0]
	if got.Type != "拼豆图纸" || got.Kind != "image" || got.MediaTier != "best" || got.Order != config.CustomOrder || len(got.Match.Exclude) == 0 {
		t.Fatalf("新建的媒体行不对：%+v", got)
	}
	tt, ok := core.MatchTaskType(table, "把这个头像做成拼豆", core.Extract("把这个头像做成拼豆", 0, 0))
	if !ok || tt.Type != "拼豆图纸" {
		t.Fatalf("短句应命中自定义规则，得到 %q", tt.Type)
	}
	if tt, _ := core.MatchTaskType(table, "拼豆图片上传接口报 500", core.Extract("拼豆图片上传接口报 500", 0, 0)); tt.Type == "拼豆图纸" {
		t.Fatal("带技术词的句子不该当成出图")
	}
	// 改回文字：去掉媒体排除词，档位落到低档
	_, m, _ = do(t, ts, "GET", "/api/rules", nil)
	rows = m["task_table"].([]any)
	rows[0].(map[string]any)["kind"] = ""
	if code, m, _ := do(t, ts, "PUT", "/api/rules", map[string]any{"task_table": rows}); code != 200 {
		t.Fatalf("保存失败：%d %v", code, m)
	}
	got = r.Config().Get().Routing.TaskTable[0]
	if got.Kind != "" || got.Tier != "low" || len(got.Match.Exclude) != 0 {
		t.Fatalf("改回文字后不对：%+v", got)
	}
	// 内置行的模态改不了
	for _, x := range rows {
		if x.(map[string]any)["task_type"] == "image" {
			x.(map[string]any)["kind"] = ""
			x.(map[string]any)["media_tier"] = "cheap"
		}
	}
	do(t, ts, "PUT", "/api/rules", map[string]any{"task_table": rows})
	for _, x := range r.Config().Get().Routing.TaskTable {
		if x.Type == "image" && (x.Kind != "image" || x.MediaTier != "cheap") {
			t.Fatalf("内置生图行只能改媒体档位：%+v", x)
		}
	}
}

// 参考图：相对路径按项目目录算，文件不在就报出路径，不发请求
func TestResolveRefs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveRefs(dir, []string{"", "a.png", "https://x.test/b.png"})
	if err != nil || len(got) != 2 || got[0] != filepath.Join(dir, "a.png") || got[1] != "https://x.test/b.png" {
		t.Fatalf("解析不对：%v %v", got, err)
	}
	if _, err := resolveRefs(dir, []string{"nope.png"}); err == nil || !strings.Contains(err.Error(), "nope.png") {
		t.Fatalf("文件不在要报出路径：%v", err)
	}
}

// 出图顺序：GET 回全部能选的，PUT 保存；不认识的丢掉；空表回到默认
func TestMediaOrderAPI(t *testing.T) {
	ts, r := newServer(t)
	_ = r.Config().Update(func(c *config.Config) error {
		c.API = append(c.API, config.APIAccount{ID: "gw", Provider: "custom", Enabled: true, AllowPaid: true, Media: map[string]string{"image": "gpt-image-2.5-flare"},
			Discovered: []config.DiscoveredModel{{ID: "gpt-image-2.5-flare"}, {ID: "gpt-image-1.5"}}})
		return nil
	})
	_, m, _ := do(t, ts, "GET", "/api/rules", nil)
	img := m["media_order"].(map[string]any)["image"].(map[string]any)
	if img["custom"] != false || len(img["picks"].([]any)) < 2 {
		t.Fatalf("没排时应列出全部、不算手排：%v", img)
	}
	code, m, _ := do(t, ts, "PUT", "/api/rules", map[string]any{"media_picks": map[string][]string{"image": {"gw/gpt-image-1.5", "bogus/x", "gw/gpt-image-2.5-flare"}}})
	if code != 200 {
		t.Fatalf("%d %v", code, m)
	}
	if got := r.Config().Get().Routing.MediaOrder["image"]; !slices.Equal(got, []string{"gw/gpt-image-1.5", "gw/gpt-image-2.5-flare"}) {
		t.Fatalf("应按顺序存、丢掉不认识的：%v", got)
	}
	do(t, ts, "PUT", "/api/rules", map[string]any{"media_picks": map[string][]string{"image": {}}})
	if _, ok := r.Config().Get().Routing.MediaOrder["image"]; ok {
		t.Fatal("空表应回到默认")
	}
}

// 整张规则原样存回（GET 回的 media_order 是表）不能报错：改别的设置时界面就是这么存的
func TestPutRulesRoundTrip(t *testing.T) {
	ts, _ := newServer(t)
	_, m, _ := do(t, ts, "GET", "/api/rules", nil)
	if code, body, _ := do(t, ts, "PUT", "/api/rules", m); code != 200 {
		t.Fatalf("原样存回应成功：%d %v", code, body)
	}
}

// 候选表只列有模型服务的：没添加账号的出图供应商不列出来
func TestMediaOrderHidesNoService(t *testing.T) {
	ts, r := newServer(t)
	_ = r.Config().Update(func(c *config.Config) error {
		c.API = append(c.API, config.APIAccount{ID: "gw", Provider: "custom", Enabled: true, AllowPaid: true, Media: map[string]string{"image": "gpt-image-2.5-flare"}})
		return nil
	})
	_, m, _ := do(t, ts, "GET", "/api/rules", nil)
	for _, p := range m["media_order"].(map[string]any)["image"].(map[string]any)["picks"].([]any) {
		pk := p.(map[string]any)
		if note, _ := pk["note"].(string); strings.HasPrefix(note, "未添加") {
			t.Fatalf("没有账号的不该列出来：%v", pk)
		}
	}
}
