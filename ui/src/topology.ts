/** 概览页全貌图的纯逻辑：谁算节点、一次调用落到哪个节点、连线怎么画 */
import type { Account, Call, Integration } from './api/types';

/** 左边列已接入的宿主；没接入但近 7 天派过活的也算，否则那条记录没地方出发 */
export function hostNodes(list: Integration[]): Integration[] {
  return list
    .filter((h) => h.status === 'connected' || (h.calls_7d ?? 0) > 0)
    .sort((a, b) => (b.calls_7d ?? 0) - (a.calls_7d ?? 0));
}

/** 右边列在用的渠道：订阅在上，按量付费在下 */
export function channelNodes(list: Account[]): Account[] {
  const live = list.filter((a) => a.status !== 'disabled');
  return [...live.filter((a) => a.kind === 'subscription'), ...live.filter((a) => a.kind === 'api')];
}

/** 托管的型号数：同一个型号挂在两个账号上只算一次 */
export function modelCount(list: Account[]): number {
  return new Set(channelNodes(list).flatMap((a) => a.models ?? [])).size;
}

/** 媒体渠道里 provider 写的是命令行的名字，对应到订阅账号 */
const PROVIDER_ACCOUNT: Record<string, string> = { codex: 'codex_sub', claude: 'claude_sub', agy: 'agy_sub' };

/** 这次调用是哪个渠道干的；路由失败没选到渠道时返回 null，光点就停在中间 */
export function channelOf(c: Call, list: Account[]): Account | null {
  if (!c.lane) return null;
  if (c.lane !== 'api' && c.lane !== 'media') return list.find((a) => a.id === c.lane) ?? null;
  return list.find((a) => a.provider === c.provider && a.kind === 'api')
    ?? list.find((a) => a.id === PROVIDER_ACCOUNT[c.provider])
    ?? list.find((a) => a.provider === c.provider)
    ?? null;
}

/** 光点颜色和概览页图例一致：订阅陶土、按量蓝、媒体绿；失败一律红 */
export function callColor(c: Pick<Call, 'ok' | 'lane'>): string {
  if (!c.ok) return 'var(--red)';
  if (c.lane === 'media') return 'var(--green)';
  return c.lane === 'api' ? 'var(--blue)' : 'var(--accent)';
}

/** n 个节点在高度 h 里均匀排开时，第 i 个的中心 */
export const rowY = (i: number, n: number, h: number) => (h / n) * (i + 0.5);

export type Link = { x1: number; y1: number; x2: number; y2: number };

/** 两点之间水平出入的曲线 */
export function linkPath({ x1, y1, x2, y2 }: Link): string {
  const mx = (x1 + x2) / 2;
  return `M${x1} ${y1} C${mx} ${y1} ${mx} ${y2} ${x2} ${y2}`;
}

/** 同一条曲线上 t（0 到 1）处的点。光点位置用公式算，不问浏览器：带 pathLength 的路径上，各内核量出来的长度不一定一致 */
export function linkPoint({ x1, y1, x2, y2 }: Link, t: number): { x: number; y: number } {
  const mx = (x1 + x2) / 2;
  const u = 1 - t;
  const a = u * u * u, b = 3 * u * u * t, c = 3 * u * t * t, d = t * t * t;
  return { x: a * x1 + b * mx + c * mx + d * x2, y: a * y1 + b * y1 + c * y2 + d * y2 };
}

/** 连线粗细按派活量：平方根压一下，别让最忙的一条把别的压成细线 */
export function linkWidth(n: number, max: number): number {
  return max <= 0 || n <= 0 ? 1 : 1 + 2 * Math.sqrt(n / max);
}
