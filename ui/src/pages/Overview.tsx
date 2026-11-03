import { useEffect, useState } from 'react';
import { api, subscribeEvents } from '../api/client';
import type { Call, Quota } from '../api/types';
import { Bar, ErrorLine, GroupChip, Seg } from '../components/ui';
import { Topology } from '../components/Topology';
import { accountIcon, Vendor } from '../components/Vendor';
import { QUOTA_ESTIMATE_NOTE } from '../copy';
import { dayLabel, usd } from '../format';
import { errText, useLoad } from '../hooks';
import { useTheme } from '../theme/ThemeContext';
import { channelNodes, hostNodes } from '../topology';
import { Calls } from './Calls';

type Range = 'today' | '7d' | '30d';
const RANGE_LABEL: Record<Range, string> = { today: '今天', '7d': '7 天', '30d': '30 天' };
const TOPO_KEY = 'overview.topology.open';

/** 总览：三个数字、一行接入情况、两张小图，下面直接是记录表 */
export function Overview({ live, onNavigate }: { live?: Call | null; onNavigate?: (page: 'integrations' | 'accounts') => void }) {
  const [quotas, setQuotas] = useState<Quota[] | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  useEffect(() => subscribeEvents((e) => { if (e.type === 'quota') { setQuotas(e.data.quotas); setRefreshing(false); } }), []);
  const refreshQuota = async () => { setRefreshing(true); try { await api.refreshQuota(); } catch { setRefreshing(false); } setTimeout(() => setRefreshing(false), 120000); };
  const { t } = useTheme();
  const [range, setRange] = useState<Range>('7d');
  const { data, error } = useLoad(() => api.overview(range), [range]);
  const [topoOpen, setTopoOpen] = useState(() => localStorage.getItem(TOPO_KEY) === '1');
  const toggleTopo = () => setTopoOpen((v) => { localStorage.setItem(TOPO_KEY, v ? '0' : '1'); return !v; });
  // 订阅额度只列用过的；窗口细节在模型页
  const used = (quotas ?? data?.quotas ?? []).filter((q) => !q.sub && q.pct > 0);
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
            <div className="card" title={QUOTA_ESTIMATE_NOTE}>
              <div className="k" style={{ display: 'flex', alignItems: 'center' }}>订阅额度<button className="btn link sm" style={{ marginLeft: 'auto', padding: 0 }} disabled={refreshing} onClick={refreshQuota}>{refreshing ? '正在刷新…' : '刷新'}</button></div>
              {used.length === 0 && <><div className="v muted">—</div><div className="d plain">还没用过订阅</div></>}
              {used.slice(0, 3).map((q, i) => (
                <div key={i}>
                  <div className="qline"><span><Vendor provider={q.provider} />{q.label}</span><em>{q.window} · {Math.round(q.pct)}%</em></div>
                  <Bar pct={q.pct} tone={q.pct >= 80 ? 'amber' : undefined} />
                </div>
              ))}
            </div>
            <div className="card">
              <div className="k">{t('dispatch.noun')}次数 · {RANGE_LABEL[range]}</div><div className="v">{data.calls}</div>
              <div className="d plain">订阅 {data.lane_dist.sub}% · 按量 {data.lane_dist.api}% · 媒体 {data.lane_dist.media}%</div>
              {data.escalation_hot_task && <div className="d bad">「{data.escalation_hot_task}」常被标记{t('escalate')}</div>}
            </div>
          </div>
          <TopoStrip open={topoOpen} onToggle={toggleTopo} />
          {topoOpen && <Topology live={live} onNavigate={onNavigate} />}
          <div className="row2">
            <div className="panel">
              <h5>每天花费</h5>
              <Spark daily={data.daily} />
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
          <Calls live={live} embedded />
        </>
      )}
    </>
  );
}

/** 一行接入情况：谁在派活、有哪些渠道；点开才是拓扑图，开合记住 */
function TopoStrip({ open, onToggle }: { open: boolean; onToggle: () => void }) {
  const { data } = useLoad(() => Promise.all([api.integrations(), api.accounts()]), []);
  const hosts = data ? hostNodes(data[0]) : [];
  const channels = data ? channelNodes(data[1]) : [];
  return (
    <div className="panel topo-strip">
      <button className="topo-toggle" aria-expanded={open} onClick={onToggle}>
        <span className="muted">接入 {hosts.length} 个</span>{hosts.map((h) => <Vendor key={h.id} provider={h.id} />)}
        <span className="gap" />
        <span className="muted">渠道 {channels.length} 个</span>{channels.map((a) => <Vendor key={a.id} provider={accountIcon(a)} />)}
        <span className="chev">{open ? '▾ 收起' : '▸ 展开'}</span>
      </button>
    </div>
  );
}

type Point = { date: string; cost_usd: number; baseline_cost_usd: number };
const SHICHEN = ['子', '丑', '寅', '卯', '辰', '巳', '午', '未', '申', '酉', '戌', '亥'];

/** 今天按小时；后宫主题把 24 小时并成十二时辰（子时跨 23 点到 1 点） */
function useBuckets(daily: Point[]): { points: Point[]; label: (p: Point, i: number) => string; title: (p: Point) => string } {
  const { effectiveId } = useTheme();
  const hourly = daily.length > 0 && daily[0].date.includes('T');
  if (!hourly) {
    return { points: daily, label: (p, i) => dayLabel(p.date, i === daily.length - 1, daily.length), title: (p) => p.date };
  }
  const hourOf = (p: Point) => Number(p.date.slice(11, 13));
  const nowH = new Date().getHours();
  if (effectiveId === 'harem') {
    const points: Point[] = SHICHEN.map((name, i) => {
      const hs = [(i * 2 + 23) % 24, i * 2];
      const members = daily.filter((p) => hs.includes(hourOf(p)));
      return { date: name, cost_usd: members.reduce((s, p) => s + p.cost_usd, 0), baseline_cost_usd: members.reduce((s, p) => s + p.baseline_cost_usd, 0) };
    });
    const cur = Math.floor(((nowH + 1) % 24) / 2);
    return { points, label: (_p, i) => `${SHICHEN[i]}${i === cur ? '·今' : ''}`, title: (p) => `${p.date}时` };
  }
  return { points: daily, label: (p, i) => { const h = hourOf(p); return h % 3 === 0 || i === daily.length - 1 ? `${h} 时` : ''; }, title: (p) => `${hourOf(p)} 时` };
}

function Spark({ daily }: { daily: Point[] }) {
  const { points, label, title } = useBuckets(daily);
  const max = Math.max(0.01, ...points.map((d) => d.cost_usd));
  const dense = points.length > 12;
  return (
    <>
      <div className={`spark ${dense ? 'dense' : ''}`}>
        {points.map((d) => (
          <div key={d.date} style={{ height: `${Math.max(2, (d.cost_usd / max) * 100)}%` }} title={`${title(d)}：${usd(d.cost_usd)}`}>
            <b style={{ height: d.cost_usd > 0 ? '100%' : '0' }} />
          </div>
        ))}
      </div>
      <div className={`days ${dense ? 'dense' : ''}`}>{points.map((d, i) => <span key={d.date}>{label(d, i)}</span>)}</div>
    </>
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
