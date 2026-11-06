/**
 * 学院页的渲染器：五套主题共用。逻辑分辨率 480×270，画的时候整体放大 S 倍，像素边缘不抗锯齿。
 *
 * 分工：谁在为谁干活、谁换了谁、谁在等，交给小人演；信使（猫头鹰、小兵、海鸥）把信送到中间，判档的把活交给模型，
 * 模型干完用自己的方式回应（魔杖的光、信鸽、纸飞机、花笺、泡泡）。每个小人此刻在哪、在干嘛，完全由剧本推出来（pure）：
 * 演示拖到哪一秒、实时晚几秒，画面都对得上。背景是一张整图，主题自己的动画（瀑布、旗、海浪）叠在上面。
 */
import { activeAt, appStates, asyncAt, clamp, type Episode, endOf, firstOf, type Floor, FLOORS, hash, modelAt, OPEN, type Phase, phOf, prOf, startOf, type Tower, type Wizard } from './model';
import { type Dir, type FishPose, fishFrame, frameOf, GH, GULL_FLY, GULL_PAL, GULL_PERCH, GW, LANE_ROBE, type Look, MICRO, microOk, microWidth, OWL_FLY, OWL_PAL, OWL_PERCH, PIGEON, PIGEON_PAL, type Pose, spriteOf } from './sprites';
import type { ThemeDef, ThemeKey } from './theme';
import { moneySaved, moneySpent, SUB_FREE } from './words';

export const WW = 480, WH = 270;
export const FONT = 'Fusion Pixel';
const C = {
  ink: '#1C1B19', muted: '#6F6A61', accent: '#C4613A', blue: '#4A6785', green: '#5F7A4A', gold: '#D1A24A', red: '#B3261E',
  wood: '#B9874F', woodD: '#8C6239', white: '#FFFFFF', pink: '#F28AA0',
};

/* ---------- 布局 ---------- */
export interface WizSpot { idle: [number, number]; slot: [number, number]; bench: [number, number]; floor: Floor; index: number; slotDir: Dir }
export interface AppSpot extends Tower { x: number; y: number; home: [number, number]; win: [number, number]; desk: [number, number]; shelf: [number, number] }
export interface Layout {
  key: string;
  theme: ThemeKey;
  towers: AppSpot[];
  wiz: Map<string, WizSpot>;
  overflow: Partial<Record<Floor, number>>;
  floorY: Record<Floor, number>;
}
/** 最多五座站台：前四座给接入的应用，第五座留给「其他应用」 */
export const TOWER_MAX = 5;

export function layoutFor(wizards: Wizard[], towers: Tower[], theme: ThemeDef): Layout {
  const L = theme.layout;
  const wiz = new Map<string, WizSpot>();
  const overflow: Layout['overflow'] = {};
  const by: Partial<Record<Floor, Wizard[]>> = {};
  for (const w of wizards) (by[w.floor] ??= []).push(w);
  for (const f of [...FLOORS, 'paint', 'potion'] as Floor[]) {
    const list = by[f] ?? [];
    const cap = f === 'paint' ? 2 : f === 'potion' ? 2 : L.idleXs.length;
    if (list.length > cap) overflow[f] = list.length - cap;
    list.slice(0, cap).forEach((w, i) => {
      const y = L.floors[f];
      if (f === 'paint') { const p: [number, number] = [L.media.paintIdle[0] + i * 22, L.media.paintIdle[1]]; wiz.set(w.id, { idle: p, slot: p, bench: p, floor: f, index: i, slotDir: 'right' }); return; }
      if (f === 'potion') { const p: [number, number] = [L.media.potionIdle[0] + i * 22, L.media.potionIdle[1]]; wiz.set(w.id, { idle: p, slot: L.media.potionSlot, bench: p, floor: f, index: i, slotDir: 'right' }); return; }
      const bench: [number, number] = L.bench ?? [L.benchX, y];
      // 同层几位一起干活时并排站：栏杆边错开 10 格，床上错开 6 格
      const step = L.slotDir === 'down' ? 6 : 10;
      const slot: [number, number] = L.workAtDesk ? [L.idleXs[i], y] : [L.slotX + (i % 3) * step, y];
      wiz.set(w.id, { idle: [L.idleXs[i], y], slot, bench, floor: f, index: i, slotDir: L.workAtDesk ? 'down' : L.slotDir });
    });
  }
  const n = Math.max(1, Math.min(TOWER_MAX, towers.length));
  const spots = towers.slice(0, TOWER_MAX).map((tw, i): AppSpot => {
    const x = L.appX(n, i), y = L.ground;
    const k = L.appKind;
    const home: [number, number] = k === 'post' ? [x - 8, y - 36] : k === 'tent' ? [x - 10, y - 30] : k === 'throne' ? [x + 11, y] : k === 'chair' ? [x + 10, y - 28] : [x, y];
    const win: [number, number] = k === 'post' ? [x - 8, y - 20] : k === 'tent' ? [x - 10, y - 18] : k === 'throne' ? [x - 11, y - 18] : k === 'chair' ? [x + 3, y - 14] : [x - 4, y - 10];
    const desk: [number, number] = k === 'desk' ? [x + 6, y] : k === 'throne' ? [x, y] : [x + 5, y];
    return { ...tw, x, y, home, win, desk, shelf: L.shelf };
  });
  return { key: `${theme.id}|${wizards.map((w) => w.id + w.floor).join(',')}|${towers.map((t) => t.id).join(',')}`, theme: theme.id, towers: spots, wiz, overflow, floorY: L.floors };
}

/* ---------- 画笔 ---------- */
let ctx: CanvasRenderingContext2D;
let S = 3;
let fontOk = false;
const R = (x: number, y: number, w: number, h: number, c: string) => { ctx.fillStyle = c; ctx.fillRect(Math.round(x) * S, Math.round(y) * S, Math.max(1, Math.round(w)) * S, Math.max(1, Math.round(h)) * S); };
const lerp = (a: [number, number], b: [number, number], p: number): [number, number] => [a[0] + (b[0] - a[0]) * p, a[1] + (b[1] - a[1]) * p];
const wq = (a: [number, number], b: [number, number], p: number) => lerp(a, b, clamp(p));
function px(map: string[], pal: Record<string, string>, x: number, y: number, flip = false) {
  for (let r = 0; r < map.length; r++) for (let c = 0; c < map[r].length; c++) {
    const ch = map[r][c]; if (ch === '.') continue;
    R(x + (flip ? map[r].length - 1 - c : c), y + r, 1, 1, pal[ch] ?? '#f0f');
  }
}
/** 像素风的框：1 格描边，四角缺一格 */
function panel(x: number, y: number, w: number, h: number, bg: string, border?: string) {
  R(x + 1, y, w - 2, h, bg); R(x, y + 1, w, h - 2, bg);
  if (border) { R(x + 1, y, w - 2, 1, border); R(x + 1, y + h - 1, w - 2, 1, border); R(x, y + 1, 1, h - 2, border); R(x + w - 1, y + 1, 1, h - 2, border); }
}
interface TextOpt { c?: string; bg?: string; border?: string; align?: CanvasTextAlign; alpha?: number; size?: 'small' | 'normal'; tail?: 'down' | 'up' }
/** 像素字：10 格高的像素字体。小字按半格画（缩放倍数是偶数，所以每个字的点还是方的），气泡用整格 */
function text(x: number, y: number, s: string, o: TextOpt = {}) {
  const small = o.size !== 'normal';
  const size = (small ? 5 : 10) * S;
  ctx.font = fontOk ? `${size}px "${FONT}", monospace` : `${size * 0.9}px -apple-system, "PingFang SC", sans-serif`;
  ctx.textAlign = 'left'; ctx.textBaseline = 'alphabetic';
  const tw = Math.ceil(ctx.measureText(s).width / S);
  const w = tw + 4, h = small ? 8 : 12;
  // 框不出画面：贴边的气泡往里挪
  const x0 = Math.max(1, Math.min(WW - w - 1, Math.round(o.align === 'left' ? x : o.align === 'right' ? x - w : x - w / 2)));
  const y0 = Math.max(1, Math.round(y - h / 2));
  ctx.globalAlpha = o.alpha ?? 1;
  if (o.bg) {
    panel(x0, y0, w, h, o.bg, o.border);
    if (o.tail === 'down') { R(x0 + Math.floor(w / 2) - 1, y0 + h, 3, 1, o.border ?? o.bg); R(x0 + Math.floor(w / 2), y0 + h + 1, 1, 1, o.border ?? o.bg); }
  }
  ctx.fillStyle = o.c ?? C.ink;
  ctx.fillText(s, (x0 + 2) * S, (y0 + h - (small ? 1.5 : 2)) * S);
  ctx.globalAlpha = 1;
  return { x0, y0, w, h };
}
const bubble = (x: number, y: number, s: string, o: TextOpt = {}) => text(x, y, s, { bg: o.bg ?? '#FFFFFF', border: o.border ?? C.ink, c: o.c ?? C.ink, tail: o.tail ?? 'down', alpha: o.alpha, size: o.size ?? 'normal' });
const label = (x: number, y: number, s: string, c = C.ink, bg = 'rgba(255,255,255,.88)') => text(x, y, s, { c, bg });
/** 名牌：英文数字用 3×5 小字，别的用像素字体 */
function nameTag(x: number, y: number, s: string, c: string, bg: string) {
  if (!microOk(s)) { text(x, y, s, { c, bg }); return; }
  const up = s.toUpperCase();
  const w = microWidth(up) + 4, h = 9;
  const x0 = Math.round(x - w / 2), y0 = Math.round(y - h / 2);
  panel(x0, y0, w, h, bg);
  let cx = x0 + 2;
  for (const ch of up) { const g = MICRO[ch]; for (let r = 0; r < 5; r++) for (let k = 0; k < g[r].length; k++) if (g[r][k] === 'X') R(cx + k, y0 + 2 + r, 1, 1, c); cx += g[0].length + 1; }
}

/* ---------- 背景：整图，按缩放和昼夜缓存 ---------- */
const bgCache = new Map<string, HTMLCanvasElement | OffscreenCanvas>();
function makeCanvas(w: number, h: number): HTMLCanvasElement | OffscreenCanvas {
  if (typeof OffscreenCanvas !== 'undefined') return new OffscreenCanvas(w, h);
  const c = document.createElement('canvas'); c.width = w; c.height = h; return c;
}

/* ---------- 剧本推出来的走位 ---------- */
export type Item = 'book' | 'letter' | 'wand' | 'stick' | 'brush' | 'flag' | 'tablet' | 'bottle' | 'phone' | 'none';
export interface Plan { x: number; y: number; pose: Pose; dir: Dir; chat?: number; sweat?: boolean; z?: boolean; tag?: string; item?: Item; hidden?: boolean; puff?: number; hearts?: boolean }

export interface Bubble { who: string; text: string; t0: number; t1: number; kind: 'say' | 'think' }

export interface Scene {
  t: number;
  theme: ThemeDef;
  bg?: CanvasImageSource | null;
  font?: boolean;
  wizards: Wizard[];
  towers: Tower[];
  episodes: Episode[];
  layout: Layout;
  night: boolean; rain: boolean; snow: boolean; paused: boolean;
  bottles: { name: string; left: number }[];
  saved: number;
  money: (usd: number) => string;
  sleeping: Set<string>;
  wakeAt: Map<string, number>;
  cooling: Set<string>;
  exam: { since: number } | null;
  pokes: Map<string, number>;
  hover?: string;
  drag?: { id: string; x: number; y: number; target?: Floor };
  /** 演示里失手后歇多久；实时由冷却状态决定 */
  coolSpan: number;
  bubbles: Bubble[];
  showNames: boolean;
}

const STEPS = 7; // 每秒走几步
const isText = (f: Floor) => f !== 'paint' && f !== 'potion';

function examRoles(sc: Scene): { seat?: string; grader?: string } {
  if (!sc.exam) return {};
  const free = (w: Wizard) => !sc.sleeping.has(w.id) && !sc.episodes.some((e) => (e.model === w.id || e.ph.some((p) => p.d.to === w.id)) && sc.t >= startOf(e) && sc.t < endOf(e) + 1);
  const seat = sc.wizards.find((w) => w.floor === 'low' && free(w)) ?? sc.wizards.find((w) => w.floor === 'mid' && free(w));
  const grader = sc.wizards.find((w) => w.floor === 'high' && free(w)) ?? sc.wizards.find((w) => w.floor === 'top' && free(w));
  return { seat: seat?.id, grader: grader?.id };
}

function idlePlan(w: Wizard, spot: WizSpot, sc: Scene): Plan {
  const t = sc.t;
  const [x, y] = spot.idle;
  const blink = Math.floor(t * 1.3 + hash(w.id)) % 9 === 0;
  if (w.floor === 'top') return { x, y, pose: 'sleep', dir: 'down', z: true };
  if (w.floor === 'paint') return { x, y, pose: 'paint', dir: 'right', item: 'brush' };
  if (w.floor === 'potion') return { x, y, pose: blink ? 'blink' : 'stand', dir: 'down' };
  if (sc.theme.layout.workAtDesk) {
    // 写字楼：坐在工位上，隔一会儿看看旁边的人
    const pairD = spot.index ^ 1;
    const mateD = sc.wizards.find((o) => o.floor === w.floor && sc.layout.wiz.get(o.id)?.index === pairD);
    const chatting = mateD && !busyAt(sc, mateD.id) && Math.floor((t + spot.index * 2.5 + hash(w.floor)) / 6) % 2 === 0;
    return { x, y, pose: blink ? 'sleep' : 'sit', dir: chatting ? (spot.index % 2 === 0 ? 'right' : 'left') : 'down', chat: chatting ? spot.index % 2 : undefined };
  }
  if (w.floor === 'high') return { x: x + Math.sin(t * 0.3 + hash(w.id)) * 2, y, pose: 'read', dir: 'down', item: 'book' };
  // 同层两两凑一起聊天
  const pair = spot.index ^ 1;
  const mate = sc.wizards.find((o) => o.floor === w.floor && sc.layout.wiz.get(o.id)?.index === pair);
  if (mate && !busyAt(sc, mate.id) && Math.floor((t + spot.index * 2.5 + hash(w.floor)) / 6) % 2 === 0) {
    return { x, y, pose: blink ? 'blink' : 'stand', dir: spot.index % 2 === 0 ? 'right' : 'left', chat: spot.index % 2 };
  }
  return { x: x + Math.sin(t * 0.5 + hash(w.id)) * 1.5, y, pose: blink ? 'blink' : 'stand', dir: 'down' };
}

export function busyAt(sc: Scene, id: string): boolean {
  return sc.episodes.some((e) => (e.model === id || e.ph.some((p) => p.d.to === id)) && sc.t >= (firstOf(e, 'go')?.t0 ?? OPEN) - 0.5 && sc.t <= endOf(e) + 1.7);
}

const walk = (a: [number, number], b: [number, number], p: number, face?: Dir): Plan => {
  const q = wq(a, b, p);
  return { x: q[0], y: q[1], pose: p >= 1 ? 'stand' : 'walk', dir: face ?? (b[0] < a[0] ? 'left' : 'right') };
};

/** 干活和回应的姿势按主题换：读信、敲键盘、躺上床、水里脸红 */
function workPose(sc: Scene, kind: 'work' | 'reply', base: Plan): Plan {
  const st = sc.theme.style;
  if (kind === 'work') {
    if (st.work === 'type') return { ...base, pose: 'type', item: 'none' };
    if (st.work === 'bed') return { ...base, pose: 'lie', item: 'none', hearts: true };
    return base;
  }
  if (st.reply === 'beam') return { ...base, pose: 'cast', item: 'wand' };
  if (st.reply === 'pigeon') return { ...base, pose: 'cast', item: 'flag' };
  if (st.reply === 'plane') return { ...base, pose: 'type', item: 'none' };
  if (st.reply === 'petal') return { ...base, pose: 'write', item: 'none', hearts: true };
  return { ...base, pose: 'wave', item: 'none' };
}

export function wizardPlan(w: Wizard, sc: Scene): Plan | null {
  const spot = sc.layout.wiz.get(w.id);
  if (!spot) return null;
  const t = sc.t;
  const slot = spot.slot, idle = spot.idle;
  const face = spot.slotDir;
  const W = sc.theme.words;
  let out: Plan | null = null;

  // 找这一刻和他有关的那一单
  for (const e of sc.episodes) {
    const go = phOf(e, 'go')[0];
    const hand = firstOf(e, 'handover'), fail = firstOf(e, 'fail');
    const mine = e.model === w.id, takeover = hand?.d.to === w.id;
    if (!mine && !takeover) continue;
    const done = firstOf(e, 'done');
    const finish = done ? done.t0 : endOf(e);
    const back = (done ? done.t1 : endOf(e)) + 1.2;
    if (!go || t < go.t0 || t > back) continue;
    // 失手的那位：垂下手、冒汗，退到一边歇着
    if (mine && fail && hand && hand.d.to !== w.id) {
      if (t < go.t1) { out = walk(idle, slot, (t - go.t0) / (go.t1 - go.t0), face); break; }
      if (t < fail.t0) { out = workPose(sc, 'work', { x: slot[0], y: slot[1], pose: 'read', dir: face, item: 'letter' }); break; }
      if (t < fail.t1) { out = { x: slot[0], y: slot[1], pose: 'droop', dir: face, sweat: true, item: 'none' }; break; }
      if (t < hand.t1) { out = walk(slot, spot.bench, (t - hand.t0) / (hand.t1 - hand.t0)); break; }
      if (t < fail.t0 + sc.coolSpan) { out = { x: spot.bench[0], y: spot.bench[1], pose: 'sit', dir: 'down', tag: W.say.cooling }; break; }
      continue;
    }
    // 接手的那位：从屋里走过来
    if (takeover && hand) {
      if (t < hand.t0) continue;
      if (t < hand.t1) { const q = (t - hand.t0) / ((hand.t1 - hand.t0) * 0.75); out = q < 1 ? walk(idle, slot, q, face) : { x: slot[0], y: slot[1], pose: 'stand', dir: face }; break; }
    } else if (t < go.t1) { out = walk(idle, slot, (t - go.t0) / (go.t1 - go.t0), face); break; }
    if (t < finish) {
      const cur = activeAt(e, t).find((p) => p.type !== 'appself');
      const ty = cur?.type;
      if (spot.floor === 'potion') { out = { x: slot[0], y: slot[1], pose: ty === 'ticket' || ty === 'result' ? 'cast' : 'stir', dir: face, item: ty === 'ticket' || ty === 'result' ? 'wand' : 'stick' }; if (sc.theme.style.reply !== 'beam' && out.pose === 'cast') out = { ...out, pose: 'wave', item: 'none' }; break; }
      if (spot.floor === 'paint') { out = { x: slot[0], y: slot[1], pose: ty === 'result' ? 'cast' : 'paint', dir: face, item: ty === 'result' ? 'wand' : 'brush' }; if (sc.theme.style.reply !== 'beam' && out.pose === 'cast') out = { ...out, pose: 'wave', item: 'none' }; break; }
      const replying = ty === 'tool' || ty === 'stream' || ty === 'result' || ty === 'ticket';
      if (replying) { out = workPose(sc, 'reply', { x: slot[0], y: slot[1], pose: 'cast', dir: face, item: 'wand' }); break; }
      if (ty === 'fail') { out = { x: slot[0], y: slot[1], pose: 'droop', dir: face, sweat: true, item: 'none' }; break; }
      out = workPose(sc, 'work', { x: slot[0], y: slot[1], pose: ty === 'work' || ty === 'toolret' ? 'read' : 'stand', dir: face, item: ty === 'work' ? 'letter' : 'none' });
      break;
    }
    // 交差：鞠躬，走回屋里
    const q = (t - finish) / (back - finish);
    if (q < 0.3) { out = { x: slot[0], y: slot[1], pose: sc.theme.layout.workAtDesk ? 'cheer' : 'bow', dir: face }; break; }
    if (sc.theme.layout.workAtDesk) { out = null; break; }
    out = walk(slot, idle, (q - 0.3) / 0.7);
    if (out.pose === 'stand') out = null;
    break;
  }

  const roles = examRoles(sc);
  if (!out && sc.exam && (roles.seat === w.id || roles.grader === w.id)) {
    const seat = roles.seat === w.id;
    const E = sc.theme.layout.exam;
    out = seat ? { x: E.seat[0], y: E.seat[1], pose: 'write', dir: 'right' } : { x: E.grader[0], y: E.grader[1], pose: 'read', dir: 'left', item: 'letter' };
    if (t - sc.exam.since < 0.9) out.puff = clamp((t - sc.exam.since) / 0.9);
  }
  if (!out && sc.sleeping.has(w.id)) out = { x: idle[0], y: idle[1], pose: 'sleep', dir: 'down', z: true, tag: W.say.asleep };
  const woke = sc.wakeAt.get(w.id);
  if (!out && woke !== undefined && t - woke >= 0 && t - woke < 1.6) out = { x: idle[0], y: idle[1], pose: 'stretch', dir: 'down' };
  if (!out) out = idlePlan(w, spot, sc);
  const poke = sc.pokes.get(w.id);
  if (poke !== undefined && t - poke >= 0 && t - poke < 1.6 && (out.pose === 'stand' || out.pose === 'blink' || out.pose === 'read' || out.pose === 'sleep' || out.pose === 'paint' || out.pose === 'sit')) out = { ...out, pose: 'wave', dir: 'down', z: false, item: 'none' };
  if (!isText(w.floor) && out.pose === 'sleep') out.pose = 'stand';
  return out;
}

/* ---------- 信使：猫头鹰、海鸥、小兵，或者寄信人自己走 ---------- */
interface CourierAt { pos: [number, number]; moving: boolean; letter?: string; perch?: boolean; dir: 1 | -1; p: number }
const arcFly = (a: [number, number], b: [number, number], p: number): [number, number] => { const q = wq(a, b, p); return [q[0], q[1] - Math.sin(clamp(p) * Math.PI) * 16]; };
/** 沿着几个路点走，总长按比例分 */
function walkVia(pts: [number, number][], p: number): [number, number] {
  const seg: number[] = []; let tot = 0;
  for (let i = 0; i + 1 < pts.length; i++) { const d = Math.hypot(pts[i + 1][0] - pts[i][0], pts[i + 1][1] - pts[i][1]); seg.push(d); tot += d; }
  let k = clamp(p) * tot;
  for (let i = 0; i < seg.length; i++) { if (k <= seg[i] || i === seg.length - 1) return lerp(pts[i], pts[i + 1], seg[i] ? clamp(k / seg[i]) : 1); k -= seg[i]; }
  return pts[pts.length - 1];
}

function courierPath(sc: Scene, tw: AppSpot, perch: [number, number], p: number): [number, number] {
  const st = sc.theme.style.courier;
  if (st === 'owl' || st === 'gull') return arcFly(tw.home, perch, p);
  return walkVia([tw.home, perch], p);
}

function couriersOf(tw: AppSpot, sc: Scene, perchIdx: { n: number }): CourierAt[] {
  const t = sc.t; const out: CourierAt[] = [];
  const P = sc.theme.layout.perches;
  for (const e of sc.episodes) {
    if (e.app !== tw.id) continue;
    for (let i = 0; i < e.ph.length; i++) {
      const p = e.ph[i];
      if (p.type === 'send') {
        const j = e.ph[i + 1];
        const back0 = j && j.type === 'judge' ? j.t1 : p.t1, back1 = back0 + 1.2;
        if (t < p.t0 || t >= back1) continue;
        const perch = P[perchIdx.n++ % P.length];
        if (t < p.t1) { const q = (t - p.t0) / (p.t1 - p.t0); out.push({ pos: courierPath(sc, tw, perch, q), moving: true, letter: C.white, dir: 1, p: q }); }
        else if (t < back0) out.push({ pos: perch, moving: false, letter: C.white, perch: true, dir: 1, p: 1 });
        else { const q = (t - back0) / 1.2; out.push({ pos: courierPath(sc, tw, perch, 1 - q), moving: true, dir: -1, p: q }); }
      }
      if (p.type === 'toolret') {
        const mid = p.t0 + (p.t1 - p.t0) * 0.6;
        if (t < p.t0 || t >= mid + 1.2) continue;
        const perch = P[perchIdx.n++ % P.length];
        if (t < mid) { const q = (t - p.t0) / (mid - p.t0); out.push({ pos: courierPath(sc, tw, perch, q), moving: true, letter: C.gold, dir: 1, p: q }); }
        else { const q = (t - mid) / 1.2; out.push({ pos: courierPath(sc, tw, perch, 1 - q), moving: true, dir: -1, p: q }); }
      }
    }
  }
  return out;
}

/** 寄信人：写信、等、去取东西、回桌前干自己的活、收到回信蹦一下；客户自己送信的主题里，送信那会儿人不在桌前 */
function studentPlan(tw: AppSpot, sc: Scene, couriers: CourierAt[]): Plan {
  const t = sc.t;
  const h: [number, number] = tw.desk, shelf: [number, number] = tw.shelf;
  const self = sc.theme.style.courier === 'self';
  if (self && couriers.length) {
    const c = couriers[0];
    return { x: c.pos[0], y: c.pos[1], pose: c.moving ? 'walk' : 'stand', dir: c.dir > 0 ? 'right' : 'left', item: c.letter ? 'letter' : 'none' };
  }
  for (const e of sc.episodes) {
    if (e.app !== tw.id || t < startOf(e) || t > endOf(e) + 0.8) continue;
    const ticket = firstOf(e, 'ticket');
    const result = ticket ? e.ph.find((p) => p.type === 'result' && p.t0 >= ticket.t1) : undefined;
    if (ticket && asyncAt(e, t)) { const q = (t - ticket.t1) / 1.2; return q < 1 ? walk(h, shelf, q) : { x: shelf[0], y: shelf[1], pose: 'write', dir: 'right' }; }
    if (ticket && result && t >= result.t0 && t < result.t1) { const q = (t - result.t0) / 1.0; return { ...walk(shelf, h, q), pose: q < 1 ? 'walk' : 'read' }; }
    for (const p of e.ph) {
      if (t < p.t0 || t >= p.t1) continue;
      const pr = prOf(p, t);
      if (p.type === 'send') return { x: h[0], y: h[1], pose: 'write', dir: 'right' };
      if (p.type === 'appwork') {
        if (p.t1 === OPEN) { const q = (t - p.t0) / 1.2; return q < 1 ? walk(h, shelf, q) : { x: shelf[0], y: shelf[1], pose: 'read', dir: 'right' }; }
        if (pr < 0.35) return walk(h, shelf, pr / 0.35);
        if (pr < 0.6) return { x: shelf[0], y: shelf[1], pose: 'read', dir: 'right' };
        return { ...walk(shelf, h, (pr - 0.6) / 0.4), pose: 'carry' };
      }
      if (p.type === 'stream' || p.type === 'result') return { x: h[0], y: h[1], pose: 'read', dir: 'left' };
      if (p.type === 'done') return { x: h[0], y: h[1], pose: 'cheer', dir: 'down' };
    }
    const d = firstOf(e, 'done');
    if (!d || t < d.t0) { const w = t - startOf(e); return { x: h[0], y: h[1], pose: w > 4 ? 'sit' : 'stand', dir: 'left' }; }
  }
  const sit = sc.theme.layout.appKind === 'throne' || sc.theme.layout.appKind === 'chair';
  const blink = Math.floor(t * 1.1 + hash(tw.id)) % 8 === 0;
  return { x: h[0], y: h[1], pose: sit ? (blink ? 'sleep' : 'sit') : blink ? 'blink' : 'stand', dir: 'down' };
}

/* ---------- 魔杖的光、信、纸条、鸽子、纸飞机、花笺、泡泡 ---------- */
const tpath = (a: [number, number], b: [number, number], p: number): [number, number] => {
  const mx = (a[0] + b[0]) / 2, my = (a[1] + b[1]) / 2 - 16; const u = 1 - p;
  return [u * u * a[0] + 2 * u * p * mx + p * p * b[0], u * u * a[1] + 2 * u * p * my + p * p * b[1]];
};
function magicRay(a: [number, number], b: [number, number], col: string, t: number, light: string) {
  ctx.strokeStyle = col; ctx.globalAlpha = 0.85; ctx.lineWidth = S * 0.8; ctx.beginPath();
  for (let i = 0; i <= 48; i++) { const q = tpath(a, b, i / 48); if (i) ctx.lineTo(q[0] * S, q[1] * S); else ctx.moveTo(q[0] * S, q[1] * S); }
  ctx.stroke();
  ctx.globalAlpha = 0.25; ctx.lineWidth = S * 2.4; ctx.stroke(); ctx.globalAlpha = 1;
  for (let k = 0; k < 5; k++) { const q = tpath(a, b, (t * 0.8 + k / 5) % 1); R(q[0], q[1], 1, 1, light); }
  R(a[0] - 1, a[1] - 1, 3, 3, light);
}
function letterAt(pt: [number, number], col: string, seal?: string) {
  R(pt[0] - 3, pt[1] - 2, 6, 4, col); R(pt[0] - 3, pt[1] - 2, 6, 1, 'rgba(28,27,25,.55)'); R(pt[0] - 2, pt[1] - 1, 1, 1, 'rgba(28,27,25,.3)'); R(pt[0] + 1, pt[1] - 1, 1, 1, 'rgba(28,27,25,.3)');
  if (seal) R(pt[0] - 1, pt[1], 2, 2, seal);
}
function note(pt: [number, number], t: number) {
  const f = Math.floor(t * 8) % 2;
  R(pt[0] - 3, pt[1], 6, 1, C.gold); R(pt[0] - 2, pt[1] - 1 - f, 1, 1, C.gold); R(pt[0] + 1, pt[1] - 1 - f, 1, 1, C.gold); R(pt[0], pt[1] - 1, 1, 1, '#FFF6D8');
}
function chunk(pt: [number, number], col: string, light: string) {
  ctx.globalAlpha = 0.4; R(pt[0] - 2, pt[1] - 2, 5, 5, col); ctx.globalAlpha = 1; R(pt[0] - 1, pt[1] - 1, 3, 3, light);
}
function arrowAt(pt: [number, number], col: string, dir: 1 | -1) {
  R(pt[0] - 3, pt[1], 7, 1, C.woodD); R(pt[0] + 3 * dir, pt[1] - 1, 1, 3, C.ink); R(pt[0] - 3 * dir, pt[1] - 1, 2, 1, col); R(pt[0] - 3 * dir, pt[1] + 1, 2, 1, col);
}
function planeAt(pt: [number, number], col: string, dir: 1 | -1) {
  for (let r = 0; r < 3; r++) R(dir > 0 ? pt[0] - 3 + r : pt[0] - 3 + r, pt[1] - 1 + r, 6 - r * 2, 1, col);
  R(dir > 0 ? pt[0] - 3 : pt[0] + 2, pt[1] - 1, 1, 3, 'rgba(28,27,25,.35)');
}
function petalAt(pt: [number, number], col: string, t: number) {
  const f = Math.floor(t * 4 + pt[0]) % 2;
  R(pt[0] - 2, pt[1] - 1 + f, 5, 3, col); R(pt[0] - 2, pt[1] - 1 + f, 5, 1, '#FFFFFF'); R(pt[0], pt[1] + f, 1, 1, '#C4613A');
}
function bubbleAt(pt: [number, number], col: string, big = false) {
  const r = big ? 2 : 1;
  R(pt[0] - r, pt[1] - r - 1, 2 * r + 1, 1, col); R(pt[0] - r, pt[1] + r + 1, 2 * r + 1, 1, col);
  R(pt[0] - r - 1, pt[1] - r, 1, 2 * r + 1, col); R(pt[0] + r + 1, pt[1] - r, 1, 2 * r + 1, col);
  R(pt[0] - r, pt[1] - r, 1, 1, '#FFFFFF');
}
function bottleAt(pt: [number, number], col: string) {
  R(pt[0] - 1, pt[1] - 2, 3, 5, '#7FA58A'); R(pt[0], pt[1] - 3, 1, 1, C.woodD); R(pt[0] - 1, pt[1] - 1, 3, 2, col); R(pt[0] + 1, pt[1] - 2, 1, 1, '#D6ECE0');
}
/** 传送：一圈金色、白色的星点散开 */
function sparkle(x: number, y: number, p: number, light: string) {
  for (let i = 0; i < 10; i++) { const a = (i / 10) * Math.PI * 2, r = 4 + p * 10; ctx.globalAlpha = 1 - p; R(x + Math.cos(a) * r, y + Math.sin(a) * r, 1, 1, i % 2 ? C.gold : light); }
  ctx.globalAlpha = 1;
}
function coinPop(x: number, y: number, p: number, s: string, c: string) {
  text(x, y - 8 - p * 10, s, { c, alpha: 1 - clamp((p - 0.6) / 0.4), bg: 'rgba(255,255,255,.92)' });
}
function hearts(x: number, y: number, t: number, n = 3) {
  for (let i = 0; i < n; i++) {
    const q = ((t * 0.6 + i / n) % 1);
    ctx.globalAlpha = 1 - q;
    const hx = x + Math.sin((t + i) * 3) * 2 + (i - 1) * 4, hy = y - q * 14;
    const col = i % 2 ? '#E8577A' : '#F28AA0';
    R(hx - 2, hy - 2, 2, 1, col); R(hx + 1, hy - 2, 2, 1, col); R(hx - 2, hy - 1, 5, 2, col); R(hx - 1, hy + 1, 3, 1, col); R(hx, hy + 2, 1, 1, col);
  }
  ctx.globalAlpha = 1;
}

/* ---------- 回应和派活的载具：按主题换 ---------- */
type Vehicle = 'beam' | 'pigeon' | 'plane' | 'petal' | 'bubble';
/** 从 a 到 b 的路：光走抛物线，鸽子和纸飞机走弧线，花笺飘下来，泡泡往上冒再拐过去 */
function vehiclePath(v: Vehicle, a: [number, number], b: [number, number], p: number): [number, number] {
  if (v === 'petal') { const q = lerp(a, b, p); return [q[0] + Math.sin(p * Math.PI * 4) * 4, q[1] - Math.sin(p * Math.PI) * 10]; }
  if (v === 'bubble') { const q = tpath(a, b, p); return [q[0] + Math.sin(p * Math.PI * 6) * 1.5, q[1]]; }
  if (v === 'plane') { const q = lerp(a, b, p); return [q[0], q[1] - Math.sin(p * Math.PI) * 12 + Math.sin(p * Math.PI * 3) * 2]; }
  return tpath(a, b, p);
}
function drawVehicle(v: Vehicle, pt: [number, number], col: string, t: number, dir: 1 | -1, light: string, kind: 'letter' | 'chunk' | 'note' = 'letter') {
  switch (v) {
    case 'beam': if (kind === 'chunk') chunk(pt, col, light); else if (kind === 'note') note(pt, t); else letterAt(pt, col, light); break;
    case 'pigeon': { const f = Math.floor(t * 8 + pt[0]) % 2; ctx.drawImage(spriteOf(`pigeon${f}`, PIGEON[f], PIGEON_PAL, S, dir < 0) as CanvasImageSource, Math.round(pt[0] - 4) * S, Math.round(pt[1] - 3) * S); if (kind !== 'chunk') R(pt[0] - 1, pt[1] + 2, 3, 2, col); break; }
    case 'plane': planeAt(pt, kind === 'chunk' ? light : col, dir); break;
    case 'petal': petalAt(pt, kind === 'chunk' ? '#FBD3DC' : col, t); break;
    case 'bubble': bubbleAt(pt, kind === 'chunk' ? light : col, kind !== 'chunk'); break;
  }
}

/* ---------- 画一个小人，返回点击区 ---------- */
export interface Hit { id: string; kind: 'wizard' | 'tower' | 'seer'; x0: number; y0: number; x1: number; y1: number }

function drawPerson(look: Look, p: Plan, t: number, alpha = 1): { hand: [number, number] } {
  const fps = p.pose === 'walk' ? STEPS : p.pose === 'cast' || p.pose === 'wave' || p.pose === 'cheer' ? 6 : p.pose === 'stir' || p.pose === 'paint' || p.pose === 'write' || p.pose === 'type' ? 4 : 2;
  const frame = Math.floor(t * fps + p.x * 0.01) % 6;
  const fr = frameOf(look, p.pose, frame, p.dir, S);
  const x0 = Math.round(p.x) - GW / 2, y0 = Math.round(p.y) - GH + fr.dy;
  ctx.globalAlpha = alpha;
  ctx.drawImage(fr.img as CanvasImageSource, x0 * S, y0 * S);
  ctx.globalAlpha = 1;
  return { hand: [x0 + fr.hand[0], y0 + fr.hand[1]] };
}

function drawFish(sc: Scene, w: Wizard, p: Plan, t: number): { hand: [number, number]; w: number; h: number } {
  const look = sc.theme.looks.fish!(w);
  const pose: FishPose = p.pose === 'droop' || p.pose === 'sit' ? 'sulk' : p.pose === 'sleep' ? 'sleep' : p.pose === 'wave' ? 'wave' : p.hearts || p.pose === 'lie' || p.pose === 'read' || p.pose === 'cast' || p.pose === 'type' ? 'blush' : 'swim';
  const frame = Math.floor(t * (p.pose === 'walk' ? 6 : 2.5) + p.x * 0.01) % 2;
  const dir: 'left' | 'right' = p.dir === 'left' ? 'left' : p.dir === 'right' ? 'right' : (Math.floor(t / 7 + hash(w.id)) % 2 ? 'left' : 'right');
  const fr = fishFrame(look, pose, frame, dir, S);
  const bob = Math.sin(t * 1.5 + hash(w.id)) * 1.5;
  const x0 = Math.round(p.x - fr.w / 2), y0 = Math.round(p.y - fr.h / 2 + bob);
  ctx.drawImage(fr.img as CanvasImageSource, x0 * S, y0 * S);
  return { hand: [x0 + fr.hand[0], y0 + fr.hand[1]], w: fr.w, h: fr.h };
}

/** 手里的东西：魔杖、书、信、搅药棒、画笔、小旗、绿头牌 */
function drawItem(p: Plan, hand: [number, number], t: number, glow: string) {
  const L = p.dir === 'left' ? -1 : 1;
  const [hx, hy] = hand;
  switch (p.item) {
    case 'wand': {
      if (p.pose === 'droop') { R(hx + L, hy + 1, 1, 1, C.woodD); R(hx + 2 * L, hy + 2, 1, 1, C.woodD); break; }
      const k = Math.floor(t * 6) % 2;
      R(hx + L, hy - 1, 1, 1, C.woodD); R(hx + 2 * L, hy - 2, 1, 1, C.woodD); R(hx + 3 * L, hy - 3 - k, 1, 1, glow);
      if (k) R(hx + 4 * L, hy - 4, 1, 1, C.gold);
      break;
    }
    case 'flag': { const k = Math.floor(t * 6) % 2; R(hx, hy - 8, 1, 9, C.woodD); R(hx + L, hy - 8 + k, 5, 4, glow); R(hx + L, hy - 8 + k, 5, 1, C.ink); break; }
    case 'tablet': R(hx - 1, hy - 4, 3, 5, '#5F7A4A'); R(hx - 1, hy - 4, 3, 1, '#F3EEE4'); break;
    case 'bottle': R(hx, hy - 3, 2, 4, '#7FA58A'); R(hx, hy - 4, 2, 1, C.woodD); break;
    case 'phone': R(hx - 1, hy - 3, 3, 4, C.ink); R(hx, hy - 2, 1, 2, glow); break;
    case 'book': R(hx - 3, hy - 1, 6, 4, '#8C2A1A'); R(hx - 1 + (Math.floor(t * 0.5) % 2), hy - 1, 1, 4, '#F3EEE4'); break;
    case 'letter': R(hx - 2 + L, hy - 2, 5, 4, '#FFFFFF'); R(hx - 2 + L, hy - 2, 5, 1, 'rgba(28,27,25,.4)'); break;
    case 'stick': R(hx + L, hy + 1, 1, 1, C.woodD); R(hx + 2 * L, hy + 3, 1, 3, C.woodD); break;
    case 'brush': R(hx + L, hy - 1, 1, 2, C.woodD); R(hx + 2 * L, hy - 2, 1, 1, [C.accent, C.blue, C.green][Math.floor(t / 2) % 3]); break;
  }
}

/** 应用的站台：猫头鹰桩、小帐篷、前台小桌、龙椅、躺椅 */
function drawStation(sc: Scene, tw: AppSpot, lit: boolean, working: boolean) {
  const t = sc.t, k = sc.theme.layout.appKind, [x, y] = [tw.x, tw.y];
  const night = sc.night;
  const lamp = lit ? tw.color : night ? '#8E7A4E' : '#5A5348';
  switch (k) {
    case 'post': {
      R(x - 9, y - 32, 3, 32, C.woodD); R(x - 9, y - 32, 1, 32, C.wood);
      R(x - 13, y - 36, 11, 2, C.woodD); R(x - 12, y - 38, 9, 2, C.accent); R(x - 11, y - 40, 7, 2, C.accent);
      R(x - 11, y - 24, 7, 8, '#3A3330'); R(x - 10, y - 23, 5, 6, lamp); if (lit) R(x - 9, y - 22, 3, 3, '#FFF6D8');
      R(x - 1, y - 9, 13, 2, C.woodD); R(x, y - 7, 1, 7, C.woodD); R(x + 10, y - 7, 1, 7, C.woodD); R(x + 2, y - 11, 5, 2, '#F3EEE4'); R(x + 8, y - 12, 1, 3, C.ink);
      break;
    }
    case 'tent': {
      for (let r = 0; r < 12; r++) R(x - 12 + r, y - 30 + r, 1 + r * 2 - (r > 5 ? (r - 5) * 0 : 0), 1, r % 3 ? '#EFE7D6' : '#E2D7C2');
      R(x - 12, y - 18, 24, 18, '#E8DFCB'); R(x - 2, y - 12, 4, 12, '#8C7B60');
      R(x - 12, y - 18, 1, 18, '#C9BFA6'); R(x + 11, y - 18, 1, 18, '#C9BFA6');
      R(x - 16, y - 34, 1, 34, C.woodD); R(x - 15, y - 34, 6, 4, tw.color); R(x - 15, y - 34, 6, 1, C.ink);
      R(x - 19, y - 24, 6, 7, '#3A3330'); R(x - 18, y - 23, 4, 5, lamp); if (lit) R(x - 17, y - 22, 2, 2, '#FFF6D8');
      R(x - 1, y - 9, 13, 2, C.woodD); R(x, y - 7, 1, 7, C.woodD); R(x + 10, y - 7, 1, 7, C.woodD); R(x + 3, y - 11, 4, 2, '#F3EEE4');
      break;
    }
    case 'desk': {
      R(x - 8, y - 9, 16, 2, '#DAD3C5'); R(x - 7, y - 7, 2, 7, '#B3A998'); R(x + 5, y - 7, 2, 7, '#B3A998');
      R(x - 6, y - 16, 8, 6, '#3A3330'); R(x - 5, y - 15, 6, 4, lamp); if (lit) R(x - 4, y - 14, 4, 2, working ? C.gold : '#FFF6D8'); R(x - 3, y - 10, 2, 1, '#3A3330');
      break;
    }
    case 'throne': {
      R(x - 8, y - 22, 16, 22, '#8C2A1A'); R(x - 7, y - 24, 14, 2, C.gold); R(x - 8, y - 22, 1, 22, C.gold); R(x + 7, y - 22, 1, 22, C.gold);
      R(x - 6, y - 8, 12, 4, '#B3261E'); R(x - 10, y - 6, 2, 6, C.gold); R(x + 8, y - 6, 2, 6, C.gold);
      R(x - 13, y - 26, 1, 26, C.woodD); R(x - 15, y - 26, 5, 7, lamp); R(x - 15, y - 26, 5, 1, C.gold); R(x - 15, y - 20, 5, 1, C.gold); if (lit) R(x - 14, y - 24, 3, 3, '#FFE2A8');
      break;
    }
    case 'chair': {
      R(x - 8, y - 10, 16, 2, C.woodD); R(x - 8, y - 14, 3, 4, C.woodD); R(x - 7, y - 8, 1, 8, C.woodD); R(x + 6, y - 8, 1, 8, C.woodD);
      for (let i = 0; i < 3; i++) R(x - 6 + i * 5, y - 9, 3, 1, i % 2 ? '#F3EEE4' : C.accent);
      R(x + 9, y - 28, 1, 28, C.woodD); R(x + 10, y - 29, 1, 1, C.ink); R(x + 8, y - 6, 3, 2, '#3A3330');
      R(x + 1, y - 17, 4, 6, '#3A3330'); R(x + 2, y - 16, 2, 4, lamp); if (lit && Math.floor(t * 3) % 2) R(x + 5, y - 19, 1, 1, C.red);
      break;
    }
  }
}

/** 判档那位手边的东西：水晶球、沙盘、屏幕、牌盘、收音机 */
function drawHubItem(sc: Scene, lit: boolean) {
  const t = sc.t, [x, y] = sc.theme.layout.hubItem, kind = sc.theme.style.hubItem;
  switch (kind) {
    case 'ball':
      R(x - 3, y - 2, 6, 9, C.woodD); R(x - 4, y - 3, 8, 2, C.wood);
      R(x - 3, y - 9, 7, 6, lit ? '#D6ECF5' : '#A9C6D6'); R(x - 2, y - 10, 5, 1, lit ? '#D6ECF5' : '#A9C6D6'); R(x - 1, y - 8, 2, 2, '#FFFFFF');
      if (lit) { ctx.globalAlpha = 0.35 + 0.25 * Math.sin(t * 8); R(x - 6, y - 13, 13, 12, '#E6F4FA'); ctx.globalAlpha = 1; }
      break;
    case 'map':
      R(x - 9, y - 10, 18, 2, C.woodD); R(x - 8, y - 8, 1, 8, C.woodD); R(x + 7, y - 8, 1, 8, C.woodD); R(x - 8, y - 12, 16, 3, '#E8DFC0');
      R(x - 5, y - 11, 3, 1, C.blue); R(x + 1, y - 11, 4, 1, C.green); if (lit) R(x - 1 + (Math.floor(t * 3) % 3), y - 12, 1, 1, C.red);
      break;
    case 'screen':
      R(x - 7, y - 20, 14, 10, '#3A3330'); R(x - 6, y - 19, 12, 8, lit ? '#D6ECF5' : '#8FA3B0'); R(x - 1, y - 10, 2, 3, '#3A3330');
      if (lit) for (let i = 0; i < 3; i++) R(x - 4, y - 17 + i * 2, 4 + ((Math.floor(t * 4) + i) % 3) * 2, 1, C.blue);
      break;
    case 'tray':
      R(x - 8, y - 9, 16, 3, '#8C2A1A'); for (let i = 0; i < 5; i++) R(x - 7 + i * 3, y - 12, 2, 3, lit && i === Math.floor(t * 3) % 5 ? '#FFFFFF' : C.green);
      break;
    case 'radio':
      R(x - 5, y - 8, 10, 6, '#8C6239'); R(x - 4, y - 7, 4, 4, '#3A3330'); R(x + 1, y - 7, 3, 3, C.gold); R(x + 3, y - 12, 1, 4, C.ink);
      if (lit) for (let i = 0; i < 3; i++) R(x + 6 + i * 2, y - 11 - ((Math.floor(t * 4) + i) % 3), 1, 1, '#FFFFFF');
      break;
  }
}

/* ---------- 一帧 ---------- */
export function render(c: CanvasRenderingContext2D, scale: number, sc: Scene): Hit[] {
  ctx = c; S = scale; fontOk = !!sc.font;
  const t = sc.t, L = sc.layout, TH = sc.theme, ST = TH.style, LY = TH.layout, W = TH.words;
  const hits: Hit[] = [];
  ctx.imageSmoothingEnabled = false;

  // 背景整图（缓存）；图还没到就先铺底色
  const key = `${TH.id}|${S}|${sc.night}|${sc.bg ? 1 : 0}`;
  let bg = bgCache.get(key);
  if (!bg) {
    bg = makeCanvas(WW * S, WH * S);
    const b = bg.getContext('2d') as CanvasRenderingContext2D;
    b.imageSmoothingEnabled = false;
    b.fillStyle = '#B7AE9F'; b.fillRect(0, 0, WW * S, WH * S);
    if (sc.bg) b.drawImage(sc.bg, 0, 0, WW * S, WH * S);
    if (sc.night) { b.fillStyle = ST.colors.night; b.fillRect(0, 0, WW * S, WH * S); }
    if (bgCache.size > 8) bgCache.clear();
    bgCache.set(key, bg);
  }
  ctx.drawImage(bg as CanvasImageSource, 0, 0);

  drawSky(sc);
  TH.deco?.(painter, sc);
  drawFloorSigns(sc);
  drawCellarLive(sc);

  // 应用这边：站台、灯亮不亮、寄信人
  const apps = appStates(sc.episodes, L.towers, t);
  const perchIdx = { n: 0 };
  const courierByApp = new Map<string, CourierAt[]>();
  for (const tw of L.towers) courierByApp.set(tw.id, couriersOf(tw, sc, perchIdx));
  L.towers.forEach((tw, ti) => {
    const s = apps[tw.id];
    const lit = !!s && (s.wait !== null || !!s.work || s.ticket);
    drawStation(sc, tw, lit, !!s?.work);
    const sp = studentPlan(tw, sc, courierByApp.get(tw.id)!);
    const { hand } = drawPerson(TH.looks.app(tw.id, tw.color), sp, t);
    if (sp.item) drawItem(sp, hand, t, ST.colors.light);
    nameTag(tw.desk[0], tw.y + 6 + (ti % 2) * 8, tw.name, C.ink, 'rgba(255,255,255,.88)');
    hits.push({ id: tw.id, kind: 'tower', x0: tw.x - 16, y0: tw.y - 40, x1: tw.x + 16, y1: tw.y + 12 });
    // 头顶只挂一句：在干自己的活 > 拿着取药券 > 在等；相邻两座错开高度
    const ly = tw.y - 34 - (ti % 2) * 9;
    if (s?.work) label(sp.x, ly, s.work, C.ink, ST.colors.paper);
    else if (s?.ticket) label(sp.x, ly, W.say.ticketHeld, C.ink, ST.colors.paper);
    else if (s && s.wait !== null) label(sp.x, ly, W.say.wait(s.wait), C.muted, 'rgba(255,255,255,.92)');
  });

  // 模型：小人或鱼
  const wandOf = new Map<string, [number, number]>();
  const handOf = new Map<string, [number, number]>();
  const posOf = new Map<string, [number, number]>();
  for (const w of sc.wizards) {
    const p = wizardPlan(w, sc);
    if (!p) continue;
    const spot = L.wiz.get(w.id)!;
    const linked = sc.episodes.some((e) => modelAt(e, t) === w.id && t >= (firstOf(e, 'go')?.t1 ?? OPEN) && t < endOf(e));
    let top = p.y - GH;
    if (ST.model === 'fish') {
      const f = drawFish(sc, w, p, t);
      wandOf.set(w.id, [p.x + (p.dir === 'left' ? -f.w / 2 : f.w / 2), p.y - 2]);
      handOf.set(w.id, [spot.slot[0], spot.slot[1] - 6]);
      top = p.y - f.h / 2;
      if (p.hearts || (linked && p.pose !== 'droop' && p.pose !== 'sit')) hearts(p.x, p.y - f.h / 2 - 2, t, 2);
      if (p.sweat) R(p.x + 4, p.y - f.h / 2 - 3, 1, 2, '#7FA0B8');
      if (p.z) { const k = Math.floor(t * 1.5) % 3; text(p.x + 8 + k, p.y - f.h / 2 - 6 - k * 3, 'z', { c: '#E8E4DC' }); }
      hits.push({ id: w.id, kind: 'wizard', x0: p.x - f.w / 2, y0: p.y - f.h / 2 - 4, x1: p.x + f.w / 2, y1: p.y + f.h / 2 + 2 });
    } else {
      const look = TH.looks.model(w);
      const { hand } = drawPerson(look, p, t, p.puff === undefined ? 1 : p.puff);
      if (p.puff !== undefined) sparkle(p.x, p.y - 12, p.puff, ST.colors.light);
      drawItem(p, hand, t, linked ? ST.colors.light : C.gold);
      const Ld = p.dir === 'left' ? -1 : 1;
      wandOf.set(w.id, [hand[0] + 3 * Ld, hand[1] - 3]);
      handOf.set(w.id, [spot.slot[0] + (spot.slotDir === 'left' ? -6 : 6), spot.slot[1] - 12]);
      if (p.hearts) hearts(p.x, p.y - GH - 2, t);
      if (p.sweat) R(p.x + 3, p.y - 21, 1, 2, '#7FA0B8');
      if (p.z) { const k = Math.floor(t * 1.5) % 3; text(p.x + 7 + k, p.y - 24 - k * 3, 'z', { c: sc.night ? '#E8E4DC' : C.muted }); }
      hits.push({ id: w.id, kind: 'wizard', x0: p.x - 8, y0: p.y - 26, x1: p.x + 8, y1: p.y + 8 });
    }
    posOf.set(w.id, [p.x, top]);
    if (p.tag && (sc.hover === w.id || p.tag === W.say.cooling)) label(p.x, top - 8, p.tag, p.tag === W.say.cooling ? C.blue : C.muted, '#fff');
    const hot = sc.hover === w.id;
    const atSlot = isText(w.floor) && Math.abs(p.x - spot.slot[0]) < 7;
    const tagY = atSlot || p.y + 14 > WH ? top - 7 - (spot.index % 2) * 8 : p.y + 6 + (spot.index % 2) * 8;
    if (sc.showNames || hot || atSlot) nameTag(p.x, tagY, w.name, hot ? C.accent : C.ink, hot ? '#fff' : 'rgba(255,255,255,.82)');
  }
  // 住不下的：挂在楼层牌子上面一行
  for (const [f, n] of Object.entries(L.overflow)) if (n) text(f === 'paint' ? LY.media.paintIdle[0] + 24 : f === 'potion' ? LY.media.potionIdle[0] + 24 : LY.band.x0 - 3, L.floorY[f as Floor] - (f === 'paint' || f === 'potion' ? 34 : 16), W.say.more(n), { c: C.muted, bg: 'rgba(255,255,255,.78)', align: f === 'paint' || f === 'potion' ? 'center' : 'right' });

  // 判档的那位
  const judging = sc.episodes.some((e) => activeAt(e, t).some((p) => p.type === 'judge'));
  const castR = sc.episodes.some((e) => activeAt(e, t).some((p) => p.type === 'go' || (p.type === 'toolret' && prOf(p, t) >= 0.6) || (p.type === 'handover' && prOf(p, t) >= 0.3)));
  const castL = sc.episodes.some((e) => activeAt(e, t).some((p) => p.type === 'bounce'));
  const hubCasting = castR || castL;
  const hubPose: Pose = hubCasting ? (ST.dispatch === 'beam' ? 'cast' : ST.dispatch === 'arrow' || ST.dispatch === 'bottle' ? 'wave' : 'carry') : judging ? 'read' : sc.paused ? 'sleep' : 'stand';
  const hubItem: Item = hubCasting ? (ST.dispatch === 'beam' ? 'wand' : ST.dispatch === 'runner' ? 'tablet' : 'none') : judging ? 'letter' : 'none';
  const seerPlan: Plan = { x: LY.hub[0], y: LY.hub[1], pose: hubPose, dir: castL && !castR ? 'left' : 'right', item: hubItem };
  const poked = sc.pokes.get('seer');
  if (poked !== undefined && t - poked >= 0 && t - poked < 1.6 && !hubCasting) { seerPlan.pose = 'wave'; seerPlan.dir = 'down'; seerPlan.item = 'none'; }
  const sh = drawPerson(TH.looks.hub, seerPlan, t);
  drawItem(seerPlan, sh.hand, t, ST.colors.light);
  const seerR: [number, number] = [LY.hub[0] + 9, LY.hub[1] - 16], seerL: [number, number] = [LY.hub[0] - 9, LY.hub[1] - 16];
  drawHubItem(sc, judging);
  posOf.set('seer', [LY.hub[0], LY.hub[1] - GH]);
  hits.push({ id: 'seer', kind: 'seer', x0: LY.hub[0] - 8, y0: LY.hub[1] - 26, x1: LY.hub[0] + 8, y1: LY.hub[1] + 2 });

  // 信使：平时蹲在自家站台，送完信回去等下一封
  for (const tw of L.towers) {
    const list = courierByApp.get(tw.id)!;
    if (ST.courier === 'self') continue;
    if (!list.length) drawCourier(sc, { pos: tw.home, moving: false, dir: 1, p: 0 }, t, tw.color);
    for (const o of list) drawCourier(sc, o, t, tw.color);
    posOf.set(`app:${tw.id}`, [tw.desk[0], tw.y - GH]);
  }
  for (const tw of L.towers) if (!posOf.has(`app:${tw.id}`)) { const sp = studentPlan(tw, sc, courierByApp.get(tw.id)!); posOf.set(`app:${tw.id}`, [sp.x, sp.y - GH]); }

  // 这一刻的来回
  for (const e of sc.episodes) {
    const tw = L.towers.find((x) => x.id === e.app);
    if (!tw) continue;
    for (const p of activeAt(e, t)) drawPhase(sc, e, p, tw, wandOf, handOf, seerR, seerL);
  }

  // 拖信：目标楼层发光
  if (sc.drag) {
    if (sc.drag.target) { const y = L.floorY[sc.drag.target]; ctx.globalAlpha = 0.22 + 0.1 * Math.sin(t * 6); R(LY.band.x0, y - LY.band.h, LY.band.x1 - LY.band.x0, LY.band.h, C.gold); ctx.globalAlpha = 1; }
    letterAt([sc.drag.x, sc.drag.y], '#FFFFFF', C.accent);
  }

  // 闲聊：两个气泡挨着就往上摞，先说的在下面
  const boxes: { x0: number; y0: number; x1: number; y1: number }[] = [];
  for (const b of [...sc.bubbles].sort((a, c) => a.t0 - c.t0)) {
    if (t < b.t0 || t >= b.t1) continue;
    const at = posOf.get(b.who);
    if (!at) continue;
    const fade = Math.min(1, (t - b.t0) / 0.15, (b.t1 - t) / 0.25);
    const o: TextOpt = { alpha: Math.max(0, fade), bg: b.kind === 'think' ? '#F3F1EC' : '#FFFFFF', border: b.kind === 'think' ? C.muted : C.ink, c: b.kind === 'think' ? C.muted : C.ink };
    let y = at[1] - 10;
    const probe = (yy: number) => { ctx.globalAlpha = 0; const r = text(at[0], yy, b.text, { ...o, alpha: 0, bg: undefined }); ctx.globalAlpha = 1; return r; };
    let r = probe(y);
    for (let k = 0; k < 4 && boxes.some((q) => r.x0 < q.x1 + 2 && r.x0 + r.w > q.x0 - 2 && r.y0 < q.y1 + 2 && r.y0 + r.h > q.y0 - 2); k++) { y -= r.h + 2; r = probe(y); }
    bubble(at[0], y, b.text, o);
    boxes.push({ x0: r.x0, y0: r.y0, x1: r.x0 + r.w, y1: r.y0 + r.h });
  }

  drawWeather(sc);
  drawHud(sc);
  return hits;
}

function drawCourier(sc: Scene, o: CourierAt, t: number, seal?: string) {
  const [x, y] = o.pos;
  const st = sc.theme.style;
  if (st.courier === 'runner') {
    const look = st.courierLook ?? sc.theme.looks.hub;
    const plan: Plan = { x, y, pose: o.moving ? 'walk' : 'stand', dir: o.moving ? (o.dir > 0 ? 'right' : 'left') : 'down', item: o.letter ? 'letter' : 'none' };
    const { hand } = drawPerson(look, plan, t);
    if (o.letter) drawItem(plan, hand, t, st.colors.light);
    return;
  }
  const gull = st.courier === 'gull';
  if (o.moving) {
    const f = Math.floor(t * 9) % 2;
    const img = gull ? spriteOf(`gull${f}`, GULL_FLY[f], GULL_PAL, S, o.dir < 0) : spriteOf(`owl${f}`, OWL_FLY[f], OWL_PAL, S, o.dir < 0);
    ctx.drawImage(img as CanvasImageSource, Math.round(x - 5) * S, Math.round(y - 4) * S);
    if (o.letter) letterAt([x, y + 4], o.letter, seal);
    return;
  }
  const blink = Math.floor(t * 0.8 + x) % 7 === 0, look = Math.floor(t * 0.4 + y) % 5 === 0;
  if (gull) { ctx.drawImage(spriteOf(`gullp${blink ? 1 : 0}`, blink ? GULL_PERCH.map((r, i) => (i === 1 ? r.replace('K', 'W') : r)) : GULL_PERCH, GULL_PAL, S) as CanvasImageSource, Math.round(x - 4) * S, Math.round(y - 7) * S); }
  else {
    const map = OWL_PERCH.map((r, i) => (i === 3 && blink ? r.replaceAll('K', 'b').replaceAll('Y', 'b') : look && (i === 2 || i === 3) ? '.' + r.slice(0, -1) : r));
    ctx.drawImage(spriteOf(`owlp${blink ? 1 : 0}${look ? 1 : 0}`, map, OWL_PAL, S) as CanvasImageSource, Math.round(x - 4) * S, Math.round(y - 8) * S);
  }
  if (o.letter) letterAt([x, y], o.letter, seal);
}

/** 判档的把活交给模型：魔杖的光、掷令箭、信封坐电梯、小太监跑去、扔漂流瓶 */
function drawDispatch(sc: Scene, from: [number, number], to: [number, number], pr: number, col: string, seal: string, t: number, floorY?: number) {
  const st = sc.theme.style, LY = sc.theme.layout;
  switch (st.dispatch) {
    case 'beam': magicRay(from, to, st.colors.ray, t, st.colors.light); letterAt(tpath(from, to, pr), col, seal); break;
    case 'arrow': { const pt = arcFly(from, to, pr); arrowAt(pt, col === C.red ? C.red : seal, to[0] >= from[0] ? 1 : -1); break; }
    case 'bottle': { const pt = arcFly(from, to, pr); bottleAt(pt, col); break; }
    case 'elevator': {
      const ex = st.elevatorX ?? LY.band.x0 - 6;
      const pts: [number, number][] = [from, [ex, from[1]], [ex, (floorY ?? to[1]) - 8], to];
      const pt = walkVia(pts, pr);
      if (Math.abs(pt[0] - ex) < 1) { R(ex - 5, pt[1] - 7, 11, 14, 'rgba(214,236,245,.55)'); R(ex - 5, pt[1] - 7, 11, 1, C.gold); R(ex - 5, pt[1] + 6, 11, 1, C.gold); }
      letterAt(pt, col, seal);
      break;
    }
    case 'runner': {
      const sx = st.stairsX ?? LY.band.x0 - 10;
      const pts: [number, number][] = [from, [sx, LY.ground], [sx, (floorY ?? to[1])], [to[0] - 8, floorY ?? to[1]]];
      const pt = walkVia(pts, pr);
      const dir: Dir = pr >= 0.98 ? 'right' : pts[3][0] >= from[0] ? 'right' : 'left';
      const plan: Plan = { x: pt[0], y: pt[1], pose: pr < 0.98 ? 'walk' : 'stand', dir, item: col === C.red ? 'letter' : 'tablet' };
      const { hand } = drawPerson(st.dispatchLook ?? sc.theme.looks.hub, plan, t);
      drawItem(plan, hand, t, st.colors.light);
      break;
    }
  }
}

function drawPhase(sc: Scene, e: Episode, p: Phase, tw: AppSpot, wandOf: Map<string, [number, number]>, handOf: Map<string, [number, number]>, seerR: [number, number], seerL: [number, number]) {
  const t = sc.t, TH = sc.theme, ST = TH.style, LY = TH.layout, W = TH.words;
  const pr = prOf(p, t);
  const m = modelAt(e, t);
  const w = sc.wizards.find((x) => x.id === m);
  const lc = w ? LANE_ROBE[w.lane] : C.accent;
  const win = tw.win;
  const wand = wandOf.get(m) ?? win;
  const slotHand = (id: string) => handOf.get(id) ?? wandOf.get(id) ?? ([LY.slotX, LY.floors.mid] as [number, number]);
  const floorOf = (id: string) => sc.layout.wiz.get(id)?.slot[1];
  const name = w?.name ?? '';
  const dir: 1 | -1 = win[0] < wand[0] ? -1 : 1;
  const V = ST.reply;
  switch (p.type) {
    case 'judge': {
      const [bx, by] = LY.bubble;
      if (p.d.text2) bubble(bx, by, pr < 0.5 ? p.d.text! : `${p.d.text} → ${p.d.text2}`, { border: pr >= 0.5 ? C.accent : C.ink, size: 'small' });
      else if (p.d.text) bubble(bx, by, p.d.text, { border: p.d.bad ? C.red : C.ink, c: p.d.bad ? C.red : C.ink, size: 'small' });
      break;
    }
    case 'go': drawDispatch(sc, seerR, slotHand(m), pr, '#FFFFFF', tw.color, t, floorOf(m)); break;
    case 'work': {
      if (!w || !isText(w.floor)) break;
      const sp = sc.layout.wiz.get(m); if (!sp) break;
      const dots = '·'.repeat(1 + (Math.floor(t * 3) % 3));
      text(sp.slot[0] + (sp.slotDir === 'left' ? -10 : 10), sp.slot[1] - 30, dots, { c: sc.night ? '#E8E4DC' : C.muted });
      break;
    }
    case 'tool': {
      if (V === 'beam') magicRay(wand, win, lc, t, ST.colors.light);
      const pt = vehiclePath(V, wand, win, pr); drawVehicle(V, pt, C.gold, t, dir, ST.colors.light, 'note'); label(pt[0], pt[1] - 10, p.d.text ?? W.say.toolAsk(1), C.ink, ST.colors.paper); break;
    }
    case 'toolret': { if (pr < 0.6) break; drawDispatch(sc, seerR, slotHand(m), (pr - 0.6) / 0.4, C.gold, tw.color, t, floorOf(m)); bubble(LY.bubble[0], LY.bubble[1], W.say.forward(name), { size: 'small' }); break; }
    case 'stream': {
      if (V === 'beam') magicRay(wand, win, lc, t, ST.colors.light);
      for (let i = 0; i < 6; i++) { const q = pr * 1.6 - i * 0.14; if (q > 0 && q < 1) drawVehicle(V, vehiclePath(V, wand, win, q), lc, t + i, dir, ST.colors.light, 'chunk'); }
      break;
    }
    case 'result': case 'ticket': {
      const col = p.type === 'ticket' ? C.gold : lc;
      if (V === 'beam') magicRay(wand, win, col, t, ST.colors.light);
      const pt = vehiclePath(V, wand, win, pr); drawVehicle(V, pt, col, t, dir, ST.colors.light);
      if (p.type === 'ticket') label(pt[0], pt[1] - 10, W.say.ticket, C.ink, ST.colors.paper);
      break;
    }
    case 'fail': { const j = slotHand(e.model); bubble(j[0] + 4, j[1] - 24, W.say.failRest(p.d.text ?? ''), { border: C.red, c: C.red, size: 'small' }); break; }
    case 'handover': { const to = p.d.to ? slotHand(p.d.to) : win; label(to[0] + 14, to[1] - 16, W.say.takeOver, C.accent, '#fff'); if (pr < 0.3) break; drawDispatch(sc, seerR, to, (pr - 0.3) / 0.7, '#FFFFFF', tw.color, t, p.d.to ? floorOf(p.d.to) : undefined); break; }
    case 'bounce': {
      if (ST.dispatch === 'beam') magicRay(seerL, win, C.red, t, ST.colors.light);
      const pt = ST.dispatch === 'runner' || ST.dispatch === 'elevator' ? walkVia([seerL, [tw.desk[0] + 12, LY.ground]], pr) : tpath(seerL, win, pr);
      if (ST.dispatch === 'runner') { const plan: Plan = { x: pt[0], y: pt[1], pose: pr < 0.98 ? 'walk' : 'stand', dir: 'left', item: 'letter' }; const { hand } = drawPerson(ST.dispatchLook ?? TH.looks.hub, plan, t); drawItem(plan, hand, t, ST.colors.light); }
      else letterAt(pt, C.red, tw.color);
      label(pt[0], pt[1] - 12, p.d.text ?? W.say.bounce, C.red, '#fff');
      break;
    }
    case 'progress': {
      const sp = sc.layout.wiz.get(m);
      const v = e.prog ?? clamp((t - p.t0) / (p.t1 === OPEN ? 30 : p.t1 - p.t0));
      const [cx, cy] = LY.media.cauldron;
      if (sp?.floor === 'potion') { R(cx - 12, cy - 22, 24, 3, C.ink); R(cx - 11, cy - 21, 22 * clamp(v), 1, '#9DBE7A'); label(cx, cy - 30, `${Math.round(clamp(v) * 100)}%`); }
      break;
    }
    case 'done': {
      const money = e.cost ? moneySpent(sc.money(e.cost)) : e.save ? moneySaved(sc.money(e.save)) : SUB_FREE;
      coinPop(tw.desk[0], tw.y - 30, pr, money, e.cost ? C.ink : C.green);
      if (e.save && pr < 0.8) { const q = arcFly([tw.desk[0], tw.y - 30], LY.media.coins, pr / 0.8); R(q[0] - 1, q[1] - 1, 3, 2, C.gold); R(q[0], q[1] - 1, 1, 1, ST.colors.light); }
      break;
    }
  }
}

function drawFloorSigns(sc: Scene) {
  const LY = sc.theme.layout, W = sc.theme.words;
  // 楼层牌子挂在楼外、本层地面上方一点，不压着栏杆边干活的人
  for (const f of FLOORS) {
    const y = LY.floors[f];
    text(LY.band.x0 - 3, y - 7, W.floor[f], { c: C.ink, bg: 'rgba(255,255,255,.78)', align: 'right' });
  }
  if (sc.paused) { text(LY.hub[0], LY.hub[1] - 44, W.say.closed, { c: '#F3EEE4', bg: C.woodD }); }
  if (sc.exam) bubble(LY.exam.seat[0] + 10, LY.exam.seat[1] - 36, W.say.exam, { border: C.gold, size: 'small' });
}

/** 底层会动的：坩埚冒泡、金币堆、额度瓶 */
function drawCellarLive(sc: Scene) {
  const t = sc.t, LY = sc.theme.layout, M = LY.media;
  const brewing = sc.episodes.some((e) => activeAt(e, t).some((p) => p.type === 'progress'));
  const [cx, cy] = M.cauldron;
  const k = brewing ? 3 : 1;
  for (let i = 0; i < k; i++) { const q = ((t * (brewing ? 1.4 : 0.5)) + i / k) % 1; R(cx - 4 + i * 4, cy - 2 - q * 12, 2, 2, '#9DBE7A'); }
  if (brewing) { ctx.globalAlpha = 0.25; for (let i = 0; i < 4; i++) R(cx - 6 + i * 3 + Math.sin(t * 2 + i) * 2, cy - 16 - i * 3, 4, 3, '#E8E4DC'); ctx.globalAlpha = 1; }
  // 金币：省得越多堆得越高（对数，免得满屏）
  const n = Math.min(48, Math.round(Math.log10(1 + sc.saved * 100) * 12));
  for (let i = 0; i < n; i++) R(M.coins[0] + (i % 12) * 3, M.coins[1] - Math.floor(i / 12) * 2, 2, 2, i % 3 ? C.gold : '#E0B15A');
  text(M.saved[0], M.saved[1], moneySaved(sc.money(sc.saved)), { c: '#F3EEE4', bg: 'rgba(28,27,25,.55)' });
  // 额度瓶：订阅还剩多少
  sc.bottles.slice(0, 3).forEach((b, i) => {
    const x = M.bottles[0] + i * 8, y = M.bottles[1];
    R(x, y - 12, 5, 12, '#E8E4DC'); R(x + 1, y - 14, 3, 2, '#E8E4DC');
    const h = Math.round(10 * clamp(b.left / 100));
    R(x + 1, y - 1 - h, 3, h, b.left < 20 ? C.red : b.left < 50 ? C.gold : '#7FA0B8');
  });
  if (sc.bottles.length) text(M.bottles[0] - 2, M.bottles[1] + 8, sc.bottles.slice(0, 3).map((b) => `${b.name} ${b.left}%`).join(' · '), { c: C.ink, bg: 'rgba(255,255,255,.8)', align: 'left' });
}

function drawSky(sc: Scene) {
  const t = sc.t, H = sc.theme.layout.skyH;
  if (sc.night) {
    for (let i = 0; i < 40; i++) { const x = (i * 71) % WW, y = (i * 23) % Math.max(20, H - 10); if ((Math.floor(t * 0.7 + i) % 5) !== 0) R(x, y, 1, 1, i % 4 ? '#E8E4DC' : '#F0D080'); }
    for (let dy = -4; dy <= 4; dy++) for (let dx = -4; dx <= 4; dx++) { const inMoon = dx * dx + dy * dy <= 18, bite = (dx - 2) * (dx - 2) + (dy + 1) * (dy + 1) <= 12; if (inMoon && !bite) R(244 + dx, 18 + dy, 1, 1, '#F3EEE4'); }
  }
  // 小鸟：隔一阵一两只掠过
  if (!sc.rain) for (let i = 0; i < 2; i++) {
    const cyc = 46 + i * 17, q = ((t + i * 23) % cyc) / cyc;
    if (q > 0.35) continue;
    const x = -10 + (q / 0.35) * (WW + 20), y = 14 + i * 9 + Math.sin(t * 2 + i) * 2;
    if (y > H - 8) continue;
    const f = Math.floor(t * 6) % 2, col = sc.night ? '#9AA3B8' : '#4A4540';
    R(x - 2, y - f, 1, 1, col); R(x - 1, y - f + 1, 1, 1, col); R(x, y + 1, 1, 1, col); R(x + 1, y - f + 1, 1, 1, col); R(x + 2, y - f, 1, 1, col);
  }
  for (let i = 0; i < 3; i++) {
    const x = ((i * 137 + t * (2 + i)) % (WW + 60)) - 40, y = 12 + i * 11;
    if (y > H - 6) continue;
    const col = sc.rain ? 'rgba(110,118,128,.8)' : sc.night ? 'rgba(80,88,110,.6)' : 'rgba(255,255,255,.85)';
    R(x, y, 26, 4, col); R(x + 5, y - 3, 14, 3, col); R(x + 10, y - 5, 7, 2, col);
  }
}

function drawWeather(sc: Scene) {
  const t = sc.t, st = sc.theme.style.weather;
  if (sc.rain && st.rain) {
    ctx.fillStyle = 'rgba(60,70,80,.16)'; ctx.fillRect(0, 0, WW * S, WH * S);
    for (let i = 0; i < 120; i++) { const x = (i * 47 + t * 60) % WW, y = (i * 31 + t * 170) % WH; R(x, y, 1, 3, 'rgba(120,150,180,.75)'); }
  }
  if (sc.rain && st.waves) { for (let i = 0; i < 24; i++) { const x = (i * 23 + t * 30) % WW; R(x, sc.theme.layout.ground + 2 + Math.round(Math.sin(x / 7 + t * 6) * 2), 5, 1, '#FFFFFF'); } }
  if (sc.snow && st.snow) {
    for (let i = 0; i < 80; i++) { const x = (i * 61 + Math.sin(t + i) * 6 + WW) % WW, y = (i * 29 + t * 18) % WH; R(x, y, 1, 1, '#FFFFFF'); }
  }
  if (sc.snow && st.fog) {
    // 雾：一条条淡白的横带慢慢飘，带子薄一点才不像贴了纸
    for (let i = 0; i < 14; i++) { const x = ((i * 73 + t * (5 + i % 3)) % (WW + 160)) - 80; const y = 30 + i * 17; ctx.globalAlpha = 0.09 + (i % 3) * 0.03; R(x, y, 90 + (i % 4) * 30, 4, '#FFFFFF'); }
    ctx.globalAlpha = 1;
  }
}

function drawHud(sc: Scene) {
  const W = sc.theme.words.weather;
  const w = sc.rain ? W.rain : sc.snow ? W.snow : sc.night ? W.night : W.clear;
  text(sc.theme.layout.hud[0], sc.theme.layout.hud[1], w, { c: C.ink, bg: 'rgba(255,255,255,.82)' });
}

/** 屏幕坐标 → 楼层：拖信到哪层就是请哪档重做 */
export function floorAt(x: number, y: number, theme: ThemeDef): Floor | undefined {
  const LY = theme.layout;
  if (x < LY.band.x0 || x > LY.band.x1) return undefined;
  for (const f of FLOORS) if (y <= LY.floors[f] + 2 && y > LY.floors[f] - LY.band.h - 2) return f;
  return undefined;
}

/** 给主题画自己的动画用的笔 */
export interface Painter {
  R: typeof R; px: typeof px; text: typeof text; label: typeof label; alpha: (a: number) => void; scale: () => number; ctx: () => CanvasRenderingContext2D;
}
const painter: Painter = { R, px, text, label, alpha: (a) => { ctx.globalAlpha = a; }, scale: () => S, ctx: () => ctx };
