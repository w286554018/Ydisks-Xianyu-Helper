import type { HotKeywordSnapshot, RankingWindow, SearchResultItem } from './types';

/** normalizeHotKeywordStatus 把服务端采集状态归一为页面状态；空状态表示当日尚未采集。 */
export const normalizeHotKeywordStatus = (status: string): HotKeywordSnapshot['status'] => {
  if (status === 'ok') return 'collected';
  if (status === 'failed') return 'failed';
  return 'pending';
};

/** normalizeRankingWindow 把任意窗口文本归一为合法统计窗口，未知值按近 7 天处理。 */
export const normalizeRankingWindow = (value: string): RankingWindow => (value === '30d' ? '30d' : '7d');

/** accountLabel 生成账号下拉展示文本：优先昵称，其次备注，最后账号标识。 */
export const accountLabel = (id: string, nickname: string, remark: string): string => {
  // name 是去空白后的平台昵称。
  const name = nickname.trim();
  if (name) return name;
  // note 是去空白后的账号备注。
  const note = remark.trim();
  return note ? `${note}（${id}）` : id;
};

/** searchItemReference 把查货结果整理为 AI 文案的参考信息，只包含公开展示字段。 */
export const searchItemReference = (item: SearchResultItem): string => {
  // parts 是按顺序拼接的参考片段，缺失字段直接跳过。
  const parts: string[] = [];
  if (item.price) parts.push(`售价 ${item.price} 元`);
  if (item.wantCount) parts.push(`${item.wantCount} 人想要`);
  if (item.location) parts.push(`卖家地区 ${item.location}`);
  return parts.join('，');
};

/** formatRevenue 以两位小数展示营收金额（元）。 */
export const formatRevenue = (value: number): string => (Number.isFinite(value) ? value.toFixed(2) : '0.00');

/** copywritingTitleAndBody 把上架文案拆成标题（首行）与描述（其余行），便于分别复制到发布页。 */
export const copywritingTitleAndBody = (content: string): { /** title 是首行标题。 */ title: string; /** body 是其余描述正文。 */ body: string } => {
  // lines 是去掉首尾空白后的文案行。
  const lines = content.trim().split(/\r?\n/u);
  // title 是首个非空行，作为商品标题。
  const title = (lines.shift() ?? '').trim();
  return { title, body: lines.join('\n').trim() };
};
