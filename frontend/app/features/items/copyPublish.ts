import { useCallback, useEffect, useRef, useState } from 'react';
import type { Item } from './api';
import { createItemCopyTask, previewItemCopy } from './api';
import type { ItemCopyPreview } from './models';

/** ItemCopyPublishOptions 描述跨账号复制 Hook 需要的外部联动。 */
export interface ItemCopyPublishOptions {
  /** onTaskCreated 在复制批次创建成功后触发，接收批次标识用于跳转既有批次进度。 */
  onTaskCreated: (batchID: string) => void;
}

/** ItemCopyPublishState 描述跨账号复制 Hook 暴露的状态与动作。 */
export interface ItemCopyPublishState {
  /** showCopyModal 表示复制弹窗是否打开。 */
  showCopyModal: boolean;
  /** copySource 是当前要复制的源商品。 */
  copySource: Item | null;
  /** copyPreview 是服务端返回的预填内容与目标账号预检。 */
  copyPreview: ItemCopyPreview | null;
  /** copyLoading 表示预览请求是否正在执行。 */
  copyLoading: boolean;
  /** copySubmitting 表示创建任务请求是否正在执行。 */
  copySubmitting: boolean;
  /** copyError 是弹窗内展示的错误说明。 */
  copyError: string;
  /** copyTitle 是可编辑的标题。 */
  copyTitle: string;
  /** setCopyTitle 更新可编辑标题。 */
  setCopyTitle: (value: string) => void;
  /** copyDescription 是可编辑的描述。 */
  copyDescription: string;
  /** setCopyDescription 更新可编辑描述。 */
  setCopyDescription: (value: string) => void;
  /** copyPrice 是可编辑的售价文本。 */
  copyPrice: string;
  /** setCopyPrice 更新可编辑售价。 */
  setCopyPrice: (value: string) => void;
  /** copyIntervalSeconds 是相邻发布的间隔秒数。 */
  copyIntervalSeconds: number;
  /** setCopyIntervalSeconds 更新发布间隔。 */
  setCopyIntervalSeconds: (value: number) => void;
  /** copySelectedTargets 是已勾选的目标账号标识集合。 */
  copySelectedTargets: string[];
  /** toggleCopyTarget 切换一个目标账号的勾选状态。 */
  toggleCopyTarget: (cookieID: string) => void;
  /** openCopyModal 打开复制弹窗并加载预览。 */
  openCopyModal: (item: Item) => void;
  /** closeCopyModal 关闭复制弹窗并清理临时状态。 */
  closeCopyModal: () => void;
  /** submitCopyTask 提交复制任务并触发批次进度跳转。 */
  submitCopyTask: () => Promise<void>;
}

/** useItemCopyPublish 管理跨账号复制发布的弹窗状态、预览加载与任务创建。 */
export const useItemCopyPublish = (options: ItemCopyPublishOptions): ItemCopyPublishState => {
  // showCopyModal 表示复制弹窗是否打开。
  const [showCopyModal, setShowCopyModal] = useState(false);
  // copySource 是当前要复制的源商品。
  const [copySource, setCopySource] = useState<Item | null>(null);
  // copyPreview 是服务端返回的预填内容与目标账号预检。
  const [copyPreview, setCopyPreview] = useState<ItemCopyPreview | null>(null);
  // copyLoading 表示预览请求是否正在执行。
  const [copyLoading, setCopyLoading] = useState(false);
  // copySubmitting 表示创建任务请求是否正在执行。
  const [copySubmitting, setCopySubmitting] = useState(false);
  // copyError 是弹窗内展示的错误说明。
  const [copyError, setCopyError] = useState('');
  // copyTitle 是可编辑的标题。
  const [copyTitle, setCopyTitle] = useState('');
  // copyDescription 是可编辑的描述。
  const [copyDescription, setCopyDescription] = useState('');
  // copyPrice 是可编辑的售价文本。
  const [copyPrice, setCopyPrice] = useState('');
  // copyIntervalSeconds 是相邻发布的间隔秒数，默认 300 秒以尊重平台风控节奏。
  const [copyIntervalSeconds, setCopyIntervalSeconds] = useState(300);
  // copySelectedTargets 是已勾选的目标账号标识集合。
  const [copySelectedTargets, setCopySelectedTargets] = useState<string[]>([]);
  // copyRequestGeneration 标识复制弹窗最新一次请求，旧响应不得覆盖较新的预览或提交结果。
  const copyRequestGeneration = useRef(0);
  // copyRequestController 保存当前请求的取消器；弹窗关闭或新请求时必须中止旧请求。
  const copyRequestController = useRef<AbortController | null>(null);
  // onTaskCreatedRef 保存最新的批次创建成功回调，避免回调变化导致动作重建。
  const onTaskCreatedRef = useRef(options.onTaskCreated);
  onTaskCreatedRef.current = options.onTaskCreated;

  // resetCopyForm 清空弹窗的临时编辑状态。
  const resetCopyForm = useCallback(/* resetAction 清空弹窗的临时编辑状态。 */ () => {
    setCopyPreview(null);
    setCopyError('');
    setCopyTitle('');
    setCopyDescription('');
    setCopyPrice('');
    setCopyIntervalSeconds(300);
    setCopySelectedTargets([]);
  }, []);

  // 组件卸载时中止未完成请求并使所有晚到响应失效。
  useEffect(
    () => (
      /* 卸载清理回调中止网络请求并使晚到响应失效。 */
      () => {
        copyRequestController.current?.abort();
        copyRequestGeneration.current += 1;
      }
    ),
    [],
  );

  // openCopyModal 打开复制弹窗，并立即用源商品发起预览（目标为空时服务端返回全部账号预检）。
  const openCopyModal = useCallback(/* openAction 打开复制弹窗并触发预览。 */ (item: Item) => {
    copyRequestController.current?.abort();
    // controller 是本次预览请求的取消器。
    const controller = new AbortController();
    copyRequestController.current = controller;
    // requestGeneration 是本次预览请求的单调递增代次。
    const requestGeneration = ++copyRequestGeneration.current;
    setCopySource(item);
    setShowCopyModal(true);
    resetCopyForm();
    setCopyLoading(true);
    // 预览请求只读本地快照，不触发任何平台请求。
    previewItemCopy({ cookieId: item.cookie_id, itemId: item.item_id }, { signal: controller.signal })
      .then(/* 收到预览后回填弹窗字段。 */ preview /* preview 表示复制预览结果。 */ => {
        if (requestGeneration !== copyRequestGeneration.current) return;
        setCopyPreview(preview);
        setCopyTitle(preview.title);
        setCopyDescription(preview.description);
        setCopyPrice(preview.price);
      })
      .catch(/* 预览失败时把原因写入弹窗。 */ error /* error 表示预览失败原因。 */ => {
        if (requestGeneration !== copyRequestGeneration.current) return;
        setCopyError(error instanceof Error ? error.message : '加载复制预览失败');
      })
      .finally(/* 收尾时关闭预览加载态。 */ () => {
        if (requestGeneration === copyRequestGeneration.current) setCopyLoading(false);
      });
  }, [resetCopyForm]);

  // closeCopyModal 关闭复制弹窗并中止未完成请求。
  const closeCopyModal = useCallback(/* closeAction 关闭复制弹窗并中止未完成请求。 */ () => {
    copyRequestController.current?.abort();
    copyRequestGeneration.current += 1;
    setShowCopyModal(false);
    setCopySource(null);
    resetCopyForm();
  }, [resetCopyForm]);

  // toggleCopyTarget 切换一个目标账号的勾选状态。
  const toggleCopyTarget = useCallback(/* toggleAction 切换一个目标账号的勾选状态。 */ (cookieID: string) => {
    setCopySelectedTargets(/* 在既有勾选集合上做增删。 */ previous /* previous 表示既有勾选集合。 */ => (
      previous.includes(cookieID) ? previous.filter(/* 保留未命中的标识。 */ id /* id 表示已选标识。 */ => id !== cookieID) : [...previous, cookieID]
    ));
  }, []);

  // submitCopyTask 校验勾选并创建复制任务；成功后关闭弹窗并跳转既有批次进度。
  const submitCopyTask = useCallback(/* submitAction 校验勾选并创建复制任务。 */ async () => {
    // source 是当前源商品。
    const source = copySource;
    if (!source || copySubmitting) return;
    if (copySelectedTargets.length === 0) {
      setCopyError('请选择要复制到的目标账号');
      return;
    }
    copyRequestController.current?.abort();
    // controller 是本次创建请求的取消器。
    const controller = new AbortController();
    copyRequestController.current = controller;
    // requestGeneration 是本次创建请求的单调递增代次。
    const requestGeneration = ++copyRequestGeneration.current;
    setCopySubmitting(true);
    setCopyError('');
    try {
      // result 是服务端创建的批次结果。
      const result = await createItemCopyTask({
        cookieId: source.cookie_id,
        itemId: source.item_id,
        targetCookieIds: copySelectedTargets,
        titleEdit: copyTitle,
        descriptionEdit: copyDescription,
        priceEdit: copyPrice,
        publishIntervalSeconds: copyIntervalSeconds,
      }, { signal: controller.signal });
      if (requestGeneration !== copyRequestGeneration.current) return;
      setShowCopyModal(false);
      setCopySource(null);
      resetCopyForm();
      onTaskCreatedRef.current(result.batch_id);
    } catch (error /* 任务创建错误 */) {
      if (requestGeneration !== copyRequestGeneration.current) return;
      setCopyError(error instanceof Error ? error.message : '创建复制任务失败');
    } finally {
      if (requestGeneration === copyRequestGeneration.current) setCopySubmitting(false);
    }
  }, [copyDescription, copyIntervalSeconds, copyPrice, copySelectedTargets, copySource, copySubmitting, copyTitle, resetCopyForm]);

  return {
    showCopyModal,
    copySource,
    copyPreview,
    copyLoading,
    copySubmitting,
    copyError,
    copyTitle,
    setCopyTitle,
    copyDescription,
    setCopyDescription,
    copyPrice,
    setCopyPrice,
    copyIntervalSeconds,
    setCopyIntervalSeconds,
    copySelectedTargets,
    toggleCopyTarget,
    openCopyModal,
    closeCopyModal,
    submitCopyTask,
  };
};
