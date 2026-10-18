import { describe, expect, it } from 'vitest';
import { ALL_SPRITES } from '../components/Pixel';

describe('像素小人', () => {
  it('每张图都是矩形，且只用字符表里的颜色', () => {
    for (const s of ALL_SPRITES) {
      const w = s.map[0].length;
      for (const row of s.map) {
        expect(row.length, row).toBe(w);
        for (const ch of row) if (ch !== '.') expect(s.palette[ch], `未知字符 ${ch} in ${row}`).toBeTruthy();
      }
    }
  });
});
