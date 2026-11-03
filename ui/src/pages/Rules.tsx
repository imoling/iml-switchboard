import { useEffect, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { Decision, ExplicitRule, PickTier, Rules as RulesT, Tier, TierPick } from '../api/types';
import { ErrorLine, GroupChip, Seg, SetRow, TierChip, Toggle, toast } from '../components/ui';
import { TierSprite } from '../components/Pixel';
import { TierList } from '../components/TierList';
import { Vendor } from '../components/Vendor';
import { sameList } from '../hostModels';
import { usd } from '../format';
import { errText, useDebounced, useLoad } from '../hooks';
import { desktop } from '../tauri';
import { Evaluation } from './Evaluation';
import { useTheme } from '../theme/ThemeContext';

type Tab = 'task' | 'explicit' | 'context' | 'tiers' | 'eval';
const PICK_TIERS: PickTier[] = ['low', 'mid', 'high'];
const TIERS: Tier[] = ['low', 'mid', 'high'];

export function Rules() {
  const { t } = useTheme();
  const [tab, setTab] = useState<Tab>('task');
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
          {tab === 'task' && <>
            <button className="btn sm" onClick={exportRules}>导出</button>
            <button className="btn sm" onClick={() => fileRef.current?.click()}>导入</button>
            <input ref={fileRef} type="file" accept=".yaml,.yml" style={{ display: 'none' }} onChange={(e) => { const f = e.target.files?.[0]; if (f) void importRules(f); e.target.value = ''; }} />
          </>}
          <Seg value={tab} onChange={setTab} options={[{ id: 'task', label: '任务类型' }, { id: 'explicit', label: '显式指令' }, { id: 'tiers', label: '档位候选' }, { id: 'context', label: '上下文' }, { id: 'eval', label: t('nav.eval') }]} />
        </div>
      </div>
      {(rules.error || err) && <ErrorLine {...errText(rules.error ?? err)!} />}
      {tab === 'task' && <TryOne />}
      {rules.data && tab === 'task' && (
        <TaskTable rules={rules.data} suggestions={sugg.data ?? []} onSave={save}
          onSuggestion={async (task, action) => { try { rules.setData(await api.suggestionAction(task, action)); void sugg.reload(); } catch (e) { setErr(e as ApiError); } }} />
      )}
      {rules.data && tab === 'explicit' && <ExplicitTab rules={rules.data} onSave={save} />}
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

function TryOne() {
  const { t } = useTheme();
  const [text, setText] = useState('');
  const dq = useDebounced(text.trim(), 300);
  const { data, error } = useLoad<Decision | null>(() => (dq ? api.tryRule({ prompt: dq }) : Promise.resolve(null)), [dq]);
  const first = data?.candidates.find((c) => !c.skipped) ?? data?.candidates[0];
  const alt = data?.candidates.find((c) => c !== first);
  return (
    <div className="panel soft mb12">
      <div className="row"><b style={{ whiteSpace: 'nowrap' }}>试一句</b><input className="input" value={text} onChange={(e) => setText(e.target.value)} placeholder="为什么这个 React 组件每次输入都重新渲染，附上报错栈" /></div>
      {error && <div className="hint err-text mt8">{error.message}</div>}
      {data && first && (
        <div className="mt8" style={{ fontSize: 12.5 }}>
          命中 <b>{data.task_name ?? data.task_type}</b>{data.reason[0] && <>（{data.reason[0]}）</>} → <TierChip tier={data.tier} /> → <GroupChip group={first.lane === 'media' ? 'media' : first.lane.endsWith('_sub') ? 'sub' : 'api'} /> <Vendor provider={first.provider} />{first.model}
          {alt && <>　<span className="muted">{alt.skipped ?? '额度紧张时改用'} <Vendor provider={alt.provider} />{alt.model}</span></>}
          {data.sticky && <span className="muted">　同一会话沿用上次的{t('model')}</span>}
        </div>
      )}
    </div>
  );
}

function TaskTable({ rules, suggestions, onSave, onSuggestion }: { rules: RulesT; suggestions: { task_type: string; name?: string; escalations: number; to_tier: Tier; weekly_delta_usd: number }[]; onSave: (r: RulesT) => void; onSuggestion: (task: string, a: 'accept' | 'ignore') => void }) {
  const { t } = useTheme();
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState({ task_type: '', how: '', tier: 'low' as Tier });
  const setTier = (task: string, tier: Tier) => onSave({ ...rules, task_table: rules.task_table.map((r) => (r.task_type === task ? { ...r, tier } : r)) });
  const toggleLane = (task: string, lane: 'sub' | 'api') => onSave({ ...rules, task_table: rules.task_table.map((r) => (r.task_type === task ? { ...r, lanes: r.lanes.includes(lane) ? r.lanes.filter((l) => l !== lane) : [...r.lanes, lane] } : r)) });
  const remove = (task: string) => onSave({ ...rules, task_table: rules.task_table.filter((r) => r.task_type !== task) });
  const add = () => {
    if (!draft.task_type.trim()) return;
    onSave({ ...rules, task_table: [...rules.task_table, { task_type: draft.task_type.trim(), how: draft.how.trim(), tier: draft.tier, lanes: ['api'], count_7d: 0 }] });
    setAdding(false); setDraft({ task_type: '', how: '', tier: 'low' });
  };
  return (
    <>
      <table className="t">
        <thead><tr><th style={{ width: '22%' }}>任务类型</th><th>怎么识别</th><th>档位</th><th>优先渠道</th><th className="right">7 天</th><th /></tr></thead>
        <tbody>
          {rules.task_table.map((r) => {
            const s = suggestions.find((x) => x.task_type === r.task_type);
            return (
              <tr key={r.task_type} className={s || r.suggest ? 'hi' : ''}>
                <td>{r.name ?? r.task_type} {(s || r.suggest) && <span className="chip c-warn">建议调高</span>}</td>
                <td className="muted" style={{ whiteSpace: 'normal' }}>{r.how}</td>
                <td>{r.kind ? <span className="chip c-m">媒体</span> : <select className="sel" value={r.tier} onChange={(e) => setTier(r.task_type, e.target.value as Tier)}>{TIERS.map((x) => <option key={x} value={x}>{t(`tier.${x}`)}</option>)}</select>}</td>
                <td>
                  {(['api', 'sub'] as const).map((l) => (
                    <button key={l} className="btn link" style={{ marginRight: 4, opacity: r.lanes.includes(l) ? 1 : 0.35 }} title={r.lanes.includes(l) ? '点一下取消' : '点一下启用'} onClick={() => toggleLane(r.task_type, l)}><GroupChip group={l} /></button>
                  ))}
                </td>
                <td className="right">{r.count_7d}</td>
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
      <div className="mt12">
        {adding ? (
          <div className="row">
            <input className="input sm" style={{ width: 140 }} placeholder="任务类型" value={draft.task_type} onChange={(e) => setDraft({ ...draft, task_type: e.target.value })} autoFocus />
            <input className="input sm grow" placeholder="怎么识别，比如「有表格、要对账」" value={draft.how} onChange={(e) => setDraft({ ...draft, how: e.target.value })} />
            <select className="sel" value={draft.tier} onChange={(e) => setDraft({ ...draft, tier: e.target.value as Tier })}>{TIERS.map((x) => <option key={x} value={x}>{t(`tier.${x}`)}</option>)}</select>
            <button className="btn pri" onClick={add}>添加</button><button className="btn" onClick={() => setAdding(false)}>取消</button>
          </div>
        ) : <button className="btn" onClick={() => setAdding(true)}>＋ 新建任务类型</button>}
      </div>
    </>
  );
}

function ExplicitTab({ rules, onSave }: { rules: RulesT; onSave: (r: RulesT) => void }) {
  const { t } = useTheme();
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState({ trigger: '', tier: 'high' as Tier });
  const set = (list: ExplicitRule[]) => onSave({ ...rules, explicit: list });
  const add = () => {
    if (!draft.trigger.trim()) return;
    set([...rules.explicit, { id: `e${Date.now()}`, triggers: draft.trigger.split(/[\s,，]+/).filter(Boolean), effect: 'tier', tier: draft.tier, builtin: false, enabled: true }]);
    setAdding(false); setDraft({ trigger: '', tier: 'high' });
  };
  return (
    <div className="panel">
      <div className="hint mb12">写在请求里的这些词会直接决定档位，优先于其他规则；{t('tier.top')}只在这里和质量模式下出现，规则表不会自动选它</div>
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
      <div className="mt12">
        {adding ? (
          <div className="row">
            <input className="input sm" style={{ width: 220 }} placeholder="/关键词，多个用空格隔开" value={draft.trigger} onChange={(e) => setDraft({ ...draft, trigger: e.target.value })} autoFocus />
            <select className="sel" value={draft.tier} onChange={(e) => setDraft({ ...draft, tier: e.target.value as Tier })}>{(['low', 'mid', 'high', 'top'] as Tier[]).map((x) => <option key={x} value={x}>{t(`tier.${x}`)}</option>)}</select>
            <button className="btn pri sm" onClick={add}>添加</button><button className="btn sm" onClick={() => setAdding(false)}>取消</button>
          </div>
        ) : <button className="btn sm" onClick={() => setAdding(true)}>＋ 添加指令</button>}
      </div>
    </div>
  );
}

/** 每档按什么顺序试：勾选、排序，第一个先试；保存后立刻生效，对所有宿主一样 */
function TiersTab({ rules, onSave }: { rules: RulesT; onSave: (r: RulesT) => void }) {
  return (
    <>
      <div className="hint mb12">全局顺序，宿主可在接入页自己排；带工具的请求跳过订阅</div>
      {PICK_TIERS.map((tier) => <TierPanel key={tier} tier={tier} table={rules.tiers?.[tier]} onSave={(ids) => onSave({ ...rules, tier_picks: { [tier]: ids } })} />)}
    </>
  );
}

function TierPanel({ tier, table, onSave }: { tier: PickTier; table?: { custom: boolean; picks: TierPick[] }; onSave: (ids: string[]) => void }) {
  const { t } = useTheme();
  const saved = table?.picks.filter((p) => p.on).map((p) => p.id) ?? [];
  const [sel, setSel] = useState<string[]>(saved);
  useEffect(() => { setSel(saved); }, [saved.join('|')]); // eslint-disable-line react-hooks/exhaustive-deps
  if (!table) return null;
  const dirty = !sameList(sel, saved);
  return (
    <div className="panel mb12">
      <div className="row mb6"><TierSprite tier={tier} scale={2} /><b>{t(`tier.${tier}`)}</b>{table.custom && <span className="chip c-warn">手排</span>}
        <span className="grow" />
        {table.custom && <button className="btn sm" onClick={() => onSave([])}>回到默认</button>}
        <button className="btn pri sm" disabled={!dirty} onClick={() => onSave(sel)}>保存</button>
      </div>
      <TierList picks={table.picks} sel={sel} onChange={setSel} />
    </div>
  );
}
