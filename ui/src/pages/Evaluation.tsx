import { useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { Evaluation as Ev } from '../api/types';
import { Empty, ErrorLine, SetRow, Toggle, toast } from '../components/ui';
import { usd } from '../format';
import { errText, useLoad } from '../hooks';
import { useTheme } from '../theme/ThemeContext';

export function Evaluation() {
  const { t } = useTheme();
  const evs = useLoad(() => api.evaluations());
  const settings = useLoad(() => api.settings());
  const [err, setErr] = useState<ApiError | null>(null);
  const [busy, setBusy] = useState(false);
  const latest = evs.data?.[0] ? { ...evs.data[0], by_task: evs.data[0].by_task ?? [], summary: evs.data[0].summary ?? { low_better: 0, tie: 0, high_better: 0 } } : undefined;
  const prev = evs.data?.find((e, i) => i > 0 && e.status === 'done');

  // 进行中的轮次每 3 秒刷一次
  useEffect(() => {
    if (latest?.status !== 'running') return;
    const id = setInterval(() => void evs.reload(), 3000);
    return () => clearInterval(id);
  }, [latest?.status, evs]);

  const start = async () => {
    setBusy(true); setErr(null);
    try { await api.startEvaluation(100); await evs.reload(); } catch (e) { setErr(e as ApiError); } finally { setBusy(false); }
  };
  const accept = async (ev: Ev, task: string) => {
    try { await api.acceptEvaluation(ev.id, task); toast('已采纳，规则已更新'); await evs.reload(); } catch (e) { setErr(e as ApiError); }
  };
  const sum = (e: Ev) => e.summary ?? { low_better: 0, tie: 0, high_better: 0 };
  const tiePct = (e: Ev) => { const s = sum(e); const n = s.low_better + s.tie + s.high_better || 1; return Math.round(((s.low_better + s.tie) / n) * 100); };
  const suggCount = latest?.by_task.filter((x) => x.suggestion).length ?? 0;

  return (
    <>
      <div className="ct">
        <h4>{t('nav.eval')}</h4>
        <button className="btn pri" disabled={busy || latest?.status === 'running'} onClick={start}>{latest?.status === 'running' ? '正在评估…' : '开始一轮评估'}</button>
      </div>
      <div className="hint" style={{ margin: '-8px 0 12px' }}>约花 $1.50，10 分钟左右；不影响正常使用</div>
      {(evs.error || err) && <ErrorLine {...errText(evs.error ?? err)!} />}
      {evs.data && !latest && <Empty>还没有评估过；点右上角开始第一轮</Empty>}
      {latest && (
        <>
          <div className="cards">
            <div className="card"><div className="k">上轮抽样</div><div className="v">{latest.sample} 条</div><div className="d plain">{latest.range_label ?? (latest.status === 'running' ? '正在抽样…' : '')}</div></div>
            <div className="card"><div className="k">{t('tier.low')}持平或更好</div><div className="v">{latest.status === 'done' ? `${tiePct(latest)}%` : '—'}</div>
              {prev && latest.status === 'done' && <div className="d">比上上轮 {tiePct(latest) - tiePct(prev) >= 0 ? '+' : ''}{tiePct(latest) - tiePct(prev)}%</div>}
              {!prev && latest.delta_pct !== undefined && <div className="d">比上上轮 {latest.delta_pct >= 0 ? '+' : ''}{latest.delta_pct}%</div>}</div>
            <div className="card"><div className="k">评估花费</div><div className="v">{usd(latest.cost_usd)}</div><div className="d plain">由{t('tier.high')}{t('model')}盲评</div></div>
            <div className="card"><div className="k">建议调整</div><div className="v">{suggCount} 项</div><div className={`d ${suggCount ? 'bad' : 'plain'}`}>{suggCount ? '见下表' : '暂不需要'}</div></div>
          </div>
          {latest.note && <div className="hint mb12">{latest.note}</div>}
          {latest.status === 'failed' && <ErrorLine message={`这轮评估没做完：${latest.error ?? "原因未知"}`} />}
          {latest.by_task.length > 0 && (
            <table className="t">
              <thead><tr><th>任务类型</th><th>抽样</th><th>{t('tier.low')}更好</th><th>持平</th><th>{t('tier.high')}更好</th><th>建议</th></tr></thead>
              <tbody>
                {latest.by_task.map((r) => (
                  <tr key={r.task_type} className={r.suggestion ? 'hi' : ''}>
                    <td>{r.name ?? r.task_type}</td><td>{r.n}</td><td>{r.low_better}</td><td>{r.tie}</td><td>{r.high_better}</td>
                    <td>{r.suggestion
                      ? <>{r.suggestion}{r.weekly_delta_usd !== undefined && <>，每周约多花 {usd(r.weekly_delta_usd)}</>}　<button className="btn sm" onClick={() => accept(latest, r.task_type)}>采纳</button></>
                      : <span className="muted">保持</span>}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </>
      )}
      {settings.data && (
        <div className="mt12">
          <SetRow title="每月自动评估一次" desc="每月 1 日凌晨运行，约花 $1.50" last>
            <Toggle on={settings.data.auto_evaluate_monthly} onChange={async (v) => { settings.setData({ ...settings.data!, auto_evaluate_monthly: v }); try { settings.setData(await api.saveSettings({ auto_evaluate_monthly: v })); } catch (e) { setErr(e as ApiError); } }} />
          </SetRow>
        </div>
      )}
    </>
  );
}
