// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { fetchHotKeywords, fetchShopRanking, fetchSourcingAccounts, generateCopywriting, refreshHotKeywords, searchItems } from './api';
import { errorText, isCancelled, useSourcing } from './hooks';
import type { SearchResultItem } from './types';

vi.mock('./api', /* apiModuleMockFactory 用可控替身替换全部选品参考请求。 */ () => ({
  fetchHotKeywords: vi.fn(),
  refreshHotKeywords: vi.fn(),
  searchItems: vi.fn(),
  fetchShopRanking: vi.fn(),
  generateCopywriting: vi.fn(),
  fetchSourcingAccounts: vi.fn(),
}));

// hotMock、refreshMock、searchMock、rankingMock、copyMock、accountsMock 是各请求的可控替身。
const hotMock = vi.mocked(fetchHotKeywords);
// refreshMock 是手动采集热词请求替身。
const refreshMock = vi.mocked(refreshHotKeywords);
// searchMock 是查货请求替身。
const searchMock = vi.mocked(searchItems);
// rankingMock 是畅销榜请求替身。
const rankingMock = vi.mocked(fetchShopRanking);
// copyMock 是文案生成请求替身。
const copyMock = vi.mocked(generateCopywriting);
// accountsMock 是账号列表请求替身。
const accountsMock = vi.mocked(fetchSourcingAccounts);

/** item 构造一条用于断言的查货结果。 */
const item = (/* id 是商品标识。 */ id: string): SearchResultItem => ({ itemId: id, title: `商品${id}`, price: '1', wantCount: '', location: '', detailUrl: '' });

/** deferred 创建可由测试手动完成的 Promise，用于模拟慢请求与乱序响应。 */
const deferred = <T,>() => {
  // resolve 是外部可调用的完成函数。
  let resolve!: (value: T) => void;
  // promise 是待完成的请求结果。
  const promise = new Promise<T>(/* executor 捕获完成函数。 */ done => { resolve = done; });
  return { promise, resolve };
};

describe('useSourcing', /* 当前回调验证选品参考 Hook 的请求编排、取消与旧响应保护。 */ () => {
  beforeEach(/* 当前回调为每个用例设置默认成功响应。 */ () => {
    vi.resetAllMocks();
    hotMock.mockResolvedValue({ date: '2026-10-29', keywords: ['耳机'], status: 'collected', errorMessage: '' });
    accountsMock.mockResolvedValue([{ id: 'acc1', label: '小鱼' }, { id: 'acc2', label: '主号' }]);
    rankingMock.mockResolvedValue({ window: '7d', entries: [] });
    refreshMock.mockResolvedValue(undefined);
  });

  test('挂载时并行读取热词、账号与畅销榜，并默认选中第一个账号', /* 当前回调验证初始加载。 */ async () => {
    // hook 是被测 Hook 实例。
    const hook = renderHook(/* useSourcingHarness 渲染被测 Hook。 */ () => useSourcing());
    await waitFor(/* 等待初始请求完成。 */ () => expect(hook.result.current.hot?.keywords).toEqual(['耳机']));
    await waitFor(/* 等待账号默认选择。 */ () => expect(hook.result.current.accountId).toBe('acc1'));
    expect(rankingMock).toHaveBeenCalledWith('7d', expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.rankingLoading).toBe(false);
  });

  test('热词、账号与畅销榜读取失败时展示错误', /* 当前回调验证失败分支。 */ async () => {
    hotMock.mockRejectedValue(new Error('读取失败'));
    accountsMock.mockRejectedValue('x');
    rankingMock.mockRejectedValue(new Error('排行失败'));
    // hook 是被测 Hook 实例。
    const hook = renderHook(/* useSourcingHarness 渲染被测 Hook。 */ () => useSourcing());
    await waitFor(/* 等待错误写入。 */ () => expect(hook.result.current.hotError).toBe('读取失败'));
    await waitFor(/* 等待账号错误写入。 */ () => expect(hook.result.current.searchError).toBe('读取账号列表失败'));
    await waitFor(/* 等待排行错误写入。 */ () => expect(hook.result.current.rankingError).toBe('排行失败'));
    expect(hook.result.current.ranking).toEqual([]);
  });

  test('切换统计窗口会取消旧请求且旧窗口响应不覆盖新结果', /* 当前回调验证窗口切换与旧响应丢弃。 */ async () => {
    // slow 是 7 天窗口的慢响应。
    const slow = deferred<{ /** window 是窗口标识。 */ window: '7d' | '30d'; /** entries 是排行条目。 */ entries: [] | { /** itemId 是商品标识。 */ itemId: string; /** itemTitle 是标题。 */ itemTitle: string; /** soldQuantity 是销量。 */ soldQuantity: number; /** revenue 是营收。 */ revenue: number; /** orderCount 是单数。 */ orderCount: number }[] }>();
    rankingMock.mockReset();
    rankingMock.mockReturnValueOnce(slow.promise).mockResolvedValueOnce({ window: '30d', entries: [{ itemId: 'b', itemTitle: '新', soldQuantity: 9, revenue: 9, orderCount: 1 }] });
    // hook 是被测 Hook 实例。
    const hook = renderHook(/* useSourcingHarness 渲染被测 Hook。 */ () => useSourcing());
    // firstSignal 是 7 天窗口请求的取消信号。
    const firstSignal = rankingMock.mock.calls[0][1]?.signal as AbortSignal;
    act(/* 切换到 30 天窗口。 */ () => hook.result.current.setRankingWindow('30d'));
    await waitFor(/* 等待新窗口结果。 */ () => expect(hook.result.current.ranking[0]?.itemId).toBe('b'));
    expect(firstSignal.aborted).toBe(true);
    await act(/* 旧窗口响应迟到。 */ async () => { slow.resolve({ window: '7d', entries: [{ itemId: 'a', itemTitle: '旧', soldQuantity: 1, revenue: 1, orderCount: 1 }] }); });
    expect(hook.result.current.ranking[0]?.itemId).toBe('b');
  });

  test('查货校验关键词与账号，成功后写入结果', /* 当前回调验证输入校验与成功分支。 */ async () => {
    accountsMock.mockResolvedValue([]);
    // hook 是被测 Hook 实例。
    const hook = renderHook(/* useSourcingHarness 渲染被测 Hook。 */ () => useSourcing());
    await waitFor(/* 等待初始加载完成。 */ () => expect(hook.result.current.hot).not.toBeNull());
    await act(/* 空关键词查货。 */ async () => { await hook.result.current.search(); });
    expect(hook.result.current.searchError).toBe('请输入要查询的关键词');
    await act(/* 无账号查货。 */ async () => { await hook.result.current.search('耳机'); });
    expect(hook.result.current.searchError).toContain('添加并选择');
    expect(searchMock).not.toHaveBeenCalled();

    act(/* 选择账号。 */ () => hook.result.current.setAccountId('acc9'));
    searchMock.mockResolvedValueOnce([item('1')]);
    await act(/* 用热词直接查货。 */ async () => { await hook.result.current.search(' 耳机 '); });
    expect(searchMock).toHaveBeenCalledWith('acc9', '耳机', expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.keyword).toBe('耳机');
    expect(hook.result.current.results).toEqual([item('1')]);
    expect(hook.result.current.searchedKeyword).toBe('耳机');
    expect(hook.result.current.searching).toBe(false);
  });

  test('新查货取消旧查货，旧响应不覆盖新结果；失败展示服务端原因', /* 当前回调验证查货旧响应保护与失败分支。 */ async () => {
    // hook 是被测 Hook 实例。
    const hook = renderHook(/* useSourcingHarness 渲染被测 Hook。 */ () => useSourcing());
    await waitFor(/* 等待账号默认选择。 */ () => expect(hook.result.current.accountId).toBe('acc1'));
    // slow 是第一次查货的慢响应。
    const slow = deferred<SearchResultItem[]>();
    searchMock.mockReturnValueOnce(slow.promise).mockResolvedValueOnce([item('new')]);
    // firstSearch 是第一次查货的进行中 Promise。
    let firstSearch: Promise<void> = Promise.resolve();
    act(/* 发起第一次查货。 */ () => { firstSearch = hook.result.current.search('旧词'); });
    await act(/* 发起第二次查货。 */ async () => { await hook.result.current.search('新词'); });
    expect((searchMock.mock.calls[0][2]?.signal as AbortSignal).aborted).toBe(true);
    await act(/* 第一次查货迟到返回。 */ async () => { slow.resolve([item('old')]); await firstSearch; });
    expect(hook.result.current.results).toEqual([item('new')]);
    expect(hook.result.current.searchedKeyword).toBe('新词');

    searchMock.mockRejectedValueOnce(new Error('该关键词刚刚查询过，请稍后再试'));
    await act(/* 冷却中的查货。 */ async () => { await hook.result.current.search('新词'); });
    expect(hook.result.current.searchError).toBe('该关键词刚刚查询过，请稍后再试');
    searchMock.mockRejectedValueOnce(new Error('请求已取消'));
    await act(/* 被取消的查货不展示错误。 */ async () => { await hook.result.current.search('取消词'); });
    expect(hook.result.current.searchError).toBe('');
  });

  test('手动采集期间重复点击只发起一次，完成后重新读取快照', /* 当前回调验证采集去重与失败提示。 */ async () => {
    // hook 是被测 Hook 实例。
    const hook = renderHook(/* useSourcingHarness 渲染被测 Hook。 */ () => useSourcing());
    await waitFor(/* 等待初始热词。 */ () => expect(hook.result.current.hot).not.toBeNull());
    // pending 是进行中的采集请求。
    const pending = deferred<void>();
    refreshMock.mockReturnValueOnce(pending.promise);
    // first、second 是两次点击产生的采集 Promise。
    let first: Promise<void> = Promise.resolve();
    // second 是采集进行中的重复点击。
    let second: Promise<void> = Promise.resolve();
    act(/* 连续点击两次。 */ () => { first = hook.result.current.refreshHot(); second = hook.result.current.refreshHot(); });
    expect(refreshMock).toHaveBeenCalledTimes(1);
    await act(/* 完成采集。 */ async () => { pending.resolve(); await first; await second; });
    expect(hotMock).toHaveBeenCalledTimes(2);
    expect(hook.result.current.refreshingHot).toBe(false);

    refreshMock.mockRejectedValueOnce(new Error('本次没有取到平台热词'));
    await act(/* 采集失败。 */ async () => { await hook.result.current.refreshHot(); });
    expect(hook.result.current.hotError).toBe('本次没有取到平台热词');
  });

  test('文案预填、生成成功与失败，旧生成结果不覆盖预填后的状态', /* 当前回调验证文案流程。 */ async () => {
    // hook 是被测 Hook 实例。
    const hook = renderHook(/* useSourcingHarness 渲染被测 Hook。 */ () => useSourcing());
    await waitFor(/* 等待初始加载。 */ () => expect(hook.result.current.hot).not.toBeNull());
    await act(/* 空主题生成。 */ async () => { await hook.result.current.generate(); });
    expect(hook.result.current.copyError).toBe('请先填写品类关键词或商品标题');
    expect(copyMock).not.toHaveBeenCalled();

    act(/* 从热词预填品类分析。 */ () => hook.result.current.prepareCopywriting('耳机', '19 元', 'analysis'));
    expect(hook.result.current.copyMode).toBe('analysis');
    copyMock.mockResolvedValueOnce('分析结果');
    await act(/* 生成分析。 */ async () => { await hook.result.current.generate(); });
    expect(copyMock).toHaveBeenCalledWith({ mode: 'analysis', topic: '耳机', reference: '19 元' }, expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(hook.result.current.copyContent).toBe('分析结果');

    // slow 是慢的文案生成响应。
    const slow = deferred<string>();
    copyMock.mockReturnValueOnce(slow.promise);
    // pendingGenerate 是进行中的生成。
    let pendingGenerate: Promise<void> = Promise.resolve();
    act(/* 发起慢生成。 */ () => { pendingGenerate = hook.result.current.generate(); });
    act(/* 切换到另一个商品，旧生成作废。 */ () => hook.result.current.prepareCopywriting('商品B', '', 'listing'));
    await act(/* 旧生成迟到。 */ async () => { slow.resolve('旧文案'); await pendingGenerate; });
    expect(hook.result.current.copyContent).toBe('');
    expect(hook.result.current.copyTopic).toBe('商品B');
    expect(hook.result.current.copying).toBe(false);

    copyMock.mockRejectedValueOnce(new Error('尚未配置 AI 接口'));
    await act(/* 生成失败。 */ async () => { await hook.result.current.generate(); });
    expect(hook.result.current.copyError).toBe('尚未配置 AI 接口');
    expect(hook.result.current.copyContent).toBe('');
  });

  test('卸载时取消进行中的请求', /* 当前回调验证卸载取消。 */ async () => {
    // pendingHot 是永不完成的热词读取。
    const pendingHot = deferred<never>();
    hotMock.mockReturnValueOnce(pendingHot.promise);
    // hook 是被测 Hook 实例。
    const hook = renderHook(/* useSourcingHarness 渲染被测 Hook。 */ () => useSourcing());
    // hotSignal 是热词读取的取消信号。
    const hotSignal = hotMock.mock.calls[0][0]?.signal as AbortSignal;
    // accountSignal 是账号读取的取消信号。
    const accountSignal = accountsMock.mock.calls[0][0]?.signal as AbortSignal;
    hook.unmount();
    expect(hotSignal.aborted).toBe(true);
    expect(accountSignal.aborted).toBe(true);
  });

  test('错误辅助函数区分取消与普通失败', /* 当前回调验证错误文本转换。 */ () => {
    expect(isCancelled(new Error('请求已取消'))).toBe(true);
    expect(isCancelled(new Error('x'))).toBe(false);
    expect(errorText(new Error('原因'), '兜底')).toBe('原因');
    expect(errorText('x', '兜底')).toBe('兜底');
    expect(errorText(new Error(''), '兜底')).toBe('兜底');
  });
});
