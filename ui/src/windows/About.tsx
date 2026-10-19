import { useEffect, useState } from 'react';
import appIcon from '../assets/app-icon.svg';
import { api } from '../api/client';
import type { UpdateInfo } from '../api/types';
import { desktop } from '../tauri';

export function About({ version: given }: { version?: string }) {
  const [version, setVersion] = useState(given ?? '');
  const [info, setInfo] = useState<UpdateInfo | null>(null);
  useEffect(() => { void api.update().then(setInfo).catch(() => null); }, []);
  useEffect(() => { if (!given) void desktop.version().then(setVersion); }, [given]);
  // 产品名不走主题词表：主题只换界面说法，「关于」说的是这个应用本身
  useEffect(() => { void desktop.setTitle('关于 Switchboard'); }, []);
  return (
    <div className="about drag" data-tauri-drag-region>
      <img src={appIcon} alt="" />
      <div className="name">Switchboard</div>
      <div className="hint">版本 {version}{info && !info.has_update && ' · 已是最新'}</div>
      {info?.has_update && <button className="btn sm pri" style={{ marginTop: 6 }} onClick={() => desktop.openUrl(info.url)}>下载新版本 {info.latest}</button>}
      <div style={{ margin: '12px 0 4px' }}>iML Studio 出品</div>
      <div className="hint">© 2026 imoling · imoling.cn@gmail.com</div>
    </div>
  );
}
