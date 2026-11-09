import { describe, expect, test } from 'vitest';
import { ApiError } from '../../../shared/http/client';
import type { Item, ItemStats } from './models';
import {
  applyStatsToItems,
  batchRefreshMessage,
  countRefreshableStats,
  formatStatsAge,
  statsRowKey,
  statsTooltip,
} from './stats';
import { statsRefreshErrorMessage } from './api';

// statsNow 是相对时间断言的固定基准时刻。
const statsNow = new Date('2026-11-20T10:00:00Z');

// statsFixture 是一条已抓取的统计样本。
const statsFixture: ItemStats = {
  cookie_id: 'acc1',
  item_id: 'i1',
  browse_cnt: 22,
  want_cnt: 2,
  collect_cnt: 1,
  sold_cnt: 0,
  fetched_at: '2026-11-20T07:00:00Z',
  fresh: true,
};

// itemFixture 是带统计的商品行样本。
const itemFixture = { cookie_id: 'acc1', item_id: 'i1', item_title: '示例商品', stats: statsFixture } as Item;

describe('statsRowKey', /* 覆盖统计索引键的账号隔离。 */ () => {
  test('同一商品在不同账号下得到不同键', /* 键必须同时包含账号与商品。 */ () => {
    expect(statsRowKey('acc1', 'i1')).not.toBe(statsRowKey('acc2', 'i1'));
  });
});

describe('formatStatsAge', /* 覆盖相对时间文案的各档位与非法输入。 */ () => {
  test('区分刚刚、分钟、小时与天', /* 四个档位都必须命中对应文案。 */ () => {
    expect(formatStatsAge('2026-11-20T09:59:30Z', statsNow)).toBe('刚刚');
    expect(formatStatsAge('2026-11-20T09:30:00Z', statsNow)).toBe('30 分钟前');
    expect(formatStatsAge('2026-11-20T07:00:00Z', statsNow)).toBe('3 小时前');
    expect(formatStatsAge('2026-11-18T10:00:00Z', statsNow)).toBe('2 天前');
  });

  test('空串与非法时间返回未知，未来时间按刚刚处理', /* 不得出现负数文案。 */ () => {
    expect(formatStatsAge('', statsNow)).toBe('未知');
    expect(formatStatsAge('not-a-date', statsNow)).toBe('未知');
    expect(formatStatsAge('2026-11-21T10:00:00Z', statsNow)).toBe('刚刚');
  });
});

describe('statsTooltip', /* 覆盖悬浮文案字段。 */ () => {
  test('包含收藏、已售与更新于', /* 三个部分都必须出现。 */ () => {
    // tooltip 是生成的悬浮文案。
    const tooltip = statsTooltip(statsFixture);
    expect(tooltip).toContain('收藏 1');
    expect(tooltip).toContain('已售 0');
    expect(tooltip).toContain('更新于');
  });
});

describe('applyStatsToItems', /* 覆盖刷新结果的就地合并语义。 */ () => {
  test('按账号与商品命中替换，未命中保持原值', /* 未命中不得被清空或补零。 */ () => {
    // other 是同账号下另一件没有统计的商品。
    const other = { cookie_id: 'acc1', item_id: 'i2' } as Item;
    // updated 是本次刷新返回的新统计。
    const updated = { ...statsFixture, browse_cnt: 99 };
    // merged 是合并后的商品列表。
    const merged = applyStatsToItems([itemFixture, other], [updated]);
    expect(merged[0].stats?.browse_cnt).toBe(99);
    expect(merged[1].stats).toBeUndefined();
  });

  test('空结果集合直接返回原数组', /* 避免无意义的数组重建。 */ () => {
    // items 是合并前的商品列表。
    const items = [itemFixture];
    expect(applyStatsToItems(items, [])).toBe(items);
  });
});

describe('countRefreshableStats', /* 覆盖需要刷新的数量统计。 */ () => {
  test('未抓取与已过期都计入需要刷新', /* 新鲜数据不计入。 */ () => {
    // stale 是缓存已过期的商品。
    const stale = { cookie_id: 'acc1', item_id: 'i2', stats: { ...statsFixture, item_id: 'i2', fresh: false } } as Item;
    // missing 是尚未抓取统计的商品。
    const missing = { cookie_id: 'acc1', item_id: 'i3' } as Item;
    expect(countRefreshableStats([itemFixture, stale, missing])).toBe(2);
  });
});

describe('batchRefreshMessage', /* 覆盖批量刷新结果提示。 */ () => {
  test('区分有刷新、全部新鲜与遇风控停止', /* 三种情况都必须给出不同提示。 */ () => {
    expect(batchRefreshMessage({ refreshed: [statsFixture], skipped: 1, remaining: 2, stopped: false })).toBe('已刷新 1 个，剩余 2 个待刷新。');
    expect(batchRefreshMessage({ refreshed: [], skipped: 3, remaining: 0, stopped: false })).toContain('都是最新的');
    expect(batchRefreshMessage({ refreshed: [statsFixture], skipped: 0, remaining: 1, stopped: true })).toContain('人机验证');
  });
});

describe('statsRefreshErrorMessage', /* 覆盖刷新失败的可操作提示。 */ () => {
  test('按状态码给出可操作提示', /* 409/503/403 与未知错误都必须给出确定文案。 */ () => {
    expect(statsRefreshErrorMessage(new ApiError(409, { code: 'conflict', message: 'x' }))).toContain('人机验证');
    expect(statsRefreshErrorMessage(new ApiError(503, { code: 'unavailable', message: 'x' }))).toContain('抓取助手不可用');
    expect(statsRefreshErrorMessage(new ApiError(403, { code: 'forbidden', message: 'x' }))).toContain('无权');
    expect(statsRefreshErrorMessage(new Error('普通失败'))).toBe('普通失败');
    expect(statsRefreshErrorMessage('字符串错误')).toBe('刷新商品统计失败。');
  });
});
