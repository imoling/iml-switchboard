/**
 * 一套主题 = 一张背景 + 一份布局 + 一套叫法 + 几个画法开关。
 * 五套主题（魔法学院、点将台、写字楼、后宫、海王）共用同一个剧本和同一个渲染器（scene.ts），
 * 空间语法也一样：应用在左边，判档的在中间，档位在右边一层层摞着，媒体在底下。
 */
import type { Tier } from '../api/types';
import type { Floor, Wizard } from './model';
import type { Painter, Scene } from './scene';
import type { Dir, FishLook, Look } from './sprites';

export type ThemeKey = 'academy' | 'camp' | 'office' | 'harem' | 'sea';

export interface SceneWords {
  /** 楼层牌子：校长室、教授 · 高档… */
  floor: Record<Floor, string>;
  /** 档位的叫法：学徒、导师、教授、校长 */
  title: Record<Tier, string>;
  /** 判档时冒的那句，键是 low/mid/high/top/image/video/animation */
  judge: Record<string, string>;
  judgeRule: (k: string) => string;
  judgeCrystal: (who: string, k: string) => string;
  judgeSticky: (name: string) => string;
  judgeTooThick: (wan: number) => string;
  nobody: string;
  /** 判档的那位叫什么：占卜师、军师、前台… */
  hub: string;
  /** 媒体两位叫什么（花名册里没名字时用） */
  painter: string; filmer: string;
  say: {
    toolAsk: (n: number) => string; toolFetch: string; toolBack: string; forward: (name: string) => string;
    ticket: string; ticketHeld: string; ownWork: string; failRest: (why: string) => string; takeOver: string;
    bounce: string; bounceFail: (why: string) => string; shorten: string; cooling: string; asleep: string; exam: string; closed: string;
    wait: (s: number) => string; more: (n: number) => string; dropHint: (title: string) => string; redoing: (title: string) => string; vault: string;
  };
  weather: { rain: string; snow: string; clear: string; night: string };
  empty: { title: string; wizard: string; tower: string };
  caption: {
    arrive: (app: string, floor: string, who: string, via: string) => string;
    rejudge: (judge: string, from: string, to: string) => string;
    sticky: (app: string, who: string) => string;
    toolAsk: (who: string, app: string) => string;
    toolBack: (app: string, who: string) => string;
    handover: (from: string, to: string, why: string) => string;
    ticket: (app: string) => string;
    done: (who: string, money: string) => string;
    failed: (app: string, why: string) => string;
    bounced: (app: string) => string;
    jobDone: (path: string) => string;
  };
  /** 这个世界里才有的闲聊（通用的在 chatter.ts） */
  banter: string[];
  /** 按时机分的场景台词：idle / task / done / fail / takeover / skipped / night / poke，有就和通用的混着用 */
  lines: Partial<Record<'idle' | 'task' | 'done' | 'fail' | 'takeover' | 'skipped' | 'night' | 'poke', string[]>>;
  /** 点一下判档的那位，他说的话 */
  hubPoke: string[];
}

export interface ThemeLayout {
  /** 应用站台脚下的 y，和第 i 座（共 n 座）的 x */
  ground: number;
  appX: (n: number, i: number) => number;
  appKind: 'post' | 'tent' | 'desk' | 'throne' | 'chair';
  /** 应用去取东西的地方：书架、军需箱、档案柜、书案、饵桶 */
  shelf: [number, number];
  hub: [number, number];
  hubItem: [number, number];
  /** 判档气泡的位置 */
  bubble: [number, number];
  /** 信使落脚点，不够就循环用 */
  perches: [number, number][];
  exam: { seat: [number, number]; grader: [number, number] };
  /** 每层脚下的 y */
  floors: Record<Floor, number>;
  /** 楼层的横向范围和层高：拖信高亮、点到哪层 */
  band: { x0: number; x1: number; h: number };
  /** 干活的位置：栏杆、床、水面 */
  slotX: number; slotDir: Dir;
  /** 在自己工位上干活（写字楼）：不走到栏杆边，闲着也坐着 */
  workAtDesk?: boolean;
  /** 闲着时站的 x，几个就是每层最多几位 */
  idleXs: number[];
  /** 歇着（冷却）的位置：x 在本层，或指定一个点（冷宫） */
  benchX: number; bench?: [number, number];
  media: {
    paintIdle: [number, number]; potionIdle: [number, number]; potionSlot: [number, number];
    /** 坩埚、放映屏、剪辑台：进度条画在它上面 */
    cauldron: [number, number];
    coins: [number, number]; saved: [number, number]; bottles: [number, number];
  };
  hud: [number, number];
  /** 天空到哪为止：星星月亮画在这上面 */
  skyH: number;
}

export interface ThemeStyle {
  /** 应用那边的跑腿：猫头鹰、海鸥、小兵/小太监、客户自己走 */
  courier: 'owl' | 'gull' | 'runner' | 'self';
  courierLook?: Look;
  /** 判档的把活交给模型：魔杖的光、掷令箭、信封坐电梯、小太监跑去、扔漂流瓶 */
  dispatch: 'beam' | 'arrow' | 'elevator' | 'runner' | 'bottle';
  dispatchLook?: Look;
  /** 模型回应：魔杖的光、信鸽、纸飞机、花笺、泡泡 */
  reply: 'beam' | 'pigeon' | 'plane' | 'petal' | 'bubble';
  model: 'person' | 'fish';
  /** 干活的姿势：读信、敲键盘、上床、游到水面 */
  work: 'read' | 'type' | 'bed' | 'swim';
  hubItem: 'ball' | 'map' | 'screen' | 'tray' | 'radio';
  weather: { rain: boolean; snow: boolean; fog: boolean; waves: boolean };
  /** 电梯井的 x（写字楼） */
  elevatorX?: number;
  /** 跑腿上楼走的路点（后宫） */
  stairsX?: number;
  colors: { light: string; ray: string; paper: string; night: string };
}

export interface ThemeDef {
  id: ThemeKey;
  bg: string;
  words: SceneWords;
  layout: ThemeLayout;
  style: ThemeStyle;
  looks: {
    model: (w: Wizard) => Look;
    fish?: (w: Wizard) => FishLook;
    app: (id: string, color: string) => Look;
    hub: Look;
  };
  /** 主题自己的动画：瀑布、旗、海浪、电梯井，叠在背景图上 */
  deco?: (p: Painter, sc: Scene) => void;
}
