/** 后台服务：发现连着的不是安装包里那份，以及从界面重启 */
import { useEffect, useState } from 'react';
import { api, ApiError } from './api/client';
import type { Status } from './api/types';
import { desktop } from './tauri';

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
const alive = () => api.status().then(() => true, () => false);

/** 正在跑的后台和安装包里附带的不是同一份构建 */
export function useStaleBackend(status: Status | null | 'offline') {
  const [bundled, setBundled] = useState<string | null>(null);
  useEffect(() => { void desktop.bundledBuild().then((b) => setBundled(b ?? null)); }, []);
  return !!(bundled && status && status !== 'offline' && status.build && status.build !== bundled);
}

/** 让旧后台退出、等它让出端口，再拉起安装包里的那份；返回失败原因，成功返回 null */
export async function restartBackend(): Promise<string | null> {
  try {
    await api.quit();
  } catch (e) {
    if (e instanceof ApiError && (e.status === 404 || e.status === 405)) return '旧版后台不支持，在终端停掉它后再点启动';
    if (await alive()) return (e as Error).message;
  }
  for (let i = 0; i < 40 && (await alive()); i++) await sleep(150);
  if (await alive()) return '旧后台没有退出';
  await desktop.startBackend();
  for (let i = 0; i < 60; i++) {
    await sleep(200);
    if (await alive()) return null;
  }
  return '新后台没有起来';
}
