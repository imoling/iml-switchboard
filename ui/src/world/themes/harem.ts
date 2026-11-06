/**
 * 后宫（彩蛋）：谁接进来谁就是皇上。三位皇上坐在白石露台上，小太监把口谕送到敬事房，总管翻牌判档；
 * 敬事房的小太监捧着绿头牌跑到那位佳丽宫门口，佳丽上小床冒着小爱心干活，干完写花笺，花笺一张张飘回皇上手里。
 * 底下是戏台、丹青房、内务府，最右边是冷宫（冷却的去那儿）。亲密只用小床和爱心表示；台词借几句宫斗剧的梗。
 */
import bg from '../../assets/world/harem-day.png';
import { flutter, lamp, sun } from '../deco';
import type { Wizard } from '../model';
import type { Painter, Scene } from '../scene';
import { faceOf, LANE_ROBE, type Look, shade } from '../sprites';
import type { SceneWords, ThemeDef } from '../theme';

const title = { low: '答应', mid: '嫔', high: '贵妃', top: '皇后' } as const;
const titleOf = (k: string) => (title as Record<string, string>)[k] ?? k;

export const HAREM_WORDS: SceneWords = {
  floor: { top: '皇后 · 顶配', high: '贵妃 · 高档', mid: '嫔 · 中档', low: '答应 · 低档', paint: '丹青房', potion: '戏台' },
  title,
  judge: { low: '翻答应的牌', mid: '翻嫔的牌', high: '翻贵妃的牌', top: '请皇后', image: '传丹青', video: '传伶人排戏', animation: '传伶人排戏' },
  judgeRule: (k) => `敬事房：翻${titleOf(k)}的牌`,
  judgeCrystal: (who, k) => `${who || '太后'}发话：换${titleOf(k)}`,
  judgeSticky: (name) => `还是 ${name}（专宠）`,
  judgeTooThick: (wan) => (wan > 0 ? `折子约 ${wan} 万，太厚` : '折子太厚'),
  nobody: '没人能接驾',
  hub: '敬事房总管',
  painter: '丹青', filmer: '伶人',
  say: {
    toolAsk: (n) => (n > 1 ? `请皇上跑 ${n} 个工具` : '请皇上跑个工具'),
    toolFetch: '皇上亲自去书案翻',
    toolBack: '送回工具结果',
    forward: (name) => `还是 ${name}（专宠）`,
    ticket: '戏单', ticketHeld: '揣着戏单', ownWork: '回去批折子',
    failRest: (why) => `臣妾身子不爽（${why}）`,
    takeOver: '姐姐歇着，妹妹来',
    bounce: '折子太厚，原折发还', bounceFail: (why) => `没办成（${why}）`, shorten: '皇上删减折子',
    cooling: '冷宫 · 冷却中', asleep: '月例用完 · 睡了', exam: '选秀', closed: '罢朝',
    wait: (s) => `等了 ${Math.round(s)} 秒`,
    more: (n) => `还有 ${n} 位`,
    dropHint: (t) => `松手：请${t}重做`, redoing: (t) => `已请${t}重做`, vault: '内务府',
  },
  weather: { rain: '下雨：刚有报错', snow: '下雪：月例（订阅额度）紧张', clear: '晴：一切正常', night: '三更了：一切正常' },
  empty: { title: '宫里还没有人', wizard: '添加模型后，每个型号是一位佳丽', tower: '接入应用后，每个应用一把龙椅' },
  caption: {
    arrive: (app, floor, who, via) => `${app} 皇上口谕，${floor}，翻 ${who} 的牌（${via}）`,
    rejudge: (judge, from, to) => `${judge || '太后'}把${from}改成${to}`,
    sticky: (app, who) => `${app} 皇上专宠 ${who}`,
    toolAsk: (who, app) => `${who} 请 ${app} 皇上在本机跑工具`,
    toolBack: (app, who) => `${app} 皇上送回工具结果，还是 ${who} 伺候`,
    handover: (from, to, why) => `${from} ${why}，同一段对话交给 ${to} 接着伺候`,
    ticket: (app) => `先递戏单，${app} 皇上不用干等`,
    done: (who, money) => `${who} 复命：${money}`,
    failed: (app, why) => `${app} 皇上这道口谕没办成（${why}）`,
    bounced: (app) => `${app} 皇上的折子谁都接不下，原折发还`,
    jobDone: (path) => `戏排好了：${path}`,
  },
  banter: ['敬事房今天翻了十块牌', '冷宫的暖气又坏了', '皇后娘娘又睡了一天', '戏台那边在排新戏', '内务府的银子又多了', '皇上今天批了八本折子'],
  lines: {
    idle: ['臣妾做不到啊', '本宫乏了', '这福气给你要不要', '翠果，看茶', '皇上今日翻了几块牌', '冷宫的饭不好吃', '位分高的活也多', '逆风如解意，容易莫摧残'],
    task: ['臣妾遵旨', '皇上有请，这就去', '牌子翻到我了', '臣妾这就伺候'],
    done: ['复命了，皇上', '臣妾办妥了', '皇上看着可还满意'],
    fail: ['臣妾身子不适', '臣妾冤枉', '是她们害臣妾'],
    takeover: ['妹妹替姐姐伺候', '本宫来'],
    skipped: ['怎么又不翻我的牌', '本宫失宠了', '敬事房收了谁的银子'],
    night: ['三更了，皇上还不歇', '夜里批折子伤身'],
    poke: ['放肆', '大胆，谁准你碰本宫', '来人，拖出去'],
  },
  hubPoke: ['牌子不是随便翻的', '翻牌看的是活，不是脸', '别戳，奴才正忙着', '皇上的口谕都记着呢'],
};

function lady(w: Wizard): Look {
  const face = faceOf(w.id);
  const robe = LANE_ROBE[w.lane];
  const base: Look = { key: `harem|${w.id}|${w.lane}|${w.floor}`, ...face, hair: '#1C1B19', robe, trim: '#F3EEE4', hat: 'qitou', hatColor: '#1C1B19', beard: 'none', pattern: 'floral' };
  switch (w.floor) {
    case 'paint': return { ...base, hat: 'beret', hatColor: shade(robe, 0.62), pattern: 'trim' };
    case 'potion': return { ...base, hat: 'none', hairStyle: 2, pattern: 'floral', accent: '#F28AA0' };
    case 'low': return base;
    case 'mid': return { ...base, accent: '#F28AA0' };
    case 'high': return { ...base, accent: '#D1A24A', trim: '#D1A24A' };
    case 'top': return { ...base, accent: '#D1A24A', trim: '#D1A24A', hat: 'crown' };
  }
}

const eunuch = (key: string, robe: string): Look => ({ key, skin: '#EBC39E', hair: '#1C1B19', robe, trim: '#8C2A1A', hat: 'eunuch', hatColor: '#1C1B19', beard: 'none', hairStyle: 0, pattern: 'belt', accent: '#B3261E' });

export const HAREM: ThemeDef = {
  id: 'harem',
  bg,
  words: HAREM_WORDS,
  layout: {
    ground: 176,
    appX: (n, i) => (n === 1 ? 82 : Math.round(34 + (i * 100) / (n - 1))),
    appKind: 'throne',
    shelf: [180, 176],
    hub: [238, 176], hubItem: [262, 170], bubble: [238, 118],
    perches: [[214, 178], [226, 178], [202, 178], [250, 178]],
    exam: { seat: [290, 176], grader: [304, 176] },
    // 每层的地板线；床在右边屋里
    floors: { top: 70, high: 106, mid: 142, low: 178, paint: 262, potion: 262 },
    band: { x0: 322, x1: 466, h: 36 },
    slotX: 426, slotDir: 'down', idleXs: [340, 358, 376], benchX: 460, bench: [428, 262],
    media: { paintIdle: [190, 262], potionIdle: [56, 262], potionSlot: [76, 262], cauldron: [66, 246], coins: [296, 258], saved: [320, 222], bottles: [352, 250] },
    hud: [240, 8],
    skyH: 40,
  },
  style: {
    courier: 'runner', dispatch: 'runner', reply: 'petal', model: 'person', work: 'bed', hubItem: 'tray', stairsX: 316,
    courierLook: eunuch('harem-runner', '#3A3935'),
    dispatchLook: eunuch('harem-dispatch', '#4A4540'),
    weather: { rain: true, snow: true, fog: false, waves: false },
    colors: { light: '#FFE2A8', ray: '#F0D080', paper: '#FBEFD6', night: 'rgba(22,26,48,.5)' },
  },
  looks: {
    model: lady,
    app: (id, color) => ({ key: `harem-app|${id}|${color}`, ...faceOf(id), hair: '#1C1B19', robe: '#D9A441', trim: color, hat: 'crown', hatColor: '#D1A24A', beard: 'short', pattern: 'dragon', accent: color }),
    hub: { ...eunuch('harem-hub', '#2A2926'), beard: 'none' },
  },
  deco(p: Painter, sc: Scene) {
    const t = sc.t;
    // 敬事房的红灯笼、各宫的床头灯
    lamp(p, t, [[184, 118], [452, 50], [452, 86], [452, 122], [452, 158], [60, 226], [100, 226]], '#FFE2A8');
    // 松针、牡丹在动；远处宫墙上的树
    flutter(p, t, [[8, 30, 60, 40], [118, 100, 36, 40], [40, 108, 22, 20], [128, 148, 24, 20], [280, 40, 40, 30], [320, 56, 50, 30], [320, 92, 50, 30], [320, 128, 50, 30], [320, 164, 50, 24]], ['#9DBE7A', '#7FA058', '#F28AA0']);
    // 花瓣飘落
    for (let i = 0; i < 6; i++) { const q = ((t * 0.25) + i / 6) % 1; p.R(20 + i * 24 + Math.sin(t * 1.5 + i) * 6, 100 + q * 70, 2, 1, i % 2 ? '#F28AA0' : '#FBD3DC'); }
    // 冷宫飘雪
    for (let i = 0; i < 10; i++) { const x = 392 + (i * 7 + Math.floor(t * 6)) % 70, y = 216 + (i * 11 + Math.floor(t * 14)) % 44; p.R(x, y, 1, 1, '#FFFFFF'); }
    if (!sc.night && !sc.rain) sun(p, 100, 14);
  },
};
