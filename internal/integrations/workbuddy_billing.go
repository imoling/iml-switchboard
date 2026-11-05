package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WorkBuddy 自带的模型按积分收费：扣的积分 ≈ 306 ×（总 token 百万数）× 倍率，缓存不打折。
// 306 是 workbuddy-usage-stats 用本地 trace 和实扣积分反推的（误差约 ±10%，2026-10）；
// 官方只公布倍率，不公布 1 倍等于多少积分。1 积分按充值包 1000 积分 50 元算。
const (
	WorkBuddyCreditsPerM   = 306.0
	WorkBuddyCNYPerCredit  = 0.05
	WorkBuddyDefaultModel  = "balanced-model" // 它的「均衡」档
	workbuddyDefaultMult   = 0.50             // 读不到本地倍率时用（2026-11-05 本机缓存里的值）
	workbuddyCacheDuration = 10 * time.Minute
)

// WorkBuddyModel 是 WorkBuddy 自带的一个模型和它的积分倍率。
type WorkBuddyModel struct {
	ID   string
	Name string
	Mult float64
}

var wbCache struct {
	sync.Mutex
	at     time.Time
	models map[string]WorkBuddyModel
}

// WorkBuddyModels 读 WorkBuddy 本地缓存的模型倍率（~/.workbuddy/local_storage/entry_*.info 里的 data.models[].credits，形如 "x0.50"）。
// 只读这几个字段，不碰登录信息；十分钟读一次。
func (m *Manager) WorkBuddyModels() map[string]WorkBuddyModel {
	wbCache.Lock()
	defer wbCache.Unlock()
	if wbCache.models != nil && time.Since(wbCache.at) < workbuddyCacheDuration {
		return wbCache.models
	}
	out := map[string]WorkBuddyModel{}
	files, _ := filepath.Glob(filepath.Join(m.home, ".workbuddy", "local_storage", "entry_*.info"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil || !strings.Contains(string(b), "credits") {
			continue
		}
		var doc any
		if json.Unmarshal(b, &doc) != nil {
			continue
		}
		collectWorkBuddyModels(doc, out)
	}
	if _, ok := out[WorkBuddyDefaultModel]; !ok {
		out[WorkBuddyDefaultModel] = WorkBuddyModel{ID: WorkBuddyDefaultModel, Name: "均衡", Mult: workbuddyDefaultMult}
	}
	wbCache.models, wbCache.at = out, time.Now()
	return out
}

// collectWorkBuddyModels 在缓存里找 {id, name, credits} 这样的条目；值里嵌着的 JSON 字符串也拆开找。
func collectWorkBuddyModels(x any, out map[string]WorkBuddyModel) {
	switch v := x.(type) {
	case string:
		if s := strings.TrimSpace(v); strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
			var inner any
			if json.Unmarshal([]byte(s), &inner) == nil {
				collectWorkBuddyModels(inner, out)
			}
		}
	case []any:
		for _, e := range v {
			collectWorkBuddyModels(e, out)
		}
	case map[string]any:
		id, _ := v["id"].(string)
		cr, _ := v["credits"].(string)
		if id != "" && strings.HasPrefix(strings.TrimSpace(cr), "x") {
			f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(cr), "x"))
			if len(f) > 0 {
				if mult, err := strconv.ParseFloat(f[0], 64); err == nil {
					name, _ := v["name"].(string)
					out[id] = WorkBuddyModel{ID: id, Name: name, Mult: mult}
				}
			}
		}
		for _, e := range v {
			collectWorkBuddyModels(e, out)
		}
	}
}

// WorkBuddyCNYPerM 是这个倍率下每百万 token 的人民币价。
func WorkBuddyCNYPerM(mult float64) float64 {
	return WorkBuddyCreditsPerM * mult * WorkBuddyCNYPerCredit
}
