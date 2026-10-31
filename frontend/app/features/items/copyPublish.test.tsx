// @vitest-environment jsdom
import { act,renderHook,waitFor } from '@testing-library/react';
import { beforeEach,describe,expect,test,vi } from 'vitest';
import type { Item,ItemCopyPreview,ItemCopyTaskResult } from './api';
import { createItemCopyTask,previewItemCopy } from './api';
import { useItemCopyPublish } from './copyPublish';

vi.mock('./api', /* copyApiMockFactory 提供跨账号复制 Hook 的确定性 API 替身。 */ () => ({
  createItemCopyTask: vi.fn(),
  previewItemCopy: vi.fn(),
}));

// createTaskMock 是创建复制任务的可控替身。
const createTaskMock = vi.mocked(createItemCopyTask);
// previewCopyMock 是复制预览的可控替身。
const previewCopyMock = vi.mocked(previewItemCopy);

// sourceFixture 是打开弹窗时使用的源商品。
const sourceFixture = { cookie_id: 'src', item_id: 'i1', item_title: '源商品', item_price: '19.9' } as unknown as Item;
// previewFixture 是预览成功响应，包含一个可用与一个暂停的目标账号。
const previewFixture: ItemCopyPreview = {
  source_cookie_id: 'src', source_item_id: 'i1', title: '源标题', description: '源描述', price: '19.9',
  image_urls: ['https://img.example.com/1.jpg'],
  targets: [
    { cookie_id: 't1', name: '目标一号', usable: true },
    { cookie_id: 't2', name: '目标二号', usable: false, reason: '账号处于暂停期' },
  ],
};
// taskFixture 是创建任务成功响应。
const taskFixture: ItemCopyTaskResult = { batch_id: 'copy-batch-1', row_count: 1 };

describe('useItemCopyPublish', /* 覆盖复制弹窗的预览加载、目标勾选与任务创建。 */ () => {
  // onTaskCreated 记录创建成功后的批次标识回调。
  const onTaskCreated = vi.fn<(batchID: string) => void>();

  beforeEach(/* 当前回调重置复制 API 替身。 */ () => {
    vi.clearAllMocks();
    previewCopyMock.mockResolvedValue(previewFixture);
    createTaskMock.mockResolvedValue(taskFixture);
  });

  // renderHooked 渲染被测 Hook 并返回实例。
  const renderHooked = () => renderHook(() => useItemCopyPublish({ onTaskCreated }));

  test('openCopyModal 加载预览并预填可编辑字段', /* 已完成。 */ async () => {
    // hook 是被测 Hook 实例。
    const hook = renderHooked();
    await act(
      // openAction 打开复制弹窗并触发预览。
      async () => hook.result.current.openCopyModal(sourceFixture),
    );
    expect(hook.result.current.showCopyModal).toBe(true);
    // loaded 等待预览加载完成。
    const loaded = /* loaded 等待预览加载完成。 */ async () => await waitFor(/* 断言预览加载结束。 */ () => expect(hook.result.current.copyLoading).toBe(false));
    await loaded();
    expect(previewCopyMock).toHaveBeenCalledWith({ cookieId: 'src', itemId: 'i1' }, expect.anything());
    expect(hook.result.current.copyPreview?.targets).toHaveLength(2);
    expect(hook.result.current.copyTitle).toBe('源标题');
    expect(hook.result.current.copyDescription).toBe('源描述');
    expect(hook.result.current.copyPrice).toBe('19.9');
    // 预览失败时错误写入弹窗。
    previewCopyMock.mockRejectedValue(new Error('源商品不存在'));
    await act(
      // reopenAction 重新打开弹窗触发失败预览。
      async () => hook.result.current.openCopyModal(sourceFixture),
    );
    await loaded();
    expect(hook.result.current.copyError).toBe('源商品不存在');
  });

  test('submitCopyTask 校验目标、透传编辑字段并回调批次标识', /* 已完成。 */ async () => {
    // hook 是被测 Hook 实例。
    const hook = renderHooked();
    await act(
      // openAction 打开复制弹窗并触发预览。
      async () => hook.result.current.openCopyModal(sourceFixture),
    );
    await waitFor(/* 断言预览加载结束。 */ () => expect(hook.result.current.copyLoading).toBe(false));
    // 未选目标时提交被拦截并提示。
    await act(
      // submitAction 提交复制任务。
      async () => hook.result.current.submitCopyTask(),
    );
    expect(createTaskMock).not.toHaveBeenCalled();
    expect(hook.result.current.copyError).toBe('请选择要复制到的目标账号');
    // 勾选一个可用目标并修改标题与间隔后提交。
    await act(
      // toggleAction 勾选可用目标账号。
      async () => hook.result.current.toggleCopyTarget('t1'),
    );
    await act(
      // titleAction 修改复制标题。
      async () => hook.result.current.setCopyTitle('新标题'),
    );
    await act(
      // intervalAction 修改发布间隔。
      async () => hook.result.current.setCopyIntervalSeconds(120),
    );
    await act(
      // submitAction 提交复制任务。
      async () => hook.result.current.submitCopyTask(),
    );
    expect(createTaskMock).toHaveBeenCalledWith(expect.objectContaining({
      cookieId: 'src', itemId: 'i1', targetCookieIds: ['t1'], titleEdit: '新标题', publishIntervalSeconds: 120,
    }), expect.anything());
    expect(onTaskCreated).toHaveBeenCalledWith('copy-batch-1');
    expect(hook.result.current.showCopyModal).toBe(false);
    // 重新打开后不可用目标也允许进入勾选集合；服务端预检负责拒绝并给出原因。
    await act(
      // reopenAction 重新打开弹窗。
      async () => hook.result.current.openCopyModal(sourceFixture),
    );
    await waitFor(/* 断言预览加载结束。 */ () => expect(hook.result.current.copyLoading).toBe(false));
    await act(
      // toggleAction 勾选暂停中的目标账号。
      async () => hook.result.current.toggleCopyTarget('t2'),
    );
    expect(hook.result.current.copySelectedTargets).toContain('t2');
  });

  test('closeCopyModal 关闭弹窗并清空临时状态', /* 已完成。 */ async () => {
    // hook 是被测 Hook 实例。
    const hook = renderHooked();
    await act(
      // openAction 打开复制弹窗并触发预览。
      async () => hook.result.current.openCopyModal(sourceFixture),
    );
    await waitFor(/* 断言预览加载结束。 */ () => expect(hook.result.current.copyLoading).toBe(false));
    await act(
      // toggleAction 勾选一个目标账号。
      async () => hook.result.current.toggleCopyTarget('t1'),
    );
    await act(
      // closeAction 关闭复制弹窗。
      async () => hook.result.current.closeCopyModal(),
    );
    expect(hook.result.current.showCopyModal).toBe(false);
    expect(hook.result.current.copySource).toBeNull();
    expect(hook.result.current.copySelectedTargets).toEqual([]);
    expect(hook.result.current.copyPreview).toBeNull();
  });
});
