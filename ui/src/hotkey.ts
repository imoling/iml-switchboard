/** 快捷键只存一种格式：Tauri 的 "Alt+Shift+Space"；显示时按系统换成键帽 */

const SYMBOL_TO_NAME: Record<string, string> = { '⌃': 'Control', '⌥': 'Alt', '⇧': 'Shift', '⌘': 'Super' };
const ORDER = ['Control', 'Alt', 'Shift', 'Super'];

/** 把符号串（旧数据「⌥Space」）或加号串统一成加号串，修饰键按固定顺序 */
export function normalizeHotkey(h: string): string {
  let rest = (h ?? '').trim();
  const mods = new Set<string>();
  for (;;) {
    const ch = rest.charAt(0);
    if (SYMBOL_TO_NAME[ch]) { mods.add(SYMBOL_TO_NAME[ch]); rest = rest.slice(1).trim(); } else break;
  }
  const parts = rest.split('+').map((p) => p.trim()).filter(Boolean);
  let key = '';
  for (const p of parts) {
    const low = p.toLowerCase();
    if (['ctrl', 'control'].includes(low)) mods.add('Control');
    else if (['alt', 'option', 'opt'].includes(low)) mods.add('Alt');
    else if (low === 'shift') mods.add('Shift');
    else if (['super', 'cmd', 'command', 'meta', 'win', 'cmdorctrl', 'commandorcontrol'].includes(low)) mods.add('Super');
    else key = p.length === 1 ? p.toUpperCase() : p;
  }
  return [...ORDER.filter((m) => mods.has(m)), key || 'Space'].join('+');
}

export const isMac = (): boolean => typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);

const MAC: Record<string, string> = { Control: '⌃', Alt: '⌥', Shift: '⇧', Super: '⌘', Space: 'Space', Enter: '↩', Escape: '⎋', Backspace: '⌫', Tab: '⇥', ArrowUp: '↑', ArrowDown: '↓', ArrowLeft: '←', ArrowRight: '→' };
const WIN: Record<string, string> = { Control: 'Ctrl', Alt: 'Alt', Shift: 'Shift', Super: 'Win', Space: 'Space', Enter: 'Enter', Escape: 'Esc', ArrowUp: '↑', ArrowDown: '↓', ArrowLeft: '←', ArrowRight: '→' };

/** 显示用的键帽列表，Mac 上是符号，其他系统是英文名 */
export function hotkeyCaps(h: string, mac = isMac()): string[] {
  const table = mac ? MAC : WIN;
  return normalizeHotkey(h).split('+').map((p) => table[p] ?? p);
}
