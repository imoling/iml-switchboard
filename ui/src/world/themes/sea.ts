/**
 * 海王（彩蛋）：Switchboard 就是海王。码头上的小弟各拿一部手机，头顶各有一只海鸥；
 * 有事海鸥叼着消息飞到海王的躺椅上，海王判档后扔一个漂流瓶，被选中的鱼从深处游到水面接住，脸红冒爱心；
 * 回话是一串泡泡冒上来，飞进小弟的手机。水越深档越高：浅水备胎、中层暧昧、深水正宫，海底的真爱只在点名时醒。
 */
import bg from '../../assets/world/sea-day.png';
import { flutter, lamp, screen, sway } from '../deco';
import type { Painter, Scene } from '../scene';
import { faceOf, fishLookFor, type Look } from '../sprites';
import type { SceneWords, ThemeDef } from '../theme';

const title = { low: '备胎', mid: '暧昧', high: '正宫', top: '真爱' } as const;
const titleOf = (k: string) => (title as Record<string, string>)[k] ?? k;

export const SEA_WORDS: SceneWords = {
  floor: { top: '海底 · 真爱', high: '深水 · 正宫', mid: '中层 · 暧昧', low: '浅水 · 备胎', paint: '自拍石', potion: '地笼' },
  title,
  judge: { low: '捞条备胎', mid: '找个暧昧', high: '得正宫出马', top: '叫真爱', image: '来张自拍', video: '下地笼拍短视频', animation: '下地笼拍短视频' },
  judgeRule: (k) => `先想找${titleOf(k)}`,
  judgeCrystal: (who, k) => `${who || '海王'}改主意：叫${titleOf(k)}`,
  judgeSticky: (name) => `还是那条 ${name}`,
  judgeTooThick: (wan) => (wan > 0 ? `约 ${wan} 万，哪条都吞不下` : '哪条都吞不下'),
  nobody: '没鱼咬钩',
  hub: '海王',
  painter: '自拍', filmer: '短视频',
  say: {
    toolAsk: (n) => (n > 1 ? `在吗？帮我跑 ${n} 个工具` : '在吗？帮我跑个工具'),
    toolFetch: '翻包找东西',
    toolBack: '送回工具结果',
    forward: (name) => `还是那条 ${name}`,
    ticket: '浮标', ticketHeld: '地笼下着', ownWork: '先去忙别的',
    failRest: (why) => `已读不回（${why}）`,
    takeOver: '无缝衔接',
    bounce: '话太多，扔回去', bounceFail: (why) => `没办成（${why}）`, shorten: '把话说短点',
    cooling: '礁石后 · 冷却中', asleep: '鱼饵用完 · 睡了', exam: '海选', closed: '上岸',
    wait: (s) => `等回复 ${Math.round(s)} 秒`,
    more: (n) => `还有 ${n} 条`,
    dropHint: (t) => `松手：请${t}重做`, redoing: (t) => `已请${t}重做`, vault: '宝箱',
  },
  weather: { rain: '起风浪：刚有报错', snow: '起雾：鱼饵（订阅额度）紧张', clear: '晴：一切正常', night: '夜钓：一切正常' },
  empty: { title: '海里还没有鱼', wizard: '添加模型后，每个型号是一条鱼', tower: '接入应用后，每个应用一把躺椅' },
  caption: {
    arrive: (app, floor, who, via) => `${app} 的消息来了，${floor}，甩给 ${who}（${via}）`,
    rejudge: (judge, from, to) => `${judge || '海王'}把${from}改成${to}`,
    sticky: (app, who) => `${app} 的对话粘住 ${who}`,
    toolAsk: (who, app) => `${who} 请 ${app} 在本机跑工具`,
    toolBack: (app, who) => `${app} 送回工具结果，还是 ${who} 接着聊`,
    handover: (from, to, why) => `${from} ${why}，同一段对话 ${to} 无缝衔接`,
    ticket: (app) => `先插浮标，${app} 不用干等`,
    done: (who, money) => `${who} 回完了：${money}`,
    failed: (app, why) => `${app} 这条没回上（${why}）`,
    bounced: (app) => `${app} 的话太多，哪条鱼都吞不下，扔回去`,
    jobDone: (path) => `短视频拍好了：${path}`,
  },
  banter: ['今天海水有点凉', '海鸥嘴里的消息又湿了', '海底那条金的在装睡', '地笼里的螃蟹又在拍', '自拍石的灯该换了', '海王今天躺了一整天'],
  lines: {
    idle: ['在吗', '宝贝，今天好忙', '鱼塘今天水温刚好', '海鸥又叼错消息了', '金鱼在海底装睡', '时间管理第一课：分派', '他昨天也这么说'],
    task: ['来了宝贝', '这条我接', '甩给我就对了', '等你好久了'],
    done: ['回完了，下一条', '已读已回', '宝贝晚安'],
    fail: ['信号不好', '手机没电了', '刚才在开会'],
    takeover: ['我来续上', '接着聊，我在'],
    skipped: ['他不爱我了', '怎么不甩给我', '我也是备胎啊'],
    night: ['夜钓的人最多情', '这么晚还在找鱼'],
    poke: ['别晃，水凉', '宝贝别闹', '再戳我就潜下去了'],
  },
  hubPoke: ['别晃躺椅', '每条消息我都看过才甩瓶子', '时间管理，靠的是分派', '海鸥比你有礼貌'],
};

const guy = (id: string, color: string): Look => ({ key: `sea-app|${id}|${color}`, ...faceOf(id), robe: color, trim: '#F3EEE4', hat: 'straw', hatColor: '#D9B86A', beard: 'none', pattern: 'floral', accent: '#FFFFFF' });

export const SEA: ThemeDef = {
  id: 'sea',
  bg,
  words: SEA_WORDS,
  layout: {
    ground: 100,
    appX: (n, i) => (n === 1 ? 80 : Math.round(30 + (i * 104) / (n - 1))),
    appKind: 'chair',
    shelf: [232, 100],
    hub: [198, 100], hubItem: [268, 98], bubble: [198, 48],
    perches: [[162, 36], [232, 30], [200, 26], [262, 36]],
    exam: { seat: [272, 100], grader: [286, 100] },
    floors: { top: 240, high: 188, mid: 152, low: 118, paint: 214, potion: 236 },
    band: { x0: 300, x1: 470, h: 34 },
    slotX: 318, slotDir: 'left', idleXs: [352, 394, 436], benchX: 458,
    media: { paintIdle: [60, 214], potionIdle: [150, 236], potionSlot: [150, 236], cauldron: [150, 222], coins: [214, 256], saved: [232, 212], bottles: [10, 96] },
    hud: [240, 8],
    skyH: 60,
  },
  style: {
    courier: 'gull', dispatch: 'bottle', reply: 'bubble', model: 'fish', work: 'swim', hubItem: 'radio',
    weather: { rain: true, snow: false, fog: true, waves: true },
    colors: { light: '#FFFFFF', ray: '#D6ECF5', paper: '#FBEFD6', night: 'rgba(12,20,48,.55)' },
  },
  looks: {
    model: (w) => guy(w.id, '#5F7A4A'),
    fish: fishLookFor,
    app: guy,
    hub: { key: 'sea-hub', skin: '#D9A27A', hair: '#3A2E26', robe: '#E07A2E', trim: '#F3EEE4', hat: 'none', hatColor: '#E07A2E', beard: 'none', hairStyle: 2, pattern: 'floral', face: 'shades', accent: '#FFFFFF' },
  },
  deco(p: Painter, sc: Scene) {
    const t = sc.t;
    // 海面的浪花
    for (let i = 0; i < 16; i++) { const x = (i * 31 + t * 14) % 180 + 300; p.R(x, 101 + Math.round(Math.sin(x / 9 + t * 3)), 4, 1, 'rgba(255,255,255,.8)'); }
    for (let i = 0; i < 6; i++) { const x = (i * 47 + t * 10) % 170 + 300; p.R(x, 70 + (i % 3) * 8, 3, 1, 'rgba(255,255,255,.6)'); }
    // 水里的光柱
    p.alpha(0.12); for (let i = 0; i < 4; i++) { const x = 120 + i * 60 + Math.round(Math.sin(t * 0.7 + i) * 4); p.R(x, 104, 10, 60, '#FFFFFF'); } p.alpha(1);
    // 水草、珊瑚摆
    sway(p, t, [[30, 268, 30], [100, 268, 22], [260, 266, 30], [330, 268, 26], [430, 266, 30], [460, 268, 22], [318, 138, 14], [462, 140, 14]], '#5F7A4A');
    flutter(p, t, [[0, 150, 40, 50], [280, 230, 60, 40], [300, 120, 40, 20], [440, 110, 40, 30], [440, 180, 40, 30]], ['#F0B35A', '#E07A2E', '#9DBE7A']);
    // 小屋的灯、小弟的手机屏、自拍石的环灯
    lamp(p, t, [[256, 50]], '#FFF2B0');
    screen(p, t, [[216, 80, 10, 5]]);
    p.alpha(0.5 + 0.3 * Math.sin(t * 5)); p.R(52, 202, 6, 1, '#FFFFFF'); p.R(50, 204, 1, 4, '#FFFFFF'); p.R(59, 204, 1, 4, '#FFFFFF'); p.alpha(1);
    // 码头桩子边的泡泡
    for (let i = 0; i < 6; i++) { const q = (t * 0.5 + i / 6) % 1; p.R(28 + i * 44, 200 - q * 80, 1, 1, 'rgba(255,255,255,.7)'); }
  },
};
