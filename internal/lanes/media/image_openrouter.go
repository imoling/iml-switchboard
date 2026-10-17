package media

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/lanes/api"
)

// DefaultOpenRouterBaseURL 是 OpenRouter 的接口地址。
const DefaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"

// OpenRouterImage 通过 OpenRouter 的 /images 接口出图（如 google/gemini-nano-banana-2.1）。
//
// TODO(未实测)：请求与回包形状按 OpenRouter 文档编写，本机无密钥未真实调用。
type OpenRouterImage struct {
	Account api.Account
	Client  *http.Client
}

// Name 返回供应商名。
func (p *OpenRouterImage) Name() string { return "openrouter" }

type openRouterImageResponse struct {
	Data []struct {
		B64 string `json:"b64_json"`
		URL string `json:"url"`
	} `json:"data"`
	// 兼容 chat 风格的回包
	Choices []struct {
		Message struct {
			Images []struct {
				ImageURL struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"images"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Generate 出图。
func (p *OpenRouterImage) Generate(ctx context.Context, req ImageRequest) (ImageResult, error) {
	headers, err := bearer(p.Account)
	if err != nil {
		return ImageResult{}, err
	}
	base := p.Account.BaseURL
	if base == "" {
		base = DefaultOpenRouterBaseURL
	}
	body := map[string]interface{}{"model": req.Model, "prompt": req.Prompt, "n": 1}
	if req.Size != "" {
		body["size"] = req.Size
	}
	if req.ImagePath != "" {
		u, err := imageDataURL(req.ImagePath)
		if err != nil {
			return ImageResult{}, err
		}
		body["image"] = u
	}
	raw, err := doJSON(ctx, p.Client, p.Name(), http.MethodPost, joinURL(base, "/images"), headers, body)
	if err != nil {
		return ImageResult{}, err
	}
	var r openRouterImageResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return ImageResult{}, &core.ErrProvider{Summary: "openrouter 回包无法解析：不是合法 JSON", Raw: string(raw), Retryable: true}
	}
	if r.Error != nil && r.Error.Message != "" {
		return ImageResult{}, &core.ErrProvider{Summary: "openrouter 出图失败：" + firstLine(r.Error.Message), Raw: string(raw), Retryable: true}
	}
	out := ImageResult{Raw: raw}
	switch {
	case len(r.Data) > 0 && r.Data[0].B64 != "":
		return decodeB64(out, r.Data[0].B64, p.Name())
	case len(r.Data) > 0 && r.Data[0].URL != "":
		return splitURL(out, r.Data[0].URL, p.Name())
	case len(r.Choices) > 0 && len(r.Choices[0].Message.Images) > 0:
		return splitURL(out, r.Choices[0].Message.Images[0].ImageURL.URL, p.Name())
	}
	return ImageResult{}, &core.ErrProvider{Summary: "openrouter 出图失败：回包里没有图片", Raw: string(raw), Retryable: true}
}

// decodeB64 解码 b64 到 Data。
func decodeB64(out ImageResult, b64, provider string) (ImageResult, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return ImageResult{}, &core.ErrProvider{Summary: provider + " 出图失败：图片数据损坏", Raw: err.Error(), Retryable: true}
	}
	out.Data = data
	out.Mime = http.DetectContentType(data)
	return out, nil
}

// splitURL 处理 data URL 或 http URL。
func splitURL(out ImageResult, u, provider string) (ImageResult, error) {
	if strings.HasPrefix(u, "data:") {
		_, b64, ok := strings.Cut(u, ",")
		if !ok {
			return ImageResult{}, &core.ErrProvider{Summary: provider + " 出图失败：图片数据损坏", Raw: u[:min(len(u), 64)], Retryable: true}
		}
		return decodeB64(out, b64, provider)
	}
	out.URL = u
	return out, nil
}
