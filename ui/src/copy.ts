/** 跨窗口复用的额度文案（不走主题）。只在这里写一次 */
import type { Quota } from './api/types';

/** 额度条变黄时底部那一行：会发生什么 */
export function quotaWarning(quotas: Quota[], yieldPct: number): string | null {
  const hot = quotas.filter((q) => !q.sub && q.pct >= yieldPct).sort((a, b) => b.pct - a.pct)[0];
  if (!hot) return null;
  const name = hot.label.split(' ')[0];
  return `${name} 额度已过 ${Math.floor(hot.pct / 10) * 10}%，低档和中档任务改走按量付费`;
}

export const QUOTA_ESTIMATE_NOTE = '带「估算」的行是按本应用记录估的；其余由官方命令回报，以官方显示为准';

/** 跟系统走的说法：macOS 叫菜单栏、Finder，Windows 叫托盘、资源管理器 */
const platform = typeof navigator !== 'undefined' ? navigator.userAgent : '';
export const IS_WINDOWS = /Windows/.test(platform);
export const TRAY_NAME = IS_WINDOWS ? '托盘' : /Mac/.test(platform) ? '菜单栏' : '托盘';
export const REVEAL_LABEL = IS_WINDOWS ? '在资源管理器中显示' : /Mac/.test(platform) ? '在 Finder 中显示' : '打开所在文件夹';
