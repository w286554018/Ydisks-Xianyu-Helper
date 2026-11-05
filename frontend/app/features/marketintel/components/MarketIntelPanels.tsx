import React from 'react';
import { Eye, Radar, Trash2, Users } from 'lucide-react';
import type { AggregateSummary, SellerBoardData, TrendData, WatchedKeyword } from '../types';

/** KeywordManagerPanelProps 描述关注词管理面板的数据与回调。 */
export interface KeywordManagerPanelProps {
  /** keywords 是词表数据；null 表示尚未加载。 */
  keywords: {
    /** items 是关注词集合。 */
    items: WatchedKeyword[];
    /** maxEnabled 是启用词上限。 */
    maxEnabled: number;
  } | null;
  /** loading 表示是否正在加载。 */
  loading: boolean;
  /** error 是加载错误说明。 */
  error: string;
  /** newKeyword 是新词输入值。 */
  newKeyword: string;
  /** onNewKeywordChange 更新新词输入。 */
  onNewKeywordChange: (value: string) => void;
  /** onAdd 提交新增。 */
  onAdd: () => void;
  /** onToggle 切换启用状态。 */
  onToggle: (item: WatchedKeyword) => void;
  /** onDelete 删除关注词。 */
  onDelete: (item: WatchedKeyword) => void;
  /** onSelect 查看该词趋势。 */
  onSelect: (item: WatchedKeyword) => void;
  /** submitting 表示新增/更新请求是否执行中。 */
  submitting: boolean;
}

/** KeywordManagerPanel 展示关注词表管理与上限提示。 */
export const KeywordManagerPanel: React.FC<KeywordManagerPanelProps> = ({ keywords, loading, error, newKeyword, onNewKeywordChange, onAdd, onToggle, onDelete, onSelect, submitting }) => {
  // enabledCount 是当前启用词数。
  const enabledCount = keywords?.items.filter(/* enabledItem 只统计启用中的词。 */ item => item.enabled).length ?? 0;
  // maxEnabled 是启用词上限。
  const maxEnabled = keywords?.maxEnabled ?? 30;
  return (
    <div className="ios-card rounded-2xl p-5">
      <div className="mb-3 flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Radar className="h-4 w-4 text-brand" />
          <h3 className="text-sm font-extrabold text-gray-900">关注词</h3>
          <span className={`rounded-md px-1.5 py-0.5 text-[10px] font-extrabold ${enabledCount >= maxEnabled ? 'bg-red-50 text-red-700' : 'bg-gray-100 text-gray-500'}`}>
            启用 {enabledCount}/{maxEnabled}
          </span>
        </div>
      </div>
      {error && <div className="mb-3 rounded-xl bg-red-50 px-3 py-2 text-xs font-bold text-red-700">{error}</div>}
      <div className="mb-3 flex gap-2">
        <input
          className="min-w-0 flex-1 ios-input rounded-xl px-3 py-2 text-xs"
          placeholder="输入搜索词，例如：AI课程"
          value={newKeyword}
          onChange={/* 输入动作更新新词草稿。 */ event => onNewKeywordChange(event.target.value)}
          onKeyDown={/* 回车提交新增。 */ event => { if (event.key === 'Enter') { event.preventDefault(); onAdd(); } }}
        />
        <button
          type="button"
          onClick={onAdd}
          disabled={submitting || !newKeyword.trim()}
          className="shrink-0 rounded-xl bg-brand px-4 py-2 text-xs font-extrabold text-white hover:bg-brand-highlight disabled:opacity-50"
        >
          {submitting ? '添加中...' : '添加'}
        </button>
      </div>
      {!keywords || keywords.items.length === 0 ? (
        <div className="py-6 text-center text-xs text-gray-400">{loading ? '正在加载关注词...' : '暂无关注词，添加后每日自动扫描'}</div>
      ) : (
        <div className="max-h-[360px] space-y-1.5 overflow-y-auto">
          {keywords.items.map(/* keywordItem 渲染单个关注词行。 */ item => (
            <div key={item.id} className={`flex items-center gap-2 rounded-xl px-3 py-2 ${item.enabled ? 'bg-gray-50' : 'bg-gray-50/40 opacity-60'}`}>
              <label className="flex shrink-0 cursor-pointer items-center">
                <input
                  type="checkbox"
                  className="h-3.5 w-3.5 rounded border-gray-300"
                  aria-label={`启用关注词：${item.keyword}`}
                  checked={item.enabled}
                  onChange={/* 切换动作交由父级提交。 */ () => onToggle(item)}
                />
              </label>
              <span className="min-w-0 flex-1 truncate text-xs font-bold text-gray-800">{item.keyword}</span>
              <button type="button" onClick={/* 查看趋势动作。 */ () => onSelect(item)} className="shrink-0 rounded-md bg-gray-100 p-1.5 text-gray-500 hover:bg-brand hover:text-white" title="查看趋势" aria-label={`查看趋势：${item.keyword}`}>
                <Eye className="h-3 w-3" />
              </button>
              <button type="button" onClick={/* 删除动作交由父级确认。 */ () => onDelete(item)} className="shrink-0 rounded-md bg-gray-100 p-1.5 text-red-500 hover:bg-red-100" title="删除" aria-label={`删除关注词：${item.keyword}`}>
                <Trash2 className="h-3 w-3" />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

/** KeywordTrendPanelProps 描述趋势面板的数据与回调。 */
export interface KeywordTrendPanelProps {
  /** trend 是趋势数据；null 表示未选择词或未加载。 */
  trend: TrendData | null;
  /** loading 表示是否正在加载。 */
  loading: boolean;
  /** error 是加载错误说明。 */
  error: string;
  /** onClose 关闭趋势视图。 */
  onClose: () => void;
}

/** KeywordTrendPanel 用简单条形图展示单词的 want 总数趋势。 */
export const KeywordTrendPanel: React.FC<KeywordTrendPanelProps> = ({ trend, loading, error, onClose }) => {
  // maxWant 是窗口内最大 want 值，用于归一化条形高度。
  const maxWant = trend ? Math.max(1, ...trend.points.map(/* pointWant 取单个趋势点的 want 值。 */ point => point.totalWant)) : 1;
  return (
    <div className="ios-card rounded-2xl p-5">
      <div className="mb-3 flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Eye className="h-4 w-4 text-brand" />
          <h3 className="text-sm font-extrabold text-gray-900">{trend ? `「${trend.keyword}」需求趋势` : '需求趋势'}</h3>
        </div>
        {trend && (
          <button type="button" onClick={onClose} className="rounded-lg bg-gray-100 px-3 py-1.5 text-xs font-bold text-gray-600 hover:bg-gray-200">关闭</button>
        )}
      </div>
      {error && <div className="mb-3 rounded-xl bg-red-50 px-3 py-2 text-xs font-bold text-red-700">{error}</div>}
      {!trend || trend.points.length === 0 ? (
        <div className="py-6 text-center text-xs text-gray-400">{loading ? '正在加载趋势...' : '暂无趋势数据，等待每日扫描积累'}</div>
      ) : (
        <div className="flex items-end gap-1.5" style={{ height: '120px' }}>
          {trend.points.map(/* trendPoint 渲染单个趋势条。 */ point => (
            <div key={point.date} className="flex min-w-0 flex-1 flex-col items-center gap-1" title={`${point.date}：want ${point.totalWant} · AI 相关 ${point.aiCount}/${point.totalCount}`}>
              <div className="flex w-full flex-1 items-end">
                <div className="w-full rounded-t-md bg-brand/80" style={{ height: `${Math.max(4, Math.round((point.totalWant / maxWant) * 100))}%` }} />
              </div>
              <span className="w-full truncate text-center text-[9px] text-gray-400">{point.date.slice(5)}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

/** SellerBoardPanelProps 描述卖家榜面板的数据与回调。 */
export interface SellerBoardPanelProps {
  /** board 是卖家榜数据；null 表示尚未加载。 */
  board: SellerBoardData | null;
  /** loading 表示是否正在加载。 */
  loading: boolean;
  /** error 是加载错误说明。 */
  error: string;
}

/** SellerBoardPanel 展示 AI 类目竞品卖家榜。 */
export const SellerBoardPanel: React.FC<SellerBoardPanelProps> = ({ board, loading, error }) => (
  <div className="ios-card rounded-2xl p-5">
    <div className="mb-3 flex items-center gap-2">
      <Users className="h-4 w-4 text-brand" />
      <h3 className="text-sm font-extrabold text-gray-900">竞品卖家</h3>
      {board && <span className="text-[10px] text-gray-400">近 {board.days} 天</span>}
    </div>
    {error && <div className="mb-3 rounded-xl bg-red-50 px-3 py-2 text-xs font-bold text-red-700">{error}</div>}
    {!board || board.sellers.length === 0 ? (
      <div className="py-6 text-center text-xs text-gray-400">{loading ? '正在加载卖家榜...' : '暂无卖家数据，等待每日扫描积累'}</div>
    ) : (
      <div className="max-h-[320px] space-y-1.5 overflow-y-auto">
        {board.sellers.map(/* sellerRow 渲染单个卖家行。 */ (seller, index) => (
          <div key={seller.nickname} className="flex items-center gap-2 rounded-xl bg-gray-50 px-3 py-2">
            <span className="w-6 shrink-0 text-center font-mono text-xs font-bold text-gray-400">{index + 1}</span>
            <span className="min-w-0 flex-1 truncate text-xs font-bold text-gray-800">{seller.nickname}</span>
            <span className="shrink-0 text-[10px] text-gray-500">{seller.appearances} 次</span>
            {seller.avgPrice && <span className="shrink-0 text-[10px] text-gray-400">均价 ¥{seller.avgPrice}</span>}
          </div>
        ))}
      </div>
    )}
  </div>
);

/** KeywordAggregatesTableProps 描述各词最新聚合表的数据。 */
export interface KeywordAggregatesTableProps {
  /** aggregates 是各词最新聚合摘要。 */
  aggregates: AggregateSummary[];
  /** loading 表示是否正在加载。 */
  loading: boolean;
}

/** KeywordAggregatesTable 展示各关注词最新一天的扫描摘要。 */
export const KeywordAggregatesTable: React.FC<KeywordAggregatesTableProps> = ({ aggregates, loading }) => (
  <div className="ios-card overflow-x-auto rounded-2xl">
    <table className="w-full min-w-[560px] text-left text-xs">
      <thead className="bg-gray-50 text-[11px] font-extrabold text-gray-500">
        <tr>
          <th className="px-4 py-3">关注词</th>
          <th className="px-4 py-3">日期</th>
          <th className="px-4 py-3">商品数</th>
          <th className="px-4 py-3">AI 相关</th>
          <th className="px-4 py-3">want 总数</th>
          <th className="px-4 py-3">判定</th>
        </tr>
      </thead>
      <tbody className="divide-y divide-gray-100">
        {aggregates.length === 0 ? (
          <tr><td colSpan={6} className="px-4 py-8 text-center text-gray-400">{loading ? '正在加载扫描摘要...' : '暂无扫描数据，等待每日扫描积累'}</td></tr>
        ) : aggregates.map(/* aggregateRow 渲染单行聚合摘要。 */ aggregate => (
          <tr key={aggregate.keyword} className="hover:bg-gray-50/60">
            <td className="px-4 py-3 font-bold text-gray-900">{aggregate.keyword}</td>
            <td className="px-4 py-3 text-gray-500">{aggregate.date}</td>
            <td className="px-4 py-3 text-gray-700">{aggregate.totalCount}</td>
            <td className="px-4 py-3 font-bold text-emerald-600">{aggregate.aiCount}</td>
            <td className="px-4 py-3 font-bold text-gray-900">{aggregate.totalWant}</td>
            <td className="px-4 py-3">
              <span className={`inline-flex rounded-md border px-1.5 py-0.5 text-[10px] font-extrabold ${aggregate.verdictStatus === 'done' ? 'bg-emerald-50 text-emerald-700 border-emerald-100' : 'bg-amber-50 text-amber-700 border-amber-100'}`}>
                {aggregate.verdictStatus === 'done' ? '已判定' : '待判定'}
              </span>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  </div>
);
