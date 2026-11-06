package themes

import "testing"

// 主题顺序固定：漏写顺序的主题会和标准并列第一，sort.Slice 不稳定，设置里的选项会乱跳
func TestOrderAndWords(t *testing.T) {
	want := []string{"standard", "dianjiang", "academy", "harem", "seaking"}
	got := All()
	if len(got) != len(want) {
		t.Fatalf("主题数不对：%d", len(got))
	}
	for i, th := range got {
		if th.ID != want[i] {
			t.Fatalf("第 %d 个应是 %s，实际 %s", i, want[i], th.ID)
		}
		if th.Words["nav.world"] == "" && th.ID != "standard" || Word(th.ID, "nav.world") == "nav.world" {
			t.Fatalf("%s 缺「学院」页的侧栏名", th.ID)
		}
	}
	if Word("academy", "tier.high") != "教授" || Word("academy", "nav.world") != "彩蛋" {
		t.Fatal("魔法学院词表不对")
	}
}
