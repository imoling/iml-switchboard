package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"0.2.0", "0.1.0", true}, {"v0.1.1", "0.1.0", true}, {"0.1.0", "0.1.0", false}, {"0.1.0-beta.2", "0.1.0", false}, {"1.0.0", "0.9.9", true}, {"0.1.0", "0.2.0", false}} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q,%q)=%v", c.a, c.b, got)
		}
	}
}

func TestCheckPicksPlatformAssetAndCaches(t *testing.T) {
	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(`[
		 {"tag_name":"v0.3.0","draft":true,"html_url":"https://x/draft","assets":[]},
		 {"tag_name":"v0.2.0","draft":false,"prerelease":true,"html_url":"https://x/v0.2.0","body":"修了托盘","published_at":"2026-10-21T00:00:00Z",
		  "assets":[{"name":"Switchboard_0.2.0_x64.dmg","browser_download_url":"https://x/x64.dmg"},
		            {"name":"Switchboard_0.2.0_aarch64.dmg","browser_download_url":"https://x/arm.dmg"},
		            {"name":"Switchboard_0.2.0_x64-setup.exe","browser_download_url":"https://x/setup.exe"},
		            {"name":"Switchboard_0.2.0_amd64.AppImage","browser_download_url":"https://x/app.AppImage"}]},
		 {"tag_name":"v0.1.0","draft":false,"html_url":"https://x/v0.1.0","assets":[]}]`))
	}))
	defer ts.Close()
	now := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
	c := &Checker{Current: "0.1.0", Sources: []Source{{Name: "github", API: ts.URL}}, GOOS: "darwin", GOARCH: "arm64", Now: func() time.Time { return now }}
	info, err := c.Check(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !info.HasUpdate || info.Latest != "0.2.0" || info.URL != "https://x/arm.dmg" || info.Page != "https://x/v0.2.0" || info.Notes != "修了托盘" {
		t.Fatalf("%+v", info)
	}
	if _, _ = c.Check(context.Background(), false); hits != 1 {
		t.Fatalf("缓存期内不该再查：%d", hits)
	}
	if _, _ = c.Check(context.Background(), true); hits != 2 {
		t.Fatalf("手动检查要绕过缓存：%d", hits)
	}
	for _, p := range []struct{ goos, goarch, want string }{{"darwin", "amd64", "https://x/x64.dmg"}, {"windows", "amd64", "https://x/setup.exe"}, {"linux", "amd64", "https://x/app.AppImage"}} {
		c := &Checker{Current: "0.1.0", Sources: []Source{{Name: "github", API: ts.URL}}, GOOS: p.goos, GOARCH: p.goarch}
		if info, _ := c.Check(context.Background(), true); info.URL != p.want {
			t.Errorf("%s/%s 应选 %s，得到 %s", p.goos, p.goarch, p.want, info.URL)
		}
	}
	same := &Checker{Current: "0.2.0", Sources: []Source{{Name: "github", API: ts.URL}}, GOOS: "darwin", GOARCH: "arm64"}
	if info, _ := same.Check(context.Background(), true); info.HasUpdate {
		t.Fatal("已是最新不该提示更新")
	}
}

func TestFallsBackToGitee(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer dead.Close()
	gitee := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Gitee 的回包：没有 draft、html_url、published_at
		w.Write([]byte(`[{"tag_name":"v0.2.0","name":"Switchboard v0.2.0","prerelease":true,"body":"修了托盘","created_at":"2026-10-21T08:00:00+08:00",
		  "assets":[{"name":"Switchboard_0.2.0_aarch64.dmg","browser_download_url":"https://gitee.com/x/arm.dmg"},{"name":"v0.2.0.zip","browser_download_url":"https://gitee.com/x/src.zip"}]}]`))
	}))
	defer gitee.Close()
	c := &Checker{Current: "0.1.0", GOOS: "darwin", GOARCH: "arm64", Sources: []Source{
		{Name: "github", API: dead.URL},
		{Name: "gitee", API: gitee.URL, Page: func(tag string) string { return "https://gitee.com/x/releases/tag/" + tag }},
	}}
	info, err := c.Check(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if info.Source != "gitee" || !info.HasUpdate || info.URL != "https://gitee.com/x/arm.dmg" || info.Page != "https://gitee.com/x/releases/tag/v0.2.0" || info.PublishedAt.IsZero() {
		t.Fatalf("%+v", info)
	}
	both := &Checker{Current: "0.1.0", Sources: []Source{{Name: "github", API: dead.URL}, {Name: "gitee", API: dead.URL}}}
	if _, err := both.Check(context.Background(), true); err == nil {
		t.Fatal("两个源都挂了应报错")
	}
}
