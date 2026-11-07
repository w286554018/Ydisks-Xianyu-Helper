// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, test, vi } from 'vitest';
import { analyzeCompetitorSellingPoints, carryCompetitorItem, fetchCompetitorDetail, rewriteCompetitorCopy } from './api';
import { useCompetitorDetail } from './hooks';
import type { CarryResultData, CompetitorDetailData, RewrittenCopyData } from './api';

vi.mock('./api', /* apiModuleMockFactory 用可控替身替换竞品详情、改写、分析与搬运请求。 */ () => ({
  fetchCompetitorDetail: vi.fn(),
  rewriteCompetitorCopy: vi.fn(),
  analyzeCompetitorSellingPoints: vi.fn(),
  carryCompetitorItem: vi.fn(),
}));

// detailMock 是竞品详情请求替身。
const detailMock = vi.mocked(fetchCompetitorDetail);
// rewriteMock 是 AI 改写请求替身。
const rewriteMock = vi.mocked(rewriteCompetitorCopy);
// analyzeMock 是 AI 分析请求替身。
const analyzeMock = vi.mocked(analyzeCompetitorSellingPoints);
// carryMock 是一键搬运请求替身。
const carryMock = vi.mocked(carryCompetitorItem);

/** detailFixture 构造一条竞品详情结果。 */
const detailFixture = (/* itemId 是商品标识。 */ itemId: string): CompetitorDetailData => ({
  itemId, title: 'AI课程代做', price: '99', descText: '专业代做', images: [], sellerNickname: '卖家甲', sellerId: 'u1', cached: false,
});

/** carryFixture 构造一条搬运结果。 */
const carryFixture = (): CarryResultData => ({
  title: '改写标题', desc: '改写描述', price: '99', images: [{ id: 9, filename: 'carry-i1-0.png' }], sameImagesHint: '图片与竞品相同',
});

describe('竞品详情弹窗 Hook', /* 当前回调验证详情抓取、改写、分析、搬运与状态清零。 */ () => {
  beforeEach(/* 当前回调重置全部请求替身。 */ () => {
    detailMock.mockReset();
    rewriteMock.mockReset();
    analyzeMock.mockReset();
    carryMock.mockReset();
  });

  test('打开详情成功后写入详情并结束加载', /* 当前回调验证成功路径与请求参数。 */ async () => {
    detailMock.mockResolvedValueOnce(detailFixture('i1'));
    // result 是被测 Hook 的渲染结果。
    const { result } = renderHook(/* 渲染竞品详情 Hook。 */ () => useCompetitorDetail());
    await act(/* openDetailAction 打开详情。 */ async () => {
      await result.current.openDetail('i1');
    });
    await waitFor(/* 等待详情写入。 */ () => expect(result.current.detail?.itemId).toBe('i1'));
    expect(result.current.detailLoading).toBe(false);
    expect(result.current.detailError).toBe('');
    expect(detailMock).toHaveBeenCalledWith('i1', expect.objectContaining({ signal: expect.anything() }));
  });

  test('打开详情失败写入错误文案', /* 当前回调验证 Error 与非 Error 两类失败。 */ async () => {
    detailMock.mockRejectedValueOnce(new Error('浏览器不可用'));
    // result 是被测 Hook 的渲染结果。
    const { result } = renderHook(/* 渲染竞品详情 Hook。 */ () => useCompetitorDetail());
    await act(/* openDetailAction 打开详情。 */ async () => {
      await result.current.openDetail('i1');
    });
    await waitFor(/* 等待错误写入。 */ () => expect(result.current.detailError).toBe('浏览器不可用'));
    expect(result.current.detail).toBeNull();

    // 非 Error 抛出必须降级为固定文案，避免页面显示 undefined。
    detailMock.mockRejectedValueOnce('boom');
    await act(/* openDetailAction 再次打开详情。 */ async () => {
      await result.current.openDetail('i2');
    });
    await waitFor(/* 等待降级文案写入。 */ () => expect(result.current.detailError).toBe('获取竞品详情失败'));
  });

  test('改写、分析与搬运成功写入各自结果', /* 当前回调验证三个动作的成功路径。 */ async () => {
    rewriteMock.mockResolvedValueOnce({ title: '改写标题', desc: '改写描述' } satisfies RewrittenCopyData);
    analyzeMock.mockResolvedValueOnce('核心卖点：定制化');
    carryMock.mockResolvedValueOnce(carryFixture());
    // result 是被测 Hook 的渲染结果。
    const { result } = renderHook(/* 渲染竞品详情 Hook。 */ () => useCompetitorDetail());
    await act(/* rewriteAction 触发改写。 */ async () => {
      await result.current.rewrite('i1');
    });
    await waitFor(/* 等待改写结果写入。 */ () => expect(result.current.rewritten?.title).toBe('改写标题'));
    // 改写期间与结束后的加载标记必须回落。
    expect(result.current.rewriting).toBe(false);

    await act(/* analyzeAction 触发分析。 */ async () => {
      await result.current.analyze('i1');
    });
    await waitFor(/* 等待分析结果写入。 */ () => expect(result.current.analysis).toBe('核心卖点：定制化'));
    expect(result.current.analyzing).toBe(false);

    // carried 是搬运动作返回的结果。
    let carried: CarryResultData | null = null;
    await act(/* carryAction 触发搬运。 */ async () => {
      carried = await result.current.carry('i1');
    });
    expect(carried).toEqual(carryFixture());
    await waitFor(/* 等待搬运结果写入。 */ () => expect(result.current.carryResult?.images).toHaveLength(1));
    expect(result.current.carrying).toBe(false);
  });

  test('动作失败写入错误并返回空搬运结果', /* 当前回调验证三个动作的失败路径。 */ async () => {
    rewriteMock.mockRejectedValueOnce(new Error('模型未配置'));
    analyzeMock.mockRejectedValueOnce('boom');
    carryMock.mockRejectedValueOnce(new Error('图片入库失败'));
    // result 是被测 Hook 的渲染结果。
    const { result } = renderHook(/* 渲染竞品详情 Hook。 */ () => useCompetitorDetail());

    await act(/* rewriteAction 触发改写失败。 */ async () => {
      await result.current.rewrite('i1');
    });
    await waitFor(/* 等待改写错误写入。 */ () => expect(result.current.detailError).toBe('模型未配置'));

    await act(/* analyzeAction 触发分析失败。 */ async () => {
      await result.current.analyze('i1');
    });
    await waitFor(/* 等待分析降级文案写入。 */ () => expect(result.current.detailError).toBe('AI 分析失败'));

    // carried 是搬运失败时的返回结果，必须为 null。
    let carried: CarryResultData | null = carryFixture();
    await act(/* carryAction 触发搬运失败。 */ async () => {
      carried = await result.current.carry('i1');
    });
    expect(carried).toBeNull();
    await waitFor(/* 等待搬运错误写入。 */ () => expect(result.current.detailError).toBe('图片入库失败'));
  });

  test('关闭与重置清空临时状态', /* 当前回调验证 closeDetail 与 resetCompetitorState 的状态清理。 */ async () => {
    detailMock.mockResolvedValueOnce(detailFixture('i1'));
    rewriteMock.mockResolvedValueOnce({ title: '改写标题', desc: '改写描述' });
    carryMock.mockResolvedValueOnce(carryFixture());
    analyzeMock.mockResolvedValueOnce('核心卖点');
    // result 是被测 Hook 的渲染结果。
    const { result } = renderHook(/* 渲染竞品详情 Hook。 */ () => useCompetitorDetail());
    await act(/* openDetailAction 打开详情。 */ async () => {
      await result.current.openDetail('i1');
    });
    await act(/* 触发改写、分析与搬运。 */ async () => {
      await result.current.rewrite('i1');
      await result.current.analyze('i1');
      await result.current.carry('i1');
    });
    await waitFor(/* 等待三类结果写入。 */ () => expect(result.current.carryResult).not.toBeNull());

    act(/* resetAction 清空临时结果，保留详情。 */ () => {
      result.current.resetCompetitorState();
    });
    expect(result.current.rewritten).toBeNull();
    expect(result.current.analysis).toBe('');
    expect(result.current.carryResult).toBeNull();
    expect(result.current.detail?.itemId).toBe('i1');

    act(/* closeAction 关闭弹窗。 */ () => {
      result.current.closeDetail();
    });
    expect(result.current.detail).toBeNull();
    expect(result.current.detailLoading).toBe(false);
    expect(result.current.detailError).toBe('');
    expect(result.current.rewriting).toBe(false);
    expect(result.current.analyzing).toBe(false);
    expect(result.current.carrying).toBe(false);
  });

  test('卸载时中止未完成的详情请求', /* 当前回调验证取消语义，避免卸载后写入状态。 */ async () => {
    // pending 是永不完成的详情请求，用于观察取消信号。
    const pending = new Promise<CompetitorDetailData>(/* executor 保存完成函数。 */ () => {});
    // result 是被测 Hook 的渲染结果；signals 记录请求携带的取消信号。
    const signals: AbortSignal[] = [];
    detailMock.mockImplementationOnce(/* detailImpl 记录取消信号并返回挂起请求。 */ (_itemId, options) => {
      if (options?.signal) signals.push(options.signal);
      return pending;
    });
    // result 与 unmount 是被测 Hook 的渲染结果与卸载函数。
    const { result, unmount } = renderHook(/* 渲染竞品详情 Hook。 */ () => useCompetitorDetail());
    await act(/* openDetailAction 打开详情并保持挂起。 */ async () => {
      void result.current.openDetail('i1');
    });
    expect(signals[0]?.aborted).toBe(false);
    unmount();
    expect(signals[0]?.aborted).toBe(true);
  });
});
