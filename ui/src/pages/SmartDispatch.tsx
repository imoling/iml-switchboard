import { useEffect, useState, type KeyboardEvent } from 'react';
import { api, ApiError } from '../api/client';
import type { Decision, Judge, LayaStatus, SmartSettings } from '../api/types';
import { ErrorLine, Seg, SetRow, TierChip, toast } from '../components/ui';
import { Model } from '../components/Vendor';
import { errText } from '../hooks';

const JUDGES: { id: Judge; label: string }[] = [{ id: 'laya', label: '本机 Laya' }, { id: 'jev', label: 'Jev' }, { id: 'model', label: '已有模型' }];
const PRIVACY: Record<Judge, string> = {
  laya: '在本机判断，任务内容不出这台电脑',
  jev: '任务开头会脱敏发给 Jev 判断（境外）',
  model: '任务开头会发给选的型号，按它的价计费',
};

/** 分派页顶部的「怎么分派」：按规则，或智能分派（请判官在能用的模型里挑）；选智能分派时同一张卡片里配判官 */
export function SmartCard({ data, setData, reload }: { data: SmartSettings; setData: (d: SmartSettings) => void; reload: () => void }) {
  const [key, setKey] = useState('');
  const [err, setErr] = useState<ApiError | null>(null);
  const [acting, setActing] = useState<string | null>(null);
  // Laya 在装、在下载时每两秒刷一次进度
  const busy = data.laya?.busy;
  useEffect(() => {
    if (!busy) return;
    const id = setInterval(() => { void reload(); }, 2000);
    return () => clearInterval(id);
  }, [busy, reload]);
  const run = async (body: Parameters<typeof api.setSmart>[0], ok?: string) => {
    try { setData(await api.setSmart(body)); setErr(null); if (ok) toast(ok); } catch (e) { setErr(e as ApiError); }
  };
  const laya = async (action: Parameters<typeof api.laya>[0], ok?: string) => {
    setActing(action); setErr(null);
    try { setData(await api.laya(action)); if (ok) toast(ok); } catch (e) { setErr(e as ApiError); void reload(); } finally { setActing(null); }
  };
  const saveKey = () => { if (key.trim()) { void run({ key: key.trim() }, '已保存 Jev 密钥'); setKey(''); } };
  const onKey = (e: KeyboardEvent) => { if (e.nativeEvent.isComposing || e.keyCode === 229) return; if (e.key === 'Enter') saveKey(); };
  const ready = judgeReady(data);
  const pick = data.judge_models.find((m) => m.account === data.judge_account && m.model === data.judge_model);
  return (
    <>
      {err && <ErrorLine {...errText(err)!} />}
      <div className="panel mb12">
        <SetRow title="怎么分派" desc={data.enabled ? PRIVACY[data.judge] : '按下面的任务类型判档，照档位候选选型号'} last={!data.enabled}>
          <Seg value={data.enabled ? 'smart' : 'rules'} onChange={(v) => run({ enabled: v === 'smart' })} options={[{ id: 'rules', label: '规则分派' }, { id: 'smart', label: '智能分派' }]} />
        </SetRow>
        {data.enabled && <>
          <SetRow title="谁来判" desc={ready ? '规则判的档位当先验；同一会话沿用第一次的选择' : '判官没准备好时按规则分派'}>
            <Seg value={data.judge} onChange={(v) => run({ judge: v })} options={JUDGES} />
          </SetRow>
          {data.judge === 'laya' && data.laya && <LayaRow st={data.laya} acting={acting} onAct={laya} />}
          {data.judge === 'jev' && (
            <SetRow last title="Jev 密钥" desc={data.has_key ? `已填，尾号 ${data.key_tail}` : '在 TypeSafe 控制台申请'}>
              <input className="input sm" type="password" style={{ width: 220 }} placeholder={data.has_key ? '换一个密钥' : '粘贴 TypeSafe 的密钥'} value={key} onChange={(e) => setKey(e.target.value)} onKeyDown={onKey} />
              <button className="btn sm pri" disabled={!key.trim()} onClick={saveKey}>保存</button>
              {data.has_key && <button className="btn sm" onClick={() => run({ clear_key: true }, '已删除 Jev 密钥')}>删除</button>}
            </SetRow>
          )}
          {data.judge === 'model' && (
            <SetRow last title="判官型号" desc={data.judge_models.length ? '选快的低档型号，每次判断约几百 token' : '先在模型页添加按量付费账号'}>
              <select className="sel" value={pick ? `${pick.account}/${pick.model}` : ''} onChange={(e) => { const [account, ...rest] = e.target.value.split('/'); void run({ judge_account: account, judge_model: rest.join('/') }); }}>
                <option value="" disabled>选一个型号</option>
                {data.judge_models.map((m) => <option key={`${m.account}/${m.model}`} value={`${m.account}/${m.model}`}>{m.display} · {m.account_label}</option>)}
              </select>
            </SetRow>
          )}
        </>}
      </div>
    </>
  );
}

/** 判官能不能用：Laya 在跑、Jev 有密钥、判官型号选了 */
export const judgeReady = (d: SmartSettings) => (d.judge === 'laya' ? !!d.laya?.running : d.judge === 'jev' ? d.has_key : !!d.judge_model);

/** 本机 Laya：下载（运行环境加模型约 1.4 GB）、启动、停止、删除 */
function LayaRow({ st, acting, onAct }: { st: LayaStatus; acting: string | null; onAct: (a: 'install' | 'start' | 'stop' | 'remove', ok?: string) => void }) {
  const desc = st.busy === 'installing' ? '正在安装运行环境…'
    : st.busy === 'downloading' ? `正在下载模型 ${Math.round((st.progress ?? 0) * 100)}%`
    : st.busy === 'starting' ? '正在启动…'
    : st.error ? st.error
    : st.running ? `运行中${st.device ? ` · ${st.device.toUpperCase()}` : ''}；空闲 10 分钟释放内存`
    : st.installed && st.downloaded ? `已下载 ${(st.disk_mb / 1000).toFixed(1)} GB，没在运行`
    : '下载后占用约 1.4 GB';
  const ready = st.installed && st.downloaded;
  return (
    <SetRow last title="Laya 模型" desc={<span className={st.error && !st.busy ? 'err-text' : undefined}>{desc}</span>}>
      {!ready && !st.busy && !st.running && <button className="btn sm pri" disabled={!!acting} onClick={() => onAct('install')}>下载</button>}
      {ready && !st.running && !st.busy && <button className="btn sm pri" disabled={!!acting} onClick={() => onAct('start', 'Laya 已启动')}>{acting === 'start' ? '正在启动…' : '启动'}</button>}
      {st.running && <button className="btn sm" disabled={!!acting} onClick={() => onAct('stop', 'Laya 已停止')}>停止</button>}
      {(st.installed || st.downloaded) && !st.busy && <button className="btn sm" disabled={!!acting} onClick={() => onAct('remove', '已删除 Laya')}>删除</button>}
    </SetRow>
  );
}

/** 判官给每个候选的概率 × 系数 = 得分，第一行就是会用的 */
export function SmartResult({ d, table }: { d: Decision; table?: boolean }) {
  const s = d.smart;
  const first = d.candidates.find((c) => !c.skipped);
  const media = d.kind !== 'text';
  const kind = kindName(d.kind);
  if (d.kind === 'animation') return <div className="hint mt8">认成动画请求，交给写动画代码的模型，不问判官</div>;
  if (!s || s.note || !s.options?.length) {
    return <div className="hint mt8">{media ? `认成${kind}请求，交给 ${first?.display ?? first?.model ?? '媒体渠道'}${s?.note ? `；${s.note}` : ''}` : s?.note ?? d.reason.join('；')}</div>;
  }
  return (
    <>
    {!table && <div className="hint mt8">{media ? `认成${kind}请求，` : ''}用 {s.options[0].display}{d.latency_ms !== undefined ? `；判断用时 ${d.latency_ms} 毫秒` : ''}</div>}
    <table className="t mt8 smart-table">
      <thead><tr><th>模型</th><th>{media ? '顺位' : '档位'}</th><th className="right">概率</th><th className="right">{media ? '顺位系数' : '档位系数'}</th><th className="right">得分</th></tr></thead>
      <tbody>
        {s.options.map((o, i) => (
          <tr key={o.model + o.lane} className={i === 0 ? 'sel' : ''}>
            <td className="ell"><Model provider={o.provider} name={o.display} /></td>
            <td>{o.rank ? <span className="muted">第 {o.rank} 个</span> : <TierChip tier={o.tier} />}</td>
            <td className="right">{Math.round(o.prob * 100)}%</td>
            <td className="right muted">×{o.weight.toFixed(2)}</td>
            <td className="right">{o.score.toFixed(2)}</td>
          </tr>
        ))}
      </tbody>
    </table>
    </>
  );
}

const KIND_LABEL: Record<string, string> = { image: '生图', video: '生视频', animation: '动画' };
function kindName(k: string) { return KIND_LABEL[k] ?? '媒体'; }
