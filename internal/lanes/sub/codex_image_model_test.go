package sub

import (
	"slices"
	"testing"

	"github.com/imoling/iml-switchboard/internal/core"
)

// 出图要明确传低档模型：不传时 Codex 用用户自己配置里的模型，免费套餐用不了就整条失败
func TestCodexImageUsesLowTierModel(t *testing.T) {
	d := NewCodexDriver()
	img := &core.Task{Kind: core.KindImage, Prompt: "一枝红杏出墙来"}
	tgt := core.Target{Lane: core.LaneCodexSub, Model: CodexImageModel}

	args, _ := d.BuildArgs(img, tgt)
	if slices.Contains(args, "-m") {
		t.Fatalf("没配低档模型时不该传 -m：%v", args)
	}
	d.ImageModel = func() string { return "gpt-5.6-luna" }
	args, _ = d.BuildArgs(img, tgt)
	if i := slices.Index(args, "-m"); i < 0 || args[i+1] != "gpt-5.6-luna" {
		t.Fatalf("出图应传低档模型：%v", args)
	}
	if slices.Contains(args, CodexImageModel) {
		t.Fatalf("虚拟模型名不能传给 codex：%v", args)
	}
}
