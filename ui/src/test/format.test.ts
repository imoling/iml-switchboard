import { describe, expect, it } from 'vitest';
import { num, setMoney, usd } from '../format';

describe('数字格式', () => {
  it('次数：千分位，十万起用万', () => {
    expect(num(7)).toBe('7');
    expect(num(1359)).toBe('1,359');
    expect(num(99999)).toBe('99,999');
    expect(num(123456)).toBe('12.3 万');
  });
  it('金额：千分位，十万起用万，不到一分多留两位', () => {
    setMoney('USD');
    expect(usd(2.41)).toBe('$2.41');
    expect(usd(1119.71)).toBe('$1,119.71');
    expect(usd(123456.78)).toBe('$12.35 万');
    expect(usd(0.005)).toBe('$0.0050');
    expect(usd(0)).toBe('$0');
  });
});
