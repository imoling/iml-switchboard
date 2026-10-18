# Switchboard

通用文案规矩在全局 CLAUDE.md，这里只放本项目的术语和例外。

## 术语表：一个概念一个词

| 用 | 不用 | 备注 |
|---|---|---|
| 订阅 | 订阅泳道、OAuth、包月 | 经本机官方 CLI 消耗套餐额度 |
| 按量付费 | API 泳道、BYOK、直连 | 用密钥按量计费 |
| 媒体 | 多模态、生成式 | 图片、视频、动画 |
| 渠道 | 泳道、lane、provider | 订阅、按量付费、媒体的统称；代码里仍叫 lane |
| 低档、中档、高档 | 轻量、强力、Tier | 路由规则只在这三档里选 |
| 顶配 | 第四档、旗舰、最强 | Fable；只由显式指令、重做、质量模式触发 |
| 便宜档、默认档、最好档 | cheap、best | 媒体三档 |
| 调度模式 | 策略、偏好 | 省钱、均衡、质量 |
| 判低了 | 误判、路由错误 | 用户认为该用更高的档 |
| 接入 | 集成、绑定、连接器 | 让宿主能调用 |
| 投屏模式 | 演示模式 | 一键切回标准主题 |

## 界面里不出现的词

MCP、stdio、OAuth、钥匙串、LiteLLM、JSONL、SSE、base_url、prompt cache。要解释放帮助文档。

## 风格主题

界面文案走词表（`internal/themes/*.json`）。标准、点将台（默认）、后宫、海王四套。
费用、额度、报错、给宿主的工具返回永远不走词表。

## 红线（代码评审必查）

1. 不读取、不存储、不转发任何订阅凭据（`~/.claude/.credentials.json`、钥匙串、`~/.codex/auth.json`、`~/.gemini/oauth_creds.json`）。
2. 不伪装官方客户端请求头。
3. 订阅额度只能由未修改的官方 CLI 子进程消耗。
4. 启动 `claude -p` 前清掉 `ANTHROPIC_API_KEY`、`ANTHROPIC_AUTH_TOKEN`、`ANTHROPIC_BASE_URL`；永远不传 `--bare`。
5. 只监听 `127.0.0.1`。

`go test ./...` 里有一条测试扫描源码保证 1 和 4。

## 开发

```
go build -o bin/imls ./cmd/imls && go test ./...
cd ui && npm install && npm run tauri dev
```
