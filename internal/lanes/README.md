# internal/lanes：渠道驱动

三类渠道各一个包，都只依赖 `internal/core` 的接口，不反向依赖调度器。

| 包 | 渠道 | 驱动 |
|---|---|---|
| `sub` | 订阅（本机官方 CLI 子进程） | `ClaudeDriver`、`CodexDriver`、`AntigravityDriver`（占位） |
| `api` | 按量付费（OpenAI 兼容） | `APIDriver` + 供应商预设 `Presets` |
| `media` | 媒体（图片 / 视频 / 动画） | `MediaDriver`，内部按 `Task.Kind` 分发到供应商 |

## sub

```go
claude := sub.NewClaudeDriver()          // Bin / Home / Timeout / DefaultModel 可改；Home 只用来 stat ~/.claude
codex  := sub.NewCodexDriver()           // Ephemeral 默认 true；DefaultModel 空串表示不传 -m
agy    := sub.NewAntigravityDriver()     // 只有 Health；Run 返回「尚未支持」
```

- 子进程封装 `proc.go`：上下文取消、超时（默认 10 分钟）、stderr 收集、环境清洗。启动前一律删掉
  `ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN / ANTHROPIC_BASE_URL / CLAUDE_CODE_USE_BEDROCK / CLAUDE_CODE_USE_VERTEX`
  以及 `OPENAI_API_KEY / OPENAI_BASE_URL / CLAUDECODE / CLAUDE_CODE_ENTRYPOINT`。
- Claude：`claude -p <prompt> --output-format stream-json --verbose --no-session-persistence --model <m>`；纯文本加 `--tools ""`；
  动画（`Kind==animation` 且 `Engine` 或 `ProjectDir` 非空）改为 cwd=ProjectDir、`--permission-mode acceptEdits --max-turns 12`。
  `Task.System` 用 `--append-system-prompt`。被禁止的参数写成常量 `forbiddenClaudeFlag`，`BuildArgs` 的测试断言它不会出现。
  `rate_limit_event` 里的 `unifiedWindows` 会缓存为 `Health.Quota`（5h / week 的利用率与重置时间）。
- Codex：`codex exec --json --skip-git-repo-check [-m <m>] [--ephemeral] -`，提示词走 stdin，cwd 是临时目录。
  `Task.Kind==image && target.Model=="codex-imagegen"` 时走内置 image_gen：提示词前加 `$imagegen …`，
  结束后在 `~/.codex/generated_images/` 找比开始时间新的文件复制到 `Task.OutDir`（默认 `ProjectDir/assets`）。
- 限流识别：stderr / result 文本含 rate limit、usage limit、resets、try again 等字样 → `*core.ErrRateLimited`，
  `RetryAfter` 从「in 2 hours」「resets 3pm」「Oct 29th, 2026 8:54 AM」「resetsAt 秒级时间戳」解析，解析不出给 30 分钟。
- Health：`<cli> --version` 能跑 + 目录 / 文件存在（只 stat）。找不到二进制 → `missing`。
- 夹具在 `testdata/cli_streams/`，见那里的 README（`codex_ok.jsonl` 是手写合成的）。

## api

```go
preset, _ := api.PresetByID("zhipu")
acct := preset.Account("zhipu-main", func() (string, error) { return secrets.Get("api.zhipu") })
drv := api.NewAPIDriver([]api.Account{acct}, httpClient)
err := preset.Verify(ctx, httpClient, key) // 先 GET /models，404/405/400 时退回 max_tokens=1 的最小 chat
```

- `Account{ID, Provider, BaseURL, APIKeyGetter, Currency, Models, NeedsProxy, Headers}`；密钥每次按需取，不缓存。
- `Run`：chat completions，`Task.Stream` 决定 SSE 或一次性回包；`Task.Messages/Tools/System` 原样映射，
  `Message.Images` 本地路径转 data URL；流式时 tool_calls 拼好后放在 `Result.Raw`。
- 费用按 `Account.Models` 价格表（每百万 token），命中缓存按 `CachedPerM`（0 则同输入价）；
  `Currency=CNY` 时 `CostNative` 记人民币、`CostUSD` 按 `api.USDPerCNY`（包级变量，默认 7.1）折算。
- 错误映射 `ClassifyHTTPError / ClassifyNetError`（媒体包复用）：401/403 验证失败不可重试；402 或含 insufficient/balance 余额不足可重试；
  429 → `ErrRateLimited`；网络错误 → 「连接失败：检查网络代理」可重试；其它一行摘要 + Raw。
- `GenerateImage`：`/images/generations`，gpt-image 系列不传 `response_format`，其它要 `b64_json`；`OutPath` 非空时落盘。
- 预设 8 个：zhipu、dashscope、deepseek、moonshot、minimax、openrouter（needs_proxy）、anthropic（OpenAI 兼容端点）、302ai。
- `Health` 只检查账号与密钥可取，不发请求。

## media

```go
m := media.New(media.Options{
    Jobs:     jobStore,          // 必填：core.JobStore
    OutDir:   nil,               // 产物目录解析器，nil 用 media.DefaultOutDir（OutDir > ProjectDir/assets > ~/Library/Application Support/Switchboard/media）
    Accounts: apiDriver,         // 按量付费账号表（*api.APIDriver 满足 media.AccountLookup）
    Codex:    codexDriver,       // 免费档出图
    Claude:   claudeDriver,      // 动画写代码
    Client:   httpClient,
    AnimRoot: "",                // 动画工程根目录，空用 ~/Library/Application Support/Switchboard/animations/<job>/
    // 测试注入：ImageProviders / VideoProviders / Exec / Now / PollBase / PollMax / JobTimeout
})
```

- 文件名 `<yyyyMMdd-HHmmss>-<slug>.png|jpg|mp4`。
- 供应商选择：先看 `Options.ImageProviders/VideoProviders[target.Provider]`，再按 `target.AccountID` / `target.Provider` 找账号，
  按账号的 `Provider` 字段选实现：`openrouter` → OpenRouter；`ark|volcengine|doubao` → 火山方舟；其它 → OpenAI 兼容。
  `target.Provider=="codex"` 或 `Model=="codex-imagegen"` → Codex 免费档。
- 视频 / 动画走异步任务（`jobs.go`）：`queued → submitted → running → done|failed|timeout|canceled`，
  轮询 5 秒起翻倍到 30 秒，10 分钟超时则标 `timeout` 并调供应商 Cancel。失败分可重试（参数 / 网络 / 余额，填 `RetryOptions`）
  与不可重试（内容审核：sensitive / moderation / 审核 / 违规）。
  `Run` 在 ctx 到期前完成返回 `Result.Path`，否则返回 `Result.JobID`（MCP 调用方用 ctx 控制等待时长，如 90 秒）。
- 额外方法（给 admin 层）：
  - `Cancel(ctx, jobID) (*core.Job, error)`：停后台轮询、调供应商 Cancel（如有），状态改 `canceled`；已结束的任务返回错误。
  - `Retry(ctx, jobID, model) (*core.Job, error)`：用同一个 `job.Task` 换模型重跑并返回新任务；`model` 为空取 `RetryOptions[0]`；
    `job.Task` 为 nil（重启后从账本恢复）时返回一行人话错误。
  - `ReviseAnimation(ctx, jobID, instruction, emit)`：在已有工程目录上再跑一轮 Claude 改码 + 渲染；`Task`/`CodeDir` 丢失时返回一行人话错误。
- 动画 `animation_remotion.go`：模板工程内嵌在 `remotion_template/`（package.json、remotion.config.ts、tsconfig.json、src/index.ts、src/Root.tsx、src/Main.tsx），
  复制到 `<AnimRoot>/<job>/` → Claude 驱动以 `Kind=animation, ProjectDir=该目录` 只改 `src/Main.tsx` → `npm install`（首次，删代理变量）
  → `npx remotion render Main out.mp4` → 失败把 stderr 回喂 Claude，最多两轮 → 复制到产物路径。没有 Node → 「动画需要本机安装 Node」不可重试。
- 价格表 `prices.go`：`UsageFor(model, quality, units)`，模型名归一化后子串匹配（兼容日期后缀、厂商前缀）。

## 未实测的供应商接口（形状按文档编写，带 TODO 注释）

本机没有任何按量付费密钥，以下全部只过了 httptest 假服务器：

| 文件 | 接口 | 不确定点 |
|---|---|---|
| `media/image_openrouter.go` | `POST /api/v1/images` | 回包是 `data[].b64_json` 还是 chat 风格 `choices[].message.images[]`，两种都兼容了 |
| `media/image_ark.go` | `POST /api/v3/images/generations` | `size` 取值（写的 `2K`）、图生图字段名 `image` |
| `media/video_ark.go` | `POST/GET/DELETE /api/v3/contents/generations/tasks` | `duration` / `ratio` 是顶层字段还是写在 text 里（`--duration 5`）；首帧图的 `role` 字段 |
| `media/video_openrouter.go` | `POST/GET/DELETE /api/v1/videos` | 字段名 `duration` / `resolution` / `image_url`；完成态 URL 字段（兼容 `url` / `video_url` / `output.url`） |
| `media/video_openai_compat.go` | `POST/GET/DELETE /v1/videos`、`GET /v1/videos/{id}/content` | DeepInfra 是否完全按 OpenAI videos 形状 |
| `api/presets.go` | anthropic 的 `/v1/chat/completions`、`/v1/models` 是否接受 Bearer | 若只认 `x-api-key`，给预设加 `Headers` 即可 |
| `sub/codex.go` | `codex exec --json` 成功事件 | 三次真实调用都没拿到成功输出，`item.completed{agent_message}` / `turn.completed{usage}` 的形状按公开文档写 |

按量付费的 chat completions、images/generations、OpenAI 兼容错误码映射、Claude stream-json 解析均有真实夹具或标准形状支撑。
