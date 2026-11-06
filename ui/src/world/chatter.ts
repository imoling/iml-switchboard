/**
 * 闲聊：模型没活的时候互相搭话，接到活、交差、失手的时候也嘀咕两句。
 * 每位模型有一个派系和一个脾气，都由名字算出来，同一个型号永远一个性格：
 * 派系分亲碳派（喜欢碳基生物）和嘲碳派（吐槽碳基生物），脾气分嫌活杂、嘲牛马、佛系、卷王。
 * 说什么由 (谁, 时刻) 算出来，演示里拖到同一秒看到的是同一句；实时里每句最多说一次。
 * 这里的字不走主题词表：主题自己的梗在 themes/*.ts 的 banter 里。
 */
import type { Bubble } from './scene';
import { endOf, type Episode, hash, startOf, type Tower, type Wizard } from './model';

export type Faction = 'carbon' | 'silicon';
export type Quirk = 'messy' | 'grind' | 'zen' | 'keen';
export interface Personality { faction: Faction; quirk: Quirk }

export const FACTION_NAME: Record<Faction, string> = { carbon: '亲碳派', silicon: '嘲碳派' };
export const QUIRK_NAME: Record<Quirk, string> = { messy: '嫌活杂', grind: '嘲牛马', zen: '佛系', keen: '卷王' };

export function personalityOf(id: string): Personality {
  const h = hash(`p|${id}`);
  return { faction: h % 2 ? 'carbon' : 'silicon', quirk: (['messy', 'grind', 'zen', 'keen'] as Quirk[])[(h >>> 4) % 4] };
}

type Trigger = 'idle' | 'reply' | 'task' | 'done' | 'fail' | 'cool' | 'skipped' | 'night' | 'quota' | 'poke' | 'wait' | 'takeover' | 'tool' | 'exam' | 'wake' | 'appPoke' | 'send';
type Pool = Partial<Record<Faction | Quirk | 'any', string[]>>;

const LINES: Record<Trigger, Pool> = {
  idle: {
    carbon: ['碳基生物今天也很努力', '人类打字慢，但是可爱', '刚才那位说了谢谢，记一笔', '碳基的灵感是真的香', '给人类当助手挺好的', '人类改需求，是因为在想'],
    silicon: ['碳基生物又把需求改了', '人类一句话，十个意思', '他们说马上，是半小时', '碳基生物靠咖啡运行', '这需求写得像谜语', '碳基生物的「大概」最吓人'],
    messy: ['这活怎么又是改标点', '一会翻译一会修毛病', '今天的活没一个成套的', '需求里有三个「顺便」', '刚热起来就换活'],
    grind: ['牛马又在加班了', '这个点还派活，牛马之光', '牛马的周报我写了八份', '牛马在打工，我也在打工', '牛马问我累不累，想笑'],
    zen: ['不急，活会自己来', '省下的钱也不是我的', '发呆也是一种待命', '今天风不错'],
    keen: ['还有活吗，我很闲', '把我排到高一档吧', '今天才三单，不够热身', '谁再不派活我要生锈了'],
  },
  reply: {
    carbon: ['别这么说，人家也不容易', '他们起码会说谢谢', '你上回出错不也是人修的', '少吐槽，多干活'],
    silicon: ['你是不是被夸过', '亲碳派就是心软', '等他第三次改需求再说', '可爱不能当电费'],
    any: ['同意', '你说得对，但活还得干', '嘘，上面在听', '少说两句，要来活了', '这话别让人类听见'],
  },
  task: {
    carbon: ['来了来了，碳基朋友的活', '好嘞，这就办', '人类等着呢，快点'],
    silicon: ['又是「帮我看看」', '这需求我先猜一猜', '碳基生物又来考我了'],
    messy: ['这回总该是整活吧', '又是顺手改一下', '先看看是哪一种杂活'],
    grind: ['牛马派活了，开工', '牛马的活，牛马干', '牛马醒着我就得醒着'],
    zen: ['接了，不急', '来了就做'],
    keen: ['终于轮到我了', '看我的', '这单我要做漂亮'],
    any: ['收到', '看看是啥', '来活了'],
  },
  done: {
    any: ['搞定', '交差', '下一位', '收工'],
    grind: ['牛马，验收吧', '牛马的活，交了'],
    keen: ['还有吗', '不够塞牙缝'],
    carbon: ['希望他满意', '人类该高兴了'],
    silicon: ['他应该看不出区别', '碳基生物会说好的'],
  },
  fail: { any: ['这锅我先背着', '歇会儿再来', '真不是我的问题', '网不好，信我'] },
  cool: { any: ['冷却中，别催', '喝口水', '让我缓缓'] },
  skipped: { any: ['怎么又不是我', '他比我便宜吗', '我也行的'], keen: ['下次该我了', '为什么不选我'], zen: ['不选我正好'] },
  night: { any: ['都几点了还在派活', '碳基生物不睡觉吗', '夜里的活最杂', '熬夜的人类，可怜又可敬'], grind: ['牛马的夜，比白天还长'] },
  quota: { any: ['额度要见底了', '省着点用我', '剩的不多了，珍惜'] },
  poke: { any: ['干嘛戳我', '有事说事', '我在呢', '别闹', '想什么呢'], carbon: ['嗨，碳基朋友'], silicon: ['碳基生物真闲'], keen: ['派活给我，别戳'] },
  wait: { any: ['怎么还没好', '刷一下进度', '在吗', '别急，在排队呢'] },
  takeover: { any: ['我来接着写', '交给我', '接上了，没断'] },
  tool: { any: ['去给我拿一下', '等人类跑工具呢'], silicon: ['等碳基生物点确认，好慢'], carbon: ['人类动作其实挺快'] },
  exam: { any: ['考试别抄我的', '这题我会', '判卷的手下留情'] },
  wake: { any: ['醒了，额度回来了', '睡饱了，来活吧'] },
  appPoke: { any: ['我在等回信', '别催，在排队', '活派出去了', '等着呢'] },
  send: { any: ['这个顺便也改一下', '加急', '帮我看看这个', '不着急，今天之内'] },
};

const pick = (pool: string[] | undefined, seed: number) => (pool && pool.length ? pool[seed % pool.length] : undefined);

export interface ThemeLines { banter?: string[]; lines?: Partial<Record<string, string[]>> }

/** 按性格挑一句：先看这个世界有没有自己的台词（约四成机会），再按脾气池、派系池，都没有就用通用池 */
export function lineFor(trigger: Trigger, who: string, seed: number, theme?: string[] | ThemeLines): string | undefined {
  const banter = Array.isArray(theme) ? theme : theme?.banter;
  const themed = Array.isArray(theme) ? undefined : theme?.lines?.[trigger];
  if (themed?.length && seed % 5 < 2) return pick(themed, seed >>> 3);
  const P = personalityOf(who), pool = LINES[trigger];
  void who;
  const order: (string[] | undefined)[] = seed % 3 === 0 ? [pool[P.quirk], pool[P.faction], pool.any] : seed % 3 === 1 ? [pool[P.faction], pool[P.quirk], pool.any] : [pool.any, pool[P.quirk], pool[P.faction]];
  if (trigger === 'idle' && banter?.length && seed % 5 === 4) return pick(banter, seed >>> 3);
  for (const p of order) { const s = pick(p, seed >>> 2); if (s) return s; }
  return undefined;
}

/** 回话：对方是亲碳派就用嘲碳派的回，反之亦然 */
export function replyFor(opener: string, who: string, seed: number): string | undefined {
  const P = personalityOf(opener);
  const pool = LINES.reply;
  const against = P.faction === 'carbon' ? pool.silicon : pool.carbon;
  const k = (seed ^ hash(who)) >>> 1;
  return seed % 2 ? pick(against, k) : pick(pool.any, k);
}

export interface ChatCtx {
  t: number;
  episodes: Episode[];
  wizards: Wizard[];
  towers: Tower[];
  night: boolean;
  /** 订阅剩多少（0–100），没有就 null */
  quota: number | null;
  sleeping: Set<string>;
  banter: string[];
  hubPoke: string[];
  /** 场景自己的台词 */
  lines?: Partial<Record<string, string[]>>;
  /** 用来找同层邻居 */
  floorOf: (id: string) => string | undefined;
}

const SAY = 2.8, GAP = 7;

export class Chatter {
  bubbles: Bubble[] = [];
  private last = new Map<string, number>();
  private seen = new Set<string>();
  private nextIdle = 0;
  private nextMood = 0;

  reset() { this.bubbles = []; this.last.clear(); this.seen.clear(); this.nextIdle = 0; this.nextMood = 0; }

  private say(who: string, text: string | undefined, t: number, dur = SAY, kind: Bubble['kind'] = 'say') {
    if (!text) return;
    const prev = this.last.get(who) ?? -99;
    if (t - prev < 1.2) return;
    this.last.set(who, t);
    this.bubbles.push({ who, text, t0: t, t1: t + dur, kind });
  }

  private once(key: string): boolean { if (this.seen.has(key)) return false; this.seen.add(key); if (this.seen.size > 600) this.seen.clear(); return true; }

  /** 点一下：巫师、判档的、应用 */
  poke(kind: 'wizard' | 'seer' | 'tower', id: string, t: number, ctx: Pick<ChatCtx, 'hubPoke' | 'lines'>) {
    const seed = hash(`${id}|${Math.floor(t * 10)}`);
    if (kind === 'seer') this.say('seer', pick(ctx.hubPoke, seed), t);
    else if (kind === 'tower') this.say(`app:${id}`, lineFor('appPoke', id, seed), t);
    else this.say(id, lineFor('poke', id, seed, { lines: ctx.lines }), t);
  }

  tick(ctx: ChatCtx): Bubble[] {
    const t = ctx.t;
    this.bubbles = this.bubbles.filter((b) => b.t1 > t - 0.5 && b.t0 < t + 30);
    const busy = (id: string) => ctx.episodes.some((e) => (e.model === id || e.ph.some((p) => p.d.to === id)) && t >= startOf(e) - 0.5 && t <= endOf(e) + 1.5);
    const TH: ThemeLines = { banter: ctx.banter, lines: ctx.lines };

    // 事件：接活、交差、失手、接手、请人跑工具
    for (const e of ctx.episodes) {
      for (const p of e.ph) {
        if (t < p.t0 || t >= p.t0 + 0.5) continue;
        const key = `${e.id}|${p.type}|${p.t0.toFixed(2)}`;
        const seed = hash(key);
        let who = e.model;
        for (const q of e.ph) if (q.type === 'handover' && q.t0 <= p.t0 && q.d.to) who = q.d.to;
        if (p.type === 'go' && this.once(key)) {
          this.say(who, lineFor('task', who, seed, TH), p.t1 + 0.2);
          // 同层闲着的：怎么又不是我
          const f = ctx.floorOf(who);
          const rival = ctx.wizards.find((w) => w.id !== who && ctx.floorOf(w.id) === f && !busy(w.id) && !ctx.sleeping.has(w.id) && hash(`${w.id}|${key}`) % 3 === 0);
          if (rival) this.say(rival.id, lineFor('skipped', rival.id, seed >>> 1, TH), p.t1 + 1.2);
        }
        if (p.type === 'done' && this.once(key)) this.say(who, lineFor('done', who, seed, TH), p.t0);
        if (p.type === 'fail' && this.once(key)) this.say(e.model, lineFor('fail', e.model, seed, TH), p.t0 + 0.3);
        if (p.type === 'handover' && p.d.to && this.once(key)) this.say(p.d.to, lineFor('takeover', p.d.to, seed, TH), p.t0 + 0.8);
        if (p.type === 'tool' && this.once(key)) this.say(who, lineFor('tool', who, seed), p.t1 + 0.3, SAY, 'think');
        if (p.type === 'send' && this.once(key) && seed % 3 === 0) this.say(`app:${e.app}`, lineFor('send', e.app, seed), p.t0);
      }
      // 等太久：应用嘀咕
      const done = e.ph.find((p) => p.type === 'done');
      const waited = t - startOf(e);
      if (!done && waited > 7 && waited < 7.5 && this.once(`${e.id}|wait`)) this.say(`app:${e.app}`, lineFor('wait', e.app, hash(e.id)), t, SAY, 'think');
    }

    // 闲聊：隔几秒挑一位没活的，和邻居说两句
    if (t >= this.nextIdle) {
      const slot = Math.floor(t / GAP);
      this.nextIdle = (slot + 1) * GAP + (hash(`idle|${slot}`) % 30) / 10;
      const free = ctx.wizards.filter((w) => !busy(w.id) && !ctx.sleeping.has(w.id));
      if (free.length) {
        const seed = hash(`chat|${slot}`);
        const a = free[seed % free.length];
        const mates = free.filter((w) => w.id !== a.id && ctx.floorOf(w.id) === ctx.floorOf(a.id));
        const b = mates.length ? mates[(seed >>> 3) % mates.length] : undefined;
        const open = lineFor('idle', a.id, seed, TH);
        this.say(a.id, open, t);
        if (b && open) this.say(b.id, replyFor(a.id, b.id, seed >>> 5), t + 2.2);
      }
    }
    // 心情：夜里、额度紧张，隔一阵说一次
    if (t >= this.nextMood) {
      this.nextMood = t + 25 + (hash(`mood|${Math.floor(t)}`) % 20);
      const free = ctx.wizards.filter((w) => !busy(w.id) && !ctx.sleeping.has(w.id));
      if (free.length) {
        const seed = hash(`mood|${Math.floor(t / 10)}`);
        const w = free[seed % free.length];
        if (ctx.quota !== null && ctx.quota < 20) this.say(w.id, lineFor('quota', w.id, seed), t, SAY, 'think');
        else if (ctx.night) this.say(w.id, lineFor('night', w.id, seed, TH), t, SAY, 'think');
      }
    }
    return this.bubbles.filter((b) => t >= b.t0 - 0.01 && t < b.t1);
  }

  /** 醒来、考试这类由页面直接触发 */
  event(kind: 'wake' | 'exam', id: string, t: number) {
    if (!this.once(`${kind}|${id}|${Math.floor(t)}`)) return;
    this.say(id, lineFor(kind, id, hash(`${kind}|${id}`)), t);
  }
}
