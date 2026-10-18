import { useEffect, useState, type ReactNode } from 'react';
import { REVEAL_LABEL } from '../copy';
import type { Tier } from '../api/types';
import { LANE_CHIP, LANE_LABEL, TIER_CHIP, laneGroup } from '../format';
import { useTheme } from '../theme/ThemeContext';
import { Mascot, TierSprite } from './Pixel';
import { desktop } from '../tauri';

export function Seg<T extends string>({ value, options, onChange, full }: { value: T; options: { id: T; label: string }[]; onChange: (v: T) => void; full?: boolean }) {
  return (
    <div className={`seg ${full ? 'full' : ''}`}>
      {options.map((o) => <button key={o.id} className={o.id === value ? 'on' : ''} onClick={() => onChange(o.id)}>{o.label}</button>)}
    </div>
  );
}

export const Toggle = ({ on, onChange, disabled }: { on: boolean; onChange: (v: boolean) => void; disabled?: boolean }) => (
  <button className={`tog ${on ? '' : 'off'}`} disabled={disabled} onClick={() => onChange(!on)} aria-pressed={on} />
);

export const LaneChip = ({ lane }: { lane: string }) => { const g = laneGroup(lane); return <span className={`chip ${LANE_CHIP[g]}`}>{LANE_LABEL[g]}</span>; };
export const GroupChip = ({ group }: { group: 'sub' | 'api' | 'media' }) => <span className={`chip ${LANE_CHIP[group]}`}>{LANE_LABEL[group]}</span>;
export function TierChip({ tier }: { tier: Tier | null | undefined }) {
  const { t } = useTheme();
  if (!tier) return <GroupChip group="media" />;
  return <span className={`chip ${TIER_CHIP[tier]}`}><TierSprite tier={tier} scale={1.4} />{t(`tier.${tier}`)}</span>;
}

export const Bar = ({ pct, tone }: { pct: number; tone?: 'blue' | 'amber' | 'gold' }) => (
  <div className={`bar ${tone ?? ''}`}><span style={{ width: `${Math.max(0, Math.min(100, pct))}%` }} /></div>
);

export const Dot = ({ s }: { s: 'ok' | 'warn' | 'no' | 'err' }) => <span className={`dot ${s}`} />;

export function SetRow({ title, desc, children, last }: { title: string; desc?: ReactNode; children?: ReactNode; last?: boolean }) {
  return (
    <div className={`setrow ${last ? 'last' : ''}`}>
      <div><div className="tt">{title}</div>{desc && <div className="ds">{desc}</div>}</div>
      <span className="row">{children}</span>
    </div>
  );
}

export const Empty = ({ children }: { children: ReactNode }) => <div className="empty"><Mascot scale={3} /><div>{children}</div></div>;

/** 错误三段式：界面上只显示一行，原文折叠 */
export function ErrorLine({ message, detail }: { message: string; detail?: string }) {
  return (
    <div className="banner err">
      <div className="grow">
        {message}
        {detail && <details className="raw"><summary>详情</summary><pre>{detail}</pre></details>}
      </div>
    </div>
  );
}

let toastFn: ((msg: string) => void) | null = null;
export const toast = (msg: string) => toastFn?.(msg);
export function ToastHost() {
  const [msg, setMsg] = useState<string | null>(null);
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    toastFn = (m) => { setMsg(m); if (timer) clearTimeout(timer); timer = setTimeout(() => setMsg(null), 1800); };
    return () => { toastFn = null; if (timer) clearTimeout(timer); };
  }, []);
  return msg ? <div className="toast">{msg}</div> : null;
}

export function Modal({ title, onClose, children, width }: { title: string; onClose: () => void; children: ReactNode; width?: number }) {
  useEffect(() => {
    const k = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', k);
    return () => window.removeEventListener('keydown', k);
  }, [onClose]);
  return (
    <div className="modal-bg" onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="modal" style={width ? { width } : undefined}>
        <div className="tb">{title}</div>
        <div className="body">{children}</div>
      </div>
    </div>
  );
}

/** 同一个动作在哪儿都叫这一个名字 */
export const RevealButton = ({ path, disabled }: { path?: string; disabled?: boolean }) => (
  <button className="btn sm" disabled={disabled || !path} onClick={() => path && desktop.revealInFinder(path)}>{REVEAL_LABEL}</button>
);
