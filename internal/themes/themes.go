// Package themes 内置四套界面词表。费用、额度、报错、给宿主的工具返回永远不走词表。
package themes

import (
	"embed"
	"encoding/json"
	"sort"
)

//go:embed *.json
var files embed.FS

// Theme 是一套词表。
type Theme struct {
	ID    string            `json:"id"`
	Name  string            `json:"name"`
	Words map[string]string `json:"words"`
}

var all map[string]Theme

func init() {
	all = map[string]Theme{}
	entries, _ := files.ReadDir(".")
	for _, e := range entries {
		b, err := files.ReadFile(e.Name())
		if err != nil {
			continue
		}
		var t Theme
		if json.Unmarshal(b, &t) == nil && t.ID != "" {
			all[t.ID] = t
		}
	}
}

// All 按固定顺序返回全部主题。
func All() []Theme {
	order := map[string]int{"standard": 0, "dianjiang": 1, "harem": 2, "seaking": 3}
	out := make([]Theme, 0, len(all))
	for _, t := range all {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return order[out[i].ID] < order[out[j].ID] })
	return out
}

// Word 取词：先当前主题，再标准，再 key 本身。
func Word(theme, key string) string {
	if t, ok := all[theme]; ok {
		if w, ok := t.Words[key]; ok {
			return w
		}
	}
	if w, ok := all["standard"].Words[key]; ok {
		return w
	}
	return key
}

// Valid 判断主题 id 是否存在。
func Valid(id string) bool { _, ok := all[id]; return ok }
