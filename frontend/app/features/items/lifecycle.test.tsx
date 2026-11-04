// @vitest-environment jsdom
import { act,renderHook,waitFor } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import type { Item,ItemLifecycleBatchDetail,ItemLifecycleCreateResult } from './api';
import { cancelItemLifecycleBatch,createItemLifecycleBatch,getItemLifecycleBatch,retryFailedItemLifecycleBatch } from './api';
import { isLifecycleBatchInProgress,requiresDeletePhrase,useItemLifecycle } from './lifecycle';

vi.mock('./api', /* lifecycleApiMockFactory 提供生命周期 Hook 的确定性 API 替身。 */ () => ({
  cancelItemLifecycleBatch: vi.fn(),
  createItemLifecycleBatch: vi.fn(),
  getItemLifecycleBatch: vi.fn(),
  itemErrorMessage: /* messageFallback 与真实实现保持同形的消息兜底。 */ (error: unknown, fallback: string) => (error instanceof Error ? error.message : fallback),
  retryFailedItemLifecycleBatch: vi.fn(),
}));

// createBatchMock 是批次创建的可控替身。
const createBatchMock = vi.mocked(createItemLifecycleBatch);
// getBatchMock 是批次详情的可控替身。
const getBatchMock = vi.mocked(getItemLifecycleBatch);
// cancelBatchMock 是批次取消的可控替身。
const cancelBatchMock = vi.mocked(cancelItemLifecycleBatch);
// retryBatchMock 是失败行重试的可控替身。
const retryBatchMock = vi.mocked(retryFailedItemLifecycleBatch);

// itemFixture 是单件目标商品。
const itemFixture = { cookie_id: 'acc1', item_id: 'i1', item_title: '示例商品' } as unknown as Item;
// runningDetail 是运行中批次详情。
const runningDetail: ItemLifecycleBatchDetail = {
  batch: { id: 'lb1', action: 'downshelf', status: 'running', total_count: 1, success_count: 0, failed_count: 0, created_at: '', updated_at: '' },
  rows: [{ id: 1, row_no: 1, cookie_id: 'acc1', item_id: 'i1', status: 'running' }],
};
// completedDetail 是完成批次详情。
const completedDetail: ItemLifecycleBatchDetail = {
  batch: { id: 'lb1', action: 'downshelf', status: 'completed', total_count: 1, success_count: 1, failed_count: 0, created_at: '', updated_at: '' },
  rows: [{ id: 1, row_no: 1, cookie_id: 'acc1', item_id: 'i1', status: 'success' }],
};
// createdResult 是批次创建响应。
const createdResult: ItemLifecycleCreateResult = { batch_id: 'lb1' };

beforeEach(/* 当前回调重置生命周期 API 替身。 */ () => {
  vi.clearAllMocks();
  createBatchMock.mockResolvedValue(createdResult);
  getBatchMock.mockResolvedValue(runningDetail);
  cancelBatchMock.mockResolvedValue({ accepted: true });
  retryBatchMock.mockResolvedValue({ reset_count: 1 });
});

describe('useItemLifecycle', /* 覆盖确认、批次创建、取消与重试。 */ () => {
  test('requiresDeletePhrase 只对批量删除生效', /* 防呆口令判定。 */ () => {
    expect(requiresDeletePhrase('delete', 1)).toBe(false);
    expect(requiresDeletePhrase('delete', 2)).toBe(true);
    expect(requiresDeletePhrase('downshelf', 5)).toBe(false);
  });

  test('isLifecycleBatchInProgress 只认 pending 与 running', /* 进行中判定。 */ () => {
    expect(isLifecycleBatchInProgress('pending')).toBe(true);
    expect(isLifecycleBatchInProgress('running')).toBe(true);
    expect(isLifecycleBatchInProgress('completed')).toBe(false);
    expect(isLifecycleBatchInProgress(undefined)).toBe(false);
  });

  test('submitLifecycleConfirm 创建批次并打开进度弹窗', /* 成功路径。 */ async () => {
    // loadItems 记录列表刷新回调。
    const loadItems = vi.fn().mockResolvedValue(undefined);
    // hook 是被测 Hook 实例。
    const hook = renderHook(/* lifecycleHookFactory 渲染被测生命周期 Hook。 */ () => useItemLifecycle({ loadItems }));
    act(/* openDownshelfConfirm 打开单件下架确认。 */ () => hook.result.current.openLifecycleConfirm('downshelf', [itemFixture]));
    expect(hook.result.current.confirmRequest?.action).toBe('downshelf');
    await act(async () => hook.result.current.submitLifecycleConfirm());
    await waitFor(/* busySettled 等待批次读取收尾。 */ () => expect(hook.result.current.lifecycleBusy).toBe(false));
    expect(createBatchMock).toHaveBeenCalledWith('downshelf', [{ cookie_id: 'acc1', item_id: 'i1' }]);
    expect(hook.result.current.confirmRequest).toBeNull();
    expect(hook.result.current.lifecycleBatchOpen).toBe(true);
    expect(hook.result.current.lifecycleBatch?.batch.id).toBe('lb1');
    // 空目标集合不允许打开确认弹窗。
    act(/* openEmptyDeleteConfirm 尝试用空集合打开删除确认。 */ () => hook.result.current.openLifecycleConfirm('delete', []));
    expect(hook.result.current.confirmRequest).toBeNull();
  });

  test('submitLifecycleConfirm 把创建失败写回弹窗错误', /* 失败路径。 */ async () => {
    createBatchMock.mockRejectedValue(new Error('已有进行中的下架/删除任务，请等待完成'));
    // hook 是被测 Hook 实例。
    const hook = renderHook(() => useItemLifecycle({ loadItems: vi.fn() }));
    act(/* openDeleteConfirm 打开单件删除确认。 */ () => hook.result.current.openLifecycleConfirm('delete', [itemFixture]));
    await act(async () => hook.result.current.submitLifecycleConfirm());
    await waitFor(/* submittingSettled 等待提交收尾。 */ () => expect(hook.result.current.lifecycleSubmitting).toBe(false));
    expect(hook.result.current.lifecycleError).toContain('已有进行中');
    expect(hook.result.current.lifecycleBatchOpen).toBe(false);
  });

  test('cancelLifecycleBatch 请求取消且不打断轮询状态', /* 取消路径。 */ async () => {
    // loadItems 记录列表刷新回调。
    const loadItems = vi.fn().mockResolvedValue(undefined);
    // hook 是被测 Hook 实例。
    const hook = renderHook(/* cancelHookFactory 渲染取消路径的被测 Hook。 */ () => useItemLifecycle({ loadItems }));
    await act(/* openRunningBatch 打开运行中的批次。 */ async () => hook.result.current.openLifecycleBatchByID('lb1'));
    await waitFor(() => expect(hook.result.current.lifecycleBusy).toBe(false));
    await act(/* cancelAction 执行批次取消。 */ async () => hook.result.current.cancelLifecycleBatch());
    expect(cancelBatchMock).toHaveBeenCalledWith('lb1');
    await act(/* closeAction 关闭批次弹窗。 */ async () => hook.result.current.closeLifecycleBatch());
    expect(hook.result.current.lifecycleBatchOpen).toBe(false);
    expect(hook.result.current.lifecycleBatch).toBeNull();
  });

  test('retryLifecycleFailed 重试后回读批次详情', /* 重试路径。 */ async () => {
    getBatchMock.mockResolvedValueOnce(completedDetail);
    // loadItems 记录列表刷新回调。
    const loadItems = vi.fn().mockResolvedValue(undefined);
    // hook 是被测 Hook 实例。
    const hook = renderHook(/* retryHookFactory 渲染重试路径的被测 Hook。 */ () => useItemLifecycle({ loadItems }));
    await act(/* openCompletedBatch 打开已完成的批次。 */ async () => hook.result.current.openLifecycleBatchByID('lb1'));
    await waitFor(/* completedLoaded 等待完成状态回读。 */ () => expect(hook.result.current.lifecycleBatch?.batch.status).toBe('completed'));
    await act(/* retryAction 执行失败行重试。 */ async () => hook.result.current.retryLifecycleFailed());
    expect(retryBatchMock).toHaveBeenCalledWith('lb1');
    // 重试后详情读取返回运行中状态。
    expect(hook.result.current.lifecycleBatch?.batch.status).toBe('running');
  });
});
