/** RankingWindow 是本店畅销榜的统计窗口：近 7 天或近 30 天。 */
export type RankingWindow = '7d' | '30d';

/** CopywritingMode 是 AI 文案模式：listing 生成可直接上架的标题描述，analysis 生成品类分析。 */
export type CopywritingMode = 'listing' | 'analysis';

/** HotKeywordSnapshot 是页面展示的当日平台热词快照。 */
export interface HotKeywordSnapshot {
  /** date 是快照归属日（北京时间，YYYY-MM-DD）。 */
  date: string;
  /** keywords 是平台推荐的热搜词条，按平台顺序排列。 */
  keywords: string[];
  /** status 是采集状态：collected 已采集、failed 采集失败、pending 当日尚未采集。 */
  status: 'collected' | 'failed' | 'pending';
  /** errorMessage 是采集失败的原因摘要，成功时为空字符串。 */
  errorMessage: string;
}

/** SourcingAccount 是可用于关键词查货的本人闲鱼账号摘要，不含任何凭证。 */
export interface SourcingAccount {
  /** id 是账号标识，提交查货请求时作为 cookie_id。 */
  id: string;
  /** label 是下拉框展示文本，优先使用平台昵称，其次备注，最后使用账号标识。 */
  label: string;
}

/** SearchResultItem 是关键词查货返回的单条在售商品摘要。 */
export interface SearchResultItem {
  /** itemId 是平台商品标识。 */
  itemId: string;
  /** title 是商品标题。 */
  title: string;
  /** price 是页面展示的价格文本（元），缺失时为空。 */
  price: string;
  /** wantCount 是“多少人想要”的人数文本，缺失时为空。 */
  wantCount: string;
  /** location 是卖家所在地区，缺失时为空。 */
  location: string;
  /** detailUrl 是商品详情页地址。 */
  detailUrl: string;
}

/** RankingEntry 是本店畅销榜中的单个商品统计。 */
export interface RankingEntry {
  /** itemId 是平台商品标识。 */
  itemId: string;
  /** itemTitle 是本地商品标题，商品未同步时为空。 */
  itemTitle: string;
  /** soldQuantity 是统计窗口内的售出件数。 */
  soldQuantity: number;
  /** revenue 是统计窗口内的营收合计（元）。 */
  revenue: number;
  /** orderCount 是统计窗口内的有效订单数。 */
  orderCount: number;
}

/** CopywritingRequest 是页面发起 AI 文案生成的输入。 */
export interface CopywritingRequest {
  /** mode 是文案模式。 */
  mode: CopywritingMode;
  /** topic 是品类关键词或商品标题。 */
  topic: string;
  /** reference 是可选参考信息，例如价格与想要人数。 */
  reference: string;
}
