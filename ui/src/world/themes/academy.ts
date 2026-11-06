/**
 * 魔法学院：原创的魔法学校，不用哈利波特的名字、人物、学院和校徽；台词里只借几句咒语当梗。
 * 寄信人在湖边的平台上，猫头鹰叼信去大厅交给占卜师；占卜师用魔杖把信送到塔楼里某位巫师手里；
 * 巫师读完举杖，回应顺着光飞回窗口。底下是画室、魔药地窖和金库。
 */
import bg from '../../assets/world/academy-day.png';
import { flutter, lamp, ripples, smoke, sun } from '../deco';
import type { Painter, Scene } from '../scene';
import { lookFor, lookStudent, SEER_LOOK } from '../sprites';
import type { SceneWords, ThemeDef } from '../theme';

const title = { low: '学徒', mid: '导师', high: '教授', top: '校长' } as const;
const titleOf = (k: string) => (title as Record<string, string>)[k] ?? k;

export const ACADEMY_WORDS: SceneWords = {
  floor: { top: '校长室', high: '教授 · 高档', mid: '导师 · 中档', low: '学徒 · 低档', paint: '画室', potion: '魔药地窖' },
  title,
  judge: { low: '学徒就行', mid: '找导师', high: '请教授', top: '请校长', image: '画一幅', video: '熬一锅魔药', animation: '熬一锅魔药' },
  judgeRule: (k) => `规则：${titleOf(k)}`,
  judgeCrystal: (who, k) => `${who || '水晶球'}：请${titleOf(k)}`,
  judgeSticky: (name) => `还找 ${name}`,
  judgeTooThick: (wan) => (wan > 0 ? `约 ${wan} 万，太厚了` : '太厚了'),
  nobody: '没人能接',
  hub: '占卜师',
  painter: '画师', filmer: '魔药师',
  say: {
    toolAsk: (n) => (n > 1 ? `帮我跑 ${n} 个工具` : '帮我跑个工具'),
    toolFetch: '去书架取东西',
    toolBack: '送回工具结果',
    forward: (name) => `转给 ${name}（粘住）`,
    ticket: '取药券', ticketHeld: '取药券在手', ownWork: '回桌前干自己的活',
    failRest: (why) => `${why}，先歇会儿`,
    takeOver: '我来接',
    bounce: '信太厚，原样退回', bounceFail: (why) => `没办成（${why}）`, shorten: '把信写短',
    cooling: '歇会儿 · 冷却中', asleep: '额度用完 · 睡了', exam: '考试中', closed: '停课',
    wait: (s) => `等 ${Math.round(s)} 秒`,
    more: (n) => `还有 ${n} 位`,
    dropHint: (t) => `松手：请${t}重做`, redoing: (t) => `已请${t}重做`, vault: '金库',
  },
  weather: { rain: '下雨：刚有报错', snow: '下雪：订阅额度紧张', clear: '晴：一切正常', night: '夜里：一切正常' },
  empty: { title: '学院里还没有巫师', wizard: '添加模型后，每个型号是一位巫师', tower: '接入应用后，每个应用一座信塔' },
  caption: {
    arrive: (app, floor, who, via) => `${app} 来信，${floor}，交给 ${who}（${via}）`,
    rejudge: (judge, from, to) => `${judge || '判官'}把${from}改判成${to}`,
    sticky: (app, who) => `${app} 的对话粘住 ${who}`,
    toolAsk: (who, app) => `${who} 请 ${app} 在本机跑工具`,
    toolBack: (app, who) => `${app} 送回工具结果，还是 ${who} 接着写`,
    handover: (from, to, why) => `${from} ${why}，同一段对话交给 ${to} 接着写`,
    ticket: (app) => `先回取药券，${app} 不用干等`,
    done: (who, money) => `${who} 交差：${money}`,
    failed: (app, why) => `${app} 这封没办成（${why}）`,
    bounced: (app) => `${app} 的信谁都装不下，退回去让它自己压缩`,
    jobDone: (path) => `魔药熬好了：${path}`,
  },
  banter: ['魔杖又该充魔力了', '猫头鹰今天送了十封信', '校长在塔顶打盹呢', '地窖的魔药又糊了', '占卜师的水晶球该擦了', '楼上教授的胡子又长了'],
  lines: {
    idle: ['荧光闪烁，照着看信', '魔杖也会选错主人', '猫头鹰比人类守时', '魔药课又炸了一锅', '禁书区今天有新活', '飞天扫帚停在塔顶了', '隐身衣下面藏着一只猫'],
    task: ['除你武器，先把活交出来', '这封信得用魔杖拆', '念个咒就好了', '魔杖准备，开工'],
    done: ['咒语生效', '一挥魔杖，交差', '信已送回，魔杖收起'],
    fail: ['咒语念错了', '魔杖没魔力了', '护法咒没撑住'],
    takeover: ['我的魔杖比他的长', '换我来念咒'],
    skipped: ['占卜师偏心', '水晶球没看见我'],
    night: ['夜里的塔楼有点冷', '守夜的猫头鹰也困了'],
    poke: ['别碰我的魔杖', '再戳就把你变成青蛙', '魔杖走火了可不管'],
  },
  hubPoke: ['水晶球说，先别点我', '每封信我都看过才派', '判档靠规则，也靠水晶球', '猫头鹰比你有礼貌'],
};

export const ACADEMY: ThemeDef = {
  id: 'academy',
  bg,
  words: ACADEMY_WORDS,
  layout: {
    ground: 196,
    appX: (n, i) => (n === 1 ? 98 : Math.round(38 + (i * 122) / (n - 1))),
    appKind: 'post',
    shelf: [202, 196],
    hub: [250, 196], hubItem: [276, 196], bubble: [250, 150],
    perches: [[232, 166], [310, 166], [206, 160], [338, 160], [222, 172], [320, 172]],
    exam: { seat: [300, 196], grader: [322, 196] },
    floors: { top: 95, high: 131, mid: 167, low: 202, paint: 262, potion: 262 },
    band: { x0: 372, x1: 466, h: 36 },
    slotX: 384, slotDir: 'left', idleXs: [412, 430, 448], benchX: 400,
    media: { paintIdle: [146, 262], potionIdle: [234, 262], potionSlot: [248, 262], cauldron: [263, 250], coins: [336, 258], saved: [368, 222], bottles: [288, 248] },
    hud: [240, 8],
    skyH: 100,
  },
  style: {
    courier: 'owl', dispatch: 'beam', reply: 'beam', model: 'person', work: 'read', hubItem: 'ball',
    weather: { rain: true, snow: true, fog: false, waves: false },
    colors: { light: '#FFF6D8', ray: '#F0D080', paper: '#FBEFD6', night: 'rgba(22,26,48,.5)' },
  },
  looks: { model: lookFor, app: lookStudent, hub: SEER_LOOK },
  deco(p: Painter, sc: Scene) {
    const t = sc.t;
    // 瀑布：水流里散着亮点，底下溅起水花
    for (let i = 0; i < 14; i++) { const y = 208 + ((i * 11 + Math.floor(t * 14)) % 54); const x = 53 + ((i * 7 + Math.floor(y / 9)) % 10); p.R(x, y, 1, 2, 'rgba(255,255,255,.85)'); }
    for (let i = 0; i < 5; i++) { const q = ((t * 1.2) + i / 5) % 1; p.R(48 + i * 4 + Math.sin(t + i) * 2, 262 - q * 6, 2, 1, 'rgba(255,255,255,.7)'); }
    // 湖面的光
    ripples(p, t, [0, 132, 44, 60], 'rgba(255,255,255,.55)', 8, 4);
    ripples(p, t, [0, 236, 44, 30], 'rgba(255,255,255,.45)', 5, 3);
    // 树叶在翻：左边两棵、右下角一片、湖边灌木
    flutter(p, t, [[18, 100, 60, 52], [88, 118, 40, 40], [438, 200, 40, 68], [0, 228, 24, 40], [140, 126, 36, 30]], ['#F0B35A', '#FFD27A', '#D9742E']);
    // 大厅的烛火、塔楼的灯
    lamp(p, t, [[232, 160], [310, 160], [432, 70], [432, 106], [432, 142], [432, 177], [456, 232]]);
    // 坩埚的烟
    smoke(p, t, 263, 244);
    if (!sc.night && !sc.rain) sun(p, 96, 26);
  },
};
