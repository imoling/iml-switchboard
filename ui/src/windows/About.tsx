import { useEffect, useState } from 'react';
import appIcon from '../assets/app-icon.svg';
import { desktop } from '../tauri';

export function About({ version: given }: { version?: string }) {
  const [version, setVersion] = useState(given ?? '');
  useEffect(() => { if (!given) void desktop.version().then(setVersion); }, [given]);
  // 产品名不走主题词表：主题只换界面说法，「关于」说的是这个应用本身
  useEffect(() => { void desktop.setTitle('关于 Switchboard'); }, []);
  return (
    <div className="about drag" data-tauri-drag-region>
      <img src={appIcon} alt="" />
      <div className="name">Switchboard</div>
      <div className="hint">版本 {version}</div>
      <div style={{ margin: '12px 0 4px' }}>iML Studio 出品</div>
      <div className="hint">© 2026 imoling · imoling.cn@gmail.com</div>
    </div>
  );
}
