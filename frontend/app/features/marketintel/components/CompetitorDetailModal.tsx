import React from 'react';
import { Loader2, Sparkles, Wand2, X } from 'lucide-react';
import type { CompetitorDetailData, RewrittenCopyData } from '../api';

/** CompetitorDetailModalProps 描述竞品详情弹窗的数据与回调。 */
export interface CompetitorDetailModalProps {
  /** open 表示弹窗是否打开。 */
  open: boolean;
  /** detail 是竞品详情；null 表示加载中。 */
  detail: CompetitorDetailData | null;
  /** loading 表示详情抓取中。 */
  loading: boolean;
  /** error 是弹窗内错误说明。 */
  error: string;
  /** rewritten 是 AI 改写结果。 */
  rewritten: RewrittenCopyData | null;
  /** rewriting 表示 AI 改写中。 */
  rewriting: boolean;
  /** analysis 是 AI 分析结果。 */
  analysis: string;
  /** analyzing 表示 AI 分析中。 */
  analyzing: boolean;
  /** carrying 表示搬运中。 */
  carrying: boolean;
  /** carryResult 是搬运结果。 */
  carryResult: {
    /** title 是预填标题。 */
    title: string;
    /** desc 是预填描述。 */
    desc: string;
    /** price 是预填价格。 */
    price: string;
    /** images 是资源库图片。 */
    images: { /** id 是资源库图片 ID。 */ id: number; /** filename 是文件名。 */ filename: string }[];
    /** sameImagesHint 是图片相同提示。 */
    sameImagesHint: string;
  } | null;
  /** onClose 关闭弹窗。 */
  onClose: () => void;
  /** onRewrite 触发 AI 改写。 */
  onRewrite: () => void;
  /** onAnalyze 触发 AI 分析。 */
  onAnalyze: () => void;
  /** onCarry 触发一键搬运。 */
  onCarry: () => void;
}

/** CompetitorDetailModal 展示竞品详情、AI 改写/分析与搬运动作。 */
export const CompetitorDetailModal: React.FC<CompetitorDetailModalProps> = ({ open, detail, loading, error, rewritten, rewriting, analysis, analyzing, carrying, carryResult, onClose, onRewrite, onAnalyze, onCarry }) => {
  if (!open) return null;
  // displayTitle 是当前展示标题：改写结果优先。
  const displayTitle = rewritten?.title ?? detail?.title ?? '';
  // displayDesc 是当前展示描述：改写结果优先。
  const displayDesc = rewritten?.desc ?? detail?.descText ?? '';
  return (
    <div className="modal-overlay-centered">
      <div className="modal-container" style={{ maxWidth: '720px', maxHeight: '85vh', overflowY: 'auto' }}>
        <div className="modal-header flex items-center justify-between">
          <div>
            <h3 className="text-xl font-extrabold text-gray-900">竞品详情</h3>
            {detail?.cached && <p className="text-xs text-gray-400 mt-1">来自缓存（7 天内）</p>}
          </div>
          <button onClick={onClose} className="p-2 rounded-xl hover:bg-gray-100 transition-colors" title="关闭">
            <X className="w-5 h-5 text-gray-500" />
          </button>
        </div>
        <div className="modal-body space-y-4">
          {loading && (
            <div className="flex items-center justify-center gap-2 py-10 text-sm text-gray-500">
              <Loader2 className="h-4 w-4 animate-spin" />
              正在打开竞品详情页抓取数据（约 10-20 秒）...
            </div>
          )}
          {error && <div className="rounded-xl bg-red-50 px-3 py-2 text-xs font-bold text-red-700">{error}</div>}
          {!loading && detail && (
            <>
              <div className="rounded-2xl bg-gray-50 p-4 space-y-2">
                <div className="flex items-center justify-between gap-3">
                  <span className="text-xs font-bold text-gray-500">价格</span>
                  <span className="text-sm font-extrabold text-red-600">{detail.price || '-'}</span>
                </div>
                <div className="flex items-center justify-between gap-3">
                  <span className="text-xs font-bold text-gray-500">卖家</span>
                  <span className="text-xs font-bold text-gray-700">{detail.sellerNickname || '-'}</span>
                </div>
              </div>
              <div>
                <div className="mb-1.5 flex items-center justify-between gap-2">
                  <span className="text-xs font-extrabold text-gray-700">标题{rewritten && <span className="ml-1 text-emerald-600">（AI 改写）</span>}</span>
                  {rewritten && <span className="text-[10px] text-gray-400">原文见下方灰字</span>}
                </div>
                <div className="rounded-xl bg-white border border-gray-100 p-3 text-xs font-bold text-gray-900">{displayTitle}</div>
                {!rewritten && <div className="mt-1 text-[10px] text-gray-400">原文：{detail.title}</div>}
              </div>
              <div>
                <div className="mb-1.5 text-xs font-extrabold text-gray-700">描述{rewritten && <span className="ml-1 text-emerald-600">（AI 改写）</span>}</div>
                <div className="rounded-xl bg-white border border-gray-100 p-3 text-xs text-gray-700 whitespace-pre-wrap max-h-[160px] overflow-y-auto">{displayDesc || '-'}</div>
                {!rewritten && detail.descText && <div className="mt-1 text-[10px] text-gray-400">原文：{detail.descText.slice(0, 120)}{detail.descText.length > 120 ? '...' : ''}</div>}
              </div>
              {detail.images.length > 0 && (
                <div>
                  <div className="mb-1.5 text-xs font-extrabold text-gray-700">图片（{detail.images.length} 张）</div>
                  <div className="grid grid-cols-5 gap-1.5">
                    {detail.images.slice(0, 10).map(/* detailImage 渲染单张竞品图片。 */ (img, i) => <img key={`${img}-${i}`} src={img} alt="" className="aspect-square w-full rounded-lg object-cover" />)}
                  </div>
                </div>
              )}
              {analysis && (
                <div className="rounded-2xl border border-blue-100 bg-blue-50 p-4 text-xs text-blue-900 whitespace-pre-wrap leading-5">
                  <div className="mb-1 font-extrabold">AI 卖点分析</div>
                  {analysis}
                </div>
              )}
              {carryResult && (
                <div className="rounded-2xl border border-emerald-100 bg-emerald-50 p-4 text-xs text-emerald-900">
                  <div className="font-extrabold">搬运完成：{carryResult.images.length} 张图片已入资源库</div>
                  <div className="mt-1">{carryResult.sameImagesHint}</div>
                  <div className="mt-1">已为你打开发布表单，确认后即可发布。</div>
                </div>
              )}
            </>
          )}
        </div>
        {detail && !loading && (
          <div className="modal-footer flex gap-2 flex-wrap">
            <button
              type="button"
              onClick={onRewrite}
              disabled={rewriting}
              className="rounded-xl bg-brand px-4 py-2.5 text-xs font-extrabold text-white hover:bg-brand-highlight disabled:opacity-50"
            >
              <Wand2 className="mr-1 inline h-3.5 w-3.5" />
              {rewriting ? 'AI 改写中...' : rewritten ? '重新改写' : 'AI 改写文案'}
            </button>
            <button
              type="button"
              onClick={onAnalyze}
              disabled={analyzing}
              className="rounded-xl bg-blue-600 px-4 py-2.5 text-xs font-extrabold text-white hover:bg-blue-700 disabled:opacity-50"
            >
              <Sparkles className="mr-1 inline h-3.5 w-3.5" />
              {analyzing ? 'AI 分析中...' : 'AI 卖点分析'}
            </button>
            <button
              type="button"
              onClick={onCarry}
              disabled={carrying}
              className="ml-auto rounded-xl bg-gray-900 px-4 py-2.5 text-xs font-extrabold text-white hover:bg-black disabled:opacity-50"
            >
              {carrying ? <Loader2 className="mr-1 inline h-3.5 w-3.5 animate-spin" /> : null}
              {carrying ? '搬运中（下载图片）...' : '一键搬运到发布表单'}
            </button>
          </div>
        )}
      </div>
    </div>
  );
};
