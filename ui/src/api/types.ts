// 与 docs/API.md 一一对应；字段蛇形命名

export type Tier = 'low' | 'mid' | 'high' | 'top';
export type Lane = 'claude_sub' | 'codex_sub' | 'agy_sub' | 'api' | 'media';
export type Kind = 'text' | 'image' | 'video' | 'animation';
export type Mode = 'save' | 'balanced' | 'quality';
export type ThemeId = 'standard' | 'dianjiang' | 'harem' | 'seaking';
export type MediaTier = 'cheap' | 'default' | 'best' | 'free';
export type Source = 'mcp' | 'facade' | 'cli' | 'quick' | 'redo';

export interface Candidate { lane: Lane; provider?: string; model: string; display?: string; est_cost_usd?: number; skipped?: string }

export interface Call {
  id: string; ts: string; source: Source; host?: string; session_id?: string;
  kind: Kind; task_type: string; tier: Tier | null; media_tier?: MediaTier;
  lane: Lane; provider: string; model: string;
  reason: string[]; candidates: Candidate[]; fallback_from?: string;
  input_tokens?: number; output_tokens?: number; cache_read_tokens?: number;
  units?: number; unit?: 'image' | 'second';
  cost_usd: number; cost_native?: number; currency?: 'USD' | 'CNY';
  quota_units?: number; baseline_cost_usd: number;
  user_escalated: boolean; latency_ms: number; ok: boolean; error?: string;
  prompt_excerpt?: string; result_path?: string; job_id?: string;
}

export interface Decision {
  task_type: string; task_name?: string; tier_label?: string; tier: Tier | null; media_tier?: MediaTier; kind: Kind;
  candidates: Candidate[]; reason: string[]; sticky?: boolean;
}

export type JobStatus = 'queued' | 'submitted' | 'running' | 'done' | 'failed' | 'canceled' | 'timeout';
export interface Job {
  id: string; kind: 'video' | 'animation'; status: JobStatus; progress: number; eta_seconds?: number;
  provider: string; model: string; prompt_excerpt: string; result_path?: string; error?: string; cost_usd?: number;
  cost_native?: number; currency?: 'USD' | 'CNY'; seconds?: number; created_at: string; updated_at: string; call_id: string;
  retry_options?: { lane: Lane; model: string; est_cost_usd: number }[]; code_dir?: string;
  // 动画详情页用
  title?: string; render_rounds?: number; render_note?: string; size?: string; fps?: number; lane?: Lane;
}

export type AccountStatus = 'ok' | 'warn' | 'missing' | 'error' | 'cooldown' | 'disabled';
export interface Account {
  id: string; kind: 'subscription' | 'api'; provider: string; label: string; status: AccountStatus; detail: string;
  month_cost_usd?: number; month_cost_native?: number; currency?: string;
  quota?: { window: string; pct: number; resets_at?: string }[];
  tiers?: Record<string, string>; models: string[]; needs_proxy?: boolean; enabled: boolean; install_hint?: string;
  top_up_url?: string;
}

export interface Status {
  version: string; uptime_s: number; mode: Mode; theme: ThemeId; paused: boolean; projection: boolean;
  node_available: boolean; data_dir: string;
}

export interface Quota { account_id: string; label: string; provider?: string; window: string; pct: number; resets_at?: string; sub?: boolean; sub_label?: string }

export interface Overview {
  calls: number; calls_delta_pct?: number; api_cost_usd: number; baseline_cost_usd: number; saved_usd: number; saved_pct: number;
  escalation_rate: number; escalation_hot_task?: string;
  tier_dist: { low: number; mid: number; high: number; top: number; media: number };
  lane_dist: { sub: number; api: number; media: number };
  daily: { date: string; cost_usd: number; baseline_cost_usd: number }[];
  quotas: Quota[]; budget: { month_usd: number; limit_usd: number };
  top_tasks: { task_type: string; lane: 'sub' | 'api' | 'media'; metric: string }[];
}

export interface TaskRule { task_type: string; name?: string; kind?: string; how: string; tier: Tier; lanes: ('sub' | 'api')[]; count_7d: number; suggest?: Tier; builtin?: boolean }
export interface ExplicitRule { id: string; triggers: string[]; effect: string; tier?: Tier; builtin: boolean; enabled: boolean }
export interface ContextRule { id: string; title: string; off_effect: string; enabled: boolean }
export interface Rules { task_table: TaskRule[]; explicit: ExplicitRule[]; context_rules: ContextRule[]; classifier?: Record<string, unknown> }
export interface Suggestion { task_type: string; name?: string; escalations: number; from_tier: Tier; to_tier: Tier; weekly_delta_usd: number }

export type IntegrationId = 'claude_code' | 'hermes' | 'workbuddy';
export interface Integration {
  id: IntegrationId; status: 'connected' | 'disconnected' | 'manual';
  options: { write_claude_md?: boolean; opusplan?: boolean }; snippet?: string;
}

export interface Settings {
  monthly_api_limit_usd: number; subscription_yield_pct: number; default_mode: Mode; theme: ThemeId;
  hotkey: string; proxy: string; save_prompts: boolean; launch_at_login: boolean; data_dir: string; facade_key: string;
  auto_evaluate_monthly: boolean; notifications: { quota: boolean; budget: boolean; media: boolean; suggestions: boolean };
}

export interface Theme { id: ThemeId; name: string; words: Record<string, string> }

export interface Evaluation {
  id: string; status: 'running' | 'done' | 'failed'; started_at: string; finished_at?: string; sample: number; cost_usd: number;
  range_label?: string; delta_pct?: number;
  summary: { low_better: number; tie: number; high_better: number };
  by_task: { task_type: string; n: number; low_better: number; tie: number; high_better: number; suggestion?: string; to_tier?: Tier; weekly_delta_usd?: number }[];
}

export interface QuickPreview extends Decision { alternatives: { label: string; lane: Lane; provider: string; model: string; display?: string; est_cost_usd?: number; note?: string; tier?: Tier; media_tier?: MediaTier }[] }

export interface Onboarding { done: boolean; steps: { cli_detected: boolean; api_added: boolean; claude_connected: boolean; tried: boolean } }

export interface MediaLibrary { jobs: Job[]; items: Call[] }

export type QuickEvent =
  | { type: 'text_delta'; text: string }
  | { type: 'media'; path: string; kind: Kind }
  | { type: 'job'; job: Job }
  | { type: 'done'; call: Call }
  | { type: 'error'; message: string };

export type StreamEvent =
  | { type: 'quota'; data: { quotas: Quota[] } }
  | { type: 'job'; data: { job: Job } }
  | { type: 'call'; data: { call: Call } }
  | { type: 'notice'; data: { title: string; body: string; level?: 'info' | 'warn' | 'error' } }
  | { type: 'status'; data: Status };
