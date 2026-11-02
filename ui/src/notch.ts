/** 刘海动画的状态：哪些请求在路上，两耳显示哪一个。事件来了就算一步，界面只管画 */
import type { Call, CallStart, Lane } from './api/types';

export type NotchState = { active: CallStart[]; shown: CallStart | null; ok: boolean };

export const NOTCH_IDLE: NotchState = { active: [], shown: null, ok: true };

/** 光的颜色：和概览页图例同色系，提亮到黑底上打得出来；失败一律红 */
export function notchTone(ok: boolean, lane: Lane | ''): string {
  if (!ok) return '#FF4D42';
  if (lane === 'media') return '#7BE35C';
  return lane === 'api' ? '#4DA8FF' : '#FF7A45';
}

/** 开始干活；同一个请求换候选时替换原来那条，渠道跟着变 */
export function notchStart(s: NotchState, f: CallStart): NotchState {
  return { active: [...s.active.filter((x) => x.id !== f.id), f], shown: f, ok: true };
}

/** 做完一个。还有别的在路上就接着显示最新的那个；全做完了就停在刚完成的这个，等收回 */
export function notchDone(s: NotchState, c: Pick<Call, 'id' | 'host' | 'lane' | 'provider' | 'ok'>): NotchState {
  const active = s.active.filter((x) => x.id !== c.id);
  return { active, shown: active[active.length - 1] ?? { id: c.id, host: c.host, lane: c.lane, provider: c.provider }, ok: active.length > 0 || c.ok };
}

/** 后台没发完成事件（比如中途重启）时，别让两耳一直开着 */
export function notchDrop(s: NotchState, id: string): NotchState {
  const active = s.active.filter((x) => x.id !== id);
  return { ...s, active, shown: active[active.length - 1] ?? s.shown };
}
