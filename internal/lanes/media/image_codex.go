package media

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

// CodexImage 委托 Codex 订阅驱动用内置 image_gen 出图（免费档）。
type CodexImage struct {
	Codex core.Driver
	// Model 是传给 Codex 驱动的虚拟模型名，默认 codex-imagegen。
	Model string
}

// Name 返回供应商名。
func (p *CodexImage) Name() string { return "codex" }

// Generate 出图：让 Codex 驱动把图落到临时目录，再读回字节。
func (p *CodexImage) Generate(ctx context.Context, req ImageRequest) (ImageResult, error) {
	if p.Codex == nil {
		return ImageResult{}, &core.ErrProvider{Summary: "Codex 出图不可用：没有接入 Codex 渠道", Retryable: true}
	}
	model := p.Model
	if model == "" {
		model = "codex-imagegen"
	}
	tmp, err := os.MkdirTemp("", "imls-codex-img-")
	if err != nil {
		return ImageResult{}, &core.ErrProvider{Summary: "Codex 出图失败：无法创建临时目录", Raw: err.Error()}
	}
	defer os.RemoveAll(tmp)
	refs, err := localRefs(ctx, tmp, req.Refs)
	if err != nil {
		return ImageResult{}, err
	}
	task := &core.Task{Kind: core.KindImage, Prompt: req.Prompt, OutDir: tmp, Size: req.Size, RefImages: refs, CreatedAt: time.Now()}
	res, err := p.Codex.Run(ctx, task, core.Target{Lane: core.LaneCodexSub, Provider: "codex", Model: model}, nil)
	if err != nil {
		return ImageResult{}, err
	}
	if res.Path == "" {
		return ImageResult{}, &core.ErrProvider{Summary: "Codex 出图失败：没有返回文件", Raw: res.Text, Retryable: true}
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		return ImageResult{}, &core.ErrProvider{Summary: "Codex 出图失败：读取产物失败", Raw: err.Error(), Retryable: true}
	}
	return ImageResult{Data: data, Mime: "image/png"}, nil
}

// localRefs 把参考图都换成本地文件：codex 的 --image 只认文件。data URL 解码、http 地址下载到 dir 里。
func localRefs(ctx context.Context, dir string, refs []string) ([]string, error) {
	var out []string
	for i, ref := range refs {
		switch {
		case strings.HasPrefix(ref, "data:"):
			head, b64, _ := strings.Cut(ref, ",")
			data, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				return nil, &core.ErrProvider{Summary: "参考图数据损坏"}
			}
			ext := ".png"
			if strings.Contains(head, "jpeg") {
				ext = ".jpg"
			} else if strings.Contains(head, "webp") {
				ext = ".webp"
			}
			p := filepath.Join(dir, fmt.Sprintf("ref%d%s", i+1, ext))
			if err := os.WriteFile(p, data, 0o600); err != nil {
				return nil, &core.ErrProvider{Summary: "参考图写入临时目录失败", Raw: err.Error()}
			}
			out = append(out, p)
		case strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://"):
			p := filepath.Join(dir, fmt.Sprintf("ref%d%s", i+1, filepath.Ext(strings.SplitN(ref, "?", 2)[0])))
			if err := download(ctx, nil, "codex", ref, p, nil); err != nil {
				return nil, err
			}
			out = append(out, p)
		default:
			out = append(out, ref)
		}
	}
	return out, nil
}
