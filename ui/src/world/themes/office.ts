/**
 * 写字楼（标准主题，投屏时用，不玩梗）：客户在门口的小桌前，有事自己走到前台递信封，再回座位等；
 * 前台把信封放进电梯送到对应楼层，员工在自己工位上敲键盘；回复折成纸飞机飞回客户手里。
 * 地下是设计工作室、剪辑室和财务室。
 */
import bg from '../../assets/world/office-day.png';
import { flutter, lamp, screen, sun } from '../deco';
import type { Wizard } from '../model';
import type { Painter, Scene } from '../scene';
import { faceOf, LANE_ROBE, type Look, shade } from '../sprites';
import type { SceneWords, ThemeDef } from '../theme';

const title = { low: '低档', mid: '中档', high: '高档', top: '顶配' } as const;
const titleOf = (k: string) => (title as Record<string, string>)[k] ?? k;

export const OFFICE_WORDS: SceneWords = {
  floor: { top: '顶配', high: '高档', mid: '中档', low: '低档', paint: '设计室', potion: '剪辑室' },
  title,
  judge: { low: '低档', mid: '中档', high: '高档', top: '顶配', image: '出图', video: '出视频', animation: '做动画' },
  judgeRule: (k) => `规则：${titleOf(k)}`,
  judgeCrystal: (who, k) => `${who || '判官'}：${titleOf(k)}`,
  judgeSticky: (name) => `还是 ${name}`,
  judgeTooThick: (wan) => (wan > 0 ? `约 ${wan} 万，装不下` : '装不下'),
  nobody: '没人能接',
  hub: '前台',
  painter: '设计', filmer: '剪辑',
  say: {
    toolAsk: (n) => (n > 1 ? `需要跑 ${n} 个工具` : '需要跑个工具'),
    toolFetch: '去档案柜取文件',
    toolBack: '送回工具结果',
    forward: (name) => `转给 ${name}（同一会话）`,
    ticket: '任务号', ticketHeld: '拿着任务号', ownWork: '回座位忙自己的',
    failRest: (why) => `${why}，换人`,
    takeOver: '我来接',
    bounce: '太长，退回', bounceFail: (why) => `没办成（${why}）`, shorten: '压缩上下文',
    cooling: '茶水间 · 冷却中', asleep: '额度用完 · 休息', exam: '评估中', closed: '暂停调度',
    wait: (s) => `等 ${Math.round(s)} 秒`,
    more: (n) => `还有 ${n} 位`,
    dropHint: (t) => `松手：请${t}重做`, redoing: (t) => `已请${t}重做`, vault: '财务室',
  },
  weather: { rain: '下雨：刚有报错', snow: '下雪：订阅额度紧张', clear: '晴：一切正常', night: '夜里：一切正常' },
  empty: { title: '楼里还没有模型', wizard: '添加模型后，每个型号是一位员工', tower: '接入应用后，每个应用一张小桌' },
  caption: {
    arrive: (app, floor, who, via) => `${app} 来活，${floor}，交给 ${who}（${via}）`,
    rejudge: (judge, from, to) => `${judge || '判官'}把${from}改判成${to}`,
    sticky: (app, who) => `${app} 的对话粘住 ${who}`,
    toolAsk: (who, app) => `${who} 请 ${app} 在本机跑工具`,
    toolBack: (app, who) => `${app} 送回工具结果，还是 ${who} 接着写`,
    handover: (from, to, why) => `${from} ${why}，同一段对话交给 ${to} 接着写`,
    ticket: (app) => `先回任务号，${app} 不用干等`,
    done: (who, money) => `${who} 交付：${money}`,
    failed: (app, why) => `${app} 这单没办成（${why}）`,
    bounced: (app) => `${app} 的对话谁都装不下，退回去让它自己压缩`,
    jobDone: (path) => `视频剪好了：${path}`,
  },
  banter: ['前台的咖啡机又坏了', '电梯今天跑了二十趟', '老板在顶楼睡着了', '剪辑室的渲染还没完', '财务室今天进账不少', '楼下设计又换了配色'],
  lines: {
    idle: ['这个需求排期下周', '对齐一下再说', '周报写了吗', '老板在顶楼开会', '电梯等了三趟', '茶水间没咖啡了', '站会开了四十分钟'],
    task: ['收到，马上对齐', '先拉个群', '这个我熟', '排进今天的待办'],
    done: ['交付了，记得验收', '发你邮箱了', '闭环了'],
    fail: ['系统卡了', '这个我没权限', '网关又超时'],
    takeover: ['我来兜底', '交接一下，我接着做'],
    skipped: ['这单不给我？', '我也能排', '我的排期是空的'],
    night: ['加班餐吃了吗', '打车能报销吗'],
    poke: ['在忙，稍后回', '工位别乱戳', '有事发消息'],
  },
  hubPoke: ['请稍等，正在分派', '每一单都按规则来', '别点我，点工作台', '电梯马上到'],
};

function worker(w: Wizard): Look {
  const face = faceOf(w.id);
  const robe = LANE_ROBE[w.lane];
  const base: Look = { key: `office|${w.id}|${w.lane}|${w.floor}`, ...face, robe, trim: '#F3EEE4', hat: 'none', hatColor: robe, beard: 'none', pattern: 'none' };
  switch (w.floor) {
    case 'paint': return { ...base, hat: 'beret', hatColor: shade(robe, 0.62), pattern: 'trim' };
    case 'potion': return { ...base, hat: 'cap', hatColor: '#3A3330', pattern: 'trim' };
    case 'low': return base;
    case 'mid': return { ...base, pattern: 'trim' };
    case 'high': return { ...base, pattern: 'tie', trim: '#2A2926', face: 'glasses' };
    case 'top': return { ...base, pattern: 'tie', trim: '#8C2A1A', face: 'glasses', hair: '#D8D3C8', hairStyle: 1 };
  }
}

export const OFFICE: ThemeDef = {
  id: 'office',
  bg,
  words: OFFICE_WORDS,
  layout: {
    ground: 192,
    appX: (n, i) => (n === 1 ? 62 : Math.round(26 + (i * 76) / (n - 1))),
    appKind: 'desk',
    shelf: [146, 192],
    hub: [214, 186], hubItem: [240, 176], bubble: [214, 122],
    perches: [[198, 196], [232, 196], [186, 196], [246, 196]],
    exam: { seat: [276, 192], grader: [292, 192] },
    // 工位上的椅子：脚踩在每层的地板线上
    floors: { top: 67, high: 112, mid: 156, low: 198, paint: 262, potion: 262 },
    band: { x0: 300, x1: 466, h: 42 },
    slotX: 395, slotDir: 'down', idleXs: [395, 421, 447], benchX: 340, workAtDesk: true,
    media: { paintIdle: [96, 262], potionIdle: [214, 262], potionSlot: [236, 262], cauldron: [240, 236], coins: [336, 258], saved: [400, 222], bottles: [338, 240] },
    hud: [240, 8],
    skyH: 90,
  },
  style: {
    courier: 'self', dispatch: 'elevator', reply: 'plane', model: 'person', work: 'type', hubItem: 'screen', elevatorX: 294,
    weather: { rain: true, snow: true, fog: false, waves: false },
    colors: { light: '#FFFFFF', ray: '#9DB3C2', paper: '#F3F1EC', night: 'rgba(22,26,48,.5)' },
  },
  looks: {
    model: worker,
    app: (id, color) => ({ key: `office-app|${id}|${color}`, ...faceOf(id), robe: color, trim: '#F3EEE4', hat: 'cap', hatColor: shade(color, 0.7), beard: 'none', pattern: 'trim' }),
    hub: { key: 'office-hub', skin: '#EBC39E', hair: '#3A2E26', robe: '#5F6B4A', trim: '#F3EEE4', hat: 'none', hatColor: '#5F6B4A', beard: 'none', hairStyle: 1, pattern: 'tie' },
  },
  deco(p: Painter, sc: Scene) {
    const t = sc.t;
    // 电梯井：玻璃柱子和缆绳
    p.alpha(0.35); p.R(289, 28, 11, 170, '#D6ECF5'); p.alpha(1);
    p.R(289, 28, 1, 170, '#9DB3C2'); p.R(299, 28, 1, 170, '#9DB3C2'); p.R(294, 28, 1, 170, '#6F6A61');
    for (const y of [67, 112, 156, 198]) p.R(289, y, 11, 1, '#9DB3C2');
    // 大堂屏幕、工位显示器、剪辑室和设计室的屏都在变
    const mons: [number, number, number, number][] = [[194, 132, 48, 24], [214, 224, 52, 14], [86, 244, 10, 5], [102, 244, 10, 5]];
    for (const fy of [67, 112, 156, 198]) for (const x of [383, 409, 435]) mons.push([x, fy - 15, 8, 5]);
    screen(p, t, mons);
    // 顶楼和各层的吊灯、大堂的灯
    lamp(p, t, [[200, 118], [236, 118], [272, 118], [320, 31], [320, 76], [320, 120], [320, 164], [420, 31], [420, 76], [420, 120], [420, 164], [440, 212], [330, 212]], '#FFF6D8');
    // 阳台和广场的植物、树叶
    flutter(p, t, [[12, 100, 32, 60], [60, 110, 50, 60], [300, 50, 70, 20], [300, 95, 70, 20], [300, 140, 70, 20], [300, 182, 70, 16], [340, 130, 40, 40]], ['#C9DF9A', '#9DBE7A', '#F0B35A']);
    // 饮水机冒泡
    for (let i = 0; i < 3; i++) { const q = ((t * 0.7) + i / 3) % 1; p.R(166 + i * 2, 176 - q * 10, 1, 1, 'rgba(255,255,255,.8)'); }
    if (!sc.night && !sc.rain) sun(p, 60, 22);
  },
};
