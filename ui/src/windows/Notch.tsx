import { useEffect, useRef, useState } from 'react';
import { subscribeEvents } from '../api/client';
import appMark from '../assets/app-mark.svg';
import { vendorSrc } from '../components/Vendor';
import { NOTCH_IDLE, notchDone, notchDrop, notchStart, notchTone, type NotchState } from '../notch';
import { NotchFx } from '../notchFx';
import { desktop } from '../tauri';

const HOLD_MS = 1500; // 没有新请求多久后收回
const CALM_MS = 6000; // 跑了这么久还没完就放慢：长任务不该一直抢眼
const STALE_MS = 11 * 60 * 1000; // 门面最长等 10 分钟，再久就是完成事件丢了
const BAND = 13; // 刘海下方能量带的高度

const reducedMotion = () => window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false;

/** 刘海动画（实验功能）：左耳是智能体，右耳是模型，一次请求是一来一回。窗口鼠标穿透，只负责画 */
export function Notch() {
  const [size, setSize] = useState(() => {
    const q = new URLSearchParams(location.search);
    return { w: Number(q.get('w')) || 208, h: Number(q.get('h')) || 37.5 };
  });
  const [state, setState] = useState<NotchState>(NOTCH_IDLE);
  const [open, setOpen] = useState(false);
  const [calm, setCalm] = useState(false);
  const [shake, setShake] = useState(0);
  const stale = useRef(new Map<string, number>());
  const canvas = useRef<HTMLCanvasElement>(null);
  const fx = useRef<NotchFx | null>(null);
  const sizeRef = useRef(size);
  sizeRef.current = size;
  const stateRef = useRef(state);
  stateRef.current = state;

  useEffect(() => {
    const el = canvas.current;
    if (!el || reducedMotion()) return;
    const fit = () => {
      const dpr = window.devicePixelRatio || 1;
      el.width = el.clientWidth * dpr;
      el.height = el.clientHeight * dpr;
    };
    fit();
    window.addEventListener('resize', fit);
    fx.current = new NotchFx(el, () => {
      const { w, h } = sizeRef.current;
      return { width: w, height: h, centerX: el.clientWidth / 2, band: BAND, logoY: h / 2 };
    });
    return () => { window.removeEventListener('resize', fit); fx.current?.stop(); fx.current = null; };
  }, []);

  useEffect(() => {
    const clear = (id: string) => { clearTimeout(stale.current.get(id)); stale.current.delete(id); };
    const off = subscribeEvents((e) => {
      if (e.type === 'call_start') {
        const f = e.data;
        clear(f.id);
        stale.current.set(f.id, window.setTimeout(() => setState((s) => notchDrop(s, f.id)), STALE_MS));
        const next = notchStart(stateRef.current, f);
        stateRef.current = next;
        setState(next);
        fx.current?.request(notchTone(true, f.lane));
      }
      if (e.type === 'call' && e.data.call) {
        const c = e.data.call;
        clear(c.id);
        const next = notchDone(stateRef.current, c);
        stateRef.current = next;
        setState(next);
        fx.current?.reply(notchTone(c.ok, c.lane), c.ok, !!c.lane);
        if (!c.ok) setShake((n) => n + 1);
      }
    });
    return () => { off(); stale.current.forEach((t) => clearTimeout(t)); };
  }, []);

  useEffect(() => {
    let un = () => {};
    void desktop.onNotchSize((w, h) => setSize({ w, h })).then((u) => { un = u; });
    return () => un();
  }, []);

  const busy = state.active.length > 0;
  const shown = state.shown;
  useEffect(() => {
    if (!shown) return;
    if (busy) { void desktop.placeNotch(); setOpen(true); return; }
    setOpen(true);
    const t = window.setTimeout(() => setOpen(false), HOLD_MS);
    return () => clearTimeout(t);
  }, [busy, shown]);

  useEffect(() => {
    setCalm(false);
    if (!busy) return;
    const t = window.setTimeout(() => setCalm(true), CALM_MS);
    return () => clearTimeout(t);
  }, [busy]);

  const tone = shown ? notchTone(state.ok, shown.lane) : 'transparent';
  const vars = { '--nw': `${size.w}px`, '--nh': `${size.h}px`, '--band': `${calm ? 5 : BAND}px`, '--tone': tone } as React.CSSProperties;
  return (
    <div className={`notch-win ${open ? 'open' : ''} ${busy ? 'busy' : ''} ${calm ? 'calm' : ''}`} style={vars}>
      <div key={shake} className={`notch ${shake ? 'shake' : ''}`}>
        <span className="ear left"><img src={vendorSrc(shown?.host) ?? appMark} alt="" /></span>
        <span className="ear right"><img src={vendorSrc(shown?.provider) ?? appMark} alt="" /></span>
      </div>
      <canvas ref={canvas} />
    </div>
  );
}
