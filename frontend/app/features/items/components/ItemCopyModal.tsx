import { Copy, Loader2 } from 'lucide-react';
import React from 'react';
import type { ItemCopyPreview } from '../models';

/** ItemCopyModalProps 描述跨账号复制弹窗需要的数据与回调。 */
export interface ItemCopyModalProps {
  /** source 是当前要复制的源商品（用于标题展示）。 */
  sourceTitle: string;
  /** preview 是服务端返回的预填内容与目标账号预检；加载中为 null。 */
  preview: ItemCopyPreview | null;
  /** loading 表示预览请求是否正在执行。 */
  loading: boolean;
  /** submitting 表示创建任务请求是否正在执行。 */
  submitting: boolean;
  /** error 是弹窗内展示的错误说明。 */
  error: string;
  /** title 是可编辑的标题。 */
  title: string;
  /** onTitleChange 更新可编辑标题。 */
  onTitleChange: (value: string) => void;
  /** description 是可编辑的描述。 */
  description: string;
  /** onDescriptionChange 更新可编辑描述。 */
  onDescriptionChange: (value: string) => void;
  /** price 是可编辑的售价文本。 */
  price: string;
  /** onPriceChange 更新可编辑售价。 */
  onPriceChange: (value: string) => void;
  /** intervalSeconds 是相邻发布的间隔秒数。 */
  intervalSeconds: number;
  /** onIntervalSecondsChange 更新发布间隔。 */
  onIntervalSecondsChange: (value: number) => void;
  /** selectedTargets 是已勾选的目标账号标识集合。 */
  selectedTargets: string[];
  /** onToggleTarget 切换一个目标账号的勾选状态。 */
  onToggleTarget: (cookieID: string) => void;
  /** onSubmit 提交复制任务。 */
  onSubmit: () => void;
  /** onClose 关闭弹窗。 */
  onClose: () => void;
}

/** ItemCopyModal 展示跨账号复制预览：预填可编辑字段、目标账号预检与发布间隔。 */
const ItemCopyModal: React.FC<ItemCopyModalProps> = ({
  sourceTitle, preview, loading, submitting, error,
  title, onTitleChange, description, onDescriptionChange, price, onPriceChange,
  intervalSeconds, onIntervalSecondsChange,
  selectedTargets, onToggleTarget, onSubmit, onClose,
}) => (
  <div className="modal-overlay-centered">
    <div className="modal-container" style={{maxWidth: '620px'}}>
      <div className="modal-header flex items-center justify-between">
        <div>
          <h3 className="font-bold text-gray-900 flex items-center gap-2"><Copy className="h-4 w-4" />复制发布到其他账号</h3>
          <p className="text-xs text-gray-500 mt-1 line-clamp-1">源商品：{sourceTitle}</p>
        </div>
        <button onClick={onClose} className="p-2 rounded-xl hover:bg-gray-100 transition-colors" title="关闭" aria-label="关闭">×</button>
      </div>
      <div className="modal-body space-y-4">
        {loading && (
          <div className="flex items-center justify-center gap-2 py-8 text-gray-500">
            <Loader2 className="h-4 w-4 animate-spin" />正在读取源商品快照…
          </div>
        )}
        {!loading && error && !preview && (
          <div className="rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>
        )}
        {!loading && preview && (
          <>
            <div className="space-y-3">
              <label className="block">
                <span className="text-xs font-bold text-gray-600">标题</span>
                <input
                  value={title}
                  onChange={/* 输入标题时同步可编辑状态。 */ event => onTitleChange(event.target.value)}
                  className="mt-1 w-full rounded-xl border border-gray-200 px-3 py-2 text-sm focus:border-brand focus:outline-none"
                  maxLength={60}
                />
              </label>
              <label className="block">
                <span className="text-xs font-bold text-gray-600">描述</span>
                <textarea
                  value={description}
                  onChange={/* 输入描述时同步可编辑状态。 */ event => onDescriptionChange(event.target.value)}
                  className="mt-1 w-full rounded-xl border border-gray-200 px-3 py-2 text-sm focus:border-brand focus:outline-none"
                  rows={4}
                />
              </label>
              <div className="grid grid-cols-2 gap-3">
                <label className="block">
                  <span className="text-xs font-bold text-gray-600">价格</span>
                  <input
                    value={price}
                    onChange={/* 输入价格时同步可编辑状态。 */ event => onPriceChange(event.target.value)}
                    className="mt-1 w-full rounded-xl border border-gray-200 px-3 py-2 text-sm focus:border-brand focus:outline-none"
                  />
                </label>
                <label className="block">
                  <span className="text-xs font-bold text-gray-600">发布间隔（秒）</span>
                  <input
                    type="number"
                    min={5}
                    value={intervalSeconds}
                    onChange={/* 输入间隔时同步发布间隔。 */ event => onIntervalSecondsChange(Number(event.target.value) || 0)}
                    className="mt-1 w-full rounded-xl border border-gray-200 px-3 py-2 text-sm focus:border-brand focus:outline-none"
                  />
                </label>
              </div>
              {preview.image_urls.length > 0 && (
                <div>
                  <span className="text-xs font-bold text-gray-600">图片（{preview.image_urls.length} 张，发布时自动下载到本地批次）</span>
                  <div className="mt-1.5 flex gap-2 overflow-x-auto pb-1">
                    {preview.image_urls.map(/* url 表示当前图片地址。 */ url => ( /* url 是当前遍历的图片地址。 */
                      <img key={url} src={url} alt="" className="h-14 w-14 shrink-0 rounded-lg border border-gray-200 object-cover" />
                    ))}
                  </div>
                </div>
              )}
            </div>
            <div>
              <span className="text-xs font-bold text-gray-600">选择目标账号（已选 {selectedTargets.length} 个）</span>
              <div className="mt-1.5 max-h-48 space-y-1.5 overflow-y-auto rounded-xl border border-gray-100 p-2">
                {preview.targets.map(/* target 表示当前预检账号。 */ target => {
                  // checked 表示该账号是否已被勾选。
                  const checked = selectedTargets.includes(target.cookie_id);
                  return (
                    <label
                      key={target.cookie_id}
                      className={`flex cursor-pointer items-center gap-2.5 rounded-lg px-2.5 py-2 text-sm ${!target.usable ? 'bg-amber-50' : 'hover:bg-gray-50'} ${checked ? 'ring-1 ring-brand' : ''}`}
                    >
                      <input
                        type="checkbox"
                        checked={checked}
                        disabled={!target.usable}
                        onChange={/* 切换该目标账号的勾选状态。 */ () => onToggleTarget(target.cookie_id)}
                        className="h-4 w-4"
                      />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate font-bold text-gray-800">{target.name}</span>
                        <span className="block truncate text-[10px] text-gray-400">{target.cookie_id}</span>
                      </span>
                      {!target.usable && target.reason && (
                        <span className="shrink-0 rounded-md bg-amber-100 px-1.5 py-0.5 text-[10px] font-bold text-amber-700">{target.reason}</span>
                      )}
                    </label>
                  );
                })}
              </div>
            </div>
            {error && <div className="rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>}
            <p className="text-[11px] text-gray-400">复制创建的是批量铺货预检批次：图片从平台 CDN 下载到本地，发布走既有批量任务，不会新增平台接口调用。</p>
          </>
        )}
      </div>
      <div className="modal-footer">
        <button onClick={onClose} className="rounded-xl bg-gray-100 px-4 py-2 text-sm font-bold text-gray-600 hover:bg-gray-200">取消</button>
        <button
          onClick={onSubmit}
          disabled={loading || submitting || !preview || selectedTargets.length === 0}
          className="inline-flex items-center gap-1.5 rounded-xl bg-brand px-4 py-2 text-sm font-bold text-white disabled:opacity-40"
        >
          {submitting && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
          创建复制任务{selectedTargets.length > 0 ? `（${selectedTargets.length} 个账号）` : ''}
        </button>
      </div>
    </div>
  </div>
);

export default ItemCopyModal;
