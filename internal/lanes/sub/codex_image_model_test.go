package sub

import (
	"slices"
	"strings"
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

// 参考图用 --image= 紧跟 exec：--image 能跟好几个值，放在末尾的 - 前面会把 - 也当成图
func TestCodexImageAttachesRefs(t *testing.T) {
	d := NewCodexDriver()
	img := &core.Task{Kind: core.KindImage, Prompt: "改成像素画", RefImages: []string{"/tmp/a.png", "/tmp/b.jpg"}}
	args, stdin := d.BuildArgs(img, core.Target{Lane: core.LaneCodexSub, Model: CodexImageModel})
	if len(args) < 3 || args[1] != "--image=/tmp/a.png" || args[2] != "--image=/tmp/b.jpg" {
		t.Fatalf("参考图应紧跟 exec：%v", args)
	}
	if args[len(args)-1] != "-" || strings.HasPrefix(args[len(args)-2], "--image") {
		t.Fatalf("最后一个参数必须是 -，且不能紧跟在 --image 后：%v", args)
	}
	if !strings.Contains(stdin, "参照附带的图片") {
		t.Fatalf("提示词里要说明参照附图：%q", stdin)
	}
	text, _ := d.BuildArgs(&core.Task{Kind: core.KindText, Prompt: "hi", RefImages: []string{"/tmp/a.png"}}, core.Target{Lane: core.LaneCodexSub, Model: "gpt-5.6"})
	if slices.ContainsFunc(text, func(a string) bool { return strings.HasPrefix(a, "--image") }) {
		t.Fatalf("文字任务不带 --image：%v", text)
	}
}
