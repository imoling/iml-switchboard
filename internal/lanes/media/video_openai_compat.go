package media

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/lanes/api"
)

// OpenAICompatVideo 走 DeepInfra 风格的 /v1/videos 异步接口（与 OpenAI videos 形状兼容）。
//
// TODO(未实测)：POST /videos {model,prompt,seconds,size,input_reference} 返回 {id,status}；
// GET /videos/{id} 返回 {status: queued|in_progress|completed|failed, progress, error}；
// 完成后若回包没有 url，则从 GET /videos/{id}/content 带认证头下载。
type OpenAICompatVideo struct {
	Account api.Account
	Client  *http.Client
}

// Name 返回供应商名（取账号 Provider）。
func (p *OpenAICompatVideo) Name() string {
	if p.Account.Provider != "" {
		return p.Account.Provider
	}
	return "openai"
}

// Submit 建任务。
func (p *OpenAICompatVideo) Submit(ctx context.Context, req VideoRequest) (string, error) {
	headers, err := bearer(p.Account)
	if err != nil {
		return "", err
	}
	body := map[string]interface{}{"model": req.Model, "prompt": req.Prompt}
	if req.Seconds > 0 {
		body["seconds"] = req.Seconds
	}
	if req.Size != "" {
		body["size"] = req.Size
	}
	if req.ImagePath != "" {
		u, err := imageDataURL(req.ImagePath)
		if err != nil {
			return "", err
		}
		body["input_reference"] = u
	}
	raw, err := doJSON(ctx, p.Client, p.Name(), http.MethodPost, joinURL(p.Account.BaseURL, "/videos"), headers, body)
	if err != nil {
		return "", err
	}
	var r struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || r.ID == "" {
		return "", &core.ErrProvider{Summary: p.Name() + " 提交视频任务失败：回包里没有任务 ID", Raw: string(raw), Retryable: true}
	}
	return r.ID, nil
}

// Poll 查任务。
func (p *OpenAICompatVideo) Poll(ctx context.Context, ref string) (VideoStatus, error) {
	headers, err := bearer(p.Account)
	if err != nil {
		return VideoStatus{}, err
	}
	raw, err := doJSON(ctx, p.Client, p.Name(), http.MethodGet, joinURL(p.Account.BaseURL, "/videos/"+ref), headers, nil)
	if err != nil {
		return VideoStatus{}, err
	}
	var g genericVideoStatus
	if err := json.Unmarshal(raw, &g); err != nil {
		return VideoStatus{}, &core.ErrProvider{Summary: p.Name() + " 回包无法解析：不是合法 JSON", Raw: string(raw), Retryable: true}
	}
	st := g.toStatus()
	if st.Done && st.Error == "" && st.URL == "" {
		st.URL = joinURL(p.Account.BaseURL, "/videos/"+ref+"/content")
	}
	return st, nil
}

// Cancel 取消任务。
func (p *OpenAICompatVideo) Cancel(ctx context.Context, ref string) error {
	headers, err := bearer(p.Account)
	if err != nil {
		return err
	}
	_, err = doJSON(ctx, p.Client, p.Name(), http.MethodDelete, joinURL(p.Account.BaseURL, "/videos/"+ref), headers, nil)
	return err
}

// Download 带认证头下载（/content 端点需要）。
func (p *OpenAICompatVideo) Download(ctx context.Context, url, dst string) error {
	var headers map[string]string
	if strings.HasPrefix(url, strings.TrimRight(p.Account.BaseURL, "/")) {
		h, err := bearer(p.Account)
		if err != nil {
			return err
		}
		headers = h
	}
	return download(ctx, p.Client, p.Name(), url, dst, headers)
}
