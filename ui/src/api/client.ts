import type { Quota,
  Account, Call, Decision, Evaluation, Integration, IntegrationId, Job, MediaLibrary, Mode, Onboarding, Overview,
  QuickEvent, QuickPreview, Rules, Settings, Status, StreamEvent, Suggestion, Theme, Tier,
} from './types';

export const BASE = 'http://127.0.0.1:18081';
const HEADERS = { 'X-IMLS-Client': 'ui' };

export class ApiError extends Error {
  constructor(public status: number, message: string, public detail?: string) { super(message); }
}

/** 后台不可达时抛 ApiError(0)，界面据此显示「后台未运行」 */
async function request<T>(method: string, path: string, body?: unknown, raw?: { contentType: string; text: string }): Promise<T> {
  let res: Response;
  try {
    res = await fetch(BASE + path, {
      method,
      headers: raw ? { ...HEADERS, 'Content-Type': raw.contentType } : body !== undefined ? { ...HEADERS, 'Content-Type': 'application/json' } : HEADERS,
      body: raw ? raw.text : body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch (e) {
    throw new ApiError(0, '后台未运行', String(e));
  }
  const text = await res.text();
  if (!res.ok) {
    let msg = `请求失败：HTTP ${res.status}`;
    try { const j = JSON.parse(text); if (j.error) msg = j.error; else if (j.message) msg = j.message; } catch { /* 原文放详情 */ }
    throw new ApiError(res.status, msg, text);
  }
  if (!text) return undefined as T;
  const ct = res.headers.get('content-type') || '';
  if (ct.includes('json')) return JSON.parse(text) as T;
  return text as unknown as T;
}

const get = <T,>(p: string) => request<T>('GET', p);
const post = <T,>(p: string, b?: unknown) => request<T>('POST', p, b);
const put = <T,>(p: string, b?: unknown) => request<T>('PUT', p, b);
const del = <T,>(p: string) => request<T>('DELETE', p);

const qs = (o: Record<string, string | number | boolean | undefined>) => {
  const u = new URLSearchParams();
  for (const [k, v] of Object.entries(o)) if (v !== undefined && v !== '') u.set(k, String(v));
  const s = u.toString();
  return s ? `?${s}` : '';
};

export const api = {
  status: () => get<Status>('/api/status'),
  pause: (paused: boolean) => post<Status>('/api/pause', { paused }),
  mode: (mode: Mode) => post<Status>('/api/mode', { mode }),
  projection: (on: boolean) => post<Status>('/api/projection', { on }),
  overview: (range: 'today' | '7d' | '30d') => get<Overview>(`/api/overview?range=${range}`),
  refreshQuota: () => post<{ started: boolean; quotas: Quota[] }>('/api/quota/refresh'),
  calls: (q: { source?: string; lane?: string; tier?: string; q?: string; limit?: number; before?: string } = {}) => get<Call[]>(`/api/calls${qs(q)}`),
  call: (id: string) => get<Call>(`/api/calls/${id}`),
  escalate: (id: string) => post<{ ok: boolean; escalations_for_task: number }>(`/api/calls/${id}/escalate`),
  redo: (id: string, body: { tier?: Tier; model?: string } = {}) => post<Call>(`/api/calls/${id}/redo`, body),
  rules: () => get<Rules>('/api/rules'),
  saveRules: (r: Rules) => put<Rules>('/api/rules', r),
  tryRule: (body: { prompt: string; has_tools?: boolean; est_tokens?: number; session_id?: string }) => post<Decision>('/api/rules/try', body),
  suggestions: () => get<Suggestion[]>('/api/rules/suggestions'),
  suggestionAction: (task: string, action: 'accept' | 'ignore') => post<Rules>(`/api/rules/suggestions/${encodeURIComponent(task)}`, { action }),
  exportRules: () => get<string>('/api/rules/export'),
  importRules: (yaml: string) => request<Rules>('POST', '/api/rules/import', undefined, { contentType: 'text/yaml', text: yaml }),
  accounts: () => get<Account[]>('/api/accounts'),
  addAccount: (b: { provider: string; api_key: string; base_url?: string }) => post<Account>('/api/accounts', b),
  updateAccount: (id: string, b: { enabled?: boolean; tiers?: Record<string, string> }) => put<Account>(`/api/accounts/${id}`, b),
  deleteAccount: (id: string) => del<{ ok: boolean }>(`/api/accounts/${id}`),
  verifyAccount: (id: string) => post<Account>(`/api/accounts/${id}/verify`),
  detectAccounts: () => post<Account[]>('/api/accounts/detect'),
  media: (kind: 'all' | 'image' | 'video' | 'animation') => get<MediaLibrary>(`/api/media?kind=${kind}`),
  job: (id: string) => get<Job>(`/api/jobs/${id}`),
  cancelJob: (id: string) => post<Job>(`/api/jobs/${id}/cancel`),
  retryJob: (id: string, model?: string) => post<Job>(`/api/jobs/${id}/retry`, { model }),
  reviseJob: (id: string, instruction: string) => post<Job>(`/api/jobs/${id}/revise`, { instruction }),
  integrations: () => get<Integration[]>('/api/integrations'),
  connect: (id: IntegrationId, options?: Integration['options']) => post<Integration>(`/api/integrations/${id}/connect`, { options }),
  disconnect: (id: IntegrationId) => post<Integration>(`/api/integrations/${id}/disconnect`),
  setIntegration: (id: IntegrationId, options: Integration['options']) => put<Integration>(`/api/integrations/${id}`, { options }),
  settings: () => get<Settings>('/api/settings'),
  saveSettings: (s: Partial<Settings>) => put<Settings>('/api/settings', s),
  themes: () => get<Theme[]>('/api/themes'),
  evaluations: () => get<Evaluation[]>('/api/evaluations'),
  startEvaluation: (sample?: number) => post<Evaluation>('/api/evaluations', { sample }),
  evaluation: (id: string) => get<Evaluation>(`/api/evaluations/${id}`),
  acceptEvaluation: (id: string, task_type: string) => post<Rules>(`/api/evaluations/${id}/accept`, { task_type }),
  quickPreview: (b: { prompt: string; image_path?: string }) => post<QuickPreview>('/api/quick/preview', b),
  onboarding: () => get<Onboarding>('/api/onboarding'),
  onboardingDone: () => post<void>('/api/onboarding/done'),
};

/** /api/quick/run：POST 后按 SSE 格式逐段读 */
export async function quickRun(
  body: { prompt: string; image_path?: string; tier?: Tier; model?: string; lane?: string },
  onEvent: (e: QuickEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  let res: Response;
  try {
    res = await fetch(`${BASE}/api/quick/run`, { method: 'POST', headers: { ...HEADERS, 'Content-Type': 'application/json' }, body: JSON.stringify(body), signal });
  } catch (e) {
    throw new ApiError(0, '后台未运行', String(e));
  }
  if (!res.ok || !res.body) throw new ApiError(res.status, `请求失败：HTTP ${res.status}`, await res.text());
  const reader = res.body.getReader();
  const dec = new TextDecoder();
  let buf = '';
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    let idx: number;
    while ((idx = buf.indexOf('\n\n')) >= 0) {
      const chunk = buf.slice(0, idx); buf = buf.slice(idx + 2);
      const ev = parseSse(chunk);
      if (ev) onEvent({ type: ev.event, ...ev.data } as QuickEvent);
    }
  }
}

function parseSse(chunk: string): { event: string; data: Record<string, unknown> } | null {
  let event = 'message'; const dataLines: string[] = [];
  for (const line of chunk.split('\n')) {
    if (line.startsWith('event:')) event = line.slice(6).trim();
    else if (line.startsWith('data:')) dataLines.push(line.slice(5).trimStart());
  }
  if (!dataLines.length) return null;
  try { return { event, data: JSON.parse(dataLines.join('\n')) }; } catch { return null; }
}

/** /api/events 事件流；返回取消函数。断线由 EventSource 自己重连 */
export function subscribeEvents(onEvent: (e: StreamEvent) => void, onState?: (open: boolean) => void): () => void {
  const es = new EventSource(`${BASE}/api/events?client=ui`);
  const types: StreamEvent['type'][] = ['quota', 'job', 'call', 'notice', 'status'];
  for (const t of types) {
    es.addEventListener(t, (ev) => {
      try { onEvent({ type: t, data: JSON.parse((ev as MessageEvent).data) } as StreamEvent); } catch { /* 坏包丢弃 */ }
    });
  }
  es.onopen = () => onState?.(true);
  es.onerror = () => onState?.(false);
  return () => es.close();
}
