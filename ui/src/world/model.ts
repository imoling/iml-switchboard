/**
 * 学院页的剧本模型：一单是一段 Episode，由若干 Phase 首尾相接。
 * 演示回放和实时画面产出的是同一种 Episode，画面是 (剧本, 花名册, 时刻) 的纯函数：拖进度条、回放、实时都对得上。
 */
import type { Kind } from '../api/types';

export type Ph =
  | 'send'      // 寄信人写信，猫头鹰叼去占卜师肩上
  | 'judge'     // 占卜师看信、看水晶球，冒出判的档
  | 'go'        // 占卜师用魔杖把信送到巫师手里
  | 'work'      // 巫师读信、想
  | 'tool'      // 巫师用魔杖递纸条，请应用跑工具
  | 'appwork'   // 寄信人去书架取东西 / 跑工具
  | 'toolret'   // 猫头鹰带着结果飞到占卜师那里，占卜师转给同一位巫师
  | 'stream'    // 巫师举杖，字沿着光一个个飞回窗口
  | 'result'    // 回信沿光飞回窗口
  | 'ticket'    // 先回取药券
  | 'progress'  // 魔药师搅坩埚，寄信人回桌前干自己的活
  | 'appself'   // 寄信人干自己的活（标签）
  | 'fail'      // 巫师魔杖一垂、冒汗
  | 'handover'  // 失手的退到长椅，旁边的走过来接手
  | 'bounce'    // 信原样退回窗口
  | 'done';     // 巫师鞠躬走回屋里，寄信人蹦一下

export interface PhaseData { text?: string; text2?: string; bad?: boolean; to?: string; n?: number }
export interface Phase { type: Ph; t0: number; t1: number; d: PhaseData }

export type LaneGroup = 'sub' | 'api' | 'media';
/** 楼层就是档位；地窖是媒体 */
export type Floor = 'top' | 'high' | 'mid' | 'low' | 'paint' | 'potion';
export const FLOORS: Floor[] = ['top', 'high', 'mid', 'low'];

export interface Wizard {
  id: string;        // 型号标识；同一个名字永远是同一张脸
  name: string;      // 短名，画在脚下
  full: string;      // 完整显示名，卡片里用
  lane: LaneGroup;
  floor: Floor;
  /** 额度归哪个账号管：订阅是渠道标识，按量付费是账号标识 */
  acct?: string;
}
export interface Tower { id: string; name: string; color: string }

export interface Episode {
  id: string;
  app: string;
  model: string;       // 最先接活的巫师；换人看 handover
  kind: Kind;
  ph: Phase[];
  save?: number;       // 省下（美元），交差时冒出来
  cost?: number;       // 花了（美元）
  sub?: boolean;       // 走订阅：不另花钱
  callIds: string[];
  session?: string;
  jobId?: string;
  /** 实时任务的真实进度 0–1；有它就不按时间推 */
  prog?: number;
  stream?: boolean;
  awaitTool?: number;  // 在等应用跑完工具送回来：记开始等的时刻
  /** 用户能把这封信拖到高一层重做：最后一条记录的 id 和档位 */
  redo?: { callId: string; tier?: string };
}

export const OPEN = Number.POSITIVE_INFINITY;
export const clamp = (v: number, a = 0, b = 1) => Math.max(a, Math.min(b, v));
export const startOf = (e: Episode) => (e.ph.length ? e.ph[0].t0 : 0);
export const endOf = (e: Episode) => (e.ph.length ? e.ph[e.ph.length - 1].t1 : 0);
export const phOf = (e: Episode, type: Ph) => e.ph.filter((p) => p.type === type);
export const firstOf = (e: Episode, type: Ph) => e.ph.find((p) => p.type === type);
export const activeAt = (e: Episode, t: number) => e.ph.filter((p) => t >= p.t0 && t < p.t1);
/** 开着的阶段（实时里还没结束的「读信」）按 4 秒一圈循环，只给动画用 */
export const prOf = (p: Phase, t: number) => (p.t1 === OPEN ? ((t - p.t0) / 4) % 1 : clamp((t - p.t0) / (p.t1 - p.t0)));

/** 这一刻是谁在干这单：换人以后就是接手的那位 */
export function modelAt(e: Episode, t: number): string {
  let m = e.model;
  for (const p of e.ph) if (p.type === 'handover' && t >= p.t0 && p.d.to) m = p.d.to;
  return m;
}

/** 取药券期间应用被放开了：不算在等 */
export function asyncAt(e: Episode, t: number): boolean {
  const tk = firstOf(e, 'ticket');
  if (!tk || t < tk.t1) return false;
  const res = e.ph.find((p) => p.type === 'result' && p.t0 >= tk.t1);
  return !res || t < res.t0;
}

export interface AppState { wait: number | null; work: string | null; ticket: boolean; busy: boolean }

/** 每个应用此刻在等、在干活，还是拿着取药券 */
export function appStates(eps: Episode[], towers: Tower[], t: number): Record<string, AppState> {
  const out: Record<string, AppState> = {};
  for (const tw of towers) out[tw.id] = { wait: null, work: null, ticket: false, busy: false };
  for (const e of eps) {
    const s = out[e.app];
    if (!s || t < startOf(e) || t >= endOf(e)) continue;
    s.busy = true;
    const done = firstOf(e, 'done');
    const act = activeAt(e, t);
    if (asyncAt(e, t)) { s.ticket = true; continue; }
    const own = act.find((p) => p.type === 'appwork' || p.type === 'appself');
    if (own) { s.work = own.d.text ?? null; if (own.type === 'appwork') continue; }
    if ((!done || t < done.t0) && !act.some((p) => p.type === 'bounce')) {
      const w = t - startOf(e);
      s.wait = s.wait === null ? w : Math.max(s.wait, w);
    }
  }
  return out;
}

/** 某位巫师此刻有没有在冷却（刚失手） */
export function failedRecently(eps: Episode[], id: string, t: number, span: number): boolean {
  return eps.some((e) => e.model === id && e.ph.some((p) => p.type === 'fail' && t >= p.t0 && t < p.t0 + span));
}

export function hash(s: string): number {
  let h = 7;
  for (const c of s) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return h;
}

/** 画面下方的旁白：[时刻, 一句话] */
export type Caption = [number, string];
