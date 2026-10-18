import { useState } from 'react';
import { api, ApiError } from '../api/client';
import type { Integration, IntegrationId } from '../api/types';
import { Dot, ErrorLine, SetRow, Toggle, toast } from '../components/ui';
import { VendorLogo } from '../components/Vendor';
import { errText, useLoad } from '../hooks';
import { useTheme } from '../theme/ThemeContext';
import { desktop } from '../tauri';

const META: Record<IntegrationId, { name: string; desc: string; manual?: boolean }> = {
  claude_code: { name: 'Claude Code', desc: 'VS Code 和终端里的 Claude Code 都能调用生图、生视频、问别的模型' },
  codex: { name: 'Codex CLI', desc: '终端里的 Codex 能调用生图、生视频、问别的模型；下次启动生效' },
  gemini_cli: { name: 'Gemini CLI', desc: '终端里的 Gemini CLI 能调用生图、生视频、问别的模型；下次启动生效' },
  doubao_work: { name: '豆包工作', desc: '在豆包工作里新建一个自定义连接器，它就能让 {app} 出图、出视频、问模型', manual: true },
  pi: { name: 'pi', desc: '终端里的 pi 多两个模型可选，自动分档或订阅优先' },
  deepseek_harness: { name: 'DeepSeek Harness', desc: '多一个自定义提供方，模型列表里选 Switchboard' },
  openclaw: { name: 'OpenClaw', desc: '多一个模型提供方；改动从下次启动网关生效' },
  hermes: { name: 'Hermes Agent', desc: '辅助任务和生图交给 {app}；改动从下次启动 Hermes 生效' },
  workbuddy: { name: 'WorkBuddy', desc: '需要在 WorkBuddy 设置里手动添加两个自定义模型', manual: true },
};

/** 说明里的 {app} 换成应用名：英文名两边留空格，中文名（点将台等主题）不留 */
function withApp(desc: string, app: string) {
  return desc.replace(/ ?\{app\} ?/, (m) => (/^[\x00-\x7F]+$/.test(app) ? m.replace('{app}', app) : app));
}

/** 「· 7 天 12 次」；接入了却一次没派过活时标黄，多半是宿主没按约定调用 */
function activity(it: Integration) {
  if (it.calls_7d === undefined) return null;
  if (it.calls_7d > 0) return <span className="hint" title={it.last_call ? `最近一次：${new Date(it.last_call).toLocaleString()}` : undefined}> · 7 天 {it.calls_7d} 次</span>;
  if (it.status === 'connected') return <span className="hint warn-text"> · 7 天没派活</span>;
  return null;
}

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
            <div className="grow"><b>{META[it.id].name}</b><div className="hint">{withApp(META[it.id].desc, appShort)}</div></div>
            {it.available === false
              ? <span className="muted" title={it.detail}>未安装</span>
              : <span><Dot s={it.status === 'connected' ? 'ok' : 'no'} />{it.status === 'connected' ? '已接入' : it.status === 'manual' ? '手动' : '未接入'}{activity(it)}</span>}
            {META[it.id].manual
              ? <button className="btn sm" disabled={it.available === false} onClick={async () => { await desktop.copyText(it.snippet ?? ''); toast(`已复制，去 ${META[it.id].name} 里粘贴`); }}>复制配置</button>
              : it.status === 'connected'
                ? <button className="btn sm" disabled={busy === it.id} onClick={() => run(it.id, () => api.disconnect(it.id))}>断开</button>
                : <button className="btn pri sm" disabled={busy === it.id || it.available === false} onClick={() => run(it.id, () => api.connect(it.id, it.id === 'claude_code' ? { write_claude_md: true } : undefined))}>{busy === it.id ? '正在接入…' : '接入'}</button>}
            {it.snippet && it.id !== 'claude_code' && (
              <details className="raw cfg">
                <summary>{META[it.id].manual ? '怎么填' : '写进去的内容'}</summary>
                <pre>{it.snippet}</pre>
              </details>
            )}
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
        </div>
      ))}
    </>
  );
}
