import { useEffect, useState, type ReactNode } from 'react';
import { api, ApiError } from '../api/client';
import type { Integration, IntegrationId } from '../api/types';
import { HostModelDialog } from '../components/HostModels';
import { HostTiersDialog } from '../components/HostTiers';
import { Dot, ErrorLine, SetRow, Toggle, toast } from '../components/ui';
import { Vendor, VendorLogo } from '../components/Vendor';
import { errText, useLoad } from '../hooks';
import { useTheme } from '../theme/ThemeContext';
import { desktop } from '../tauri';

/** 每个宿主只说一句自己特有的：在哪生效、什么时候生效 */
const META: Record<IntegrationId, { name: string; note: string; manual?: boolean; takeoverOnly?: boolean }> = {
  claude_code: { name: 'Claude Code', note: 'VS Code 和终端里都生效' },
  claude_desktop: { name: 'Claude Desktop', note: '只有「接管模型」这一个开关', takeoverOnly: true },
  codex: { name: 'Codex', note: '终端和 ChatGPT 桌面版都生效；下次启动生效' },
  gemini_cli: { name: 'Gemini CLI', note: '下次启动生效' },
  doubao_work: { name: '豆包工作', note: '新建一个自定义连接器，把下面的内容填进去', manual: true },
  pi: { name: 'pi', note: '型号在模型列表里定；下次启动生效' },
  deepseek_harness: { name: 'DeepSeek Harness', note: '在它的模型列表里选 Switchboard' },
  openclaw: { name: 'OpenClaw', note: '下次启动网关生效' },
  hermes: { name: 'Hermes Agent', note: '下次启动 Hermes 生效' },
  workbuddy: { name: 'WorkBuddy', note: '型号出现在它的自定义模型里；重开生效' },
  winwork: { name: 'WinWork', note: '下次新建会话生效' },
};

/** 「接管模型」在三个宿主里的后果各不相同 */
const TAKEOVER_DESC: Partial<Record<IntegrationId, string>> = {
  claude_code: '对话改走按量付费，订阅暂停；/model 里可选档位',
  codex: '对话改走按量付费；彻底重启 Codex 生效',
  claude_desktop: '对话改走按量付费，订阅暂停；完全退出后重开生效',
};

/** 订阅只能由官方命令行消耗，跑不了宿主自己的工具：派活和纯对话用得上，带工具的回合和接管走按量付费 */
const SUB_USE: Record<IntegrationId, string> = {
  claude_code: '订阅 · 派活', claude_desktop: '不走订阅', codex: '订阅 · 派活', gemini_cli: '订阅 · 派活',
  doubao_work: '订阅 · 派活', winwork: '订阅 · 派活', hermes: '订阅 · 纯对话',
  workbuddy: '订阅 · 纯对话', pi: '订阅 · 纯对话', deepseek_harness: '订阅 · 纯对话', openclaw: '订阅 · 纯对话',
};
const SUB_RULE = '订阅只接不带工具的请求：派活工具和纯对话能用，带工具的智能体回合和接管走按量付费';

/** 带型号表的宿主；Codex 只在接管时有菜单 */
const MODEL_LIST_HOSTS = new Set<IntegrationId>(['codex', 'workbuddy', 'pi', 'deepseek_harness', 'openclaw']);

const inUse = (it: Integration) => it.status === 'connected' || !!it.options?.takeover || (it.status === 'manual' && (it.calls_7d ?? 0) > 0);
const installed = (it: Integration) => it.available !== false;
const STATUS: Record<string, string> = { connected: '已接入', manual: '手动', disconnected: '未接入' };

export function Integrations({ onConnected }: { onConnected?: () => void }) {
  const { t } = useTheme();
  const { data, setData, error } = useLoad(() => api.integrations());
  const [err, setErr] = useState<ApiError | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [dialog, setDialog] = useState<IntegrationId | null>(null);
  const [tiersDialog, setTiersDialog] = useState<IntegrationId | null>(null);
  const [selId, setSelId] = useState<IntegrationId | null>(null);

  const list = data ?? [];
  const groups: [string, Integration[]][] = [
    ['已接入', list.filter((it) => installed(it) && inUse(it))],
    ['可接入', list.filter((it) => installed(it) && !inUse(it))],
    ['未安装', list.filter((it) => !installed(it))],
  ];
  // 默认选第一个已接入的；列表变了（断开、卸载）选中的还在就不动
  useEffect(() => {
    if (!data) return;
    if (selId && data.some((it) => it.id === selId)) return;
    setSelId(groups[0][1][0]?.id ?? groups[1][1][0]?.id ?? data[0]?.id ?? null);
  }, [data]); // eslint-disable-line react-hooks/exhaustive-deps
  const sel = list.find((it) => it.id === selId) ?? null;

  const run = async (id: IntegrationId, fn: () => Promise<Integration>) => {
    setBusy(id); setErr(null);
    try { const it = await fn(); setData((old) => old?.map((x) => (x.id === id ? it : x)) ?? old); if (it.status === 'connected' && id === 'claude_code') onConnected?.(); }
    catch (e) { setErr(e as ApiError); } finally { setBusy(null); }
  };
  const set = (it: Integration, patch: Integration['options']) => run(it.id, () => api.setIntegration(it.id, { ...it.options, ...patch }));

  /** 宿主自己的开关：接入之后才有意义 */
  const ownRows = (it: Integration): ReactNode[] => {
    const rows: ReactNode[] = [];
    const connected = it.status === 'connected';
    if (connected && (it.id === 'claude_code' || it.id === 'winwork')) {
      rows.push(
        <SetRow key="md" title="写入调用约定" desc={it.id === 'winwork' ? '写进 WinWork 的 CLAUDE.md；不写它不知道什么时候把活交出来' : '写进全局 CLAUDE.md；不写它不知道什么时候把活交出来'}>
          <Toggle on={!!it.options?.write_claude_md} onChange={(v) => set(it, { write_claude_md: v })} />
        </SetRow>,
      );
    }
    if (connected && it.id === 'claude_code') {
      rows.push(
        <SetRow key="plan" title="规划用 Opus，执行用 Sonnet" desc="额度约省三成">
          <Toggle on={!!it.options?.opusplan} onChange={(v) => set(it, { opusplan: v })} />
        </SetRow>,
      );
    }
    return rows;
  };

  /** 接管与档位：改的是请求往哪走 */
  const routeRows = (it: Integration): ReactNode[] => {
    const rows: ReactNode[] = [];
    const connected = it.status === 'connected';
    if ((connected && (it.id === 'claude_code' || it.id === 'codex')) || (it.id === 'claude_desktop' && installed(it))) {
      rows.push(
        <SetRow key="takeover" title="接管模型" desc={TAKEOVER_DESC[it.id]}>
          <Toggle on={!!it.options?.takeover} onChange={(v) => set(it, { takeover: v })} />
        </SetRow>,
      );
    }
    if (connected || !!it.options?.takeover) {
      rows.push(
        <SetRow key="tiers" title="档位候选">
          <span className="hint">{it.tiers_custom ? `${it.tiers_custom} 档自己排` : '跟全局'}</span>
          <button className="btn sm" onClick={() => setTiersDialog(it.id)}>调整</button>
        </SetRow>,
      );
    }
    if (MODEL_LIST_HOSTS.has(it.id) && (it.id === 'codex' ? !!it.options?.takeover : connected)) {
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
    if (!installed(it) || m.takeoverOnly) return null;
    if (m.manual) return <button className="btn sm" onClick={async () => { await desktop.copyText(it.snippet ?? ''); toast(`已复制，去 ${m.name} 里粘贴`); }}>复制配置</button>;
    if (it.status === 'connected') return <button className="btn sm" disabled={busy === it.id} onClick={() => run(it.id, () => api.disconnect(it.id))}>断开</button>;
    return <button className="btn pri sm" disabled={busy === it.id} onClick={() => run(it.id, () => api.connect(it.id, it.id === 'claude_code' || it.id === 'winwork' ? { write_claude_md: true, commands: true } : undefined))}>{busy === it.id ? '正在接入…' : '接入'}</button>;
  };

  const activity = (it: Integration) => {
    if (it.calls_7d === undefined) return null;
    if (it.calls_7d > 0) return <span className="hint" title={it.last_call ? `最近一次：${new Date(it.last_call).toLocaleString()}` : undefined}>近 7 天 {it.calls_7d} 次</span>;
    if (it.status === 'connected') return <span className="hint warn-text">7 天没派活</span>;
    return null;
  };

  return (
    <>
      <div className="ct" data-tauri-drag-region><h4 data-tauri-drag-region>{t('nav.integrations')}</h4></div>
      {(error || err) && <ErrorLine {...errText(error ?? err)!} />}
      {data && (
        <div className="hosts">
          <div className="hlist">
            {groups.map(([title, items]) => items.length > 0 && (
              <div key={title}>
                <div className="hint hgrp">{title}</div>
                {items.map((it) => (
                  <button key={it.id} className={`hitem ${it.id === selId ? 'on' : ''} ${installed(it) ? '' : 'muted'}`} onClick={() => setSelId(it.id)}>
                    <Vendor provider={it.id} /><span className="grow">{META[it.id].name}</span>
                    {installed(it) && <Dot s={inUse(it) ? 'ok' : 'no'} />}
                  </button>
                ))}
              </div>
            ))}
          </div>
          {sel && (() => {
            const m = META[sel.id];
            const own = ownRows(sel);
            const route = routeRows(sel);
            return (
              <div className="hdetail">
                <div className="hhead">
                  <VendorLogo provider={sel.id} />
                  <div className="grow">
                    <b>{m.name}</b>
                    <div className="row hint" style={{ gap: 8 }}>
                      {installed(sel) ? <span>{STATUS[sel.status] ?? sel.status}</span> : <span>未安装</span>}
                      {installed(sel) && <span className="chip" title={SUB_RULE}>{SUB_USE[sel.id]}</span>}
                      {activity(sel)}
                    </div>
                  </div>
                  {action(sel)}
                </div>
                <div className="hint mb12">{installed(sel) ? m.note : sel.detail}</div>
                {own.length > 0 && <div className="panel mb12">{own}</div>}
                {route.length > 0 && <div className="panel mb12">{route}</div>}
                {sel.snippet && sel.id !== 'claude_code' && sel.id !== 'winwork' && (
                  <details className="raw cfg host-more">
                    <summary>{m.manual ? '要填的内容' : '写进去的内容'}</summary>
                    <pre>{sel.snippet}</pre>
                  </details>
                )}
              </div>
            );
          })()}
        </div>
      )}
      {tiersDialog && (
        <HostTiersDialog host={tiersDialog} hostName={META[tiersDialog].name} onClose={() => setTiersDialog(null)}
          onSaved={(n) => setData((old) => old?.map((x) => (x.id === tiersDialog ? { ...x, tiers_custom: n } : x)) ?? old)} />
      )}
      {dialog && (
        <HostModelDialog host={dialog} hostName={META[dialog].name} onClose={() => setDialog(null)}
          onSaved={(n) => setData((old) => old?.map((x) => (x.id === dialog ? { ...x, models_count: n } : x)) ?? old)} />
      )}
    </>
  );
}
