// Package media 实现媒体渠道：图片、视频、动画。一个驱动内部按 Task.Kind 分发到各供应商。
package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/lanes/api"
)

// ImageRequest 是一次出图请求。
type ImageRequest struct {
	Prompt  string
	Model   string
	Size    string
	Quality string
	// ImagePath 是参考图（图生图），本地路径或 URL。
	ImagePath string
}

// ImageResult 是出图结果：Data 与 URL 至少一个有值。
type ImageResult struct {
	Data []byte
	URL  string
	Mime string
	Raw  json.RawMessage
}

// ImageProvider 是图片供应商接口。
type ImageProvider interface {
	Name() string
	Generate(ctx context.Context, req ImageRequest) (ImageResult, error)
}

// VideoRequest 是一次视频生成请求。
type VideoRequest struct {
	Prompt    string
	Model     string
	Seconds   int
	ImagePath string
	Size      string
}

// VideoStatus 是异步视频任务的状态。
type VideoStatus struct {
	Progress float64
	Done     bool
	URL      string
	Error    string
}

// VideoProvider 是视频供应商接口：提交、轮询、取消。
type VideoProvider interface {
	Name() string
	Submit(ctx context.Context, req VideoRequest) (ref string, err error)
	Poll(ctx context.Context, ref string) (VideoStatus, error)
	Cancel(ctx context.Context, ref string) error
}

// Downloader 由需要带认证头下载产物的供应商实现。
type Downloader interface {
	Download(ctx context.Context, url string, dst string) error
}

// ---- HTTP 小工具 ----

// doJSON 发请求、把非 2xx 映射成一行人话，成功时返回回包字节。
func doJSON(ctx context.Context, client *http.Client, provider string, method, url string, headers map[string]string, body interface{}) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, api.ClassifyNetError(provider, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode/100 != 2 {
		return nil, api.ClassifyHTTPError(provider, resp.StatusCode, b)
	}
	return b, nil
}

// bearer 生成 Authorization 头。
func bearer(acct api.Account) (map[string]string, error) {
	key, err := acct.APIKey()
	if err != nil {
		return nil, err
	}
	h := map[string]string{"Authorization": "Bearer " + key}
	for k, v := range acct.Headers {
		h[k] = v
	}
	return h, nil
}

// imageDataURL 把本地图片转成 data URL；URL 原样返回。
func imageDataURL(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if strings.HasPrefix(p, "data:") || strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p, nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", &core.ErrProvider{Summary: "读取参考图失败：" + p, Raw: err.Error()}
	}
	mt := mime.TypeByExtension(filepath.Ext(p))
	if mt == "" {
		mt = http.DetectContentType(b)
	}
	return "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(b), nil
}

// download 把 URL 下载到 dst；headers 可为空。
func download(ctx context.Context, client *http.Client, provider, url, dst string, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return api.ClassifyNetError(provider, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &core.ErrProvider{Summary: provider + " 下载产物失败：HTTP " + fmt.Sprint(resp.StatusCode), Raw: string(b), Retryable: true}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return &core.ErrProvider{Summary: "无法创建产物目录：" + filepath.Dir(dst), Raw: err.Error()}
	}
	f, err := os.Create(dst)
	if err != nil {
		return &core.ErrProvider{Summary: "无法写入产物：" + dst, Raw: err.Error()}
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return &core.ErrProvider{Summary: provider + " 下载产物中断：" + firstLine(err.Error()), Raw: err.Error(), Retryable: true}
	}
	return f.Close()
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if len(l) > 160 {
				l = l[:160] + "…"
			}
			return l
		}
	}
	return ""
}

// joinURL 拼接地址。
func joinURL(base, path string) string {
	return strings.TrimRight(base, "/") + path
}
