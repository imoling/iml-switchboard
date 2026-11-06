/**
 * 点将台（默认主题）：河边的大营。各路统领立在自家帐前，传令兵把军令送上点将台交给军师；
 * 军师掷出令箭，被点到的将出营到瞭望楼的栏杆边看令；干完挥旗放信鸽，鸽子一只只飞回统领手里。
 * 底下是画师的帐、影师的皮影戏帐和军需官的帐。
 */
import bg from '../../assets/world/camp-day.png';
import { flutter, lamp, ripples, sun, sway } from '../deco';
import type { Wizard } from '../model';
import type { Painter, Scene } from '../scene';
import { faceOf, LANE_ROBE, type Look, shade } from '../sprites';
import type { SceneWords, ThemeDef } from '../theme';

const title = { low: '偏将', mid: '副将', high: '大将', top: '主帅' } as const;
const titleOf = (k: string) => (title as Record<string, string>)[k] ?? k;

export const CAMP_WORDS: SceneWords = {
  floor: { top: '帅帐', high: '大将 · 高档', mid: '副将 · 中档', low: '偏将 · 低档', paint: '画师帐', potion: '皮影戏帐' },
  title,
  judge: { low: '点偏将', mid: '点副将', high: '点大将', top: '请主帅', image: '请画师', video: '请影师排戏', animation: '请影师排戏' },
  judgeRule: (k) => `按规矩：${titleOf(k)}`,
  judgeCrystal: (who, k) => `${who || '军师'}改点${titleOf(k)}`,
  judgeSticky: (name) => `还交给 ${name}`,
  judgeTooThick: (wan) => (wan > 0 ? `军令约 ${wan} 万，太长` : '军令太长'),
  nobody: '无人可点',
  hub: '军师',
  painter: '画师', filmer: '影师',
  say: {
    toolAsk: (n) => (n > 1 ? `请跑 ${n} 个工具` : '请跑一遍工具'),
    toolFetch: '去军需箱翻东西',
    toolBack: '送回工具结果',
    forward: (name) => `还交给 ${name}（粘住）`,
    ticket: '回执', ticketHeld: '拿着回执', ownWork: '回帐里干自己的活',
    failRest: (why) => `${why}，先下去歇着`,
    takeOver: '末将来接',
    bounce: '军令太长，退回', bounceFail: (why) => `没办成（${why}）`, shorten: '把军令写短',
    cooling: '伤兵营 · 冷却中', asleep: '粮草用完 · 睡了', exam: '校场比试', closed: '收兵',
    wait: (s) => `等 ${Math.round(s)} 秒`,
    more: (n) => `还有 ${n} 员`,
    dropHint: (t) => `松手：请${t}重做`, redoing: (t) => `已请${t}重做`, vault: '军需帐',
  },
  weather: { rain: '下雨：刚有报错', snow: '下雪：粮草（订阅额度）紧张', clear: '晴：一切正常', night: '夜里：一切正常' },
  empty: { title: '大营里还没有将', wizard: '添加模型后，每个型号是一员将', tower: '接入应用后，每个应用立一座帐' },
  caption: {
    arrive: (app, floor, who, via) => `${app} 来令，${floor}，点 ${who}（${via}）`,
    rejudge: (judge, from, to) => `${judge || '军师'}把${from}改点成${to}`,
    sticky: (app, who) => `${app} 的对话粘住 ${who}`,
    toolAsk: (who, app) => `${who} 请 ${app} 在本机跑工具`,
    toolBack: (app, who) => `${app} 送回工具结果，还是 ${who} 接着办`,
    handover: (from, to, why) => `${from} ${why}，同一段对话交给 ${to} 接着办`,
    ticket: (app) => `先回回执，${app} 不用干等`,
    done: (who, money) => `${who} 交令：${money}`,
    failed: (app, why) => `${app} 这道令没办成（${why}）`,
    bounced: (app) => `${app} 的军令谁都接不下，退回去让它自己压缩`,
    jobDone: (path) => `皮影戏排好了：${path}`,
  },
  banter: ['军师今天掷了八支令箭', '信鸽飞累了，歇会儿', '主帅在帐里睡得真香', '伤兵营今天没人，好事', '影师的皮影又掉了一个', '军需帐的银子又多了'],
  lines: {
    idle: ['末将愿往', '军师妙计安天下', '粮草先行，活后到', '这仗打完就回家', '令箭比将还多', '城楼上的风真大', '鼓一响就得上'],
    task: ['末将领命', '得令，这就去', '此活吾来办', '擂鼓，出发'],
    done: ['大捷', '交令', '幸不辱命', '此役已毕'],
    fail: ['中了埋伏', '马失前蹄', '粮道断了'],
    takeover: ['末将顶上', '换我上阵'],
    skipped: ['为何不点我', '末将也能打', '军师看不见我'],
    night: ['夜袭又来了', '守夜的也想睡'],
    poke: ['军中无戏言', '别戳盔甲', '再戳就军法处置'],
  },
  hubPoke: ['令箭不是随便掷的', '点将看的是活，不是脸', '别戳，军令都看过才点', '传令兵比你跑得快'],
};

function soldier(w: Wizard): Look {
  const face = faceOf(w.id);
  const robe = LANE_ROBE[w.lane];
  const base: Look = { key: `camp|${w.id}|${w.lane}|${w.floor}`, ...face, robe, trim: '#E6D3B0', hat: 'helmet', hatColor: '#9A968C', beard: 'none', pattern: 'belt', accent: '#B3261E' };
  switch (w.floor) {
    case 'paint': return { ...base, hat: 'beret', hatColor: shade(robe, 0.62), pattern: 'trim' };
    case 'potion': return { ...base, hat: 'cap', hatColor: '#3A3330', pattern: 'trim' };
    case 'low': return base;
    case 'mid': return { ...base, hat: 'plume', pattern: 'armor' };
    case 'high': return { ...base, hat: 'plume', pattern: 'armor', beard: 'short', accent: '#D1A24A' };
    case 'top': return { ...base, hat: 'crown', pattern: 'armor', beard: 'long', hair: '#E8E4DC', trim: '#D1A24A' };
  }
}

export const CAMP: ThemeDef = {
  id: 'camp',
  bg,
  words: CAMP_WORDS,
  layout: {
    ground: 176,
    appX: (n, i) => (n === 1 ? 84 : Math.round(34 + (i * 104) / (n - 1))),
    appKind: 'tent',
    shelf: [156, 178],
    hub: [236, 148], hubItem: [280, 148], bubble: [236, 100],
    perches: [[216, 150], [256, 150], [204, 150], [268, 150]],
    exam: { seat: [300, 148], grader: [322, 148] },
    floors: { top: 71, high: 108, mid: 146, low: 184, paint: 262, potion: 262 },
    band: { x0: 372, x1: 466, h: 36 },
    slotX: 386, slotDir: 'left', idleXs: [418, 436, 454], benchX: 404,
    media: { paintIdle: [56, 262], potionIdle: [186, 262], potionSlot: [204, 262], cauldron: [228, 248], coins: [392, 258], saved: [400, 222], bottles: [322, 250] },
    hud: [240, 8],
    skyH: 60,
  },
  style: {
    courier: 'runner', dispatch: 'arrow', reply: 'pigeon', model: 'person', work: 'read', hubItem: 'map',
    courierLook: { key: 'camp-runner', skin: '#EBC39E', hair: '#3A2E26', robe: '#8C8A84', trim: '#E6D3B0', hat: 'helmet', hatColor: '#9A968C', beard: 'none', hairStyle: 0, pattern: 'belt' },
    weather: { rain: true, snow: true, fog: false, waves: false },
    colors: { light: '#FFF6D8', ray: '#F0D080', paper: '#F3E6C8', night: 'rgba(22,26,48,.5)' },
  },
  looks: {
    model: soldier,
    app: (id, color) => ({ key: `camp-app|${id}|${color}`, ...faceOf(id), robe: color, trim: '#E6D3B0', hat: 'plume', hatColor: '#9A968C', beard: 'none', pattern: 'armor', accent: color }),
    hub: { key: 'camp-hub', skin: '#EBC39E', hair: '#3A2E26', robe: '#EFE7D6', trim: '#8C6239', hat: 'none', hatColor: '#EFE7D6', beard: 'short', hairStyle: 1, pattern: 'belt' },
  },
  deco(p: Painter, sc: Scene) {
    const t = sc.t;
    // 帅旗飘
    for (let i = 0; i < 8; i++) { const y = 26 + i * 5; const dx = Math.round(Math.sin(t * 4 + i * 0.8) * 2); p.R(192 + dx + i * 2, y, 4, 1, i % 2 ? '#C4613A' : '#B04A2A'); }
    for (let i = 0; i < 4; i++) { const y = 10 + i * 3; p.R(418 + Math.round(Math.sin(t * 4 + i) * 1.5) + i, y, 3, 1, '#C4613A'); }
    // 河面的光
    ripples(p, t, [0, 94, 150, 30], 'rgba(255,255,255,.6)', 10, 6);
    ripples(p, t, [300, 94, 70, 28], 'rgba(255,255,255,.5)', 5, 5);
    // 柳条随风、树叶翻
    sway(p, t, [[18, 118, 40], [30, 120, 44], [44, 116, 36], [122, 120, 40], [134, 118, 44], [146, 116, 34]], '#9DBE7A');
    flutter(p, t, [[0, 60, 70, 60], [100, 70, 60, 50], [300, 120, 60, 40], [460, 120, 20, 50]], ['#C9DF9A', '#9DBE7A', '#7FA058']);
    // 瞭望楼的灯笼、帐里的灯
    lamp(p, t, [[432, 52], [432, 89], [432, 127], [432, 165], [262, 222], [102, 222]]);
    if (!sc.night && !sc.rain) sun(p, 70, 18);
  },
};
