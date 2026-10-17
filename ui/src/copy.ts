/** 跨窗口复用的额度文案（不走主题）。只在这里写一次 */
import type { Quota } from './api/types';

/** 额度条变黄时底部那一行：会发生什么 */
export function quotaWarning(quotas: Quota[], yieldPct: number): string | null {
  const hot = quotas.filter((q) => !q.sub && q.pct >= yieldPct).sort((a, b) => b.pct - a.pct)[0];
  if (!hot) return null;
  const name = hot.label.split(' ')[0];
  return `${name} 额度已过 ${Math.floor(hot.pct / 10) * 10}%，低档和中档任务改走按量付费`;
}

export const QUOTA_ESTIMATE_NOTE = '额度为本应用记录的估算，以官方显示为准';
