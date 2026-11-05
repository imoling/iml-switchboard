import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { api } from '../api/client';
import type { Call } from '../api/types';
import appMark from '../assets/app-mark.svg';
import { cost, HOST_LABEL, num } from '../format';
import { useLoad } from '../hooks';
import { useTheme } from '../theme/ThemeContext';
import { callColor, channelNodes, channelOf, hostNodes, type Link, linkPath, linkPoint, linkWidth, modelCount, rowY } from '../topology';
import { Dot } from './ui';
import { accountIcon, Vendor } from './Vendor';

const ROW = 30; // 每个节点占的高度
const NODE_W = 148;
const HUB_W = 168;
const IN_MS = 420; // 宿主到中间
const HOLD_MS = 110; // 中间停一下：这一下就是「挑模型」
const OUT_MS = 480; // 中间到渠道
const MAX_DOTS = 6; // Codex 一轮能连发十几个请求，同时在路上的光点封顶
const REPLAY = 6; // 打开页面时把最近几次真实记录走一遍

type Flight = { start: number; segs: { link: Link | null; ms: number }[]; dots: SVGCircleElement[]; end: number };

const reducedMotion = () => window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false;

/** 全貌：谁来派活、谁去干活。光点是真实的调用，空闲时不动 */
/** bare：嵌在总览那条接入栏的卡片里，不自带边框和标题行 */
export function Topology({ live, onNavigate, bare }: { live?: Call | null; onNavigate?: (page: 'integrations' | 'accounts') => void; bare?: boolean }) {
  const { t } = useTheme();
  const { data } = useLoad(() => Promise.all([api.integrations(), api.accounts(), api.calls({ limit: 200 })]), []);
  const hosts = useMemo(() => (data ? hostNodes(data[0]) : []), [data]);
  const channels = useMemo(() => (data ? channelNodes(data[1]) : []), [data]);
  const recent = useMemo(() => data?.[2] ?? [], [data]);

  const boxRef = useRef<HTMLDivElement>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const hubRef = useRef<HTMLDivElement>(null);
  const links = useRef(new Map<string, Link>());
  const nodes = useRef(new Map<string, HTMLElement>());
  const flying = useRef<Flight[]>([]);
  const raf = useRef(0);
  const seen = useRef(new Set<string>());
  const liveAtMount = useRef(live?.id);
  const [width, setWidth] = useState(0);
  const [last, setLast] = useState<Call | null>(null);

  useLayoutEffect(() => {
    const el = boxRef.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setWidth(el.clientWidth));
    ro.observe(el);
    setWidth(el.clientWidth);
    return () => ro.disconnect();
  }, [data]);

  const flash = useCallback((el: HTMLElement | null | undefined, color: string) => {
    el?.animate?.([{ boxShadow: `0 0 0 0 ${color}` }, { boxShadow: '0 0 0 5px transparent' }], { duration: 420, easing: 'ease-out' });
  }, []);

  const tick = useCallback((now: number) => {
    flying.current = flying.current.filter((d) => {
      const at = (ms: number) => {
        let left = ms;
        for (const s of d.segs) {
          if (left <= s.ms) return s.link ? linkPoint(s.link, left / s.ms) : null; // 没有连线就是藏在中间那一下
          left -= s.ms;
        }
        return null;
      };
      const e = now - d.start;
      d.dots.forEach((c, i) => {
        const p = e - i * 26 >= 0 ? at(e - i * 26) : null; // 后面两颗跟着走，看起来是一道拖尾
        c.style.display = p ? '' : 'none';
        if (p) { c.setAttribute('cx', String(p.x)); c.setAttribute('cy', String(p.y)); }
      });
      if (e < d.end + 60) return true;
      d.dots.forEach((c) => c.remove());
      return false;
    });
    raf.current = flying.current.length ? requestAnimationFrame(tick) : 0;
  }, []);

  /** 放一颗光点：从宿主经中间到渠道。找不到宿主（快速面板、命令行）就从中间出发，失败且没选到渠道就停在中间 */
  const fire = useCallback((c: Call, accounts: typeof channels) => {
    setLast(c);
    if (reducedMotion() || flying.current.length >= MAX_DOTS || !svgRef.current) return;
    const color = callColor(c);
    const channel = channelOf(c, accounts);
    const from = c.host ? links.current.get(`h:${c.host}`) : undefined;
    const to = channel ? links.current.get(`c:${channel.id}`) : undefined;
    if (!from && !to) return;
    const dots = [3.6, 2.8, 2].map((r, i) => {
      const el = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
      el.setAttribute('r', String(r));
      el.style.fill = color;
      el.style.opacity = String(1 - i * 0.3);
      el.style.display = 'none';
      svgRef.current!.appendChild(el);
      return el;
    });
    const inMs = from ? IN_MS : 0;
    const outMs = to ? OUT_MS : 0;
    const hold = from && to ? HOLD_MS : 0;
    if (from) flash(nodes.current.get(`h:${c.host}`), color);
    window.setTimeout(() => flash(hubRef.current, color), inMs);
    if (to && channel) window.setTimeout(() => flash(nodes.current.get(`c:${channel.id}`), color), inMs + hold + outMs);
    const segs: Flight['segs'] = [];
    if (from) segs.push({ link: from, ms: IN_MS });
    if (hold) segs.push({ link: null, ms: hold });
    if (to) segs.push({ link: to, ms: OUT_MS });
    flying.current.push({ start: performance.now(), segs, dots, end: inMs + hold + outMs });
    if (!raf.current) raf.current = requestAnimationFrame(tick);
  }, [flash, tick]);

  useEffect(() => () => {
    cancelAnimationFrame(raf.current);
    flying.current.forEach((d) => d.dots.forEach((c) => c.remove()));
    flying.current = [];
  }, []);

  // 打开页面时把最近几次真实记录走一遍，空闲时图是静止的
  useEffect(() => {
    if (!data || width === 0) return;
    recent.forEach((c) => seen.current.add(c.id));
    const list = recent.slice(0, REPLAY).reverse();
    if (reducedMotion()) { setLast(recent[0] ?? null); return; }
    const timers = list.map((c, i) => window.setTimeout(() => fire(c, channels), 500 + i * 380));
    return () => timers.forEach(clearTimeout);
  }, [data, width === 0]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!live || live.id === liveAtMount.current || seen.current.has(live.id)) return;
    seen.current.add(live.id);
    fire(live, channels);
  }, [live, channels, fire]);

  if (!data) return <div className={bare ? 'topo-wait bare' : 'panel topo-wait'} />;

  const height = Math.max(3, hosts.length, channels.length) * ROW;
  const hubY = height / 2;
  const hubL = width / 2 - HUB_W / 2;
  const hubR = width / 2 + HUB_W / 2;
  const maxHost = Math.max(0, ...hosts.map((h) => h.calls_7d ?? 0));
  const week = Date.now() - 7 * 864e5;
  const load = (id: string) => recent.filter((c) => new Date(c.ts).getTime() >= week && channelOf(c, channels)?.id === id).length;
  const maxChannel = Math.max(0, ...channels.map((a) => load(a.id)));
  const subs = channels.filter((a) => a.kind === 'subscription').length;
  const lastCost = last ? cost(last) : null;

  const hostLinks = hosts.map((_, i): Link => ({ x1: NODE_W, y1: rowY(i, hosts.length, height), x2: hubL, y2: hubY }));
  const channelLinks = channels.map((_, i): Link => ({ x1: hubR, y1: hubY, x2: width - NODE_W, y2: rowY(i, channels.length, height) }));
  links.current = new Map([...hosts.map((h, i) => [`h:${h.id}`, hostLinks[i]] as const), ...channels.map((a, i) => [`c:${a.id}`, channelLinks[i]] as const)]);
  const keepNode = (key: string) => (el: HTMLElement | null) => { if (el) nodes.current.set(key, el); else nodes.current.delete(key); };

  return (
    <div className={bare ? 'topo bare' : 'panel topo'}>
      {!bare && (
        <div className="topo-head">
          <span>已接入 {hosts.length} 个</span>
          <span>渠道 {channels.length} 个<em> · 订阅 {subs} · 按量付费 {channels.length - subs}</em></span>
        </div>
      )}
      <div className="topo-map" ref={boxRef} style={{ height }}>
        {width > 0 && (
          <svg ref={svgRef} width={width} height={height}>
            {hosts.map((h, i) => (
              <path key={h.id} pathLength={1} className={(h.calls_7d ?? 0) === 0 ? 'idle' : undefined} strokeWidth={linkWidth(h.calls_7d ?? 0, maxHost)} d={linkPath(hostLinks[i])} />
            ))}
            {channels.map((a, i) => (
              <path key={a.id} pathLength={1} className={load(a.id) === 0 ? 'idle' : undefined} strokeWidth={linkWidth(load(a.id), maxChannel)} d={linkPath(channelLinks[i])} />
            ))}
          </svg>
        )}
        {hosts.length === 0 && <button className="btn sm topo-empty" style={{ left: 0, top: hubY - 12 }} onClick={() => onNavigate?.('integrations')}>接入一个智能体</button>}
        {hosts.map((h, i) => (
          <button key={h.id} ref={keepNode(`h:${h.id}`)} className={`topo-node ${(h.calls_7d ?? 0) === 0 ? 'quiet' : ''}`} style={{ left: 0, top: rowY(i, hosts.length, height) - 12 }}
            title={h.calls_7d ? `近 7 天 ${num(h.calls_7d)} 次` : '近 7 天没派活'} onClick={() => onNavigate?.('integrations')}>
            <Vendor provider={h.id} /><span>{HOST_LABEL[h.id] ?? h.id}</span>
          </button>
        ))}
        <div className="topo-hub" ref={hubRef} style={{ left: hubL, top: hubY - 26, width: HUB_W }}>
          <img src={appMark} alt="" />
          <div><b>{t('app.title')}</b><div className="hint">托管 {modelCount(data[1])} 个型号</div></div>
        </div>
        {last && <div className="topo-last hint" style={{ left: hubL - 16, top: hubY + 32, width: HUB_W + 32 }}>{last.ok ? `${last.task_name ?? last.task_type} → ${last.model_display ?? last.model} · ${lastCost?.text}` : `${last.task_name ?? last.task_type} 失败`}</div>}
        {channels.length === 0 && <button className="btn sm topo-empty" style={{ right: 0, top: hubY - 12 }} onClick={() => onNavigate?.('accounts')}>添加模型账号</button>}
        {channels.map((a, i) => (
          <button key={a.id} ref={keepNode(`c:${a.id}`)} className="topo-node" style={{ left: width - NODE_W, top: rowY(i, channels.length, height) - 12 }} title={a.detail} onClick={() => onNavigate?.('accounts')}>
            <Vendor provider={accountIcon(a)} /><span>{a.label}</span>
            {a.status !== 'ok' && <Dot s={a.status === 'warn' || a.status === 'cooldown' ? 'warn' : 'err'} />}
          </button>
        ))}
      </div>
    </div>
  );
}
