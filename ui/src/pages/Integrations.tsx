import { useState, type ReactNode } from 'react';
import { api, ApiError } from '../api/client';
import type { Integration, IntegrationId } from '../api/types';
import { HostModelDialog } from '../components/HostModels';
import { Dot, ErrorLine, SetRow, Toggle, toast } from '../components/ui';
import { VendorLogo } from '../components/Vendor';
import { errText, useLoad } from '../hooks';
import { useTheme } from '../theme/ThemeContext';
import { desktop } from '../tauri';

const META: Record<IntegrationId, { name: string; desc: string; manual?: boolean; takeoverOnly?: boolean }> = {
  claude_code: { name: 'Claude Code', desc: 'VS Code 和终端里的 Claude Code 都能调用生图、生视频、问别的模型' },
  claude_desktop: { name: 'Claude Desktop', desc: '桌面版的对话改走 {app} 挑的按量付费模型', takeoverOnly: true },
  codex: { name: 'Codex', desc: '终端里的 Codex 能调用生图、生视频、问别的模型；下次启动生效' },
  gemini_cli: { name: 'Gemini CLI', desc: '终端里的 Gemini CLI 能调用生图、生视频、问别的模型；下次启动生效' },
  doubao_work: { name: '豆包工作', desc: '在豆包工作里新建一个自定义连接器，它就能让 {app} 出图、出视频、问模型', manual: true },
  pi: { name: 'pi', desc: '终端里的 pi 多一个 Switchboard 提供方，型号在模型列表里定' },
  deepseek_harness: { name: 'DeepSeek Harness', desc: '多一个自定义提供方，模型列表里选 Switchboard' },
  openclaw: { name: 'OpenClaw', desc: '多一个模型提供方；改动从下次启动网关生效' },
  hermes: { name: 'Hermes Agent', desc: '辅助任务和生图交给 {app}；改动从下次启动 Hermes 生效' },
  workbuddy: { name: 'WorkBuddy', desc: 'WorkBuddy 的自定义模型里多一组 Switchboard 的型号；重开生效' },
  winwork: { name: 'WinWork', desc: 'WinWork 里的智能体能调用生图、生视频、问别的模型；下次新建会话生效' },
};

/** 「接管模型」在三个宿主里的后果各不相同，句式保持一致 */
const TAKEOVER_DESC: Partial<Record<IntegrationId, string>> = {
  claude_code: '对话改走按量付费，订阅暂停；/model 里可选档位',
  codex: '对话改走按量付费；彻底重启 Codex 生效',
  claude_desktop: '对话改走按量付费，订阅暂停；完全退出后重开生效',
};

/** 带型号表的宿主：菜单里列哪些型号、谁是默认在这里定；Codex 只在接管时有菜单 */
const MODEL_LIST_HOSTS = new Set<IntegrationId>(['codex', 'workbuddy', 'pi', 'deepseek_harness', 'openclaw']);

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
  const [dialog, setDialog] = useState<IntegrationId | null>(null);
  const appShort = t('app.title').replace(/^iML\s*/, '');

  const run = async (id: IntegrationId, fn: () => Promise<Integration>) => {
    setBusy(id); setErr(null);
    try { const it = await fn(); setData((old) => old?.map((x) => (x.id === id ? it : x)) ?? old); if (it.status === 'connected' && id === 'claude_code') onConnected?.(); }
    catch (e) { setErr(e as ApiError); } finally { setBusy(null); }
  };
  const set = (it: Integration, patch: Integration['options']) => run(it.id, () => api.setIntegration(it.id, { ...it.options, ...patch }));

  /** 卡片里的选项行：接入之后才有意义的开关；Claude Desktop 只有接管这一件事，装了就显示 */
  const optionRows = (it: Integration): ReactNode[] => {
    const rows: ReactNode[] = [];
    const connected = it.status === 'connected';
    if (connected && (it.id === 'claude_code' || it.id === 'winwork')) {
      rows.push(
        <SetRow key="md" title="写入调用约定" desc={it.id === 'winwork' ? '加到 WinWork 自己的 CLAUDE.md，它才知道什么时候把活交出来' : '加到你的全局 CLAUDE.md，Claude 才知道什么时候把活交出来'}>
          <Toggle on={!!it.options.write_claude_md} onChange={(v) => set(it, { write_claude_md: v })} />
        </SetRow>,
      );
    }
    if (connected && it.id === 'claude_code') {
      rows.push(
        <SetRow key="plan" title="规划用 Opus，执行用 Sonnet" desc="改的是 Claude Code 自己的设置，额度消耗约减三成">
          <Toggle on={!!it.options.opusplan} onChange={(v) => set(it, { opusplan: v })} />
        </SetRow>,
      );
    }
    if ((connected && (it.id === 'claude_code' || it.id === 'codex')) || (it.id === 'claude_desktop' && it.available !== false)) {
      rows.push(
        <SetRow key="takeover" title="接管模型" desc={TAKEOVER_DESC[it.id]}>
          <Toggle on={!!it.options.takeover} onChange={(v) => set(it, { takeover: v })} />
        </SetRow>,
      );
    }
    if (MODEL_LIST_HOSTS.has(it.id) && (it.id === 'codex' ? !!it.options.takeover : connected)) {
      rows.push(
        <SetRow key="models" title="模型列表">
          <span className="hint">{it.models_count ?? 0} 个</span>
          <button className="btn sm" onClick={() => setDialog(it.id)}>调整</button>
        </SetRow>,
      );
    }
    return rows;
  };

  const action = (it: Integration) => {
    const m = META[it.id];
    if (m.takeoverOnly) return null; // 开关就在下面的选项行里
    if (m.manual) return <button className="btn sm" disabled={it.available === false} onClick={async () => { await desktop.copyText(it.snippet ?? ''); toast(`已复制，去 ${m.name} 里粘贴`); }}>复制配置</button>;
    if (it.status === 'connected') return <button className="btn sm" disabled={busy === it.id} onClick={() => run(it.id, () => api.disconnect(it.id))}>断开</button>;
    return <button className="btn pri sm" disabled={busy === it.id || it.available === false} onClick={() => run(it.id, () => api.connect(it.id, it.id === 'claude_code' || it.id === 'winwork' ? { write_claude_md: true, commands: true } : undefined))}>{busy === it.id ? '正在接入…' : '接入'}</button>;
  };

  return (
    <>
      <div className="ct"><h4>{t('nav.integrations')}</h4></div>
      {(error || err) && <ErrorLine {...errText(error ?? err)!} />}
      {data?.map((it) => {
        const m = META[it.id];
        const rows = optionRows(it);
        return (
          <section key={it.id} className="host">
            <div className="host-head">
              <VendorLogo provider={it.id} />
              <div className="grow"><b>{m.name}</b><div className="hint">{withApp(m.desc, appShort)}</div></div>
              {it.available === false
                ? <span className="muted" title={it.detail}>未安装</span>
                : <span><Dot s={it.status === 'connected' ? 'ok' : 'no'} />{it.status === 'connected' ? '已接入' : it.status === 'manual' ? '手动' : '未接入'}{activity(it)}</span>}
              {action(it)}
            </div>
            {rows.length > 0 && <div className="host-opts">{rows}</div>}
            {it.snippet && it.id !== 'claude_code' && it.id !== 'winwork' && (
              <details className="raw cfg host-more">
                <summary>{m.manual || it.status === 'manual' ? '怎么填' : '写进去的内容'}</summary>
                <pre>{it.snippet}</pre>
              </details>
            )}
          </section>
        );
      })}
      {dialog && (
        <HostModelDialog host={dialog} hostName={META[dialog].name} onClose={() => setDialog(null)}
          onSaved={(n) => setData((old) => old?.map((x) => (x.id === dialog ? { ...x, models_count: n } : x)) ?? old)} />
      )}
    </>
  );
}
