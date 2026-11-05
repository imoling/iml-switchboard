import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { api, ApiError } from '../api/client';
import type { Decision, ExplicitRule, MediaTier, PickTier, Rules as RulesT, TaskRule, Tier, TierPick } from '../api/types';
import { ErrorLine, GroupChip, Seg, SetRow, TierChip, Toggle, toast } from '../components/ui';
import { MediaSprite, TierSprite } from '../components/Pixel';
import { TierList } from '../components/TierList';
import { Model, Vendor } from '../components/Vendor';
import { sameList } from '../hostModels';
import { MEDIA_TIER_LABEL, num, usd } from '../format';
import { errText, useDebounced, useLoad } from '../hooks';
import { desktop } from '../tauri';
import { Evaluation } from './Evaluation';
import { SmartCard, SmartResult, judgeReady } from './SmartDispatch';
import { useTheme } from '../theme/ThemeContext';

type Tab = 'task' | 'explicit' | 'context' | 'tiers' | 'eval';
const PICK_TIERS: PickTier[] = ['low', 'mid', 'high'];
const TIERS: Tier[] = ['low', 'mid', 'high'];
const MEDIA_TIERS: MediaTier[] = ['cheap', 'default', 'best'];
/** 一条规则交给谁：文字三档，或画师、影师 */
type Target = Tier | 'image' | 'video';
const targetOf = (r: TaskRule): Target => (r.kind === 'image' || r.kind === 'video' ? r.kind : r.tier);

export function Rules() {
  const { t } = useTheme();
  const [tab, setTab] = useState<Tab>('task');
  const smart = useLoad(() => api.smart(), []);
  const [adding, setAdding] = useState(false);
  useEffect(() => { setAdding(false); }, [tab]);
  const smartMode = !!smart.data?.enabled;
  const rules = useLoad(() => api.rules());
  const sugg = useLoad(() => api.suggestions());
  const [err, setErr] = useState<ApiError | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const exportRules = async () => {
    try {
      const yaml = await api.exportRules();
      const path = await desktop.saveDialog('switchboard-rules.yaml');
      if (path === null && !('__TAURI_INTERNALS__' in window)) { const a = document.createElement('a'); a.href = URL.createObjectURL(new Blob([yaml], { type: 'text/yaml' })); a.download = 'switchboard-rules.yaml'; a.click(); return; }
      if (path) { await desktop.copyText(yaml); toast('规则已复制到剪贴板，粘贴到文件里保存'); }
    } catch (e) { setErr(e as ApiError); }
  };
  const importRules = async (file: File) => {
    try { await api.importRules(await file.text()); toast('规则已导入'); void rules.reload(); } catch (e) { setErr(e as ApiError); }
  };

  const save = async (next: RulesT) => {
    rules.setData(next);
    try { rules.setData(await api.saveRules(next)); setErr(null); } catch (e) { setErr(e as ApiError); void rules.reload(); }
  };

  return (
    <>
      <div className="ct" data-tauri-drag-region>
        <h4 data-tauri-drag-region>{t('nav.rules')}</h4>
        <div className="tools">
          {(tab === 'task' || tab === 'explicit') && <button className="btn pri sm" onClick={() => setAdding(true)}>{tab === 'task' ? '＋ 新建任务规则' : '＋ 添加指令'}</button>}
          {tab === 'task' && <>
            <button className="btn sm" onClick={exportRules}>导出</button>
            <button className="btn sm" onClick={() => fileRef.current?.click()}>导入</button>
            <input ref={fileRef} type="file" accept=".yaml,.yml" style={{ display: 'none' }} onChange={(e) => { const f = e.target.files?.[0]; if (f) void importRules(f); e.target.value = ''; }} />
          </>}
          <Seg value={tab} onChange={setTab} options={[{ id: 'task', label: '分派方案' }, { id: 'explicit', label: '显式指令' }, { id: 'tiers', label: '档位候选' }, { id: 'context', label: '上下文' }, { id: 'eval', label: t('nav.eval') }]} />
        </div>
      </div>
      {(rules.error || err) && <ErrorLine {...errText(rules.error ?? err)!} />}
      {smart.data && tab === 'task' && <SmartCard data={smart.data} setData={smart.setData} reload={() => void smart.reload()} />}
      {tab === 'task' && <TryOne smart={smart.data && smartMode ? { ready: judgeReady(smart.data) } : undefined} />}
      {rules.data && tab === 'task' && (
        <TaskTable rules={rules.data} suggestions={sugg.data ?? []} onSave={save} adding={adding} setAdding={setAdding} smartMode={smartMode}
          onSuggestion={async (task, action) => { try { rules.setData(await api.suggestionAction(task, action)); void sugg.reload(); } catch (e) { setErr(e as ApiError); } }} />
      )}
      {rules.data && tab === 'explicit' && <ExplicitTab rules={rules.data} onSave={save} adding={adding} setAdding={setAdding} />}
      {rules.data && tab === 'tiers' && <TiersTab rules={rules.data} onSave={save} />}
      {tab === 'eval' && <Evaluation embedded />}
      {rules.data && tab === 'context' && (
        <div className="panel">
          {rules.data.context_rules.map((r, i) => (
            <SetRow key={r.id} title={r.title} desc={r.off_effect} last={i === rules.data!.context_rules.length - 1}>
              <Toggle on={r.enabled} onChange={(v) => save({ ...rules.data!, context_rules: rules.data!.context_rules.map((x) => (x.id === r.id ? { ...x, enabled: v } : x)) })} />
            </SetRow>
          ))}
        </div>
      )}
    </>
  );
}

/** 试一句。规则分派：打字就显示规则怎么判。智能分派：点「问判官」，规则和判官同时判，两个结果并排（判官可能在境外，不边打字边发） */
function TryOne({ smart }: { smart?: { ready: boolean } }) {
  const { t } = useTheme();
  const [text, setText] = useState('');
  const dq = useDebounced(text.trim(), 300);
  const { data, error } = useLoad<Decision | null>(() => (dq && !smart ? api.tryRule({ prompt: dq }) : Promise.resolve(null)), [dq, !!smart]);
  const [pair, setPair] = useState<{ rule?: Decision; judge?: Decision; err?: string } | null>(null);
  const [asking, setAsking] = useState(false);
  useEffect(() => { setPair(null); }, [dq]);
  const ask = async () => {
    if (!dq || asking) return;
    setAsking(true);
    try {
      const [rule, judge] = await Promise.all([api.tryRule({ prompt: dq }), api.testSmart({ prompt: dq })]);
      setPair({ rule, judge });
    } catch (e) { setPair({ err: (e as Error).message }); } finally { setAsking(false); }
  };
  const onKey = (e: KeyboardEvent) => { if (e.nativeEvent.isComposing || e.keyCode === 229) return; if (e.key === 'Enter' && smart?.ready) void ask(); };
  const first = data?.candidates.find((c) => !c.skipped) ?? data?.candidates[0];
  const alt = data?.candidates.find((c) => c !== first);
  return (
    <div className="panel soft mb12">
      <div className="row">
        <b style={{ whiteSpace: 'nowrap' }}>试一句</b>
        <input className="input" value={text} onChange={(e) => setText(e.target.value)} onKeyDown={onKey} placeholder="为什么这个 React 组件每次输入都重新渲染，附上报错栈" />
        {smart && <button className="btn sm pri" disabled={!smart.ready || !dq || asking} onClick={ask}>{asking ? '正在判断…' : '问判官'}</button>}
      </div>
      {error && <div className="hint err-text mt8">{error.message}</div>}
      {!smart && data && first && (
        <div className="mt8" style={{ fontSize: 12.5 }}>
          命中 <b>{data.task_name ?? data.task_type}</b>{data.reason[0] && <>（{data.reason[0]}）</>} → <TierChip tier={data.tier} /> → <GroupChip group={first.lane === 'media' ? 'media' : first.lane.endsWith('_sub') ? 'sub' : 'api'} /> <Vendor provider={first.provider} />{first.model}
          {alt && <>　<span className="muted">{alt.skipped ?? '额度紧张时改用'} <Vendor provider={alt.provider} />{alt.model}</span></>}
          {data.sticky && <span className="muted">　同一会话沿用上次的{t('model')}</span>}
        </div>
      )}
      {pair?.err && <div className="hint err-text mt8">{pair.err}</div>}
      {pair?.rule && pair.judge && <BothResults rule={pair.rule} judge={pair.judge} />}
    </div>
  );
}

const pickOf = (d: Decision) => d.candidates.find((c) => !c.skipped);
const KIND_NAME: Record<string, string> = { image: '生图', video: '生视频', animation: '动画' };

/** 规则分派和智能分派各一块：先写用哪个型号，再写为什么，右上角是用时；判官的打分表跟在后面 */
function BothResults({ rule, judge }: { rule: Decision; judge: Decision }) {
  const rp = pickOf(rule);
  const jp = judge.smart?.options?.[0];
  const jm = jp ? { model: jp.model, display: jp.display, provider: jp.provider } : pickOf(judge);
  const same = !!rp && !!jm && rp.model === jm.model;
  const ruleWhy = rule.kind !== 'text' ? `认成${KIND_NAME[rule.kind] ?? '媒体'}请求` : `命中「${rule.task_name ?? rule.task_type}」→ ${rule.tier_label ?? rule.tier ?? ''}`;
  const judgeWhy = jp ? `判官 ${Math.round(jp.prob * 100)}% × ${jp.weight.toFixed(2)}，得分 ${jp.score.toFixed(2)}` : judge.smart?.note ?? judge.reason[judge.reason.length - 1] ?? '';
  return (
    <>
      <div className="try-res">
        <div className="try-card">
          <div className="try-top"><span className="chip">规则分派</span><span className="hint">{rule.latency_ms ?? 0} 毫秒</span></div>
          <div className="try-pick">{rp ? <Model provider={rp.provider} name={rp.display ?? rp.model} /> : '没有可用的模型'}</div>
          <div className="hint">{ruleWhy}</div>
        </div>
        <div className={`try-card ${same ? '' : 'diff'}`}>
          <div className="try-top"><span className="chip c-m">智能分派</span>{!same && rp && jm && <span className="chip c-warn">和规则不同</span>}<span className="grow" /><span className="hint">{judge.latency_ms ?? 0} 毫秒</span></div>
          <div className="try-pick">{jm ? <Model provider={jm.provider} name={jm.display ?? jm.model} /> : '没有可用的模型'}</div>
          <div className="hint">{judgeWhy}</div>
        </div>
      </div>
      {judge.smart?.options?.length ? <SmartResult d={judge} table /> : null}
    </>
  );
}

/** 任务类型表。智能分派时它仍决定是不是出图出视频、给判官定基准档位；判官跨渠道打分，所以「优先渠道」一列不显示 */
function TaskTable({ rules, suggestions, onSave, onSuggestion, adding, setAdding, smartMode }: { rules: RulesT; suggestions: { task_type: string; name?: string; escalations: number; to_tier: Tier; weekly_delta_usd: number }[]; onSave: (r: RulesT) => void; onSuggestion: (task: string, a: 'accept' | 'ignore') => void; adding: boolean; setAdding: (v: boolean) => void; smartMode: boolean }) {
  const { t } = useTheme();
  const blank = { task_type: '', how: '', target: 'low' as Target, media_tier: 'default' as MediaTier };
  const [draft, setDraft] = useState(blank);
  const setRow = (task: string, patch: Partial<TaskRule>) => onSave({ ...rules, task_table: rules.task_table.map((r) => (r.task_type === task ? { ...r, ...patch } : r)) });
  const setTarget = (r: TaskRule, to: Target) => setRow(r.task_type, to === 'image' || to === 'video' ? { kind: to, media_tier: r.media_tier ?? 'default' } : { kind: '', tier: to });
  const toggleLane = (r: TaskRule, lane: 'sub' | 'api') => setRow(r.task_type, { lanes: r.lanes.includes(lane) ? r.lanes.filter((l) => l !== lane) : [...r.lanes, lane] });
  const remove = (task: string) => onSave({ ...rules, task_table: rules.task_table.filter((r) => r.task_type !== task) });
  const isMedia = (x: Target) => x === 'image' || x === 'video';
  const add = () => {
    const name = draft.task_type.trim();
    if (!name) return;
    if (rules.task_table.some((r) => r.task_type === name)) { toast(`已有「${name}」，换个名字`); return; }
    const media = isMedia(draft.target);
    const row: TaskRule = { task_type: name, how: draft.how.trim(), tier: media ? 'low' : (draft.target as Tier), kind: media ? draft.target : '', media_tier: media ? draft.media_tier : undefined, lanes: ['api'], count_7d: 0 };
    onSave({ ...rules, task_table: [row, ...rules.task_table] }); // 新建的排最前，也最先生效
    setAdding(false); setDraft(blank);
  };
  const onKey = (e: KeyboardEvent) => { if (e.nativeEvent.isComposing || e.keyCode === 229) return; if (e.key === 'Enter') add(); if (e.key === 'Escape') setAdding(false); }; // 拼音上屏的回车不算
  const targetLabel = (x: Target) => (x === 'image' ? t('media.image') : x === 'video' ? t('media.video') : t(`tier.${x}`));
  const targetOptions = (withMedia: boolean) => [...TIERS, ...(withMedia ? (['image', 'video'] as Target[]) : [])].map((x) => <option key={x} value={x}>{targetLabel(x)}</option>);
  const mediaTierSelect = (value: MediaTier | undefined, onChange: (v: MediaTier) => void) => (
    <select className="sel" value={value ?? 'default'} onChange={(e) => onChange(e.target.value as MediaTier)}>{MEDIA_TIERS.map((x) => <option key={x} value={x}>{MEDIA_TIER_LABEL[x]}</option>)}</select>
  );
  return (
    <>
      {adding && (
        <div className="panel soft mb12 row">
          <input className="input sm" style={{ width: 140 }} placeholder="任务类型" value={draft.task_type} onChange={(e) => setDraft({ ...draft, task_type: e.target.value })} onKeyDown={onKey} autoFocus />
          <input className="input sm grow" placeholder="关键词，用顿号隔开，比如「拼豆、像素头像」" value={draft.how} onChange={(e) => setDraft({ ...draft, how: e.target.value })} onKeyDown={onKey} />
          <select className="sel" aria-label="交给" value={draft.target} onChange={(e) => setDraft({ ...draft, target: e.target.value as Target })}>{targetOptions(true)}</select>
          {isMedia(draft.target) && mediaTierSelect(draft.media_tier, (v) => setDraft({ ...draft, media_tier: v }))}
          <button className="btn pri sm" disabled={!draft.task_type.trim()} onClick={add}>添加</button><button className="btn sm" onClick={() => setAdding(false)}>取消</button>
        </div>
      )}
      <table className="t">
        <thead><tr><th style={{ width: '22%' }}>任务类型</th><th>识别规则</th><th>{smartMode ? '基准档位' : '交给'}</th>{!smartMode && <th>优先渠道</th>}<th className="right">7 天</th><th /></tr></thead>
        <tbody>
          {rules.task_table.map((r) => {
            const s = suggestions.find((x) => x.task_type === r.task_type);
            const target = targetOf(r);
            const media = r.kind === 'image' || r.kind === 'video';
            return (
              <tr key={r.task_type} className={s || r.suggest ? 'hi' : ''}>
                <td>{r.name ?? r.task_type} {(s || r.suggest) && <span className="chip c-warn">建议调高</span>}</td>
                <td className="muted" style={{ whiteSpace: 'normal' }}>{r.how}</td>
                <td>
                  <span className="row" style={{ gap: 4, flexWrap: 'nowrap' }}>
                    {r.kind === 'animation' ? <span className="chip c-m">动画</span>
                      : r.builtin && media ? <span className="chip c-m">{targetLabel(target)}</span>
                      : <select className="sel" value={target} onChange={(e) => setTarget(r, e.target.value as Target)}>{targetOptions(!r.builtin)}</select>}
                    {media && mediaTierSelect(r.media_tier, (v) => setRow(r.task_type, { media_tier: v }))}
                  </span>
                </td>
                {!smartMode && (
                <td>
                  {!r.kind && (['api', 'sub'] as const).map((l) => (
                    <button key={l} className="btn link" style={{ marginRight: 4, opacity: r.lanes.includes(l) ? 1 : 0.35 }} title={r.lanes.includes(l) ? '点一下取消' : '点一下启用'} onClick={() => toggleLane(r, l)}><GroupChip group={l} /></button>
                  ))}
                </td>
                )}
                <td className="right">{num(r.count_7d)}</td>
                <td className="right">{!r.builtin && <button className="btn link muted" onClick={() => remove(r.task_type)}>删除</button>}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {suggestions.map((s) => (
        <div key={s.task_type} className="hint warn-text mt8 row wrap">
          <span>「{s.name ?? s.task_type}」7 天内被标记判低 {s.escalations} 次，调到{t(`tier.${s.to_tier}`)}后这类任务每周约多花 {usd(s.weekly_delta_usd)}</span>
          <button className="btn sm" onClick={() => onSuggestion(s.task_type, 'accept')}>调高一档</button><button className="btn sm" onClick={() => onSuggestion(s.task_type, 'ignore')}>忽略</button>
        </div>
      ))}
    </>
  );
}

function ExplicitTab({ rules, onSave, adding, setAdding }: { rules: RulesT; onSave: (r: RulesT) => void; adding: boolean; setAdding: (v: boolean) => void }) {
  const { t } = useTheme();
  const [draft, setDraft] = useState({ trigger: '', tier: 'high' as Tier });
  const set = (list: ExplicitRule[]) => onSave({ ...rules, explicit: list });
  const add = () => {
    if (!draft.trigger.trim()) return;
    set([...rules.explicit, { id: `e${Date.now()}`, triggers: draft.trigger.split(/[\s,，]+/).filter(Boolean), effect: 'tier', tier: draft.tier, builtin: false, enabled: true }]);
    setAdding(false); setDraft({ trigger: '', tier: 'high' });
  };
  const onKey = (e: KeyboardEvent) => { if (e.nativeEvent.isComposing || e.keyCode === 229) return; if (e.key === 'Enter') add(); if (e.key === 'Escape') setAdding(false); };
  return (
    <div className="panel">
      <div className="hint mb12">写在请求里的这些词会直接决定档位，优先于其他规则；{t('tier.top')}只在这里和质量模式下出现，规则表不会自动选它</div>
      {adding && (
        <div className="row mb12">
          <input className="input sm" style={{ width: 220 }} placeholder="/关键词，多个用空格隔开" value={draft.trigger} onChange={(e) => setDraft({ ...draft, trigger: e.target.value })} onKeyDown={onKey} autoFocus />
          <select className="sel" value={draft.tier} onChange={(e) => setDraft({ ...draft, tier: e.target.value as Tier })}>{(['low', 'mid', 'high', 'top'] as Tier[]).map((x) => <option key={x} value={x}>{t(`tier.${x}`)}</option>)}</select>
          <button className="btn pri sm" disabled={!draft.trigger.trim()} onClick={add}>添加</button><button className="btn sm" onClick={() => setAdding(false)}>取消</button>
        </div>
      )}
      <table className="t">
        <thead><tr><th>指令</th><th>效果</th><th /></tr></thead>
        <tbody>
          {rules.explicit.map((r) => (
            <tr key={r.id} style={r.enabled ? undefined : { opacity: 0.5 }}>
              <td>{r.triggers.map((x, i) => <span key={i}>{i > 0 && '　'}{x.startsWith('/') ? <code>{x}</code> : x}</span>)}</td>
              <td>{r.effect === 'tier' && r.tier ? <TierChip tier={r.tier} /> : r.effect}</td>
              <td className="right">
                {r.builtin
                  ? <span className="row" style={{ justifyContent: 'flex-end' }}><span className="muted">内置</span><Toggle on={r.enabled} onChange={(v) => set(rules.explicit.map((x) => (x.id === r.id ? { ...x, enabled: v } : x)))} /></span>
                  : <button className="btn sm" onClick={() => set(rules.explicit.filter((x) => x.id !== r.id))}>删除</button>}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** 每档按什么顺序试：勾选、排序，第一个先试；保存后立刻生效，对所有宿主一样 */
function TiersTab({ rules, onSave }: { rules: RulesT; onSave: (r: RulesT) => void }) {
  const { t } = useTheme();
  return (
    <>
      <div className="hint mb12">全局顺序，各应用可在应用页自己排；带工具的请求跳过订阅</div>
      {PICK_TIERS.map((tier) => <TierPanel key={tier} icon={<TierSprite tier={tier} scale={2} />} title={t(`tier.${tier}`)} table={rules.tiers?.[tier]} onSave={(ids) => onSave({ ...rules, tier_picks: { [tier]: ids } })} />)}
      {(['image', 'video'] as const).map((k) => (
        <TierPanel key={k} icon={<MediaSprite kind={k} scale={2} />} title={t(k === 'image' ? 'media.image' : 'media.video')} hint="没指定档位时照这个顺序" table={rules.media_order?.[k]} onSave={(ids) => onSave({ ...rules, media_picks: { [k]: ids } })} />
      ))}
    </>
  );
}

function TierPanel({ icon, title, hint, table, onSave }: { icon: ReactNode; title: string; hint?: string; table?: { custom: boolean; picks: TierPick[] }; onSave: (ids: string[]) => void }) {
  const saved = table?.picks.filter((p) => p.on).map((p) => p.id) ?? [];
  const [sel, setSel] = useState<string[]>(saved);
  useEffect(() => { setSel(saved); }, [saved.join('|')]); // eslint-disable-line react-hooks/exhaustive-deps
  if (!table) return null;
  const dirty = !sameList(sel, saved);
  return (
    <div className="panel mb12">
      <div className="row mb6">{icon}<b>{title}</b>{table.custom && <span className="chip c-warn">手排</span>}{hint && <span className="hint">{hint}</span>}
        <span className="grow" />
        {table.custom && <button className="btn sm" onClick={() => onSave([])}>回到默认</button>}
        <button className="btn pri sm" disabled={!dirty} onClick={() => onSave(sel)}>保存</button>
      </div>
      <TierList picks={table.picks} sel={sel} onChange={setSel} />
    </div>
  );
}
