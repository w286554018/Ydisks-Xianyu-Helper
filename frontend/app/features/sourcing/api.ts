import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import { type RequestControlOptions } from '../../../shared/http/client';
import { collectionFrom } from '../../../shared/http/contract';
import { accountLabel, normalizeHotKeywordStatus, normalizeRankingWindow } from './models';
import type { CopywritingRequest, HotKeywordSnapshot, RankingEntry, RankingWindow, SearchResultItem, SourcingAccount } from './types';

/** SEARCH_REQUEST_TIMEOUT_MS 是关键词查货的请求超时；浏览器打开搜索页并等待渲染需要较长时间。 */
const SEARCH_REQUEST_TIMEOUT_MS = 90_000;

/** COPYWRITING_REQUEST_TIMEOUT_MS 是 AI 文案生成的请求超时，适应推理模型的首 token 延迟。 */
const COPYWRITING_REQUEST_TIMEOUT_MS = 90_000;

/** fetchHotKeywords 读取当日平台热词快照。 */
export const fetchHotKeywords = async (options?: RequestControlOptions): Promise<HotKeywordSnapshot> => {
  // data 是热词接口的传输 DTO。
  const data = await runContractRequest(/* signal 是本次热词读取的取消与超时信号。 */ signal => contractClient.GET('/api/v1/sourcing/hot-keywords', { signal }), options);
  return {
    date: data.date,
    keywords: data.keywords ?? [],
    status: normalizeHotKeywordStatus(data.status),
    errorMessage: data.error_message ?? '',
  };
};

/** refreshHotKeywords 手动触发一次热词采集（游客态，不使用账号凭证）。 */
export const refreshHotKeywords = async (options?: RequestControlOptions): Promise<void> => {
  await runContractRequest(/* signal 是本次热词采集的取消与超时信号。 */ signal => contractClient.POST('/api/v1/sourcing/hot-keywords/refresh', { signal }), { timeoutMs: SEARCH_REQUEST_TIMEOUT_MS, ...options });
};

/** searchItems 以指定账号执行一次关键词查货；每次调用对应服务端一次搜索请求。 */
export const searchItems = async (cookieId: string, keyword: string, options?: RequestControlOptions): Promise<SearchResultItem[]> => {
  // data 是查货接口的传输 DTO。
  const data = await runContractRequest(/* signal 是本次查货的取消与超时信号。 */ signal => contractClient.POST('/api/v1/sourcing/search', {
    body: { cookie_id: cookieId, keyword },
    signal,
  }), { timeoutMs: SEARCH_REQUEST_TIMEOUT_MS, ...options });
  return (data.items ?? []).map(/* item 是服务端返回的一条商品摘要 DTO。 */ item => ({
    itemId: item.item_id,
    title: item.title,
    price: item.price,
    wantCount: item.want_count,
    location: item.location,
    detailUrl: item.detail_url,
  }));
};

/** fetchShopRanking 读取当前用户在指定窗口内的本店畅销榜（只读本地订单）。 */
export const fetchShopRanking = async (window: RankingWindow, options?: RequestControlOptions): Promise<{ /** window 是服务端实际使用的统计窗口。 */ window: RankingWindow; /** entries 是排行条目。 */ entries: RankingEntry[] }> => {
  // data 是畅销榜接口的传输 DTO。
  const data = await runContractRequest(/* signal 是本次畅销榜读取的取消与超时信号。 */ signal => contractClient.GET('/api/v1/sourcing/shop-ranking', { params: { query: { window } }, signal }), options);
  return {
    window: normalizeRankingWindow(data.window),
    entries: (data.entries ?? []).map(/* entry 是服务端返回的一条排行 DTO。 */ entry => ({
      itemId: entry.item_id,
      itemTitle: entry.item_title,
      soldQuantity: entry.sold_quantity,
      revenue: entry.revenue,
      orderCount: entry.order_count,
    })),
  };
};

/** generateCopywriting 请求 AI 生成上架文案或品类分析。 */
export const generateCopywriting = async (request: CopywritingRequest, options?: RequestControlOptions): Promise<string> => {
  // data 是文案接口的传输 DTO。
  const data = await runContractRequest(/* signal 是本次文案生成的取消与超时信号。 */ signal => contractClient.POST('/api/v1/sourcing/copywriting', {
    body: { mode: request.mode, topic: request.topic, reference: request.reference || undefined },
    signal,
  }), { timeoutMs: COPYWRITING_REQUEST_TIMEOUT_MS, ...options });
  return data.content;
};

/** SourcingAccountDTO 是账号摘要接口中本功能需要的最小字段，不读取任何凭证字段。 */
interface SourcingAccountDTO {
  /** id 是账号标识。 */
  id?: string;
  /** nickname 是平台昵称缓存。 */
  nickname?: string;
  /** remark 是账号备注。 */
  remark?: string;
}

/** fetchSourcingAccounts 读取当前用户名下账号，用作查货账号选择。 */
export const fetchSourcingAccounts = async (options?: RequestControlOptions): Promise<SourcingAccount[]> => {
  // response 是账号摘要接口的原始响应，兼容数组与历史包装。
  const response = await runContractRequest(/* signal 是本次账号列表读取的取消与超时信号。 */ signal => contractClient.GET('/api/v1/accounts/details', { signal }), options);
  // rows 是解包后的账号摘要集合。
  const rows = collectionFrom<SourcingAccountDTO>(response, ['data', 'accounts', 'details']);
  return rows
    .filter(/* row 是一条账号摘要，缺少标识的记录无法用于查货。 */ row => typeof row.id === 'string' && row.id !== '')
    .map(/* row 是一条有效账号摘要。 */ row => ({ id: row.id as string, label: accountLabel(row.id as string, row.nickname ?? '', row.remark ?? '') }));
};
