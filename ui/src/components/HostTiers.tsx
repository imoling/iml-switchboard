import { useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { HostTiers, IntegrationId, PickKey, PickTier } from '../api/types';
import { errText } from '../hooks';
import { sameList } from '../hostModels';
import { useTheme } from '../theme/ThemeContext';
import { TierSprite } from './Pixel';
import { TierList } from './TierList';
import { ErrorLine, Modal, toast } from './ui';

const TIERS: PickTier[] = ['low', 'mid', 'high'];
const MEDIA_KINDS: ('image' | 'video')[] = ['image', 'video'];
const ALL_KEYS: PickKey[] = ['low', 'mid', 'high', 'image', 'video'];
type Sel = Record<PickKey, string[]>;
type Own = Record<PickKey, boolean>;

/** 一个应用自己的档位与媒体候选：每项可以自己排，也可以跟全局；一次保存五项 */
export function HostTiersDialog({ host, hostName, onClose, onSaved }: { host: IntegrationId; hostName: string; onClose: () => void; onSaved?: (customCount: number) => void }) {
  const { t } = useTheme();
  const [data, setData] = useState<HostTiers | null>(null);
  const [sel, setSel] = useState<Sel>({ low: [], mid: [], high: [], image: [], video: [] });
  const [own, setOwn] = useState<Own>({ low: false, mid: false, high: false, image: false, video: false });
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<ApiError | null>(null);

  const load = (d: HostTiers) => {
    setData(d);
    setSel({ low: onIds(d, 'low'), mid: onIds(d, 'mid'), high: onIds(d, 'high'), image: onIds(d, 'image'), video: onIds(d, 'video') });
    setOwn({ low: d.low.custom, mid: d.mid.custom, high: d.high.custom, image: !!d.image?.custom, video: !!d.video?.custom });
  };
  useEffect(() => { api.hostTiers(host).then(load).catch((e) => setErr(e as ApiError)); }, [host]);

  const dirty = !!data && ALL_KEYS.some((k) => {
    const tb = data[k];
    if (!tb) return false;
    return own[k] !== tb.custom || (own[k] && !sameList(sel[k], onIds(data, k)));
  });
  const save = async () => {
    setBusy(true); setErr(null);
    try {
      const payload: Partial<Record<PickKey, string[]>> = {
        low: own.low ? sel.low : [],
        mid: own.mid ? sel.mid : [],
        high: own.high ? sel.high : [],
      };
      if (data?.image) payload.image = own.image ? sel.image : [];
      if (data?.video) payload.video = own.video ? sel.video : [];
      const d = await api.setHostTiers(host, payload);
      const n = ALL_KEYS.filter((k) => d[k]?.custom).length;
      onSaved?.(n);
      toast(n ? '已保存，立刻生效' : '已改回跟全局');
      onClose();
    } catch (e) { setErr(e as ApiError); setBusy(false); }
  };

  return (
    <Modal title={`${hostName} 的档位候选`} onClose={onClose} width={600}>
      {err && <ErrorLine {...errText(err)!} />}
      {!data && !err && <div className="hint">正在读取…</div>}
      {data && (
        <div className="hm-scroll">
          <div className="hint mb12">只对这个应用生效；没自己排的项跟全局</div>
          {TIERS.map((k) => (
            <div key={k} className="mb12">
              <div className="row mb6">
                <TierSprite tier={k} scale={2} /><b>{t(`tier.${k}`)}</b>
                <span className={`chip ${own[k] ? 'c-warn' : ''}`}>{own[k] ? '自己排' : '跟全局'}</span>
                <span className="grow" />
                {own[k] && <button className="btn sm" onClick={() => { setSel({ ...sel, [k]: data[k].inherited ?? onIds(data, k) }); setOwn({ ...own, [k]: false }); }}>改回跟全局</button>}
              </div>
              <TierList picks={data[k].picks} sel={sel[k]} onChange={(ids) => { setSel({ ...sel, [k]: ids }); setOwn({ ...own, [k]: true }); }} />
            </div>
          ))}
          {MEDIA_KINDS.map((k) => {
            const tb = data[k];
            if (!tb || tb.picks.length === 0) return null;
            return (
              <div key={k} className="mb12">
                <div className="row mb6">
                  <b>{t(`media.${k}`)}</b>
                  <span className={`chip ${own[k] ? 'c-warn' : ''}`}>{own[k] ? '自己排' : '跟全局'}</span>
                  <span className="grow" />
                  {own[k] && <button className="btn sm" onClick={() => { setSel({ ...sel, [k]: tb.inherited ?? onIds(data, k) }); setOwn({ ...own, [k]: false }); }}>改回跟全局</button>}
                </div>
                <TierList picks={tb.picks} sel={sel[k]} onChange={(ids) => { setSel({ ...sel, [k]: ids }); setOwn({ ...own, [k]: true }); }} />
              </div>
            );
          })}
        </div>
      )}
      <div className="hm-foot">
        <span className="grow" />
        <button className="btn sm" disabled={busy} onClick={onClose}>取消</button>
        <button className="btn pri sm" disabled={!dirty || busy} onClick={save}>{busy ? '正在保存…' : '保存'}</button>
      </div>
    </Modal>
  );
}

function onIds(d: HostTiers, k: PickKey): string[] {
  return (d[k]?.picks ?? []).filter((p) => p.on).map((p) => p.id);
}
