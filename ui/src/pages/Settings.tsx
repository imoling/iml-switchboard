import { hotkeyCaps, normalizeHotkey } from '../hotkey';
import { useEffect, useRef, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { Mode, Settings as S, ThemeId } from '../api/types';
import { ErrorLine, RevealButton, Seg, SetRow, Toggle, toast } from '../components/ui';
import { errText, useLoad } from '../hooks';
import { useTheme } from '../theme/ThemeContext';
import { desktop } from '../tauri';

export const MODE_OPTIONS: { id: Mode; label: string }[] = [{ id: 'save', label: '省钱' }, { id: 'balanced', label: '均衡' }, { id: 'quality', label: '质量' }];

export function Settings() {
  const { t, themes, setThemeId } = useTheme();
  const { data, setData, error } = useLoad(() => api.settings());
  const [err, setErr] = useState<ApiError | null>(null);
  const [recording, setRecording] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

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

  const exportRules = async () => {
    try {
      const yaml = await api.exportRules();
      const path = await desktop.saveDialog('switchboard-rules.yaml');
      if (path === null && !('__TAURI_INTERNALS__' in window)) { const a = document.createElement('a'); a.href = URL.createObjectURL(new Blob([yaml], { type: 'text/yaml' })); a.download = 'switchboard-rules.yaml'; a.click(); return; }
      if (path) { await desktop.copyText(yaml); toast('规则已复制到剪贴板，粘贴到文件里保存'); }
    } catch (e) { setErr(e as ApiError); }
  };
  const importRules = async (file: File) => {
    try { await api.importRules(await file.text()); toast('规则已导入'); } catch (e) { setErr(e as ApiError); }
  };

  const themeOptions = (themes.length ? themes : [{ id: 'standard' as ThemeId, name: '标准' }, { id: 'dianjiang' as ThemeId, name: '点将台' }]).map((x) => ({ id: x.id, label: x.name }));
  const hotkeyParts = hotkeyCaps(data?.hotkey ?? 'Alt+Space');

  return (
    <>
      <div className="ct"><h4>{t('nav.settings')}</h4></div>
      {(error || err) && <ErrorLine {...errText(error ?? err)!} />}
      {data && (
        <>
          <div className="hint">花费与额度</div>
          <SetRow title="按量付费月上限" desc="到达后只用订阅，媒体生成暂停">
            <span className="row">$<input className="input num" type="number" min={0} value={data.monthly_api_limit_usd} onChange={(e) => setData({ ...data, monthly_api_limit_usd: Number(e.target.value) })} onBlur={() => save({ monthly_api_limit_usd: data.monthly_api_limit_usd })} /></span>
          </SetRow>
          <SetRow title="订阅额度让出线" desc={`超过后${t('tier.low')}和${t('tier.mid')}任务改走按量付费`}>
            <span className="row"><input className="input num" type="number" min={0} max={100} value={data.subscription_yield_pct} onChange={(e) => setData({ ...data, subscription_yield_pct: Number(e.target.value) })} onBlur={() => save({ subscription_yield_pct: data.subscription_yield_pct })} />%</span>
          </SetRow>
          <SetRow title="默认调度模式" desc={`菜单栏里可以临时切换；质量模式下困难任务用 Fable`}>
            <Seg value={data.default_mode} onChange={(v) => save({ default_mode: v })} options={MODE_OPTIONS} />
          </SetRow>
          <div className="hint" style={{ marginTop: 14 }}>使用</div>
          <SetRow title="风格主题" desc="只换界面里的说法；费用、报错和给 Claude 的结果不变">
            <Seg value={data.theme} onChange={(v) => save({ theme: v })} options={themeOptions} />
          </SetRow>
          <SetRow title="快速面板快捷键" desc={recording ? '按下新的组合键，Esc 取消' : undefined}>
            <button className={`hotkey ${recording ? 'rec' : ''}`} title="点一下换一个" onClick={() => setRecording(true)}>{recording ? '正在录…' : hotkeyParts.map((p, i) => <kbd key={i}>{p}</kbd>)}</button>
          </SetRow>
          <SetRow title="网络代理" desc="只用于需要代理的服务，比如 OpenRouter">
            <input className="input sm" style={{ width: 160 }} placeholder="127.0.0.1:7890" value={data.proxy} onChange={(e) => setData({ ...data, proxy: e.target.value })} onBlur={() => save({ proxy: data.proxy })} />
          </SetRow>
          <SetRow title="开机启动" desc="关闭后，宿主调用时才启动，首次约慢 2 秒">
            <Toggle on={data.launch_at_login} onChange={(v) => save({ launch_at_login: v })} />
          </SetRow>
          <div className="hint" style={{ marginTop: 14 }}>数据</div>
          <SetRow title="保存请求内容" desc="关闭后记录里只留费用和模型">
            <Toggle on={data.save_prompts} onChange={(v) => save({ save_prompts: v })} />
          </SetRow>
          <SetRow title="数据位置" desc={<><span className="path">{(data.data_dir ?? '').replace(/^\/Users\/[^/]+/, '~')}</span>记录、规则、账号都在这里，换电脑时带走它；图片视频存在各自项目里</>}>
            <RevealButton path={data.data_dir} />
          </SetRow>
          <SetRow title="通知" desc="只发四类：额度、预算、媒体完成或失败、规则建议">
            {(['quota', 'budget', 'media', 'suggestions'] as const).map((k) => (
              <label key={k} className="row" style={{ gap: 4, marginLeft: 8 }}><input type="checkbox" checked={data.notifications[k]} onChange={(e) => save({ notifications: { ...data.notifications, [k]: e.target.checked } })} />{{ quota: '额度', budget: '预算', media: '媒体', suggestions: '建议' }[k]}</label>
            ))}
          </SetRow>
          <SetRow title="规则" last>
            <button className="btn sm" onClick={exportRules}>导出</button>
            <button className="btn sm" onClick={() => fileRef.current?.click()}>导入</button>
            <input ref={fileRef} type="file" accept=".yaml,.yml" style={{ display: 'none' }} onChange={(e) => { const f = e.target.files?.[0]; if (f) void importRules(f); e.target.value = ''; }} />
          </SetRow>
        </>
      )}
    </>
  );
}
