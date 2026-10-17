package media

import (
	"context"
	"os"
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
	task := &core.Task{Kind: core.KindImage, Prompt: req.Prompt, OutDir: tmp, Size: req.Size, ImagePath: req.ImagePath, CreatedAt: time.Now()}
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
