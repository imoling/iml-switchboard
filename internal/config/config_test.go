package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// 老配置（没有 rules_version）升级：没改过的内置行换新规则，改过档位的保留档位，新类型补进来，删掉的旧行不复活。
func TestMergeRulesUpgradesOldConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IMLS_DATA_DIR", dir)
	old := Default()
	old.Routing.RulesVersion = 0
	var table []TaskType
	for _, r := range old.Routing.TaskTable {
		switch r.Type {
		case "hard_problem", "code_write", "tech_issue", "explain":
			continue // 旧版没有这几行
		case "summarize":
			continue // 用户删掉了
		case "animation":
			r.Match.Exclude = nil
			r.Tier = ""
		case "review":
			r.Tier = "high" // 用户改过档位
			r.Match.Exclude = nil
		case "rewrite":
			r.Hint = "我的说明" // 用户改过识别说明
			r.Match = Match{Keywords: []string{"我的说明"}}
		}
		table = append(table, r)
	}
	old.Routing.TaskTable = table
	b, _ := yaml.Marshal(old)
	s := strings.Replace(string(b), "rules_version: 0\n", "", 1)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]TaskType{}
	for _, r := range st.Get().Routing.TaskTable {
		got[r.Type] = r
	}
	for _, typ := range []string{"hard_problem", "code_write", "tech_issue", "explain"} {
		if _, ok := got[typ]; !ok {
			t.Errorf("新类型 %s 没补进来", typ)
		}
	}
	if _, ok := got["summarize"]; ok {
		t.Error("用户删掉的行不该复活")
	}
	if len(got["animation"].Match.Exclude) == 0 {
		t.Error("没改过的内置行应换成带排除词的新规则")
	}
	if got["review"].Tier != "high" {
		t.Errorf("用户改的档位被覆盖了：%s", got["review"].Tier)
	}
	if kw := got["rewrite"].Match.Keywords; len(kw) != 1 || kw[0] != "我的说明" {
		t.Errorf("用户改过的识别说明被覆盖了：%v", kw)
	}
	if st.Get().Routing.RulesVersion != RulesVersion {
		t.Errorf("版本号没更新：%d", st.Get().Routing.RulesVersion)
	}
	// 合并已落盘：删掉一个新行后重开，不该再补回来
	_ = st.Update(func(c *Config) error {
		var keep []TaskType
		for _, r := range c.Routing.TaskTable {
			if r.Type != "explain" {
				keep = append(keep, r)
			}
		}
		c.Routing.TaskTable = keep
		return nil
	})
	st2, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range st2.Get().Routing.TaskTable {
		if r.Type == "explain" {
			t.Error("升级后删掉的新行，重开又补回来了")
		}
	}
}

func TestMoney(t *testing.T) {
	usd := Settings{USDPerCNY: 1.0 / 7.1}
	cny := Settings{USDPerCNY: 1.0 / 7.1, Currency: "CNY"}
	for _, c := range []struct {
		st   Settings
		in   float64
		want string
	}{
		{usd, 30, "$30.00"}, {usd, 0, "$0"}, {usd, 0.0015, "$0.0015"}, {usd, 1.5, "$1.50"},
		{cny, 30, "¥213.00"}, {cny, 0, "¥0"}, {cny, 0.0003, "¥0.0021"}, {cny, 1.5, "¥10.65"},
	} {
		if got := c.st.Money(c.in); got != c.want {
			t.Errorf("%s %v → %s，想要 %s", c.st.Currency, c.in, got, c.want)
		}
	}
	// 人民币账号：显示人民币时用账单原价，显示美元时用美元
	if got := cny.MoneyNative(0.1, 0.72, "CNY"); got != "¥0.72" {
		t.Errorf("got %s", got)
	}
	if got := usd.MoneyNative(0.1, 0.72, "CNY"); got != "$0.10" {
		t.Errorf("got %s", got)
	}
}

func TestCheckHostKey(t *testing.T) {
	for _, c := range []struct {
		got, host string
		ok        bool
	}{
		{"imls-k", "", true}, {"imls-k.pi", "pi", true}, {"imls-k.deepseek_harness", "deepseek_harness", true},
		{"imls-k.", "", false}, {"imls-kx", "", false}, {"x.imls-k.pi", "", false}, {"imls-k.Pi", "", false}, {"imls-k.a/b", "", false}, {"", "", false},
	} {
		host, ok := CheckHostKey(c.got, "imls-k")
		if host != c.host || ok != c.ok {
			t.Errorf("%q → %q %v，想要 %q %v", c.got, host, ok, c.host, c.ok)
		}
	}
	if _, ok := CheckHostKey("anything", ""); ok {
		t.Error("没设密钥时一律拒绝")
	}
}
