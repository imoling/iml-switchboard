/**
 * 小人按层拼：身体 → 袍子花纹 → 头发 → 头饰 → 胡子 → 眼镜，最后描一圈深色边，手里拿什么由画面另画。
 * 长相由名字算：同一个型号永远是同一张脸；袍子看渠道（订阅陶土、按量付费蓝、媒体绿），头饰看档位，
 * 每套主题换自己的头饰和花纹（魔法学院尖帽、点将台头盔、后宫旗头、写字楼眼镜）。新加一个型号自动就有自己的样子。
 * 16×24，朝下、朝右、朝上三个方向，朝左用镜像。海王主题的鱼另有一套。
 */
import { type Floor, hash, type LaneGroup, type Wizard } from './model';

export type Dir = 'down' | 'right' | 'up' | 'left';
export type Pose =
  | 'stand' | 'walk' | 'cast' | 'read' | 'sit' | 'sleep' | 'bow' | 'cheer' | 'wave' | 'droop'
  | 'stir' | 'paint' | 'write' | 'carry' | 'stretch' | 'blink' | 'type' | 'lie';
export type Hat = 'none' | 'hat' | 'tall' | 'hood' | 'beret' | 'helmet' | 'plume' | 'crown' | 'qitou' | 'eunuch' | 'straw' | 'cap';
export type FaceGear = 'none' | 'glasses' | 'shades';
export type Pattern = 'none' | 'trim' | 'belt' | 'armor' | 'dragon' | 'tie' | 'floral';

export interface Look {
  key: string;
  skin: string; hair: string; robe: string; trim: string;
  hat: Hat;
  hatColor: string;
  beard: 'none' | 'short' | 'long';
  hairStyle: 0 | 1 | 2;
  face?: FaceGear;
  pattern?: Pattern;
  /** 旗头上的花、头盔上的翎、金冠：花纹的第二种颜色 */
  accent?: string;
}

export const GW = 16, GH = 24;
export const LANE_ROBE: Record<LaneGroup, string> = { sub: '#C4613A', api: '#4A6785', media: '#5F7A4A' };
export const OUTLINE = '#2A221C';
const SKINS = ['#F3D5B5', '#EBC39E', '#D9A27A', '#B98360', '#8E5E3E'];
const HAIRS = ['#3A2E26', '#6B4A2E', '#C9A06A', '#1C1B19', '#8E5A3A', '#A33A2A', '#D8D3C8'];
const TRIMS = ['#F3EEE4', '#E6D3B0', '#2A2926', '#F3EEE4'];

export function shade(hex: string, k = 0.78): string {
  const n = parseInt(hex.slice(1), 16);
  const f = (v: number) => Math.max(0, Math.min(255, Math.round(v * k)));
  return `#${[f(n >> 16), f((n >> 8) & 255), f(n & 255)].map((v) => v.toString(16).padStart(2, '0')).join('')}`;
}

/** 名字定脸：肤色、发色、发型 */
export function faceOf(id: string): Pick<Look, 'skin' | 'hair' | 'hairStyle'> {
  const h = hash(id);
  return { skin: SKINS[(h >>> 5) % SKINS.length], hair: HAIRS[h % HAIRS.length], hairStyle: ((h >>> 3) % 3) as Look['hairStyle'] };
}

/** 魔法学院的巫师：渠道定袍子，楼层定头饰（学徒不戴帽，导师尖帽，教授加白胡子或白发，校长金边高帽） */
export function lookFor(w: Pick<Wizard, 'id' | 'lane' | 'floor'>): Look {
  const h = hash(w.id);
  const robe = LANE_ROBE[w.lane];
  const f: Floor = w.floor;
  const face = faceOf(w.id);
  let hat: Hat = 'hat', beard: Look['beard'] = 'none';
  let hair = face.hair, hairStyle = face.hairStyle;
  if (f === 'low') hat = 'none';
  if (f === 'paint') hat = 'beret';
  if (f === 'high') { if (h % 2 === 0) beard = 'short'; else { hair = '#E8E4DC'; hairStyle = 1; } }
  if (f === 'top') { hat = 'tall'; beard = 'long'; hair = '#E8E4DC'; }
  const hatColor = f === 'top' ? '#3E5870' : shade(robe, 0.62);
  return { key: `${w.id}|${w.lane}|${f}`, skin: face.skin, hair, robe, trim: f === 'top' ? '#D1A24A' : TRIMS[(h >>> 7) % TRIMS.length], hat, hatColor, beard, hairStyle, pattern: 'trim' };
}

/** 寄信人：应用的颜色做袍子，不戴帽 */
export function lookStudent(id: string, color: string): Look {
  const face = faceOf(id);
  return { key: `app|${id}|${color}`, ...face, hair: HAIRS[hash(id) % 6], robe: color, trim: '#F3EEE4', hat: 'none', hatColor: color, beard: 'none', pattern: 'trim' };
}

/** 占卜师：兜帽，就是 Switchboard 自己 */
export const SEER_LOOK: Look = { key: 'seer', skin: '#EBC39E', hair: '#D8D3C8', robe: '#5F6B4A', trim: '#D1A24A', hat: 'hood', hatColor: '#3E4A33', beard: 'none', hairStyle: 1, pattern: 'trim' };

type Grid = string[][];
interface Built { grid: Grid; hand: [number, number]; dy: number }

function blank(): Grid { return Array.from({ length: GH }, () => Array<string>(GW).fill('.')); }
function put(g: Grid, x: number, y: number, c: string) { if (x >= 0 && x < GW && y >= 0 && y < GH) g[y][x] = c; }
function rows(g: Grid, x0: number, y0: number, arr: string[]) {
  arr.forEach((r, j) => { for (let i = 0; i < r.length; i++) if (r[i] !== '.') put(g, x0 + i, y0 + j, r[i]); });
}

const BODY_DOWN = [
  '....RRRRRRRR....',
  '...RRRRTTRRRr...',
  '...RRRRTTRRRr...',
  '...RRRRRRRRRr...',
  '...RRRRRRRRRr...',
  '..RRRRRRRRRRrr..',
  '..RRRRRRRRRRrr..',
  '..rrrrrrrrrrrr..',
];
const BODY_UP = BODY_DOWN.map((r) => r.replaceAll('T', 'R'));
const BODY_RIGHT = [
  '.....RRRRRR.....',
  '....RRRRRRRT....',
  '....RRRRRRRT....',
  '....RRRRRRRr....',
  '....RRRRRRRr....',
  '...RRRRRRRRRr...',
  '...RRRRRRRRRr...',
  '...rrrrrrrrrr...',
];
const HEAD: Record<'down' | 'right' | 'up', string[]> = {
  down: ['.....HHHHHH.....', '....HHHHHHHH....', '....HSSSSSSH....', '....SSKSSKSS....', '....SSSSSSSS....', '.....SSssSS.....', '.......ss.......'],
  right: ['.....HHHHHH.....', '....HHHHHHHH....', '....HHHSSSSS....', '....HHSSSKSS....', '....HHSSSSSSS...', '.....HSSSSSS....', '.......ss.......'],
  up: ['.....HHHHHH.....', '....HHHHHHHH....', '....HHHHHHHH....', '....HHHHHHHH....', '....HHHHHHHH....', '.....HHHHHH.....', '.......ss.......'],
};
const HATS: Record<'hat' | 'tall', Record<'down' | 'right', string[]>> = {
  hat: {
    down: ['.........A......', '........AA......', '.......AAA......', '.......AAAA.....', '......AAAAA.....', '......AAAAAA....', '.....aaaaaaa....', '...AAAAAAAAAAA..'],
    right: ['....A...........', '....AA..........', '.....AAA........', '.....AAAA.......', '......AAAAA.....', '......AAAAA.....', '.....aaaaaaa....', '...AAAAAAAAAAAA.'],
  },
  tall: {
    down: ['........A.......', '.......AAA......', '.......AAA......', '......AAAAA.....', '......AAAAA.....', '.....GGGGGGG....', '.....AAAAAAA....', '..GAAAAAAAAAAG..'],
    right: ['...AA...........', '....AA..........', '.....AAA........', '.....AAAAA......', '......AAAAA.....', '.....GGGGGGG....', '.....AAAAAAA....', '..GAAAAAAAAAAAG.'],
  },
};
const HOOD: Record<'down' | 'right' | 'up', string[]> = {
  down: ['.....OOOOOO.....', '....OOOOOOOO....', '...OOOOOOOOOO...', '...OOSSSSSSOO...', '...OSSKSSKSSO...', '...OSSSSSSSSO...', '...OOSSssSSOO...', '....OOOOOOOO....'],
  right: ['.....OOOOOO.....', '....OOOOOOOO....', '...OOOOOOOOOO...', '...OOOOSSSSSS...', '...OOOSSSKSSS...', '...OOOSSSSSSSS..', '...OOOOSSSSSO...', '....OOOOOOOO....'],
  up: ['.....OOOOOO.....', '....OOOOOOOO....', '...OOOOOOOOOO...', '...OOOOOOOOOO...', '...OOOOOOOOOO...', '...OOOOOOOOOO...', '...OOOOOOOOOO...', '....OOOOOOOO....'],
};
const BEARD: Record<'short' | 'long', Record<'down' | 'right', string[]>> = {
  short: { down: ['....WSSSSSSW....', '....WWSSSSWW....', '....WWWWWWWW....', '.....WWWWWW.....', '......WWWW......'], right: ['.......SSSSSW...', '.......WWWWWW...', '........WWWWW...', '.........WWW....'] },
  long: { down: ['....WSSSSSSW....', '....WWSSSSWW....', '....WWWWWWWW....', '.....WWWWWW.....', '.....WWWWWW.....', '......WWWW......', '......wWWw......', '.......WW.......'], right: ['.......SSSSSW...', '.......WWWWWW...', '........WWWWW...', '........WWWWW...', '.........WWWW...', '.........WWW....', '..........W.....'] },
};
/** 头盔、金冠、旗头这些盖在头发上：从头顶上方两格起画 */
const CAPS: Record<'helmet' | 'crown' | 'qitou' | 'eunuch' | 'straw' | 'cap', Record<'down' | 'right', string[]>> = {
  helmet: { down: ['.....NNNNNN.....', '....NNNNNNNN....', '....NnNNNNnN....', '....NNNNNNNN....'], right: ['.....NNNNNN.....', '....NNNNNNNN....', '....NNNNNNnN....', '....NNNNNNNN....'] },
  crown: { down: ['....G..GG..G....', '....GGGGGGGG....', '....GgGGGGgG....'], right: ['....G..GG..G....', '....GGGGGGGG....', '....GgGGGGgG....'] },
  qitou: { down: ['..QQQQQQQQQQQQ..', '..QqQQQQQQQQqQ..', '....QQQQQQQQ....'], right: ['..QQQQQQQQQQQQ..', '..QqQQQQQQQQqQ..', '....QQQQQQQQ....'] },
  eunuch: { down: ['........P.......', '.....KKKKKK.....', '....KKKKKKKK....'], right: ['........P.......', '.....KKKKKK.....', '....KKKKKKKK....'] },
  straw: { down: ['.....YYYYYY.....', '....YYYYYYYY....', '..YYYYYYYYYYYY..'], right: ['.....YYYYYY.....', '....YYYYYYYY....', '..YYYYYYYYYYYY..'] },
  cap: { down: ['.....NNNNNN.....', '....NNNNNNNN....', '....NNNNNNNNNN..'], right: ['.....NNNNNN.....', '....NNNNNNNN....', '....NNNNNNNNNN..'] },
};

/** 拼一帧。手的位置单独返回，魔杖、书、搅药棒由画面接着画 */
export function build(look: Look, pose: Pose, frame: number, dir: 'down' | 'right' | 'up'): Built {
  const g = blank();
  const f = frame % 3;
  let dy = 0, headDy = 0, headDx = 0, sit = false;
  if (pose === 'cheer' && frame % 2) dy = -2;
  if (pose === 'bow') { headDy = 2; headDx = dir === 'right' ? 1 : 0; }
  if (pose === 'sit' || pose === 'sleep' || pose === 'lie') sit = true;
  if (pose === 'sleep' || pose === 'lie') headDy = 1;

  // 身体：坐下时整个往下挪两格，腿收起来
  const bodyY = sit ? 16 : 14;
  const body = dir === 'down' ? BODY_DOWN : dir === 'up' ? BODY_UP : BODY_RIGHT;
  rows(g, 0, bodyY, sit ? body.slice(0, 6).concat(body[7]) : body);
  if (!sit) {
    if (dir === 'right') {
      const legs = pose === 'walk' ? [[5, 9], [4, 10], [6, 8]][f] : [5, 8];
      for (const lx of legs) { put(g, lx, 22, 'B'); put(g, lx + 1, 22, 'B'); put(g, lx, 23, 'B'); put(g, lx + 1, 23, 'B'); }
    } else {
      const lift = pose === 'walk' ? f : 0;
      for (const [lx, side] of [[4, 1], [10, 2]] as const) { put(g, lx, 22, 'B'); put(g, lx + 1, 22, 'B'); if (lift !== side) { put(g, lx, 23, 'B'); put(g, lx + 1, 23, 'B'); } }
    }
  } else if (dir === 'right') { put(g, 9, 22, 'B'); put(g, 10, 22, 'B'); put(g, 11, 22, 'B'); }
  else { put(g, 4, 22, 'B'); put(g, 5, 22, 'B'); put(g, 10, 22, 'B'); put(g, 11, 22, 'B'); }

  // 袍子花纹
  const pat = look.pattern ?? 'none';
  if (dir !== 'right') {
    if (pat === 'belt') for (let x = 3; x < 13; x++) put(g, x, bodyY + 4, 'G');
    if (pat === 'armor') { for (let y = 0; y < 4; y++) for (let x = 4; x < 12; x++) put(g, x, bodyY + y, x === 11 ? 'n' : 'N'); put(g, 7, bodyY + 1, 'G'); put(g, 8, bodyY + 1, 'G'); }
    if (pat === 'dragon') { put(g, 7, bodyY, 'G'); put(g, 8, bodyY, 'G'); for (const [x, y] of [[6, 2], [9, 2], [7, 3], [8, 3], [7, 1], [8, 1]]) put(g, x, bodyY + y, 'G'); for (let x = 3; x < 13; x++) put(g, x, bodyY + 5, 'G'); }
    if (pat === 'tie') { put(g, 6, bodyY, 'W'); put(g, 9, bodyY, 'W'); put(g, 5, bodyY + 1, 'W'); put(g, 10, bodyY + 1, 'W'); for (let y = 1; y < 5; y++) { put(g, 7, bodyY + y, 'T'); put(g, 8, bodyY + y, 'T'); } }
    if (pat === 'floral') for (const [x, y] of [[5, 2], [10, 1], [7, 5], [11, 4], [4, 6]]) put(g, x, bodyY + y, 'F');
  } else {
    if (pat === 'belt') for (let x = 4; x < 12; x++) put(g, x, bodyY + 4, 'G');
    if (pat === 'armor') for (let y = 0; y < 4; y++) for (let x = 5; x < 11; x++) put(g, x, bodyY + y, 'N');
    if (pat === 'dragon') for (let x = 4; x < 12; x++) put(g, x, bodyY + 5, 'G');
    if (pat === 'floral') for (const [x, y] of [[6, 2], [9, 4], [7, 6]]) put(g, x, bodyY + y, 'F');
  }

  // 头
  const headY = (sit ? 9 : 7) + headDy;
  if (look.hat === 'hood') rows(g, headDx, headY - 1, HOOD[dir]);
  else {
    rows(g, headDx, headY, HEAD[dir]);
    if (look.hairStyle === 1) for (let y = headY + 3; y < headY + (dir === 'up' ? 9 : 7); y++) { put(g, 4 + headDx, y, 'H'); if (dir !== 'right') put(g, 11 + headDx, y, 'H'); }
    if (look.hairStyle === 2 && look.hat === 'none') { put(g, 7 + headDx, headY - 1, 'H'); put(g, 8 + headDx, headY - 1, 'H'); put(g, 7 + headDx, headY - 2, 'h'); put(g, 8 + headDx, headY - 2, 'h'); }
  }
  // 眼睛：眨眼、睡觉闭上；眼镜、墨镜
  const eyesShut = pose === 'blink' || pose === 'sleep' || pose === 'lie';
  if (eyesShut) for (let x = 0; x < GW; x++) if (g[headY + 3]?.[x] === 'K') g[headY + 3][x] = pose === 'sleep' || pose === 'lie' ? 's' : 'S';
  if (look.face === 'shades' && dir !== 'up') { const xs = dir === 'right' ? [7, 8, 9, 10, 11] : [4, 5, 6, 7, 8, 9, 10, 11]; for (const x of xs) put(g, x + headDx, headY + 3, 'K'); }
  if (look.face === 'glasses' && dir !== 'up') { const xs = dir === 'right' ? [8, 10, 11] : [5, 7, 8, 10]; for (const x of xs) put(g, x + headDx, headY + 3, 'w'); }
  // 胡子
  if (look.beard !== 'none' && dir !== 'up') rows(g, headDx, headY + 4, BEARD[look.beard][dir]);
  // 帽子
  const hd = dir === 'up' ? 'down' : dir;
  if (look.hat === 'hat' || look.hat === 'tall') rows(g, headDx, headY - 7, HATS[look.hat][hd]);
  if (look.hat === 'beret') rows(g, headDx, headY - 1, dir === 'right' ? ['....AAAAAAA.....', '...AAAAAAAAa....'] : ['....AAAAAAAA....', '...AAAAAAAAAa...']);
  if (look.hat === 'helmet' || look.hat === 'plume') { rows(g, headDx, headY - 2, CAPS.helmet[hd]); if (look.hat === 'plume') { put(g, 8 + headDx, headY - 3, 'F'); put(g, 8 + headDx, headY - 4, 'F'); put(g, 9 + headDx, headY - 5, 'F'); } }
  if (look.hat === 'crown') rows(g, headDx, headY - 3, CAPS.crown[hd]);
  if (look.hat === 'qitou') { rows(g, headDx, headY - 3, CAPS.qitou[hd]); if (look.accent) { put(g, 3 + headDx, headY - 4, 'F'); put(g, 4 + headDx, headY - 4, 'F'); put(g, 3 + headDx, headY - 3, 'F'); put(g, 12 + headDx, headY - 4, 'G'); } }
  if (look.hat === 'eunuch') rows(g, headDx, headY - 3, CAPS.eunuch[hd]);
  if (look.hat === 'straw') rows(g, headDx, headY - 2, CAPS.straw[hd]);
  if (look.hat === 'cap') rows(g, headDx, headY - 2, CAPS.cap[hd]);

  // 胳膊：袖子 r，手 S
  const arm = (pts: [number, number][], hand: [number, number]) => { for (const [x, y] of pts) put(g, x, y + (sit ? 2 : 0), 'r'); put(g, hand[0], hand[1] + (sit ? 2 : 0), 'S'); return [hand[0], hand[1] + (sit ? 2 : 0)] as [number, number]; };
  let hand: [number, number] = [8, 20];
  const k = frame % 2;
  if (dir === 'right') {
    switch (pose) {
      case 'cast': hand = arm([[8, 15], [9, 15], [10, 14], [11, 14], [12, 13]], [13, 12 - k]); break;
      case 'droop': hand = arm([[8, 15], [8, 16], [9, 17], [9, 18], [10, 19]], [10, 20]); break;
      case 'read': case 'carry': hand = arm([[8, 15], [8, 16], [9, 17]], [10, 17]); break;
      case 'stir': hand = arm([[8, 15], [9, 16], [10, 17]], [11 + k, 18]); break;
      case 'paint': hand = arm([[8, 15], [9, 15], [10, 15], [11, 15 - k]], [12, 14 - k]); break;
      case 'write': case 'type': hand = arm([[8, 15], [9, 16], [10, 17]], [11, 18 - k]); break;
      case 'walk': hand = arm([[7, 15], [7, 16], [8, 17], [8, 18]], f === 1 ? [9, 19] : f === 2 ? [7, 19] : [8, 19]); break;
      case 'sit': case 'sleep': case 'lie': hand = arm([[8, 15], [9, 16]], [10, 17]); break;
      default: hand = arm([[8, 15], [8, 16], [8, 17], [8, 18]], [8, 19]);
    }
  } else if (dir === 'down') {
    const sides = () => { arm([[2, 15], [2, 16], [2, 17], [2, 18]], [2, 19]); return arm([[13, 15], [13, 16], [13, 17], [13, 18]], [13, 19]); };
    switch (pose) {
      case 'read': case 'carry': case 'write': case 'sit': case 'sleep': case 'lie': arm([[3, 15], [4, 16], [5, 17]], [6, 17]); hand = arm([[12, 15], [11, 16], [10, 17]], [9, 17]); break;
      case 'type': arm([[3, 15], [4, 16], [5, 17]], [6, 17 + k]); hand = arm([[12, 15], [11, 16], [10, 17]], [9, 18 - k]); break;
      case 'cheer': case 'stretch': arm([[2, 14], [2, 13], [1, 12]], [1, 11]); hand = arm([[13, 14], [13, 13], [14, 12]], [14, 11]); break;
      case 'wave': arm([[2, 15], [2, 16], [2, 17], [2, 18]], [2, 19]); hand = arm([[13, 14], [13, 13], [14 - k, 12]], [14 - k, 11]); break;
      case 'walk': arm([[2, 15], [2, 16], [2, 17]], [2, f === 1 ? 17 : 18]); hand = arm([[13, 15], [13, 16], [13, 17]], [13, f === 2 ? 17 : 18]); break;
      case 'cast': arm([[2, 15], [2, 16], [2, 17], [2, 18]], [2, 19]); hand = arm([[13, 15], [13, 14], [14, 13]], [14, 12 - k]); break;
      default: hand = sides();
    }
  } else {
    arm([[2, 15], [2, 16], [2, 17], [2, 18]], [2, pose === 'walk' && f === 1 ? 18 : 19]);
    hand = arm([[13, 15], [13, 16], [13, 17], [13, 18]], [13, pose === 'walk' && f === 2 ? 18 : 19]);
  }
  outline(g);
  return { grid: g, hand, dy };
}

/** 描边：挨着实心格的空格涂成深色，小人在花哨的背景前才看得清 */
export function outline(g: Grid) {
  const H = g.length, W = g[0].length;
  const solid = (x: number, y: number) => x >= 0 && y >= 0 && x < W && y < H && g[y][x] !== '.' && g[y][x] !== 'Z';
  const edge: [number, number][] = [];
  for (let y = 0; y < H; y++) for (let x = 0; x < W; x++) {
    if (g[y][x] !== '.') continue;
    if (solid(x - 1, y) || solid(x + 1, y) || solid(x, y - 1) || solid(x, y + 1)) edge.push([x, y]);
  }
  for (const [x, y] of edge) g[y][x] = 'Z';
}

function palette(look: Look): Record<string, string> {
  return {
    K: '#1C1B19', S: look.skin, s: shade(look.skin, 0.84), H: look.hair, h: shade(look.hair, 0.8), R: look.robe, r: shade(look.robe, 0.78),
    T: look.trim, A: look.hatColor, a: look.hat === 'tall' ? '#D1A24A' : shade(look.hatColor, 0.75), G: '#D1A24A', g: '#A67C2E', W: '#F3F1EC', w: '#D8D3C8', B: '#3A3330',
    O: look.hatColor, o: shade(look.hatColor, 0.8), N: '#9A968C', n: '#6E6A62', Q: '#1C1B19', q: '#3A3330', P: '#B3261E', Y: '#D9B86A', F: look.accent ?? '#E8577A', Z: OUTLINE,
  };
}

type Canvasish = HTMLCanvasElement | OffscreenCanvas;
const cache = new Map<string, { img: Canvasish; hand: [number, number]; dy: number; w: number; h: number }>();

function makeCanvas(w: number, h: number): Canvasish {
  if (typeof OffscreenCanvas !== 'undefined') return new OffscreenCanvas(w, h);
  const c = document.createElement('canvas'); c.width = w; c.height = h; return c;
}

function paint(grid: Grid, pal: Record<string, string>, S: number, flip: boolean): Canvasish {
  const h = grid.length, w = grid[0].length;
  const img = makeCanvas(w * S, h * S);
  const c = img.getContext('2d') as CanvasRenderingContext2D | null;
  if (c) {
    for (let y = 0; y < h; y++) for (let x = 0; x < w; x++) {
      const ch = grid[y][x];
      if (ch === '.') continue;
      c.fillStyle = pal[ch] ?? '#f0f';
      c.fillRect((flip ? w - 1 - x : x) * S, y * S, S, S);
    }
  }
  return img;
}

/** 取一帧（按缩放缓存）。朝左是朝右的镜像 */
export function frameOf(look: Look, pose: Pose, frame: number, dir: Dir, S: number) {
  const base = dir === 'left' ? 'right' : dir;
  const key = `${look.key}|${pose}|${frame % 6}|${dir}|${S}`;
  let hit = cache.get(key);
  if (hit) return hit;
  const b = build(look, pose, frame, base);
  const img = paint(b.grid, palette(look), S, dir === 'left');
  const hand: [number, number] = dir === 'left' ? [GW - 1 - b.hand[0], b.hand[1]] : b.hand;
  hit = { img, hand, dy: b.dy, w: GW, h: GH };
  if (cache.size > 4000) cache.clear();
  cache.set(key, hit);
  return hit;
}

/** 测试用：只拼不画 */
export const buildForTest = build;

/* ---------- 鱼：海王主题的模型 ---------- */
export interface FishLook { key: string; body: string; belly: string; size: 'small' | 'mid' | 'big' | 'gold'; shades?: boolean }
export type FishPose = 'swim' | 'blush' | 'sulk' | 'sleep' | 'wave';

const FISH: Record<FishLook['size'], string[]> = {
  small: [
    '...BBBBB....',
    '..BBBEBBB.TT',
    '.BBBBBBBBTT.',
    '..bbLbbbbTT.',
    '...bbbbb..TT',
  ],
  mid: [
    '....BBBBBB.....',
    '..BBBBBBBBB..TT',
    '.BBBBEBBBBBBTT.',
    '.BBBBBBBBBBBT..',
    '..bLLbbbbbbbTT.',
    '...bbbbbbb...TT',
  ],
  big: [
    '.....BBBBBBB......',
    '...BBBBBBBBBBB....',
    '..BBBBBEBBBBBBB.TT',
    '.BBBBBBBBBBBBBBTT.',
    '.BBBBBBBBBBBBBBT..',
    '..bbLLLbbbbbbbbTT.',
    '...bbbbbbbbbbb..TT',
    '.....bbbbbbb......',
  ],
  gold: [
    '......GGGGGGGG........',
    '....GGGGGGGGGGGG......',
    '...GGGGGGEGGGGGGGG..TT',
    '..GGGGGGGGGGGGGGGGGTT.',
    '..GGGGGGGGGGGGGGGGGT..',
    '...gggLLLgggggggggTT..',
    '....ggggggggggggg..TT.',
    '......ggggggggg.......',
  ],
};

export function fishLookFor(w: Pick<Wizard, 'id' | 'lane' | 'floor'>): FishLook {
  const h = hash(w.id);
  const size: FishLook['size'] = w.floor === 'top' ? 'gold' : w.floor === 'high' ? 'big' : w.floor === 'mid' ? 'mid' : 'small';
  const bodies = w.lane === 'sub' ? ['#E07A2E', '#C4613A', '#D98B1F'] : w.lane === 'api' ? ['#4A6785', '#3E7CB1', '#5B8BA8'] : ['#5F7A4A', '#6F9A5A', '#4E7A5A'];
  return { key: `fish|${w.id}|${w.lane}|${w.floor}`, body: bodies[h % bodies.length], belly: '#F3EEE4', size, shades: w.floor === 'paint' };
}

export function fishFrame(look: FishLook, pose: FishPose, frame: number, dir: 'left' | 'right', S: number) {
  const key = `${look.key}|${pose}|${frame % 2}|${dir}|${S}`;
  let hit = cache.get(key);
  if (hit) return hit;
  const src = FISH[look.size];
  const w = src[0].length, h = src.length;
  const g: Grid = src.map((r) => r.split(''));
  // 尾巴摆：隔帧把尾鳍整体挪一格
  if (frame % 2) for (let y = 0; y < h; y++) { const r = g[y]; const i = r.indexOf('T'); if (i > 0) { const j = r.lastIndexOf('T'); for (let x = i; x <= j; x++) if (r[x] === 'T') { r[x] = '.'; } for (let x = i; x <= j; x++) if (src[y][x] === 'T') r[x - 1] = r[x - 1] === '.' ? 'T' : r[x - 1]; } }
  // 眼睛：E；闭眼、墨镜
  for (let y = 0; y < h; y++) for (let x = 0; x < w; x++) {
    if (g[y][x] !== 'E') continue;
    if (pose === 'sleep' || pose === 'sulk') g[y][x] = 'b';
    if (look.shades) { g[y][x] = 'K'; if (x + 1 < w) g[y][x + 1] = 'K'; if (x - 1 >= 0) g[y][x - 1] = 'K'; }
    if (pose === 'blush' && y + 1 < h) g[y + 1][x] = 'P';
  }
  outline(g);
  const pal = { B: look.body, b: shade(look.body, 0.72), E: '#1C1B19', K: '#1C1B19', L: look.belly, T: shade(look.body, 0.85), G: '#E0B15A', g: '#B8892E', P: '#F28AA0', Z: OUTLINE };
  const img = paint(g, pal, S, dir === 'left');
  hit = { img, hand: [dir === 'left' ? w - 2 : 1, Math.floor(h / 2)], dy: 0, w, h };
  if (cache.size > 4000) cache.clear();
  cache.set(key, hit);
  return hit;
}

/* ---------- 猫头鹰、海鸥、鸽子 ---------- */
export const OWL_PERCH = [
  '.b.....b.',
  '.bbbbbbb.',
  'bYYbbbYYb',
  'bYKbObKYb',
  'bbbbbbbbb',
  '.bcbcbcb.',
  '.bcccccb.',
  '..bbbbb..',
  '...g.g...',
];
export const OWL_FLY = [
  [
    'w.........w',
    'ww.......ww',
    '.wwbbbbbww.',
    '..bbYKbbO..',
    '..bbbbbbb..',
    '...bcccb...',
    '....g.g....',
  ],
  [
    '...........',
    '...........',
    '..bbbbbb...',
    '.wbbYKbbO..',
    'wwwbbbbbwww',
    'w..bcccb..w',
    '....g.g....',
  ],
];
export const OWL_PAL: Record<string, string> = { b: '#8A6A48', c: '#D8C3A0', Y: '#F0D080', K: '#1C1B19', O: '#D1A24A', g: '#D1A24A', w: '#6F5238', Z: OUTLINE };

export const GULL_PERCH = [
  '...WWW...',
  '..WWKWWO.',
  '..WWWWW..',
  '.WWWWWWW.',
  '.WWWWWWn.',
  '..WWWWW..',
  '...g.g...',
];
export const GULL_FLY = [
  ['W.........W', 'WW.......WW', '.WWWWWWWWW.', '...WWKWWO..', '....WWW....'],
  ['...........', '....WWW....', '.WWWWKWWO..', 'WW..WWW..WW', 'W.........W'],
];
export const GULL_PAL: Record<string, string> = { W: '#F3F1EC', n: '#9A968C', K: '#1C1B19', O: '#D98B1F', g: '#D98B1F', Z: OUTLINE };

export const PIGEON = [
  ['w.....w', '.wwWWw.', '..WWKO.', '..WWW..'],
  ['.......', '..WWKO.', 'wwWWWww', 'w..W..w'],
];
export const PIGEON_PAL: Record<string, string> = { W: '#F3F1EC', w: '#C9C4BA', K: '#1C1B19', O: '#D98B1F', Z: OUTLINE };

/** 描边后的小图（鸟、鸽子）按缩放缓存 */
export function spriteOf(key: string, map: string[], pal: Record<string, string>, S: number, flip = false): Canvasish {
  const k = `px|${key}|${S}|${flip}`;
  let hit = cache.get(k);
  if (hit) return hit.img;
  const g: Grid = map.map((r) => r.split(''));
  outline(g);
  const img = paint(g, pal, S, flip);
  hit = { img, hand: [0, 0], dy: 0, w: map[0].length, h: map.length };
  cache.set(k, hit);
  return img;
}

/* ---------- 3×5 小字：型号名的英文字母和数字，1 格一个像素点 ---------- */
export const MICRO: Record<string, string[]> = {
  A: ['.X.', 'X.X', 'XXX', 'X.X', 'X.X'], B: ['XX.', 'X.X', 'XX.', 'X.X', 'XX.'], C: ['.XX', 'X..', 'X..', 'X..', '.XX'], D: ['XX.', 'X.X', 'X.X', 'X.X', 'XX.'],
  E: ['XXX', 'X..', 'XX.', 'X..', 'XXX'], F: ['XXX', 'X..', 'XX.', 'X..', 'X..'], G: ['.XX', 'X..', 'X.X', 'X.X', '.XX'], H: ['X.X', 'X.X', 'XXX', 'X.X', 'X.X'],
  I: ['XXX', '.X.', '.X.', '.X.', 'XXX'], J: ['..X', '..X', '..X', 'X.X', '.X.'], K: ['X.X', 'X.X', 'XX.', 'X.X', 'X.X'], L: ['X..', 'X..', 'X..', 'X..', 'XXX'],
  M: ['X.X', 'XXX', 'XXX', 'X.X', 'X.X'], N: ['XX.', 'X.X', 'X.X', 'X.X', 'X.X'], O: ['.X.', 'X.X', 'X.X', 'X.X', '.X.'], P: ['XX.', 'X.X', 'XX.', 'X..', 'X..'],
  Q: ['.X.', 'X.X', 'X.X', '.X.', '..X'], R: ['XX.', 'X.X', 'XX.', 'X.X', 'X.X'], S: ['.XX', 'X..', '.X.', '..X', 'XX.'], T: ['XXX', '.X.', '.X.', '.X.', '.X.'],
  U: ['X.X', 'X.X', 'X.X', 'X.X', 'XXX'], V: ['X.X', 'X.X', 'X.X', 'X.X', '.X.'], W: ['X.X', 'X.X', 'XXX', 'XXX', 'X.X'], X: ['X.X', 'X.X', '.X.', 'X.X', 'X.X'],
  Y: ['X.X', 'X.X', '.X.', '.X.', '.X.'], Z: ['XXX', '..X', '.X.', 'X..', 'XXX'],
  '0': ['.X.', 'X.X', 'X.X', 'X.X', '.X.'], '1': ['.X.', 'XX.', '.X.', '.X.', 'XXX'], '2': ['XX.', '..X', '.X.', 'X..', 'XXX'], '3': ['XX.', '..X', '.X.', '..X', 'XX.'],
  '4': ['X.X', 'X.X', 'XXX', '..X', '..X'], '5': ['XXX', 'X..', 'XX.', '..X', 'XX.'], '6': ['.XX', 'X..', 'XX.', 'X.X', '.X.'], '7': ['XXX', '..X', '.X.', '.X.', '.X.'],
  '8': ['.X.', 'X.X', '.X.', 'X.X', '.X.'], '9': ['.X.', 'X.X', '.XX', '..X', 'XX.'],
  '-': ['...', '...', 'XXX', '...', '...'], '.': ['...', '...', '...', '...', '.X.'], ' ': ['..', '..', '..', '..', '..'],
};
/** 能不能全用小字写出来（只有英文、数字、空格、横线、点） */
export const microOk = (s: string) => [...s.toUpperCase()].every((ch) => MICRO[ch] !== undefined);
export const microWidth = (s: string) => [...s.toUpperCase()].reduce((w, ch) => w + (MICRO[ch]?.[0].length ?? 3) + 1, -1);
