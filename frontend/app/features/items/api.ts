import type { PublishLocation } from './models';
import {
AccountDetail,
BatchCancelResponse,
BatchIDResponse,
CategoryRecommendationResponse,
Item,
ItemAISettings,
ItemCopyPreview,
ItemCopyTaskResult,
ItemDetailResponse,
ItemLifecycleAction,
ItemLifecycleBatch,
ItemLifecycleBatchDetail,
ItemLifecycleCreateResult,
ItemLifecycleRetryResult,
ItemLifecycleStatusResult,
ItemLifecycleTarget,
ItemPublishBatchPreviewResponse,
ItemPublishBatchResponse,
ItemPublishResponse,ItemSyncResponse,
ItemStats,
ItemStatsBatchResult,
OperationResponse,
ShippingRule
} from './models';
import { ApiError, type RequestControlOptions } from '../../../shared/http/client';
import { contractClient, contractMultipartBody, runContractRequest } from '../../../shared/api-contract/client';
import { collectionFrom } from '../../../shared/http/contract';
import type { PublishSkuRow, PublishSpec } from '../../../shared/ui/publish/publishSpecs';
export type * from './models';
import { getPublishLocations as queryPublishLocations,type PublishLocationRequestOptions } from './amapLocation';

/** 商品账号筛选器读取非敏感账号摘要。 */
export const getAccountDetails = async (options?: RequestControlOptions): Promise<AccountDetail[]> => runContractRequest(/* signal 控制商品页账号摘要请求的取消和超时。 */ signal => contractClient.GET('/api/v1/accounts/details', { signal }), options);

/** 商品页面读取自动化发货规则的兼容列表。 */
export const getShippingRules = async (options?: RequestControlOptions): Promise<ShippingRule[]> => runContractRequest(/* signal 控制商品页规则读取请求的取消和超时。 */ signal => contractClient.GET('/api/v1/automation-rules', { signal }), options) as unknown as Promise<ShippingRule[]>;

// getPublishLocations 通过 feature API 边界读取地点，并把取消/超时控制传入地图服务。
export const getPublishLocations = (longitude: number, latitude: number, options?: PublishLocationRequestOptions): Promise<PublishLocation[]> => queryPublishLocations(longitude, latitude, options);
export type { PublishLocation } from './models';

// itemErrorMessage 将统一 HTTP 错误码归一为商品 feature 可执行的用户提示。
export const itemErrorMessage = (error: unknown, fallback: string): string => {
  if (error instanceof ApiError) {
    switch (error.code) {
      case 'stock_permission_missing':
        return '发布失败：该账号没有库存发布权限，无法按库存数量发布商品。请换账号或先在闲鱼确认库存能力。';
      case 'conflict':
      case 'item_conflict':
        return '商品状态已发生变化，请刷新后重试。';
      case 'external_result_unknown':
      case 'manual_review_required':
        return '平台结果暂时无法确认，请先人工核对闲鱼状态，再决定是否重试。';
      case 'retryable':
      case 'temporarily_unavailable':
        return '平台暂时不可用，请稍后重试。';
      default:
        return error.message || fallback;
    }
  }
  if (error instanceof Error) return error.message;
  // message 保存兼容 API 错误对象中的文本说明。
  const message = typeof error === 'object' && error !== null && 'message' in error ? (error as { /** message 是兼容错误对象中的文本说明。 */ message?: unknown }).message : undefined;
  return typeof message === 'string' ? message : fallback;
};
// Items
// normalizeBooleanFlag 归一化布尔标记。
const normalizeBooleanFlag = (value: unknown): boolean =>
    value === true || value === 1 || value === '1';

// getItems 读取商品列表。
export const getItems = async (cookieId?: string, options?: RequestControlOptions): Promise<Item[]> => {
    // res 接口响应结果，用于当前 API 处理流程。
    const res = await runContractRequest(/* signal 控制商品列表请求的取消和超时。 */ signal => contractClient.GET('/api/v1/items', { params: { query: { cookie_id: cookieId } }, signal }), options) as unknown;
    // items 商品列表，用于当前 API 处理流程。
    const items = collectionFrom<Item>(res, ['items', 'data', 'results']);
    return items.map(/* 当前回调用于处理集合元素或接口响应。 */ (item: any) => ({
      ...item,
      id: item.id || `${item.cookie_id}-${item.item_id}`,
      is_multi_spec: normalizeBooleanFlag(item.is_multi_spec),
      is_multi_qty_ship: normalizeBooleanFlag(item.is_multi_qty_ship ?? item.multi_quantity_delivery),
      multi_quantity_delivery: normalizeBooleanFlag(item.multi_quantity_delivery ?? item.is_multi_qty_ship),
    }));
}

// getItemAISettings 在用户打开配置弹窗时读取单件商品的完整 AI 配置。
export const getItemAISettings = async (cookieId: string, itemId: string, options?: RequestControlOptions): Promise<ItemAISettings> => {
    // response 是生成契约约束的商品级 AI 配置传输对象。
    const response = await runContractRequest(/* signal 控制商品级 AI 配置读取的取消和超时。 */ signal => contractClient.GET('/api/v1/items/{cookie_id}/{item_id}/ai-settings', {
      params: { path: { cookie_id: cookieId, item_id: itemId } },
      signal,
    }), options);
    return { ai_override: response.ai_override, item_context: response.item_context };
}

// updateItemAISettings 保存单件商品的 AI 覆盖状态和专属资料。
export const updateItemAISettings = async (cookieId: string, itemId: string, settings: ItemAISettings, options?: RequestControlOptions): Promise<ItemAISettings> => {
    // response 是服务端保存后返回的规范化商品级 AI 配置。
    const response = await runContractRequest(/* signal 控制商品级 AI 配置保存的取消和超时。 */ signal => contractClient.PUT('/api/v1/items/{cookie_id}/{item_id}/ai-settings', {
      params: { path: { cookie_id: cookieId, item_id: itemId } },
      body: settings,
      signal,
    }), options);
    return { ai_override: response.ai_override, item_context: response.item_context };
}

// syncItemsFromAccount 从账号同步商品。
export const syncItemsFromAccount = async (cookieId: string): Promise<ItemSyncResponse> => {
    return runContractRequest(/* signal 控制商品同步请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/get-all-from-account', { body: { cookie_id: cookieId } as never, signal })) as unknown as Promise<ItemSyncResponse>;
}

// deleteItem 删除商品。
export const deleteItem = async (cookieId: string, itemId: string): Promise<OperationResponse> => {
    return runContractRequest(/* signal 控制商品删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/items/{cookie_id}/{item_id}', { params: { path: { cookie_id: cookieId, item_id: itemId } }, signal }));
}

// createItem 创建商品。
export const createItem = async (cookieId: string, data: Partial<Item>): Promise<OperationResponse> => {
    return runContractRequest(/* signal 控制商品创建请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/{cookie_id}', { params: { path: { cookie_id: cookieId } }, body: data as never, signal }));
}

// publishItem 发布商品。
export const publishItem = async (form: {
    /** cookie_id 表示登录凭证标识。 */ cookie_id: string;
    /** title 表示标题。 */ title: string;
    /** description 表示描述。 */ description: string;
    /** price 表示售价。 */ price: string;
    /** original_price 表示原始售价。 */ original_price?: string;
    /** quantity 表示待发布商品的件数，提交前会转换为表单字符串。 */ quantity: string | number;
    /** postage_mode 表示运费模式。 */ postage_mode: string;
    /** postage 表示运费。 */ postage?: string;
	/** images 表示图片列表。 */ images: File[];
	/** specs 表示商品规格维度及其规格值。 */ specs?: PublishSpec[];
	/** skuRows 表示规格组合的价格和库存。 */ skuRows?: PublishSkuRow[];
	/** location 表示地址。 */ location?: PublishLocation;
	/** category 表示用户选中的闲鱼类目；省略时由后端自动识别并使用电子资料兜底。 */ category?: {
	  /** cat_id 表示闲鱼类目主键。 */ cat_id: string;
	  /** cat_name 表示闲鱼类目名称。 */ cat_name: string;
	  /** channel_cat_id 表示闲鱼频道类目主键。 */ channel_cat_id?: string;
	  /** tb_cat_id 表示可选淘宝类目主键。 */ tb_cat_id?: string;
	};
}): Promise<ItemPublishResponse> => {
    // body 请求体，用于当前 API 处理流程。
    const body = new FormData();
    body.set('cookie_id', form.cookie_id);
    body.set('title', form.title);
    body.set('description', form.description);
    body.set('price', form.price);
    body.set('original_price', form.original_price || '');
    body.set('quantity', String(form.quantity));
    body.set('postage_mode', form.postage_mode);
    body.set('postage', form.postage || '');
	if (form.location) body.set('location', JSON.stringify(form.location));
    if (form.category) {
	  body.set('category_id', form.category.cat_id);
	  body.set('category_name', form.category.cat_name);
	  body.set('channel_category_id', form.category.channel_cat_id || '');
	  body.set('tb_category_id', form.category.tb_cat_id || '');
	}
    if (form.specs?.length && form.skuRows?.length) {
      // specImages 保存规格值图片，并按下标与 itemProperties 中的 image_index 对应。
      const specImages: File[] = [];
      // itemProperties 保存与闲鱼官方发布请求一致的规格维度和规格值结构。
      const itemProperties = form.specs.map(/* spec 转换为官方规格维度 DTO。 */ spec => ({
        propertyName: spec.name.trim(),
        supportImage: spec.supportImage,
        propertyValues: spec.values.filter(/* value 筛选非空规格值。 */ value => value.value.trim()).map(/* value 转换为官方规格值 DTO。 */ value => {
          // imageIndex 表示该规格值图片在 spec_images multipart 字段中的位置。
          const imageIndex = spec.supportImage && value.image ? specImages.push(value.image) - 1 : -1;
          return { propertyValue: value.value.trim(), ...(imageIndex >= 0 ? { image_index: imageIndex } : {}) };
        }),
      }));
      // itemSkuList 保存每个组合的官方 propertyList、price 和 quantity 字段。
      const itemSkuList = form.skuRows.map(/* row 转换为官方 SKU 行 DTO。 */ row => ({
        price: row.price,
        quantity: Number(row.quantity),
        propertyList: row.valueLabels.map(/* valueText、index 组成当前 SKU 的规格对。 */ (valueText, index) => ({ propertyText: form.specs?.[index]?.name.trim() || '', valueText })),
      }));
      body.set('item_properties', JSON.stringify(itemProperties));
      body.set('item_sku_list', JSON.stringify(itemSkuList));
      // specImage 表示当前待上传到规格图片字段的文件。
      for (const /* specImage 表示当前待上传到规格图片字段的文件。 */ specImage of specImages) body.append('spec_images', specImage);
	}
    for (const // file 上传文件，用于当前 API 处理流程。
file of form.images) {
      body.append('images', file);
    }
    return runContractRequest(/* signal 控制商品发布上传请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish', { body: contractMultipartBody(body), signal })) as unknown as Promise<ItemPublishResponse>;
}

// recommendPublishCategory 推荐商品发布分类。
export const recommendPublishCategory = async (cookieId: string, keyword: string, options?: RequestControlOptions): Promise<CategoryRecommendationResponse> => {
    // 类目推荐成功响应使用共享 CategoryRecommendationResponse。
    // category 字段保留平台类目 ID、名称和频道类目 ID。
    // tb_cat_id 继续保持可选，兼容电子资料类目。
    // 请求仍携带账号 ID 和关键词。
    // 失败响应由共享 HTTP 错误结构处理。
    // 该类型收口不改变凭证刷新和错误处理。
    // 前端批量发布流程可直接复用 category。
    // 旧路径继续由现有 Vite 代理转发。
	return runContractRequest(/* signal 控制商品类目推荐请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish-categories/recommend', { body: { cookie_id: cookieId, keyword } as never, signal }), options) as unknown as Promise<CategoryRecommendationResponse>;
};

// previewItemPublishBatch 预览商品批量发布。
export const previewItemPublishBatch = async (form: {
    /** file 表示上传文件。 */ file: File;
    /** imagesZip 表示图片压缩包。 */ imagesZip?: File | null;
    /** defaultCookieId 表示默认账号凭证标识。 */ defaultCookieId?: string;
    /** fallbackCategory 表示备用分类。 */ fallbackCategory: {
      /** catId 表示分类标识。 */ catId: string;
      /** catName 表示分类名称。 */ catName: string;
      /** channelCatId 表示渠道分类标识。 */ channelCatId?: string;
      /** tbCatId 表示淘宝分类标识。 */ tbCatId?: string;
    };
	/** location 表示地址。 */ location?: PublishLocation;
	/** publishIntervalSeconds 表示最终商品发布之间的最小间隔秒数。 */
	publishIntervalSeconds?: number;
}, options?: RequestControlOptions): Promise<ItemPublishBatchPreviewResponse> => {
    // body 请求体，用于当前 API 处理流程。
    const body = new FormData();
    body.set('file', form.file);
    if (form.imagesZip) body.set('images_zip', form.imagesZip);
    if (form.defaultCookieId) body.set('default_cookie_id', form.defaultCookieId);
    body.set('fallback_category_id', form.fallbackCategory.catId);
    body.set('fallback_category_name', form.fallbackCategory.catName);
    body.set('fallback_channel_category_id', form.fallbackCategory.channelCatId || '');
    body.set('fallback_tb_category_id', form.fallbackCategory.tbCatId || '');
	if (form.location) body.set('location', JSON.stringify(form.location));
	body.set('publish_interval_seconds', String(form.publishIntervalSeconds ?? 5));
	return runContractRequest(/* signal 控制批量发布预览上传的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish-batches/preview', { body: contractMultipartBody(body), signal }), options) as unknown as Promise<ItemPublishBatchPreviewResponse>;
}

// startItemPublishBatch 启动商品批量发布。
export const startItemPublishBatch = async (previewId: string, options?: RequestControlOptions): Promise<BatchIDResponse> => {
	return runContractRequest(/* signal 控制批量发布启动请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish-batches', { body: { preview_id: previewId } as never, signal }), options) as unknown as Promise<BatchIDResponse>;
}

// getItemPublishBatch 读取商品发布批次。
export const getItemPublishBatch = async (batchId: string, options?: RequestControlOptions): Promise<ItemPublishBatchResponse> => {
	return runContractRequest(/* signal 控制批量发布状态读取的取消和超时。 */ signal => contractClient.GET('/api/v1/items/publish-batches/{batch_id}', { params: { path: { batch_id: batchId } }, signal }), options) as unknown as Promise<ItemPublishBatchResponse>;
}

// getItemPublishBatches 读取商品发布批次列表。
export const getItemPublishBatches = async (limit = 20, options?: RequestControlOptions): Promise<ItemPublishBatchResponse[]> => {
    // res 接口响应结果，用于当前 API 处理流程。
    const res = await runContractRequest(/* signal 控制批量发布列表读取的取消和超时。 */ signal => contractClient.GET('/api/v1/items/publish-batches', { params: { query: { limit } }, signal }), options) as unknown;
    return collectionFrom<ItemPublishBatchResponse>(res, ['batches', 'data', 'items']);
}

// deleteItemPublishBatch 删除商品发布批次。
export const deleteItemPublishBatch = async (batchId: string, options?: RequestControlOptions): Promise<OperationResponse> => {
	return runContractRequest(/* signal 控制批量发布删除请求的取消和超时。 */ signal => contractClient.DELETE('/api/v1/items/publish-batches/{batch_id}', { params: { path: { batch_id: batchId } }, signal }), options);
}

// cancelItemPublishBatch 取消商品发布批次。
export const cancelItemPublishBatch = async (batchId: string, options?: RequestControlOptions): Promise<BatchCancelResponse> => {
	return runContractRequest(/* signal 控制批量发布取消请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish-batches/{batch_id}/cancel', { params: { path: { batch_id: batchId } }, body: {} as never, signal }), options) as unknown as Promise<BatchCancelResponse>;
}

// retryFailedItemPublishBatch 重试失败的商品发布任务。
export const retryFailedItemPublishBatch = async (batchId: string, options?: RequestControlOptions): Promise<BatchIDResponse> => {
	return runContractRequest(/* signal 控制批量发布重试请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/publish-batches/{batch_id}/retry-failed', { params: { path: { batch_id: batchId } }, body: {} as never, signal }), options) as unknown as Promise<BatchIDResponse>;
}

// updateItem 更新商品。
export const updateItem = async (cookieId: string, itemId: string, data: Partial<Item>): Promise<OperationResponse> => {
    return runContractRequest(/* signal 控制商品更新请求的取消和超时。 */ signal => contractClient.PUT('/api/v1/items/{cookie_id}/{item_id}', { params: { path: { cookie_id: cookieId, item_id: itemId } }, body: data as never, signal }));
}

/** 读取指定账号与商品的详情，用于编辑器恢复表单，并将生成传输 DTO 保持在 feature adapter 边界。 */
export const getItemDetail = async (accountID: string, itemID: string): Promise<ItemDetailResponse> => {
  // response 保存已由 OpenAPI 路径类型约束的商品详情传输响应。
  const response = await runContractRequest(/* signal 控制商品详情请求的取消和超时。 */ signal => contractClient.GET('/api/v1/items/{cookie_id}/{item_id}', { params: { path: { cookie_id: accountID, item_id: itemID } }, signal }));
  return response;
};
/** 预览跨账号复制：读取源商品快照预填内容与全部候选目标账号的预检状态，全程不发平台请求。 */
export const previewItemCopy = async (form: {
  /** cookieId 是源账号标识。 */
  cookieId: string;
  /** itemId 是源商品标识。 */
  itemId: string;
  /** targetCookieIds 是候选目标账号列表；为空时服务端返回全部账号预检。 */
  targetCookieIds?: string[];
}, options?: RequestControlOptions): Promise<ItemCopyPreview> => {
  return runContractRequest(/* signal 控制复制预览请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/copy-preview', {
    body: {
      cookie_id: form.cookieId,
      item_id: form.itemId,
      target_cookie_ids: form.targetCookieIds ?? [],
    } as never,
    signal,
  }), options) as unknown as Promise<ItemCopyPreview>;
};

/** 创建跨账号复制任务：服务端落库为批量铺货预检批次，前端用既有批次进度接口轮询。 */
export const createItemCopyTask = async (form: {
  /** cookieId 是源账号标识。 */
  cookieId: string;
  /** itemId 是源商品标识。 */
  itemId: string;
  /** targetCookieIds 是目标账号列表。 */
  targetCookieIds: string[];
  /** titleEdit 为空时沿用源标题。 */
  titleEdit?: string;
  /** descriptionEdit 为空时沿用源描述。 */
  descriptionEdit?: string;
  /** priceEdit 为空时沿用源价格。 */
  priceEdit?: string;
  /** publishIntervalSeconds 是相邻发布的最小间隔秒数；非正数使用系统默认。 */
  publishIntervalSeconds?: number;
}, options?: RequestControlOptions): Promise<ItemCopyTaskResult> => {
  return runContractRequest(/* signal 控制复制任务创建请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/copy-tasks', {
    body: {
      cookie_id: form.cookieId,
      item_id: form.itemId,
      target_cookie_ids: form.targetCookieIds,
      title_edit: form.titleEdit ?? '',
      description_edit: form.descriptionEdit ?? '',
      price_edit: form.priceEdit ?? '',
      publish_interval_seconds: form.publishIntervalSeconds ?? 0,
    } as never,
    signal,
  }), options) as unknown as Promise<ItemCopyTaskResult>;
};

// createItemLifecycleBatch 创建商品下架/删除批次；单商品操作也走 1 行批次。
export const createItemLifecycleBatch = async (action: ItemLifecycleAction, targets: ItemLifecycleTarget[], options?: RequestControlOptions): Promise<ItemLifecycleCreateResult> => {
  return runContractRequest(/* signal 控制生命周期批次创建请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/lifecycle', { body: { action, items: targets } as never, signal }), options) as unknown as Promise<ItemLifecycleCreateResult>;
}

// getItemLifecycleBatch 读取生命周期批次详情（批次头加行明细）。
export const getItemLifecycleBatch = async (batchId: string, options?: RequestControlOptions): Promise<ItemLifecycleBatchDetail> => {
  return runContractRequest(/* signal 控制生命周期批次读取请求的取消和超时。 */ signal => contractClient.GET('/api/v1/items/lifecycle-batches/{batch_id}', { params: { path: { batch_id: batchId } }, signal }), options) as unknown as Promise<ItemLifecycleBatchDetail>;
}

// listItemLifecycleBatches 读取最近的生命周期批次列表。
export const listItemLifecycleBatches = async (options?: RequestControlOptions): Promise<ItemLifecycleBatch[]> => {
  // res 是批次列表原始响应，按集合键归一为数组。
  const res = await runContractRequest(/* signal 控制生命周期批次列表读取的取消和超时。 */ signal => contractClient.GET('/api/v1/items/lifecycle-batches', { signal }), options) as unknown;
  return collectionFrom<ItemLifecycleBatch>(res, ['data', 'items']);
}

// retryFailedItemLifecycleBatch 重试生命周期批次内的失败行并重新执行。
export const retryFailedItemLifecycleBatch = async (batchId: string, options?: RequestControlOptions): Promise<ItemLifecycleRetryResult> => {
  return runContractRequest(/* signal 控制生命周期批次重试请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/lifecycle-batches/{batch_id}/retry-failed', { params: { path: { batch_id: batchId } }, body: {} as never, signal }), options) as unknown as Promise<ItemLifecycleRetryResult>;
}

// cancelItemLifecycleBatch 取消仍在执行的生命周期批次。
export const cancelItemLifecycleBatch = async (batchId: string, options?: RequestControlOptions): Promise<ItemLifecycleStatusResult> => {
  return runContractRequest(/* signal 控制生命周期批次取消请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/lifecycle-batches/{batch_id}/cancel', { params: { path: { batch_id: batchId } }, body: {} as never, signal }), options) as unknown as Promise<ItemLifecycleStatusResult>;
}

// refreshItemStats 强制刷新单个商品的浏览量/想要量统计；服务端忽略 24 小时缓存。
export const refreshItemStats = async (cookieId: string, itemId: string, options?: RequestControlOptions): Promise<ItemStats> => {
  // response 是生成契约约束的单商品统计传输对象。
  const response = await runContractRequest(/* signal 控制单商品统计刷新请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/{cookie_id}/{item_id}/stats/refresh', {
    params: { path: { cookie_id: cookieId, item_id: itemId } },
    signal,
  }), options);
  return response as unknown as ItemStats;
}

// refreshStatsBatch 提交当前页商品并执行一轮限量批量刷新；单轮上限与风控即停由服务端强制。
export const refreshStatsBatch = async (cookieId: string, itemIds: string[], limit?: number, options?: RequestControlOptions): Promise<ItemStatsBatchResult> => {
  // response 是生成契约约束的批量刷新传输对象。
  const response = await runContractRequest(/* signal 控制批量统计刷新请求的取消和超时。 */ signal => contractClient.POST('/api/v1/items/stats/refresh-batch', {
    body: { cookie_id: cookieId, item_ids: itemIds, ...(limit === undefined ? {} : { limit }) } as never,
    signal,
  }), options);
  return response as unknown as ItemStatsBatchResult;
}

// statsRefreshErrorMessage 把商品统计刷新失败转换为可操作提示，优先区分人工验证与助手不可用。
// 该函数放在 API 适配层：只有适配器可以引用共享 HTTP 错误类型。
export const statsRefreshErrorMessage = (error: unknown): string => {
  if (error instanceof ApiError) {
    if (error.status === 409) return '闲鱼要求人机验证：请在抓取助手浏览器中完成验证后重试。';
    if (error.status === 503) return '本机抓取助手不可用：请确认助手已启动、反向隧道已连接。';
    if (error.status === 403) return '无权操作该账号。';
    return error.message || '刷新商品统计失败。';
  }
  return error instanceof Error ? error.message : '刷新商品统计失败。';
};
