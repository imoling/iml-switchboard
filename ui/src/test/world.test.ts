import { describe, expect, it } from 'vitest';
import type { Call, CallStart, Job } from '../api/types';
import { Chatter, lineFor, personalityOf, replyFor } from '../world/chatter';
import { layoutFor, type Scene, WH, wizardPlan, WW } from '../world/scene';
import { buildDemo } from '../world/demo';
import { ACADEMY } from '../world/themes/academy';
import { THEMES } from '../world/themes';
import { DEMO_EPISODES, DEMO_LEN, DEMO_TOWERS, DEMO_WIZARDS } from '../world/demo';
import { Director, TOOL_WAIT } from '../world/live';
import { endOf, OPEN, type Episode } from '../world/model';
import { buildRoster, shortName, towerFor } from '../world/roster';
import { buildForTest, GH, GW, lookFor } from '../world/sprites';

const resolver = () => {
  const added: string[] = [];
  return {
    added,
    r: {
      wizard: (m: string) => { if (m && !added.includes(m)) added.push(m); return m; },
      tower: (h?: string) => h || 'other', name: (id: string) => id, towerName: (id: string) => id, money: (v: number) => `$${v.toFixed(2)}`,
    },
  };
};
const start = (o: Partial<CallStart> = {}): CallStart => ({ id: 'c1', host: 'codex', lane: 'claude_sub', provider: 'claude', model: 'sonnet', kind: 'text', tier: 'mid', attempt: 1, ...o });
const call = (o: Partial<Call> = {}): Call => ({
  id: 'c1', ts: '', source: 'facade', host: 'codex', kind: 'text', task_type: 'chat', tier: 'mid', lane: 'claude_sub', provider: 'claude', model: 'sonnet',
  reason: [], candidates: [], cost_usd: 0, baseline_cost_usd: 0.3, user_escalated: false, latency_ms: 800, ok: true, ...o,
});
const types = (e: Episode) => e.ph.map((p) => p.type);

describe('实时画面：事件翻成剧本', () => {
  it('一问一答：送信、判档、交给巫师、读信，做完回信交差，省下的钱算上', () => {
    const { r } = resolver(); const d = new Director(r);
    d.start(start(), 0);
    expect(types(d.episodes[0])).toEqual(['send', 'judge', 'go', 'work']);
    expect(d.episodes[0].ph[3].t1).toBe(OPEN);
    d.call(call(), 0.4); // 真实 0.4 秒就回来了：画面照样演完最短时长
    const e = d.episodes[0];
    expect(types(e)).toEqual(['send', 'judge', 'go', 'work', 'result', 'done']);
    for (let i = 1; i < e.ph.length; i++) expect(e.ph[i].t0).toBeGreaterThanOrEqual(e.ph[i - 1].t1 - 1e-9);
    expect(e.save).toBeCloseTo(0.3);
    expect(d.saved).toBeCloseTo(0.3);
  });

  it('换人：同一单第二次报开始，失手的歇着、下一位接手，应用那头不断', () => {
    const { r } = resolver(); const d = new Director(r);
    d.start(start({ model: 'kimi', lane: 'api' }), 0);
    d.start(start({ model: 'glm', lane: 'api', attempt: 2 }), 3);
    const e = d.episodes[0];
    expect(d.episodes).toHaveLength(1);
    expect(types(e)).toEqual(['send', 'judge', 'go', 'work', 'fail', 'handover', 'work']);
    expect(e.ph[5].d.to).toBe('glm');
    expect(d.curModel(e)).toBe('glm');
    expect(d.failures).toHaveLength(1);
  });

  it('工具来回：模型要跑工具，应用跑完送回来，还是同一段、同一位巫师', () => {
    const { r } = resolver(); const d = new Director(r);
    d.start(start({ session_id: 's1' }), 0);
    d.call(call({ tool_calls: 2, session_id: 's1' }), 6);
    let e = d.episodes[0];
    expect(types(e).slice(-2)).toEqual(['tool', 'appwork']);
    expect(e.ph[e.ph.length - 1].t1).toBe(OPEN);
    expect(e.awaitTool).toBe(6);
    d.start(start({ id: 'c2', session_id: 's1', tool_turn: true, stream: true }), 9);
    expect(d.episodes).toHaveLength(1);
    e = d.episodes[0];
    expect(types(e).slice(-3)).toEqual(['appwork', 'toolret', 'work']);
    d.call(call({ id: 'c2', output_tokens: 400 }), 12);
    expect(types(e).slice(-2)).toEqual(['stream', 'done']);
    expect(e.save).toBeCloseTo(0.6); // 两轮的原价都算进这一单
  });

  it('等工具等太久就当这段收尾了', () => {
    const { r } = resolver(); const d = new Director(r);
    d.start(start(), 0);
    d.call(call({ tool_calls: 1 }), 5);
    d.prune(5 + TOOL_WAIT + 1);
    expect(types(d.episodes[0]).at(-1)).toBe('done');
  });

  it('改判：规则判中档、判官改成高档，气泡先说规则再说水晶球', () => {
    const { r } = resolver(); const d = new Director(r);
    d.start(start({ model: 'opus', tier: 'high', rule_tier: 'mid', judge: 'Laya' }), 0);
    const j = d.episodes[0].ph[1];
    expect(j.d.text).toContain('导师');
    expect(j.d.text2).toContain('Laya');
    expect(j.d.text2).toContain('教授');
    expect(j.t1 - j.t0).toBeGreaterThan(1.5);
  });

  it('取药券：先回券、坩埚熬着、寄信人回桌前，熬好了送成品、记花费', () => {
    const { r } = resolver(); const d = new Director(r);
    d.start(start({ model: 'veo', lane: 'media', kind: 'video' }), 0);
    d.call(call({ kind: 'video', lane: 'media', model: 'veo', job_id: 'j1', cost_usd: 0, baseline_cost_usd: 0 }), 1);
    const e = d.episodes[0];
    expect(types(e).slice(-3)).toEqual(['ticket', 'progress', 'appself']);
    const job: Job = { id: 'j1', kind: 'video', status: 'running', progress: 0.4, provider: 'x', model: 'veo', prompt_excerpt: '', created_at: '', updated_at: '', call_id: 'c1' };
    d.job(job, 5);
    expect(e.prog).toBeCloseTo(0.4);
    d.job({ ...job, status: 'done', progress: 1, cost_usd: 2.4, result_path: '/p/assets/intro.mp4' }, 20);
    expect(types(e).slice(-2)).toEqual(['result', 'done']);
    expect(e.cost).toBeCloseTo(2.4);
    expect(e.ph.every((p) => p.t1 !== OPEN)).toBe(true);
  });

  it('没报过开始就失败（太长、谁都用不了）：照样演一封退回的信', () => {
    const { r } = resolver(); const d = new Director(r);
    d.call(call({ id: 'x', ok: false, model: '', too_long: true, input_tokens: 2_100_000, error: '内容太长' }), 0);
    expect(types(d.episodes[0])).toEqual(['send', 'judge', 'bounce', 'appwork']);
    expect(d.episodes[0].ph[1].d.bad).toBe(true);
    d.bounce({ host: 'codex', est_tokens: 300000 }, 1);
    expect(types(d.episodes[1])).toEqual(['send', 'judge', 'bounce', 'appwork']);
  });

  it('十几单同时进来：各演各的，都从现在开始，不排成一长串', () => {
    const { r } = resolver(); const d = new Director(r);
    for (let i = 0; i < 15; i++) d.start(start({ id: `b${i}`, model: `m${i % 4}` }), 0);
    expect(d.episodes).toHaveLength(15);
    expect(d.episodes.every((e) => e.ph[0].t0 === 0)).toBe(true);
  });

  it('演完的过几秒就收掉，记录表不越攒越多', () => {
    const { r } = resolver(); const d = new Director(r);
    d.start(start(), 0); d.call(call(), 1);
    const end = endOf(d.episodes[0]);
    d.prune(end + 10);
    expect(d.episodes).toHaveLength(0);
  });
});

describe('花名册', () => {
  it('同一个型号在几档都有时住最高那层；媒体进地窖；顶配进校长室', () => {
    const pick = (model: string, on = true) => ({ id: model, lane: 'claude_sub' as const, model, name: `Claude ${model}`, group: '', on });
    const rules = { task_table: [], explicit: [], context_rules: [], tiers: { low: { custom: false, picks: [pick('haiku'), pick('sonnet')] }, mid: { custom: false, picks: [pick('sonnet'), pick('off', false)] }, high: { custom: false, picks: [pick('opus')] } } };
    const accounts = [{ id: 'claude_sub', kind: 'subscription' as const, provider: 'anthropic', label: 'Claude', status: 'ok' as const, detail: '', enabled: true, models: [], tiers: { top: 'fable' }, labels: { fable: 'Fable 5.1' }, media: [{ kind: 'image' as const, model: 'img', display: 'GPT Image' }] }];
    const ws = buildRoster(rules, accounts);
    const floor = Object.fromEntries(ws.map((w) => [w.id, w.floor]));
    expect(floor).toEqual({ fable: 'top', opus: 'high', sonnet: 'mid', haiku: 'low', img: 'paint' });
    expect(ws.find((w) => w.id === 'opus')!.name).toBe('opus');
  });

  it('短名去掉厂商前缀，超长按词截', () => {
    expect(shortName('Claude Sonnet 5.5')).toBe('Sonnet 5.5');
    expect(shortName('WinWork DeepSeek V4.1 Flash')).toBe('DS V4.1 Flash');
    expect(shortName('WinWork DeepSeek V4 Pro')).toBe('DS V4 Pro');
    expect(shortName('GPT-6.1 Terra')).toBe('GPT-6.1 Terra');
    expect(shortName('Gemini 3.8 Flash（低）')).toBe('Gem 3.8 低');
    for (const n of ['WinWork OS 5.5', 'Haiku 5.5', 'GPT-6 Luna', 'Some Very Long Model Name 2']) expect(shortName(n).length).toBeLessThanOrEqual(13);
  });

  it('信塔满了，别的应用的信落到「其他应用」那座，不借别人的塔', () => {
    const tw = (id: string) => ({ id, name: id, color: '#000' });
    expect(towerFor([tw('a')], 'b').add?.id).toBe('b');
    const full = [tw('a'), tw('b'), tw('c'), tw('d')];
    const r = towerFor(full, 'e');
    expect(r.id).toBe('other');
    expect(r.add?.name).toBe('其他应用');
    expect(towerFor([...full, r.add!], undefined)).toEqual({ id: 'other' });
    expect(towerFor([...full, r.add!], 'f')).toEqual({ id: 'other' });
    expect(towerFor([tw('a')], undefined).id).toBe('other');
  });
  it('五座站台排得下，彼此不压着，每套主题都在画面里', () => {
    const towers = ['a', 'b', 'c', 'd', 'other'].map((id) => ({ id, name: id, color: '#000' }));
    for (const th of Object.values(THEMES)) {
      const L = layoutFor([], towers, th);
      expect(L.towers).toHaveLength(5);
      for (let i = 1; i < L.towers.length; i++) expect(L.towers[i].x - L.towers[i - 1].x).toBeGreaterThanOrEqual(18);
      for (const tw of L.towers) { expect(tw.x).toBeGreaterThan(8); expect(tw.x).toBeLessThan(WW); expect(tw.y).toBeLessThan(WH); }
    }
  });
});

describe('小人', () => {
  it('同一个名字永远是同一张脸；每一帧都是 16×24', () => {
    const w = { id: 'sonnet', lane: 'sub' as const, floor: 'mid' as const };
    expect(lookFor(w)).toEqual(lookFor(w));
    const poses = ['stand', 'walk', 'cast', 'read', 'sit', 'sleep', 'bow', 'cheer', 'wave', 'droop', 'stir', 'paint', 'write', 'carry', 'stretch', 'blink'] as const;
    for (const id of ['sonnet', 'x', 'GPT-6.1 Terra', '画师']) for (const floor of ['low', 'mid', 'high', 'top', 'paint', 'potion'] as const) {
      const look = lookFor({ id, lane: 'api', floor });
      expect(look.skin && look.hair && look.robe && look.hatColor).toBeTruthy();
      for (const p of poses) for (const dir of ['down', 'right', 'up'] as const) for (const f of [0, 1, 2]) {
        const b = buildForTest(look, p, f, dir);
        expect(b.grid).toHaveLength(GH);
        const bad = b.grid.filter((row) => row.length !== GW || !/^[.KSsHhRrTAaGgWwBOoNnPYFQqZ]+$/.test(row.join('')));
        if (bad.length) expect(bad).toEqual([]);
      }
    }
  });
});

describe('演示剧本和走位', () => {
  const scene = (t: number): Scene => ({
    t, theme: ACADEMY, wizards: DEMO_WIZARDS, towers: DEMO_TOWERS, episodes: DEMO_EPISODES, layout: layoutFor(DEMO_WIZARDS, DEMO_TOWERS, ACADEMY),
    night: false, rain: false, snow: false, paused: false, bottles: [], saved: 0, money: String,
    sleeping: new Set(), wakeAt: new Map(), cooling: new Set(), exam: null, pokes: new Map(), coolSpan: 10, bubbles: [], showNames: true,
  });
  it('每一单的阶段首尾相接，都在剧本时长里', () => {
    for (const e of DEMO_EPISODES) {
      expect(endOf(e)).toBeLessThanOrEqual(DEMO_LEN);
      const main = e.ph.filter((p) => p.type !== 'appself');
      for (let i = 1; i < main.length; i++) expect(main[i].t0).toBeCloseTo(main[i - 1].t1, 5);
    }
  });
  it('换人那一段：Kimi 坐到长椅上歇着，GLM 站在栏杆边干活', () => {
    const sc = scene(18.2);
    const kimi = wizardPlan(DEMO_WIZARDS.find((w) => w.id === 'kimi')!, sc)!;
    const glm = wizardPlan(DEMO_WIZARDS.find((w) => w.id === 'glm')!, sc)!;
    const spot = sc.layout.wiz.get('glm')!;
    expect(kimi.pose).toBe('sit');
    expect(kimi.x).toBe(sc.layout.wiz.get('kimi')!.bench[0]);
    expect(glm.x).toBe(spot.slot[0]);
  });
  it('五套主题：每套都有自己的叫法，剧本按叫法生成，每个小人都站在画面里', () => {
    const towers = ['claude_code', 'codex', 'workbuddy'].map((id) => ({ id, name: id, color: '#000' }));
    for (const th of Object.values(THEMES)) {
      const demo = buildDemo(th.words);
      expect(demo.episodes).toHaveLength(DEMO_EPISODES.length);
      for (const k of ['low', 'mid', 'high', 'top', 'image', 'video', 'animation']) expect(th.words.judge[k], `${th.id} judge.${k}`).toBeTruthy();
      expect(th.words.banter.length).toBeGreaterThan(2);
      const L = layoutFor(demo.wizards, towers, th);
      for (const w of demo.wizards) {
        const sp = L.wiz.get(w.id)!;
        for (const [x, y] of [sp.idle, sp.slot, sp.bench]) { expect(x, `${th.id} ${w.id}`).toBeGreaterThan(8); expect(x).toBeLessThan(WW - 4); expect(y).toBeGreaterThan(20); expect(y).toBeLessThanOrEqual(WH); }
      }
      for (let t = 0; t < DEMO_LEN; t += 1.7) for (const w of demo.wizards) {
        const sc: Scene = { t, theme: th, wizards: demo.wizards, towers, episodes: demo.episodes, layout: L, night: false, rain: false, snow: false, paused: false, bottles: [], saved: 0, money: String, sleeping: new Set(), wakeAt: new Map(), cooling: new Set(), exam: null, pokes: new Map(), coolSpan: 10, bubbles: [], showNames: true };
        const p = wizardPlan(w, sc);
        if (p) { expect(p.x).toBeGreaterThan(0); expect(p.x).toBeLessThan(WW); expect(p.y).toBeGreaterThan(0); expect(p.y).toBeLessThanOrEqual(WH); }
      }
    }
  });
  it('额度用完的趴下睡，点一下的挥手', () => {
    const sc = scene(55);
    sc.sleeping = new Set(['sonnet']);
    expect(wizardPlan(DEMO_WIZARDS.find((w) => w.id === 'sonnet')!, sc)!.pose).toBe('sleep');
    sc.pokes = new Map([['kimi', 54.5]]);
    expect(wizardPlan(DEMO_WIZARDS.find((w) => w.id === 'kimi')!, sc)!.pose).toBe('wave');
  });
});

describe('闲聊', () => {
  it('性格由名字算，稳定；两派都有人', () => {
    expect(personalityOf('sonnet')).toEqual(personalityOf('sonnet'));
    const ids = ['sonnet', 'haiku', 'opus', 'kimi', 'glm', 'ds', 'gpt', 'fable', 'qwen', 'minimax', 'gemini', 'seed'];
    const f = new Set(ids.map((id) => personalityOf(id).faction));
    expect(f.size).toBe(2);
  });
  it('每句都有话说，不用半角标点和三个点', () => {
    const triggers = ['idle', 'reply', 'task', 'done', 'fail', 'cool', 'skipped', 'night', 'quota', 'poke', 'wait', 'takeover', 'tool', 'exam', 'wake', 'appPoke', 'send'] as const;
    for (const id of ['sonnet', 'kimi', 'opus', 'ds']) for (const tr of triggers) for (let seed = 0; seed < 12; seed++) {
      const s = lineFor(tr, id, seed);
      expect(s, `${tr} ${id} ${seed}`).toBeTruthy();
      expect(s).not.toMatch(/[,:;!?]/);
      expect(s).not.toContain('...');
      expect(s!.length).toBeLessThanOrEqual(16);
    }
    expect(replyFor('sonnet', 'kimi', 3)).toBeTruthy();
  });
  it('演示里同一秒看到的是同一句；接活、交差各说一次', () => {
    const run = () => {
      const c = new Chatter();
      const seen: string[] = [];
      const L = layoutFor(DEMO_WIZARDS, DEMO_TOWERS, ACADEMY);
      for (let t = 0; t < 30; t += 0.25) {
        const b = c.tick({ t, episodes: DEMO_EPISODES, wizards: DEMO_WIZARDS, towers: DEMO_TOWERS, night: false, quota: 60, sleeping: new Set(), banter: ACADEMY.words.banter, hubPoke: ACADEMY.words.hubPoke, floorOf: (id) => L.wiz.get(id)?.floor });
        for (const x of b) if (!seen.includes(`${x.who}|${x.text}|${x.t0}`)) seen.push(`${x.who}|${x.text}|${x.t0}`);
      }
      return seen;
    };
    const a = run(), b = run();
    expect(a).toEqual(b);
    expect(a.length).toBeGreaterThan(5);
    expect(a.filter((x) => x.startsWith('haiku|')).length).toBeGreaterThanOrEqual(2); // 接活一句、交差一句
  });
});
