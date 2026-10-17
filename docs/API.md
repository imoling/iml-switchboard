# 接口契约

后台 `imls serve` 只监听 `127.0.0.1:18081`。三组接口：

| 前缀 | 给谁用 | 鉴权 |
|---|---|---|
| `/api/*` | 界面（Tauri）、命令行 | 请求头 `X-IMLS-Client: ui`，仅本机 |
| `/v1/*` | WorkBuddy、Hermes 等 OpenAI 兼容客户端 | `Authorization: Bearer <facade_key>`，key 写在 config.yaml |
| MCP | Claude Code、Hermes、Codex | stdio，由 `imls mcp` 转发到 `/api/internal/*` |

所有 JSON 字段蛇形命名。金额单位：`cost_usd` 美元浮点；人民币账号折算成美元记账，同时保留 `cost_native` 与 `currency`。时间 ISO 8601 带时区。

## 数据模型

```ts
type Tier = 'low' | 'mid' | 'high' | 'top';          // 低档 中档 高档 顶配
type Lane = 'claude_sub' | 'codex_sub' | 'agy_sub' | 'api' | 'media';
type Kind = 'text' | 'image' | 'video' | 'animation';
type Mode = 'save' | 'balanced' | 'quality';         // 省钱 均衡 质量
type Theme = 'standard' | 'dianjiang' | 'harem' | 'seaking';

interface Call {
  id: string; ts: string; source: 'mcp' | 'facade' | 'cli' | 'quick' | 'redo';
  host?: string;                 // 'claude_code' | 'hermes' | 'workbuddy' | ...
  session_id?: string;
  kind: Kind; task_type: string; tier: Tier | null;  // media 任务 tier 为 null，用 media_tier
  media_tier?: 'cheap' | 'default' | 'best' | 'free';
  lane: Lane; provider: string; model: string;
  reason: string[];              // 人话理由，按决策顺序
  candidates: { lane: Lane; model: string; skipped?: string }[];
  fallback_from?: string;
  input_tokens?: number; output_tokens?: number; cache_read_tokens?: number;
  units?: number; unit?: 'image' | 'second';
  cost_usd: number; cost_native?: number; currency?: 'USD' | 'CNY';
  quota_units?: number;          // 订阅渠道：估算的额度占用
  baseline_cost_usd: number;     // 同一请求全用 Opus 5.5 的估算
  user_escalated: boolean; latency_ms: number; ok: boolean; error?: string;
  prompt_excerpt?: string;       // 设置里关掉「保存请求内容」则为空
  result_path?: string;          // 媒体产物
  job_id?: string;
}

interface Decision {
  task_type: string; tier: Tier | null; media_tier?: string; kind: Kind;
  candidates: { lane: Lane; provider: string; model: string; est_cost_usd: number; skipped?: string }[];
  reason: string[];
  sticky?: boolean;
}

interface Job { id: string; kind: 'video' | 'animation'; status: 'queued' | 'submitted' | 'running' | 'done' | 'failed' | 'canceled' | 'timeout';
  progress: number; eta_seconds?: number; provider: string; model: string; prompt_excerpt: string;
  result_path?: string; error?: string; cost_usd?: number; created_at: string; updated_at: string; call_id: string;
  retry_options?: { lane: Lane; model: string; est_cost_usd: number }[]; code_dir?: string }

interface Account { id: string; kind: 'subscription' | 'api'; provider: string; label: string;
  status: 'ok' | 'warn' | 'missing' | 'error' | 'cooldown' | 'disabled'; detail: string;
  month_cost_usd?: number; month_cost_native?: number; currency?: string;
  quota?: { window: string; pct: number; resets_at?: string }[];
  tiers?: Record<string, string>;     // 该账号在各档位用哪个模型
  models: string[]; needs_proxy?: boolean; enabled: boolean; install_hint?: string }
```

## /api

| 方法 路径 | 说明 | 请求 | 响应 |
|---|---|---|---|
| GET /api/status | 服务状态 | | `{version, uptime_s, mode, theme, paused, projection, node_available, data_dir}` |
| POST /api/pause | 暂停 / 恢复调度 | `{paused}` | status |
| POST /api/mode | 切调度模式 | `{mode}` | status |
| POST /api/projection | 投屏模式开关（临时切标准主题） | `{on}` | status |
| GET /api/overview?range=today\|7d\|30d | 概览 | | `{calls, api_cost_usd, baseline_cost_usd, saved_usd, saved_pct, escalation_rate, escalation_hot_task?, tier_dist{low,mid,high,top,media}, lane_dist{sub,api,media}, daily[{date,cost_usd,baseline_cost_usd}], quotas[{account_id,label,window,pct,resets_at,sub?}], budget{month_usd,limit_usd}, top_tasks[{task_type,lane,metric}]}` |
| GET /api/calls | 请求记录 | query `source,lane,tier,q,limit,before` | `Call[]` |
| GET /api/calls/{id} | 详情 | | `Call` |
| POST /api/calls/{id}/escalate | 标记「判低了」 | | `{ok, escalations_for_task}` |
| POST /api/calls/{id}/redo | 重做 | `{tier?: Tier, model?: string}` | `Call`（文本同步返回；媒体返回带 job_id） |
| GET /api/rules | 规则 | | `{task_table[], explicit[], context_rules[], classifier}` |
| PUT /api/rules | 保存规则 | 同上 | 同上 |
| POST /api/rules/try | 试一句 | `{prompt, has_tools?, est_tokens?, session_id?}` | `Decision` |
| GET /api/rules/suggestions | 调整建议 | | `[{task_type, escalations, from_tier, to_tier, weekly_delta_usd}]` |
| POST /api/rules/suggestions/{task_type} | 采纳 / 忽略 | `{action:'accept'\|'ignore'}` | rules |
| GET /api/rules/export | 导出 YAML | | text/yaml |
| POST /api/rules/import | 导入 YAML | text/yaml | rules |
| GET /api/accounts | 模型与账号 | | `Account[]` |
| POST /api/accounts | 添加按量付费账号 | `{provider, api_key, base_url?}` | `Account`（已验证） |
| PUT /api/accounts/{id} | 启停 / 档位 | `{enabled?, tiers?}` | `Account` |
| DELETE /api/accounts/{id} | 删除 | | `{ok}` |
| POST /api/accounts/{id}/verify | 重新验证 | | `Account` |
| POST /api/accounts/detect | 重新检测本机 CLI | | `Account[]` |
| GET /api/media?kind=all\|image\|video\|animation | 媒体库 | | `{jobs: Job[], items: Call[]}` |
| GET /api/jobs/{id} | 任务 | | `Job` |
| POST /api/jobs/{id}/cancel | 取消 | | `Job` |
| POST /api/jobs/{id}/retry | 换模型重做 | `{model?}` | `Job` |
| POST /api/jobs/{id}/revise | 动画：改一句重做 | `{instruction}` | `Job` |
| GET /api/integrations | 接入 | | `[{id:'claude_code'\|'hermes'\|'workbuddy', status:'connected'\|'disconnected'\|'manual', options:{write_claude_md?,opusplan?}, snippet?}]` |
| POST /api/integrations/{id}/connect | 接入 | `{options?}` | integration |
| POST /api/integrations/{id}/disconnect | 断开 | | integration |
| PUT /api/integrations/{id} | 改选项 | `{options}` | integration |
| GET /api/settings | 设置 | | `Settings` |
| PUT /api/settings | 保存 | `Partial<Settings>` | `Settings` |
| GET /api/themes | 主题词表 | | `[{id, name, words: Record<string,string>}]` |
| GET /api/evaluations | 评估历史 | | `Evaluation[]` |
| POST /api/evaluations | 开始一轮 | `{sample?: number}` | `Evaluation`（status running） |
| GET /api/evaluations/{id} | 详情 | | `Evaluation` |
| POST /api/evaluations/{id}/accept | 采纳某条建议 | `{task_type}` | rules |
| POST /api/quick/preview | 快速面板预览 | `{prompt, image_path?}` | `Decision` + `alternatives[]` |
| POST /api/quick/run | 快速面板执行 | `{prompt, image_path?, tier?, model?, lane?}` | SSE：`text_delta{text}` / `media{path,kind}` / `job{job}` / `done{call}` / `error{message}` |
| GET /api/events | 事件流 SSE | | `quota{...}` / `job{job}` / `call{call}` / `notice{title,body,level}` / `status{...}` |
| GET /api/onboarding | 引导状态 | | `{done, steps:{cli_detected, api_added, claude_connected, tried}}` |
| POST /api/onboarding/done | 完成引导 | | |

```ts
interface Settings { monthly_api_limit_usd: number; subscription_yield_pct: number; default_mode: Mode; theme: Theme;
  hotkey: string; proxy: string; save_prompts: boolean; launch_at_login: boolean; data_dir: string; facade_key: string;
  auto_evaluate_monthly: boolean; notifications: { quota: boolean; budget: boolean; media: boolean; suggestions: boolean } }

interface Evaluation { id: string; status: 'running'|'done'|'failed'; started_at: string; finished_at?: string; sample: number;
  cost_usd: number; summary: { low_better: number; tie: number; high_better: number };
  by_task: { task_type: string; n: number; low_better: number; tie: number; high_better: number; suggestion?: string; to_tier?: Tier; weekly_delta_usd?: number }[] }
```

## /v1（OpenAI 兼容门面）

- `GET /v1/models` → 两个模型：`router-auto`（带工具请求走按量付费，按规则选档）、`router-sub`（无工具单轮，优先订阅）。
- `POST /v1/chat/completions`，支持 `stream: true`。请求里带 `tools` 且 `model=router-sub` 时返回 400，提示改用 `router-auto`。
- `POST /v1/images/generations` → `{data:[{b64_json, revised_prompt?}]}`，`size`、`quality` 映射到媒体档位。
- 响应头 `X-IMLS-Call-Id`、`X-IMLS-Model`。

## MCP 工具（`imls mcp`）

| 工具 | 参数 | 返回 |
|---|---|---|
| `generate_image` | `prompt, tier?=default, size?=1024x1024, out_dir?=assets` | `已生成 <路径>（<模型> · <费用>）` |
| `generate_video` | `prompt, seconds?=5, tier?=cheap, image_path?` | 90 秒内完成返回路径；否则 `任务 <job_id> 进行中，用 get_job 查询` |
| `make_animation` | `spec, seconds?=8, engine?=remotion` | 同上 |
| `ask_model` | `prompt, tier?=low` | 结果文本 + 末行 `（<模型> · <费用>）` |
| `get_job` | `job_id` | 进度或结果路径 |

工具返回永远是标准措辞，不走主题词表。

## 事件与通知

只发四类系统通知：额度到让出线、按量付费到上限、媒体完成或失败、规则建议出现。文本请求从不通知。
