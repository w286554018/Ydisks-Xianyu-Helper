import React from 'react';
import { Copy,Sparkles } from 'lucide-react';
import { copywritingTitleAndBody } from '../models';
import type { CopywritingMode } from '../types';

/** CopywritingPanelProps 描述 AI 文案面板的输入、结果与操作回调。 */
interface CopywritingPanelProps {
  /** mode 是文案模式。 */
  mode: CopywritingMode;
  /** onModeChange 切换文案模式。 */
  onModeChange: (mode: CopywritingMode) => void;
  /** topic 是品类关键词或商品标题。 */
  topic: string;
  /** onTopicChange 更新主题。 */
  onTopicChange: (value: string) => void;
  /** reference 是参考信息。 */
  reference: string;
  /** onReferenceChange 更新参考信息。 */
  onReferenceChange: (value: string) => void;
  /** content 是生成结果。 */
  content: string;
  /** generating 表示是否正在生成。 */
  generating: boolean;
  /** error 是生成失败原因。 */
  error: string;
  /** onGenerate 生成一次文案。 */
  onGenerate: () => void;
  /** onCopy 把文本复制到剪贴板。 */
  onCopy: (text: string) => void;
}

/** MODE_OPTIONS 是文案模式及其说明。 */
const MODE_OPTIONS: ReadonlyArray<{ /** value 是模式标识。 */ value: CopywritingMode; /** label 是按钮文本。 */ label: string }> = [
  { value: 'listing', label: '上架文案' },
  { value: 'analysis', label: '品类分析' },
];

/** CopywritingPanel 渲染 AI 文案生成：上架文案可分别复制标题与描述到发布页。 */
export const CopywritingPanel: React.FC<CopywritingPanelProps> = ({
  mode, onModeChange, topic, onTopicChange, reference, onReferenceChange, content, generating, error, onGenerate, onCopy,
}) => {
  // parts 是上架文案拆分出的标题与描述，仅在上架模式下使用。
  const parts = copywritingTitleAndBody(content);
  return (
    <section className="ios-card rounded-2xl bg-white p-5 shadow-sm">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Sparkles className="h-5 w-5 text-indigo-500" />
          <h2 className="text-lg font-extrabold text-gray-900">AI 文案</h2>
        </div>
        <div className="flex rounded-xl bg-gray-100 p-1">
          {MODE_OPTIONS.map(/* option 是一个文案模式选项。 */ option => (
            <button key={option.value} type="button" onClick={/* selectMode 切换到该模式。 */ () => onModeChange(option.value)} className={`rounded-lg px-3 py-1 text-sm font-bold ${mode === option.value ? 'bg-white text-gray-900 shadow-sm' : 'text-gray-500'}`}>
              {option.label}
            </button>
          ))}
        </div>
      </div>
      <p className="mt-1 text-xs leading-5 text-gray-500">使用「系统与AI」中配置的 AI 接口。上架文案第一行是标题，其余是描述；品类分析给出卖点、定价区间与注意事项。</p>
      <div className="mt-4 space-y-3">
        <input className="ios-input w-full rounded-xl px-4 py-2.5" placeholder="品类关键词或商品标题，例如：蓝牙耳机" value={topic} onChange={/* changeTopic 更新文案主题。 */ event => onTopicChange(event.target.value)} />
        <textarea className="ios-input h-20 w-full resize-none rounded-xl px-4 py-2.5" placeholder="参考信息（可选），例如：售价 19 元，485 人想要；九成新，配件齐全" value={reference} onChange={/* changeReference 更新参考信息。 */ event => onReferenceChange(event.target.value)} />
        <button type="button" onClick={onGenerate} disabled={generating} className="ios-btn-primary w-full rounded-xl px-5 py-2.5 font-bold disabled:opacity-50">{generating ? '生成中...' : '生成文案'}</button>
      </div>
      {error && <div className="mt-3 rounded-xl bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
      {content && mode === 'listing' && (
        <div className="mt-4 space-y-3">
          <div className="rounded-xl border border-gray-200 p-3">
            <div className="mb-1 flex items-center justify-between text-xs font-bold text-gray-500">
              <span>标题</span>
              <button type="button" onClick={/* copyTitle 复制标题。 */ () => onCopy(parts.title)} className="flex items-center gap-1 text-indigo-600 hover:text-indigo-800"><Copy className="h-3.5 w-3.5" />复制</button>
            </div>
            <div className="text-sm font-bold text-gray-900">{parts.title}</div>
          </div>
          <div className="rounded-xl border border-gray-200 p-3">
            <div className="mb-1 flex items-center justify-between text-xs font-bold text-gray-500">
              <span>描述</span>
              <button type="button" onClick={/* copyBody 复制描述。 */ () => onCopy(parts.body)} className="flex items-center gap-1 text-indigo-600 hover:text-indigo-800"><Copy className="h-3.5 w-3.5" />复制</button>
            </div>
            <div className="whitespace-pre-wrap text-sm leading-6 text-gray-800">{parts.body}</div>
          </div>
        </div>
      )}
      {content && mode === 'analysis' && (
        <div className="mt-4 rounded-xl border border-gray-200 p-3">
          <div className="mb-1 flex items-center justify-between text-xs font-bold text-gray-500">
            <span>品类分析</span>
            <button type="button" onClick={/* copyAll 复制全部分析。 */ () => onCopy(content)} className="flex items-center gap-1 text-indigo-600 hover:text-indigo-800"><Copy className="h-3.5 w-3.5" />复制</button>
          </div>
          <div className="whitespace-pre-wrap text-sm leading-6 text-gray-800">{content}</div>
        </div>
      )}
    </section>
  );
};
