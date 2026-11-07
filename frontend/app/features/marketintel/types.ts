// 市场情报 feature 的 UI 模型类型；不直接暴露 HTTP DTO。

/** KeywordMomentumStatus 是热词涨幅状态。 */
export type KeywordMomentumStatus = 'new' | 'rising' | 'steady' | 'falling';

/** MomentumEntry 是热词涨幅榜的单条词条。 */
export interface MomentumEntry {
  /** keyword 是热词文本。 */
  keyword: string;
  /** status 是涨幅状态。 */
  status: KeywordMomentumStatus;
  /** position 是最新名次。 */
  position: number;
  /** previousPosition 是前一日名次。 */
  previousPosition: number;
  /** daysOnList 是窗口内在榜天数。 */
  daysOnList: number;
}

/** MomentumSummary 是热词涨幅榜视图。 */
export interface MomentumSummary {
  /** windowDays 是对比窗口天数。 */
  windowDays: number;
  /** latestDate 是最新快照归属日。 */
  latestDate: string;
  /** items 是涨幅词条。 */
  items: MomentumEntry[];
}

/** WatchedKeyword 是一条关注词。 */
export interface WatchedKeyword {
  /** id 是关注词主键。 */
  id: number;
  /** keyword 是搜索词文本。 */
  keyword: string;
  /** enabled 表示是否参与每日扫描。 */
  enabled: boolean;
  /** sortOrder 是展示排序值。 */
  sortOrder: number;
}

/** KeywordListResult 是词表读取结果。 */
export interface KeywordListResult {
  /** items 是关注词集合。 */
  items: WatchedKeyword[];
  /** maxEnabled 是启用词上限。 */
  maxEnabled: number;
}

/** AggregateSummary 是某词最新聚合摘要。 */
export interface AggregateSummary {
  /** keyword 是关注词。 */
  keyword: string;
  /** date 是归属日。 */
  date: string;
  /** totalCount 是当日商品总数。 */
  totalCount: number;
  /** aiCount 是 AI 相关条数。 */
  aiCount: number;
  /** totalWant 是想要人数总和。 */
  totalWant: number;
  /** verdictStatus 是判定状态。 */
  verdictStatus: 'pending' | 'done';
}

/** OverviewData 是市场情报概览。 */
export interface OverviewData {
  /** momentum 是热词涨幅榜。 */
  momentum: MomentumSummary;
  /** keywords 是各词最新聚合。 */
  keywords: AggregateSummary[];
}

/** TrendPoint 是趋势曲线单点。 */
export interface TrendPoint {
  /** date 是归属日。 */
  date: string;
  /** totalWant 是当日 want 总数。 */
  totalWant: number;
  /** aiCount 是 AI 相关条数。 */
  aiCount: number;
  /** totalCount 是当日商品总数。 */
  totalCount: number;
}

/** TrendData 是单词趋势视图。 */
export interface TrendData {
  /** keywordId 是关注词主键。 */
  keywordId: number;
  /** keyword 是关注词文本。 */
  keyword: string;
  /** days 是窗口天数。 */
  days: number;
  /** points 是趋势点。 */
  points: TrendPoint[];
}

/** SellerStat 是卖家榜单条统计。 */
export interface SellerStat {
  /** nickname 是卖家昵称。 */
  nickname: string;
  /** appearances 是出现次数。 */
  appearances: number;
  /** avgPrice 是平均价格文本。 */
  avgPrice: string;
}

/** SellerBoardData 是卖家榜视图。 */
export interface SellerBoardData {
  /** days 是统计窗口天数。 */
  days: number;
  /** sellers 是卖家统计。 */
  sellers: SellerStat[];
}

/** ScanResult 是手动扫描结果摘要。 */
export interface ScanResult {
  /** date 是扫描归属日。 */
  date: string;
  /** scanned 是实际扫描词数。 */
  scanned: number;
  /** skipped 是幂等跳过词数。 */
  skipped: number;
  /** failedWords 是扫描失败词数。 */
  failedWords: number;
  /** verdictFailed 是判定失败词数。 */
  verdictFailed: number;
  /** stoppedReason 是中止原因。 */
  stoppedReason?: string;
}
