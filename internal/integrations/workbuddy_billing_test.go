package integrations

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 倍率在 local_storage 的缓存里，值里还嵌着一层 JSON 字符串
func TestWorkBuddyModels(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".workbuddy", "local_storage")
	_ = os.MkdirAll(dir, 0o755)
	body := `{"value":"{\"data\":{\"models\":[{\"id\":\"balanced-model\",\"name\":\"均衡\",\"credits\":\"x0.50\"},{\"id\":\"deepseek-v4-pro\",\"name\":\"DeepSeek-V4-Pro\",\"credits\":\"x0.51 credits\"},{\"id\":\"hy3\",\"credits\":\"\"}]}}"}`
	if err := os.WriteFile(filepath.Join(dir, "entry_x.info"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manager{home: home}
	wbCache.models, wbCache.at = nil, time.Time{}
	got := m.WorkBuddyModels()
	if got["balanced-model"].Mult != 0.50 || got["deepseek-v4-pro"].Mult != 0.51 {
		t.Fatalf("倍率没读对：%+v", got)
	}
	if _, ok := got["hy3"]; ok {
		t.Fatal("没标倍率的不算")
	}
	// 均衡档每百万 token：306 × 0.5 × 0.05 = ¥7.65
	if v := WorkBuddyCNYPerM(0.5); v < 7.6499 || v > 7.6501 {
		t.Fatalf("均衡档单价不对：%v", v)
	}
	wbCache.models, wbCache.at = nil, time.Time{}
}
