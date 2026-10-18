package core

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type goldenCase struct {
	P    string `yaml:"p"`
	Want string `yaml:"want"`
}

// 门槛只升不降：规则改好了就把数字往上调，防止以后的改动悄悄变差。
const (
	goldenMinAccuracy = 0.97 // 档位完全对上的比例
	goldenMaxUnder    = 0.02 // 判低了（比该用的档低）的比例，比判高更伤

	// 盲测只量规则本身（不含低档模型判档）。2026-10-18：规则 v1 46.7%，v2 65.0%
	holdoutMinAccuracy = 0.65
	holdoutMaxUnder    = 0.34
)

func loadGolden(t *testing.T, name string) []goldenCase {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var cs []goldenCase
	if err := yaml.Unmarshal(b, &cs); err != nil {
		t.Fatal(err)
	}
	return cs
}

// wantRank 把标注换成可比较的档位；媒体返回 0。
func wantRank(w string) int {
	switch w {
	case "low":
		return 1
	case "mid":
		return 2
	case "high":
		return 3
	}
	return 0
}

// TestGoldenTiers 用金标集给路由打分：均衡模式、无会话，只看判出来的模态和档位。
func TestGoldenTiers(t *testing.T) {
	scoreGolden(t, "golden.yaml", goldenMinAccuracy, goldenMaxUnder)
}

// TestHoldoutTiers 用盲测集打分。门槛比金标松，因为规则没照着它调；数字是对外能说的准确率。
func TestHoldoutTiers(t *testing.T) {
	scoreGolden(t, "holdout.yaml", holdoutMinAccuracy, holdoutMaxUnder)
}

func scoreGolden(t *testing.T, file string, minAcc, maxUnder float64) {
	r, _, _ := newTestRouter(t)
	r.SetMode(ModeBalanced)
	cases := loadGolden(t, file)
	ok, under, over := 0, 0, 0
	var misses []string
	confusion := map[string]int{}
	for _, c := range cases {
		d := r.Decide(&Task{Prompt: c.P, Source: "golden"})
		got := string(d.Kind)
		if d.Kind == KindText {
			got = string(d.Tier)
		}
		confusion[c.Want+"→"+got]++
		if got == c.Want {
			ok++
			continue
		}
		wr, gr := wantRank(c.Want), tierRank(Tier(got))
		kind := "模态不对"
		switch {
		case wr > 0 && gr > 0 && gr < wr:
			under++
			kind = "判低了"
		case wr > 0 && gr > 0 && gr > wr:
			over++
			kind = "判高了"
		case wr > 0 || c.Want != got:
			// 文本被当成媒体，或媒体被当成文本，都按判错算，同时计入判低（该做的事没做成）
			under++
		}
		line := strings.ReplaceAll(c.P, "\n", "⏎")
		if len([]rune(line)) > 50 {
			line = string([]rune(line)[:50]) + "…"
		}
		misses = append(misses, fmt.Sprintf("  %s：想要 %s，得到 %s（%s）· %s", kind, c.Want, got, d.TaskType, line))
	}
	n := float64(len(cases))
	acc, underRate := float64(ok)/n, float64(under)/n
	keys := make([]string, 0, len(confusion))
	for k := range confusion {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var cm []string
	for _, k := range keys {
		cm = append(cm, fmt.Sprintf("%s %d", k, confusion[k]))
	}
	t.Logf("%s %d 条：准确率 %.1f%%，判低了 %d（%.1f%%），判高了 %d", file, len(cases), acc*100, under, underRate*100, over)
	t.Logf("混淆：%s", strings.Join(cm, "，"))
	for _, m := range misses {
		t.Log(m)
	}
	if acc < minAcc || underRate > maxUnder {
		t.Errorf("%s：路由准确率 %.1f%%（门槛 %.0f%%），判低了 %.1f%%（上限 %.0f%%）；加 -v 看明细",
			file, acc*100, minAcc*100, underRate*100, maxUnder*100)
	}
}
