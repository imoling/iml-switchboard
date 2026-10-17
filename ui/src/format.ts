import type { Call, Job, Lane, Tier } from './api/types';

export const usd = (n: number) => `$${n.toFixed(2)}`;
export const cny = (n: number) => `¥${n.toFixed(2)}`;

/** 订阅渠道没有钱的概念，显示「订阅」；人民币账号显示原币 */
export function cost(c: Pick<Call, 'lane' | 'cost_usd' | 'cost_native' | 'currency'>): { text: string; muted: boolean } {
  if (isSub(c.lane)) return { text: '订阅', muted: true };
  if (c.currency === 'CNY' && c.cost_native !== undefined) return { text: cny(c.cost_native), muted: false };
  return { text: usd(c.cost_usd), muted: false };
}
export const jobCost = (j: Job) => (j.cost_usd === undefined ? '' : j.currency === 'CNY' && j.cost_native !== undefined ? cny(j.cost_native) : usd(j.cost_usd));

export const isSub = (lane: Lane | string) => lane.endsWith('_sub');
export type LaneGroup = 'sub' | 'api' | 'media';
export const laneGroup = (lane: Lane | string): LaneGroup => (lane === 'media' ? 'media' : isSub(lane) ? 'sub' : 'api');
export const LANE_LABEL: Record<LaneGroup, string> = { sub: '订阅', api: '按量', media: '媒体' };
export const LANE_CHIP: Record<LaneGroup, string> = { sub: 'c-s', api: 'c-a', media: 'c-m' };
export const TIER_CHIP: Record<Tier, string> = { low: 'c-lo', mid: 'c-mid', high: 'c-hi', top: 'c-top' };
export const MEDIA_TIER_LABEL: Record<string, string> = { cheap: '便宜档', default: '默认档', best: '最好档', free: '订阅出图' };

export const HOST_LABEL: Record<string, string> = { claude_code: 'Claude Code', hermes: 'Hermes', workbuddy: 'WorkBuddy', quick: '快速面板', cli: '命令行', redo: '重做', facade: '兼容接口', mcp: '宿主' };
export const hostLabel = (c: Pick<Call, 'source' | 'host'>) => HOST_LABEL[c.host ?? ''] ?? HOST_LABEL[c.source] ?? c.source;

export const hhmm = (iso: string) => { const d = new Date(iso); return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`; };
export const tokens = (n?: number) => (n === undefined ? '—' : n >= 1000 ? `${(n / 1000).toFixed(1)}K` : String(n));
export const secs = (ms: number) => (ms >= 60000 ? `${Math.round(ms / 60000)} 分钟` : `${(ms / 1000).toFixed(1)} 秒`);
export const eta = (s?: number) => (s === undefined ? '' : s >= 60 ? `约 ${Math.round(s / 60)} 分钟` : `约 ${s} 秒`);

const WEEK = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];
export function dayLabel(date: string, isLast: boolean) {
  if (isLast) return '今天';
  const d = new Date(date + 'T00:00:00');
  return WEEK[d.getDay()];
}
export const pct = (n: number) => `${n % 1 === 0 ? n : n.toFixed(1)}%`;
