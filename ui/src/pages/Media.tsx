import { useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { Call, Job } from '../api/types';
import { Empty, ErrorLine, GroupChip, LaneChip, Modal, RevealButton, Seg, toast } from '../components/ui';
import { Model, Vendor } from '../components/Vendor';
import { cost, eta, jobCost, usd } from '../format';
import { errText, useLoad } from '../hooks';
import { useTheme } from '../theme/ThemeContext';
import { desktop, fileUrl } from '../tauri';

type Kind = 'all' | 'image' | 'video' | 'animation';
const KIND_LABEL: Record<string, string> = { image: '图片', video: '视频', animation: '动画' };

/** 后台的进度是 0 到 1，mock 是 0 到 100，两种都认 */
const pct = (p: number) => (p <= 1 ? p * 100 : p);

export function Media({ liveJob }: { liveJob?: Job | null }) {
  const { t } = useTheme();
  const [kind, setKind] = useState<Kind>('all');
  const { data, setData, error, reload } = useLoad(() => api.media(kind), [kind]);
  const [err, setErr] = useState<ApiError | null>(null);
  const [open, setOpen] = useState<Job | null>(null);
  const [pick, setPick] = useState<Job | null>(null);

  useEffect(() => {
    if (!liveJob) return;
    setData((old) => {
      if (!old) return old;
      const exists = old.jobs.some((j) => j.id === liveJob.id);
      const jobs = exists ? old.jobs.map((j) => (j.id === liveJob.id ? { ...j, ...liveJob } : j)) : [liveJob, ...old.jobs];
      return { ...old, jobs };
    });
    if (liveJob.status === 'done') void reload();
  }, [liveJob, setData, reload]);

  const act = async (fn: () => Promise<unknown>) => { setErr(null); try { await fn(); await reload(); } catch (e) { setErr(e as ApiError); } };
  const isFailed = (j: Job) => j.status === 'failed' || j.status === 'timeout';
  const active = data?.jobs.filter((j) => j.status !== 'done' && j.status !== 'canceled' && !isFailed(j)) ?? [];
  const failed = data?.jobs.filter(isFailed) ?? [];
  /** 卡片标题：去掉宿主塞进来的提示块，取用户自己那句 */
  const titleOf = (s?: string) => (s ?? '').replace(/<system-reminder>[\s\S]*?<\/system-reminder>/g, '').trim().slice(0, 60) || '（没有描述）';

  return (
    <>
      <div className="ct">
        <h4>{t('nav.media')}</h4>
        <Seg value={kind} onChange={setKind} options={[{ id: 'all', label: '全部' }, { id: 'image', label: '图片' }, { id: 'video', label: '视频' }, { id: 'animation', label: '动画' }]} />
      </div>
      {(error || err) && <ErrorLine {...errText(error ?? err)!} />}
      {data && (
        <>
          {active.length > 0 && <div className="hint mb6">进行中</div>}
          {active.map((j) => (
            <div key={j.id} className="job">
              <GroupChip group="media" />
              <span className="name" onClick={() => j.kind === 'animation' && setOpen(j)} style={j.kind === 'animation' ? { cursor: 'pointer' } : undefined}>
                <span className="muted">{KIND_LABEL[j.kind]}</span> {titleOf(j.prompt_excerpt)}{j.seconds ? ` ${j.seconds} 秒` : ''} · <Model provider={j.provider} name={j.model} />
              </span>
              <div className="prog"><span style={{ width: `${pct(j.progress)}%` }} /></div>
              <span className="muted">{j.kind === 'animation' && pct(j.progress) < 100 ? '正在渲染…' : eta(j.eta_seconds)}</span>
              <button className="btn sm" onClick={() => act(() => api.cancelJob(j.id))}>取消</button>
            </div>
          ))}
          {failed.length > 0 && <div className="hint" style={{ margin: '14px 0 6px' }}>没做成</div>}
          {failed.map((j) => (
            <div key={j.id} className="job failed">
              <GroupChip group="media" />
              <span className="name"><span className="muted">{KIND_LABEL[j.kind]}</span> {titleOf(j.prompt_excerpt)} · <Model provider={j.provider} name={j.model} /></span>
              <span className="grow err-text">{j.error ?? '生成失败：10 分钟内没有完成'}</span>
              <button className="btn sm" onClick={() => (j.retry_options && j.retry_options.length > 1 ? setPick(j) : act(() => api.retryJob(j.id, j.retry_options?.[0]?.model)))}>换个模型重做</button>
              <button className="btn sm" title="只删这条记录" onClick={() => act(() => api.deleteJob(j.id))}>删除</button>
            </div>
          ))}
          <div className="hint" style={{ margin: '14px 0 6px' }}>已完成</div>
          {data.items.length === 0 && data.jobs.filter((j) => j.status === 'done').length === 0 && <Empty>还没有生成过{kind === 'all' ? '媒体' : KIND_LABEL[kind]}；在快速面板里说一句试试</Empty>}
          <div className="gal">
            {data.jobs.filter((j) => j.status === 'done').map((j) => (
              <div key={j.id} className="thumb" onClick={() => j.kind === 'animation' && setOpen(j)}>
                <Preview kind={j.kind} path={j.result_path} />
                <div className="meta"><b>{j.title ?? titleOf(j.prompt_excerpt)}</b><div className="muted"><Vendor provider={j.provider} />{j.model}{j.seconds ? ` · ${j.seconds} 秒` : ''}{jobCost(j) && ` · ${jobCost(j)}`}</div></div>
              </div>
            ))}
            {data.items.map((c) => <Thumb key={c.id} c={c} onDelete={() => act(() => api.deleteCall(c.id))} />)}
          </div>
          <div className="hint" style={{ marginTop: 10 }}>文件存在各项目的 assets 文件夹里；删除记录不会删文件</div>
        </>
      )}
      {open && <AnimationDetail job={open} onClose={() => setOpen(null)} onChanged={(j) => { setOpen(j); void reload(); }} />}
      {pick && (
        <Modal title="换个模型重做" onClose={() => setPick(null)}>
          {pick.retry_options!.map((o) => (
            <div key={o.model} className="setrow">
              <div><div className="tt"><Model provider={pick.provider} name={o.model} /></div><div className="ds"><LaneChip lane={o.lane} /> 约 {usd(o.est_cost_usd)}</div></div>
              <button className="btn sm pri" onClick={() => { void act(() => api.retryJob(pick.id, o.model)); setPick(null); }}>用它重做</button>
            </div>
          ))}
        </Modal>
      )}
    </>
  );
}

/** 缩略图：图片直接显示，视频和动画取首帧；拿不到文件时退回色块 */
function Preview({ kind, path }: { kind: string; path?: string }) {
  const src = fileUrl(path);
  if (src && kind === 'image') return <div className="img"><img src={src} alt="" /></div>;
  if (src) return <div className={`img ${kind === 'animation' ? 'anim' : 'video'}`}><video src={src} muted preload="metadata" playsInline /></div>;
  return <div className={`img ${kind === 'animation' ? 'anim' : kind === 'video' ? 'video' : ''}`}>{kind === 'image' ? '' : '▶'}</div>;
}

function Thumb({ c, onDelete }: { c: Call; onDelete: () => void }) {
  const m = cost(c);
  const label = m.muted ? 'ChatGPT 订阅出图' : m.text;
  const title = (c.prompt_excerpt ?? '').replace(/<system-reminder>[\s\S]*?<\/system-reminder>/g, '').trim().slice(0, 60) || c.task_name || c.task_type;
  return (
    <div className="thumb" title={c.result_path} onDoubleClick={() => c.result_path && desktop.revealInFinder(c.result_path)}>
      <button className="x" title="只删记录，不删文件" onClick={(e) => { e.stopPropagation(); onDelete(); }}>×</button>
      <Preview kind={c.kind} path={c.result_path} />
      <div className="meta"><b>{title}</b><div className="muted"><Vendor provider={c.provider} />{m.muted ? label : <>{c.model_display ?? c.model}{c.units && c.unit === 'second' ? ` · ${c.units} 秒` : ''} · {label}</>}</div></div>
    </div>
  );
}

/** 4.12 动画详情：重做是在现有代码上改，不从头生成 */
export function AnimationDetail({ job, onClose, onChanged }: { job: Job; onClose: () => void; onChanged: (j: Job) => void }) {
  const [instruction, setInstruction] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<ApiError | null>(null);
  const redo = async () => {
    if (!instruction.trim()) return;
    setBusy(true); setErr(null);
    try { onChanged(await api.reviseJob(job.id, instruction.trim())); setInstruction(''); toast('已开始重做'); } catch (e) { setErr(e as ApiError); } finally { setBusy(false); }
  };
  return (
    <Modal title={`${job.title ?? job.prompt_excerpt} · ${job.seconds ?? 8} 秒`} onClose={onClose} width={760}>
      <div className="anim-wrap" style={{ margin: -16 }}>
        <div className="preview">
          <div className="screen">{job.title ?? job.prompt_excerpt} ▶</div>
          <div className="row mt12">
            <RevealButton path={job.result_path} />
            <button className="btn sm" disabled={!job.result_path} onClick={() => toast('导出 GIF 由后台完成，做好后会在媒体库出现')}>导出 GIF</button>
            <button className="btn sm" disabled={!job.code_dir} onClick={() => job.code_dir && desktop.revealInFinder(job.code_dir)}>打开代码</button>
          </div>
        </div>
        <div className="info">
          <div className="kv" style={{ gridTemplateColumns: '80px 1fr' }}>
            <span>描述</span><span>{job.prompt_excerpt}</span>
            <span>写代码</span><span><Model provider={job.provider} name={job.model} /> {job.lane && <LaneChip lane={job.lane} />}</span>
            {job.render_note && <><span>渲染</span><span>{job.render_note}</span></>}
            {job.size && <><span>尺寸</span><span>{job.size}{job.fps ? `，${job.fps} 帧` : ''}</span></>}
            {job.result_path && <><span>文件</span><span style={{ wordBreak: 'break-all' }}>{job.result_path}</span></>}
            {job.status !== 'done' && <><span>状态</span><span>{job.status === 'failed' ? job.error : `正在渲染… ${Math.round(pct(job.progress))}%`}</span></>}
          </div>
          <div className="hint" style={{ margin: '10px 0 4px' }}>改一句重做</div>
          <input className="input" placeholder="标题改成从下往上滑入，再慢一点" value={instruction} onChange={(e) => setInstruction(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && redo()} />
          {err && <div className="hint err-text mt8">{err.message}</div>}
          <div className="mt8"><button className="btn pri sm" disabled={busy || !instruction.trim()} onClick={redo}>{busy ? '正在重做…' : '重做'}</button>　<span className="hint">在这一版代码上改，通常 1 分钟内</span></div>
        </div>
      </div>
    </Modal>
  );
}
