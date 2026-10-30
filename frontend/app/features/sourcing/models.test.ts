import { describe, expect, test } from 'vitest';
import { accountLabel, copywritingTitleAndBody, formatRevenue, normalizeHotKeywordStatus, normalizeRankingWindow, searchItemReference } from './models';

describe('选品参考模型函数', /* 当前回调验证纯函数的归一化与格式化规则。 */ () => {
  test('热词状态与窗口归一化', /* 当前回调验证未知值的安全回退。 */ () => {
    expect(normalizeHotKeywordStatus('ok')).toBe('collected');
    expect(normalizeHotKeywordStatus('failed')).toBe('failed');
    expect(normalizeHotKeywordStatus('')).toBe('pending');
    expect(normalizeRankingWindow('30d')).toBe('30d');
    expect(normalizeRankingWindow('x')).toBe('7d');
  });

  test('账号展示名优先昵称其次备注', /* 当前回调验证三种回退层级。 */ () => {
    expect(accountLabel('a1', ' 小鱼 ', '主号')).toBe('小鱼');
    expect(accountLabel('a1', '', ' 主号 ')).toBe('主号（a1）');
    expect(accountLabel('a1', ' ', '')).toBe('a1');
  });

  test('查货结果参考信息只拼接存在的字段', /* 当前回调验证缺失字段被跳过。 */ () => {
    expect(searchItemReference({ itemId: '1', title: 't', price: '19', wantCount: '485', location: '广东', detailUrl: '' })).toBe('售价 19 元，485 人想要，卖家地区 广东');
    expect(searchItemReference({ itemId: '1', title: 't', price: '', wantCount: '', location: '', detailUrl: '' })).toBe('');
  });

  test('营收格式化与文案拆分', /* 当前回调验证两位小数与首行标题拆分。 */ () => {
    expect(formatRevenue(12.345)).toBe('12.35');
    expect(formatRevenue(Number.NaN)).toBe('0.00');
    expect(copywritingTitleAndBody('  标题\n第一行\r\n第二行  ')).toEqual({ title: '标题', body: '第一行\n第二行' });
    expect(copywritingTitleAndBody('')).toEqual({ title: '', body: '' });
  });
});
