import React from 'react';
import { Sparkles,Trophy } from 'lucide-react';
import { formatRevenue } from '../models';
import type { RankingEntry,RankingWindow } from '../types';

/** ShopRankingPanelProps 描述本店畅销榜面板的数据与操作回调。 */
interface ShopRankingPanelProps {
  /** window 是当前统计窗口。 */
  window: RankingWindow;
  /** onWindowChange 切换统计窗口。 */
  onWindowChange: (window: RankingWindow) => void;
  /** entries 是排行条目。 */
  entries: RankingEntry[];
  /** loading 表示是否正在读取。 */
  loading: boolean;
  /** error 是读取失败原因。 */
  error: string;
  /** onWriteCopy 用畅销商品预填上架文案。 */
  onWriteCopy: (entry: RankingEntry) => void;
}

/** WINDOW_OPTIONS 是可选的统计窗口及其展示文本。 */
const WINDOW_OPTIONS: ReadonlyArray<{ /** value 是窗口标识。 */ value: RankingWindow; /** label 是按钮文本。 */ label: string }> = [
  { value: '7d', label: '近 7 天' },
  { value: '30d', label: '近 30 天' },
];

/** ShopRankingPanel 渲染本店畅销榜；数据只来自本地订单，不访问闲鱼。 */
export const ShopRankingPanel: React.FC<ShopRankingPanelProps> = ({ window, onWindowChange, entries, loading, error, onWriteCopy }) => (
  <section className="ios-card rounded-2xl bg-white p-5 shadow-sm">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div className="flex items-center gap-2">
        <Trophy className="h-5 w-5 text-amber-500" />
        <h2 className="text-lg font-extrabold text-gray-900">我的畅销榜</h2>
      </div>
      <div className="flex rounded-xl bg-gray-100 p-1">
        {WINDOW_OPTIONS.map(/* option 是一个统计窗口选项。 */ option => (
          <button key={option.value} type="button" onClick={/* selectWindow 切换到该窗口。 */ () => onWindowChange(option.value)} className={`rounded-lg px-3 py-1 text-sm font-bold ${window === option.value ? 'bg-white text-gray-900 shadow-sm' : 'text-gray-500'}`}>
            {option.label}
          </button>
        ))}
      </div>
    </div>
    <p className="mt-1 text-xs leading-5 text-gray-500">按售出件数排序，只统计已付款之后的订单，数据来自本地订单记录。</p>
    {error && <div className="mt-3 rounded-xl bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
    {loading && <div className="mt-4 text-sm text-gray-400">加载中...</div>}
    {!loading && !error && entries.length === 0 && <div className="mt-4 text-sm text-gray-500">这个时间段还没有有效订单。先在订单管理同步订单后再来看。</div>}
    {!loading && entries.length > 0 && (
      <ol className="mt-4 space-y-2">
        {entries.map(/* entry 是一条排行；index 决定名次。 */ (entry, index /* index 是零基名次。 */) => (
          <li key={entry.itemId} className="flex items-center gap-3 rounded-xl bg-gray-50 px-3 py-2">
            <span className={`w-6 text-center text-sm font-extrabold ${index < 3 ? 'text-amber-600' : 'text-gray-400'}`}>{index + 1}</span>
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-bold text-gray-800">{entry.itemTitle || entry.itemId}</div>
              <div className="text-xs text-gray-500">售出 {entry.soldQuantity} 件 · {entry.orderCount} 单 · 营收 ¥{formatRevenue(entry.revenue)}</div>
            </div>
            <button type="button" onClick={/* writeCopy 用该畅销商品预填上架文案。 */ () => onWriteCopy(entry)} className="flex items-center gap-1 rounded-lg bg-indigo-50 px-2 py-1 text-xs font-bold text-indigo-700 hover:bg-indigo-100">
              <Sparkles className="h-3.5 w-3.5" />写文案
            </button>
          </li>
        ))}
      </ol>
    )}
  </section>
);
