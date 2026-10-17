package media

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/lanes/api"
)

// ArkVideo 通过火山方舟 /contents/generations/tasks 生成视频（doubao-seedance-2-0-fast-260128 等）。
//
// TODO(未实测)：按火山方舟官方文档的形状编写：POST 建任务返回 {id}，GET /tasks/{id} 返回
// {status: queued|running|succeeded|failed|cancelled, content:{video_url}, error:{code,message}}，DELETE 取消。
// 时长与比例作为顶层 duration / ratio 传，若线上要求写在 text 里（"--duration 5"），改 Submit 即可。
type ArkVideo struct {
	Account api.Account
	Client  *http.Client
}

// Name 返回供应商名。
func (p *ArkVideo) Name() string { return "ark" }

func (p *ArkVideo) base() string {
	if p.Account.BaseURL != "" {
		return p.Account.BaseURL
	}
	return DefaultArkBaseURL
}

// Submit 建任务。
func (p *ArkVideo) Submit(ctx context.Context, req VideoRequest) (string, error) {
	headers, err := bearer(p.Account)
	if err != nil {
		return "", err
	}
	content := []map[string]interface{}{{"type": "text", "text": req.Prompt}}
	if req.ImagePath != "" {
		u, err := imageDataURL(req.ImagePath)
		if err != nil {
			return "", err
		}
		content = append(content, map[string]interface{}{"type": "image_url", "image_url": map[string]string{"url": u}, "role": "first_frame"})
	}
	body := map[string]interface{}{"model": req.Model, "content": content, "watermark": false}
	if req.Seconds > 0 {
		body["duration"] = req.Seconds
	}
	if req.Size != "" {
		body["ratio"] = req.Size
	}
	raw, err := doJSON(ctx, p.Client, p.Name(), http.MethodPost, joinURL(p.base(), "/contents/generations/tasks"), headers, body)
	if err != nil {
		return "", err
	}
	var r struct {
		ID    string `json:"id"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || r.ID == "" {
		msg := "回包里没有任务 ID"
		if r.Error != nil && r.Error.Message != "" {
			msg = firstLine(r.Error.Message)
		}
		return "", &core.ErrProvider{Summary: "ark 提交视频任务失败：" + msg, Raw: string(raw), Retryable: true}
	}
	return r.ID, nil
}

// Poll 查任务。
func (p *ArkVideo) Poll(ctx context.Context, ref string) (VideoStatus, error) {
	headers, err := bearer(p.Account)
	if err != nil {
		return VideoStatus{}, err
	}
	raw, err := doJSON(ctx, p.Client, p.Name(), http.MethodGet, joinURL(p.base(), "/contents/generations/tasks/"+ref), headers, nil)
	if err != nil {
		return VideoStatus{}, err
	}
	var r struct {
		Status  string `json:"status"`
		Content struct {
			VideoURL string `json:"video_url"`
		} `json:"content"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return VideoStatus{}, &core.ErrProvider{Summary: "ark 回包无法解析：不是合法 JSON", Raw: string(raw), Retryable: true}
	}
	switch strings.ToLower(r.Status) {
	case "succeeded", "completed", "success":
		return VideoStatus{Progress: 1, Done: true, URL: r.Content.VideoURL}, nil
	case "failed", "error":
		msg := "任务失败"
		if r.Error != nil {
			msg = r.Error.Code + " " + r.Error.Message
		}
		return VideoStatus{Done: true, Error: strings.TrimSpace(msg)}, nil
	case "cancelled", "canceled":
		return VideoStatus{Done: true, Error: "任务已取消"}, nil
	case "running":
		return VideoStatus{Progress: 0.5}, nil
	}
	return VideoStatus{Progress: 0.1}, nil
}

// Cancel 取消任务。
func (p *ArkVideo) Cancel(ctx context.Context, ref string) error {
	headers, err := bearer(p.Account)
	if err != nil {
		return err
	}
	_, err = doJSON(ctx, p.Client, p.Name(), http.MethodDelete, joinURL(p.base(), "/contents/generations/tasks/"+ref), headers, nil)
	return err
}
