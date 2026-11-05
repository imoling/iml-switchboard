import { useState, type ReactNode } from 'react';
import { api } from '../api/client';
import type { Call } from '../api/types';
import { ErrorLine, GroupChip, Seg } from '../components/ui';
import { Topology } from '../components/Topology';
import { accountIcon, Vendor } from '../components/Vendor';
import { dayLabel, num, usd } from '../format';
import { errText, useLoad } from '../hooks';
import { useTheme } from '../theme/ThemeContext';
import { channelNodes, hostNodes } from '../topology';

type Range = 'today' | '7d' | '30d';
const RANGE_LABEL: Record<Range, string> = { today: '今天', '7d': '7 天', '30d': '30 天' };

/** 总览：三个数字、一行接入情况、两张小图，下面直接是记录表 */
export function Overview({ live, onNavigate }: { live?: Call | null; onNavigate?: (page: 'integrations' | 'accounts') => void }) {
  const { t } = useTheme();
  const [range, setRange] = useState<Range>('7d');
  const { data, error } = useLoad(() => api.overview(range), [range]);
  const over = !!data && data.budget.limit_usd > 0 && data.budget.month_usd >= data.budget.limit_usd;

  return (
    <>
      <div className="ct" data-tauri-drag-region>
        <h4 data-tauri-drag-region>{t('nav.overview')}</h4>
        <Seg value={range} onChange={setRange} options={[{ id: 'today', label: '今天' }, { id: '7d', label: '7 天' }, { id: '30d', label: '30 天' }]} />
      </div>
      {error && <ErrorLine {...errText(error)!} />}
      {data && (
        <>
          <div className="cards three">
            <div className="card">
              <div className="k">按量花费 · {RANGE_LABEL[range]}</div><div className="v">{usd(data.api_cost_usd)}</div>
              <div className={`d ${over ? 'bad' : 'plain'}`}>本月 {usd(data.budget.month_usd)}{data.budget.limit_usd > 0 && <> / 上限 {usd(data.budget.limit_usd)}</>}</div>
            </div>
            <div className="card" title="应用原价减实际花费；原价按每个应用原来用的型号和单价算，在应用页可以改">
              <div className="k">自动分档省下 · {RANGE_LABEL[range]}</div><div className="v ok-text">{usd(data.saved_usd)}</div>
              <div className="d plain">应用原价 {usd(data.baseline_cost_usd)}{!!data.baseline_unpriced && <> · {data.baseline_unpriced} 条未设单价</>}</div>
            </div>
            <div className="card">
              <div className="k">{t('dispatch.noun')}次数 · {RANGE_LABEL[range]}</div><div className="v">{num(data.calls)}</div>
              <div className="d plain">订阅 {data.lane_dist.sub}% · 按量 {data.lane_dist.api}% · 媒体 {data.lane_dist.media}%</div>
              {data.escalation_hot_task && <div className="d bad">「{data.escalation_hot_task}」常被标记{t('escalate')}</div>}
            </div>
          </div>
          <TopoStrip><Topology bare live={live} onNavigate={onNavigate} /></TopoStrip>
          <div className="row2">
            <div className="panel">
              <DailyChart daily={data.daily} range={RANGE_LABEL[range]} />
              <h5 style={{ marginTop: 14 }}>档位分布</h5>
              <TierStack d={data.tier_dist} />
            </div>
            <div className="panel">
              <h5>花得最多的任务类型</h5>
              {data.top_tasks.length === 0 && <div className="hint">这段时间还没有记录</div>}
              <table className="t">
                <tbody>
                  {data.top_tasks.map((x) => (
                    <tr key={x.task_type}><td>{x.name ?? x.task_type}</td><td className="muted"><GroupChip group={x.lane} /></td><td className="right">{x.metric}</td></tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}
    </>
  );
}

/** 接入与渠道：上面一行汇总，下面是拓扑图 */
function TopoStrip({ children }: { children?: ReactNode }) {
  const { data } = useLoad(() => Promise.all([api.integrations(), api.accounts()]), []);
  const hosts = data ? hostNodes(data[0]) : [];
  const channels = data ? channelNodes(data[1]) : [];
  const subs = channels.filter((a) => a.kind === 'subscription').length;
  return (
    <div className="panel topo-strip">
      <div className="topo-sum">
        <span className="grp"><span className="k">接入</span><b>{hosts.length}</b><span className="icons">{hosts.map((h) => <Vendor key={h.id} provider={h.id} />)}</span></span>
        <span className="grp"><span className="k">渠道</span><b>{channels.length}</b><span className="icons">{channels.map((a) => <Vendor key={a.id} provider={accountIcon(a)} />)}</span></span>
        <span className="split-note">订阅 {subs} · 按量付费 {channels.length - subs}</span>
      </div>
      {children}
    </div>
  );
}

type Point = { date: string; cost_usd: number; baseline_cost_usd: number; calls: number };
const SHICHEN = ['子', '丑', '寅', '卯', '辰', '巳', '午', '未', '申', '酉', '戌', '亥'];

/** 今天按小时；后宫主题把 24 小时并成十二时辰（子时跨 23 点到 1 点） */
function useBuckets(daily: Point[]): { points: Point[]; label: (p: Point, i: number) => string; title: (p: Point) => string } {
  const { effectiveId } = useTheme();
  const hourly = daily.length > 0 && daily[0].date.includes('T');
  if (!hourly) {
    return { points: daily, label: (p, i) => dayLabel(p.date, i === daily.length - 1, daily.length), title: (p) => `${Number(p.date.slice(5, 7))} 月 ${Number(p.date.slice(8, 10))} 日` };
  }
  const hourOf = (p: Point) => Number(p.date.slice(11, 13));
  const nowH = new Date().getHours();
  if (effectiveId === 'harem') {
    const points: Point[] = SHICHEN.map((name, i) => {
      const hs = [(i * 2 + 23) % 24, i * 2];
      const members = daily.filter((p) => hs.includes(hourOf(p)));
      return { date: name, cost_usd: members.reduce((s, p) => s + p.cost_usd, 0), baseline_cost_usd: members.reduce((s, p) => s + p.baseline_cost_usd, 0), calls: members.reduce((s, p) => s + (p.calls ?? 0), 0) };
    });
    const cur = Math.floor(((nowH + 1) % 24) / 2);
    return { points, label: (_p, i) => `${SHICHEN[i]}${i === cur ? '·今' : ''}`, title: (p) => `${p.date}时` };
  }
  return { points: daily, label: (p, i) => { const h = hourOf(p); return h % 3 === 0 || i === daily.length - 1 ? `${h} 时` : ''; }, title: (p) => `${hourOf(p)} 时` };
}

type DailyView = 'saved' | 'calls';
const DAILY_VIEW_KEY = 'imls.overview.daily';

/** 每天：一次看一张，右上角切「省下」或「调度量」，选择记住。省下：柱高是应用原价，实色是实际花费，浅色是省下 */
function DailyChart({ daily, range }: { daily: Point[]; range: string }) {
  const { points, label, title } = useBuckets(daily);
  const [view, setView] = useState<DailyView>(() => (localStorage.getItem(DAILY_VIEW_KEY) === 'calls' ? 'calls' : 'saved'));
  const pickView = (v: DailyView) => { localStorage.setItem(DAILY_VIEW_KEY, v); setView(v); };
  const [hover, setHover] = useState<number | null>(null);
  const top = (p: Point) => (view === 'saved' ? Math.max(p.baseline_cost_usd, p.cost_usd) : p.calls ?? 0);
  const max = Math.max(view === 'saved' ? 1e-6 : 1, ...points.map(top));
  const saved = points.reduce((s, p) => s + Math.max(0, p.baseline_cost_usd - p.cost_usd), 0);
  const calls = points.reduce((s, p) => s + (p.calls ?? 0), 0);
  const dense = points.length > 12;
  const hourly = daily.length > 0 && daily[0].date.includes('T');
  const h = hover !== null ? points[hover] : null;
  const pct = (v: number) => `${v > 0 ? Math.max(3, (v / max) * 100) : 0}%`;
  return (
    <div className="dc" onMouseLeave={() => setHover(null)}>
      <div className="dc-title">
        <h5>每天</h5>
        <Seg value={view} onChange={pickView} options={[{ id: 'saved', label: '省下' }, { id: 'calls', label: '调度量' }]} />
      </div>
      <div className="dc-head">
        {view === 'saved' ? <span className="dc-key"><i className="sw saved" />省下<i className="sw cost" />实际花费</span> : <span />}
        <span className="hint">{view === 'saved' ? `${range}省 ${usd(saved)}` : `${range}共 ${num(calls)} 次`}</span>
      </div>
      <div className={`dc-bars ${view === 'saved' ? 'money' : 'calls'} ${dense ? 'dense' : ''}`}>
        {points.map((p, i) => (
          <div key={p.date} className={`col ${hover === i ? 'on' : ''}`} onMouseEnter={() => setHover(i)}>
            <div className="bar" style={{ height: pct(top(p)) }}>{view === 'saved' && <b style={{ height: top(p) > 0 ? `${(p.cost_usd / top(p)) * 100}%` : 0 }} />}</div>
          </div>
        ))}
      </div>
      <div className={`days ${dense ? 'dense' : ''}`}>{points.map((p, i) => <span key={p.date}>{label(p, i)}</span>)}</div>
      <div className="hint dc-read">{h
        ? view === 'saved'
          ? `${title(h)}：原价 ${usd(h.baseline_cost_usd)} · 实际 ${usd(h.cost_usd)} · 省 ${usd(Math.max(0, h.baseline_cost_usd - h.cost_usd))}`
          : `${title(h)}：${num(h.calls ?? 0)} 次`
        : hourly ? '指着某个时段看明细' : '指着某一天看明细'}</div>
    </div>
  );
}

function TierStack({ d }: { d: { low: number; mid: number; high: number; top: number; media: number } }) {
  const { t } = useTheme();
  const total = d.low + d.mid + d.high + d.top || 1;
  const w = (n: number) => `${(n / total) * 100}%`;
  return (
    <div className="stack">
      {d.low > 0 && <div style={{ width: w(d.low), background: '#8c8c94' }}>{t('tier.low')} {Math.round((d.low / total) * 100)}%</div>}
      {d.mid > 0 && <div style={{ width: w(d.mid), background: '#D1A24A' }}>{t('tier.mid')} {Math.round((d.mid / total) * 100)}%</div>}
      {d.high > 0 && <div style={{ width: w(d.high), background: '#cf4b3f' }}>{t('tier.high')} {Math.round((d.high / total) * 100)}%</div>}
      {d.top > 0 && <div style={{ width: w(d.top), background: '#1C1B19', color: '#E0B15A' }}>{t('tier.top')} {Math.round((d.top / total) * 100)}%</div>}
    </div>
  );
}
