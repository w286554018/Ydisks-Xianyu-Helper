import { beforeEach, describe, expect, test, vi } from 'vitest';

// getMock、postMock 是选品参考契约请求的共享客户端替身；runOptions 记录每次请求的超时与取消参数。
const { getMock, postMock, runOptions } = vi.hoisted(/* contractMockFactory 创建契约客户端方法替身。 */ () => ({ getMock: vi.fn(), postMock: vi.fn(), runOptions: [] as unknown[] }));

// ContractMockResult 是测试契约请求回调返回的最小响应模型。
interface ContractMockResult {
  // data 是契约客户端解码出的成功响应。
  data?: unknown;
}

vi.mock('../../../shared/api-contract/client', /* contractClientModuleMockFactory 提供选品参考 API 适配测试所需的契约依赖。 */ () => ({
  // contractClientMock 只提供本测试覆盖的 GET 与 POST 操作。
  contractClient: { GET: getMock, POST: postMock },
  // runContractRequestMock 记录请求选项并执行 feature adapter 传入的请求回调。
  runContractRequest: /* runContractRequestMock 执行契约请求回调。 */ async (/* execute 是 feature adapter 构造的契约请求动作。 */ execute: (signal: AbortSignal) => Promise<ContractMockResult>, /* options 是请求超时与取消参数。 */ options?: unknown) => {
    runOptions.push(options);
    return (await execute(new AbortController().signal)).data;
  },
}));

import { fetchHotKeywords, fetchShopRanking, fetchSourcingAccounts, generateCopywriting, refreshHotKeywords, searchItems } from './api';

describe('选品参考 API 适配', /* 当前回调验证传输 DTO 到页面模型的转换与请求参数。 */ () => {
  beforeEach(/* 当前回调重置契约替身与请求选项记录。 */ () => {
    getMock.mockReset();
    postMock.mockReset();
    runOptions.length = 0;
  });

  test('热词快照状态被归一为页面状态', /* 当前回调验证 ok、failed 与空状态的映射。 */ async () => {
    getMock.mockResolvedValueOnce({ data: { date: '2026-10-29', keywords: ['耳机'], status: 'ok', error_message: '' } });
    await expect(fetchHotKeywords()).resolves.toEqual({ date: '2026-10-29', keywords: ['耳机'], status: 'collected', errorMessage: '' });
    getMock.mockResolvedValueOnce({ data: { date: '2026-10-29', keywords: [], status: 'failed', error_message: '本次没有取到平台热词' } });
    await expect(fetchHotKeywords()).resolves.toMatchObject({ status: 'failed', errorMessage: '本次没有取到平台热词' });
    getMock.mockResolvedValueOnce({ data: { date: '2026-10-29', keywords: null, status: '', error_message: null } });
    await expect(fetchHotKeywords()).resolves.toEqual({ date: '2026-10-29', keywords: [], status: 'pending', errorMessage: '' });
    expect(getMock).toHaveBeenCalledWith('/api/v1/sourcing/hot-keywords', expect.anything());
  });

  test('手动采集热词使用较长超时', /* 当前回调验证采集请求路径与超时设置。 */ async () => {
    postMock.mockResolvedValueOnce({ data: { success: true } });
    await refreshHotKeywords();
    expect(postMock).toHaveBeenCalledWith('/api/v1/sourcing/hot-keywords/refresh', expect.anything());
    expect(runOptions[0]).toMatchObject({ timeoutMs: 90000 });
  });

  test('查货提交账号与关键词并转换商品字段', /* 当前回调验证查货请求体与响应映射。 */ async () => {
    postMock.mockResolvedValueOnce({ data: { keyword: '耳机', items: [{ item_id: '1', title: '蓝牙耳机', price: '19', want_count: '485', location: '广东', detail_url: 'https://www.goofish.com/item?id=1' }] } });
    // items 是转换后的查货结果。
    const items = await searchItems('acc1', '耳机');
    expect(postMock).toHaveBeenCalledWith('/api/v1/sourcing/search', expect.objectContaining({ body: { cookie_id: 'acc1', keyword: '耳机' } }));
    expect(items).toEqual([{ itemId: '1', title: '蓝牙耳机', price: '19', wantCount: '485', location: '广东', detailUrl: 'https://www.goofish.com/item?id=1' }]);
    expect(runOptions[0]).toMatchObject({ timeoutMs: 90000 });
    postMock.mockResolvedValueOnce({ data: { keyword: '冷门', items: null } });
    await expect(searchItems('acc1', '冷门')).resolves.toEqual([]);
  });

  test('畅销榜按窗口查询并转换条目', /* 当前回调验证查询参数与条目映射。 */ async () => {
    getMock.mockResolvedValueOnce({ data: { date: '2026-10-29', window: '30d', status: 'ok', entries: [{ item_id: 'a', item_title: '商品甲', sold_quantity: 3, revenue: 30.5, order_count: 2 }] } });
    // ranking 是转换后的畅销榜。
    const ranking = await fetchShopRanking('30d');
    expect(getMock).toHaveBeenCalledWith('/api/v1/sourcing/shop-ranking', expect.objectContaining({ params: { query: { window: '30d' } } }));
    expect(ranking).toEqual({ window: '30d', entries: [{ itemId: 'a', itemTitle: '商品甲', soldQuantity: 3, revenue: 30.5, orderCount: 2 }] });
    getMock.mockResolvedValueOnce({ data: { date: '2026-10-29', window: 'weird', status: 'ok', entries: null } });
    await expect(fetchShopRanking('7d')).resolves.toEqual({ window: '7d', entries: [] });
  });

  test('文案生成省略空参考信息并返回正文', /* 当前回调验证请求体与返回值。 */ async () => {
    postMock.mockResolvedValueOnce({ data: { mode: 'listing', content: '标题\n描述' } });
    await expect(generateCopywriting({ mode: 'listing', topic: '耳机', reference: '' })).resolves.toBe('标题\n描述');
    expect(postMock).toHaveBeenCalledWith('/api/v1/sourcing/copywriting', expect.objectContaining({ body: { mode: 'listing', topic: '耳机', reference: undefined } }));
    postMock.mockResolvedValueOnce({ data: { mode: 'analysis', content: '分析' } });
    await generateCopywriting({ mode: 'analysis', topic: '耳机', reference: '19 元' });
    expect(postMock).toHaveBeenLastCalledWith('/api/v1/sourcing/copywriting', expect.objectContaining({ body: { mode: 'analysis', topic: '耳机', reference: '19 元' } }));
  });

  test('账号列表只保留有标识的账号并生成展示名', /* 当前回调验证账号解包、过滤与标签生成。 */ async () => {
    getMock.mockResolvedValueOnce({ data: { data: [{ id: 'acc1', nickname: '小鱼', remark: '' }, { id: 'acc2', nickname: '', remark: '主号' }, { id: 'acc3' }, { nickname: '无标识' }] } });
    await expect(fetchSourcingAccounts()).resolves.toEqual([
      { id: 'acc1', label: '小鱼' },
      { id: 'acc2', label: '主号（acc2）' },
      { id: 'acc3', label: 'acc3' },
    ]);
    expect(getMock).toHaveBeenCalledWith('/api/v1/accounts/details', expect.anything());
  });
});
