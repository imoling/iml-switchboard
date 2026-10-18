package media

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/lanes/api"
)

// DefaultAgnesBaseURL 是 Agnes AI 的接口地址（国内直连）。
const DefaultAgnesBaseURL = "https://api.agnes-ai.cn/v1"

// ---------- 图片 ----------

// AgnesImage 走 Agnes 的 /v1/images/generations。它的 size 是 1K/2K/3K/4K 档位，比例单独传 ratio，
// 文生图用 return_base64 直接拿 Base64；这些和 OpenAI 原版不同，所以单独实现。
type AgnesImage struct {
	Account api.Account
	Client  *http.Client
}

func (p *AgnesImage) Name() string { return "agnes" }

func (p *AgnesImage) base() string {
	if p.Account.BaseURL != "" {
		return p.Account.BaseURL
	}
	return DefaultAgnesBaseURL
}

// agnesSize 把 1024x1536 这类尺寸换成 Agnes 的档位和比例。
func agnesSize(size string) (tier, ratio string) {
	tier, ratio = "1K", "1:1"
	s := strings.ToLower(strings.TrimSpace(size))
	switch s {
	case "", "1k", "2k", "3k", "4k":
		if s != "" {
			tier = strings.ToUpper(s)
		}
		return
	}
	w, h := 0, 0
	if parts := strings.Split(s, "x"); len(parts) == 2 {
		w, _ = strconv.Atoi(parts[0])
		h, _ = strconv.Atoi(parts[1])
	}
	if w == 0 || h == 0 {
		return
	}
	if max(w, h) >= 2000 {
		tier = "2K"
	}
	r := float64(w) / float64(h)
	switch {
	case r > 2.0:
		ratio = "21:9"
	case r > 1.6:
		ratio = "16:9"
	case r > 1.4:
		ratio = "3:2"
	case r > 1.15:
		ratio = "4:3"
	case r > 0.87:
		ratio = "1:1"
	case r > 0.7:
		ratio = "3:4"
	case r > 0.62:
		ratio = "2:3"
	default:
		ratio = "9:16"
	}
	return
}

func (p *AgnesImage) Generate(ctx context.Context, req ImageRequest) (ImageResult, error) {
	headers, err := bearer(p.Account)
	if err != nil {
		return ImageResult{}, err
	}
	tier, ratio := agnesSize(req.Size)
	if strings.EqualFold(req.Quality, "high") || strings.EqualFold(req.Quality, "hd") {
		tier = "2K"
	}
	body := map[string]interface{}{"model": req.Model, "prompt": req.Prompt, "size": tier, "ratio": ratio, "return_base64": true}
	if req.ImagePath != "" {
		u, err := imageDataURL(req.ImagePath)
		if err != nil {
			return ImageResult{}, err
		}
		body["image"] = []string{u}
	}
	raw, err := doJSON(ctx, p.Client, p.Name(), http.MethodPost, joinURL(p.base(), "/images/generations"), headers, body)
	if err != nil {
		return ImageResult{}, err
	}
	var r struct {
		Data []struct {
			B64 string `json:"b64_json"`
			URL string `json:"url"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return ImageResult{}, &core.ErrProvider{Summary: "Agnes AI 回包无法解析：不是合法 JSON", Raw: string(raw), Retryable: true}
	}
	if r.Error != nil && r.Error.Message != "" {
		return ImageResult{}, &core.ErrProvider{Summary: "Agnes AI 出图失败：" + firstLineOf(r.Error.Message), Raw: string(raw), Retryable: true}
	}
	if len(r.Data) == 0 {
		return ImageResult{}, &core.ErrProvider{Summary: "Agnes AI 出图失败：回包里没有图片", Raw: string(raw), Retryable: true}
	}
	out := ImageResult{Raw: raw, Mime: "image/png"}
	if r.Data[0].B64 != "" {
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(r.Data[0].B64, "data:image/png;base64,"))
		if err != nil {
			return ImageResult{}, &core.ErrProvider{Summary: "Agnes AI 回包的图片数据无法解码", Raw: string(raw), Retryable: true}
		}
		out.Data = data
		return out, nil
	}
	out.URL = r.Data[0].URL
	return out, nil
}

// ---------- 视频 ----------

// AgnesVideo 走 Agnes 的 POST /v1/videos 提交、GET /agnesapi 轮询（注意轮询地址不带 /v1）。
type AgnesVideo struct {
	Account api.Account
	Client  *http.Client
}

func (p *AgnesVideo) Name() string { return "agnes" }

func (p *AgnesVideo) base() string {
	if p.Account.BaseURL != "" {
		return p.Account.BaseURL
	}
	return DefaultAgnesBaseURL
}

// agnesRef 把模型名一起存进任务引用，轮询时要带 model_name。
func agnesRef(id, model string) string { return id + "|" + model }

func splitAgnesRef(ref string) (id, model string) {
	id, model, _ = strings.Cut(ref, "|")
	return
}

func (p *AgnesVideo) Submit(ctx context.Context, req VideoRequest) (string, error) {
	headers, err := bearer(p.Account)
	if err != nil {
		return "", err
	}
	secs := req.Seconds
	if secs < 4 {
		secs = 4
	}
	if secs > 12 {
		secs = 12
	}
	size := "720P"
	if strings.Contains(strings.ToLower(req.Size), "1080") && !strings.HasSuffix(req.Model, "-flash") {
		size = "1080P"
	}
	body := map[string]interface{}{"model": req.Model, "prompt": req.Prompt, "mode": "text", "seconds": strconv.Itoa(secs), "size": size, "aspect_ratio": "16:9", "n": 1}
	if req.ImagePath != "" {
		u, err := imageDataURL(req.ImagePath)
		if err != nil {
			return "", err
		}
		body["mode"] = "keyframe"
		body["first_frame"] = u
	}
	raw, err := doJSON(ctx, p.Client, p.Name(), http.MethodPost, joinURL(p.base(), "/videos"), headers, body)
	if err != nil {
		return "", err
	}
	var r struct {
		VideoID string `json:"video_id"`
		ID      string `json:"id"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || (r.VideoID == "" && r.ID == "") {
		return "", &core.ErrProvider{Summary: "Agnes AI 提交视频任务失败：回包里没有任务 ID", Raw: string(raw), Retryable: true}
	}
	id := r.VideoID
	if id == "" {
		id = r.ID
	}
	return agnesRef(id, req.Model), nil
}

func (p *AgnesVideo) pollURL(ref string) string {
	id, model := splitAgnesRef(ref)
	root := strings.TrimSuffix(strings.TrimSuffix(p.base(), "/"), "/v1")
	q := url.Values{"video_id": {id}}
	if model != "" {
		q.Set("model_name", model)
	}
	return root + "/agnesapi?" + q.Encode()
}

func (p *AgnesVideo) Poll(ctx context.Context, ref string) (VideoStatus, error) {
	headers, err := bearer(p.Account)
	if err != nil {
		return VideoStatus{}, err
	}
	raw, err := doJSON(ctx, p.Client, p.Name(), http.MethodGet, p.pollURL(ref), headers, nil)
	if err != nil {
		return VideoStatus{}, err
	}
	var r struct {
		Status   string  `json:"status"`
		Progress float64 `json:"progress"`
		URL      string  `json:"url"`
		Error    string  `json:"error"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return VideoStatus{}, &core.ErrProvider{Summary: "Agnes AI 任务状态无法解析", Raw: string(raw), Retryable: true}
	}
	st := VideoStatus{Progress: r.Progress / 100}
	switch strings.ToLower(r.Status) {
	case "completed", "succeeded", "success":
		st.Done, st.Progress, st.URL = true, 1, r.URL
		if st.URL == "" {
			st.Error = "任务完成但没有视频地址"
		}
	case "failed", "error", "canceled", "cancelled":
		st.Done = true
		st.Error = firstLineOf(r.Error)
		if st.Error == "" {
			st.Error = "生成失败"
		}
	}
	return st, nil
}

// Cancel：Agnes 没有公开取消接口，只能停止轮询。
func (p *AgnesVideo) Cancel(ctx context.Context, ref string) error { return nil }

func firstLineOf(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return fmt.Sprint(s)
}
