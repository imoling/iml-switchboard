import { useCallback, useEffect, useRef, useState } from 'react';
import { IS_WINDOWS } from '../copy';
import { api, ApiError, quickRun } from '../api/client';
import type { Call, Job, QuickPreview, Tier } from '../api/types';
import { LaneChip, TierChip, ToastHost, toast } from '../components/ui';
import { Vendor } from '../components/Vendor';
import { MEDIA_TIER_LABEL, cost, secs, setMoney, usd } from '../format';
import { useDebounced } from '../hooks';
import { desktop, inTauri } from '../tauri';

type Alt = QuickPreview['alternatives'][number];
interface Result { text: string; imagePath?: string; job?: Job; call?: Call; error?: string; detail?: string }

export function Quick() {
  const [prompt, setPrompt] = useState('');
  const [imagePath, setImagePath] = useState<string | undefined>();
  const [preview, setPreview] = useState<QuickPreview | null>(null);
  // Tab 换的模型
  const [picked, setPicked] = useState<Alt | null>(null);
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<Result | null>(null);
  const [dropping, setDropping] = useState(false);
  const [startedAt, setStartedAt] = useState(0);
  const abort = useRef<AbortController | null>(null);
  const ta = useRef<HTMLTextAreaElement>(null);
  const dq = useDebounced(prompt.trim(), 250);

  const reset = useCallback(() => { abort.current?.abort(); setPrompt(''); setImagePath(undefined); setPreview(null); setPicked(null); setResult(null); setRunning(false); }, []);
  const close = useCallback(() => { reset(); void desktop.hide('quick'); }, [reset]);

  // 费用按设置里的币种显示：每次呼出时读一次，设置改了下次呼出就生效
  const [, setMoneyTick] = useState(0);
  const loadMoney = useCallback(() => { void api.status().then((st) => { setMoney(st.currency, st.usd_per_cny); setMoneyTick((n) => n + 1); }).catch(() => {}); }, []);
  useEffect(() => { loadMoney(); }, [loadMoney]);
  useEffect(() => { let un = () => {}; void desktop.onShown(() => { ta.current?.focus(); loadMoney(); }).then((u) => { un = u; }); return () => un(); }, [loadMoney]);
  useEffect(() => { ta.current?.focus(); }, []);

  // 发送前预览：将使用谁、多少钱
  useEffect(() => {
    if (!dq || result) { if (!dq) setPreview(null); return; }
    let alive = true;
    api.quickPreview({ prompt: dq, image_path: imagePath }).then((p) => { if (alive) { setPreview(p); setPicked(null); } }).catch(() => { if (alive) setPreview(null); });
    return () => { alive = false; };
  }, [dq, imagePath, result]);

  // 拖入图片（Tauri 给路径；浏览器里只能拿到文件名）
  useEffect(() => {
    if (!inTauri) return;
    let un = () => {};
    void import('@tauri-apps/api/webview').then(async ({ getCurrentWebview }) => {
      un = await getCurrentWebview().onDragDropEvent((e) => {
        if (e.payload.type === 'over' || e.payload.type === 'enter') setDropping(true);
        else if (e.payload.type === 'leave') setDropping(false);
        else if (e.payload.type === 'drop') { setDropping(false); const p = e.payload.paths.find((x) => /\.(png|jpe?g|webp|gif)$/i.test(x)); if (p) setImagePath(p); }
      });
    });
    return () => un();
  }, []);

  const run = async (opts: { best?: boolean; alt?: Alt | null; override?: { tier?: Tier; model?: string; lane?: string } } = {}) => {
    const p = prompt.trim();
    if (!p || running) return;
    const alt = opts.alt === undefined ? picked : opts.alt;
    const body: Parameters<typeof quickRun>[0] = { prompt: p, image_path: imagePath };
    if (opts.override) Object.assign(body, opts.override);
    else if (opts.best) { if (preview?.kind === 'text') body.tier = 'top'; else { const b = preview?.alternatives.find((a) => a.media_tier === 'best'); if (b) { body.model = b.model; body.lane = b.lane; } } }
    else if (alt) { body.model = alt.model; body.lane = alt.lane; if (alt.tier) body.tier = alt.tier; }
    setRunning(true); setResult({ text: '' }); setStartedAt(Date.now());
    abort.current = new AbortController();
    try {
      await quickRun(body, (e) => {
        if (e.type === 'text_delta') setResult((r) => ({ ...(r ?? { text: '' }), text: (r?.text ?? '') + e.text }));
        if (e.type === 'media') setResult((r) => ({ ...(r ?? { text: '' }), imagePath: e.path }));
        if (e.type === 'job') setResult((r) => ({ ...(r ?? { text: '' }), job: e.job }));
        if (e.type === 'done') setResult((r) => ({ ...(r ?? { text: '' }), call: e.call }));
        if (e.type === 'error') setResult((r) => ({ ...(r ?? { text: '' }), error: e.message }));
      }, abort.current.signal);
    } catch (e) {
      if ((e as Error).name !== 'AbortError') setResult((r) => ({ ...(r ?? { text: '' }), error: (e as ApiError).message, detail: (e as ApiError).detail }));
    } finally { setRunning(false); }
  };

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') { e.preventDefault(); if (result) { setResult(null); } else close(); return; }
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void run({ best: e.metaKey || e.ctrlKey }); return; }
    if (e.key === 'Tab' && preview?.alternatives.length) {
      e.preventDefault();
      const list: (Alt | null)[] = [null, ...preview.alternatives];
      const i = list.findIndex((a) => a?.model === picked?.model);
      setPicked(list[(i + 1) % list.length]);
    }
  };

  const first = preview?.candidates.find((c) => !c.skipped) ?? preview?.candidates[0];
  const shown = picked ?? (first ? { label: '', lane: first.lane, provider: first.provider ?? '', model: first.model, display: first.display, est_cost_usd: first.est_cost_usd } : null);
  const isMedia = preview && preview.kind !== 'text';
  const tierLabel = (a: Alt | null) => (isMedia ? MEDIA_TIER_LABEL[a?.media_tier ?? preview?.media_tier ?? 'default'] : a?.tier ? <TierChip tier={a.tier} /> : <TierChip tier={preview?.tier ?? 'low'} />);
  const call = result?.call;
  const elapsed = call ? secs(call.latency_ms) : running ? secs(Date.now() - startedAt) : '';

  return (
    <div className={`palette ${dropping ? 'dropping' : ''}`} style={{ width: '100%', borderRadius: 14 }} onDragOver={(e) => { e.preventDefault(); setDropping(true); }} onDragLeave={() => setDropping(false)} onDrop={(e) => { e.preventDefault(); setDropping(false); const f = e.dataTransfer.files[0]; if (f && !inTauri) setImagePath(f.name); }}>
      <div className="top drag" data-tauri-drag-region>
        <textarea ref={ta} rows={prompt.length > 60 ? 2 : 1} value={prompt} placeholder={imagePath ? '说说要把这张图做成什么' : '问一句、出一张图、做个 5 秒视频…'} onChange={(e) => { setPrompt(e.target.value); if (result && !running) setResult(null); }} onKeyDown={onKey} />
        {imagePath && <div className="hint row"><span className="chip c-m">图片</span><span className="grow" style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{imagePath}</span><button className="btn link" onClick={() => setImagePath(undefined)}>移除</button></div>}
      </div>

      {/* 结果态：顶部始终显示用了谁、花了多少 */}
      {result ? (
        <>
          <div className="route-chip">
            {call ? <>{call.kind === 'text' && call.tier ? <TierChip tier={call.tier} /> : <LaneChip lane={call.lane} />}<b><Vendor provider={call.provider} />{call.model}</b><span className="muted">· {call.kind === 'text' ? (call.lane.endsWith('_sub') ? '订阅' : '按量') : MEDIA_TIER_LABEL[call.media_tier ?? 'default']} · {cost(call).text} · {elapsed}</span></>
              : shown ? <><b><Vendor provider={shown.provider} />{shown.display ?? shown.model}</b><span className="muted">· {running ? '正在生成…' : ''}</span></> : <span className="muted">正在生成…</span>}
            {running && <button className="btn sm" style={{ marginLeft: 'auto' }} onClick={() => abort.current?.abort()}>停止</button>}
          </div>
          <div className="body">
            {result.error && <div className="err-text">{result.error}{result.detail && <details className="raw"><summary>详情</summary><pre>{result.detail}</pre></details>}</div>}
            {result.imagePath && <div className="imgbox">{inTauri && <img src={`asset://localhost/${encodeURIComponent(result.imagePath)}`} alt="" onError={(e) => { (e.target as HTMLImageElement).style.display = 'none'; }} />}</div>}
            {result.job && <div className="row"><span className="chip c-m">视频</span><span className="grow">{result.job.prompt_excerpt} · {result.job.model}</span><div className="prog" style={{ maxWidth: 160 }}><span style={{ width: `${result.job.progress <= 1 ? result.job.progress * 100 : result.job.progress}%` }} /></div><span className="hint">已加入媒体库，做好会通知</span></div>}
            {result.text && <div className="result">{result.text}</div>}
          </div>
          <div className="actions">
            {call?.kind === 'text' && <>
              <button className="btn sm" onClick={async () => { await desktop.copyText(result.text); toast('已复制'); }}>复制</button>
              {call.tier !== 'top' && <button className="btn sm" onClick={() => run({ override: { tier: 'top' } })}>用 Fable 再问</button>}
              <button className="btn sm" onClick={async () => { await desktop.copyText(`${prompt.trim()}\n\n---\n${result.text}`); toast('已复制，到 Claude Code 里粘贴'); }}>发到 Claude Code</button>
            </>}
            {result.imagePath && <>
              <button className="btn sm pri" onClick={async () => { const to = await desktop.saveDialog(result.imagePath!.split('/').pop() ?? 'image.png'); if (to) { await desktop.copyText(result.imagePath!); toast(`原图在 ${result.imagePath}`); } else if (!inTauri) toast('浏览器里没有保存对话框'); }}>存进项目…</button>
              <button className="btn sm" onClick={async () => { const ok = await desktop.copyImage(result.imagePath!); toast(ok ? '已复制图片' : '复制失败：只支持 PNG'); }}>复制图片</button>
              <button className="btn sm" onClick={() => { const alt = preview?.alternatives.find((a) => a.model !== call?.model); void run({ alt: alt ?? null }); }}>换个模型重做</button>
              <button className="btn sm" onClick={() => { setImagePath(result.imagePath); setResult(null); setPrompt(`${prompt.trim()}，做成 5 秒视频`); }}>做成 5 秒视频</button>
            </>}
            <span className="hint" style={{ marginLeft: 'auto' }}>Esc 关闭</span>
          </div>
        </>
      ) : (
        <>
          <div className="route-chip">
            {shown ? <>
              <span className="muted">将使用</span>{isMedia ? <LaneChip lane="media" /> : tierLabel(picked)}
              <b><Vendor provider={shown.provider} />{shown.display ?? shown.model}</b>
              <span className="muted">· {isMedia ? MEDIA_TIER_LABEL[picked?.media_tier ?? preview?.media_tier ?? 'default'] : shown.lane.endsWith('_sub') ? '订阅' : '按量'}{shown.est_cost_usd !== undefined ? ` · 约 ${usd(shown.est_cost_usd)}` : shown.note ? ` · ${shown.note}` : ''}</span>
              {preview && preview.alternatives.length > 0 && (
                <select className="sel" style={{ marginLeft: 'auto' }} value={picked?.model ?? ''} onChange={(e) => setPicked(preview.alternatives.find((a) => a.model === e.target.value) ?? null)}>
                  <option value="">换模型 ▾</option>{preview.alternatives.map((a) => <option key={a.model} value={a.model}>{a.display ?? a.model}</option>)}
                </select>
              )}
            </> : <span className="muted">{prompt.trim() ? '正在判断…' : '输入后先告诉你会用谁、花多少'}</span>}
          </div>
          {preview && preview.alternatives.length > 0 && (
            <div className="body">
              <div className="muted mb6">也可以</div>
              <div className="alts">
                {preview.alternatives.map((a) => (
                  <div key={a.model}><button onClick={() => setPicked(a)} style={picked?.model === a.model ? { fontWeight: 600 } : undefined}>{a.label === '用' ? '用 ' : `${a.label}：`}<Vendor provider={a.provider} />{a.model}{a.est_cost_usd !== undefined ? ` · 约 ${usd(a.est_cost_usd)}` : a.note ? ` · ${a.note}` : ''}</button></div>
                ))}
              </div>
            </div>
          )}
          <div className="foot">
            <span><span className="kbd">↵</span> 发送</span><span><span className="kbd">{IS_WINDOWS ? 'Ctrl' : '⌘'}</span><span className="kbd">↵</span> 用最好档</span><span><span className="kbd">Tab</span> 换模型</span>
            <span style={{ marginLeft: 'auto' }}>拖入图片可做图生视频</span>
          </div>
        </>
      )}
      <ToastHost />
    </div>
  );
}
