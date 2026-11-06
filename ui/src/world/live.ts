/**
 * 实时画面：把后台事件（call_start、call、job、bounce）翻成剧本。
 *
 * 真实请求快的 0.3 秒、慢的几分钟，画面跟不上也不该硬跟：每个阶段有最短时长，排在上一段后面，
 * 画面比真实时间晚几秒没关系；积压多了就快放，免得十几单同时进来排出五分钟的戏。
 * 这个类不碰界面、不碰时钟，时刻由调用方传进来，测试可以一步步喂事件。气泡和旁白里的叫法跟着主题的 words 走。
 */
import type { Bounce, Call, CallStart, Job, Kind } from '../api/types';
import { type Caption, type Episode, endOf, OPEN, type Ph, type PhaseData } from './model';
import type { SceneWords } from './theme';
import { ACADEMY_WORDS } from './themes/academy';
import { moneySaved, moneySpent, shortWhy, SUB_FREE, TIER_TERM } from './words';

export interface Resolver {
  /** 型号对应哪位巫师；花名册里没有就当场加一位，返回标识 */
  wizard(model: string, display: string | undefined, lane: string, kind: Kind, tier?: string): string;
  /** 应用对应哪座信塔 */
  tower(host?: string): string;
  name(id: string): string;
  towerName(id: string): string;
  money(usd: number): string;
}

/** 各阶段的最短时长（秒） */
const DUR: Partial<Record<Ph, number>> = {
  send: 1.3, judge: 0.9, go: 1.1, work: 0.8, tool: 1.2, appwork: 1.2, toolret: 1.6, result: 1.2, ticket: 1.2,
  done: 1.3, fail: 1.0, handover: 1.3, bounce: 1.5, progress: 1.5, stream: 2,
};
/** 等应用把工具结果送回来最多等多久，过了就当这段对话收尾了 */
export const TOOL_WAIT = 60;
/** 一单开着最久多久没消息就收掉（后台重启、事件丢了） */
const STALE = 15 * 60;
const SUB_LANES = new Set(['claude_sub', 'codex_sub', 'agy_sub']);

export class Director {
  episodes: Episode[] = [];
  captions: Caption[] = [];
  /** 失败的时刻：5 分钟内有就下雨 */
  failures: number[] = [];
  saved = 0;
  private byCall = new Map<string, Episode>();
  private byJob = new Map<string, Episode>();
  private seq = 0;

  constructor(private r: Resolver, public words: SceneWords = ACADEMY_WORDS) {}

  private say(t: number, s: string) {
    this.captions.push([t, s]);
    if (this.captions.length > 40) this.captions.splice(0, this.captions.length - 40);
  }

  /** 排一段：接在上一段后面；上一段开着就先收掉 */
  private push(e: Episode, type: Ph, now: number, d: PhaseData = {}, dur?: number): number {
    this.close(e, now);
    const last = endOf(e);
    const t0 = Math.max(now, e.ph.length ? last : now);
    const backlog = t0 - now;
    const k = backlog > 6 ? 0.35 : backlog > 3 ? 0.6 : 1; // 积压了就快放
    const len = (dur ?? DUR[type] ?? 1) * k;
    e.ph.push({ type, t0, t1: t0 + len, d });
    return t0;
  }

  private open(e: Episode, type: Ph, now: number, d: PhaseData = {}) {
    const t0 = this.push(e, type, now, d);
    e.ph[e.ph.length - 1].t1 = OPEN;
    return t0;
  }

  /** 开着的阶段收尾：至少演够最短时长 */
  private close(e: Episode, now: number) {
    const p = e.ph[e.ph.length - 1];
    if (!p || p.t1 !== OPEN) return;
    p.t1 = Math.max(now, p.t0 + (DUR[p.type] ?? 0.8));
  }

  private create(id: string, app: string, model: string, kind: Kind): Episode {
    const e: Episode = { id: `${id}#${this.seq++}`, app, model, kind, ph: [], callIds: [id] };
    this.episodes.push(e);
    this.byCall.set(id, e);
    return e;
  }

  private judgeText(ev: Pick<CallStart, 'tier' | 'rule_tier' | 'judge' | 'kind' | 'sticky'>, who: string): PhaseData {
    const W = this.words;
    const kind = ev.kind ?? 'text';
    if (kind !== 'text') return { text: W.judge[kind] };
    if (ev.sticky) return { text: W.judgeSticky(who) };
    if (ev.rule_tier && ev.tier && ev.rule_tier !== ev.tier) return { text: W.judgeRule(ev.rule_tier), text2: W.judgeCrystal(ev.judge ?? '', ev.tier) };
    return { text: W.judge[ev.tier ?? 'mid'] ?? W.judge.mid };
  }

  start(ev: CallStart, now: number) {
    const W = this.words, CAP = W.caption;
    const app = this.r.tower(ev.host);
    const kind: Kind = ev.kind ?? 'text';
    const who = this.r.wizard(ev.model ?? '', ev.display, ev.lane, kind, ev.tier);
    const appName = this.r.towerName(app);
    const known = this.byCall.get(ev.id);

    // 换人：同一单又报了一次开始，接手的是下一位候选
    if (known && (ev.attempt ?? 1) > 1) {
      const from = this.curModel(known);
      if (from === who) return;
      this.push(known, 'fail', now, { text: '失手' });
      this.failures.push(now);
      this.push(known, 'handover', now, { to: who });
      this.open(known, 'work', now);
      this.say(now, CAP.handover(this.r.name(from), this.r.name(who), '失手'));
      return;
    }
    if (known) return;

    // 工具来回：应用把工具结果送回来，接着找同一位巫师（会话粘住）
    if (ev.tool_turn && kind === 'text') {
      const waiting = this.episodes.find((e) => e.awaitTool !== undefined && e.app === app && (ev.session_id ? e.session === ev.session_id : this.curModel(e) === who));
      if (waiting) {
        waiting.awaitTool = undefined;
        waiting.callIds.push(ev.id);
        this.byCall.set(ev.id, waiting);
        this.push(waiting, 'toolret', now);
        if (this.curModel(waiting) !== who) this.push(waiting, 'handover', now, { to: who });
        this.open(waiting, 'work', now);
        waiting.stream = ev.stream;
        this.say(now, CAP.toolBack(appName, this.r.name(who)));
        return;
      }
      const e = this.create(ev.id, app, who, kind);
      e.session = ev.session_id; e.stream = ev.stream;
      this.push(e, 'appwork', now, { text: W.say.toolBack });
      this.push(e, 'toolret', now);
      this.open(e, 'work', now);
      this.say(now, CAP.toolBack(appName, this.r.name(who)));
      return;
    }

    const e = this.create(ev.id, app, who, kind);
    e.session = ev.session_id; e.stream = ev.stream;
    this.push(e, 'send', now);
    const jd = this.judgeText(ev, this.r.name(who));
    this.push(e, 'judge', now, jd, jd.text2 ? 1.8 : undefined);
    this.push(e, 'go', now);
    this.open(e, 'work', now);
    if (jd.text2) this.say(now, CAP.rejudge(ev.judge ?? '', W.title[ev.rule_tier!] ?? '', W.title[ev.tier!] ?? ''));
    if (ev.sticky) this.say(now, CAP.sticky(appName, this.r.name(who)));
    else this.say(now, CAP.arrive(appName, kind === 'text' ? `判${TIER_TERM[ev.tier ?? 'mid'] ?? '中档'}` : (W.judge[kind] ?? ''), this.r.name(who), SUB_LANES.has(ev.lane) ? '订阅' : ev.lane === 'media' ? '媒体' : '按量付费'));
  }

  call(c: Call, now: number) {
    const W = this.words, CAP = W.caption;
    let e = this.byCall.get(c.id);
    const appName = this.r.towerName(this.r.tower(c.host));
    if (!e) {
      // 没报过开始的：太长被拦下、谁都用不了，或者画面打开前就在跑的
      const who = c.model ? this.r.wizard(c.model, c.model_display, c.lane, c.kind, c.tier ?? undefined) : '';
      e = this.create(c.id, this.r.tower(c.host), who, c.kind);
      e.session = c.session_id;
      this.push(e, 'send', now);
      if (!who || c.too_long) {
        const wan = c.too_long ? Math.round((c.input_tokens ?? 0) / 1e4) : 0;
        this.push(e, 'judge', now, { text: c.too_long ? W.judgeTooThick(wan) : W.nobody, bad: true });
        this.push(e, 'bounce', now, { text: c.too_long ? W.say.bounce : W.say.bounceFail(shortWhy(c.error)) });
        if (c.too_long) this.push(e, 'appwork', now, { text: W.say.shorten });
        this.failures.push(now);
        this.say(now, c.too_long ? CAP.bounced(appName) : CAP.failed(appName, shortWhy(c.error)));
        return;
      }
      this.push(e, 'judge', now, { text: c.kind === 'text' ? (W.judge[c.tier ?? 'mid'] ?? W.judge.mid) : W.judge[c.kind] });
      this.push(e, 'go', now);
      this.push(e, 'work', now);
    }
    // 老版后台的开始事件不带型号：做完时补上，回信才知道从谁的魔杖出来
    if (!e.model && c.model) e.model = this.r.wizard(c.model, c.model_display, c.lane, c.kind, c.tier ?? undefined);
    const who = this.curModel(e);
    if (!c.ok) {
      const why = shortWhy(c.error);
      this.push(e, 'fail', now, { text: why });
      this.push(e, 'bounce', now, { text: c.too_long ? W.say.bounce : W.say.bounceFail(why) });
      this.failures.push(now);
      this.say(now, CAP.failed(appName, why));
      return;
    }
    const sub = SUB_LANES.has(c.lane);
    const save = Math.max(0, (c.baseline_cost_usd ?? 0) - (c.cost_usd ?? 0));
    e.redo = { callId: c.id, tier: c.tier ?? undefined };
    if (c.job_id) {
      e.jobId = c.job_id;
      this.byJob.set(c.job_id, e);
      this.push(e, 'ticket', now);
      e.prog = 0;
      // 坩埚在熬，寄信人回桌前干自己的活：两段同时开着，熬好了一起收
      const t0 = this.open(e, 'progress', now);
      e.ph.push({ type: 'appself', t0, t1: OPEN, d: { text: W.say.ownWork } });
      this.say(now, CAP.ticket(appName));
      return;
    }
    e.save = (e.save ?? 0) + (c.kind === 'text' ? save : 0);
    e.cost = (e.cost ?? 0) + (sub ? 0 : c.cost_usd ?? 0);
    e.sub = sub && !e.cost;
    if (c.kind === 'text' && (c.tool_calls ?? 0) > 0) {
      this.push(e, 'tool', now, { text: W.say.toolAsk(c.tool_calls!), n: c.tool_calls });
      this.open(e, 'appwork', now, { text: W.say.toolFetch });
      e.awaitTool = now;
      e.session = e.session ?? c.session_id;
      this.say(now, CAP.toolAsk(this.r.name(who), appName));
      return;
    }
    if (e.stream && c.kind === 'text') this.push(e, 'stream', now, {}, Math.min(3.5, Math.max(1.2, (c.output_tokens ?? 200) / 160)));
    else this.push(e, 'result', now);
    this.finish(e, now);
  }

  private finish(e: Episode, now: number) {
    this.push(e, 'done', now);
    this.saved += e.save ?? 0;
    const money = e.cost ? moneySpent(this.r.money(e.cost)) : e.save ? `${moneySaved(this.r.money(e.save))}${e.sub ? `，${SUB_FREE}` : ''}` : SUB_FREE;
    this.say(now, this.words.caption.done(this.r.name(this.curModel(e)), money));
  }

  job(j: Job, now: number) {
    const W = this.words;
    let e = this.byJob.get(j.id) ?? (j.call_id ? this.byCall.get(j.call_id) : undefined);
    const running = j.status === 'queued' || j.status === 'submitted' || j.status === 'running';
    if (!e) {
      if (!running) return;
      // 画面打开前就在熬的魔药：直接从坩埚开始
      const who = this.r.wizard(j.model, undefined, 'media', j.kind);
      e = this.create(j.call_id || j.id, this.r.tower(undefined), who, j.kind);
      e.jobId = j.id; this.byJob.set(j.id, e);
      this.open(e, 'progress', now);
    }
    e.prog = Math.max(0, Math.min(1, (j.progress ?? 0) > 1 ? j.progress / 100 : j.progress ?? 0));
    if (running) return;
    // 收尾：appself 和 progress 一起关
    for (const p of e.ph) if (p.t1 === OPEN) p.t1 = Math.max(now, p.t0 + 1.5);
    if (j.status === 'done') {
      e.prog = 1;
      e.cost = (e.cost ?? 0) + (j.cost_usd ?? 0);
      this.push(e, 'result', now);
      if (j.result_path) this.say(now, W.caption.jobDone(j.result_path.split('/').slice(-2).join('/')));
      this.finish(e, now);
    } else {
      const why = shortWhy(j.error);
      this.push(e, 'fail', now, { text: why });
      this.push(e, 'bounce', now, { text: W.say.bounceFail(why) });
      this.failures.push(now);
    }
    this.byJob.delete(j.id);
  }

  bounce(b: Bounce, now: number) {
    const W = this.words;
    const app = this.r.tower(b.host);
    const e = this.create(`bounce-${this.seq}`, app, '', 'text');
    e.session = b.session_id;
    this.push(e, 'send', now);
    this.push(e, 'judge', now, { text: W.judgeTooThick(Math.round(b.est_tokens / 1e4)), bad: true });
    this.push(e, 'bounce', now, { text: W.say.bounce });
    this.push(e, 'appwork', now, { text: W.say.shorten });
    this.say(now, W.caption.bounced(this.r.towerName(app)));
  }

  curModel(e: Episode): string {
    let m = e.model;
    for (const p of e.ph) if (p.type === 'handover' && p.d.to) m = p.d.to;
    return m;
  }

  /** 收掉演完的、等工具等太久的、丢了消息的 */
  prune(now: number) {
    for (const e of this.episodes) {
      if (e.awaitTool !== undefined && now - e.awaitTool > TOOL_WAIT) { e.awaitTool = undefined; this.finish(e, now); }
      const last = e.ph[e.ph.length - 1];
      if (last && last.t1 === OPEN && now - last.t0 > STALE) { for (const p of e.ph) if (p.t1 === OPEN) p.t1 = now; this.push(e, 'done', now); }
    }
    const keep = (e: Episode) => endOf(e) > now - 4;
    for (const e of this.episodes) if (!keep(e)) { for (const id of e.callIds) if (this.byCall.get(id) === e) this.byCall.delete(id); }
    this.episodes = this.episodes.filter(keep);
    if (this.episodes.length > 60) this.episodes.splice(0, this.episodes.length - 60);
    this.failures = this.failures.filter((t) => now - t < 300);
  }
}
