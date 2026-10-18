import { useEffect, useState } from 'react';
import { api, subscribeEvents } from '../api/client';
import type { Quota } from '../api/types';
import { Bar, ErrorLine, GroupChip, Seg } from '../components/ui';
import { Vendor } from '../components/Vendor';
import { QUOTA_ESTIMATE_NOTE } from '../copy';
import { dayLabel, pct, usd } from '../format';
import { errText, useLoad } from '../hooks';
import { useTheme } from '../theme/ThemeContext';

type Range = 'today' | '7d' | '30d';

export function Overview() {
  const [quotas, setQuotas] = useState<Quota[] | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  useEffect(() => subscribeEvents((e) => { if (e.type === 'quota') { setQuotas(e.data.quotas); setRefreshing(false); } }), []);
  const refreshQuota = async () => { setRefreshing(true); try { await api.refreshQuota(); } catch { setRefreshing(false); } setTimeout(() => setRefreshing(false), 120000); };
  const { t } = useTheme();
  const [range, setRange] = useState<Range>('7d');
  const { data, error } = useLoad(() => api.overview(range), [range]);

  return (
    <>
      <div className="ct">
        <h4>{t('nav.overview')}</h4>
        <Seg value={range} onChange={setRange} options={[{ id: 'today', label: '今天' }, { id: '7d', label: '7 天' }, { id: '30d', label: '30 天' }]} />
      </div>
      {error && <ErrorLine {...errText(error)!} />}
      {data && (
        <>
          <div className="cards">
            <div className="card"><div className="k">{t('dispatch.noun')}次数</div><div className="v">{data.calls}</div>
              {data.calls_delta_pct !== undefined && <div className="d">比上周 {data.calls_delta_pct >= 0 ? '+' : ''}{data.calls_delta_pct}%</div>}</div>
            <div className="card"><div className="k">按量花费</div><div className="v">{usd(data.api_cost_usd)}</div><div className="d plain">本月上限 {usd(data.budget.limit_usd)}</div></div>
            <div className="card"><div className="k">比全用 Opus 省</div><div className="v ok-text">{usd(data.saved_usd)}</div><div className="d">约省 {Math.round(data.saved_pct)}%</div></div>
            <div className="card"><div className="k">手动升档率</div><div className="v">{pct(data.escalation_rate)}</div>
              {data.escalation_hot_task ? <div className="d bad">「{data.escalation_hot_task}」偏高</div> : <div className="d plain">没有偏高的任务类型</div>}</div>
          </div>
          <div className="row2">
            <div className="panel">
              <h5>每天节省 <span className="muted" style={{ fontWeight: 400 }}>深色为实际花费，浅色为全用 Opus 的花费</span></h5>
              <Spark daily={data.daily} />
              <h5 style={{ marginTop: 14 }}>档位分布</h5>
              <TierStack d={data.tier_dist} />
              <div className="legend">
                <span><i style={{ background: '#C4613A' }} />订阅 {data.lane_dist.sub}%</span>
                <span><i style={{ background: '#4A6785' }} />按量 {data.lane_dist.api}%</span>
                <span><i style={{ background: '#5F7A4A' }} />媒体 {data.lane_dist.media}%</span>
              </div>
            </div>
            <div className="panel">
              <h5 style={{ display: 'flex', alignItems: 'center' }}>订阅额度<button className="btn sm" style={{ marginLeft: 'auto' }} disabled={refreshing} onClick={refreshQuota}>{refreshing ? '正在刷新…' : '刷新'}</button></h5>
              {(quotas ?? data.quotas).map((q, i) => (
                <div key={i}>
                  <div className="qline">
                    <span style={q.sub ? { paddingLeft: 18 } : undefined}>{!q.sub && <Vendor provider={q.provider} />}{q.label}</span>
                    <em>{q.window} · {Math.round(q.pct)}%</em>
                  </div>
                  <Bar pct={q.pct} tone={q.sub ? 'gold' : q.pct >= 80 ? 'amber' : undefined} />
                </div>
              ))}
              <div className="hint mb12">{QUOTA_ESTIMATE_NOTE}</div>
              <h5>花得最多的任务类型</h5>
              <table className="t">
                <tbody>
                  {data.top_tasks.map((x) => (
                    <tr key={x.task_type}><td>{x.task_type}</td><td className="muted"><GroupChip group={x.lane} /></td><td className="right">{x.metric}</td></tr>
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

function Spark({ daily }: { daily: { date: string; cost_usd: number; baseline_cost_usd: number }[] }) {
  const max = Math.max(0.01, ...daily.map((d) => d.baseline_cost_usd));
  return (
    <>
      <div className="spark">
        {daily.map((d) => (
          <div key={d.date} style={{ height: `${(d.baseline_cost_usd / max) * 100}%` }} title={`${d.date}：${usd(d.cost_usd)}，全用 Opus ${usd(d.baseline_cost_usd)}`}>
            <b style={{ height: `${d.baseline_cost_usd ? (d.cost_usd / d.baseline_cost_usd) * 100 : 0}%` }} />
          </div>
        ))}
      </div>
      <div className="days">{daily.map((d, i) => <span key={d.date}>{dayLabel(d.date, i === daily.length - 1)}</span>)}</div>
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
