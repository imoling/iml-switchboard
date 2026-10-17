import { useCallback, useEffect, useState } from 'react';
import { api, subscribeEvents } from '../api/client';
import type { Call, Mode, Overview, Settings, Status } from '../api/types';
import { Bar, Dot, LaneChip, Seg, TierChip } from '../components/ui';
import { Vendor } from '../components/Vendor';
import { quotaWarning } from '../copy';
import { cost, usd } from '../format';
import { MODE_OPTIONS } from '../pages/Settings';
import { useTheme } from '../theme/ThemeContext';
import { desktop } from '../tauri';
import { Offline } from './Offline';

export function Popover() {
  const { t, themes, effectiveId, setThemeId, setProjection, projection } = useTheme();
  const [status, setStatus] = useState<Status | null | 'offline'>(null);
  const [ov, setOv] = useState<Overview | null>(null);
  const [recent, setRecent] = useState<Call[]>([]);
  const [settings, setSettings] = useState<Settings | null>(null);
  const [error, setError] = useState(false);

  const load = useCallback(async () => {
    try {
      const s = await api.status(); setStatus(s); setThemeId(s.theme); setProjection(s.projection);
      const [o, r, st] = await Promise.all([api.overview('today'), api.calls({ limit: 3 }), api.settings().catch(() => null)]);
      setOv(o); setRecent(r.slice(0, 3)); if (st) setSettings(st); setError(r.some((c) => !c.ok));
    } catch { setStatus('offline'); }
  }, [setThemeId, setProjection]);

  useEffect(() => { void load(); }, [load]);
  useEffect(() => { let un = () => {}; void desktop.onShown(() => void load()).then((u) => { un = u; }); return () => un(); }, [load]);
  useEffect(() => subscribeEvents((e) => {
    if (e.type === 'quota') setOv((o) => (o ? { ...o, quotas: e.data.quotas } : o));
    if (e.type === 'status') { setStatus(e.data); setThemeId(e.data.theme); setProjection(e.data.projection); }
    if (e.type === 'call') { setRecent((r) => [e.data.call, ...r].slice(0, 3)); setOv((o) => (o ? { ...o, calls: o.calls + 1 } : o)); }
  }, (open) => { if (!open) setStatus('offline'); }), [setThemeId, setProjection]);
  useEffect(() => { const k = (e: KeyboardEvent) => { if (e.key === 'Escape') void desktop.hide('popover'); }; window.addEventListener('keydown', k); return () => window.removeEventListener('keydown', k); }, []);

  const yieldPct = settings?.subscription_yield_pct ?? 80;
  const hot = ov?.quotas.some((q) => !q.sub && q.pct >= yieldPct);
  const dot = status === 'offline' ? 'no' : error ? 'err' : hot ? 'warn' : 'ok';
  const dotText = { no: '未运行', err: '出错', warn: '额度紧张', ok: '正常' }[dot];
  const warning = ov ? quotaWarning(ov.quotas, yieldPct) : null;

  const setMode = async (m: Mode) => { if (status && status !== 'offline') setStatus({ ...status, mode: m }); try { setStatus(await api.mode(m)); } catch { /* 下次刷新纠正 */ } };
  const togglePause = async () => { if (!status || status === 'offline') return; try { setStatus(await api.pause(!status.paused)); } catch { /* 同上 */ } };
  const toggleProjection = async () => { try { const s = await api.projection(!projection); setStatus(s); setProjection(s.projection); } catch { /* 同上 */ } };

  return (
    <div className="pop" style={{ margin: 0, width: '100%', height: '100vh', overflow: 'auto', borderRadius: 12 }}>
      <div className="row" style={{ justifyContent: 'space-between' }}><b style={{ fontSize: 14 }}>{t('app.title')}</b><span><Dot s={dot} />{dotText}</span></div>
      {status === 'offline' && <Offline compact onStarted={load} />}
      {status && status !== 'offline' && ov && (
        <>
          <div className="stats">
            <div><div className="hint">今天</div><b>{ov.calls} 次</b></div>
            <div><div className="hint">按量花费</div><b>{usd(ov.api_cost_usd)}</b></div>
            <div><div className="hint">比全用 Opus 省</div><b className="ok-text">{usd(ov.saved_usd)}</b></div>
          </div>
          {ov.quotas.map((q, i) => (
            <div key={i}>
              <div className="qline"><span style={q.sub ? { paddingLeft: 18 } : undefined}>{!q.sub && <Vendor provider={q.provider} />}{q.label}</span><em>{q.window} · {Math.round(q.pct)}%</em></div>
              <Bar pct={q.pct} tone={q.sub ? 'gold' : q.pct >= yieldPct ? 'amber' : undefined} />
            </div>
          ))}
          <div className="qline"><span>按量付费</span><em>本月 {usd(ov.budget.month_usd)} / {usd(ov.budget.limit_usd)}</em></div>
          <Bar pct={(ov.budget.month_usd / (ov.budget.limit_usd || 1)) * 100} tone="blue" />
          <div className="hint" style={{ margin: '10px 0 6px' }}>调度模式</div>
          <Seg full value={status.mode} onChange={setMode} options={MODE_OPTIONS} />
          <div className="hint" style={{ margin: '12px 0 4px' }}>{t('dispatch.recent')}</div>
          <div className="recent">
            {recent.map((c) => { const m = cost(c); return (
              <div key={c.id}>{c.kind === 'text' && c.tier ? <TierChip tier={c.tier} /> : <LaneChip lane={c.lane} />} {c.task_type} · <Vendor provider={c.provider} />{c.model}{!m.muted && <> · {m.text}</>}</div>
            ); })}
            {recent.length === 0 && <div className="hint">还没有请求</div>}
          </div>
          <div className="row" style={{ marginTop: 12 }}>
            <button className="btn pri grow" onClick={() => desktop.showMain()}>{t('app.open')}</button>
            <button className="btn" onClick={togglePause}>{status.paused ? t('resume') : t('pause')}</button>
          </div>
          {warning && <div className="hint warn-text mt8">{warning}</div>}
        </>
      )}
      <div className="foot hint">
        <span>主题：{themes.find((x) => x.id === effectiveId)?.name ?? '点将台'}</span>
        <button className="btn link" style={{ fontSize: 12, color: projection ? '#6F6A61' : '#C4613A' }} onClick={toggleProjection}>{projection ? '退出投屏' : '投屏模式'}</button>
      </div>
    </div>
  );
}
