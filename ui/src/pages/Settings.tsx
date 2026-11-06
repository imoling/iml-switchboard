import { hotkeyCaps, isMac, normalizeHotkey } from '../hotkey';
import { STALE_BACKEND, TRAY_NAME } from '../copy';
import { restartBackend, useStaleBackend } from '../backend';
import { useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { Mode, Settings as S, ThemeId, TrayIcon, UpdateInfo } from '../api/types';
import trayTile from '../assets/tray/tile.png';
import trayFrame from '../assets/tray/frame.png';
import trayFork from '../assets/tray/fork.png';
import trayPatchbay from '../assets/tray/patchbay.png';
import trayLever from '../assets/tray/lever.png';
import trayBreakout from '../assets/tray/breakout.png';
import trayPixel from '../assets/tray/pixel.png';
import trayLadder from '../assets/tray/ladder.png';
import { ErrorLine, RevealButton, Seg, SetRow, Toggle, toast } from '../components/ui';
import { fromDisplay, moneySymbol, setMoney, toDisplay, uptime } from '../format';
import { errText, useLoad } from '../hooks';
import { useTheme } from '../theme/ThemeContext';
import { desktop, inTauri } from '../tauri';

/** 词表还没从后台拿到时的选项，id 与 internal/themes/*.json 一致 */
const FALLBACK_THEMES: { id: ThemeId; name: string }[] = [{ id: 'standard', name: '标准' }, { id: 'dianjiang', name: '点将台' }, { id: 'academy', name: '魔法学院' }, { id: 'harem', name: '后宫' }, { id: 'seaking', name: '海王' }];

/** 菜单栏图标的八种样式；图是 2x 正常态，和菜单栏里一样按 18 pt 显示 */
const TRAY_ICONS: { id: TrayIcon; label: string; src: string }[] = [
  { id: 'tile', label: '底板', src: trayTile }, { id: 'frame', label: '线框', src: trayFrame }, { id: 'fork', label: '无框', src: trayFork },
  { id: 'patchbay', label: '插线板', src: trayPatchbay }, { id: 'lever', label: '拨杆', src: trayLever }, { id: 'breakout', label: '破框', src: trayBreakout },
  { id: 'pixel', label: '像素主帅', src: trayPixel }, { id: 'ladder', label: '三档', src: trayLadder },
];

function TrayIconPicker({ value, onChange }: { value: TrayIcon; onChange: (v: TrayIcon) => void }) {
  return (
    <div className="ipick">
      {TRAY_ICONS.map((o) => (
        <button key={o.id} className={value === o.id ? 'on' : ''} title={o.label} onClick={() => onChange(o.id)}><img src={o.src} alt={o.label} /></button>
      ))}
    </div>
  );
}

export const MODE_OPTIONS: { id: Mode; label: string }[] = [{ id: 'save', label: '省钱' }, { id: 'balanced', label: '均衡' }, { id: 'quality', label: '质量' }];

export function Settings() {
  const { t, themes, setThemeId } = useTheme();
  const { data, setData, error } = useLoad(() => api.settings());
  const [err, setErr] = useState<ApiError | null>(null);
  const [recording, setRecording] = useState(false);
  // 月上限输入框里正在改的字，按显示币种；失焦时换回美元再存
  const [limitDraft, setLimitDraft] = useState<string | null>(null);

  const save = async (patch: Partial<S>) => {
    if (!data) return;
    setData({ ...data, ...patch });
    try { const s = await api.saveSettings(patch); setData(s); setErr(null); if (patch.theme) setThemeId(patch.theme); if (patch.hotkey) await desktop.setHotkey(patch.hotkey); }
    catch (e) { setErr(e as ApiError); }
  };

  // 录快捷键：按下组合键即记录
  useEffect(() => {
    if (!recording) return;
    const k = (e: KeyboardEvent) => {
      e.preventDefault();
      if (['Shift', 'Alt', 'Meta', 'Control'].includes(e.key)) return;
      if (e.key === 'Escape') { setRecording(false); return; }
      const mods = [e.ctrlKey && 'Control', e.altKey && 'Alt', e.shiftKey && 'Shift', e.metaKey && 'Super'].filter(Boolean) as string[];
      const key = e.code === 'Space' ? 'Space' : e.key.length === 1 ? e.key.toUpperCase() : e.key;
      if (mods.length === 0) return;
      void save({ hotkey: normalizeHotkey([...mods, key].join('+')) }); setRecording(false);
    };
    window.addEventListener('keydown', k);
    return () => window.removeEventListener('keydown', k);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [recording, data]);

  const themeOptions = (themes.length ? themes : FALLBACK_THEMES).map((x) => ({ id: x.id, label: x.name }));
  const hotkeyParts = hotkeyCaps(data?.hotkey ?? 'Alt+Space');

  return (
    <>
      <div className="ct" data-tauri-drag-region><h4 data-tauri-drag-region>{t('nav.settings')}</h4></div>
      {(error || err) && <ErrorLine {...errText(error ?? err)!} />}
      {data && (
        <>
          <div className="hint">使用</div>
          <SetRow title="风格主题" desc="只换界面里的说法；费用、报错和给 Claude 的结果不变">
            <Seg value={data.theme} onChange={(v) => save({ theme: v })} options={themeOptions} />
          </SetRow>
          <SetRow title="快速面板快捷键" desc={recording ? '按下新的组合键，Esc 取消' : undefined}>
            <button className={`hotkey ${recording ? 'rec' : ''}`} title="点一下换一个" onClick={() => setRecording(true)}>{recording ? '正在录…' : hotkeyParts.map((p, i) => <kbd key={i}>{p}</kbd>)}</button>
          </SetRow>
          <SetRow title="开机启动" desc="关闭后，应用调用时才启动，首次约慢 2 秒">
            <Toggle on={data.launch_at_login} onChange={(v) => save({ launch_at_login: v })} />
          </SetRow>
          <SetRow title="通知" desc="额度、预算、媒体完成或失败、规则建议、新版本">
            {(['quota', 'budget', 'media', 'suggestions'] as const).map((k) => (
              <label key={k} className="row" style={{ gap: 4, marginLeft: 8 }}><input type="checkbox" checked={data.notifications[k]} onChange={(e) => save({ notifications: { ...data.notifications, [k]: e.target.checked } })} />{{ quota: '额度', budget: '预算', media: '媒体', suggestions: '建议' }[k]}</label>
            ))}
          </SetRow>
          <div className="hint" style={{ marginTop: 14 }}>调度与花费</div>
          <SetRow title="默认调度模式" desc={`${TRAY_NAME}里可以临时切换；质量模式下困难任务用 Fable`}>
            <Seg value={data.default_mode} onChange={(v) => save({ default_mode: v })} options={MODE_OPTIONS} />
          </SetRow>
          <SetRow title="拿不准时先问低档" desc="关掉后只看关键词，没关键词的难题容易判低">
            <Toggle on={!data.rules_only} onChange={(v) => save({ rules_only: !v })} />
          </SetRow>
          <SetRow title="费用显示" desc={`按 1 美元 ≈ ${(1 / (data.usd_per_cny || 1 / 7.1)).toFixed(1)} 元折算；人民币账号显示原价`}>
            <Seg value={data.currency === 'CNY' ? 'CNY' : 'USD'} onChange={(v) => { setMoney(v, data.usd_per_cny); setLimitDraft(null); void save({ currency: v }); }} options={[{ id: 'CNY', label: '人民币' }, { id: 'USD', label: '美元' }]} />
          </SetRow>
          <SetRow title="按量付费月上限" desc="到达后只用订阅，媒体生成暂停">
            <span className="row">{moneySymbol()}<input className="input num" type="number" min={0} value={limitDraft ?? Math.round(toDisplay(data.monthly_api_limit_usd) * 100) / 100} onChange={(e) => setLimitDraft(e.target.value)} onBlur={() => { if (limitDraft !== null) { const v = fromDisplay(Number(limitDraft) || 0); setLimitDraft(null); setData({ ...data, monthly_api_limit_usd: v }); void save({ monthly_api_limit_usd: v }); } }} /></span>
          </SetRow>
          <SetRow title="订阅额度让出线" desc={`超过后${t('tier.low')}和${t('tier.mid')}任务改走按量付费`}>
            <span className="row"><input className="input num" type="number" min={0} max={100} value={data.subscription_yield_pct} onChange={(e) => setData({ ...data, subscription_yield_pct: Number(e.target.value) })} onBlur={() => save({ subscription_yield_pct: data.subscription_yield_pct })} />%</span>
          </SetRow>
          <SetRow title="网络代理" desc="只用于需要代理的服务，比如 OpenRouter">
            <input className="input sm" style={{ width: 160 }} placeholder="127.0.0.1:7890" value={data.proxy} onChange={(e) => setData({ ...data, proxy: e.target.value })} onBlur={() => save({ proxy: data.proxy })} />
          </SetRow>
          <div className="hint" style={{ marginTop: 14 }}>数据</div>
          <SetRow title="保存请求内容" desc="关闭后记录里只留费用和模型">
            <Toggle on={data.save_prompts} onChange={(v) => save({ save_prompts: v })} />
          </SetRow>
          <SetRow title="数据位置" desc={<><span className="path">{(data.data_dir ?? '').replace(/^\/Users\/[^/]+/, '~')}</span>记录、规则、账号都在这里，换电脑时带走它；图片视频存在各自项目里</>}>
            <RevealButton path={data.data_dir} />
          </SetRow>
          <BackendRow />
          <VersionRow />
          {inTauri && (
            <>
              <div className="hint" style={{ marginTop: 14 }}>实验功能</div>
              <SetRow title="状态栏图标" desc="菜单栏里用哪一个；告警、出错两张带色点，深色菜单栏下不反白" last={!isMac()}>
                <TrayIconPicker value={data.tray_icon ?? 'tile'} onChange={(v) => { void save({ tray_icon: v }); void desktop.setTrayStyle(v); }} />
              </SetRow>
              {isMac() && (
                <SetRow title="刘海动画" desc="请求经过时刘海两侧亮起；只在带刘海的屏幕显示" last>
                  <Toggle on={!!data.lab_notch} onChange={(v) => { void save({ lab_notch: v }); void desktop.setNotch(v); }} />
                </SetRow>
              )}
            </>
          )}
        </>
      )}
    </>
  );
}

/** 后台服务：运行多久、是不是这个版本、重启 */
function BackendRow() {
  const { data: status, setData: setStatus } = useLoad(() => api.status());
  const { reload: reloadThemes } = useTheme();
  const stale = useStaleBackend(status);
  const [confirming, setConfirming] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [failed, setFailed] = useState<string | null>(null);

  const restart = async () => {
    const busy = (await api.status().catch(() => null))?.busy ?? 0;
    if (busy > 0 && !confirming) { setConfirming(true); return; }
    setConfirming(false); setRestarting(true); setFailed(null);
    const why = await restartBackend();
    setRestarting(false);
    if (why) { setFailed(why); return; }
    setStatus(await api.status().catch(() => null));
    void reloadThemes();
    toast('后台已重启');
  };

  const desc = failed ? `重启失败：${failed}`
    : confirming ? `正在进行的 ${status?.busy ?? 0} 件事会中断`
    : stale ? STALE_BACKEND
    : status ? `运行中 · 已运行 ${uptime(status.uptime_s)}` : undefined;
  return (
    <SetRow title="后台服务" desc={<span className={failed || stale ? 'warn-text' : undefined}>{desc}</span>}>
      {confirming && <button className="btn sm" onClick={() => setConfirming(false)}>取消</button>}
      {inTauri && <button className={`btn sm ${stale || confirming ? 'pri' : ''}`} disabled={restarting} onClick={restart}>{restarting ? '正在重启…' : confirming ? '仍要重启' : '重启'}</button>}
    </SetRow>
  );
}

/** 上次检查的时间：今天只写几点几分，更早的带上日期；点了「检查更新」时间会变，能看出点过了 */
function checkedAt(iso: string): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '';
  const hm = d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false });
  return d.toDateString() === new Date().toDateString() ? `${hm} ` : `${d.getMonth() + 1} 月 ${d.getDate()} 日 ${hm} `;
}

/** 版本：是不是最新，有新版本就给下载 */
function VersionRow() {
  const [version, setVersion] = useState('');
  const [info, setInfo] = useState<UpdateInfo | null>(null);
  const [checking, setChecking] = useState(false);
  const [failed, setFailed] = useState<string | null>(null);
  useEffect(() => { void desktop.version().then(setVersion); void api.update().then(setInfo).catch(() => null); }, []);
  const check = async () => {
    setChecking(true); setFailed(null);
    try { setInfo(await api.checkUpdate()); } catch (e) { setFailed((e as Error).message); }
    setChecking(false);
  };
  const desc = failed ? `检查失败：${failed}`
    : info?.has_update ? `有新版本 ${info.latest}，发布于 ${new Date(info.published_at).toLocaleDateString()}`
    : info ? `已是最新 · ${checkedAt(info.checked_at)}检查过` : undefined;
  return (
    <SetRow title={`版本 ${version}`} desc={<span className={failed ? 'warn-text' : undefined}>{desc}</span>} last>
      {info?.has_update && <button className="btn sm pri" onClick={() => desktop.openUrl(info.url)}>下载</button>}
      <button className="btn sm" disabled={checking} onClick={check}>{checking ? '正在检查…' : '检查更新'}</button>
    </SetRow>
  );
}
