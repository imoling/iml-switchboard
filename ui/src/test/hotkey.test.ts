import { describe, expect, it } from 'vitest';
import { hotkeyCaps, normalizeHotkey } from '../hotkey';

describe('快捷键', () => {
  it('旧的符号串和加号串都归一成 Tauri 格式', () => {
    expect(normalizeHotkey('⌥Space')).toBe('Alt+Space');
    expect(normalizeHotkey('Alt+Space')).toBe('Alt+Space');
    expect(normalizeHotkey('⇧⌘P')).toBe('Shift+Super+P');
    expect(normalizeHotkey('ctrl+shift+k')).toBe('Control+Shift+K');
  });
  it('Mac 显示符号，其他系统显示英文', () => {
    expect(hotkeyCaps('Alt+Space', true)).toEqual(['⌥', 'Space']);
    expect(hotkeyCaps('Control+Shift+K', false)).toEqual(['Ctrl', 'Shift', 'K']);
  });
});
