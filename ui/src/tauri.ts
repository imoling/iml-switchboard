/** Tauri 桥：在浏览器里开发时所有调用都安全降级 */
import { invoke } from '@tauri-apps/api/core';

export const inTauri = typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window;

export function windowLabel(): 'main' | 'popover' | 'quick' | 'about' {
  const q = new URLSearchParams(location.search).get('win');
  if (q === 'popover' || q === 'quick' || q === 'about') return q;
  return 'main';
}

async function call<T = void>(cmd: string, args?: Record<string, unknown>): Promise<T | undefined> {
  if (!inTauri) return undefined;
  try { return await invoke<T>(cmd, args); } catch (e) { console.warn(cmd, e); return undefined; }
}

export const desktop = {
  showMain: () => call('show_main'),
  toggleQuick: () => call('toggle_quick'),
  hide: (label: string) => call('hide_window', { label }),
  setHotkey: (hotkey: string) => call('set_hotkey', { hotkey: toAccelerator(hotkey) }),
  setTrayStatus: (status: 'ok' | 'warn' | 'error' | 'off', tooltip?: string) => call('set_tray_status', { status, tooltip }),
  startBackend: () => call('start_backend'),
  showAbout: () => call('show_about'),
  version: async () => (await call<string>('app_version')) ?? '1.0.0',
  async setTitle(title: string) {
    if (!inTauri) { document.title = title; return; }
    const { getCurrentWindow } = await import('@tauri-apps/api/window');
    await getCurrentWindow().setTitle(title);
  },
  async startDrag() {
    if (!inTauri) return;
    const { getCurrentWindow } = await import('@tauri-apps/api/window');
    await getCurrentWindow().startDragging();
  },
  async onShown(cb: () => void): Promise<() => void> {
    if (!inTauri) return () => {};
    const { getCurrentWindow } = await import('@tauri-apps/api/window');
    const un = await getCurrentWindow().listen('imls://shown', cb);
    return un;
  },
  async copyText(text: string) {
    if (inTauri) {
      const { writeText } = await import('@tauri-apps/plugin-clipboard-manager');
      await writeText(text);
    } else {
      await navigator.clipboard.writeText(text);
    }
  },
  async copyImage(path: string) {
    if (!inTauri) return false;
    return (await call<boolean>('copy_image', { path })) ?? false;
  },
  async notify(title: string, body: string) {
    if (!inTauri) { console.info('[notice]', title, body); return; }
    const n = await import('@tauri-apps/plugin-notification');
    let ok = await n.isPermissionGranted();
    if (!ok) ok = (await n.requestPermission()) === 'granted';
    if (ok) n.sendNotification({ title, body });
  },
  async revealInFinder(path: string) {
    if (!inTauri) return;
    const { revealItemInDir } = await import('@tauri-apps/plugin-opener');
    await revealItemInDir(path);
  },
  async openUrl(url: string) {
    if (!inTauri) { window.open(url, '_blank'); return; }
    const { openUrl } = await import('@tauri-apps/plugin-opener');
    await openUrl(url);
  },
  async saveDialog(defaultName: string): Promise<string | null> {
    if (!inTauri) return null;
    const { save } = await import('@tauri-apps/plugin-dialog');
    return save({ defaultPath: defaultName });
  },
  async openDialog(): Promise<string | null> {
    if (!inTauri) return null;
    const { open } = await import('@tauri-apps/plugin-dialog');
    const r = await open({ multiple: false, filters: [{ name: 'YAML', extensions: ['yaml', 'yml'] }] });
    return typeof r === 'string' ? r : null;
  },
};

/** 设置里存的是 ⌥Space 这种写法，插件要 Alt+Space */
export function toAccelerator(h: string): string {
  const map: Record<string, string> = { '⌥': 'Alt', '⌘': 'Super', '⇧': 'Shift', '⌃': 'Control' };
  const mods: string[] = [];
  let rest = h.trim();
  for (;;) {
    const ch = rest.charAt(0);
    if (map[ch]) { mods.push(map[ch]); rest = rest.slice(1).trim(); } else break;
  }
  rest = rest.replace(/^\+/, '').trim();
  return [...mods, rest || 'Space'].join('+');
}
