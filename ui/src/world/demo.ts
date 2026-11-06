/**
 * 演示回放：设计稿 v5 那段剧本，再补上额度用完、评估两段。七种来回加三种状态都能在这里看到，
 * 没接应用、没有请求的时候也能知道这个页面在演什么；截图和录宣传视频也用它。
 * 剧本按主题的叫法生成一份（气泡和旁白里的字不一样，时间轴一样）。
 */
import { type Caption, type Episode, type Phase, type PhaseData, type Ph, type Tower, type Wizard } from './model';
import type { SceneWords } from './theme';
import { ACADEMY_WORDS } from './themes/academy';

export const DEMO_LEN = 70;

export const DEMO_TOWERS: Tower[] = [
  { id: 'claude_code', name: 'Claude Code', color: '#C98A3B' },
  { id: 'codex', name: 'Codex', color: '#3A3935' },
  { id: 'workbuddy', name: 'WorkBuddy', color: '#7D8F5E' },
];

const wz = (id: string, name: string, lane: Wizard['lane'], floor: Wizard['floor'], acct?: string): Wizard => ({ id, name, full: name, lane, floor, acct });
function wizardsFor(w: SceneWords): Wizard[] {
  return [
    wz('fable', 'Fable', 'sub', 'top', 'claude_sub'),
    wz('opus', 'Opus', 'sub', 'high', 'claude_sub'), wz('gpt', 'GPT', 'sub', 'high', 'codex_sub'),
    wz('sonnet', 'Sonnet', 'sub', 'mid', 'claude_sub'), wz('kimi', 'Kimi', 'api', 'mid', 'kimi'), wz('glm', 'GLM', 'api', 'mid', 'glm'),
    wz('haiku', 'Haiku', 'sub', 'low', 'claude_sub'), wz('ds', 'DeepSeek', 'api', 'low', 'deepseek'),
    wz('painter', w.painter, 'media', 'paint'), wz('filmer', w.filmer, 'media', 'potion'),
  ];
}
export const DEMO_WIZARDS: Wizard[] = wizardsFor(ACADEMY_WORDS);

type Row = [Ph, number, number, PhaseData?];
const mk = (rows: Row[]): Phase[] => rows.map(([type, t0, t1, d]) => ({ type, t0, t1, d: d ?? {} }));
const ep = (id: string, app: string, model: string, rows: Row[], money: { save?: number; cost?: number; sub?: boolean }, kind: Episode['kind'] = 'text'): Episode =>
  ({ id, app, model, kind, ph: mk(rows), callIds: [id], redo: { callId: id }, ...money });

function episodesFor(w: SceneWords): Episode[] {
  const J = w.judge, S = w.say;
  return [
    ep('d1', 'claude_code', 'haiku', [['send', .5, 2], ['judge', 2, 2.8, { text: J.low }], ['go', 2.8, 4], ['work', 4, 5.5], ['result', 5.5, 7], ['done', 7, 8.2]], { save: 0.03, sub: true }),
    ep('d2', 'codex', 'sonnet', [['send', 3, 4.5], ['judge', 4.5, 5.5, { text: J.mid }], ['go', 5.5, 6.7], ['work', 6.7, 8],
      ['tool', 8, 9.2, { text: S.toolAsk(1) }], ['appwork', 9.2, 10.4, { text: S.toolFetch }], ['toolret', 10.4, 11.6], ['work', 11.6, 12.6],
      ['tool', 12.6, 13.8, { text: S.toolAsk(2) }], ['appwork', 13.8, 15, { text: S.toolFetch }], ['toolret', 15, 16.2], ['stream', 16.2, 19.5], ['done', 19.5, 20.7]], { save: 0.17, sub: true }),
    ep('d3', 'workbuddy', 'kimi', [['send', 12, 13.5], ['judge', 13.5, 14.3, { text: J.mid }], ['go', 14.3, 15.5], ['fail', 15.5, 16.3, { text: '限流' }],
      ['handover', 16.3, 17.5, { to: 'glm' }], ['work', 17.5, 19], ['stream', 19, 21], ['done', 21, 22.2]], { save: 0.05, cost: 0.004 }),
    ep('d4', 'claude_code', 'filmer', [['send', 22, 23.5], ['judge', 23.5, 24.2, { text: J.video }], ['go', 24.2, 25.4], ['ticket', 25.4, 26.6],
      ['progress', 26.6, 36], ['appself', 27.5, 35.5, { text: S.ownWork }], ['result', 36, 37.5], ['done', 37.5, 38.7]], { cost: 0.34 }, 'video'),
    ep('d5', 'workbuddy', 'opus', [['send', 27, 28.5], ['judge', 28.5, 30.5, { text: w.judgeRule('mid'), text2: w.judgeCrystal('', 'high') }], ['go', 30.5, 31.8], ['work', 31.8, 34], ['stream', 34, 36.5], ['done', 36.5, 37.7]], { save: 0.44, sub: true }),
    ep('d6', 'codex', 'sonnet', [['send', 40, 41.5], ['judge', 41.5, 42.5, { text: w.judgeTooThick(21), bad: true }], ['bounce', 42.5, 44, { text: S.bounce }], ['appwork', 44, 45.5, { text: S.shorten }],
      ['send', 45.5, 47], ['judge', 47, 47.8, { text: w.judgeSticky('Sonnet') }], ['go', 47.8, 48.8], ['work', 48.8, 49.8], ['stream', 49.8, 51.6], ['done', 51.6, 52.8]], { save: 0.13, sub: true }),
    ep('d7', 'claude_code', 'painter', [['send', 53, 54.4], ['judge', 54.4, 55.2, { text: J.image }], ['go', 55.2, 56.4], ['work', 56.4, 59], ['result', 59, 60.4], ['done', 60.4, 61.6]], { cost: 0.04 }, 'image'),
  ];
}
export const DEMO_EPISODES: Episode[] = episodesFor(ACADEMY_WORDS);

/** 章节按钮：点一下跳到那一段 */
export const DEMO_CHAPTERS: [number, string][] = [
  [0, '一问一答'], [3, '工具来回'], [12, '换人'], [22, '取件单'], [27, '改判'], [40, '退回'], [53, '额度用完'], [62, '评估'],
];

/** 额度用完：Claude 订阅的巫师趴下睡，重置时伸懒腰醒来 */
export const DEMO_SLEEP: [number, number] = [53, 60.5];
/** 评估：抽样重做，一个出题一个判卷 */
export const DEMO_EXAM: [number, number] = [62, 69];

function captionsFor(w: SceneWords): Caption[] {
  const C = w.caption;
  return ([
    [2, C.arrive('Claude Code', '判低档', 'Haiku', '订阅')], [4.5, C.arrive('Codex', '判中档', 'Sonnet', '订阅')], [5.5, C.sticky('Codex', 'Sonnet')],
    [8, C.toolAsk('Sonnet', 'Codex')], [10.4, C.toolBack('Codex', 'Sonnet')], [12.6, C.toolAsk('Sonnet', 'Codex')],
    [13.5, C.arrive('WorkBuddy', '判中档', 'Kimi', '按量付费')], [15.5, C.handover('Kimi', 'GLM', '限流')], [16.2, `Sonnet 边写边回`], [19, `GLM 边写边回，WorkBuddy 只多等了 2 秒`],
    [23.5, C.arrive('Claude Code', w.judge.video, w.filmer, '媒体')], [25.4, C.ticket('Claude Code')], [28.5, C.arrive('WorkBuddy', '判中档', 'Opus', '订阅')], [29.5, C.rejudge('', w.title.mid, w.title.high)],
    [36, C.jobDone('assets/intro.mp4')], [41.5, `Codex 的对话 21 万 token，候选里没人装得下`],
    [42.5, C.bounced('Codex')], [47, `压缩到 8 万，判中档，还是 Sonnet`],
    [53, `Claude 订阅这一窗额度用完，几位趴下睡了`], [54.4, `画图走媒体，不受影响`], [60.5, `额度重置，醒来接着干`],
    [62, `每月抽样重做：低档的答卷请高档判一判`], [69, `判完了：这类活低档够用`],
  ] as Caption[]).sort((a, b) => a[0] - b[0]);
}
export const DEMO_CAPTIONS: Caption[] = captionsFor(ACADEMY_WORDS);

export interface Demo { episodes: Episode[]; captions: Caption[]; wizards: Wizard[]; towers: Tower[] }
const cache = new Map<SceneWords, Demo>();
/** 按主题的叫法生成一份演示剧本（时间轴一样，只有字不同） */
export function buildDemo(w: SceneWords): Demo {
  let d = cache.get(w);
  if (!d) { d = { episodes: episodesFor(w), captions: captionsFor(w), wizards: wizardsFor(w), towers: DEMO_TOWERS }; cache.set(w, d); }
  return d;
}

/** 订阅剩多少：演示里随剧本往下掉，额度用完那段归零 */
export function demoQuota(t: number): number {
  if (t >= DEMO_SLEEP[0] && t < DEMO_SLEEP[1]) return 0;
  if (t >= DEMO_SLEEP[1]) return 100;
  let used = 58;
  for (const [tt, v] of [[5.5, 2], [19.5, 6], [36.5, 9], [51.6, 5]] as const) if (t >= tt) used += v;
  return 100 - used;
}

/** 金库里的钱：交差一单进一笔 */
export function demoSaved(t: number, episodes: Episode[] = DEMO_EPISODES): number {
  let s = 0;
  for (const e of episodes) { const d = e.ph.find((p) => p.type === 'done'); if (d && t >= d.t0 && e.save) s += e.save; }
  return s;
}
