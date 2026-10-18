import { useState } from 'react';
import { api, ApiError } from '../api/client';
import type { Account, Tier } from '../api/types';
import { Dot, ErrorLine, Modal, toast } from '../components/ui';
import { MediaSprite, TierSprite } from '../components/Pixel';
import { VendorLogo } from '../components/Vendor';
import { cny, usd } from '../format';
import { errText, useLoad } from '../hooks';
import { useTheme } from '../theme/ThemeContext';
import { desktop } from '../tauri';

const API_PROVIDERS = [
  { id: 'zhipu', label: '智谱', url: 'https://open.bigmodel.cn' },
  { id: 'dashscope', label: '百炼', url: 'https://bailian.console.aliyun.com' },
  { id: 'deepseek', label: 'DeepSeek', url: 'https://platform.deepseek.com' },
  { id: 'ark', label: '火山方舟', url: 'https://console.volcengine.com/ark' },
  { id: 'agnes', label: 'Agnes AI', url: 'https://www.agnes-ai.cn' },
  { id: 'moonshot', label: 'Moonshot', url: 'https://platform.moonshot.cn' },
  { id: 'minimax', label: 'MiniMax', url: 'https://platform.minimaxi.com' },
  { id: 'openrouter', label: 'OpenRouter', url: 'https://openrouter.ai' },
];

const STATUS: Record<Account['status'], { dot: 'ok' | 'warn' | 'no' | 'err'; text: (a: Account) => string }> = {
  ok: { dot: 'ok', text: () => '可用' },
  warn: { dot: 'warn', text: (a) => `额度 ${a.quota?.[0]?.pct ?? ''}%` },
  missing: { dot: 'no', text: () => '未连接' },
  error: { dot: 'err', text: (a) => (/余额/.test(a.detail) || a.top_up_url ? '余额不足' : '出错') },
  cooldown: { dot: 'warn', text: () => '额度暂时用完' },
  disabled: { dot: 'no', text: () => '已停用' },
};

export function Accounts() {
  const { t } = useTheme();
  const { data, setData, error, reload } = useLoad(() => api.accounts());
  const [adding, setAdding] = useState(false);
  const [err, setErr] = useState<ApiError | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [tierEdit, setTierEdit] = useState<Account | null>(null);
  const subs = data?.filter((a) => a.kind === 'subscription') ?? [];
  const apis = data?.filter((a) => a.kind === 'api') ?? [];

  const act = async (id: string, fn: () => Promise<unknown>) => {
    setBusy(id); setErr(null);
    try { await fn(); await reload(); } catch (e) { setErr(e as ApiError); } finally { setBusy(null); }
  };
  const monthCost = (a: Account) => (a.month_cost_native !== undefined && a.currency === 'CNY' ? cny(a.month_cost_native) : a.month_cost_usd !== undefined ? usd(a.month_cost_usd) : '');
  const ORDER = ['top', 'high', 'mid', 'low'];
  const name = (a: Account, m: string) => (m ? a.labels?.[m] ?? m : '不用');
  const sortedTiers = (a: Account) => Object.entries(a.tiers ?? {}).sort(([x], [y]) => ORDER.indexOf(x) - ORDER.indexOf(y));
  const isTier = (k: string): k is Tier => ['low', 'mid', 'high', 'top'].includes(k);
  /** 档位一行瓦片，不做成按钮；点任意一块进编辑 */
  const tierTiles = (a: Account) => (
    <div className="tiles" role="button" tabIndex={0} title="点一下改各档位用谁" onClick={() => setTierEdit(a)} onKeyDown={(e) => { if (e.key === 'Enter') setTierEdit(a); }}>
      {sortedTiers(a).map(([k, v]) => (
        <span key={k} className={`tile ${v ? '' : 'off'}`}>{isTier(k) && <TierSprite tier={k} scale={2} />}<span className="tn">{isTier(k) ? t(`tier.${k}`) : k}</span><span className="tm">{name(a, v)}</span></span>
      ))}
      {(a.media ?? []).map((m) => (
        <span key={m.model} className="tile media" title={m.model}><MediaSprite kind={m.kind} scale={2} /><span className="chip c-m">{t(m.kind === 'video' ? 'media.video' : 'media.image')}</span><span className="tm">{m.display}</span></span>
      ))}
      <span className="chev">▾</span>
    </div>
  );

  return (
    <>
      <div className="ct">
        <h4>{t('nav.accounts')}</h4>
        <div className="tools">
          <button className="btn" disabled={busy === 'detect'} onClick={() => act('detect', () => api.detectAccounts())}>{busy === 'detect' ? '正在检测…' : '重新检测本机命令'}</button>
          <button className="btn" onClick={() => setAdding(true)}>＋ 添加按量付费账号</button>
        </div>
      </div>
      {(error || err) && <ErrorLine {...errText(error ?? err)!} />}
      {data && (
        <>
          <div className="hint mb6">订阅</div>
          {subs.map((a) => (
            <div key={a.id} className={`acct ${a.enabled ? '' : 'dim'}`}>
              <VendorLogo provider={a.provider} />
              <div className="grow"><b>{a.label}</b>{a.plan && <span className="plan">{a.plan}</span>}<div className="hint">{a.detail}</div></div>
              <span><Dot s={STATUS[a.status].dot} />{STATUS[a.status].text(a)}</span>
              {a.status === 'missing'
                ? <button className="btn sm" onClick={() => a.install_hint && toast(`在终端运行：${a.install_hint}`)}>怎么安装</button>
                : null}
              {((a.tiers && Object.keys(a.tiers).length > 0) || (a.media && a.media.length > 0)) && tierTiles(a)}
              {a.status === 'cooldown' && <span className="hint">{a.detail}</span>}
            </div>
          ))}
          <div className="hint" style={{ margin: '14px 0 6px' }}>按量付费</div>
          {apis.length === 0 && <div className="hint mb12">还没有按量付费账号；杂活会先用订阅额度</div>}
          {apis.map((a) => (
            <div key={a.id} className={`acct ${a.enabled ? '' : 'dim'}`}>
              <VendorLogo provider={a.provider} />
              <div className="grow"><b>{a.label}</b>{a.plan && <span className="plan">{a.plan}</span>}<div className="hint">{a.detail}</div></div>
              <span><Dot s={STATUS[a.status].dot} />{STATUS[a.status].text(a)}</span>
              {a.status === 'error' && a.top_up_url
                ? <button className="btn sm" onClick={() => desktop.openUrl(a.top_up_url!)}>去充值</button>
                : a.status === 'error'
                  ? <button className="btn sm" disabled={busy === a.id} onClick={() => act(a.id, () => api.verifyAccount(a.id))}>重新验证</button>
                  : <span className="muted">{monthCost(a) && `本月 ${monthCost(a)}`}</span>}

              <select className="sel" value="" onChange={(e) => { const v = e.target.value; if (v === 'toggle') void act(a.id, () => api.updateAccount(a.id, { enabled: !a.enabled })); if (v === 'verify') void act(a.id, () => api.verifyAccount(a.id)); if (v === 'delete') void act(a.id, () => api.deleteAccount(a.id)); }}>
                <option value="">操作 ▾</option><option value="verify">重新验证</option><option value="toggle">{a.enabled ? '停用' : '启用'}</option><option value="delete">删除</option>
              </select>
              {((a.tiers && Object.keys(a.tiers).length > 0) || (a.media && a.media.length > 0)) && tierTiles(a)}
            </div>
          ))}
          <div className="hint mt12">密钥只存在这台电脑上，不会上传</div>
        </>
      )}
      {adding && <AddAccount onClose={() => setAdding(false)} onAdded={(a) => { setData([...(data ?? []), a]); setAdding(false); }} />}
      {tierEdit && (
        <Modal title={`${tierEdit.label} · 各档位用谁`} onClose={() => setTierEdit(null)}>
          {Object.entries(tierEdit.tiers ?? {}).sort(([x], [y]) => ORDER.indexOf(x) - ORDER.indexOf(y)).map(([k, v]) => (
            <div key={k} className="setrow">
              <div className="tt">{isTier(k) ? <><TierSprite tier={k} scale={2} /> {t(`tier.${k}`)}</> : k}</div>
              <select className="sel" value={v} onChange={async (e) => { const tiers = { ...tierEdit.tiers, [k]: e.target.value }; setTierEdit({ ...tierEdit, tiers }); await act(tierEdit.id, () => api.updateAccount(tierEdit.id, { tiers })); }}>
                {tierEdit.kind === 'api' && <option value="">不用</option>}
                {[...(v ? [v] : []), ...tierEdit.models.filter((m) => m !== v)].map((m) => <option key={m} value={m}>{name(tierEdit, m)}</option>)}
              </select>
            </div>
          ))}
        </Modal>
      )}
    </>
  );
}

export function AddAccount({ onClose, onAdded, embedded, onSkip }: { onClose?: () => void; onAdded: (a: Account) => void; embedded?: boolean; onSkip?: () => void }) {
  const [provider, setProvider] = useState('zhipu');
  const [key, setKey] = useState('');
  const [baseUrl, setBaseUrl] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<ApiError | null>(null);
  const p = API_PROVIDERS.find((x) => x.id === provider)!;
  const submit = async () => {
    if (!key.trim()) return;
    setBusy(true); setErr(null);
    try { onAdded(await api.addAccount({ provider, api_key: key.trim(), base_url: baseUrl.trim() || undefined })); }
    catch (e) { setErr(e as ApiError); } finally { setBusy(false); }
  };
  const body = (
    <>
      <div className="onb-grid" style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 8, marginBottom: 12 }}>
        {API_PROVIDERS.slice(0, embedded ? 3 : API_PROVIDERS.length).map((x) => (
          <button key={x.id} className={`acct ${provider === x.id ? 'pick' : ''}`} style={{ margin: 0 }} onClick={() => setProvider(x.id)}><VendorLogo provider={x.id} /><b>{x.label}</b></button>
        ))}
      </div>
      <input className="input mb6" placeholder={`粘贴${p.label} API Key`} value={key} onChange={(e) => setKey(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && submit()} autoFocus />
      {provider === 'openrouter' && <input className="input mb6" placeholder="自定义地址，留空用官方" value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} />}
      {err && <div className="hint err-text mb6">{err.message}</div>}
      <div className="hint">没有账号？<a href={p.url} onClick={(e) => { e.preventDefault(); void desktop.openUrl(p.url); }}>去{p.label}开放平台注册</a>{embedded && <>　也可以跳过，杂活会先用订阅额度</>}</div>
    </>
  );
  if (embedded) {
    return (
      <>
        {body}
        <div className="nav" style={{ display: 'flex', justifyContent: 'flex-end', gap: 6, marginTop: 18 }}>
          {onSkip && <button className="btn" onClick={onSkip}>跳过</button>}
          <button className="btn pri" disabled={busy || !key.trim()} onClick={submit}>{busy ? '正在验证…' : '验证并继续'}</button>
        </div>
      </>
    );
  }
  return (
    <Modal title="添加按量付费账号" onClose={onClose!} width={520}>
      {body}
      <div className="row mt12" style={{ justifyContent: 'flex-end' }}>
        <button className="btn" onClick={onClose}>取消</button>
        <button className="btn pri" disabled={busy || !key.trim()} onClick={submit}>{busy ? '正在验证…' : '验证并添加'}</button>
      </div>
    </Modal>
  );
}
