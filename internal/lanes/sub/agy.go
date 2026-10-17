package sub

import (
	"context"

	"github.com/imoling/iml-switchboard/internal/core"
)

// AntigravityDriver 是 Antigravity 订阅渠道的占位实现：只报告健康状态，尚不支持执行。
type AntigravityDriver struct {
	// Bin 是 CLI 可执行文件名，空串表示 "agy"。
	Bin string
}

// NewAntigravityDriver 构造占位驱动。
func NewAntigravityDriver() *AntigravityDriver { return &AntigravityDriver{} }

// ID 返回渠道标识。
func (d *AntigravityDriver) ID() core.LaneID { return core.LaneAgySub }

func (d *AntigravityDriver) bin() string {
	if d.Bin != "" {
		return d.Bin
	}
	return "agy"
}

// Run 尚未支持。
func (d *AntigravityDriver) Run(ctx context.Context, task *core.Task, target core.Target, emit func(core.Event)) (*core.Result, error) {
	return nil, &core.ErrProvider{Summary: "Antigravity 尚未支持：请改用其它渠道", Retryable: true}
}

// Health 检查本机是否安装了 CLI；没装返回 missing 和安装提示。
func (d *AntigravityDriver) Health(ctx context.Context) core.Health {
	if lookBin(d.bin()) == "" {
		return core.Health{Status: "missing", Detail: "本机没有安装 Antigravity：从 antigravity.google 下载后在设置里开启命令行工具"}
	}
	return core.Health{Status: "disabled", Detail: "Antigravity 已安装，但本版本尚未支持通过它执行任务"}
}
