import React from 'react';
import { Flame, RefreshCw, TrendingDown, TrendingUp, Sparkles, Minus } from 'lucide-react';
import type { MomentumEntry, MomentumSummary } from '../types';

/** momentumStatusLabel 把涨幅状态转换为中文标签。 */
const momentumStatusLabel: Record<MomentumEntry['status'], string> = {
  new: '新冒头',
  rising: '上升',
  steady: '稳定',
  falling: '回落',
};

/** momentumStatusClass 把涨幅状态转换为样式。 */
const momentumStatusClass: Record<MomentumEntry['status'], string> = {
  new: 'bg-emerald-50 text-emerald-700 border-emerald-100',
  rising: 'bg-blue-50 text-blue-700 border-blue-100',
  steady: 'bg-gray-100 text-gray-600 border-gray-200',
  falling: 'bg-amber-50 text-amber-700 border-amber-100',
};

/** momentumStatusIcon 把涨幅状态转换为图标。 */
const momentumStatusIcon: Record<MomentumEntry['status'], React.ReactNode> = {
  new: <Sparkles className="h-3 w-3" />,
  rising: <TrendingUp className="h-3 w-3" />,
  steady: <Minus className="h-3 w-3" />,
  falling: <TrendingDown className="h-3 w-3" />,
};

/** MomentumPanelProps 描述热词涨幅面板的数据与回调。 */
export interface MomentumPanelProps {
  /** momentum 是涨幅榜数据；null 表示尚未加载。 */
  momentum: MomentumSummary | null;
  /** loading 表示是否正在加载。 */
  loading: boolean;
  /** error 是加载错误说明。 */
  error: string;
  /** onRefresh 手动刷新涨幅榜。 */
  onRefresh: () => void;
  /** onUseAsKeyword 把热词加入关注词表。 */
  onUseAsKeyword: (keyword: string) => void;
}

/** MomentumPanel 展示热词涨幅榜：新冒头优先，支持一键关注。 */
export const MomentumPanel: React.FC<MomentumPanelProps> = ({ momentum, loading, error, onRefresh, onUseAsKeyword }) => (
  <div className="ios-card rounded-2xl p-5">
    <div className="mb-4 flex items-center justify-between gap-3">
      <div className="flex items-center gap-2">
        <Flame className="h-4 w-4 text-orange-500" />
        <h3 className="text-sm font-extrabold text-gray-900">热词涨幅</h3>
        {momentum?.latestDate && <span className="text-[10px] text-gray-400">最新 {momentum.latestDate} · 窗口 {momentum.windowDays} 天</span>}
      </div>
      <button type="button" onClick={onRefresh} disabled={loading} className="rounded-lg bg-gray-100 px-3 py-1.5 text-xs font-bold text-gray-600 hover:bg-gray-200 disabled:opacity-50">
        <RefreshCw className={`mr-1 inline h-3 w-3 ${loading ? 'animate-spin' : ''}`} />
        刷新
      </button>
    </div>
    {error && <div className="mb-3 rounded-xl bg-red-50 px-3 py-2 text-xs font-bold text-red-700">{error}</div>}
    {!momentum || momentum.items.length === 0 ? (
      <div className="py-6 text-center text-xs text-gray-400">{loading ? '正在加载热词涨幅...' : '暂无热词快照，等待每日采集后展示'}</div>
    ) : (
      <div className="max-h-[320px] space-y-1.5 overflow-y-auto">
        {momentum.items.map(/* momentumEntry 渲染单条涨幅词条。 */ entry => (
          <div key={entry.keyword} className="flex items-center gap-2 rounded-xl bg-gray-50 px-3 py-2">
            <span className="w-6 shrink-0 text-center font-mono text-xs font-bold text-gray-400">{entry.position}</span>
            <span className="min-w-0 flex-1 truncate text-xs font-bold text-gray-800">{entry.keyword}</span>
            <span className={`inline-flex shrink-0 items-center gap-1 rounded-md border px-1.5 py-0.5 text-[10px] font-extrabold ${momentumStatusClass[entry.status]}`}>
              {momentumStatusIcon[entry.status]}
              {momentumStatusLabel[entry.status]}
            </span>
            <span className="shrink-0 text-[10px] text-gray-400">{entry.daysOnList} 天</span>
            <button
              type="button"
              onClick={/* 关注动作把热词加入词表。 */ () => onUseAsKeyword(entry.keyword)}
              className="shrink-0 rounded-md bg-brand px-2 py-1 text-[10px] font-extrabold text-white hover:bg-brand-highlight"
              title="加入关注词表"
            >
              关注
            </button>
          </div>
        ))}
      </div>
    )}
  </div>
);
