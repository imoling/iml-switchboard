package core

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCleanExcerpts(t *testing.T) {
	l, err := OpenLedger(filepath.Join(t.TempDir(), "l.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_ = l.Insert(&Call{ID: "c1", TS: time.Now(), PromptExcerpt: "<session>\n生成图片：一枝红杏出墙来\n</session>"})
	_ = l.Insert(&Call{ID: "c2", TS: time.Now(), PromptExcerpt: "干净的"})
	// 摘录被截断、注入块没闭合，但存了完整原话：按原话重算
	_ = l.Insert(&Call{ID: "c3", TS: time.Now(), PromptExcerpt: `<system-reminder data-role="x"> 很长的环境信息被截`, PromptFull: `<system-reminder data-role="x">很长的环境信息</system-reminder>` + "\n帮我看看这段代码"})
	// 截断且没存原话：只能清空
	_ = l.Insert(&Call{ID: "c4", TS: time.Now(), PromptExcerpt: `<system-reminder data-role="x"> 很长的环境信息被截`})
	_ = l.SaveJob(&Job{ID: "j1", Kind: KindImage, Status: "done", PromptExcerpt: `<system-reminder data-role="x">环境</system-reminder> 画一张海报`})
	n, err := l.CleanExcerpts(StripInjected)
	if err != nil || n != 4 {
		t.Fatalf("应清掉 4 条：%d %v", n, err)
	}
	if c, _ := l.Get("c3"); c.PromptExcerpt != "帮我看看这段代码" {
		t.Fatalf("有原话的应按原话重算：%q", c.PromptExcerpt)
	}
	if c, _ := l.Get("c4"); c.PromptExcerpt != "" {
		t.Fatalf("没原话的截断摘录应清空：%q", c.PromptExcerpt)
	}
	if c, _ := l.Get("c1"); c.PromptExcerpt != "生成图片：一枝红杏出墙来" {
		t.Fatalf("记录摘录没清干净：%q", c.PromptExcerpt)
	}
	jobs, _ := l.LoadJobs()
	if len(jobs) != 1 || jobs[0].PromptExcerpt != "画一张海报" {
		t.Fatalf("媒体任务摘录没清干净：%+v", jobs)
	}
}
