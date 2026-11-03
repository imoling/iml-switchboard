import { useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { HostModel, HostModelList, IntegrationId } from '../api/types';
import { errText } from '../hooks';
import { moveItem, sameList, splitModels, toggleId } from '../hostModels';
import { ErrorLine, Modal, toast } from './ui';
import { Vendor } from './Vendor';

/** 保存之后宿主什么时候才看得到 */
const TAKES_EFFECT: Partial<Record<IntegrationId, string>> = {
  codex: '保存后彻底重启 Codex 生效',
  workbuddy: '保存后重开 WorkBuddy 生效',
  pi: '保存后下次启动 pi 生效',
  deepseek_harness: '保存后下次启动 dsh 生效',
  openclaw: '保存后重启 OpenClaw 网关生效',
};

/** 「订阅优先」这一行要让人知道它接不了带工具的回合 */
const SUB_ONLY_NOTE = '只接不带工具的对话';

/** 宿主菜单里列哪些型号：勾选、排序，第一个是默认；保存后立刻写进宿主 */
export function HostModelDialog({ host, hostName, onClose, onSaved }: { host: IntegrationId; hostName: string; onClose: () => void; onSaved?: (count: number) => void }) {
  const [models, setModels] = useState<HostModel[] | null>(null);
  const [saved, setSaved] = useState<string[]>([]);
  const [custom, setCustom] = useState(false);
  const [sel, setSel] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<ApiError | null>(null);

  const load = (l: HostModelList) => {
    const on = l.models.filter((m) => m.on).map((m) => m.id);
    setModels(l.models); setSaved(on); setSel(on); setCustom(l.custom);
  };
  useEffect(() => { api.hostModels(host).then(load).catch((e) => setErr(e as ApiError)); }, [host]);

  const save = async (ids: string[]) => {
    setBusy(true); setErr(null);
    try {
      const l = await api.setHostModels(host, ids);
      onSaved?.(l.models.filter((m) => m.on).length);
      toast(ids.length ? '已写进去' : '已回到默认');
      onClose();
    } catch (e) { setErr(e as ApiError); setBusy(false); }
  };

  const { chosen, rest } = splitModels(models ?? [], sel);
  const dirty = !sameList(sel, saved);
  return (
    <Modal title={`${hostName} 的模型列表`} onClose={onClose} width={560}>
      {err && <ErrorLine {...errText(err)!} />}
      {!models && !err && <div className="hint">正在读取…</div>}
      {models && (
        <div className="hm-scroll">
          {chosen.map((m, i) => (
            <div key={m.id} className="hm-row">
              <input type="checkbox" checked disabled={chosen.length === 1} onChange={() => setSel(toggleId(sel, m.id))} />
              <Vendor provider={m.icon} model={m.id} />
              <span className="grow">{m.name}{m.id === 'router-sub' && <span className="hint">　{SUB_ONLY_NOTE}</span>}</span>
              {i === 0 && <span className="chip hm-def">默认</span>}
              <button className="hm-mv" disabled={i === 0} aria-label="上移" onClick={() => setSel(moveItem(sel, i, i - 1))}>↑</button>
              <button className="hm-mv" disabled={i === chosen.length - 1} aria-label="下移" onClick={() => setSel(moveItem(sel, i, i + 1))}>↓</button>
            </div>
          ))}
          {rest.map((g) => (
            <div key={g.group} className="hm-group">
              <div className="hint">{g.group}</div>
              {g.items.map((m) => (
                <div key={m.id} className="hm-row">
                  <input type="checkbox" checked={false} onChange={() => setSel(toggleId(sel, m.id))} />
                  <Vendor provider={m.icon} model={m.id} />
                  <span className="grow">{m.name}{m.id === 'router-sub' && <span className="hint">　{SUB_ONLY_NOTE}</span>}</span>
                </div>
              ))}
            </div>
          ))}
        </div>
      )}
      <div className="hm-foot">
        {custom && <button className="btn sm" disabled={busy} onClick={() => save([])}>回到默认</button>}
        <span className="grow hint">{TAKES_EFFECT[host]}</span>
        <button className="btn sm" disabled={busy} onClick={onClose}>取消</button>
        <button className="btn pri sm" disabled={!dirty || busy} onClick={() => save(sel)}>{busy ? '正在保存…' : '保存'}</button>
      </div>
    </Modal>
  );
}
