import { useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { Call } from '../api/types';
import { Empty, ErrorLine, LaneChip, TierChip, toast } from '../components/ui';
import { Model } from '../components/Vendor';
import { cost, hhmm, hostLabel, secs, tokens, usd } from '../format';
import { errText, useDebounced, useLoad } from '../hooks';
import { useTheme } from '../theme/ThemeContext';

export function Calls({ live }: { live?: Call | null }) {
  const { t } = useTheme();
  const [q, setQ] = useState('');
  const [source, setSource] = useState('');
  const [lane, setLane] = useState('');
  const dq = useDebounced(q, 250);
  const { data, setData, error } = useLoad(() => api.calls({ q: dq, source, lane, limit: 200 }), [dq, source, lane]);
  const [sel, setSel] = useState<Call | null>(null);

  // 事件流里来的新记录直接插到顶上
  useEffect(() => {
    if (!live) return;
    setData((old) => (old && !old.some((c) => c.id === live.id) ? [live, ...old] : old));
  }, [live, setData]);

  const update = (c: Call) => { setData((old) => old?.map((x) => (x.id === c.id ? c : x)) ?? old); setSel(c); };

  return (
    <>
      <div className="ct">
        <h4>{t('records')}</h4>
        <div className="tools">
          <input className="input sm" style={{ width: 180 }} placeholder="搜索内容或模型" value={q} onChange={(e) => setQ(e.target.value)} />
          <select className="sel" value={source} onChange={(e) => setSource(e.target.value)}>
            <option value="">全部来源</option><option value="claude_code">Claude Code</option><option value="hermes">Hermes</option><option value="workbuddy">WorkBuddy</option><option value="quick">快速面板</option><option value="cli">命令行</option>
          </select>
          <select className="sel" value={lane} onChange={(e) => setLane(e.target.value)}>
            <option value="">全部渠道</option><option value="sub">订阅</option><option value="api">按量付费</option><option value="media">媒体</option>
          </select>
        </div>
      </div>
      {error && <ErrorLine {...errText(error)!} />}
      <div className="split" style={{ margin: '0 -22px 0 0', height: 'calc(100vh - 90px)' }}>
        <div className="grow" style={{ overflow: 'auto', paddingRight: 12 }}>
          {data && data.length === 0 && <Empty>还没有请求；在 Claude Code 里让它画张图试试</Empty>}
          {data && data.length > 0 && (
            <table className="t">
              <thead><tr><th>时间</th><th>来源</th><th>任务</th><th>档位</th><th>{t('model')}</th><th className="right">费用</th></tr></thead>
              <tbody>
                {data.map((c) => {
                  const m = cost(c);
                  return (
                    <tr key={c.id} className={`clickable ${sel?.id === c.id ? 'sel' : ''}`} onClick={() => setSel(c)}>
                      <td>{hhmm(c.ts)}</td><td>{hostLabel(c)}</td>
                      <td>{c.task_type}{c.tier === 'top' && c.source !== 'redo' ? ' · 仔细想' : ''}</td>
                      <td><TierChip tier={c.tier} /></td>
                      <td><Model provider={c.provider} name={c.model} />{c.fallback_from && <> <span className="chip c-warn">已降级</span></>}{!c.ok && <> <span className="chip c-hi">失败</span></>}</td>
                      <td className={`right ${m.muted ? 'muted' : ''}`}>{m.text}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>
        {sel && <Drawer call={sel} onUpdate={update} />}
      </div>
    </>
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
    <div className="drawer">
      <h5>{c.task_type} · {c.tier ? t(`tier.${c.tier}`) : '媒体'}</h5>
      <div className="hint">{hhmm(c.ts)} · 来自 {hostLabel(c)} · 耗时 {secs(c.latency_ms)}</div>
      <div className="kv">
        <span>{t('model')}</span><span><Model provider={c.provider} name={c.model} /> <LaneChip lane={c.lane} /></span>
        {c.kind === 'text'
          ? <><span>用量</span><span>输入 {tokens(c.input_tokens)} · 输出 {tokens(c.output_tokens)}</span></>
          : <><span>用量</span><span>{c.units ?? 1} {c.unit === 'second' ? '秒' : '张'}</span></>}
        <span>费用</span><span>{m.muted ? `订阅额度${c.quota_units ? ` ${c.quota_units} 份` : ''}` : m.text}，全用 Opus 约 {usd(c.baseline_cost_usd)}</span>
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
