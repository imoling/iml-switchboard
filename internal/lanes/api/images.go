package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/imoling/iml-switchboard/internal/core"
)

// ImageGenRequest 是 /v1/images/generations 的参数。
type ImageGenRequest struct {
	Model   string
	Prompt  string
	Size    string
	Quality string
	N       int
	// Refs 是参考图：有就改走 /images/edits（gpt-image 系列支持多张）。
	Refs []string
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
	var hreq *http.Request
	var err error
	if len(req.Refs) > 0 {
		hreq, err = d.editsRequest(ctx, acct, body, req.Refs)
	} else {
		raw, _ := json.Marshal(body)
		hreq, err = acct.newRequest(ctx, http.MethodPost, endpoint(acct.BaseURL, "/images/generations"), raw)
	}
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

// editsRequest 拼 /images/edits 的 multipart 请求：参考图放进 image[]，其余参数照 generations。
func (d *APIDriver) editsRequest(ctx context.Context, acct Account, body imagesRequest, refs []string) (*http.Request, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	field := "image"
	if len(refs) > 1 {
		field = "image[]"
	}
	for i, ref := range refs {
		data, err := d.loadRef(ctx, ref)
		if err != nil {
			return nil, err
		}
		mt := http.DetectContentType(data)
		ext := map[string]string{"image/jpeg": ".jpg", "image/webp": ".webp", "image/gif": ".gif"}[mt]
		if ext == "" {
			ext = ".png"
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="ref%d%s"`, field, i+1, ext))
		h.Set("Content-Type", mt)
		w, _ := mw.CreatePart(h)
		_, _ = w.Write(data)
	}
	_ = mw.WriteField("model", body.Model)
	_ = mw.WriteField("prompt", body.Prompt)
	_ = mw.WriteField("n", strconv.Itoa(body.N))
	for k, v := range map[string]string{"size": body.Size, "quality": body.Quality, "response_format": body.ResponseFormat} {
		if v != "" {
			_ = mw.WriteField(k, v)
		}
	}
	_ = mw.Close()
	hreq, err := acct.newRequest(ctx, http.MethodPost, endpoint(acct.BaseURL, "/images/edits"), buf.Bytes())
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", mw.FormDataContentType())
	return hreq, nil
}

// loadRef 读一张参考图的字节：本地路径、data URL、http 地址都认。
func (d *APIDriver) loadRef(ctx context.Context, ref string) ([]byte, error) {
	switch {
	case strings.HasPrefix(ref, "data:"):
		_, b64, ok := strings.Cut(ref, ",")
		data, err := base64.StdEncoding.DecodeString(b64)
		if !ok || err != nil {
			return nil, &core.ErrProvider{Summary: "参考图数据损坏"}
		}
		return data, nil
	case strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://"):
		hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
		if err != nil {
			return nil, &core.ErrProvider{Summary: "参考图地址不对：" + ref, Raw: err.Error()}
		}
		resp, err := d.client().Do(hreq)
		if err != nil {
			return nil, &core.ErrProvider{Summary: "下载参考图失败：" + ref, Raw: err.Error(), Retryable: true}
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return nil, &core.ErrProvider{Summary: fmt.Sprintf("下载参考图失败：HTTP %d", resp.StatusCode), Raw: ref}
		}
		return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		return nil, &core.ErrProvider{Summary: "读取参考图失败：" + ref, Raw: err.Error()}
	}
	return data, nil
}

func isGPTImage(model string) bool {
	return len(model) >= 9 && model[:9] == "gpt-image"
}
