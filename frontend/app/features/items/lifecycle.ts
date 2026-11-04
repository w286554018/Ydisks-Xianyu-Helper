import { useCallback, useEffect, useRef, useState } from 'react';
import type { Item, ItemLifecycleAction, ItemLifecycleBatchDetail } from './api';
import { cancelItemLifecycleBatch, createItemLifecycleBatch, getItemLifecycleBatch, itemErrorMessage, retryFailedItemLifecycleBatch } from './api';

/** LifecycleConfirmRequest 描述一次待确认的下架/删除操作。 */
export interface LifecycleConfirmRequest {
  /** action 是本次生命周期动作：downshelf 或 delete。 */
  action: ItemLifecycleAction;
  /** items 是本次操作的目标商品集合；单件商品也以 1 行批次提交。 */
  items: Item[];
}

/** requiresDeletePhrase 判断删除确认是否需要输入防呆口令；批量删除必须输入「删除」。 */
export const requiresDeletePhrase = (action: ItemLifecycleAction, itemCount: number): boolean =>
  action === 'delete' && itemCount > 1;

/** isLifecycleBatchInProgress 判断生命周期批次是否仍在执行或等待执行。 */
export const isLifecycleBatchInProgress = (status?: string): boolean => status === 'pending' || status === 'running';

/** ItemLifecycleOptions 描述生命周期 Hook 需要的外部联动。 */
export interface ItemLifecycleOptions {
  /** loadItems 在批次结束后刷新商品列表，让下架/删除结果立即反映到页面。 */
  loadItems: () => Promise<void>;
}

/** ItemLifecycleState 暴露生命周期确认弹窗与批次进度的状态和动作。 */
export interface ItemLifecycleState {
  /** confirmRequest 是当前待确认的下架/删除操作；为空表示确认弹窗关闭。 */
  confirmRequest: LifecycleConfirmRequest | null;
  /** lifecycleSubmitting 表示确认提交请求是否正在执行。 */
  lifecycleSubmitting: boolean;
  /** lifecycleError 是确认弹窗内展示的错误说明。 */
  lifecycleError: string;
  /** openLifecycleConfirm 打开指定动作与目标集合的确认弹窗。 */
  openLifecycleConfirm: (action: ItemLifecycleAction, items: Item[]) => void;
  /** closeLifecycleConfirm 关闭确认弹窗并清理临时状态。 */
  closeLifecycleConfirm: () => void;
  /** submitLifecycleConfirm 校验口令、创建批次并打开批次进度弹窗。 */
  submitLifecycleConfirm: () => Promise<void>;
  /** lifecycleBatchOpen 表示批次进度弹窗是否打开。 */
  lifecycleBatchOpen: boolean;
  /** lifecycleBatch 是当前展示批次的服务端详情。 */
  lifecycleBatch: ItemLifecycleBatchDetail | null;
  /** lifecycleBusy 表示批次进度弹窗内动作请求是否正在执行。 */
  lifecycleBusy: boolean;
  /** openLifecycleBatchByID 按批次标识打开进度弹窗并开始轮询。 */
  openLifecycleBatchByID: (batchId: string) => Promise<void>;
  /** closeLifecycleBatch 关闭批次进度弹窗并停止轮询。 */
  closeLifecycleBatch: () => void;
  /** cancelLifecycleBatch 请求取消当前批次并等待状态回写。 */
  cancelLifecycleBatch: () => Promise<void>;
  /** retryLifecycleFailed 重试当前批次内的失败行。 */
  retryLifecycleFailed: () => Promise<void>;
}

/** useItemLifecycle 管理商品下架/删除的确认、批次创建、进度轮询与取消重试。 */
export const useItemLifecycle = (options: ItemLifecycleOptions): ItemLifecycleState => {
  // confirmRequest 是当前待确认的下架/删除操作。
  const [confirmRequest, setConfirmRequest] = useState<LifecycleConfirmRequest | null>(null);
  // lifecycleSubmitting 表示确认提交请求是否正在执行。
  const [lifecycleSubmitting, setLifecycleSubmitting] = useState(false);
  // lifecycleError 是确认弹窗内展示的错误说明。
  const [lifecycleError, setLifecycleError] = useState('');
  // lifecycleBatchOpen 表示批次进度弹窗是否打开。
  const [lifecycleBatchOpen, setLifecycleBatchOpen] = useState(false);
  // lifecycleBatch 是当前展示批次的服务端详情。
  const [lifecycleBatch, setLifecycleBatch] = useState<ItemLifecycleBatchDetail | null>(null);
  // lifecycleBusy 表示批次进度弹窗内动作请求是否正在执行。
  const [lifecycleBusy, setLifecycleBusy] = useState(false);
  // lifecycleRequestGeneration 标识最新一次生命周期请求，晚到响应不得覆盖较新的状态。
  const lifecycleRequestGeneration = useRef(0);
  // lifecycleRequestController 保存当前请求的取消器；新动作或组件卸载时必须中止旧请求。
  const lifecycleRequestController = useRef<AbortController | null>(null);
  // loadItemsRef 保存最新的列表刷新回调，避免回调变化导致动作重建。
  const loadItemsRef = useRef(options.loadItems);
  loadItemsRef.current = options.loadItems;

  // beginLifecycleRequest 取消上一个请求并返回当前动作独占的代次与取消器。
  const beginLifecycleRequest = useCallback(/* beginLifecycleRequest 的回调负责取消旧动作并创建新请求代次。 */ () => {
    lifecycleRequestController.current?.abort();
    // controller 是当前动作独占的 HTTP 取消器。
    const controller = new AbortController();
    lifecycleRequestController.current = controller;
    // requestGeneration 是当前动作的单调递增代次，晚到响应只能在代次匹配时回写状态。
    const requestGeneration = ++lifecycleRequestGeneration.current;
    return { controller, requestGeneration };
  }, []);

  // 组件卸载时中止未完成请求并使所有晚到响应失效。
  useEffect(
    () => (
      /* 卸载清理回调中止网络请求并使晚到响应失效。 */
      () => {
        lifecycleRequestController.current?.abort();
        lifecycleRequestGeneration.current += 1;
      }
    ),
    [],
  );

  // openLifecycleConfirm 打开确认弹窗并清空上一次错误。
  const openLifecycleConfirm = useCallback(/* openConfirmAction 打开下架/删除确认弹窗。 */ (action: ItemLifecycleAction, items: Item[]) => {
    if (items.length === 0) return;
    setConfirmRequest({ action, items });
    setLifecycleError('');
  }, []);

  // closeLifecycleConfirm 关闭确认弹窗并中止未完成提交。
  const closeLifecycleConfirm = useCallback(/* closeConfirmAction 关闭确认弹窗。 */ () => {
    lifecycleRequestController.current?.abort();
    lifecycleRequestGeneration.current += 1;
    setConfirmRequest(null);
    setLifecycleSubmitting(false);
    setLifecycleError('');
  }, []);

  // submitLifecycleConfirm 创建生命周期批次；成功后关闭确认弹窗并打开批次进度。
  const submitLifecycleConfirm = useCallback(/* submitConfirmAction 提交下架/删除批次创建。 */ async () => {
    // request 是当前确认弹窗对应的操作。
    const request = confirmRequest;
    if (!request || lifecycleSubmitting) return;
    beginLifecycleRequest();
    setLifecycleSubmitting(true);
    setLifecycleError('');
    try {
      // result 是服务端创建的批次结果。
      const result = await createItemLifecycleBatch(
        request.action,
        request.items.map(/* target 转换为批次目标商品。 */ item => ({ cookie_id: item.cookie_id, item_id: item.item_id })),
      );
      setConfirmRequest(null);
      setLifecycleSubmitting(false);
      await openLifecycleBatchByIDRef.current(result.batch_id);
    } catch (error /* 批次创建错误 */) {
      setLifecycleError(itemErrorMessage(error, '创建下架/删除任务失败，请稍后重试'));
      setLifecycleSubmitting(false);
    }
  }, [beginLifecycleRequest, confirmRequest, lifecycleSubmitting]);

  // openLifecycleBatchByIDRef 保存按标识打开进度弹窗的最新回调，供确认提交复用且不引入循环依赖。
  const openLifecycleBatchByIDRef = useRef<(batchId: string) => Promise<void>>(async () => {});

  // openLifecycleBatchByID 读取批次详情并打开进度弹窗。
  const openLifecycleBatchByID = useCallback(/* openBatchAction 按标识打开生命周期批次进度。 */ async (batchId: string) => {
    if (!batchId) return;
    // request 是本次批次读取的独占取消器和代次。
    const request = beginLifecycleRequest();
    setLifecycleBusy(true);
    setLifecycleBatchOpen(true);
    try {
      // detail 是该批次的最新服务端状态。
      const detail = await getItemLifecycleBatch(batchId, { signal: request.controller.signal });
      if (request.requestGeneration !== lifecycleRequestGeneration.current) return;
      setLifecycleBatch(detail);
    } catch (error /* 批次读取错误 */) {
      if (request.requestGeneration !== lifecycleRequestGeneration.current) return;
      console.error('加载下架/删除批次进度失败:', error);
      setLifecycleBatch(null);
    } finally {
      if (request.requestGeneration === lifecycleRequestGeneration.current) setLifecycleBusy(false);
    }
  }, [beginLifecycleRequest]);
  openLifecycleBatchByIDRef.current = openLifecycleBatchByID;

  // closeLifecycleBatch 关闭进度弹窗并使轮询与晚到响应失效。
  const closeLifecycleBatch = useCallback(/* closeBatchAction 关闭批次进度弹窗。 */ () => {
    lifecycleRequestController.current?.abort();
    lifecycleRequestGeneration.current += 1;
    setLifecycleBatchOpen(false);
    setLifecycleBatch(null);
  }, []);

  // lifecycleActionGeneration 记录弹窗内取消/重试动作的代次，晚到的动作响应不得覆盖较新的批次状态。
  const lifecycleActionGeneration = useRef(0);

  // cancelLifecycleBatch 请求安全取消当前批次；不打断轮询，由轮询把最终状态带回。
  const cancelLifecycleBatch = useCallback(/* cancelBatchAction 取消当前生命周期批次。 */ async () => {
    // batchID 是当前展示批次的标识。
    const batchID = lifecycleBatch?.batch.id;
    if (!batchID || lifecycleBusy) return;
    // actionGeneration 是本次取消动作的代次。
    const actionGeneration = ++lifecycleActionGeneration.current;
    setLifecycleBusy(true);
    try {
      await cancelItemLifecycleBatch(batchID);
    } catch (error /* 批次取消错误 */) {
      if (actionGeneration === lifecycleActionGeneration.current) {
        console.error('取消下架/删除批次失败:', error);
        alert(itemErrorMessage(error, '取消失败，请稍后重试'));
      }
    } finally {
      if (actionGeneration === lifecycleActionGeneration.current) setLifecycleBusy(false);
    }
  }, [lifecycleBatch?.batch.id, lifecycleBusy]);

  // retryLifecycleFailed 重试当前批次内失败行；完成后回读详情，让轮询效应按新状态重启。
  const retryLifecycleFailed = useCallback(/* retryBatchAction 重试失败行。 */ async () => {
    // batchID 是当前展示批次的标识。
    const batchID = lifecycleBatch?.batch.id;
    if (!batchID || lifecycleBusy) return;
    // actionGeneration 是本次重试动作的代次。
    const actionGeneration = ++lifecycleActionGeneration.current;
    setLifecycleBusy(true);
    try {
      await retryFailedItemLifecycleBatch(batchID);
      // detail 是重试后的最新批次状态。
      const detail = await getItemLifecycleBatch(batchID);
      if (actionGeneration !== lifecycleActionGeneration.current) return;
      setLifecycleBatch(detail);
    } catch (error /* 批次重试错误 */) {
      if (actionGeneration === lifecycleActionGeneration.current) {
        console.error('重试下架/删除批次失败:', error);
        alert(itemErrorMessage(error, '重试失败，请稍后重试'));
      }
    } finally {
      if (actionGeneration === lifecycleActionGeneration.current) setLifecycleBusy(false);
    }
  }, [lifecycleBatch?.batch.id, lifecycleBusy]);

  // 批次进度弹窗打开且批次仍在执行时启动轮询；结束后刷新商品列表反映最新状态。
  useEffect(
    /* lifecyclePollEffect 按批次状态启动或停止进度轮询。 */
    () => {
      if (!lifecycleBatchOpen || !lifecycleBatch?.batch.id || !isLifecycleBatchInProgress(lifecycleBatch.batch.status)) return;
      // requestGeneration 标记本轮轮询生命周期，弹窗关闭时失效。
      const requestGeneration = ++lifecycleRequestGeneration.current;
      // controller 是本轮轮询独占的取消器。
      const controller = new AbortController();
      lifecycleRequestController.current = controller;
      // pollInFlight 防止上一轮请求未结束时重复发起。
      let pollInFlight = false;
      // pollLifecycleBatch 读取批次最新进度并在结束时刷新商品列表。
      const pollLifecycleBatch = async () => {
        if (pollInFlight || controller.signal.aborted) return;
        pollInFlight = true;
        try {
          // detail 是轮询返回的最新批次详情。
          const detail = await getItemLifecycleBatch(lifecycleBatch.batch.id, { signal: controller.signal });
          if (requestGeneration !== lifecycleRequestGeneration.current) return;
          setLifecycleBatch(detail);
          if (!isLifecycleBatchInProgress(detail.batch.status)) await loadItemsRef.current();
        } catch (error /* 轮询读取错误 */) {
          if (!controller.signal.aborted) console.error('刷新下架/删除进度失败:', error);
        } finally {
          pollInFlight = false;
        }
      };
      // timer 是当前批次的轮询计时器。
      const timer = window.setInterval(/* 轮询回调异步读取批次进度。 */ () => void pollLifecycleBatch(), 3000);
      return (
        /* 轮询清理器停止计时器并使未完成响应失效。 */
        () => {
          window.clearInterval(timer);
          controller.abort();
        }
      );
    },
    [lifecycleBatch?.batch.id, lifecycleBatch?.batch.status, lifecycleBatchOpen],
  );

  return {
    confirmRequest,
    lifecycleSubmitting,
    lifecycleError,
    openLifecycleConfirm,
    closeLifecycleConfirm,
    submitLifecycleConfirm,
    lifecycleBatchOpen,
    lifecycleBatch,
    lifecycleBusy,
    openLifecycleBatchByID,
    closeLifecycleBatch,
    cancelLifecycleBatch,
    retryLifecycleFailed,
  };
};
