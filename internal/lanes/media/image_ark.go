package media

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/lanes/api"
)

// DefaultArkBaseURL 是火山方舟的接口地址。
const DefaultArkBaseURL = "https://ark.cn-beijing.volces.com/api/v3"

// ArkImage 通过火山方舟 /images/generations 出图（doubao-seedream-5-0-flash-260915 / doubao-seedream-5-0-pro-260628）。
//
// TODO(未实测)：按火山方舟官方文档的形状编写（response_format 可为 url 或 b64_json，data[] 里对应字段），本机无密钥未真实调用。
type ArkImage struct {
	Account api.Account
	Client  *http.Client
}

// Name 返回供应商名。
func (p *ArkImage) Name() string { return "ark" }

type arkImageResponse struct {
	Model string `json:"model"`
	Data  []struct {
		URL string `json:"url"`
		B64 string `json:"b64_json"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Generate 出图。
func (p *ArkImage) Generate(ctx context.Context, req ImageRequest) (ImageResult, error) {
	headers, err := bearer(p.Account)
	if err != nil {
		return ImageResult{}, err
	}
	base := p.Account.BaseURL
	if base == "" {
		base = DefaultArkBaseURL
	}
	size := req.Size
	if size == "" {
		size = "2K"
	}
	body := map[string]interface{}{
		"model":           req.Model,
		"prompt":          req.Prompt,
		"size":            size,
		"response_format": "b64_json",
		"watermark":       false,
	}
	if req.ImagePath != "" {
		u, err := imageDataURL(req.ImagePath)
		if err != nil {
			return ImageResult{}, err
		}
		body["image"] = u
	}
	raw, err := doJSON(ctx, p.Client, p.Name(), http.MethodPost, joinURL(base, "/images/generations"), headers, body)
	if err != nil {
		return ImageResult{}, err
	}
	var r arkImageResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return ImageResult{}, &core.ErrProvider{Summary: "ark 回包无法解析：不是合法 JSON", Raw: string(raw), Retryable: true}
	}
	if r.Error != nil && r.Error.Message != "" {
		return ImageResult{}, &core.ErrProvider{Summary: "ark 出图失败：" + firstLine(r.Error.Message), Raw: string(raw), Retryable: !api.IsModerationError(&core.ErrProvider{Raw: r.Error.Message})}
	}
	if len(r.Data) == 0 {
		return ImageResult{}, &core.ErrProvider{Summary: "ark 出图失败：回包里没有图片", Raw: string(raw), Retryable: true}
	}
	out := ImageResult{Raw: raw}
	if r.Data[0].B64 != "" {
		return decodeB64(out, r.Data[0].B64, p.Name())
	}
	out.URL = r.Data[0].URL
	return out, nil
}
