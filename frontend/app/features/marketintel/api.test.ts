import { beforeEach, describe, expect, test, vi } from 'vitest';

// getMock、postMock、putMock、deleteMock 是市场情报契约请求的共享客户端替身；runOptions 记录每次请求的超时与取消参数。
const { getMock, postMock, putMock, deleteMock, runOptions } = vi.hoisted(/* contractMockFactory 创建契约客户端方法替身。 */ () => ({
  getMock: vi.fn(),
  postMock: vi.fn(),
  putMock: vi.fn(),
  deleteMock: vi.fn(),
  runOptions: [] as unknown[],
}));

// ContractMockResult 是测试契约请求回调返回的最小响应模型。
interface ContractMockResult {
  // data 是契约客户端解码出的成功响应。
  data?: unknown;
}

vi.mock('../../../shared/api-contract/client', /* contractClientModuleMockFactory 提供市场情报 API 适配测试所需的契约依赖。 */ () => ({
  // contractClientMock 只提供本测试覆盖的四个 HTTP 操作。
  contractClient: { GET: getMock, POST: postMock, PUT: putMock, DELETE: deleteMock },
  // runContractRequestMock 记录请求选项并执行 feature adapter 传入的请求回调。
  runContractRequest: /* runContractRequestMock 执行契约请求回调。 */ async (/* execute 是 feature adapter 构造的契约请求动作。 */ execute: (signal: AbortSignal) => Promise<ContractMockResult>, /* options 是请求超时与取消参数。 */ options?: unknown) => {
    runOptions.push(options);
    return (await execute(new AbortController().signal)).data;
  },
}));

import {
  analyzeCompetitorSellingPoints,
  carryCompetitorItem,
  createKeyword,
  deleteKeyword,
  fetchCompetitorDetail,
  fetchKeywordItems,
  fetchKeywordTrend,
  fetchKeywords,
  fetchMomentum,
  fetchOverview,
  fetchSellerBoard,
  rewriteCompetitorCopy,
  triggerScan,
  updateKeyword,
} from './api';

describe('市场情报 API 适配', /* 当前回调验证传输 DTO 到页面模型的转换与请求参数。 */ () => {
  beforeEach(/* 当前回调重置契约替身与请求选项记录。 */ () => {
    getMock.mockReset();
    postMock.mockReset();
    putMock.mockReset();
    deleteMock.mockReset();
    runOptions.length = 0;
  });

  test('涨幅榜归一状态并省略缺失字段', /* 当前回调验证状态枚举归一与空日期兜底。 */ async () => {
    getMock.mockResolvedValueOnce({
      data: {
        window_days: 14,
        latest_date: null,
        items: [{ keyword: 'AI课程', status: 'rising', position: 3, previous_position: 8, days_on_list: 5 }],
      },
    });
    await expect(fetchMomentum()).resolves.toEqual({
      windowDays: 14,
      latestDate: '',
      items: [{ keyword: 'AI课程', status: 'rising', position: 3, previousPosition: 8, daysOnList: 5 }],
    });
    // 未知状态必须降级为 steady，避免页面出现未定义样式。
    getMock.mockResolvedValueOnce({ data: { window_days: 14, latest_date: '2026-11-07', items: [{ keyword: 'x', status: 'unknown' }] } });
    await expect(fetchMomentum()).resolves.toMatchObject({ items: [{ status: 'steady' }] });
    // 词条缺失时返回空数组而不是 undefined。
    getMock.mockResolvedValueOnce({ data: { window_days: 14, latest_date: '2026-11-07', items: null } });
    await expect(fetchMomentum()).resolves.toMatchObject({ items: [] });
    expect(getMock).toHaveBeenCalledWith('/api/v1/sourcing/hot-keywords/momentum', expect.anything());
  });

  test('关注词列表与新增归一字段', /* 当前回调验证词表读取、上限与创建结果映射。 */ async () => {
    getMock.mockResolvedValueOnce({ data: { items: [{ id: 1, keyword: 'AI课程', enabled: true, sort_order: 0 }], max_enabled: 30 } });
    await expect(fetchKeywords()).resolves.toEqual({ items: [{ id: 1, keyword: 'AI课程', enabled: true, sortOrder: 0 }], maxEnabled: 30 });
    getMock.mockResolvedValueOnce({ data: { items: null, max_enabled: 30 } });
    await expect(fetchKeywords()).resolves.toEqual({ items: [], maxEnabled: 30 });

    postMock.mockResolvedValueOnce({ data: { id: 2, keyword: 'AI绘画', enabled: false, sort_order: 1 } });
    await expect(createKeyword('AI绘画', false)).resolves.toEqual({ id: 2, keyword: 'AI绘画', enabled: false, sortOrder: 1 });
    expect(postMock).toHaveBeenCalledWith('/api/v1/market-intel/keywords', expect.objectContaining({ body: { keyword: 'AI绘画', enabled: false } }));
  });

  test('更新与删除关注词使用路径参数', /* 当前回调验证 PUT/DELETE 路径与请求体。 */ async () => {
    putMock.mockResolvedValueOnce({ data: undefined });
    await expect(updateKeyword(7, 'AI写作', true)).resolves.toBeUndefined();
    expect(putMock).toHaveBeenCalledWith('/api/v1/market-intel/keywords/{id}', expect.objectContaining({ params: { path: { id: '7' } }, body: { keyword: 'AI写作', enabled: true } }));

    deleteMock.mockResolvedValueOnce({ data: undefined });
    await expect(deleteKeyword(7)).resolves.toBeUndefined();
    expect(deleteMock).toHaveBeenCalledWith('/api/v1/market-intel/keywords/{id}', expect.objectContaining({ params: { path: { id: '7' } } }));
  });

  test('手动扫描使用长超时并透传失败词数', /* 当前回调验证扫描请求路径、超时与结果字段。 */ async () => {
    postMock.mockResolvedValueOnce({ data: { date: '2026-11-07', scanned: 5, skipped: 1, failed_words: 2, verdict_failed: 1, stopped_reason: '风控中止' } });
    await expect(triggerScan()).resolves.toEqual({ date: '2026-11-07', scanned: 5, skipped: 1, failedWords: 2, verdictFailed: 1, stoppedReason: '风控中止' });
    expect(postMock).toHaveBeenCalledWith('/api/v1/market-intel/scan', expect.objectContaining({ body: {} }));
    expect(runOptions[0]).toMatchObject({ timeoutMs: 600000 });
  });

  test('概览归一涨幅与聚合摘要', /* 当前回调验证概览嵌套结构与判定状态归一。 */ async () => {
    getMock.mockResolvedValueOnce({
      data: {
        momentum: { window_days: 14, latest_date: null, items: [{ keyword: 'AI课程', status: 'new', position: 1, previous_position: 0, days_on_list: 1 }] },
        keywords: [
          { keyword: 'AI课程', date: '2026-11-07', total_count: 30, ai_count: 12, total_want: 4560, verdict_status: 'done' },
          { keyword: 'AI绘画', date: '2026-11-07', total_count: 10, ai_count: 0, total_want: 20, verdict_status: 'weird' },
        ],
      },
    });
    // overview 是归一后的概览数据。
    const overview = await fetchOverview();
    expect(overview.momentum).toEqual({ windowDays: 14, latestDate: '', items: [{ keyword: 'AI课程', status: 'new', position: 1, previousPosition: 0, daysOnList: 1 }] });
    expect(overview.keywords).toEqual([
      { keyword: 'AI课程', date: '2026-11-07', totalCount: 30, aiCount: 12, totalWant: 4560, verdictStatus: 'done' },
      { keyword: 'AI绘画', date: '2026-11-07', totalCount: 10, aiCount: 0, totalWant: 20, verdictStatus: 'pending' },
    ]);
    expect(getMock).toHaveBeenCalledWith('/api/v1/market-intel/overview', expect.anything());
  });

  test('趋势与卖家榜转换查询参数与条目', /* 当前回调验证趋势点与卖家统计映射。 */ async () => {
    getMock.mockResolvedValueOnce({
      data: {
        keyword_id: 3, keyword: 'AI课程', days: 30,
        points: [{ date: '2026-11-06', total_want: 100, ai_count: 4, total_count: 10 }],
      },
    });
    await expect(fetchKeywordTrend(3, 30)).resolves.toEqual({
      keywordId: 3, keyword: 'AI课程', days: 30,
      points: [{ date: '2026-11-06', totalWant: 100, aiCount: 4, totalCount: 10 }],
    });
    expect(getMock).toHaveBeenCalledWith('/api/v1/market-intel/keywords/{id}/trend', expect.objectContaining({ params: { path: { id: '3' }, query: { days: 30 } } }));
    getMock.mockResolvedValueOnce({ data: { keyword_id: 3, keyword: 'AI课程', days: 30, points: null } });
    await expect(fetchKeywordTrend(3, 30)).resolves.toMatchObject({ points: [] });

    getMock.mockResolvedValueOnce({ data: { days: 30, sellers: [{ nickname: '卖家甲', appearances: 5, avg_price: null, sample_url: 'https://www.goofish.com/item?id=1' }] } });
    await expect(fetchSellerBoard(30)).resolves.toEqual({
      days: 30,
      sellers: [{ nickname: '卖家甲', appearances: 5, avgPrice: '', sampleUrl: 'https://www.goofish.com/item?id=1' }],
    });
    getMock.mockResolvedValueOnce({ data: { days: 30, sellers: null } });
    await expect(fetchSellerBoard(30)).resolves.toEqual({ days: 30, sellers: [] });
  });

  test('竞品详情使用抓取超时并归一图片', /* 当前回调验证详情请求路径、超时与空图片兜底。 */ async () => {
    getMock.mockResolvedValueOnce({
      data: {
        item_id: 'i1', title: 'AI课程代做', price: '99', desc_text: '专业代做',
        images: null, seller_nickname: '卖家甲', seller_id: 'u1', cached: true,
      },
    });
    await expect(fetchCompetitorDetail('i1')).resolves.toEqual({
      itemId: 'i1', title: 'AI课程代做', price: '99', descText: '专业代做',
      images: [], sellerNickname: '卖家甲', sellerId: 'u1', cached: true,
    });
    expect(getMock).toHaveBeenCalledWith('/api/v1/market-intel/items/{item_id}/detail', expect.objectContaining({ params: { path: { item_id: 'i1' } } }));
    expect(runOptions[0]).toMatchObject({ timeoutMs: 90000 });
  });

  test('改写、分析与搬运返回预填数据', /* 当前回调验证三个动作端点的请求体与结果映射。 */ async () => {
    postMock.mockResolvedValueOnce({ data: { title: '改写标题', desc: '改写描述' } });
    await expect(rewriteCompetitorCopy('i1')).resolves.toEqual({ title: '改写标题', desc: '改写描述' });
    expect(postMock).toHaveBeenCalledWith('/api/v1/market-intel/items/{item_id}/rewrite', expect.objectContaining({ params: { path: { item_id: 'i1' } }, body: {} }));

    postMock.mockResolvedValueOnce({ data: { analysis: '核心卖点：定制化' } });
    await expect(analyzeCompetitorSellingPoints('i1')).resolves.toBe('核心卖点：定制化');
    expect(postMock).toHaveBeenCalledWith('/api/v1/market-intel/items/{item_id}/analyze', expect.anything());

    postMock.mockResolvedValueOnce({
      data: { title: '改写标题', desc: '改写描述', price: '99', images: [{ id: 9, filename: 'carry-i1-0.png' }], same_images_hint: '图片与竞品相同' },
    });
    await expect(carryCompetitorItem('i1')).resolves.toEqual({
      title: '改写标题', desc: '改写描述', price: '99',
      images: [{ id: 9, filename: 'carry-i1-0.png' }], sameImagesHint: '图片与竞品相同',
    });
    // 无图片可入库时返回空数组而不是 undefined。
    postMock.mockResolvedValueOnce({ data: { title: 't', desc: 'd', price: '1', images: null, same_images_hint: '' } });
    await expect(carryCompetitorItem('i1')).resolves.toMatchObject({ images: [] });
    expect(runOptions[1]).toMatchObject({ timeoutMs: 90000 });
  });

  test('词商品列表归一快照商品', /* 当前回调验证关键词商品列表的路径与条目映射。 */ async () => {
    getMock.mockResolvedValueOnce({
      data: {
        keyword: 'AI课程',
        items: [{ item_id: 'i1', title: 'AI课程代做', price: '99', want_count_text: '120人想要', seller_nickname: '卖家甲' }],
      },
    });
    await expect(fetchKeywordItems('AI课程')).resolves.toEqual([
      { itemId: 'i1', title: 'AI课程代做', price: '99', wantCountText: '120人想要', sellerNickname: '卖家甲' },
    ]);
    expect(getMock).toHaveBeenCalledWith('/api/v1/market-intel/keywords/{keyword}/items', expect.objectContaining({ params: { path: { keyword: 'AI课程' } } }));
    getMock.mockResolvedValueOnce({ data: { keyword: 'AI课程', items: null } });
    await expect(fetchKeywordItems('AI课程')).resolves.toEqual([]);
  });
});
