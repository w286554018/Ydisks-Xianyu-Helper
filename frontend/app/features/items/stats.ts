import type { Item, ItemStats, ItemStatsBatchResult } from './models';

/** statsRowKey 计算商品统计的稳定索引键；同一商品可能属于不同账号，键必须同时包含两者。 */
export const statsRowKey = (cookieId: string, itemId: string): string => `${cookieId}-${itemId}`;

/** formatStatsAge 把抓取时间格式化为相对时间文案；空值、非法时间与未来时间都返回可读结果而不是负数。 */
export const formatStatsAge = (fetchedAt: string, now: Date = new Date()): string => {
  // timestamp 是解析抓取时间得到的毫秒时间戳；空串或非法时间解析为 NaN。
  const timestamp = Date.parse(fetchedAt);
  if (Number.isNaN(timestamp)) return '未知';
  // elapsedMs 是距抓取时间的毫秒差；未来时间会得到负数，统一按「刚刚」展示。
  const elapsedMs = now.getTime() - timestamp;
  if (elapsedMs < 60 * 1000) return '刚刚';
  // minutes 是向下取整的分钟差。
  const minutes = Math.floor(elapsedMs / (60 * 1000));
  if (minutes < 60) return `${minutes} 分钟前`;
  // hours 是向下取整的小时差。
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时前`;
  // days 是向下取整的天数差。
  const days = Math.floor(hours / 24);
  return `${days} 天前`;
};

/** statsTooltip 生成商品统计的悬浮说明：收藏、已售与数据新鲜度。 */
export const statsTooltip = (stats: ItemStats): string =>
  `收藏 ${stats.collect_cnt} · 已售 ${stats.sold_cnt} · 更新于 ${formatStatsAge(stats.fetched_at)}`;

/** applyStatsToItems 按「账号 + 商品」就地替换统计并返回新数组；未命中的商品保持原样。 */
export const applyStatsToItems = (items: Item[], refreshed: ItemStats[]): Item[] => {
  if (refreshed.length === 0) return items;
  // refreshedByKey 是本次刷新结果的索引映射，避免逐行线性查找。
  const refreshedByKey = new Map<string, ItemStats>();
  for (const /* stats 是本次刷新返回的一条统计。 */ stats of refreshed) {
    refreshedByKey.set(statsRowKey(stats.cookie_id, stats.item_id), stats);
  }
  return items.map(/* item 是当前待合并的商品行。 */ item => {
    // nextStats 是该商品的新统计；未命中时保持原值，避免把已有数据清空。
    const nextStats = refreshedByKey.get(statsRowKey(item.cookie_id, item.item_id));
    return nextStats ? { ...item, stats: nextStats } : item;
  });
};

/** countRefreshableStats 统计当前页需要刷新的商品数量：从未抓取或缓存已过期都算需要刷新。 */
export const countRefreshableStats = (items: Item[]): number =>
  items.filter(/* item 是当前判断是否可刷新的商品行。 */ item => !item.stats || item.stats.fresh === false).length;

/** batchRefreshMessage 生成批量刷新的结果提示：区分有刷新、全部新鲜与遇风控停止。 */
export const batchRefreshMessage = (result: ItemStatsBatchResult): string => {
  // base 是基础结果文案，包含已刷新与剩余数量。
  const base = `已刷新 ${result.refreshed.length} 个，剩余 ${result.remaining} 个待刷新。`;
  if (result.stopped) return `${base}遇到闲鱼人机验证，已停止，请在抓取助手浏览器中完成验证后重试。`;
  if (result.refreshed.length === 0) return `当前页数据都是最新的，共 ${result.skipped} 个无需刷新。`;
  return base;
};
