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
