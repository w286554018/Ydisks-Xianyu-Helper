import { useCallback,useEffect,useRef,useState,type Dispatch,type SetStateAction } from 'react';
import {
cancelItemPublishBatch,
deleteItemPublishBatch,
getItemPublishBatch,
getItemPublishBatches,
previewItemPublishBatch,
recommendPublishCategory,
retryFailedItemPublishBatch,
startItemPublishBatch,
} from './api';
import { refreshItemStats,refreshStatsBatch,statsRefreshErrorMessage } from './api';
import { applyStatsToItems,batchRefreshMessage,statsRowKey } from './stats';
import { canRetryBatch,canStartBatch,isBatchInProgress,isCurrentBatchRequest,selectActivePublishBatch } from './batchState';
import type {
BatchFallbackCategory,
BatchPhase,
ItemPublishBatchOptions,
ItemPublishBatchState,
PublishBatchDetail,
PublishBatchPreview,
} from './types';
import type { Item } from './models';

// useItemPublishBatch 集中管理批量铺货的表单、任务恢复、轮询和重试状态。
export const useItemPublishBatch = (options: ItemPublishBatchOptions): ItemPublishBatchState => {
  // showBatchModal 表示批量铺货弹窗是否打开。
  const [showBatchModal, setShowBatchModal] = useState(false);
  // batchPhase 表示批量流程当前所处步骤。
  const [batchPhase, setBatchPhase] = useState<BatchPhase>('upload');
  // batchFile 保存待预检的商品表格文件。
  const [batchFile, setBatchFile] = useState<File | null>(null);
  // batchImagesZip 保存可选的商品图片压缩包。
  const [batchImagesZip, setBatchImagesZip] = useState<File | null>(null);
  // batchCategoryKeyword 保存默认类目搜索词。
  const [batchCategoryKeyword, setBatchCategoryKeyword] = useState('');
  // batchCategoryLoading 表示默认类目推荐请求是否正在执行。
  const [batchCategoryLoading, setBatchCategoryLoading] = useState(false);
  // batchFallbackCategory 保存默认类目配置。
  const [batchFallbackCategory, setBatchFallbackCategory] = useState<BatchFallbackCategory>({ catId: '', catName: '', channelCatId: '', tbCatId: '' });
  // batchPreview 保存最近一次批量预检结果。
  const [batchPreview, setBatchPreview] = useState<PublishBatchPreview | null>(null);
  // batchDetail 保存当前批量任务的服务端详情。
  const [batchDetail, setBatchDetail] = useState<PublishBatchDetail | null>(null);
  // recentBatch 保存页面入口处展示的最近任务结果。
  const [recentBatch, setRecentBatch] = useState<PublishBatchDetail | null>(null);
  // batchLocations 保存批量任务可选的发货地列表。
  const [batchLocations, setBatchLocations] = useState<NonNullable<ItemPublishBatchState['batchLocations']>>([]);
  // batchLocation 保存批量任务当前选中的发货地。
  const [batchLocation, setBatchLocation] = useState<ItemPublishBatchState['batchLocation']>(null);
  // batchPublishIntervalSeconds 保存当前批量任务的最终发布最小间隔，默认五秒。
  const [batchPublishIntervalSeconds, setBatchPublishIntervalSeconds] = useState(5);
  // batchLoading 表示批量任务请求是否正在执行。
  const [batchLoading, setBatchLoading] = useState(false);
  // batchRequestGeneration 用于丢弃弹窗关闭后返回的过期轮询响应。
  const batchRequestGeneration = useRef(0);
  // batchRequestController 保存当前用户触发请求的取消器；新动作或组件卸载时必须中止旧请求。
  const batchRequestController = useRef<AbortController | null>(null);

  // beginBatchRequest 取消上一个用户动作并返回当前动作独占的代次与取消器。
  const beginBatchRequest = useCallback(/* beginBatchRequest 的回调负责取消旧动作并创建新请求代次。 */ () => {
    batchRequestController.current?.abort();
    // controller 是当前用户动作独占的 HTTP 取消器。
    const controller = new AbortController();
    batchRequestController.current = controller;
    // requestGeneration 是当前动作的单调递增代次，晚到响应只能在代次仍匹配时回写状态。
    const requestGeneration = ++batchRequestGeneration.current;
    return { controller, requestGeneration };
  }, []);

  // isCurrentBatchOperation 判断给定用户动作是否仍是当前弹窗会话的最新请求。
  const isCurrentBatchOperation = useCallback(/* isCurrentBatchOperation 的回调负责验证异步响应是否仍归属当前动作。 */ (requestGeneration: number, controller: AbortController) => (
    requestGeneration === batchRequestGeneration.current && controller === batchRequestController.current && !controller.signal.aborted
  ), []);

  useEffect(
    // 组件卸载时中止未完成请求并使所有尚未返回的操作响应失效。
    () => (
      /* 卸载清理回调中止网络请求并使晚到响应失效。 */
      () => {
      batchRequestController.current?.abort();
      batchRequestGeneration.current += 1;
      }
    ),
    [],
  );

  // openBatchModal 初始化上传表单，并恢复仍在执行的批量任务。
  const openBatchModal = useCallback(
    // 批量弹窗打开器负责初始化临时表单和恢复任务。
    async () => {
    // request 是本次打开弹窗及恢复任务读取的独占请求控制器。
    const request = beginBatchRequest();
    setBatchPhase('upload');
    setBatchPreview(null);
    setBatchDetail(null);
    setBatchFile(null);
    setBatchImagesZip(null);
    setBatchCategoryKeyword('');
    setBatchFallbackCategory({ catId: '', catName: '', channelCatId: '', tbCatId: '' });
    setBatchLocations([]);
    setBatchLocation(null);
    setBatchPublishIntervalSeconds(5);
    setShowBatchModal(true);
    setBatchLoading(true);
    try {
      // batches 是最近批量任务摘要，用于寻找可恢复任务。
      const batches = await getItemPublishBatches(20, { signal: request.controller.signal });
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      // recoverable 是仍处于运行或安全取消阶段的任务。
      const recoverable = selectActivePublishBatch(batches);
      if (recoverable?.id) {
        // detail 是可恢复任务的完整详情。
        const detail = await getItemPublishBatch(recoverable.id, { signal: request.controller.signal });
        if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
        setRecentBatch(detail);
        setBatchDetail(detail);
        setBatchPublishIntervalSeconds(detail.publish_interval_seconds || 5);
        setBatchPhase(isBatchInProgress(detail.status) ? 'running' : 'done');
      }
    } catch (error /* 恢复任务错误 */) {
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      console.error('恢复最近批量铺货任务失败:', error);
    } finally {
      if (isCurrentBatchOperation(request.requestGeneration, request.controller)) setBatchLoading(false);
    }
    },
    [beginBatchRequest, isCurrentBatchOperation],
  );

  // handleRecommendBatchCategory 请求默认发布账号对应的推荐类目。
  const handleRecommendBatchCategory = useCallback(
    // 类目推荐动作响应用户点击或回车提交。
    async () => {
    // keyword 是去除空白后的类目搜索词。
    const keyword = batchCategoryKeyword.trim();
    if (!options.selectedAccount) {
      alert('请先选择默认发布账号');
      return;
    }
    if (!keyword) {
      alert('请输入类目关键词');
      return;
    }
    // request 是本次类目推荐请求的独占取消器和代次。
    const request = beginBatchRequest();
    setBatchCategoryLoading(true);
    try {
      // result 是类目推荐接口返回的具名响应。
      const result = await recommendPublishCategory(options.selectedAccount, keyword, { signal: request.controller.signal });
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      // category 是后端返回的推荐类目。
      const category = result.category;
      setBatchFallbackCategory({
        catId: category.cat_id,
        catName: category.cat_name,
        channelCatId: category.channel_cat_id,
        tbCatId: category.tb_cat_id || '',
      });
    } catch (error: any /* 推荐类目错误 */) {
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      console.error('获取推荐类目失败:', error);
      alert(error?.message || '没有匹配到类目，请换一个更具体的关键词');
    } finally {
      if (isCurrentBatchOperation(request.requestGeneration, request.controller)) setBatchCategoryLoading(false);
    }
    },
    [batchCategoryKeyword, beginBatchRequest, isCurrentBatchOperation, options.selectedAccount],
  );

  // openBatchResultByID 按批次标识加载任务详情并打开批次结果弹窗，供跨账号复制创建成功后复用。
  const openBatchResultByID = useCallback(
    // 按 ID 打开器只读取已存在的批量任务。
    async (batchID: string) => {
    if (!batchID) return;
    // request 是本次批次读取的独占取消器和代次。
    const request = beginBatchRequest();
    setBatchLoading(true);
    setShowBatchModal(true);
    try {
      // detail 是该批次的最新服务端状态。
      const detail = await getItemPublishBatch(batchID, { signal: request.controller.signal });
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      setBatchDetail(detail);
      setBatchPublishIntervalSeconds(detail.publish_interval_seconds || 5);
      setBatchPhase(isBatchInProgress(detail.status) ? 'running' : 'done');
      setRecentBatch(detail);
    } catch (error /* 批次详情读取错误 */) {
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      console.error('加载批量铺货结果失败:', error);
    } finally {
      if (isCurrentBatchOperation(request.requestGeneration, request.controller)) setBatchLoading(false);
    }
    },
    [beginBatchRequest, isCurrentBatchOperation, setRecentBatch],
  );

  // openRecentBatchResult 加载最近批量任务详情并打开结果弹窗。
  const openRecentBatchResult = useCallback(
    async () => { /* 打开动作无参数。 */
    if (!recentBatch?.id) return;
    // request 是本次最近结果读取的独占取消器和代次。
    const request = beginBatchRequest();
    setBatchLoading(true);
    setShowBatchModal(true);
    try {
      // detail 是最近任务的最新服务端状态。
      const detail = await getItemPublishBatch(recentBatch.id, { signal: request.controller.signal });
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      setBatchDetail(detail);
      setBatchPublishIntervalSeconds(detail.publish_interval_seconds || 5);
      setBatchPhase(isBatchInProgress(detail.status) ? 'running' : 'done');
    } catch (error /* 最近结果读取错误 */) {
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      console.error('加载最近批量铺货结果失败:', error);
    } finally {
      if (isCurrentBatchOperation(request.requestGeneration, request.controller)) setBatchLoading(false);
    }
    },
    [beginBatchRequest, isCurrentBatchOperation, recentBatch?.id],
  );

  // handlePreviewBatch 上传表格并执行批量预检。
  const handlePreviewBatch = useCallback(
    // 批量预检动作提交上传文件和默认配置。
    async () => {
    if (!batchFile) {
      alert('请先上传商品表格');
      return;
    }
    if (!options.selectedAccount) {
      alert('请先选择默认发布账号');
      return;
    }
    // request 是本次批量预检上传的独占取消器和代次。
    const request = beginBatchRequest();
    setBatchLoading(true);
    try {
      // result 是批量预检接口返回的行级校验结果。
      const result = await previewItemPublishBatch({
        file: batchFile,
        imagesZip: batchImagesZip,
        defaultCookieId: options.selectedAccount,
        fallbackCategory: batchFallbackCategory,
        location: batchLocation || undefined,
        publishIntervalSeconds: batchPublishIntervalSeconds,
      }, { signal: request.controller.signal });
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      setBatchPreview(result);
      setBatchDetail(null);
      setBatchPhase('preview');
    } catch (error: any /* 预检失败错误 */) {
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      console.error('批量铺货预检失败:', error);
      alert(error?.message || '预检失败，请检查表格和图片 zip');
    } finally {
      if (isCurrentBatchOperation(request.requestGeneration, request.controller)) setBatchLoading(false);
    }
    },
    [batchFallbackCategory, batchFile, batchImagesZip, batchLocation, batchPublishIntervalSeconds, beginBatchRequest, isCurrentBatchOperation, options.selectedAccount],
  );

  // handleStartBatch 启动预检通过的批量任务并读取首个详情。
  const handleStartBatch = useCallback(
    // 批量启动动作只允许预检通过的有效行进入执行阶段。
    async () => {
    if (!batchPreview?.preview_id) return;
    if (!canStartBatch(batchPreview)) {
      alert('没有可发布的商品行');
      return;
    }
    // request 是本次启动任务及读取首个详情的独占取消器和代次。
    const request = beginBatchRequest();
    setBatchLoading(true);
    try {
      // started 是批量任务启动接口返回的任务标识。
      const started = await startItemPublishBatch(batchPreview.preview_id, { signal: request.controller.signal });
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      // detail 是启动后用于驱动轮询的任务详情。
      const detail = await getItemPublishBatch(started.batch_id || batchPreview.preview_id, { signal: request.controller.signal });
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      setBatchDetail(detail);
      setRecentBatch(detail);
      setBatchPhase(detail.status === 'running' ? 'running' : 'done');
    } catch (error: any /* 启动失败错误 */) {
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      console.error('启动批量铺货失败:', error);
      alert(error?.message || '启动发布任务失败');
    } finally {
      if (isCurrentBatchOperation(request.requestGeneration, request.controller)) setBatchLoading(false);
    }
    },
    [batchPreview, beginBatchRequest, isCurrentBatchOperation],
  );

  // handleCancelBatch 请求安全取消当前批量任务，并保留远端收尾状态。
  const handleCancelBatch = useCallback(
    // 批量取消动作遵循后端的安全取消语义。
    async () => {
    if (!batchDetail?.id) return;
    if (!confirm('确认取消当前批量铺货任务吗？正在发布的单个商品可能会继续完成。')) return;
    // request 是本次安全取消和状态回读的独占取消器和代次。
    const request = beginBatchRequest();
    setBatchLoading(true);
    try {
      // result 是取消请求返回的过渡状态。
      const result = await cancelItemPublishBatch(batchDetail.id, { signal: request.controller.signal });
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      // detail 是取消请求后的最新任务状态。
      const detail = await getItemPublishBatch(batchDetail.id, { signal: request.controller.signal });
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      setBatchDetail(detail);
      setBatchPhase(result?.status === 'canceling' || detail.status === 'canceling' ? 'running' : 'done');
    } catch (error: any /* 取消失败错误 */) {
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      alert(error?.message || '取消失败');
    } finally {
      if (isCurrentBatchOperation(request.requestGeneration, request.controller)) setBatchLoading(false);
    }
    },
    [batchDetail?.id, beginBatchRequest, isCurrentBatchOperation],
  );

  // abandonBatchPreview 删除未启动的预检任务并恢复上传步骤。
  const abandonBatchPreview = useCallback(
    // 预检放弃动作删除未启动任务并恢复上传步骤。
    async () => {
    // previewId 是当前临时预检任务标识。
    const previewId = batchPreview?.preview_id;
    // request 是本次预检清理操作的独占取消器和代次。
    const request = beginBatchRequest();
    if (previewId && batchPhase === 'preview') {
      try {
        await deleteItemPublishBatch(previewId, { signal: request.controller.signal });
      } catch (error /* 删除预检错误 */) {
        if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
        console.error('清理批量铺货预检失败:', error);
      }
    }
    if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
    setBatchPreview(null);
    setBatchPhase('upload');
    },
    [batchPhase, batchPreview?.preview_id, beginBatchRequest, isCurrentBatchOperation],
  );

  // closeBatchModal 清理临时预检后关闭批量弹窗。
  const closeBatchModal = useCallback(
    // 批量弹窗关闭动作先清理临时预检任务。
    async () => {
    // previewId 是关闭前可安全删除的未启动预检任务标识。
    const previewId = batchPreview?.preview_id;
    // batchRequestGeneration 在关闭时递增，确保所有弹窗动作和轮询响应立即失效。
    batchRequestController.current?.abort();
    batchRequestGeneration.current += 1;
    setBatchPreview(null);
    setBatchPhase('upload');
    setShowBatchModal(false);
    if (previewId && batchPhase === 'preview') {
      try {
        await deleteItemPublishBatch(previewId);
      } catch (error /* 关闭时预检清理错误 */) {
        console.error('关闭批量铺货弹窗时清理预检失败:', error);
      }
    }
    },
    [batchPhase, batchPreview?.preview_id],
  );

  // handleRetryBatchFailed 重新执行当前批次中可重试的失败行。
  const handleRetryBatchFailed = useCallback(
    // 失败重试动作只提交后端允许重试的行。
    async () => {
    if (!batchDetail?.id || !canRetryBatch(batchDetail)) return;
    // request 是本次失败行重试和详情读取的独占取消器和代次。
    const request = beginBatchRequest();
    setBatchLoading(true);
    try {
      await retryFailedItemPublishBatch(batchDetail.id, { signal: request.controller.signal });
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      // detail 是重试请求返回后读取的权威任务详情。
      const detail = await getItemPublishBatch(batchDetail.id, { signal: request.controller.signal });
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      setBatchDetail(detail);
      setBatchPhase('running');
    } catch (error: any /* 重试失败错误 */) {
      if (!isCurrentBatchOperation(request.requestGeneration, request.controller)) return;
      alert(error?.message || '重试失败');
    } finally {
      if (isCurrentBatchOperation(request.requestGeneration, request.controller)) setBatchLoading(false);
    }
    },
    [batchDetail, beginBatchRequest, isCurrentBatchOperation],
  );

  useEffect(
    // 批量轮询副作用只在弹窗展示运行任务时启动。
    () => {
      if (!showBatchModal || !batchDetail?.id || !isBatchInProgress(batchDetail.status)) return;
      // requestGeneration 标记本次轮询生命周期，弹窗关闭时会失效。
      const requestGeneration = ++batchRequestGeneration.current;
      // controller 是本轮弹窗会话独占的轮询取消器；关闭、切换批次或卸载时必须中止未完成读取。
      const controller = new AbortController();
      // pollInFlight 只保护当前批次会话，旧请求不会阻塞重新打开后的新批次轮询。
      let pollInFlight = false;
      // pollBatch 读取任务最新进度并在结束后刷新商品和规则列表。
      const pollBatch = async () => {
        if (pollInFlight || controller.signal.aborted) return;
        pollInFlight = true;
        try {
          // detail 是轮询返回的最新任务详情。
          const detail = await getItemPublishBatch(batchDetail.id, { signal: controller.signal });
          if (!isCurrentBatchRequest(requestGeneration, batchRequestGeneration.current)) return;
          setBatchDetail(detail);
          setRecentBatch(detail);
          if (!isBatchInProgress(detail.status)) {
            setBatchPhase('done');
            await Promise.all([options.loadItems(), options.loadShippingRules()]);
          }
        } catch (error /* 轮询读取错误 */) {
          if (!controller.signal.aborted) console.error('刷新批量铺货进度失败:', error);
        } finally {
          pollInFlight = false;
        }
      };
      // timer 是当前批次的轮询计时器。
      const timer = window.setInterval(
        // 轮询回调异步读取任务进度。
        () => void pollBatch(),
        3000,
      );
      return (
        // 轮询清理器停止计时器并使未完成响应失效。
        () => {
        window.clearInterval(timer);
        controller.abort();
        batchRequestGeneration.current += 1;
        }
      );
    },
    [batchDetail?.id, batchDetail?.status, options.loadItems, options.loadShippingRules, showBatchModal],
  );

  // result 汇总批量状态、更新器和流程动作，保持页面只消费 feature 边界。
  const result: ItemPublishBatchState = {
    showBatchModal,
    setShowBatchModal,
    batchLoading,
    batchPhase,
    setBatchPhase,
    batchFile,
    setBatchFile,
    batchImagesZip,
    setBatchImagesZip,
    batchCategoryKeyword,
    setBatchCategoryKeyword,
    batchCategoryLoading,
    batchFallbackCategory,
    setBatchFallbackCategory,
    batchPreview,
    setBatchPreview,
    batchDetail,
    setBatchDetail,
    recentBatch,
    setRecentBatch,
    batchLocations,
    batchLocation,
    setBatchLocations,
    setBatchLocation,
    batchPublishIntervalSeconds,
    setBatchPublishIntervalSeconds,
    openBatchModal,
    handleRecommendBatchCategory,
    openRecentBatchResult,
    openBatchResultByID,
    handlePreviewBatch,
    handleStartBatch,
    handleCancelBatch,
    abandonBatchPreview,
    closeBatchModal,
    handleRetryBatchFailed,
  };
  return result;
};

// ItemStatsRefreshState 描述商品统计刷新的进行状态、结果提示与两个刷新动作。
export interface ItemStatsRefreshState {
  // refreshingKeys 是正在刷新中的商品行键集合，用于禁用对应行的行内按钮。
  refreshingKeys: string[];
  // batchRefreshing 表示「刷新本页」是否正在执行。
  batchRefreshing: boolean;
  // message 是最近一次刷新的结果或可操作错误提示；空串表示无需提示。
  message: string;
  // refreshOne 强制刷新单个商品的统计并就地更新该行。
  refreshOne: (item: Item) => Promise<void>;
  // refreshPage 提交当前页商品执行一轮限量批量刷新并就地更新命中行。
  refreshPage: (pageItems: Item[]) => Promise<void>;
}

// useItemStatsRefresh 管理商品统计刷新的进行状态、结果提示与就地更新。
// 单行结果按「账号 + 商品」定位，晚到的响应不会污染其它行；批量刷新使用代次守卫，
// 避免较旧的批量响应覆盖较新的结果；组件卸载后不再写入任何状态。
export const useItemStatsRefresh = (setItems: Dispatch<SetStateAction<Item[]>>): ItemStatsRefreshState => {
  // refreshingKeys 保存正在刷新中的商品行键。
  const [refreshingKeys, setRefreshingKeys] = useState<string[]>([]);
  // batchRefreshing 表示批量刷新是否进行中。
  const [batchRefreshing, setBatchRefreshing] = useState(false);
  // message 保存最近一次刷新的提示文案。
  const [message, setMessage] = useState('');
  // batchGeneration 标识最新一次批量刷新，旧响应不得覆盖新结果。
  const batchGeneration = useRef(0);
  // mountedRef 记录组件是否仍然挂载，避免卸载后写入状态。
  const mountedRef = useRef(true);
  // markUnmounted 在组件卸载时标记状态已失效，阻止后续异步结果写入状态。
  const markUnmounted = useCallback(/* markUnmountedAction 标记组件已经卸载。 */ () => {
    mountedRef.current = false;
  }, []);
  useEffect(/* 挂载时注册卸载标记；卸载后所有异步回调都不再写入状态。 */ () => markUnmounted, [markUnmounted]);
  // refreshOne 强制刷新单个商品：用户点击即要求最新值，因此不做新鲜度判断。
  const refreshOne = useCallback(/* refreshOneAction 刷新一行商品统计。 */ async (item: Item) => {
    // rowKey 是该行的稳定索引键。
    const rowKey = statsRowKey(item.cookie_id, item.item_id);
    setRefreshingKeys(/* previous 是加入前的刷新中集合。 */ previous => previous.includes(rowKey) ? previous : [...previous, rowKey]);
    setMessage('');
    try {
      // stats 是服务端返回的最新统计。
      const stats = await refreshItemStats(item.cookie_id, item.item_id);
      if (mountedRef.current) setItems(/* previous 是刷新前的商品列表。 */ previous => applyStatsToItems(previous, [stats]));
    } catch (error /* error 是单行统计刷新失败原因  */) {
      if (mountedRef.current) setMessage(statsRefreshErrorMessage(error));
    } finally {
      if (mountedRef.current) setRefreshingKeys(/* previous 是移除后的刷新中集合。 */ previous => previous.filter(/* key 是当前仍在刷新的行键。 */ key => key !== rowKey));
    }
  }, [setItems]);

  // refreshPage 提交当前页商品并执行一轮限量批量刷新。
  const refreshPage = useCallback(/* refreshPageAction 刷新当前页商品统计。 */ async (pageItems: Item[]) => {
    // 当前页没有商品时不发起请求，避免产生一次无意义的平台访问。
    if (pageItems.length === 0) return;
    // requestGeneration 是本次批量刷新的单调递增代次。
    const requestGeneration = ++batchGeneration.current;
    // cookieId 是本次提交的账号；同一页可能跨账号，按第一个商品所属账号提交以匹配服务端接口。
    const cookieId = pageItems[0].cookie_id;
    // itemIds 是本次提交的商品标识集合，只包含与提交账号一致的商品。
    const itemIds = pageItems.filter(/* item 只保留与提交账号一致的商品。 */ item => item.cookie_id === cookieId).map(/* item 取商品标识。 */ item => item.item_id);
    setBatchRefreshing(true);
    setMessage('');
    try {
      // result 是服务端返回的批量刷新结果。
      const result = await refreshStatsBatch(cookieId, itemIds);
      if (!mountedRef.current || requestGeneration !== batchGeneration.current) return;
      setItems(/* previous 是刷新前的商品列表。 */ previous => applyStatsToItems(previous, result.refreshed));
      setMessage(batchRefreshMessage(result));
    } catch (error /* error 是批量统计刷新失败原因  */) {
      if (mountedRef.current && requestGeneration === batchGeneration.current) setMessage(statsRefreshErrorMessage(error));
    } finally {
      if (mountedRef.current) setBatchRefreshing(false);
    }
  }, [setItems]);

  // state 是本 Hook 对外暴露的状态与动作快照。
  const state: ItemStatsRefreshState = { refreshingKeys, batchRefreshing, message, refreshOne, refreshPage };
  return state;
};
