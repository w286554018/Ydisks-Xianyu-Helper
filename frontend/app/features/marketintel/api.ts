import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import { type RequestControlOptions } from '../../../shared/http/client';
import type { KeywordListResult, MomentumSummary, OverviewData, ScanResult, SellerBoardData, TrendData, WatchedKeyword } from './types';

/** SCAN_REQUEST_TIMEOUT_MS 是手动扫描的请求超时；串行扫描多词需要较长时间。 */
const SCAN_REQUEST_TIMEOUT_MS = 600_000;

/** normalizeMomentumStatus 把传输状态归一为涨幅枚举。 */
const normalizeMomentumStatus = (status: string): MomentumSummary['items'][number]['status'] => {
  if (status === 'new' || status === 'rising' || status === 'steady' || status === 'falling') return status;
  return 'steady';
};

/** fetchMomentum 读取热词涨幅榜。 */
export const fetchMomentum = async (options?: RequestControlOptions): Promise<MomentumSummary> => {
  // data 是涨幅接口的传输 DTO。
  const data = await runContractRequest(/* signal 控制涨幅读取的取消和超时。 */ /* momentumSignal 控制涨幅读取的取消。 */ signal => contractClient.GET('/api/v1/sourcing/hot-keywords/momentum', { signal }), options);
  return {
    windowDays: data.window_days,
    latestDate: data.latest_date ?? '',
    items: (data.items ?? []).map(/* momentumEntry 归一单条涨幅词条。 */ entry => ({
      keyword: entry.keyword, status: normalizeMomentumStatus(entry.status),
      position: entry.position, previousPosition: entry.previous_position, daysOnList: entry.days_on_list,
    })),
  };
};

/** fetchKeywords 读取全部关注词。 */
export const fetchKeywords = async (options?: RequestControlOptions): Promise<KeywordListResult> => {
  // data 是词表接口的传输 DTO。
  const data = await runContractRequest(/* signal 控制词表读取的取消和超时。 */ signal => contractClient.GET('/api/v1/market-intel/keywords', { signal }), options);
  return {
    items: (data.items ?? []).map(/* keywordItem 归一单个关注词。 */ item => ({ id: item.id, keyword: item.keyword, enabled: item.enabled, sortOrder: item.sort_order })),
    maxEnabled: data.max_enabled,
  };
};

/** createKeyword 新增关注词。 */
export const createKeyword = async (keyword: string, enabled: boolean, options?: RequestControlOptions): Promise<WatchedKeyword> => {
  // data 是创建接口的传输 DTO。
  const data = await runContractRequest(/* signal 控制创建请求的取消和超时。 */ signal => contractClient.POST('/api/v1/market-intel/keywords', { body: { keyword, enabled } as never, signal }), options);
  return { id: data.id, keyword: data.keyword, enabled: data.enabled, sortOrder: data.sort_order };
};

/** updateKeyword 更新关注词。 */
export const updateKeyword = async (id: number, keyword: string, enabled: boolean, options?: RequestControlOptions): Promise<void> => {
  await runContractRequest(/* signal 控制更新请求的取消和超时。 */ /* updateSignal 控制更新请求的取消。 */ signal => contractClient.PUT('/api/v1/market-intel/keywords/{id}', { params: { path: { id: String(id) } }, body: { keyword, enabled } as never, signal }), options);
};

/** deleteKeyword 删除关注词。 */
export const deleteKeyword = async (id: number, options?: RequestControlOptions): Promise<void> => {
  await runContractRequest(/* signal 控制删除请求的取消和超时。 */ /* deleteSignal 控制删除请求的取消。 */ signal => contractClient.DELETE('/api/v1/market-intel/keywords/{id}', { params: { path: { id: String(id) } }, signal }), options);
};

/** triggerScan 手动触发一次扫描（幂等）。 */
export const triggerScan = async (options?: RequestControlOptions): Promise<ScanResult> => {
  // data 是扫描接口的传输 DTO。
  const data = await runContractRequest(/* signal 控制扫描请求的取消和超时。 */ signal => contractClient.POST('/api/v1/market-intel/scan', { body: {} as never, signal }), { timeoutMs: SCAN_REQUEST_TIMEOUT_MS, ...options });
  return { date: data.date, scanned: data.scanned, skipped: data.skipped, failedWords: data.failed_words, verdictFailed: data.verdict_failed, stoppedReason: data.stopped_reason };
};

/** fetchOverview 读取市场情报概览。 */
export const fetchOverview = async (options?: RequestControlOptions): Promise<OverviewData> => {
  // data 是概览接口的传输 DTO。
  const data = await runContractRequest(/* signal 控制概览读取的取消和超时。 */ signal => contractClient.GET('/api/v1/market-intel/overview', { signal }), options);
  return {
    momentum: {
      windowDays: data.momentum.window_days,
      latestDate: data.momentum.latest_date ?? '',
      items: (data.momentum.items ?? []).map(/* overviewMomentumEntry 归一概览涨幅词条。 */ entry => ({
        keyword: entry.keyword, status: normalizeMomentumStatus(entry.status),
        position: entry.position, previousPosition: entry.previous_position, daysOnList: entry.days_on_list,
      })),
    },
    keywords: (data.keywords ?? []).map(/* overviewKeyword 归一概览聚合摘要。 */ item => ({
      keyword: item.keyword, date: item.date, totalCount: item.total_count,
      aiCount: item.ai_count, totalWant: item.total_want,
      verdictStatus: item.verdict_status === 'done' ? 'done' : 'pending',
    })),
  };
};

/** fetchKeywordTrend 读取单词趋势。 */
export const fetchKeywordTrend = async (id: number, days: number, options?: RequestControlOptions): Promise<TrendData> => {
  // data 是趋势接口的传输 DTO。
  const data = await runContractRequest(/* signal 控制趋势读取的取消和超时。 */ /* trendSignal 控制趋势读取的取消。 */ signal => contractClient.GET('/api/v1/market-intel/keywords/{id}/trend', { params: { path: { id: String(id) }, query: { days } }, signal }), options);
  return {
    keywordId: Number(data.keyword_id), keyword: data.keyword, days: data.days,
    points: (data.points ?? []).map(/* trendPoint 归一趋势点。 */ point => ({ date: point.date, totalWant: point.total_want, aiCount: point.ai_count, totalCount: point.total_count })),
  };
};

/** fetchSellerBoard 读取竞品卖家榜。 */
export const fetchSellerBoard = async (days: number, options?: RequestControlOptions): Promise<SellerBoardData> => {
  // data 是卖家榜接口的传输 DTO。
  const data = await runContractRequest(/* signal 控制卖家榜读取的取消和超时。 */ signal => contractClient.GET('/api/v1/market-intel/sellers', { params: { query: { days } }, signal }), options);
  return {
    days: data.days,
    sellers: (data.sellers ?? []).map(/* sellerStat 归一卖家统计。 */ seller => ({ nickname: seller.nickname, appearances: seller.appearances, avgPrice: seller.avg_price ?? '', sampleUrl: seller.sample_url })),
  };
};

/** DETAIL_REQUEST_TIMEOUT_MS 是竞品详情抓取的请求超时；实时抓取需要打开详情页。 */
const DETAIL_REQUEST_TIMEOUT_MS = 90_000;

/** CompetitorDetailData 是竞品详情视图。 */
export interface CompetitorDetailData {
  /** itemId 是平台商品标识。 */
  itemId: string;
  /** title 是商品标题。 */
  title: string;
  /** price 是价格文本。 */
  price: string;
  /** descText 是商品描述纯文本。 */
  descText: string;
  /** images 是商品图片 URL 列表。 */
  images: string[];
  /** sellerNickname 是卖家昵称。 */
  sellerNickname: string;
  /** sellerId 是卖家 userId。 */
  sellerId: string;
  /** cached 表示是否来自缓存。 */
  cached: boolean;
}

/** RewrittenCopyData 是 AI 改写结果。 */
export interface RewrittenCopyData {
  /** title 是改写后标题。 */
  title: string;
  /** desc 是改写后描述。 */
  desc: string;
}

/** CarryImageData 是搬运结果中的资源库图片。 */
export interface CarryImageData {
  /** id 是资源库图片 ID。 */
  id: number;
  /** filename 是文件名。 */
  filename: string;
}

/** CarryResultData 是一键搬运结果。 */
export interface CarryResultData {
  /** title 是预填标题（AI 改写后）。 */
  title: string;
  /** desc 是预填描述。 */
  desc: string;
  /** price 是预填价格。 */
  price: string;
  /** images 是资源库图片。 */
  images: CarryImageData[];
  /** sameImagesHint 提示图片与竞品相同。 */
  sameImagesHint: string;
}

/** fetchCompetitorDetail 读取竞品详情（7 天缓存，未命中实时抓取）。 */
export const fetchCompetitorDetail = async (itemId: string, options?: RequestControlOptions): Promise<CompetitorDetailData> => {
  // data 是详情接口的传输 DTO。
  const data = await runContractRequest(/* detailSignal 控制详情抓取的取消和超时。 */ signal => contractClient.GET('/api/v1/market-intel/items/{item_id}/detail', { params: { path: { item_id: itemId } }, signal }), { timeoutMs: DETAIL_REQUEST_TIMEOUT_MS, ...options });
  return {
    itemId: data.item_id, title: data.title, price: data.price, descText: data.desc_text,
    images: data.images ?? [], sellerNickname: data.seller_nickname, sellerId: data.seller_id, cached: data.cached,
  };
};

/** rewriteCompetitorCopy 用 AI 改写竞品文案。 */
export const rewriteCompetitorCopy = async (itemId: string, options?: RequestControlOptions): Promise<RewrittenCopyData> => {
  // data 是改写接口的传输 DTO。
  const data = await runContractRequest(/* rewriteSignal 控制改写请求的取消和超时。 */ signal => contractClient.POST('/api/v1/market-intel/items/{item_id}/rewrite', { params: { path: { item_id: itemId } }, body: {} as never, signal }), { timeoutMs: DETAIL_REQUEST_TIMEOUT_MS, ...options });
  return { title: data.title, desc: data.desc };
};

/** analyzeCompetitorSellingPoints 用 AI 分析竞品卖点。 */
export const analyzeCompetitorSellingPoints = async (itemId: string, options?: RequestControlOptions): Promise<string> => {
  // data 是分析接口的传输 DTO。
  const data = await runContractRequest(/* analyzeSignal 控制分析请求的取消和超时。 */ signal => contractClient.POST('/api/v1/market-intel/items/{item_id}/analyze', { params: { path: { item_id: itemId } }, body: {} as never, signal }), { timeoutMs: DETAIL_REQUEST_TIMEOUT_MS, ...options });
  return data.analysis;
};

/** carryCompetitorItem 一键搬运（AI 改写 + 图片入库）。 */
export const carryCompetitorItem = async (itemId: string, options?: RequestControlOptions): Promise<CarryResultData> => {
  // data 是搬运接口的传输 DTO。
  const data = await runContractRequest(/* carrySignal 控制搬运请求的取消和超时。 */ signal => contractClient.POST('/api/v1/market-intel/items/{item_id}/carry', { params: { path: { item_id: itemId } }, body: {} as never, signal }), { timeoutMs: DETAIL_REQUEST_TIMEOUT_MS, ...options });
  return {
    title: data.title, desc: data.desc, price: data.price,
    images: (data.images ?? []).map(/* carryImage 归一资源库图片。 */ img => ({ id: img.id, filename: img.filename })),
    sameImagesHint: data.same_images_hint,
  };
};

/** KeywordItemSummary 是快照商品摘要。 */
export interface KeywordItemSummary {
  /** itemId 是平台商品标识。 */
  itemId: string;
  /** title 是商品标题。 */
  title: string;
  /** price 是价格文本。 */
  price: string;
  /** wantCountText 是想要人数文本。 */
  wantCountText: string;
  /** sellerNickname 是卖家昵称。 */
  sellerNickname: string;
}

/** fetchKeywordItems 读取某词最新一天的快照商品列表。 */
export const fetchKeywordItems = async (keyword: string, options?: RequestControlOptions): Promise<KeywordItemSummary[]> => {
  // data 是商品列表接口的传输 DTO。
  const data = await runContractRequest(/* itemsSignal 控制商品列表读取的取消和超时。 */ signal => contractClient.GET('/api/v1/market-intel/keywords/{keyword}/items', { params: { path: { keyword } }, signal }), options);
  return (data.items ?? []).map(/* keywordItem 归一快照商品。 */ item => ({
    itemId: item.item_id, title: item.title, price: item.price,
    wantCountText: item.want_count_text, sellerNickname: item.seller_nickname,
  }));
};
