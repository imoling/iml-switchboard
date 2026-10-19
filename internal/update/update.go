// Package update 到 GitHub Releases 查有没有新版本，连不上就查 Gitee 镜像。只查、只给下载地址，不自己替换程序。
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Repo 是发布所在的仓库。
const Repo = "imoling/iml-switchboard"

// Info 是一次检查的结果。
type Info struct {
	Current     string    `json:"current"`
	Latest      string    `json:"latest"`
	HasUpdate   bool      `json:"has_update"`
	URL         string    `json:"url"`  // 本平台安装包的下载地址；没匹配到就是发布页
	Page        string    `json:"page"` // 发布页
	Notes       string    `json:"notes,omitempty"`
	PublishedAt time.Time `json:"published_at"`
	CheckedAt   time.Time `json:"checked_at"`
	Source      string    `json:"source"` // github 或 gitee
}

// Source 是一个发布源：接口地址，以及没给发布页时怎么拼。
type Source struct {
	Name string
	API  string
	Page func(tag string) string
}

// DefaultSources 先 GitHub，连不上再 Gitee 镜像（镜像只放最新一个发布的安装包）。
func DefaultSources() []Source {
	return []Source{
		{Name: "github", API: "https://api.github.com/repos/" + Repo + "/releases?per_page=10", Page: func(tag string) string { return "https://github.com/" + Repo + "/releases/tag/" + tag }},
		{Name: "gitee", API: "https://gitee.com/api/v5/repos/" + Repo + "/releases?per_page=10", Page: func(tag string) string { return "https://gitee.com/" + Repo + "/releases/tag/" + tag }},
	}
}

// Checker 带缓存地查最新发布。
type Checker struct {
	Client  *http.Client
	Current string
	// Sources 是依次尝试的发布源；空用 DefaultSources。
	Sources []Source
	// GOOS、GOARCH 便于测试指定平台；空串用本机。
	GOOS, GOARCH string
	Now          func() time.Time

	mu      sync.Mutex
	cached  *Info
	cacheAt time.Time
}

// TTL 是缓存时长：一天查一次足够，手动点「检查」会绕过。
const TTL = 6 * time.Hour

type release struct {
	TagName     string    `json:"tag_name"`
	Draft       bool      `json:"draft"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	CreatedAt   time.Time `json:"created_at"` // Gitee 没有 published_at
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Check 返回最新发布；force 为假时在缓存期内直接返回上次结果。
func (c *Checker) Check(ctx context.Context, force bool) (*Info, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	c.mu.Lock()
	if !force && c.cached != nil && now().Sub(c.cacheAt) < TTL {
		defer c.mu.Unlock()
		return c.cached, nil
	}
	c.mu.Unlock()

	sources := c.Sources
	if len(sources) == 0 {
		sources = DefaultSources()
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	var info *Info
	var lastErr error
	for _, src := range sources {
		info, lastErr = c.fetch(ctx, client, src, now())
		if lastErr == nil {
			break
		}
	}
	if info == nil {
		return nil, lastErr
	}
	c.mu.Lock()
	c.cached, c.cacheAt = info, now()
	c.mu.Unlock()
	return info, nil
}

// fetch 从一个源取最新发布。
func (c *Checker) fetch(ctx context.Context, client *http.Client, src Source, at time.Time) (*Info, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", src.API, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Switchboard/"+c.Current)
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("连不上 " + src.Name + "：" + shortErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s 返回 %d", src.Name, resp.StatusCode)
	}
	var rels []release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rels); err != nil {
		return nil, errors.New(src.Name + " 回包无法解析")
	}
	goos, goarch := c.GOOS, c.GOARCH
	if goos == "" {
		goos, goarch = runtime.GOOS, runtime.GOARCH
	}
	for _, r := range rels {
		if r.Draft || !strings.HasPrefix(r.TagName, "v") {
			continue
		}
		info := &Info{Current: c.Current, CheckedAt: at, Source: src.Name, Latest: strings.TrimPrefix(r.TagName, "v"), Notes: r.Body, PublishedAt: r.PublishedAt}
		if info.PublishedAt.IsZero() {
			info.PublishedAt = r.CreatedAt
		}
		info.Page = r.HTMLURL
		if info.Page == "" && src.Page != nil {
			info.Page = src.Page(r.TagName)
		}
		info.URL = info.Page
		info.HasUpdate = Newer(info.Latest, c.Current)
		for _, a := range r.Assets {
			if matchAsset(a.Name, goos, goarch) {
				info.URL = a.URL
				break
			}
		}
		return info, nil
	}
	return nil, errors.New(src.Name + " 上还没有发布")
}

// matchAsset 按平台挑安装包：macOS 按芯片分 dmg，Windows 用 exe 安装程序，Linux 用 AppImage。
func matchAsset(name, goos, goarch string) bool {
	n := strings.ToLower(name)
	switch goos {
	case "darwin":
		if !strings.HasSuffix(n, ".dmg") {
			return false
		}
		if goarch == "arm64" {
			return strings.Contains(n, "aarch64") || strings.Contains(n, "arm64")
		}
		return strings.Contains(n, "x64") || strings.Contains(n, "x86_64") || strings.Contains(n, "amd64")
	case "windows":
		return strings.HasSuffix(n, "setup.exe") || strings.HasSuffix(n, ".exe")
	case "linux":
		return strings.HasSuffix(n, ".appimage")
	}
	return false
}

// Newer 判断 a 是否比 b 新；只比较点分数字，后缀（-beta.1）忽略。
func Newer(a, b string) bool {
	pa, pb := parse(a), parse(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parse(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	return s
}
