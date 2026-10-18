import { useCallback, useEffect, useRef, useState } from 'react';
import { api, subscribeEvents } from '../api/client';
import type { Quota, Call, Mode, Overview, Settings, Status } from '../api/types';
import { Bar, Dot, LaneChip, Seg, TierChip } from '../components/ui';
import { Vendor } from '../components/Vendor';
import { quotaWarning } from '../copy';
import { cost, usd } from '../format';
import { MODE_OPTIONS } from '../pages/Settings';
import { useTheme } from '../theme/ThemeContext';
import { desktop } from '../tauri';
import { Offline } from './Offline';



/** 「4 小时后重置」「6 天后重置」 */
function resetIn(iso: string): string {
  const ms = new Date(iso).getTime() - Date.now();
  if (!Number.isFinite(ms) || ms <= 0) return '即将重置';
  const h = Math.round(ms / 3600000);
  if (h < 1) return `${Math.max(1, Math.round(ms / 60000))} 分钟后重置`;
  if (h < 48) return `${h} 小时后重置`;
  return `${Math.round(h / 24)} 天后重置`;
}

function observedAgo(iso?: string): string {
  if (!iso) return '刚刚';
  const m = Math.round((Date.now() - new Date(iso).getTime()) / 60000);
  if (m < 1) return '刚刚';
  if (m < 60) return `${m} 分钟前`;
  return `${Math.round(m / 60)} 小时前`;
}

/** 菜单栏空间小：每个订阅压成一行，右侧列出各窗口，进度条取最紧张的那个 */
function groupQuotas(quotas: Quota[]) {
  const short = (w: string) => w.replace('5 小时窗口', '5 小时').replace('Claude / GPT ', 'Claude/GPT ').replace('（估算）', '');
  const out: { id: string; label: string; provider?: string; parts: string[]; max: number; estimated: boolean }[] = [];
  for (const q of quotas) {
    let g = out.find((x) => x.id === q.account_id);
    if (!g) { g = { id: q.account_id, label: q.label.replace(' 订阅', ''), provider: q.provider, parts: [], max: 0, estimated: !!q.estimated }; out.push(g); }
    if (q.window.startsWith('Claude / GPT')) {
      // Antigravity 里 Claude 与 GPT 共用一组额度，两个窗口合成一项，取更紧的那个
      const i = g.parts.findIndex((x) => x.startsWith('Claude/GPT'));
      const cur = i >= 0 ? Number(g.parts[i].replace(/\D/g, '')) : -1;
      if (q.pct > cur) { const txt = `Claude/GPT ${Math.round(q.pct)}%`; if (i >= 0) g.parts[i] = txt; else g.parts.push(txt); }
      g.max = Math.max(g.max, q.pct);
      continue;
    }
    const name = q.sub ? 'Fable' : short(q.window);
    g.parts.push(`${name} ${Math.round(q.pct)}%`);
    if (!q.sub) g.max = Math.max(g.max, q.pct);
  }
  return out;
}

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

  const [expanded, setExpanded] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  // 窗口高度跟着内容走：展开明细、记录变多时长高，收起时缩回
  useEffect(() => {
    const el = rootRef.current;
    if (!el) return;
    // 根元素撑满窗口（minHeight 100vh），量它永远缩不回去；量里面那层内容，再加上根元素的内边距和边框
    const apply = () => {
      const inner = el.firstElementChild as HTMLElement | null;
      const content = inner ? inner.getBoundingClientRect().height : el.scrollHeight;
      const h = Math.min(760, Math.max(300, Math.ceil(content + 26)));
      void desktop.resizeSelf(340, h);
    };
    const ro = new ResizeObserver(apply);
    ro.observe(el.firstElementChild ?? el);
    apply();
    return () => ro.disconnect();
  }, [status, ov, recent, expanded]);
  useEffect(() => subscribeEvents((e) => { if (e.type === 'quota') setRefreshing(false); }), []);
  const refreshQuota = async () => { setRefreshing(true); try { await api.refreshQuota(); } catch { setRefreshing(false); } setTimeout(() => setRefreshing(false), 120000); };
  const yieldPct = settings?.subscription_yield_pct ?? 80;
  const hot = ov?.quotas.some((q) => !q.sub && q.pct >= yieldPct);
  const dot = status === 'offline' ? 'no' : error ? 'err' : hot ? 'warn' : 'ok';
  const dotText = { no: '未运行', err: '出错', warn: '额度紧张', ok: '正常' }[dot];
  const warning = ov ? quotaWarning(ov.quotas, yieldPct) : null;

  const setMode = async (m: Mode) => { if (status && status !== 'offline') setStatus({ ...status, mode: m }); try { setStatus(await api.mode(m)); } catch { /* 下次刷新纠正 */ } };
  const togglePause = async () => { if (!status || status === 'offline') return; try { setStatus(await api.pause(!status.paused)); } catch { /* 同上 */ } };
  const toggleProjection = async () => { try { const s = await api.projection(!projection); setStatus(s); setProjection(s.projection); } catch { /* 同上 */ } };

  return (
    <div ref={rootRef} className="pop" style={{ margin: 0, width: '100%', minHeight: '100vh', overflow: 'auto', borderRadius: 12 }}><div>
      <div className="row" style={{ justifyContent: 'space-between' }}><b style={{ fontSize: 14 }}>{t('app.title')}</b><span><Dot s={dot} />{dotText}</span></div>
      {status === 'offline' && <Offline compact onStarted={load} />}
      {status && status !== 'offline' && ov && (
        <>
          <div className="stats">
            <div><div className="hint">今天</div><b>{ov.calls} 次</b></div>
            <div><div className="hint">按量花费</div><b>{usd(ov.api_cost_usd)}</b></div>
            <div><div className="hint">比全用 Opus 省</div><b className="ok-text">{usd(ov.saved_usd)}</b></div>
          </div>
          {groupQuotas(ov.quotas).map((g) => (
            <div key={g.id}>
              <div className="qline qtoggle" role="button" tabIndex={0} onClick={() => setExpanded(expanded === g.id ? null : g.id)} onKeyDown={(e) => { if (e.key === 'Enter') setExpanded(expanded === g.id ? null : g.id); }}>
                <span style={{ whiteSpace: 'nowrap' }}><Vendor provider={g.provider} />{g.label}</span>
                <em style={{ textAlign: 'right' }}>{expanded === g.id ? '收起' : g.parts.join(' · ')} <span className="chev">{expanded === g.id ? '▴' : '▾'}</span></em>
              </div>
              <Bar pct={g.max} tone={g.max >= yieldPct ? 'amber' : undefined} />
              {expanded === g.id && (
                <div className="qdetail">
                  {ov.quotas.filter((q) => q.account_id === g.id).map((q, i) => (
                    <div key={i} className="qrow">
                      <div className="qline"><span>{q.sub ? '其中 Fable' : q.window}</span><em>{Math.round(q.pct)}%{q.resets_at ? ` · ${resetIn(q.resets_at)}` : ''}</em></div>
                      <Bar pct={q.pct} tone={q.sub ? 'gold' : q.pct >= yieldPct ? 'amber' : undefined} />
                    </div>
                  ))}
                  <div className="qline"><span className="hint">{g.estimated ? '按本应用记录估算' : `官方命令回报 · ${observedAgo(ov.quotas.find((q) => q.account_id === g.id)?.observed_at)}`}</span><button className="btn sm" disabled={refreshing} onClick={(e) => { e.stopPropagation(); void refreshQuota(); }}>{refreshing ? '正在刷新…' : '刷新'}</button></div>
                </div>
              )}
            </div>
          ))}
          <div className="qline"><span>按量付费</span><em>本月 {usd(ov.budget.month_usd)} / {usd(ov.budget.limit_usd)}</em></div>
          <Bar pct={(ov.budget.month_usd / (ov.budget.limit_usd || 1)) * 100} tone="blue" />
          <div className="hint" style={{ margin: '10px 0 6px' }}>调度模式</div>
          <Seg full value={status.mode} onChange={setMode} options={MODE_OPTIONS} />
          <div className="hint" style={{ margin: '12px 0 4px' }}>{t('dispatch.recent')}</div>
          <div className="recent">
            {recent.map((c) => { const m = cost(c); return (
              <div key={c.id}>{c.kind === 'text' && c.tier ? <TierChip tier={c.tier} /> : <LaneChip lane={c.lane} />} {c.task_name ?? c.task_type} · <Vendor provider={c.provider} />{c.model_display ?? c.model}{!m.muted && <> · {m.text}</>}</div>
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
    </div></div>
  );
}
