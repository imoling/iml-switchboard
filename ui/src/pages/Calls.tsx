import { useEffect, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { Call } from '../api/types';
import { Empty, ErrorLine, LaneChip, Pager, TierChip, toast } from '../components/ui';
import { Model, Vendor } from '../components/Vendor';
import { cost, hhmm, hostLabel, secs, tokens, unitPrice, usd } from '../format';
import { errText, useDebounced, useLoad } from '../hooks';
import { useTheme } from '../theme/ThemeContext';

/** 把路由层的长错误压成一眼能看懂的一句：去掉「没有可用的模型：最后一个错误是」这种前缀 */
function shortError(e?: string): string {
  if (!e) return '';
  const t = e.replace(/^没有可用的模型[：:]\s*(最后一个错误是\s*)?/, '').replace(/\s+/g, ' ');
  return t.length > 40 ? t.slice(0, 40) + '…' : t;
}

const ROW_H = 34; // 一行的高度（7 + 7 内边距、19 行高、1 分隔线），用来按窗口高度算一页放几条

/** 记录页：一页正好铺满窗口，不出滚动条；点一行在右侧浮出详情 */
export function Calls({ live }: { live?: Call | null }) {
  const { t } = useTheme();
  const [q, setQ] = useState('');
  const [source, setSource] = useState('');
  const [lane, setLane] = useState('');
  const dq = useDebounced(q, 250);
  const [sel, setSel] = useState<Call | null>(null);
  const [failedOnly, setFailedOnly] = useState(false);
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(0);
  const boxRef = useRef<HTMLDivElement>(null);
  // 表格区多高就放几行：窗口拉高拉低时保住当前看到的第一条
  useEffect(() => {
    const el = boxRef.current;
    if (!el) return;
    const fit = () => {
      const n = Math.max(5, Math.floor((el.clientHeight - ROW_H) / ROW_H));
      setSize((old) => {
        if (old && old !== n) setPage((p) => Math.floor(((p - 1) * old) / n) + 1);
        return n;
      });
    };
    fit();
    const ro = new ResizeObserver(fit);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  useEffect(() => { setPage(1); }, [dq, source, lane, failedOnly]);
  const { data: pg, setData: setPg, error } = useLoad(() => (size ? api.callsPage({ q: dq, source, lane, failed: failedOnly, page, size }) : Promise.resolve(null)), [dq, source, lane, failedOnly, page, size]);
  const rows = pg?.items;

  // 事件流里来的新记录：只在第一页、没有筛选时插到顶上
  useEffect(() => {
    if (!live || page !== 1 || dq || source || lane || failedOnly) return;
    setPg((old) => (old && !old.items.some((c) => c.id === live.id) ? { ...old, items: [live, ...old.items].slice(0, size), total: old.total + 1 } : old));
  }, [live, setPg]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!sel) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape' && !e.defaultPrevented) setSel(null); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [sel]);

  const update = (c: Call) => { setPg((old) => (old ? { ...old, items: old.items.map((x) => (x.id === c.id ? c : x)) } : old)); setSel(c); };

  return (
    <div className="calls-page">
      <div className="ct" data-tauri-drag-region>
        <h4 data-tauri-drag-region>{t('nav.calls')}</h4>
        <div className="tools">
          <input className="input sm" style={{ width: 180 }} placeholder="搜索内容或模型" value={q} onChange={(e) => setQ(e.target.value)} />
          <select className="sel" value={source} onChange={(e) => setSource(e.target.value)}>
            <option value="">全部来源</option><option value="codex">Codex</option><option value="claude_code">Claude Code</option><option value="hermes">Hermes</option><option value="workbuddy">WorkBuddy</option><option value="winwork">WinWork</option><option value="quick">快速面板</option><option value="cli">命令行</option>
          </select>
          <select className="sel" value={lane} onChange={(e) => setLane(e.target.value)}>
            <option value="">全部渠道</option><option value="sub">订阅</option><option value="api">按量付费</option><option value="media">媒体</option>
          </select>
          <label className="hint" style={{ whiteSpace: 'nowrap' }}><input type="checkbox" checked={failedOnly} onChange={(e) => setFailedOnly(e.target.checked)} /> 只看失败</label>
        </div>
      </div>
      {error && <ErrorLine {...errText(error)!} />}
      <div className="calls-box" ref={boxRef}>
        {rows && rows.length === 0 && <Empty>{failedOnly ? '这段时间没有失败的请求' : '还没有请求；在 Claude Code 里让它画张图试试'}</Empty>}
        {rows && rows.length > 0 && (
          <table className="t calls">
            <colgroup><col className="c-time" /><col className="c-host" /><col className="c-task" /><col className="c-tier" /><col /><col className="c-use" /><col className="c-cost" /><col className="c-price" /><col className="c-base" /></colgroup>
            <thead><tr><th>时间</th><th>来源</th><th>任务</th><th>档位</th><th>{t('model')}</th><th className="right">用量</th><th className="right">费用</th><th className="right">单价</th><th className="right">原价</th></tr></thead>
            <tbody>
              {rows.map((c) => {
                const m = cost(c);
                const free = c.ok && c.lane === 'api' && c.cost_usd === 0;
                return (
                  <tr key={c.id} className={`clickable ${sel?.id === c.id ? 'sel' : ''}`} onClick={() => setSel(sel?.id === c.id ? null : c)}>
                    <td>{hhmm(c.ts)}</td>
                    <td className="ell"><Vendor provider={c.host ?? c.source} />{hostLabel(c)}</td>
                    <td className="ell">{c.task_name ?? c.task_type}{c.tier === 'top' && c.source !== 'redo' ? ' · 仔细想' : ''}</td>
                    <td>{c.kind === 'text' && !c.tier ? <span className="chip" title="应用点名的型号，不在任何一档里">指定</span> : <TierChip tier={c.tier} />}</td>
                    <td className="ell">{c.ok
                      ? <><Model provider={c.provider} name={c.model_display ?? c.model} />{c.fallback_from && <> <span className="chip c-warn">已降级</span></>}</>
                      : <><span className="chip c-hi">失败</span> <span className="muted">{shortError(c.error)}</span></>}</td>
                    <td className="right num muted" title={usageTitle(c)}>{usage(c)}</td>
                    <td className={`right num ${m.muted || free || !c.ok ? 'muted' : ''}`}>{!c.ok ? '—' : free ? '免费' : m.text}</td>
                    <td className="right num muted" title={priceTitle(c)}>{!c.ok ? '—' : price(c)}</td>
                    <td className="right num" title={c.baseline_display ? `按 ${c.baseline_display} 算` : undefined}>{!c.ok ? '—' : c.baseline_unpriced ? <span className="muted">未设单价</span> : usd(c.baseline_cost_usd)}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
        {sel && (
          <div className="drawer float">
            <button className="btn link drawer-x" aria-label="关闭" onClick={() => setSel(null)}>×</button>
            <Drawer call={sel} onUpdate={update} />
          </div>
        )}
      </div>
      {pg && size > 0 && <Pager page={page} total={pg.total} size={size} onChange={(p) => { setPage(p); setSel(null); }} />}
    </div>
  );
}

function Drawer({ call: c, onUpdate }: { call: Call; onUpdate: (c: Call) => void }) {
  const { t } = useTheme();
  const [busy, setBusy] = useState<string | null>(null);
  const [err, setErr] = useState<ApiError | null>(null);
  const [escalations, setEscalations] = useState<number | null>(null);
  const m = cost(c);
  const isOpus = /opus/i.test(c.model);
  const redoLabel = c.tier === 'top' ? null : isOpus || c.tier === 'high' ? '用 Fable 重做' : `用${t('tier.high')}重做`;

  const escalate = async () => {
    setBusy('esc'); setErr(null);
    try { const r = await api.escalate(c.id); setEscalations(r.escalations_for_task); onUpdate({ ...c, user_escalated: true }); }
    catch (e) { setErr(e as ApiError); } finally { setBusy(null); }
  };
  const redo = async () => {
    setBusy('redo'); setErr(null);
    try { const r = await api.redo(c.id, { tier: isOpus || c.tier === 'high' ? 'top' : 'high' }); toast(r.job_id ? '已加入媒体库' : '已重做，新记录在顶部'); onUpdate(c); }
    catch (e) { setErr(e as ApiError); } finally { setBusy(null); }
  };

  return (
    <div className="drawer-body">
      <h5>{c.task_type} · {c.tier ? t(`tier.${c.tier}`) : c.kind === 'text' ? '指定型号' : '媒体'}</h5>
      <div className="hint">{hhmm(c.ts)} · 来自 {hostLabel(c)} · 耗时 {secs(c.latency_ms)}</div>
      <div className="kv">
        <span>{t('model')}</span><span><Model provider={c.provider} name={c.model} /> <LaneChip lane={c.lane} /></span>
        {c.kind === 'text'
          ? <><span>用量</span><span>输入 {tokens(c.input_tokens)} · 输出 {tokens(c.output_tokens)}</span></>
          : <><span>用量</span><span>{c.units ?? 1} {c.unit === 'second' ? '秒' : '张'}</span></>}
        <span>费用</span><span>{m.muted ? `订阅额度${c.quota_units ? ` ${c.quota_units} 份` : ''}` : m.text}</span>
        <span>原价</span><span>{c.baseline_unpriced ? `未设单价（${c.baseline_display ?? c.baseline_model}），到应用页填` : `${usd(c.baseline_cost_usd)}${c.baseline_display ? `，按 ${c.baseline_display} 算` : ''}`}</span>
        {c.result_path && <><span>文件</span><span style={{ wordBreak: 'break-all', whiteSpace: 'normal' }}>{c.result_path}</span></>}
      </div>
      {!c.ok && c.error && <div className="banner err">{c.error}</div>}
      <div className="why"><b>为什么这样选</b>
        <ol>
          {c.reason.map((r, i) => <li key={i}>{r}</li>)}
        </ol>
      </div>
      {c.prompt_excerpt && <div className="why"><b>内容</b><div className="muted" style={{ marginTop: 4, whiteSpace: 'normal', userSelect: 'text' }}>{c.prompt_excerpt}</div></div>}
      {err && <ErrorLine {...errText(err)!} />}
      {c.kind === 'text' && (
        <div className="row mt12">
          <button className="btn" disabled={!!busy || c.user_escalated} onClick={escalate}>{c.user_escalated ? '已标记' : t('escalate')}</button>
          {redoLabel && <button className="btn" disabled={!!busy} onClick={redo}>{busy === 'redo' ? '正在重做…' : redoLabel}</button>}
        </div>
      )}
      {c.kind === 'text' && (
        <div className="hint" style={{ marginTop: 6 }}>
          {escalations !== null ? `已记一次，这类任务共标记 ${escalations} 次` : '标记后会计入手动升档率，攒够 5 次会建议调高这一类'}
        </div>
      )}
    </div>
  );
}

/** 用量：文字是「输入 / 输出」token，媒体是张数或秒数 */
function usage(c: Call): string {
  if (!c.ok) return '—';
  if (c.kind !== 'text') return c.units ? `${c.units} ${c.unit === 'second' ? '秒' : '张'}` : '—';
  if (!c.input_tokens && !c.output_tokens) return '—';
  return `${tokens(c.input_tokens ?? 0)} / ${tokens(c.output_tokens ?? 0)}`;
}
function usageTitle(c: Call): string | undefined {
  if (c.kind !== 'text' || (!c.input_tokens && !c.output_tokens)) return undefined;
  return `输入 ${tokens(c.input_tokens ?? 0)}${c.cache_read_tokens ? `（缓存 ${tokens(c.cache_read_tokens)}）` : ''} · 输出 ${tokens(c.output_tokens ?? 0)}`;
}
/** 单价：原价用的价，每百万 token「输入 / 输出」；三项同价（WorkBuddy 积分折算）只写一个；媒体按张或秒 */
function price(c: Call): string {
  if (c.kind !== 'text') return c.baseline_per_unit_usd ? `${usd(c.baseline_per_unit_usd)}/${c.unit === 'second' ? '秒' : '张'}` : '—';
  const p = c.baseline_price;
  if (!p) return c.baseline_unpriced ? '未设单价' : '—';
  const f = (v: number) => unitPrice(v, p.currency);
  if (p.input_per_m === p.output_per_m) return f(p.input_per_m);
  const out = f(p.output_per_m).replace(/^[^\d]+/, ''); // 货币符号只写一次
  return `${f(p.input_per_m)} / ${out}`;
}
function priceTitle(c: Call): string | undefined {
  const p = c.baseline_price;
  if (!p) return c.baseline_display ? `按 ${c.baseline_display} 算` : undefined;
  const f = (v: number) => unitPrice(v, p.currency);
  return `按 ${c.baseline_display ?? c.baseline_model} 算，每百万 token：输入 ${f(p.input_per_m)} · 缓存 ${f(p.cache_read_per_m ?? 0)} · 输出 ${f(p.output_per_m)}`;
}
