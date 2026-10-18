// 开发期 mock 后台：Node 内置 http，不装依赖。数据照设计稿示意值。
// 用法：npm run mock   （监听 127.0.0.1:18081；MOCK_ONBOARDING=1 可看首次引导）
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const PORT = 18081;
const now = () => new Date().toISOString();
const todayAt = (hm) => { const d = new Date(); const [h, m] = hm.split(':').map(Number); d.setHours(h, m, 0, 0); return d.toISOString(); };

// ---------- 状态 ----------
const THEME = process.env.MOCK_THEME || 'dianjiang'; // MOCK_THEME=standard 可换主题（截图用）
let status = { version: '0.1.0', uptime_s: 3600, mode: 'balanced', theme: THEME, paused: false, projection: false, node_available: true, data_dir: `${process.env.HOME}/Library/Application Support/Switchboard` };
let onboarding = { done: process.env.MOCK_ONBOARDING !== '1', steps: { cli_detected: false, api_added: false, claude_connected: false, tried: false } };

let settings = {
  monthly_api_limit_usd: 30, subscription_yield_pct: 70, default_mode: 'balanced', theme: THEME, hotkey: '⌥Space',
  proxy: '127.0.0.1:7890', save_prompts: true, launch_at_login: true, data_dir: status.data_dir, facade_key: 'imls-7f3a9c2e',
  auto_evaluate_monthly: false, notifications: { quota: true, budget: true, media: true, suggestions: true },
};

// ---------- 主题词表：直接读仓库里的 ----------
function themes() {
  const dir = path.resolve(__dirname, '../../internal/themes');
  return fs.readdirSync(dir).filter((f) => f.endsWith('.json')).map((f) => JSON.parse(fs.readFileSync(path.join(dir, f), 'utf8')));
}

// ---------- 额度 ----------
let quotas = [
  { account_id: 'claude', label: 'Claude Max', provider: 'anthropic', window: '5 小时窗口', pct: 46 },
  { account_id: 'claude', label: 'Claude Max', provider: 'anthropic', window: '本周', pct: 61 },
  { account_id: 'claude', label: '其中 Fable', provider: 'anthropic', window: '本周', pct: 18, sub: true },
  { account_id: 'codex', label: 'ChatGPT Plus', provider: 'openai', window: '5 小时窗口', pct: 82 },
];

// ---------- 请求记录 ----------
const calls = [
  { id: 'c8', ts: todayAt('14:18'), source: 'mcp', host: 'claude_code', kind: 'image', task_type: '封面图', tier: null, media_tier: 'default', lane: 'media', provider: 'openrouter', model: 'Nano Banana 2.1', reason: ['命中「封面图」：要一张图', '默认档候选：Nano Banana 2.1、Seedream 5.0'], candidates: [{ lane: 'media', provider: 'openrouter', model: 'Nano Banana 2.1' }, { lane: 'media', provider: 'volcengine', model: 'Seedream 5.0', skipped: '余额不足' }], units: 1, unit: 'image', cost_usd: 0.04, baseline_cost_usd: 0.04, user_escalated: false, latency_ms: 9000, ok: true, prompt_excerpt: '给公众号文章《模型路由》做一张 2.35:1 的封面，陶土色', result_path: '/Users/me/proj/assets/cover.png' },
  { id: 'c7', ts: todayAt('14:17'), source: 'mcp', host: 'claude_code', kind: 'text', task_type: '小段代码', tier: 'mid', lane: 'api', provider: 'zhipu', model: 'GLM-5.3', reason: ['命中「小段代码」：有代码块，42 行，没有报错栈', '中档候选：GLM-5.3、Claude Sonnet', '请求带工具，跳过订阅渠道'], candidates: [{ lane: 'api', provider: 'zhipu', model: 'GLM-5.3' }, { lane: 'claude_sub', provider: 'anthropic', model: 'Claude Sonnet', skipped: '请求带工具' }], input_tokens: 2100, output_tokens: 640, cost_usd: 0.004, cost_native: 0.03, currency: 'CNY', baseline_cost_usd: 0.02, user_escalated: false, latency_ms: 3200, ok: true, prompt_excerpt: '把这个函数改成支持批量传入，保留原签名…' },
  { id: 'c6', ts: todayAt('14:09'), source: 'mcp', host: 'claude_code', kind: 'text', task_type: '日志归类', tier: 'low', lane: 'api', provider: 'zhipu', model: 'GLM-5.3-Flash', reason: ['命中「摘要与归类」：长日志', '低档候选：GLM-5.3-Flash、Qwen3.8-Flash'], candidates: [{ lane: 'api', provider: 'zhipu', model: 'GLM-5.3-Flash' }], input_tokens: 8800, output_tokens: 300, cost_usd: 0.0014, cost_native: 0.01, currency: 'CNY', baseline_cost_usd: 0.07, user_escalated: false, latency_ms: 2100, ok: true, prompt_excerpt: '把这段日志按错误类型归类…' },
  { id: 'c5', ts: todayAt('13:52'), source: 'mcp', host: 'hermes', kind: 'text', task_type: '疑难调试', tier: 'high', lane: 'claude_sub', provider: 'anthropic', model: 'Opus 5.5', reason: ['命中「疑难调试」：有报错栈', '高档候选：Opus 5.5', '订阅额度 46%，优先订阅'], candidates: [{ lane: 'claude_sub', provider: 'anthropic', model: 'Opus 5.5' }], input_tokens: 12000, output_tokens: 1800, cost_usd: 0, quota_units: 3, baseline_cost_usd: 0.31, user_escalated: false, latency_ms: 24000, ok: true, prompt_excerpt: '排查这个死锁…' },
  { id: 'c4', ts: todayAt('13:47'), source: 'mcp', host: 'claude_code', kind: 'text', task_type: '架构设计', tier: 'top', lane: 'claude_sub', provider: 'anthropic', model: 'Fable 5.1', reason: ['显式指令「仔细想」→ 顶配', '顶配候选：Fable 5.1'], candidates: [{ lane: 'claude_sub', provider: 'anthropic', model: 'Fable 5.1' }], input_tokens: 9000, output_tokens: 2600, cost_usd: 0, quota_units: 6, baseline_cost_usd: 0.42, user_escalated: false, latency_ms: 41000, ok: true, prompt_excerpt: '仔细想：这套调度器的模块边界怎么划…' },
  { id: 'c3', ts: todayAt('13:40'), source: 'facade', host: 'workbuddy', kind: 'text', task_type: '改写', tier: 'low', lane: 'api', provider: 'bailian', model: 'Qwen3.8-Flash', reason: ['命中「改写与翻译」', '低档候选：Qwen3.8-Flash'], candidates: [{ lane: 'api', provider: 'bailian', model: 'Qwen3.8-Flash' }], input_tokens: 600, output_tokens: 500, cost_usd: 0.0002, cost_native: 0.0, currency: 'CNY', baseline_cost_usd: 0.03, user_escalated: false, latency_ms: 1500, ok: true, prompt_excerpt: '把这段话改得正式一点' },
  { id: 'c2', ts: todayAt('13:31'), source: 'mcp', host: 'claude_code', kind: 'text', task_type: '代码评审', tier: 'mid', lane: 'api', provider: 'openai', model: 'GPT-6.1 Sol', fallback_from: 'codex_sub', reason: ['命中「代码评审」：review', '中档候选：ChatGPT 订阅、GPT-6.1 Sol', 'ChatGPT 额度 82%，超过让出线，改走按量付费'], candidates: [{ lane: 'codex_sub', provider: 'openai', model: 'GPT-6.1', skipped: '额度超过让出线' }, { lane: 'api', provider: 'openai', model: 'GPT-6.1 Sol' }], input_tokens: 5000, output_tokens: 900, cost_usd: 0.02, baseline_cost_usd: 0.12, user_escalated: false, latency_ms: 6000, ok: true, prompt_excerpt: '帮我看看这个 PR' },
  { id: 'c1', ts: todayAt('13:05'), source: 'quick', kind: 'video', task_type: '短视频', tier: null, media_tier: 'cheap', lane: 'media', provider: 'volcengine', model: 'Seedance 2.0 Fast', reason: ['命中「短视频」：5 秒', '便宜档候选：Seedance 2.0 Fast'], candidates: [{ lane: 'media', provider: 'volcengine', model: 'Seedance 2.0 Fast' }], units: 5, unit: 'second', cost_usd: 0.55, cost_native: 4.0, currency: 'CNY', baseline_cost_usd: 0.55, user_escalated: false, latency_ms: 95000, ok: true, prompt_excerpt: '产品演示 5 秒', result_path: '/Users/me/proj/assets/demo.mp4', job_id: 'j0' },
];

// ---------- 规则 ----------
let rules = {
  task_table: [
    { task_type: '短问答', how: '300 字以内，无代码、无附件', tier: 'low', lanes: ['api', 'sub'], count_7d: 96 },
    { task_type: '改写与翻译', how: '改写、润色、翻译、转成表格', tier: 'low', lanes: ['api'], count_7d: 71 },
    { task_type: '摘要与归类', how: '总结、归类、提取；长日志', tier: 'low', lanes: ['api'], count_7d: 58 },
    { task_type: '小段代码', how: '有代码块，80 行以内，无报错栈', tier: 'mid', lanes: ['api', 'sub'], count_7d: 64, suggest: 'high' },
    { task_type: '代码评审', how: 'review、帮我看看、另一个思路', tier: 'mid', lanes: ['sub', 'api'], count_7d: 22 },
    { task_type: '疑难调试', how: '有报错栈，或反复失败', tier: 'high', lanes: ['sub'], count_7d: 31 },
    { task_type: '架构设计', how: '架构、方案、怎么设计、重构', tier: 'high', lanes: ['sub'], count_7d: 9 },
  ],
  explicit: [
    { id: 'e1', triggers: ['/opus', '/hard'], effect: 'tier', tier: 'high', builtin: true, enabled: true },
    { id: 'e2', triggers: ['/fable', '仔细想', '想清楚再答'], effect: 'tier', tier: 'top', builtin: true, enabled: true },
    { id: 'e3', triggers: ['/quick', '/cheap'], effect: 'tier', tier: 'low', builtin: true, enabled: true },
    { id: 'e4', triggers: ['/sub'], effect: '只用订阅', builtin: false, enabled: true },
    { id: 'e5', triggers: ['/img-best'], effect: '媒体用最好档', builtin: false, enabled: true },
  ],
  context_rules: [
    { id: 'ctx_100k', title: '超过 100K 不用低档', off_effect: '低档模型处理超长内容反而更贵', enabled: true },
    { id: 'ctx_60k', title: '超过 60K 不换模型', off_effect: '换模型要重读全部内容，又慢又贵', enabled: true },
    { id: 'ctx_sticky', title: '同一会话不换模型', off_effect: '只在新任务或分派子任务时重新选', enabled: true },
    { id: 'ctx_vision', title: '带图片时只用能看图的模型', off_effect: '关掉后图片会被忽略', enabled: true },
  ],
  classifier: { fallback: 'rules' },
};
let suggestions = [{ task_type: '小段代码', escalations: 6, from_tier: 'mid', to_tier: 'high', weekly_delta_usd: 1.8 }];

// ---------- 账号 ----------
let accounts = [
  { id: 'claude', kind: 'subscription', provider: 'anthropic', label: 'Claude', status: 'ok', detail: '用本机 claude 命令的登录，消耗你的订阅额度', quota: [{ window: '5 小时窗口', pct: 46 }, { window: '本周', pct: 61 }], tiers: { top: 'Fable', high: 'Opus', mid: 'Sonnet' }, models: ['Fable 5.1', 'Opus 5.5', 'Sonnet 5'], enabled: true },
  { id: 'codex', kind: 'subscription', provider: 'openai', label: 'ChatGPT', status: 'warn', detail: '用本机 codex 命令的登录；含订阅出图', quota: [{ window: '5 小时窗口', pct: 82 }], tiers: { mid: '评审', image: '出图' }, models: ['GPT-6.1', 'GPT Image 2.5'], enabled: true },
  { id: 'agy', kind: 'subscription', provider: 'google', label: 'Google AI', status: 'missing', detail: '未检测到 agy 命令；安装并登录后自动出现', models: [], enabled: false, install_hint: 'npm install -g @google/agy && agy login' },
  { id: 'zhipu', kind: 'api', provider: 'zhipu', label: '智谱', status: 'ok', detail: 'GLM-5.3、GLM-5.3-Flash', month_cost_usd: 1.14, month_cost_native: 8.2, currency: 'CNY', models: ['GLM-5.3', 'GLM-5.3-Flash'], enabled: true },
  { id: 'bailian', kind: 'api', provider: 'bailian', label: '阿里云百炼', status: 'ok', detail: 'Qwen3.8-Flash、Qwen-Image', month_cost_usd: 0.43, month_cost_native: 3.1, currency: 'CNY', models: ['Qwen3.8-Flash', 'Qwen-Image'], enabled: true },
  { id: 'volcengine', kind: 'api', provider: 'volcengine', label: '火山方舟', status: 'error', detail: 'Seedream 5.0、Seedance 2.0', month_cost_usd: 2.2, month_cost_native: 16, currency: 'CNY', models: ['Seedream 5.0', 'Seedance 2.0'], enabled: true, top_up_url: 'https://console.volcengine.com/ark' },
  { id: 'openrouter', kind: 'api', provider: 'openrouter', label: 'OpenRouter', status: 'ok', detail: 'Nano Banana 2.1、Veo 3.1；需要代理', month_cost_usd: 2.6, currency: 'USD', models: ['Nano Banana 2.1', 'Veo 3.1'], needs_proxy: true, enabled: true },
];

// ---------- 媒体 ----------
let jobs = [
  { id: 'j1', kind: 'video', status: 'running', progress: 60, eta_seconds: 40, provider: 'volcengine', model: 'Seedance 2.0 Fast', prompt_excerpt: '产品介绍 5 秒', seconds: 5, created_at: now(), updated_at: now(), call_id: 'c9' },
  { id: 'j2', kind: 'animation', status: 'running', progress: 25, provider: 'anthropic', model: 'Opus 写代码', prompt_excerpt: '标题动效 8 秒', seconds: 8, created_at: now(), updated_at: now(), call_id: 'c10', title: '标题动效', lane: 'claude_sub', render_rounds: 2, render_note: '2 轮，第 1 轮报错已自动修复', size: '1920 × 1080', fps: 30, code_dir: '/Users/me/proj/.imls/anim-j2', result_path: 'assets/title-intro.mp4' },
  { id: 'j3', kind: 'video', status: 'failed', progress: 0, provider: 'openrouter', model: 'Veo 3.1 Lite', prompt_excerpt: '城市夜景', error: '生成失败：内容未通过审核', created_at: now(), updated_at: now(), call_id: 'c11', retry_options: [{ lane: 'media', model: 'Seedance 2.0 Fast', est_cost_usd: 0.55 }, { lane: 'media', model: 'Veo 3.1', est_cost_usd: 1.2 }] },
];
const mediaItems = [
  { id: 'm1', ts: todayAt('14:18'), source: 'mcp', host: 'claude_code', kind: 'image', task_type: '公众号封面', tier: null, media_tier: 'default', lane: 'media', provider: 'openrouter', model: 'Nano Banana 2.1', reason: [], candidates: [], cost_usd: 0.04, baseline_cost_usd: 0.04, user_escalated: false, latency_ms: 9000, ok: true, result_path: '/Users/me/proj/assets/cover.png' },
  { id: 'm2', ts: todayAt('11:02'), source: 'mcp', host: 'claude_code', kind: 'image', task_type: 'README 头图', tier: null, media_tier: 'cheap', lane: 'media', provider: 'volcengine', model: 'Seedream 5.0', reason: [], candidates: [], cost_usd: 0.017, cost_native: 0.12, currency: 'CNY', baseline_cost_usd: 0.04, user_escalated: false, latency_ms: 7000, ok: true, result_path: '/Users/me/proj/assets/readme.png' },
  { id: 'm3', ts: todayAt('13:05'), source: 'quick', kind: 'video', task_type: '产品演示', tier: null, media_tier: 'cheap', lane: 'media', provider: 'volcengine', model: 'Seedance 2.0', reason: [], candidates: [], units: 5, unit: 'second', cost_usd: 0.55, cost_native: 4, currency: 'CNY', baseline_cost_usd: 0.55, user_escalated: false, latency_ms: 95000, ok: true, result_path: '/Users/me/proj/assets/demo.mp4', job_id: 'j0' },
  { id: 'm4', ts: todayAt('09:40'), source: 'mcp', host: 'hermes', kind: 'image', task_type: '架构插图', tier: null, media_tier: 'free', lane: 'codex_sub', provider: 'openai', model: 'ChatGPT 订阅出图', reason: [], candidates: [], cost_usd: 0, baseline_cost_usd: 0.04, user_escalated: false, latency_ms: 30000, ok: true, result_path: '/Users/me/proj/assets/arch.png' },
];

// ---------- 接入 ----------
let integrations = [
  { id: 'claude_code', status: 'connected', options: { write_claude_md: true, opusplan: false }, calls_7d: 186, last_call: '2026-10-18T14:18:00+08:00' },
  { id: 'codex', status: 'connected', options: {}, calls_7d: 41, last_call: '2026-10-18T11:02:00+08:00' },
  { id: 'gemini_cli', status: 'disconnected', options: {}, calls_7d: 0 },
  { id: 'workbuddy', status: 'connected', options: {}, calls_7d: 23, snippet: '模型 1：router-auto\n模型 2：router-sub\n地址：http://127.0.0.1:18081/v1\n密钥：imls-7f3a9c2e' },
  { id: 'doubao_work', status: 'manual', options: {}, calls_7d: 0, snippet: '命令：imls\n参数：mcp' },
  { id: 'deepseek_harness', status: 'connected', options: {}, calls_7d: 0, snippet: 'providers.switchboard …' },
  { id: 'pi', status: 'connected', options: {}, calls_7d: 12, snippet: '{ "providers": { "switchboard": … } }' },
  { id: 'openclaw', status: 'connected', options: {}, calls_7d: 9, snippet: '{ "models": { "providers": { "switchboard": … } } }' },
  { id: 'hermes', status: 'disconnected', options: {}, calls_7d: 0 },
];

// ---------- 评估 ----------
let evaluations = [
  { id: 'ev1', status: 'done', started_at: '2026-10-15T02:00:00+08:00', finished_at: '2026-10-15T02:11:00+08:00', sample: 100, cost_usd: 1.38, range_label: '10 月 1 日至 14 日', delta_pct: 4,
    summary: { low_better: 12, tie: 70, high_better: 18 },
    by_task: [
      { task_type: '短问答', n: 30, low_better: 4, tie: 24, high_better: 2 },
      { task_type: '改写与翻译', n: 22, low_better: 3, tie: 18, high_better: 1 },
      { task_type: '摘要与归类', n: 20, low_better: 2, tie: 15, high_better: 3 },
      { task_type: '小段代码', n: 18, low_better: 1, tie: 9, high_better: 8, suggestion: '调到高档', to_tier: 'high', weekly_delta_usd: 1.8 },
      { task_type: '代码评审', n: 10, low_better: 2, tie: 4, high_better: 4, suggestion: '先用订阅渠道' },
    ] },
];

// ---------- 概览 ----------
function overview(range) {
  const base = {
    tier_dist: { low: 57, mid: 31, high: 12, top: 0, media: 7 }, lane_dist: { sub: 38, api: 55, media: 7 },
    daily: [
      { date: '2026-10-12', cost_usd: 0.62, baseline_cost_usd: 2.9 }, { date: '2026-10-13', cost_usd: 0.74, baseline_cost_usd: 3.8 },
      { date: '2026-10-14', cost_usd: 0.58, baseline_cost_usd: 2.5 }, { date: '2026-10-15', cost_usd: 0.9, baseline_cost_usd: 4.8 },
      { date: '2026-10-16', cost_usd: 0.77, baseline_cost_usd: 3.5 }, { date: '2026-10-17', cost_usd: 0.9, baseline_cost_usd: 2.0 },
      { date: '2026-10-18', cost_usd: 0.41, baseline_cost_usd: 3.67 },
    ],
    quotas, budget: { month_usd: 6.8, limit_usd: settings.monthly_api_limit_usd },
    top_tasks: [{ task_type: '疑难调试', lane: 'sub', metric: '31 次' }, { task_type: '生视频', lane: 'media', metric: '$2.40' }, { task_type: '小段代码', lane: 'api', metric: '$1.12' }],
  };
  if (range === 'today') return { calls: 58, calls_delta_pct: 12, api_cost_usd: 0.41, baseline_cost_usd: 3.67, saved_usd: 3.26, saved_pct: 89, escalation_rate: 5.1, escalation_hot_task: '小段代码', ...base };
  if (range === '30d') return { calls: 1630, calls_delta_pct: 9, api_cost_usd: 18.4, baseline_cost_usd: 96.1, saved_usd: 77.7, saved_pct: 81, escalation_rate: 7.0, escalation_hot_task: '小段代码', ...base };
  return { calls: 412, calls_delta_pct: 18, api_cost_usd: 4.92, baseline_cost_usd: 26.62, saved_usd: 21.7, saved_pct: 41, escalation_rate: 6.3, escalation_hot_task: '小段代码', ...base };
}

// ---------- 快速面板 ----------
const isMedia = (p) => /封面|图|画|海报|插图|视频|动效|动画/.test(p);
function quickPreview({ prompt }) {
  if (isMedia(prompt)) {
    return {
      task_type: '封面图', tier: null, media_tier: 'default', kind: /视频/.test(prompt) ? 'video' : 'image',
      candidates: [{ lane: 'media', provider: 'openrouter', model: 'Nano Banana 2.1', est_cost_usd: 0.04 }],
      reason: ['命中「封面图」'],
      alternatives: [
        { label: '便宜档', lane: 'media', provider: 'volcengine', model: 'Seedream 5.0 Flash', note: '¥0.12', media_tier: 'cheap' },
        { label: '最好档', lane: 'media', provider: 'openai', model: 'GPT Image 2.5', est_cost_usd: 0.17, media_tier: 'best' },
        { label: '用', lane: 'codex_sub', provider: 'openai', model: 'ChatGPT 订阅出图', note: '不花钱，画质一般', media_tier: 'free' },
      ],
    };
  }
  return {
    task_type: '短问答', tier: 'low', kind: 'text',
    candidates: [{ lane: 'api', provider: 'zhipu', model: 'GLM-5.3-Flash', est_cost_usd: 0.0003 }],
    reason: ['命中「短问答」：300 字以内'],
    alternatives: [
      { label: '中档', lane: 'api', provider: 'zhipu', model: 'GLM-5.3', est_cost_usd: 0.004, tier: 'mid' },
      { label: '高档', lane: 'claude_sub', provider: 'anthropic', model: 'Opus 5.5', note: '订阅', tier: 'high' },
      { label: '顶配', lane: 'claude_sub', provider: 'anthropic', model: 'Fable 5.1', note: '订阅', tier: 'top' },
    ],
  };
}

// ---------- SSE 订阅者 ----------
const clients = new Set();
function broadcast(event, data) {
  for (const res of clients) res.write(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`);
}
setInterval(() => {
  quotas = quotas.map((q) => (q.sub ? q : { ...q, pct: Math.max(5, Math.min(99, q.pct + (Math.random() < 0.5 ? 1 : -1))) }));
  broadcast('quota', { quotas });
}, 5000);
setInterval(() => {
  let changed = false;
  jobs = jobs.map((j) => {
    if (j.status !== 'running') return j;
    const p = Math.min(100, j.progress + 10);
    changed = true;
    if (p >= 100) {
      const done = { ...j, status: 'done', progress: 100, updated_at: now(), result_path: j.result_path || `/Users/me/proj/assets/${j.id}.mp4`, cost_usd: j.kind === 'video' ? 0.55 : 0, cost_native: j.kind === 'video' ? 4 : undefined, currency: j.kind === 'video' ? 'CNY' : undefined };
      setTimeout(() => broadcast('notice', { title: j.kind === 'video' ? '视频已生成' : '动画已生成', body: `${j.prompt_excerpt} · ${j.model}${j.kind === 'video' ? ' · ¥4.00' : ''}`, level: 'info' }), 200);
      return done;
    }
    return { ...j, progress: p, eta_seconds: Math.max(0, (j.eta_seconds ?? 60) - 6), updated_at: now() };
  });
  if (changed) for (const j of jobs) if (j.status === 'running' || j.progress === 100) broadcast('job', { job: j });
}, 6000);

// ---------- 路由 ----------
const json = (res, code, body) => { res.writeHead(code, { 'Content-Type': 'application/json; charset=utf-8' }); res.end(body === undefined ? '' : JSON.stringify(body)); };
const readBody = (req) => new Promise((resolve) => { let b = ''; req.on('data', (c) => (b += c)); req.on('end', () => resolve(b)); });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const server = http.createServer(async (req, res) => {
  const origin = req.headers.origin || '*';
  res.setHeader('Access-Control-Allow-Origin', origin);
  res.setHeader('Access-Control-Allow-Headers', 'Content-Type, X-IMLS-Client');
  res.setHeader('Access-Control-Allow-Methods', 'GET, POST, PUT, DELETE, OPTIONS');
  if (req.method === 'OPTIONS') { res.writeHead(204); return res.end(); }

  const url = new URL(req.url, `http://127.0.0.1:${PORT}`);
  const p = url.pathname; const m = req.method;
  const raw = m === 'GET' ? '' : await readBody(req);
  let body = {}; try { body = raw ? JSON.parse(raw) : {}; } catch { body = {}; }
  const seg = p.split('/').filter(Boolean); // ['api', 'calls', 'c7', 'escalate']

  try {
    if (p === '/api/events') {
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive' });
      res.write(`event: status\ndata: ${JSON.stringify(status)}\n\n`);
      res.write(`event: quota\ndata: ${JSON.stringify({ quotas })}\n\n`);
      clients.add(res); req.on('close', () => clients.delete(res));
      return;
    }
    if (p === '/api/status') return json(res, 200, status);
    if (p === '/api/pause') { status = { ...status, paused: !!body.paused }; broadcast('status', status); return json(res, 200, status); }
    if (p === '/api/mode') { status = { ...status, mode: body.mode }; broadcast('status', status); return json(res, 200, status); }
    if (p === '/api/projection') { status = { ...status, projection: !!body.on }; broadcast('status', status); return json(res, 200, status); }
    if (p === '/api/overview') return json(res, 200, overview(url.searchParams.get('range') || '7d'));
    if (p === '/api/calls' && m === 'GET') {
      const q = url.searchParams; let out = calls;
      if (q.get('source')) out = out.filter((c) => (c.host || c.source) === q.get('source') || c.source === q.get('source'));
      if (q.get('lane')) { const l = q.get('lane'); out = out.filter((c) => (l === 'sub' ? c.lane.endsWith('_sub') : c.lane === l)); }
      if (q.get('tier')) out = out.filter((c) => c.tier === q.get('tier'));
      if (q.get('q')) { const s = q.get('q').toLowerCase(); out = out.filter((c) => (c.prompt_excerpt || '').toLowerCase().includes(s) || c.model.toLowerCase().includes(s) || c.task_type.includes(s)); }
      return json(res, 200, out);
    }
    if (seg[1] === 'calls' && seg[2]) {
      const c = calls.find((x) => x.id === seg[2]); if (!c) return json(res, 404, { error: '没有这条记录' });
      if (!seg[3]) return json(res, 200, c);
      if (seg[3] === 'escalate') { c.user_escalated = true; return json(res, 200, { ok: true, escalations_for_task: 7 }); }
      if (seg[3] === 'redo') {
        await sleep(600);
        const redo = { ...c, id: `r${Date.now()}`, ts: now(), source: 'redo', tier: body.tier || 'high', lane: 'claude_sub', provider: 'anthropic', model: body.model || (body.tier === 'top' ? 'Fable 5.1' : 'Opus 5.5'), cost_usd: 0, reason: ['手动重做'], user_escalated: false };
        calls.unshift(redo); broadcast('call', { call: redo }); return json(res, 200, redo);
      }
    }
    if (p === '/api/rules' && m === 'GET') return json(res, 200, rules);
    if (p === '/api/rules' && m === 'PUT') { rules = body; return json(res, 200, rules); }
    if (p === '/api/rules/try') {
      const t = body.prompt || '';
      if (/报错|栈|死锁|崩/.test(t)) return json(res, 200, { task_type: '疑难调试', tier: 'high', kind: 'text', reason: ['有报错栈'], candidates: [{ lane: 'claude_sub', provider: 'anthropic', model: 'Opus 5.5', est_cost_usd: 0 }, { lane: 'api', provider: 'openai', model: 'GPT-6 Astra', est_cost_usd: 0.2, skipped: '额度紧张时改用' }] });
      if (/架构|方案|设计|重构/.test(t)) return json(res, 200, { task_type: '架构设计', tier: 'high', kind: 'text', reason: ['架构、方案、怎么设计、重构'], candidates: [{ lane: 'claude_sub', provider: 'anthropic', model: 'Opus 5.5', est_cost_usd: 0 }] });
      if (/```|function|def |const /.test(t)) return json(res, 200, { task_type: '小段代码', tier: 'mid', kind: 'text', reason: ['有代码块，80 行以内，无报错栈'], candidates: [{ lane: 'api', provider: 'zhipu', model: 'GLM-5.3', est_cost_usd: 0.004 }, { lane: 'claude_sub', provider: 'anthropic', model: 'Claude Sonnet', est_cost_usd: 0 }] });
      if (/翻译|改写|润色/.test(t)) return json(res, 200, { task_type: '改写与翻译', tier: 'low', kind: 'text', reason: ['改写、润色、翻译、转成表格'], candidates: [{ lane: 'api', provider: 'bailian', model: 'Qwen3.8-Flash', est_cost_usd: 0.0002 }] });
      return json(res, 200, { task_type: '短问答', tier: 'low', kind: 'text', reason: ['300 字以内，无代码、无附件'], candidates: [{ lane: 'api', provider: 'zhipu', model: 'GLM-5.3-Flash', est_cost_usd: 0.0003 }, { lane: 'claude_sub', provider: 'anthropic', model: 'Claude Haiku', est_cost_usd: 0 }] });
    }
    if (p === '/api/rules/suggestions' && m === 'GET') return json(res, 200, suggestions);
    if (seg[1] === 'rules' && seg[2] === 'suggestions' && seg[3]) {
      const task = decodeURIComponent(seg[3]);
      if (body.action === 'accept') { const s = suggestions.find((x) => x.task_type === task); const r = rules.task_table.find((x) => x.task_type === task); if (s && r) { r.tier = s.to_tier; delete r.suggest; } }
      else { const r = rules.task_table.find((x) => x.task_type === task); if (r) delete r.suggest; }
      suggestions = suggestions.filter((x) => x.task_type !== task); return json(res, 200, rules);
    }
    if (p === '/api/rules/export') { res.writeHead(200, { 'Content-Type': 'text/yaml; charset=utf-8' }); return res.end(`# Switchboard 规则\ntask_table:\n${rules.task_table.map((r) => `  - task_type: ${r.task_type}\n    tier: ${r.tier}\n`).join('')}`); }
    if (p === '/api/rules/import') return json(res, 200, rules);
    if (p === '/api/accounts' && m === 'GET') return json(res, 200, accounts);
    if (p === '/api/accounts' && m === 'POST') {
      await sleep(800);
      if (!body.api_key || body.api_key.length < 8) return json(res, 400, { error: `${body.provider === 'zhipu' ? '智谱' : body.provider} 验证失败：密钥无效或已过期` });
      const labels = { zhipu: '智谱', bailian: '百炼', deepseek: 'DeepSeek', moonshot: 'Moonshot', minimax: 'MiniMax', openrouter: 'OpenRouter', volcengine: '火山方舟' };
      const a = { id: `${body.provider}_${Date.now()}`, kind: 'api', provider: body.provider, label: labels[body.provider] || body.provider, status: 'ok', detail: '刚添加', month_cost_usd: 0, currency: 'USD', models: [], enabled: true };
      accounts.push(a); onboarding.steps.api_added = true; return json(res, 200, a);
    }
    if (p === '/api/accounts/detect') { await sleep(700); onboarding.steps.cli_detected = true; return json(res, 200, accounts); }
    if (seg[1] === 'accounts' && seg[2]) {
      const a = accounts.find((x) => x.id === seg[2]); if (!a) return json(res, 404, { error: '没有这个账号' });
      if (m === 'DELETE') { accounts = accounts.filter((x) => x !== a); return json(res, 200, { ok: true }); }
      if (m === 'PUT') { Object.assign(a, body.enabled !== undefined ? { enabled: body.enabled, status: body.enabled ? 'ok' : 'disabled' } : {}, body.tiers ? { tiers: body.tiers } : {}); return json(res, 200, a); }
      if (seg[3] === 'verify') { await sleep(600); return json(res, 200, a); }
    }
    if (p === '/api/media') {
      const k = url.searchParams.get('kind') || 'all';
      return json(res, 200, { jobs: k === 'all' ? jobs : jobs.filter((j) => j.kind === k), items: k === 'all' ? mediaItems : mediaItems.filter((i) => i.kind === k) });
    }
    if (seg[1] === 'jobs' && seg[2]) {
      const j = jobs.find((x) => x.id === seg[2]); if (!j) return json(res, 404, { error: '没有这个任务' });
      if (!seg[3]) return json(res, 200, j);
      if (seg[3] === 'cancel') { j.status = 'canceled'; broadcast('job', { job: j }); return json(res, 200, j); }
      if (seg[3] === 'retry') { Object.assign(j, { status: 'running', progress: 5, error: undefined, model: body.model || j.retry_options?.[0]?.model || j.model }); broadcast('job', { job: j }); return json(res, 200, j); }
      if (seg[3] === 'revise') { Object.assign(j, { status: 'running', progress: 10, render_rounds: (j.render_rounds || 0) + 1 }); broadcast('job', { job: j }); return json(res, 200, j); }
    }
    if (p === '/api/integrations' && m === 'GET') return json(res, 200, integrations);
    if (seg[1] === 'integrations' && seg[2]) {
      const it = integrations.find((x) => x.id === seg[2]); if (!it) return json(res, 404, { error: '没有这个宿主' });
      await sleep(500);
      if (seg[3] === 'connect') { it.status = it.id === 'workbuddy' ? 'manual' : 'connected'; if (body.options) it.options = { ...it.options, ...body.options }; if (it.id === 'claude_code') onboarding.steps.claude_connected = true; return json(res, 200, it); }
      if (seg[3] === 'disconnect') { it.status = 'disconnected'; return json(res, 200, it); }
      if (m === 'PUT') { it.options = { ...it.options, ...body.options }; return json(res, 200, it); }
    }
    if (p === '/api/settings' && m === 'GET') return json(res, 200, settings);
    if (p === '/api/settings' && m === 'PUT') { settings = { ...settings, ...body, notifications: { ...settings.notifications, ...(body.notifications || {}) } }; if (body.theme) { status = { ...status, theme: body.theme }; broadcast('status', status); } return json(res, 200, settings); }
    if (p === '/api/themes') return json(res, 200, themes());
    if (p === '/api/evaluations' && m === 'GET') return json(res, 200, evaluations);
    if (p === '/api/evaluations' && m === 'POST') {
      const ev = { id: `ev${Date.now()}`, status: 'running', started_at: now(), sample: body.sample || 100, cost_usd: 0, summary: { low_better: 0, tie: 0, high_better: 0 }, by_task: [] };
      evaluations.unshift(ev);
      setTimeout(() => { Object.assign(ev, evaluations[evaluations.length - 1], { id: ev.id, status: 'done', finished_at: now(), range_label: '最近 14 天', delta_pct: 2 }); broadcast('notice', { title: '评估完成', body: '有 2 项建议调整', level: 'info' }); }, 8000);
      return json(res, 200, ev);
    }
    if (seg[1] === 'evaluations' && seg[2]) {
      const ev = evaluations.find((x) => x.id === seg[2]); if (!ev) return json(res, 404, { error: '没有这轮评估' });
      if (!seg[3]) return json(res, 200, ev);
      if (seg[3] === 'accept') { const row = ev.by_task.find((x) => x.task_type === body.task_type); const r = rules.task_table.find((x) => x.task_type === body.task_type); if (row && r && row.to_tier) r.tier = row.to_tier; if (row) delete row.suggestion; return json(res, 200, rules); }
    }
    if (p === '/api/quick/preview') { await sleep(150); return json(res, 200, quickPreview(body)); }
    if (p === '/api/quick/run') {
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' });
      const send = (ev, d) => res.write(`event: ${ev}\ndata: ${JSON.stringify(d)}\n\n`);
      const pv = quickPreview(body);
      const model = body.model || pv.candidates[0].model;
      if (pv.kind !== 'text') {
        await sleep(1200);
        if (pv.kind === 'video') {
          const job = { id: `jq${Date.now()}`, kind: 'video', status: 'running', progress: 10, eta_seconds: 60, provider: 'volcengine', model: 'Seedance 2.0 Fast', prompt_excerpt: body.prompt.slice(0, 30), seconds: 5, created_at: now(), updated_at: now(), call_id: 'cq' };
          jobs.unshift(job); send('job', { job }); broadcast('job', { job });
        } else {
          send('media', { path: '/Users/me/proj/assets/cover.png', kind: 'image' });
        }
        const call = { id: `cq${Date.now()}`, ts: now(), source: 'quick', kind: pv.kind, task_type: pv.task_type, tier: null, media_tier: pv.media_tier, lane: 'media', provider: 'openrouter', model, reason: pv.reason, candidates: pv.candidates, cost_usd: 0.04, baseline_cost_usd: 0.04, user_escalated: false, latency_ms: 9000, ok: true, prompt_excerpt: body.prompt, result_path: '/Users/me/proj/assets/cover.png' };
        calls.unshift(call); send('done', { call }); broadcast('call', { call }); onboarding.steps.tried = true; return res.end();
      }
      const parts = ['用 `itertools.groupby` 前先按同一字段排序，', '或者用 `defaultdict(list)` 一次遍历搞定。', '数据量大、只需要分组时，后者更快。'];
      for (const t of parts) { await sleep(350); send('text_delta', { text: t }); }
      const call = { id: `cq${Date.now()}`, ts: now(), source: 'quick', kind: 'text', task_type: pv.task_type, tier: body.tier || pv.tier, lane: body.lane || 'api', provider: body.tier === 'high' || body.tier === 'top' ? 'anthropic' : 'zhipu', model, reason: pv.reason, candidates: pv.candidates, cost_usd: 0.0003, cost_native: 0.002, currency: 'CNY', baseline_cost_usd: 0.02, user_escalated: false, latency_ms: 1400, ok: true, prompt_excerpt: body.prompt };
      calls.unshift(call); send('done', { call }); broadcast('call', { call }); onboarding.steps.tried = true; return res.end();
    }
    if (p === '/api/onboarding' && m === 'GET') return json(res, 200, onboarding);
    if (p === '/api/onboarding/done') { onboarding = { ...onboarding, done: true }; return json(res, 200, onboarding); }
    json(res, 404, { error: `没有这个接口：${m} ${p}` });
  } catch (e) {
    json(res, 500, { error: String(e) });
  }
});

server.listen(PORT, '127.0.0.1', () => console.log(`mock imls serve → http://127.0.0.1:${PORT}  (MOCK_ONBOARDING=${process.env.MOCK_ONBOARDING || 0})`));
