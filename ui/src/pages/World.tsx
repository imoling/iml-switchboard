import { useCallback, useEffect, useRef, useState } from 'react';
import { api, subscribeEvents } from '../api/client';
import type { Account, Integration, Quota, Rules, StreamEvent } from '../api/types';
import { mascotSprite, Sprite } from '../components/Pixel';
import { Seg, toast } from '../components/ui';
import { usd } from '../format';
import { useTheme } from '../theme/ThemeContext';
import { type ChatCtx, Chatter, FACTION_NAME, personalityOf, QUIRK_NAME } from '../world/chatter';
import { buildDemo, DEMO_CHAPTERS, DEMO_EXAM, DEMO_LEN, DEMO_SLEEP, demoQuota, demoSaved } from '../world/demo';
import { Director } from '../world/live';
import { type Caption, endOf, type Episode, type Floor, modelAt, startOf, type Tower, type Wizard } from '../world/model';
import { buildRoster, buildTowers, guestWizard, towerFor, towerName } from '../world/roster';
import { floorAt, FONT, type Hit, layoutFor, type Layout, render, type Scene, WH, WW } from '../world/scene';
import type { ThemeDef, ThemeKey } from '../world/theme';
import { themeFor, THEMES } from '../world/themes';
import { LANE_NAME, WORLD } from '../world/words';

const now = () => performance.now() / 1000;
const RANK: Partial<Record<Floor, number>> = { low: 0, mid: 1, high: 2, top: 3 };
const FLOOR_TIER: Partial<Record<Floor, 'low' | 'mid' | 'high' | 'top'>> = { low: 'low', mid: 'mid', high: 'high', top: 'top' };
const REDO_WINDOW = 10 * 60;

/**
 * 实时画面的状态放在模块里：切到别的页再回来，正在演的那几单还在；事件流只开一条，开着就不关。
 */
const live = {
  wizards: [] as Wizard[], guests: [] as Wizard[], towers: [] as Tower[], extra: [] as Tower[],
  director: null as Director | null,
  chatter: new Chatter(),
  lastByWizard: new Map<string, { callId: string; tier?: string; t: number }>(),
  version: 0,
};
const allWizards = () => [...live.wizards, ...live.guests.filter((g) => !live.wizards.some((w) => w.id === g.id))];
const allTowers = () => [...live.towers, ...live.extra.filter((x) => !live.towers.some((t) => t.id === x.id))];

function ensureLive(): Director {
  if (live.director) return live.director;
  const d = new Director({
    wizard: (model, display, lane, kind, tier) => {
      if (!model) return '';
      if (allWizards().some((w) => w.id === model)) return model;
      live.guests.push(guestWizard(model, display, lane, kind, tier)); live.version++;
      return model;
    },
    tower: (host) => { const r = towerFor(allTowers(), host); if (r.add) { live.extra.push(r.add); live.version++; } return r.id; },
    name: (id) => allWizards().find((w) => w.id === id)?.name ?? id,
    towerName: (id) => allTowers().find((x) => x.id === id)?.name ?? towerName(id),
    money: (v) => usd(v),
  });
  live.director = d;
  subscribeEvents(onEvent);
  return d;
}

function onEvent(e: StreamEvent) {
  const d = live.director!;
  const t = now();
  if (e.type === 'call_start') d.start(e.data, t);
  else if (e.type === 'call') {
    const c = e.data.call;
    d.call(c, t);
    if (c.ok && c.kind === 'text' && c.model && c.tier && c.tier !== 'top') live.lastByWizard.set(c.model, { callId: c.id, tier: c.tier, t });
  } else if (e.type === 'job') d.job(e.data.job, t);
  else if (e.type === 'bounce') d.bounce(e.data, t);
}

/** 背景图按主题缓存；加载完才画，没加载完先铺底色 */
const bgImages = new Map<string, HTMLImageElement>();
function bgFor(theme: ThemeDef): HTMLImageElement | null {
  let img = bgImages.get(theme.id);
  if (!img) { img = new Image(); img.src = theme.bg; bgImages.set(theme.id, img); }
  return img.complete && img.naturalWidth > 0 ? img : null;
}

interface Data { rules: Rules | null; accounts: Account[]; integrations: Integration[]; quotas: Quota[]; saved: number; evalRunning: boolean }
interface Card { kind: 'wizard' | 'tower'; id: string; x: number; y: number }
interface Confirm { callId: string; tier: 'low' | 'mid' | 'high' | 'top'; name: string }

/** 学院：一次请求里模型和应用怎么来回，演给人看 */
export function World({ paused, onNavigate }: { paused: boolean; onNavigate?: (page: 'accounts' | 'integrations') => void }) {
  const { t, effectiveId } = useTheme();
  const [override, setOverride] = useState<ThemeKey | null>(null);
  const theme = override ? THEMES[override] : themeFor(effectiveId);
  const [mode, setMode] = useState<'live' | 'demo'>('live');
  const [data, setData] = useState<Data | null>(null);
  const [card, setCard] = useState<Card | null>(null);
  const [confirm, setConfirm] = useState<Confirm | null>(null);
  const [playing, setPlaying] = useState(true);
  const [showNames, setShowNames] = useState(false);
  const [chapter, setChapter] = useState(0);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const wrapRef = useRef<HTMLDivElement>(null);
  const capRef = useRef<HTMLDivElement>(null);
  const barRef = useRef<HTMLElement>(null);
  const st = useRef({
    S: 3, hits: [] as Hit[], hover: undefined as string | undefined, pokes: new Map<string, number>(),
    demoT: 0, playing: true, mode: 'live' as 'live' | 'demo', layout: null as Layout | null, layoutKey: '',
    down: null as null | { x: number; y: number; id?: string; kind?: Hit['kind'] }, drag: undefined as Scene['drag'],
    demoExtra: [] as Episode[], demoChatter: new Chatter(), sleeping: new Set<string>(), wakeAt: new Map<string, number>(), examSince: 0, paused,
    data: null as Data | null, lastCap: '', chapter: 0, theme, font: false, showNames: false, nightOverride: null as boolean | null,
  });
  st.current.mode = mode; st.current.playing = playing; st.current.paused = paused; st.current.data = data; st.current.theme = theme; st.current.showNames = showNames;
  if (live.director) live.director.words = theme.words;

  // 像素字体：加载完再用，不然名牌和气泡会先用系统字体画一帧
  useEffect(() => {
    const fonts = (document as Document & { fonts?: FontFaceSet }).fonts;
    if (!fonts) return;
    void fonts.load(`10px "${FONT}"`).then(() => { st.current.font = true; }).catch(() => undefined);
  }, []);

  // 真实数据：花名册、信塔、额度、省下的钱、评估在不在跑
  const load = useCallback(async () => {
    const [rules, accounts, integrations, ov, evals] = await Promise.all([
      api.rules().catch(() => null), api.accounts().catch(() => [] as Account[]), api.integrations().catch(() => [] as Integration[]),
      api.overview('today').catch(() => null), api.evaluations().catch(() => []),
    ]);
    const d = ensureLive();
    live.wizards = buildRoster(rules, accounts);
    live.towers = buildTowers(integrations);
    live.version++;
    d.saved = 0;
    setData({ rules, accounts, integrations, quotas: ov?.quotas ?? [], saved: ov?.saved_usd ?? 0, evalRunning: evals.some((e) => e.status === 'running') });
  }, []);
  useEffect(() => { void load(); const id = setInterval(() => void load(), 30000); return () => clearInterval(id); }, [load]);

  // 额度用完的订阅：那几位趴下睡；醒来时伸个懒腰
  useEffect(() => {
    if (!data) return;
    const subIds = new Set(data.accounts.filter((a) => a.kind === 'subscription').map((a) => a.id));
    const out = new Set<string>();
    for (const w of live.wizards) {
      if (!w.acct || !subIds.has(w.acct)) continue;
      const acc = data.accounts.find((a) => a.id === w.acct);
      const full = data.quotas.some((q) => q.account_id === w.acct && !q.sub && q.pct >= 100) || acc?.status === 'cooldown';
      if (full) out.add(w.id);
    }
    const s = st.current;
    for (const id of s.sleeping) if (!out.has(id)) { s.wakeAt.set(id, now()); live.chatter.event('wake', id, now()); }
    s.sleeping = out;
    if (data.evalRunning && !s.examSince) s.examSince = now();
    if (!data.evalRunning) s.examSince = 0;
  }, [data]);

  // 画布按容器宽度取整数倍放大
  useEffect(() => {
    const cv = canvasRef.current, wrap = wrapRef.current;
    if (!cv || !wrap) return;
    const fit = () => {
      const dpr = window.devicePixelRatio || 1;
      // 取偶数倍：小字按半格画，偶数倍下每个字的点才是整数个设备像素
      const S = Math.max(2, 2 * Math.round((wrap.clientWidth * dpr) / WW / 2));
      if (S !== st.current.S || cv.width !== WW * S) { st.current.S = S; cv.width = WW * S; cv.height = WH * S; }
    };
    fit();
    const ro = new ResizeObserver(fit); ro.observe(wrap);
    return () => ro.disconnect();
  }, []);

  // 画面循环：30 帧封顶，窗口藏起来就停
  useEffect(() => {
    let raf = 0, last = now(), lastDraw = 0;
    const frame = () => {
      raf = requestAnimationFrame(frame);
      const tn = now();
      const dt = Math.min(0.1, tn - last); last = tn;
      const s = st.current;
      if (document.hidden) return;
      if (s.mode === 'demo' && s.playing) { s.demoT += dt; if (s.demoT >= DEMO_LEN) { s.demoT = 0; s.demoExtra = []; s.demoChatter.reset(); } }
      if (tn - lastDraw < 1 / 30) return;
      lastDraw = tn;
      const cv = canvasRef.current; const c = cv?.getContext('2d');
      if (!cv || !c) return;
      const scene = s.mode === 'demo' ? demoScene(s) : liveScene(s);
      s.hits = render(c, s.S, scene);
      // 旁白和进度条直接改 DOM，不让 React 每帧重画
      const demo = buildDemo(s.theme.words);
      const caps: Caption[] = s.mode === 'demo' ? demo.captions.filter((x) => x[0] <= s.demoT) : (live.director?.captions ?? []);
      const recent = caps.slice(-3).map((x) => x[1]);
      const key = recent.join('\n');
      if (key !== s.lastCap && capRef.current) { s.lastCap = key; capRef.current.replaceChildren(...recent.map((line) => { const d = document.createElement('div'); d.textContent = line; return d; })); }
      if (barRef.current) barRef.current.style.width = `${(s.demoT / DEMO_LEN) * 100}%`;
      if (s.mode === 'demo') { let ch = 0; DEMO_CHAPTERS.forEach(([ct], i) => { if (s.demoT >= ct) ch = i; }); if (ch !== s.chapter) { s.chapter = ch; setChapter(ch); } }
    };
    raf = requestAnimationFrame(frame);
    return () => cancelAnimationFrame(raf);
  }, []);

  // 开发时给无头浏览器截图用：跳到演示的某一秒、换主题看效果
  useEffect(() => {
    if (!import.meta.env.DEV) return;
    const w = window as unknown as { __world?: unknown };
    w.__world = {
      seek: (sec: number) => { st.current.demoT = sec; st.current.demoChatter.reset(); }, pause: (p: boolean) => setPlaying(!p),
      feed: (type: string, data: unknown) => { ensureLive(); onEvent({ type, data } as StreamEvent); }, hits: () => st.current.hits,
      demo: () => setMode('demo'), names: (v: boolean) => setShowNames(v), font: () => st.current.font, theme: (k: ThemeKey | null) => setOverride(k), night: (v: boolean | null) => { st.current.nightOverride = v; },
    };
  }, []);

  /* ---------- 交互：点一下他挥手说句话；把信拖到楼上请高一档重做 ---------- */
  const toLogical = (e: React.PointerEvent) => {
    const r = canvasRef.current!.getBoundingClientRect();
    return { x: ((e.clientX - r.left) / r.width) * WW, y: ((e.clientY - r.top) / r.height) * WH, cx: e.clientX - r.left, cy: e.clientY - r.top };
  };
  const hitAt = (x: number, y: number) => { const hs = st.current.hits; for (let i = hs.length - 1; i >= 0; i--) { const h = hs[i]; if (x >= h.x0 && x <= h.x1 && y >= h.y0 && y <= h.y1) return h; } return undefined; };
  const wizardsNow = () => (st.current.mode === 'demo' ? buildDemo(st.current.theme.words).wizards : allWizards());
  const redoable = (id: string) => {
    const w = wizardsNow().find((x) => x.id === id);
    if (!w || RANK[w.floor] === undefined || w.floor === 'top') return false;
    if (st.current.mode === 'demo') return true;
    const r = live.lastByWizard.get(id);
    return !!r && now() - r.t < REDO_WINDOW;
  };

  const onDown = (e: React.PointerEvent) => {
    const p = toLogical(e); const h = hitAt(p.x, p.y);
    st.current.down = { x: p.x, y: p.y, id: h?.id, kind: h?.kind };
    (e.target as HTMLElement).setPointerCapture?.(e.pointerId);
  };
  const onMove = (e: React.PointerEvent) => {
    const p = toLogical(e); const s = st.current;
    const h = hitAt(p.x, p.y);
    s.hover = h?.kind === 'wizard' ? h.id : undefined;
    if (canvasRef.current) canvasRef.current.style.cursor = s.down?.id && s.down.kind === 'wizard' && redoable(s.down.id) && s.drag ? 'grabbing' : h ? 'pointer' : 'default';
    const dn = s.down;
    if (dn?.id && dn.kind === 'wizard' && redoable(dn.id) && (s.drag || Math.hypot(p.x - dn.x, p.y - dn.y) > 4)) {
      const w = wizardsNow().find((x) => x.id === dn.id)!;
      const f = floorAt(p.x, p.y, s.theme);
      s.drag = { id: dn.id, x: p.x, y: p.y, target: f && (RANK[f] ?? -1) > (RANK[w.floor] ?? 9) ? f : undefined };
      setCard(null);
    }
  };
  const onUp = (e: React.PointerEvent) => {
    const p = toLogical(e); const s = st.current; const dn = s.down; s.down = null;
    const drag = s.drag; s.drag = undefined;
    if (drag) {
      if (!drag.target) return;
      const tier = FLOOR_TIER[drag.target]!;
      if (s.mode === 'demo') { injectDemoRedo(drag.id, drag.target); return; }
      const r = live.lastByWizard.get(drag.id);
      if (r) setConfirm({ callId: r.callId, tier, name: allWizards().find((w) => w.id === drag.id)?.name ?? drag.id });
      return;
    }
    if (!dn?.id) { setCard(null); return; }
    const tn = s.mode === 'demo' ? s.demoT : now();
    const chat = s.mode === 'demo' ? s.demoChatter : live.chatter;
    if (dn.kind === 'wizard' || dn.kind === 'seer') s.pokes.set(dn.id, tn);
    if (dn.kind) chat.poke(dn.kind, dn.id, tn, { hubPoke: s.theme.words.hubPoke, lines: s.theme.words.lines });
    if (dn.kind === 'wizard' || dn.kind === 'tower') setCard({ kind: dn.kind, id: dn.id, x: p.cx, y: p.cy });
  };

  /** 演示里拖到楼上：那层的第一位走上栏杆接手 */
  const injectDemoRedo = (fromId: string, target: Floor) => {
    const s = st.current;
    const demo = buildDemo(s.theme.words);
    const to = demo.wizards.find((w) => w.floor === target);
    if (!to) return;
    const t0 = s.demoT;
    const tier = FLOOR_TIER[target]!;
    const ph = (type: Episode['ph'][number]['type'], a: number, b: number, d = {}) => ({ type, t0: t0 + a, t1: t0 + b, d });
    const from = [...demo.episodes].reverse().find((e) => modelAt(e, t0) === fromId && startOf(e) <= t0);
    s.demoExtra.push({ id: `redo-${t0}`, app: from?.app ?? demo.towers[0].id, model: to.id, kind: 'text', callIds: [], save: 0.21, sub: true,
      ph: [ph('judge', 0, 1.2, { text: s.theme.words.judge[tier] }), ph('go', 1.2, 2.4), ph('work', 2.4, 4), ph('stream', 4, 6.4), ph('done', 6.4, 7.6)] });
    toast(s.theme.words.say.redoing(s.theme.words.title[tier]));
  };

  const doRedo = async (c: Confirm) => {
    setConfirm(null);
    try {
      await api.escalate(c.callId).catch(() => undefined);
      await api.redo(c.callId, { tier: c.tier });
      toast(theme.words.say.redoing(theme.words.title[c.tier]));
    } catch (e) { toast(`${WORLD.redoFailed}：${e instanceof Error ? e.message : String(e)}`); }
  };

  const seekChapter = (i: number) => { st.current.demoT = DEMO_CHAPTERS[i][0]; st.current.demoExtra = []; st.current.demoChatter.reset(); setChapter(i); setPlaying(true); };
  const empty = mode === 'live' && data !== null && live.wizards.length === 0;
  const mascot = mascotSprite(effectiveId);

  return (
    <div className="world">
      <div className="ct" data-tauri-drag-region>
        <h4 data-tauri-drag-region>{t('nav.world')}</h4>
        <div className="tools">
          <button className={`btn sm ${showNames ? 'on' : ''}`} onClick={() => setShowNames((v) => !v)}>{WORLD.names}</button>
          {mode === 'demo' && <button className="btn sm" onClick={() => setPlaying((p) => !p)}>{playing ? WORLD.pause : WORLD.play}</button>}
          <Seg value={mode} onChange={(m) => { setMode(m); setCard(null); if (m === 'demo') { st.current.demoT = 0; st.current.demoExtra = []; st.current.demoChatter.reset(); setPlaying(true); } }} options={[{ id: 'live', label: WORLD.live }, { id: 'demo', label: WORLD.demo }]} />
        </div>
      </div>
      <div className="stage" ref={wrapRef}>
        <canvas ref={canvasRef} width={WW * 3} height={WH * 3} onPointerDown={onDown} onPointerMove={onMove} onPointerUp={onUp} onPointerLeave={() => { st.current.hover = undefined; }} />
        {card && <WorldCard card={card} mode={mode} theme={theme} onClose={() => setCard(null)} redoable={card.kind === 'wizard' && redoable(card.id)} />}
        {confirm && (
          <div className="wcard confirm">
            <div className="tt">{WORLD.redoTitle(theme.words.title[confirm.tier])}</div>
            <div className="ds">{WORLD.redoDesc(confirm.name)}</div>
            <div className="row mt8"><button className="btn pri sm" onClick={() => void doRedo(confirm)}>{WORLD.redo}</button><button className="btn sm" onClick={() => setConfirm(null)}>{WORLD.cancel}</button></div>
          </div>
        )}
        {empty && (
          <div className="wempty">
            {mascot && <Sprite def={mascot} scale={4} className="px" />}
            <div className="tt">{theme.words.empty.title}</div>
            <ul>
              <li><span className="ic">✦</span>{theme.words.empty.wizard}</li>
              <li><span className="ic">✦</span>{theme.words.empty.tower}</li>
            </ul>
            <div className="row">
              <button className="btn pri sm" onClick={() => onNavigate?.('accounts')}>{WORLD.addModel}</button>
              <button className="btn sm" onClick={() => setMode('demo')}>{WORLD.watchDemo}</button>
            </div>
          </div>
        )}
      </div>
      {mode === 'demo' && (
        <>
          <div className="scrub" onClick={(e) => { const r = e.currentTarget.getBoundingClientRect(); st.current.demoT = ((e.clientX - r.left) / r.width) * DEMO_LEN; st.current.demoExtra = []; st.current.demoChatter.reset(); }}><i ref={barRef} /></div>
          <div className="chapters">
            {DEMO_CHAPTERS.map(([, name], i) => <button key={name} className={`btn sm ${chapter === i ? 'on' : ''}`} onClick={() => seekChapter(i)}>{name}</button>)}
          </div>
        </>
      )}
      <div className="cap" ref={capRef} />
    </div>
  );

  function chatCtx(s: typeof st.current, base: Pick<ChatCtx, 't' | 'episodes' | 'wizards' | 'towers' | 'night' | 'quota' | 'sleeping'>, layout: Layout): ChatCtx {
    return { ...base, banter: s.theme.words.banter, hubPoke: s.theme.words.hubPoke, lines: s.theme.words.lines, floorOf: (id) => layout.wiz.get(id)?.floor };
  }

  function liveScene(s: typeof st.current): Scene {
    const d = ensureLive();
    const tn = now();
    d.prune(tn);
    const wizards = allWizards(), towers = allTowers();
    const key = `${live.version}|${s.theme.id}`;
    if (!s.layout || s.layoutKey !== key) { s.layout = layoutFor(wizards, towers, s.theme); s.layoutKey = key; }
    const dt = s.data;
    const subAccts = new Set((dt?.accounts ?? []).filter((a) => a.kind === 'subscription').map((a) => a.id));
    const subQ = (dt?.quotas ?? []).filter((q) => !q.sub && subAccts.has(q.account_id));
    const byAcct = new Map<string, { name: string; pct: number }>();
    for (const q of subQ) { const cur = byAcct.get(q.account_id); const name = (q.label || q.account_id).split(/\s+/)[0]; if (!cur || q.pct > cur.pct) byAcct.set(q.account_id, { name, pct: q.pct }); }
    const hour = new Date().getHours();
    const night = s.nightOverride ?? (hour >= 19 || hour < 6);
    const bottles = [...byAcct.values()].map((b) => ({ name: b.name, left: Math.max(0, Math.round(100 - b.pct)) }));
    const bubbles = live.chatter.tick(chatCtx(s, { t: tn, episodes: d.episodes, wizards, towers, night, quota: bottles.length ? Math.min(...bottles.map((b) => b.left)) : null, sleeping: s.sleeping }, s.layout));
    return {
      t: tn, theme: s.theme, bg: bgFor(s.theme), font: s.font, wizards, towers, episodes: d.episodes, layout: s.layout,
      night, rain: d.failures.length > 0, snow: subQ.some((q) => q.pct >= 80), paused: s.paused,
      bottles,
      saved: (dt?.saved ?? 0) + d.saved, money: usd,
      sleeping: s.sleeping, wakeAt: s.wakeAt, cooling: new Set(), exam: s.examSince ? { since: s.examSince } : null,
      pokes: s.pokes, hover: s.hover, drag: s.drag, coolSpan: 20, bubbles, showNames: s.showNames,
    };
  }
}

function demoScene(s: { demoT: number; demoExtra: Episode[]; demoChatter: Chatter; pokes: Map<string, number>; hover?: string; drag?: Scene['drag']; layout: Layout | null; layoutKey: string; theme: ThemeDef; font: boolean; showNames: boolean; nightOverride: boolean | null }): Scene {
  const t = s.demoT;
  const demo = buildDemo(s.theme.words);
  if (!s.layout || s.layoutKey !== `demo|${s.theme.id}`) { s.layout = layoutFor(demo.wizards, demo.towers, s.theme); s.layoutKey = `demo|${s.theme.id}`; }
  const asleep = t >= DEMO_SLEEP[0] && t < DEMO_SLEEP[1];
  const claude = demo.wizards.filter((w) => w.acct === 'claude_sub').map((w) => w.id);
  const q = demoQuota(t);
  const episodes = [...demo.episodes, ...s.demoExtra];
  const sleeping = new Set(asleep ? claude : []);
  const layout = s.layout;
  const bubbles = s.demoChatter.tick({ t, episodes, wizards: demo.wizards, towers: demo.towers, night: false, quota: q, sleeping, banter: s.theme.words.banter, hubPoke: s.theme.words.hubPoke, lines: s.theme.words.lines, floorOf: (id) => layout.wiz.get(id)?.floor });
  return {
    t, theme: s.theme, bg: bgFor(s.theme), font: s.font, wizards: demo.wizards, towers: demo.towers, episodes, layout,
    night: s.nightOverride ?? false, rain: episodes.some((e) => e.ph.some((p) => p.type === 'fail' && t >= p.t0 && t < p.t0 + 5.5)), snow: q < 35, paused: false,
    bottles: [{ name: 'Claude', left: q }, { name: 'ChatGPT', left: 64 }],
    saved: demoSaved(t, episodes), money: usd,
    sleeping, wakeAt: new Map(claude.map((id) => [id, DEMO_SLEEP[1]])), cooling: new Set(),
    exam: t >= DEMO_EXAM[0] && t < DEMO_EXAM[1] ? { since: DEMO_EXAM[0] } : null,
    pokes: s.pokes, hover: s.hover, drag: s.drag, coolSpan: 10, bubbles, showNames: s.showNames,
  };
}

/** 点开一位模型或一座站台：是谁、在哪层、什么性格、这会儿在干嘛 */
function WorldCard({ card, mode, theme, onClose, redoable }: { card: Card; mode: 'live' | 'demo'; theme: ThemeDef; onClose: () => void; redoable: boolean }) {
  const demo = buildDemo(theme.words);
  const wizards = mode === 'demo' ? demo.wizards : allWizards();
  const towers = mode === 'demo' ? demo.towers : allTowers();
  const eps = mode === 'demo' ? demo.episodes : live.director?.episodes ?? [];
  const tn = now();
  const style = { left: Math.max(8, card.x - 110), top: Math.max(8, card.y + 14) };
  if (card.kind === 'tower') {
    const tw = towers.find((x) => x.id === card.id);
    if (!tw) return null;
    const busy = mode === 'live' ? eps.filter((e) => e.app === tw.id && tn >= startOf(e) && tn < endOf(e)).length : 0;
    return (
      <div className="wcard" style={style} onPointerDown={(e) => e.stopPropagation()}>
        <div className="tt">{tw.name}</div>
        <div className="ds">{busy ? WORLD.towerBusy(busy) : WORLD.towerIdle}</div>
        <button className="btn link sm mt8" onClick={onClose}>{WORLD.close}</button>
      </div>
    );
  }
  const w = wizards.find((x) => x.id === card.id);
  if (!w) return null;
  const doing = mode === 'live' ? eps.find((e) => modelAt(e, tn) === w.id && tn >= startOf(e) && tn < endOf(e)) : undefined;
  const app = doing ? towers.find((x) => x.id === doing.app)?.name : undefined;
  const P = personalityOf(w.id);
  return (
    <div className="wcard" style={style} onPointerDown={(e) => e.stopPropagation()}>
      <div className="tt">{w.full}</div>
      <div className="ds">{theme.words.floor[w.floor]} · {LANE_NAME[w.lane]}</div>
      <div className="ds">{WORLD.personality(FACTION_NAME[P.faction], QUIRK_NAME[P.quirk])}</div>
      {app && <div className="ds">{WORLD.replying(app)}</div>}
      {redoable && <div className="ds hint-line">{WORLD.dragHint}</div>}
      <button className="btn link sm mt8" onClick={onClose}>{WORLD.close}</button>
    </div>
  );
}
