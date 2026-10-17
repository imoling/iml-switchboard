package media

import (
	"context"
	"net/http"

	"github.com/imoling/iml-switchboard/internal/lanes/api"
)

// OpenAICompatImage 走 OpenAI 兼容的 /v1/images/generations（OpenAI gpt-image-2.5-*、302.ai）。
type OpenAICompatImage struct {
	Account api.Account
	Client  *http.Client
}

// Name 返回供应商名（取账号的 Provider）。
func (p *OpenAICompatImage) Name() string {
	if p.Account.Provider != "" {
		return p.Account.Provider
	}
	return "openai"
}

// Generate 出图。
func (p *OpenAICompatImage) Generate(ctx context.Context, req ImageRequest) (ImageResult, error) {
	d := api.NewAPIDriver(nil, p.Client)
	res, err := d.GenerateImage(ctx, p.Account, api.ImageGenRequest{Model: req.Model, Prompt: req.Prompt, Size: req.Size, Quality: req.Quality})
	if err != nil {
		return ImageResult{}, err
	}
	out := ImageResult{Data: res.Data, URL: res.URL, Raw: res.Raw}
	if len(out.Data) > 0 {
		out.Mime = http.DetectContentType(out.Data)
	}
	return out, nil
}
