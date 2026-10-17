import { useState } from 'react';
import { api, ApiError } from '../api/client';
import type { Integration, IntegrationId } from '../api/types';
import { Dot, ErrorLine, SetRow, Toggle, toast } from '../components/ui';
import { VendorLogo } from '../components/Vendor';
import { errText, useLoad } from '../hooks';
import { useTheme } from '../theme/ThemeContext';
import { desktop } from '../tauri';

const META: Record<IntegrationId, { name: string; desc: string }> = {
  claude_code: { name: 'Claude Code', desc: 'VS Code 和终端里的 Claude Code 都能调用生图、生视频、问别的模型' },
  hermes: { name: 'Hermes Agent', desc: '辅助任务和生图交给{app}；改动从下次启动 Hermes 生效' },
  workbuddy: { name: 'WorkBuddy', desc: '需要在 WorkBuddy 设置里手动添加两个自定义模型' },
};

export function Integrations({ onConnected }: { onConnected?: () => void }) {
  const { t } = useTheme();
  const { data, setData, error } = useLoad(() => api.integrations());
  const [err, setErr] = useState<ApiError | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const appShort = t('app.title').replace(/^iML\s*/, '');

  const run = async (id: IntegrationId, fn: () => Promise<Integration>) => {
    setBusy(id); setErr(null);
    try { const it = await fn(); setData((old) => old?.map((x) => (x.id === id ? it : x)) ?? old); if (it.status === 'connected' && id === 'claude_code') onConnected?.(); }
    catch (e) { setErr(e as ApiError); } finally { setBusy(null); }
  };

  return (
    <>
      <div className="ct"><h4>{t('nav.integrations')}</h4></div>
      {(error || err) && <ErrorLine {...errText(error ?? err)!} />}
      {data?.map((it) => (
        <div key={it.id}>
          <div className="acct" style={it.id === 'claude_code' && it.status === 'connected' ? { marginBottom: 0, borderBottomLeftRadius: 0, borderBottomRightRadius: 0 } : undefined}>
            <VendorLogo provider={it.id} />
            <div className="grow"><b>{META[it.id].name}</b><div className="hint">{META[it.id].desc.replace('{app}', appShort)}</div></div>
            <span><Dot s={it.status === 'connected' ? 'ok' : 'no'} />{it.status === 'connected' ? '已接入' : '未接入'}</span>
            {it.id === 'workbuddy'
              ? <button className="btn sm" onClick={async () => { await desktop.copyText(it.snippet ?? ''); toast('已复制，去 WorkBuddy 设置里粘贴'); }}>复制配置</button>
              : it.status === 'connected'
                ? <button className="btn sm" disabled={busy === it.id} onClick={() => run(it.id, () => api.disconnect(it.id))}>断开</button>
                : <button className="btn pri sm" disabled={busy === it.id} onClick={() => run(it.id, () => api.connect(it.id, it.id === 'claude_code' ? { write_claude_md: true } : undefined))}>{busy === it.id ? '正在接入…' : '接入'}</button>}
          </div>
          {it.id === 'claude_code' && it.status === 'connected' && (
            <div className="panel" style={{ margin: '-1px 0 10px', borderTopLeftRadius: 0, borderTopRightRadius: 0, fontSize: 12.5, padding: '4px 14px' }}>
              <SetRow title="写入调用约定" desc="加到你的全局 CLAUDE.md，Claude 才知道什么时候把活交出来">
                <Toggle on={!!it.options.write_claude_md} onChange={(v) => run(it.id, () => api.setIntegration(it.id, { ...it.options, write_claude_md: v }))} />
              </SetRow>
              <SetRow title="规划用 Opus，执行用 Sonnet" desc="改的是 Claude Code 自己的设置，额度消耗约减三成" last>
                <Toggle on={!!it.options.opusplan} onChange={(v) => run(it.id, () => api.setIntegration(it.id, { ...it.options, opusplan: v }))} />
              </SetRow>
            </div>
          )}
          {it.id === 'workbuddy' && it.snippet && <pre style={{ margin: '-2px 0 10px' }}>{it.snippet}</pre>}
        </div>
      ))}
    </>
  );
}
