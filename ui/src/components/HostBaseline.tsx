import { useState } from 'react';
import { api, ApiError } from '../api/client';
import type { AppBaseline, IntegrationId, Price } from '../api/types';
import { ErrorLine, SetRow, toast } from './ui';
import { errText } from '../hooks';

const money = (p: Price, v?: number) => `${p.currency === 'CNY' ? '¥' : '$'}${+(v ?? 0).toFixed(4)}`;

/** 应用原价：不经过 Switchboard、应用自己调时按哪个型号、什么单价算；省下的钱 = 原价 − 实际花费 */
export function HostBaselineRow({ host, value, onSaved }: { host: IntegrationId; value: AppBaseline; onSaved: (b: AppBaseline) => void }) {
  const [editing, setEditing] = useState(false);
  const [err, setErr] = useState<ApiError | null>(null);
  const [draft, setDraft] = useState({ model: '', input: '', output: '', cached: '', currency: 'USD' as 'USD' | 'CNY' });
  const open = () => {
    const p = value.source === 'custom' ? value.price : undefined;
    setDraft({ model: value.model === value.default_model ? '' : value.model, input: p ? String(p.input_per_m) : '', output: p ? String(p.output_per_m) : '', cached: p?.cache_read_per_m ? String(p.cache_read_per_m) : '', currency: p?.currency ?? 'USD' });
    setErr(null); setEditing(true);
  };
  const save = async (reset = false) => {
    const num = (s: string) => (s.trim() === '' ? 0 : Number(s));
    const price: Price = { input_per_m: num(draft.input), output_per_m: num(draft.output), cache_read_per_m: num(draft.cached), currency: draft.currency };
    if (!reset && [price.input_per_m, price.output_per_m, price.cache_read_per_m].some((v) => Number.isNaN(v) || (v ?? 0) < 0)) { setErr(new ApiError(400, '单价要填不小于 0 的数字')); return; }
    try {
      const b = await api.setBaseline(host, reset ? { model: '' } : { model: draft.model.trim(), price });
      onSaved(b); setEditing(false);
      toast(b.recomputed ? `已按新单价重算 ${b.recomputed} 条记录` : '已保存');
    } catch (e) { setErr(e as ApiError); }
  };
  const p = value.price;
  return (
    <>
      <SetRow title="原价按" desc="算省下多少钱用；没单价的记录不算省">
        <span className={`hint ${value.source === 'none' ? 'warn-text' : ''}`}>
          {value.display}{!p ? ' · 未设单价' : p.input_per_m === p.output_per_m && (p.cache_read_per_m ?? 0) === p.input_per_m ? ` · ${money(p, p.input_per_m)} / 百万 token` : ` · 输入 ${money(p, p.input_per_m)} · 输出 ${money(p, p.output_per_m)} / 百万 token`}
        </span>
        <button className="btn sm" onClick={open}>调整</button>
      </SetRow>
      {editing && (
        <div className="panel soft mt8">
          {err && <ErrorLine {...errText(err)!} />}
          <div className="row wrap" style={{ gap: 8 }}>
            <input className="input sm" style={{ width: 160 }} placeholder={value.default_model} value={draft.model} onChange={(e) => setDraft({ ...draft, model: e.target.value })} aria-label="型号" />
            <input className="input sm" style={{ width: 90 }} placeholder="输入单价" inputMode="decimal" value={draft.input} onChange={(e) => setDraft({ ...draft, input: e.target.value })} aria-label="输入单价" />
            <input className="input sm" style={{ width: 90 }} placeholder="输出单价" inputMode="decimal" value={draft.output} onChange={(e) => setDraft({ ...draft, output: e.target.value })} aria-label="输出单价" />
            <input className="input sm" style={{ width: 90 }} placeholder="缓存单价" inputMode="decimal" value={draft.cached} onChange={(e) => setDraft({ ...draft, cached: e.target.value })} aria-label="缓存单价" />
            <select className="sel" value={draft.currency} onChange={(e) => setDraft({ ...draft, currency: e.target.value as 'USD' | 'CNY' })} aria-label="币种"><option value="USD">美元</option><option value="CNY">人民币</option></select>
            <span className="hint">每百万 token；单价留空用内置价目表</span>
          </div>
          <div className="row mt8" style={{ gap: 8 }}>
            <button className="btn pri sm" onClick={() => save()}>保存并重算</button>
            {(value.source === 'custom' || value.model !== value.default_model) && <button className="btn sm" onClick={() => save(true)}>回到默认</button>}
            <button className="btn sm" onClick={() => setEditing(false)}>取消</button>
          </div>
        </div>
      )}
    </>
  );
}
