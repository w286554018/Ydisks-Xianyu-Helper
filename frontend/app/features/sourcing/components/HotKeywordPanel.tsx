import React from 'react';
import { Flame,RefreshCw,Sparkles } from 'lucide-react';
import type { HotKeywordSnapshot } from '../types';

/** HotKeywordPanelProps 描述热词面板的展示数据与操作回调。 */
interface HotKeywordPanelProps {
  /** hot 是当日热词快照；为空表示首次加载中。 */
  hot: HotKeywordSnapshot | null;
  /** error 是读取或采集失败原因。 */
  error: string;
  /** refreshing 表示是否正在手动采集。 */
  refreshing: boolean;
  /** onRefresh 手动触发一次热词采集。 */
  onRefresh: () => void;
  /** onSearch 用热词直接执行一次查货。 */
  onSearch: (keyword: string) => void;
  /** onAnalyze 用热词预填品类分析文案。 */
  onAnalyze: (keyword: string) => void;
}

/** HotKeywordPanel 渲染平台当日热词，热词可一键查货或生成品类分析。 */
export const HotKeywordPanel: React.FC<HotKeywordPanelProps> = ({ hot, error, refreshing, onRefresh, onSearch, onAnalyze }) => (
  <section className="ios-card rounded-2xl bg-white p-5 shadow-sm">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div className="flex items-center gap-2">
        <Flame className="h-5 w-5 text-orange-500" />
        <h2 className="text-lg font-extrabold text-gray-900">今日平台热词</h2>
        {hot?.date && <span className="text-xs text-gray-400">{hot.date}</span>}
      </div>
      <button type="button" onClick={onRefresh} disabled={refreshing} className="flex items-center gap-1.5 rounded-xl border border-gray-200 px-3 py-2 text-sm font-bold text-gray-700 hover:bg-gray-50 disabled:opacity-50">
        <RefreshCw className={`h-4 w-4 ${refreshing ? 'animate-spin' : ''}`} />
        {refreshing ? '采集中...' : '立即采集'}
      </button>
    </div>
    <p className="mt-1 text-xs leading-5 text-gray-500">每天北京时间 8 点后自动采集一次，使用游客身份、不带任何账号凭证。点击热词即可查货。</p>
    {error && <div className="mt-3 rounded-xl bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
    {hot === null && !error && <div className="mt-4 text-sm text-gray-400">加载中...</div>}
    {hot?.status === 'pending' && <div className="mt-4 text-sm text-gray-500">今天还没有采集热词，可点击「立即采集」。</div>}
    {hot?.status === 'failed' && <div className="mt-4 rounded-xl bg-amber-50 px-3 py-2 text-sm text-amber-800">本次未取到热词：{hot.errorMessage || '原因未知'}</div>}
    {hot?.status === 'collected' && (
      <div className="mt-4 flex flex-wrap gap-2">
        {hot.keywords.map(/* keyword 是一条平台热词。 */ keyword => (
          <span key={keyword} className="inline-flex items-center overflow-hidden rounded-full border border-orange-200 bg-orange-50 text-sm">
            <button type="button" onClick={/* searchKeyword 用该热词查货。 */ () => onSearch(keyword)} className="px-3 py-1.5 font-bold text-orange-800 hover:bg-orange-100" title="用这个词查在售商品">{keyword}</button>
            <button type="button" onClick={/* analyzeKeyword 用该热词生成品类分析。 */ () => onAnalyze(keyword)} className="border-l border-orange-200 px-2 py-1.5 text-orange-600 hover:bg-orange-100" title="生成品类分析" aria-label={`为${keyword}生成品类分析`}>
              <Sparkles className="h-3.5 w-3.5" />
            </button>
          </span>
        ))}
      </div>
    )}
  </section>
);
