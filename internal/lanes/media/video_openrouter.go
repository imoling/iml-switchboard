package media

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/lanes/api"
)

// OpenRouterVideo 通过 OpenRouter /videos 异步任务生成视频（如 google/veo-3.1-lite）。
//
// TODO(未实测)：按 OpenRouter 文档形状编写：POST /videos 返回 {id,status}；GET /videos/{id} 返回
// {status: queued|processing|completed|failed, progress, url | video_url | output:{url}, error}；DELETE 取消。
type OpenRouterVideo struct {
	Account api.Account
	Client  *http.Client
}

// Name 返回供应商名。
func (p *OpenRouterVideo) Name() string { return "openrouter" }

func (p *OpenRouterVideo) base() string {
	if p.Account.BaseURL != "" {
		return p.Account.BaseURL
	}
	return DefaultOpenRouterBaseURL
}

// Submit 建任务。
func (p *OpenRouterVideo) Submit(ctx context.Context, req VideoRequest) (string, error) {
	headers, err := bearer(p.Account)
	if err != nil {
		return "", err
	}
	body := map[string]interface{}{"model": req.Model, "prompt": req.Prompt}
	if req.Seconds > 0 {
		body["duration"] = req.Seconds
	}
	if req.Size != "" {
		body["resolution"] = req.Size
	}
	if req.ImagePath != "" {
		u, err := imageDataURL(req.ImagePath)
		if err != nil {
			return "", err
		}
		body["image_url"] = u
	}
	raw, err := doJSON(ctx, p.Client, p.Name(), http.MethodPost, joinURL(p.base(), "/videos"), headers, body)
	if err != nil {
		return "", err
	}
	var r struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || r.ID == "" {
		return "", &core.ErrProvider{Summary: "openrouter 提交视频任务失败：回包里没有任务 ID", Raw: string(raw), Retryable: true}
	}
	return r.ID, nil
}

type genericVideoStatus struct {
	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
	URL      string  `json:"url"`
	VideoURL string  `json:"video_url"`
	Output   *struct {
		URL string `json:"url"`
	} `json:"output"`
	Error json.RawMessage `json:"error"`
}

func (g genericVideoStatus) url() string {
	switch {
	case g.URL != "":
		return g.URL
	case g.VideoURL != "":
		return g.VideoURL
	case g.Output != nil:
		return g.Output.URL
	}
	return ""
}

func (g genericVideoStatus) errText() string {
	if len(g.Error) == 0 || string(g.Error) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(g.Error, &s) == nil {
		return s
	}
	var m struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	}
	if json.Unmarshal(g.Error, &m) == nil && m.Message != "" {
		return strings.TrimSpace(m.Code + " " + m.Message)
	}
	return string(g.Error)
}

// toStatus 把通用状态字段转成 VideoStatus。
func (g genericVideoStatus) toStatus() VideoStatus {
	prog := g.Progress
	if prog > 1 {
		prog /= 100
	}
	switch strings.ToLower(g.Status) {
	case "completed", "succeeded", "success", "done":
		return VideoStatus{Progress: 1, Done: true, URL: g.url()}
	case "failed", "error", "cancelled", "canceled":
		msg := g.errText()
		if msg == "" {
			msg = "任务" + g.Status
		}
		return VideoStatus{Done: true, Error: msg}
	}
	if prog == 0 && strings.ToLower(g.Status) != "queued" {
		prog = 0.3
	}
	return VideoStatus{Progress: prog}
}

// Poll 查任务。
func (p *OpenRouterVideo) Poll(ctx context.Context, ref string) (VideoStatus, error) {
	headers, err := bearer(p.Account)
	if err != nil {
		return VideoStatus{}, err
	}
	raw, err := doJSON(ctx, p.Client, p.Name(), http.MethodGet, joinURL(p.base(), "/videos/"+ref), headers, nil)
	if err != nil {
		return VideoStatus{}, err
	}
	var g genericVideoStatus
	if err := json.Unmarshal(raw, &g); err != nil {
		return VideoStatus{}, &core.ErrProvider{Summary: "openrouter 回包无法解析：不是合法 JSON", Raw: string(raw), Retryable: true}
	}
	return g.toStatus(), nil
}

// Cancel 取消任务。
func (p *OpenRouterVideo) Cancel(ctx context.Context, ref string) error {
	headers, err := bearer(p.Account)
	if err != nil {
		return err
	}
	_, err = doJSON(ctx, p.Client, p.Name(), http.MethodDelete, joinURL(p.base(), "/videos/"+ref), headers, nil)
	return err
}
