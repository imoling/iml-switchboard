package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/imoling/iml-switchboard/internal/core"
)

// ImageGenRequest 是 /v1/images/generations 的参数。
type ImageGenRequest struct {
	Model   string
	Prompt  string
	Size    string
	Quality string
	N       int
	// OutPath 非空时把第一张图写到该路径。
	OutPath string
}

// ImageGenResult 是出图结果。
type ImageGenResult struct {
	// Data 是解码后的图片字节（b64_json）；供应商只给 URL 时为空、URL 字段有值。
	Data []byte
	URL  string
	// Path 是写盘后的路径（OutPath 非空时）。
	Path string
	Raw  json.RawMessage
}

type imagesRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	Quality        string `json:"quality,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

type imagesResponse struct {
	Data []struct {
		B64 string `json:"b64_json"`
		URL string `json:"url"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// GenerateImage 调 OpenAI 兼容的 /v1/images/generations（OpenAI 直连、302.ai 等），返回 b64 并按需落盘。
func (d *APIDriver) GenerateImage(ctx context.Context, acct Account, req ImageGenRequest) (*ImageGenResult, error) {
	n := req.N
	if n <= 0 {
		n = 1
	}
	body := imagesRequest{Model: req.Model, Prompt: req.Prompt, N: n, Size: req.Size, Quality: req.Quality}
	// gpt-image 系列只返回 b64，不接受 response_format；其它兼容实现需要显式要 b64。
	if !isGPTImage(req.Model) {
		body.ResponseFormat = "b64_json"
	}
	raw, _ := json.Marshal(body)
	hreq, err := acct.newRequest(ctx, http.MethodPost, endpoint(acct.BaseURL, "/images/generations"), raw)
	if err != nil {
		return nil, err
	}
	resp, err := d.client().Do(hreq)
	if err != nil {
		return nil, ClassifyNetError(acct.Provider, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode/100 != 2 {
		return nil, ClassifyHTTPError(acct.Provider, resp.StatusCode, b)
	}
	var ir imagesResponse
	if err := json.Unmarshal(b, &ir); err != nil {
		return nil, &core.ErrProvider{Summary: acct.Provider + " 回包无法解析：不是合法 JSON", Raw: string(b[:min(len(b), 2000)]), Retryable: true}
	}
	if ir.Error != nil && ir.Error.Message != "" {
		return nil, &core.ErrProvider{Summary: acct.Provider + " 出图失败：" + firstLine(ir.Error.Message), Raw: string(b), Retryable: true}
	}
	if len(ir.Data) == 0 {
		return nil, &core.ErrProvider{Summary: acct.Provider + " 出图失败：回包里没有图片", Raw: string(b[:min(len(b), 2000)]), Retryable: true}
	}
	out := &ImageGenResult{Raw: b, URL: ir.Data[0].URL}
	if ir.Data[0].B64 != "" {
		data, err := base64.StdEncoding.DecodeString(ir.Data[0].B64)
		if err != nil {
			return nil, &core.ErrProvider{Summary: acct.Provider + " 出图失败：图片数据损坏", Raw: err.Error(), Retryable: true}
		}
		out.Data = data
	}
	if req.OutPath != "" && out.Data != nil {
		if err := os.MkdirAll(filepath.Dir(req.OutPath), 0o755); err != nil {
			return nil, &core.ErrProvider{Summary: "无法创建产物目录：" + filepath.Dir(req.OutPath), Raw: err.Error()}
		}
		if err := os.WriteFile(req.OutPath, out.Data, 0o644); err != nil {
			return nil, &core.ErrProvider{Summary: "写入图片失败：" + req.OutPath, Raw: err.Error()}
		}
		out.Path = req.OutPath
	}
	return out, nil
}

func isGPTImage(model string) bool {
	return len(model) >= 9 && model[:9] == "gpt-image"
}
