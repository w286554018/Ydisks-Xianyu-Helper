// @vitest-environment jsdom
import { act,renderHook } from '@testing-library/react';
import { useState } from 'react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import { refreshItemStats,refreshStatsBatch,statsRefreshErrorMessage } from './api';
import { useItemStatsRefresh } from './hooks';
import type { Item,ItemStats } from './models';

vi.mock('./api', /* statsApiMockFactory 提供商品统计刷新 Hook 所需的确定性 API 替身。 */ () => ({
  refreshItemStats: vi.fn(),
  refreshStatsBatch: vi.fn(),
  statsRefreshErrorMessage: vi.fn(),
  cancelItemPublishBatch: vi.fn(),
  deleteItemPublishBatch: vi.fn(),
  getItemPublishBatch: vi.fn(),
  getItemPublishBatches: vi.fn(),
  previewItemPublishBatch: vi.fn(),
  recommendPublishCategory: vi.fn(),
  retryFailedItemPublishBatch: vi.fn(),
  startItemPublishBatch: vi.fn(),
}));

// refreshOneMock 是单商品统计刷新的可控替身。
const refreshOneMock = vi.mocked(refreshItemStats);
// refreshBatchMock 是批量统计刷新的可控替身。
const refreshBatchMock = vi.mocked(refreshStatsBatch);
// errorMessageMock 是统计错误文案的可控替身。
const errorMessageMock = vi.mocked(statsRefreshErrorMessage);

// statsSample 是账号 acc1 下一件商品的统计样本。
const statsSample: ItemStats = {
  cookie_id: 'acc1', item_id: 'i1', browse_cnt: 31, want_cnt: 2, collect_cnt: 1, sold_cnt: 0,
  fetched_at: '2026-11-09T15:00:00Z', fresh: true,
};

// itemSample 是待刷新的商品行样本。
const itemSample = { cookie_id: 'acc1', item_id: 'i1', item_title: '示例商品' } as Item;

// useStatsHarness 是被测 Hook 的最小宿主：持有商品列表状态并暴露刷新动作与当前列表。
const useStatsHarness = (initial: Item[]) => {
  // [items, setItems] 是宿主持有的商品列表状态。
  const [items, setItems] = useState<Item[]>(initial);
  // state 是被测 Hook 返回的状态与动作。
  const state = useItemStatsRefresh(setItems);
  return { items, ...state };
};

// deferred 构造一个可手动完成的 Promise，用于控制异步时序。
const deferred = <T,>() => {
  // resolve、reject 是手动完成该 Promise 的入口。
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  // promise 是受控的 Promise 实例。
  const promise = new Promise<T>(/* executor 捕获完成入口，供用例控制异步时序。 */ (res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
};

beforeEach(/* 每个用例前重置全部替身，避免相互影响。 */ () => {
  vi.resetAllMocks();
  errorMessageMock.mockReturnValue('刷新失败，请稍后重试。');
});

// statsBatchResponseForTest 是批量刷新结果的测试替身类型，字段与接口响应一一对应。
interface statsBatchResponseForTest {
  /** refreshed 是本次成功刷新的统计集合。 */
  refreshed: ItemStats[];
  /** skipped 是因数据仍新鲜而跳过的数量。 */
  skipped: number;
  /** remaining 是被上限截断、留待下次刷新的数量。 */
  remaining: number;
  /** stopped 表示是否因平台风控提前停止。 */
  stopped: boolean;
}

describe('useItemStatsRefresh', /* 覆盖商品统计刷新的成功、失败、并发与卸载行为。 */ () => {
  test('单行刷新成功后就地更新该行并清空进行中状态', /* 完成后不得整页重载。 */ async () => {
    // pending 是受控的单行刷新结果。
    const pending = deferred<ItemStats>();
    refreshOneMock.mockReturnValue(pending.promise);
    // hook 是被测 Hook 的渲染结果。
    const hook = renderHook(/* 宿主使用一件商品渲染被测 Hook。 */ () => useStatsHarness([itemSample]));

    act(/* 触发单行刷新但不等待完成。 */ () => { void hook.result.current.refreshOne(itemSample); });
    expect(hook.result.current.refreshingKeys).toEqual(['acc1-i1']);

    await act(/* 完成单行刷新并等待状态收敛。 */ async () => { pending.resolve(statsSample); await pending.promise; });
    expect(hook.result.current.items[0].stats?.browse_cnt).toBe(31);
    expect(hook.result.current.refreshingKeys).toEqual([]);
    // 成功后不展示额外提示：行内数字就地变化即为反馈，message 必须保持为空。
    expect(hook.result.current.message).toBe('');
    expect(refreshOneMock).toHaveBeenCalledWith('acc1', 'i1');
  });

  test('单行刷新失败时给出可操作提示且不清空已有统计', /* 失败不得写入半成品数据。 */ async () => {
    refreshOneMock.mockRejectedValue(new Error('boom'));
    // hook 是被测 Hook 的渲染结果，初始行已带统计。
    const hook = renderHook(/* 宿主使用带统计的商品渲染。 */ () => useStatsHarness([{ ...itemSample, stats: { ...statsSample, browse_cnt: 7 } }]));

    await act(/* 触发并等待失败的单行刷新。 */ async () => { await hook.result.current.refreshOne(itemSample); });
    expect(hook.result.current.message).toBe('刷新失败，请稍后重试。');
    expect(hook.result.current.items[0].stats?.browse_cnt).toBe(7);
    expect(hook.result.current.refreshingKeys).toEqual([]);
  });

  test('批量刷新成功后就地更新命中行并提示剩余数量', /* 批量结果只更新返回的行。 */ async () => {
    refreshBatchMock.mockResolvedValue({ refreshed: [statsSample], skipped: 1, remaining: 2, stopped: false });
    // other 是本次批量未命中的商品行，必须保持原样。
    const other = { cookie_id: 'acc1', item_id: 'i2' } as Item;
    // hook 是被测 Hook 的渲染结果。
    const hook = renderHook(/* 宿主使用两件商品渲染。 */ () => useStatsHarness([itemSample, other]));

    await act(/* 触发并等待批量刷新。 */ async () => { await hook.result.current.refreshPage([itemSample, other]); });
    expect(hook.result.current.items[0].stats?.want_cnt).toBe(2);
    expect(hook.result.current.items[1].stats).toBeUndefined();
    expect(hook.result.current.message).toBe('已刷新 1 个，剩余 2 个待刷新。');
    expect(hook.result.current.batchRefreshing).toBe(false);
    expect(refreshBatchMock).toHaveBeenCalledWith('acc1', ['i1', 'i2']);
  });

  test('批量刷新遇到风控停止时提示人工验证', /* stopped 为真时必须转成可操作提示。 */ async () => {
    refreshBatchMock.mockResolvedValue({ refreshed: [statsSample], skipped: 0, remaining: 3, stopped: true, stop_reason: 'risk_control' });

    // hook 是被测 Hook 的渲染结果。
    const hook = renderHook(/* 宿主使用一件商品渲染。 */ () => useStatsHarness([itemSample]));

    await act(/* 触发并等待被风控中止的批量刷新。 */ async () => { await hook.result.current.refreshPage([itemSample]); });
    expect(hook.result.current.message).toContain('人机验证');
  });

  test('批量刷新失败时展示错误提示且不修改列表', /* 失败不得写入任何统计。 */ async () => {
    refreshBatchMock.mockRejectedValue(new Error('boom'));
    errorMessageMock.mockReturnValue('本机抓取助手不可用：请确认助手已启动、反向隧道已连接。');
    // hook 是被测 Hook 的渲染结果。
    const hook = renderHook(/* 宿主使用一件商品渲染。 */ () => useStatsHarness([itemSample]));

    await act(/* 触发并等待失败的批量刷新。 */ async () => { await hook.result.current.refreshPage([itemSample]); });
    expect(hook.result.current.message).toContain('本机抓取助手不可用');
    expect(hook.result.current.items[0].stats).toBeUndefined();
  });

  test('较旧的批量响应不得覆盖较新的结果', /* 并发批量刷新必须保持最新结果。 */ async () => {
    // first 是较早发起、较晚完成的批量请求。
    // first 是较早发起、较晚完成的批量请求。
    const first = deferred<statsBatchResponseForTest>();
    // second 是较晚发起、较早完成的批量请求。
    const second = deferred<statsBatchResponseForTest>();
    refreshBatchMock.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    // hook 是被测 Hook 的渲染结果。
    const hook = renderHook(/* 宿主使用一件商品渲染。 */ () => useStatsHarness([itemSample]));

    act(/* 发起第一次批量刷新。 */ () => { void hook.result.current.refreshPage([itemSample]); });
    act(/* 发起第二次批量刷新。 */ () => { void hook.result.current.refreshPage([itemSample]); });
    await act(/* 先完成第二次（较新）请求。 */ async () => {
      second.resolve({ refreshed: [{ ...statsSample, browse_cnt: 99 }], skipped: 0, remaining: 0, stopped: false });
      await second.promise;
    });
    await act(/* 再完成第一次（较旧）请求。 */ async () => {
      first.resolve({ refreshed: [{ ...statsSample, browse_cnt: 1 }], skipped: 0, remaining: 0, stopped: false });
      await first.promise;
    });
    expect(hook.result.current.items[0].stats?.browse_cnt).toBe(99);
  });

  test('组件卸载后完成的刷新不再写入状态', /* 卸载后不得触发状态更新。 */ async () => {
    // pending 是受控的单行刷新结果。
    const pending = deferred<ItemStats>();
    refreshOneMock.mockReturnValue(pending.promise);
    // hook 是被测 Hook 的渲染结果。
    const hook = renderHook(/* 宿主使用一件商品渲染。 */ () => useStatsHarness([itemSample]));

    act(/* 触发单行刷新后立刻卸载组件。 */ () => {
      void hook.result.current.refreshOne(itemSample);
      hook.unmount();
    });
    await act(/* 卸载后再完成请求；不得抛出异常。 */ async () => { pending.resolve(statsSample); await pending.promise; });
    expect(refreshOneMock).toHaveBeenCalledTimes(1);
  });

  test('空商品集合不会发起批量请求', /* 空页时不应产生任何平台访问。 */ async () => {
    // hook 是被测 Hook 的渲染结果。
    const hook = renderHook(/* 宿主使用空列表渲染。 */ () => useStatsHarness([]));

    await act(/* 触发空集合的批量刷新。 */ async () => { await hook.result.current.refreshPage([]); });
    expect(refreshBatchMock).not.toHaveBeenCalled();
  });
});
