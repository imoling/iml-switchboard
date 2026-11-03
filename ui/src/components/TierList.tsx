import type { TierPick } from '../api/types';
import { moveItem, toggleId } from '../hostModels';
import { Vendor } from './Vendor';

/** 一档的候选：勾上的按顺序在前，第一个标「先试」，其余在「不用」里；全局的档位候选和宿主自己的表都用它 */
export function TierList({ picks, sel, onChange }: { picks: TierPick[]; sel: string[]; onChange: (ids: string[]) => void }) {
  const byId = new Map(picks.map((p) => [p.id, p]));
  const chosen = sel.map((id) => byId.get(id)).filter((p): p is TierPick => !!p);
  const rest = picks.filter((p) => !sel.includes(p.id));
  const row = (p: TierPick, i: number | null) => (
    <div key={p.id} className="hm-row">
      <input type="checkbox" checked={i !== null} disabled={i !== null && chosen.length === 1} onChange={() => onChange(toggleId(sel, p.id))} />
      <Vendor provider={p.icon} model={p.model} />
      <span className="grow">{p.name}<span className="muted">　{p.group}</span>{p.note && <span className="hint">　{p.note}</span>}</span>
      {i === 0 && <span className="chip hm-def">先试</span>}
      {i !== null && <>
        <button className="hm-mv" disabled={i === 0} aria-label="上移" onClick={() => onChange(moveItem(sel, i, i - 1))}>↑</button>
        <button className="hm-mv" disabled={i === chosen.length - 1} aria-label="下移" onClick={() => onChange(moveItem(sel, i, i + 1))}>↓</button>
      </>}
    </div>
  );
  return (
    <>
      {chosen.map((p, i) => row(p, i))}
      {rest.length > 0 && <div className="hm-group"><div className="hint">不用</div>{rest.map((p) => row(p, null))}</div>}
    </>
  );
}
