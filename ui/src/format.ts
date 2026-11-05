import type { Call, Job, Lane, Tier } from './api/types';

/** 费用显示的币种。账本一律按美元记，这里只管显示；由各窗口从 /api/status 读到后调 setMoney */
let display: 'USD' | 'CNY' = 'USD';
let usdPerCny = 1 / 7.1;
export function setMoney(currency?: string, rate?: number) {
  display = currency === 'CNY' ? 'CNY' : 'USD';
  if (rate && rate > 0) usdPerCny = rate;
}
export const moneySymbol = () => (display === 'CNY' ? '¥' : '$');
/** 美元金额换成显示币种的数值 */
export const toDisplay = (usdAmount: number) => (display === 'CNY' ? usdAmount / usdPerCny : usdAmount);
export const fromDisplay = (v: number) => (display === 'CNY' ? v * usdPerCny : v);

/** 给数字串的整数部分加千分位：1234567.89 → 1,234,567.89 */
const group = (s: string) => { const [i, f] = s.split('.'); const g = i.replace(/\B(?=(\d{3})+(?!\d))/g, ','); return f === undefined ? g : `${g}.${f}`; };

function digits(n: number) {
  if (n === 0) return '0';
  if (Math.abs(n) < 0.01) return n.toFixed(4); // 不到一分的小额多留两位，免得全是 0.00
  if (Math.abs(n) >= 1e5) return `${(n / 1e4).toFixed(2)} 万`; // 太长看不清：十万起用万
  return group(Number.isInteger(n) ? String(n) : n.toFixed(2));
}

/** 次数、条数：千分位，十万起用万 */
export const num = (n: number) => (Math.abs(n) >= 1e5 ? `${(n / 1e4).toFixed(1)} 万` : group(String(Math.round(n))));
/** 美元金额按显示币种格式化 */
export const usd = (usdAmount: number) => moneySymbol() + digits(toDisplay(usdAmount));
const cny = (n: number) => `¥${digits(n)}`;
/** 单价按显示币种格式化：人民币单价在显示人民币时不来回换算 */
export const unitPrice = (v: number, currency?: string) => (currency === 'CNY' ? (display === 'CNY' ? cny(v) : usd(v * usdPerCny)) : usd(v));

type Priced = { cost_usd?: number; cost_native?: number; currency?: string };
/** 显示人民币时，人民币账号直接用账单原价，不来回换算 */
const priced = (c: Priced) => (display === 'CNY' && c.currency === 'CNY' && c.cost_native !== undefined ? cny(c.cost_native) : usd(c.cost_usd ?? 0));

/** 订阅渠道没有钱的概念，显示「订阅」 */
export function cost(c: Pick<Call, 'lane' | 'cost_usd' | 'cost_native' | 'currency'>): { text: string; muted: boolean } {
  if (isSub(c.lane)) return { text: '订阅', muted: true };
  return { text: priced(c), muted: false };
}
export const jobCost = (j: Job) => (j.cost_usd === undefined || (j.cost_usd === 0 && j.lane && isSub(j.lane)) ? '' : priced(j));
/** 账号的本月花费 */
export const accountCost = (a: { month_cost_usd?: number; month_cost_native?: number; currency?: string }) =>
  a.month_cost_usd === undefined && a.month_cost_native === undefined ? '' : priced({ cost_usd: a.month_cost_usd, cost_native: a.month_cost_native, currency: a.currency });

export const isSub = (lane: Lane | string) => lane.endsWith('_sub');
export type LaneGroup = 'sub' | 'api' | 'media';
export const laneGroup = (lane: Lane | string): LaneGroup => (lane === 'media' ? 'media' : isSub(lane) ? 'sub' : 'api');
export const LANE_LABEL: Record<LaneGroup, string> = { sub: '订阅', api: '按量', media: '媒体' };
export const LANE_CHIP: Record<LaneGroup, string> = { sub: 'c-s', api: 'c-a', media: 'c-m' };
export const TIER_CHIP: Record<Tier, string> = { low: 'c-lo', mid: 'c-mid', high: 'c-hi', top: 'c-top' };
export const MEDIA_TIER_LABEL: Record<string, string> = { cheap: '便宜档', default: '默认档', best: '最好档', free: '订阅出图' };

export const HOST_LABEL: Record<string, string> = { claude_code: 'Claude Code', claude_desktop: 'Claude Desktop', codex: 'Codex', gemini_cli: 'Gemini CLI', pi: 'pi', openclaw: 'OpenClaw', deepseek_harness: 'DeepSeek Harness', doubao_work: '豆包工作', hermes: 'Hermes', workbuddy: 'WorkBuddy', winwork: 'WinWork', quick: '快速面板', cli: '命令行', redo: '重做', facade: '兼容接口', mcp: '其他应用' };
export const hostLabel = (c: Pick<Call, 'source' | 'host'>) => HOST_LABEL[c.host ?? ''] ?? HOST_LABEL[c.source] ?? c.source;

export const hhmm = (iso: string) => { const d = new Date(iso); return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`; };
export const tokens = (n?: number) => (n === undefined ? '—' : n >= 1000 ? `${(n / 1000).toFixed(1)}K` : String(n));
export const uptime = (s: number) => (s < 3600 ? `${Math.max(1, Math.round(s / 60))} 分钟` : s < 172800 ? `${Math.round(s / 3600)} 小时` : `${Math.round(s / 86400)} 天`);
export const secs = (ms: number) => (ms >= 60000 ? `${Math.round(ms / 60000)} 分钟` : `${(ms / 1000).toFixed(1)} 秒`);
export const eta = (s?: number) => (s === undefined ? '' : s >= 60 ? `约 ${Math.round(s / 60)} 分钟` : `约 ${s} 秒`);

const WEEK = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];
/** 横轴标签：7 天以内标周几；更长只在每周一和首尾标日期，其余留空 */
export function dayLabel(date: string, isLast: boolean, total = 7) {
  if (isLast) return '今天';
  const d = new Date(date + 'T00:00:00');
  if (total <= 10) return WEEK[d.getDay()];
  const md = `${d.getMonth() + 1}/${d.getDate()}`;
  return d.getDay() === 1 ? md : '';
}
export const pct = (n: number) => `${n % 1 === 0 ? n : n.toFixed(1)}%`;
