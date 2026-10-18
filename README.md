# Switchboard · 点将台

订阅额度先用，难事交给强模型，杂活交给便宜模型。

一个本机常驻的服务，替 Claude Code、Hermes Agent、WorkBuddy 这些宿主按任务选模型：
文本任务分低、中、高三档路由，生图、生视频、动画走媒体渠道，订阅额度经官方 CLI 子进程消耗。
配一个菜单栏应用和控制台窗口，看额度、看花费、改规则。

iML Studio 出品。调研报告、设计稿与技术方案见 `docs/`（索引在 `docs/README.md`）。

## 组成

| 部分 | 实现 | 位置 |
|---|---|---|
| 后台服务、MCP、命令行 | Go 单文件 `imls` | `cmd/imls`、`internal/` |
| 界面（菜单栏、控制台、快速面板） | Tauri 2 + React | `ui/` |
| 宿主接入素材 | Claude Code 命令、CLAUDE.md 片段 | `assets/claude` |

## 命令

```
imls serve            # 常驻后台，监听 127.0.0.1:18081
imls mcp              # 作为 MCP 小程序被宿主拉起，转发给后台
imls route "任务"      # 只打印路由决策
imls route --run "任务" # 真正执行
imls stats            # 本周账本摘要
imls doctor           # 检查本机 CLI、密钥、Node
```

## 许可

CC BY-NC 4.0，见 `LICENSE.md`。
