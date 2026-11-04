import React from 'react';
import { AlertTriangle, X } from 'lucide-react';
import type { Item, ItemLifecycleBatchDetail } from '../api';
import { batchStatusClass, batchStatusText } from '../batchState';
import { requiresDeletePhrase } from '../lifecycle';

/** ItemLifecycleConfirmModalProps 描述下架/删除确认弹窗的数据与回调。 */
export interface ItemLifecycleConfirmModalProps {
  /** request 是当前待确认的操作；为空时不渲染。 */
  request: {
    /** action 是本次生命周期动作：downshelf 或 delete。 */
    action: 'downshelf' | 'delete';
    /** items 是本次操作的目标商品集合。 */
    items: Item[];
  } | null;
  /** submitting 表示批次创建请求是否正在执行。 */
  submitting: boolean;
  /** error 是弹窗内展示的错误说明。 */
  error: string;
  /** onClose 关闭确认弹窗。 */
  onClose: () => void;
  /** onSubmit 确认提交批次。 */
  onSubmit: () => void;
}

/** lifecycleActionTitle 把生命周期动作转换为标题文本。 */
const lifecycleActionTitle = (action: 'downshelf' | 'delete'): string => (action === 'delete' ? '删除商品' : '下架商品');

/** ItemLifecycleConfirmModal 展示下架/删除确认弹窗：下架普通确认、删除警告、批量删除需要输入口令。 */
export const ItemLifecycleConfirmModal: React.FC<ItemLifecycleConfirmModalProps> = ({ request, submitting, error, onClose, onSubmit }) => {
  // phrase 是批量删除防呆口令输入值；不需要口令时保持为空。
  const [phrase, setPhrase] = React.useState('');
  // needPhrase 表示本次操作是否需要输入「删除」口令。
  const needPhrase = request ? requiresDeletePhrase(request.action, request.items.length) : false;
  // phraseReady 表示口令校验是否通过；不需要口令时视为通过。
  const phraseReady = !needPhrase || phrase.trim() === '删除';
  // 操作变化时重置口令输入，避免上一次的口令残留到新弹窗。
  React.useEffect(/* phraseResetEffect 在操作变化时清空防呆口令。 */ () => {
    setPhrase('');
  }, [request?.action, request?.items.length]);
  if (!request) return null;
  // titles 是目标商品标题列表，用于展示前几个示例。
  const titles = request.items.map(/* titleItem 取单个目标商品的展示标题。 */ item => item.item_title || item.item_id);
  return (
    <div className="modal-overlay-centered">
      <div className="modal-container" style={{ maxWidth: '480px' }}>
        <div className="modal-header flex items-center justify-between">
          <div>
            <h3 className="text-xl font-extrabold text-gray-900">{lifecycleActionTitle(request.action)}</h3>
            <p className="text-xs text-gray-500 mt-1">共 {request.items.length} 件商品</p>
          </div>
          <button onClick={onClose} className="p-2 rounded-xl hover:bg-gray-100 transition-colors" title="关闭">
            <X className="w-5 h-5 text-gray-500" />
          </button>
        </div>
        <div className="modal-body space-y-4">
          {request.action === 'delete' ? (
            <div className="rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-800 leading-6">
              <div className="flex items-start gap-2">
                <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                <span>
                  删除会直接从闲鱼平台移除商品，操作不可撤销。已下架或不再售卖的商品才建议删除。
                </span>
              </div>
            </div>
          ) : (
            <div className="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800 leading-6">
              下架后商品将不再展示给买家，可随时重新上架。确认要对所选商品执行下架吗？
            </div>
          )}
          <div className="max-h-[160px] overflow-y-auto rounded-xl bg-gray-50 p-3 text-xs text-gray-600 space-y-1">
            {titles.slice(0, 5).map(/* previewTitle 渲染单个目标标题预览。 */ (title, index) => <div key={`${title}-${index}`} className="truncate">· {title}</div>)}
            {titles.length > 5 && <div className="text-gray-400">…等共 {titles.length} 件</div>}
          </div>
          {needPhrase && (
            <div className="space-y-2">
              <label className="block text-sm font-bold text-gray-700" htmlFor="lifecycle-delete-phrase">请输入「删除」以确认批量删除</label>
              <input
                id="lifecycle-delete-phrase"
                className="w-full ios-input px-4 py-3 rounded-xl"
                placeholder="输入：删除"
                value={phrase}
                onChange={/* phraseChangeAction 记录防呆口令输入。 */ event => setPhrase(event.target.value)}
              />
            </div>
          )}
          {error && <div className="rounded-xl bg-red-50 px-3 py-2 text-xs font-bold text-red-700">{error}</div>}
        </div>
        <div className="modal-footer flex gap-3">
          <button onClick={onClose} className="flex-1 px-6 py-3 rounded-xl bg-gray-100 hover:bg-gray-200 text-gray-800 font-bold">取消</button>
          <button
            disabled={submitting || !phraseReady}
            onClick={onSubmit}
            className={`flex-1 px-6 py-3 rounded-xl font-bold text-white disabled:opacity-50 ${request.action === 'delete' ? 'bg-red-600 hover:bg-red-700' : 'bg-gray-900 hover:bg-black'}`}
          >
            {submitting ? '正在提交...' : `确认${request.action === 'delete' ? '删除' : '下架'} ${request.items.length} 件`}
          </button>
        </div>
      </div>
    </div>
  );
};

/** ItemLifecycleBatchModalProps 描述下架/删除批次进度弹窗的数据与回调。 */
export interface ItemLifecycleBatchModalProps {
  /** batch 是当前展示批次的服务端详情；为空表示加载中。 */
  batch: ItemLifecycleBatchDetail | null;
  /** busy 表示弹窗内动作请求或详情读取是否正在执行。 */
  busy: boolean;
  /** onClose 关闭进度弹窗。 */
  onClose: () => void;
  /** onCancel 请求取消当前批次。 */
  onCancel: () => void;
  /** onRetry 重试批次内失败行。 */
  onRetry: () => void;
}

/** ItemLifecycleBatchModal 展示下架/删除批次进度、行明细与取消/重试动作。 */
export const ItemLifecycleBatchModal: React.FC<ItemLifecycleBatchModalProps> = ({ batch, busy, onClose, onCancel, onRetry }) => {
  // actionText 是批次动作的中文标题。
  const actionText = batch?.batch.action === 'delete' ? '删除' : '下架';
  // retryableCount 是当前可重试的失败行数。
  const retryableCount = batch?.rows.filter(/* failedRow 筛选失败状态的行。 */ row => row.status === 'failed').length ?? 0;
  // inProgress 表示批次是否仍在执行或等待执行。
  const inProgress = batch ? ['pending', 'running'].includes(batch.batch.status) : false;
  return (
    <div className="modal-overlay-centered">
      <div className="modal-container" style={{ maxWidth: '720px' }}>
        <div className="modal-header flex items-center justify-between">
          <div>
            <h3 className="text-xl font-extrabold text-gray-900">批量{actionText}任务</h3>
            <p className="text-xs text-gray-500 mt-1">平台请求按账号节流串行执行，请耐心等待进度刷新。</p>
          </div>
          <button onClick={onClose} className="p-2 rounded-xl hover:bg-gray-100 transition-colors" title="关闭">
            <X className="w-5 h-5 text-gray-500" />
          </button>
        </div>
        <div className="modal-body space-y-4">
          {!batch ? (
            <div className="py-10 text-center text-sm text-gray-400">{busy ? '正在加载任务进度...' : '任务详情不可用'}</div>
          ) : (
            <>
              <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                <div className={`rounded-2xl p-4 border ${batchStatusClass(batch.batch.status)}`}>
                  <div className="text-xs font-bold opacity-70">任务状态</div>
                  <div className="text-xl font-extrabold mt-1">{batchStatusText(batch.batch.status)}</div>
                </div>
                <div className="rounded-2xl bg-gray-50 p-4 border border-gray-100">
                  <div className="text-xs font-bold text-gray-500">总数</div>
                  <div className="text-xl font-extrabold text-gray-900 mt-1">{batch.batch.total_count}</div>
                </div>
                <div className="rounded-2xl bg-emerald-50 p-4 border border-emerald-100">
                  <div className="text-xs font-bold text-emerald-700">成功</div>
                  <div className="text-xl font-extrabold text-emerald-700 mt-1">{batch.batch.success_count}</div>
                </div>
                <div className="rounded-2xl bg-red-50 p-4 border border-red-100">
                  <div className="text-xs font-bold text-red-700">失败</div>
                  <div className="text-xl font-extrabold text-red-700 mt-1">{batch.batch.failed_count}</div>
                </div>
              </div>
              <div className="max-h-[360px] overflow-y-auto rounded-2xl border border-gray-100">
                <table className="w-full text-left text-sm">
                  <thead className="sticky top-0 bg-white text-xs text-gray-400 border-b border-gray-100">
                    <tr>
                      <th className="px-4 py-3">序号</th>
                      <th className="px-4 py-3">状态</th>
                      <th className="px-4 py-3">商品</th>
                      <th className="px-4 py-3">说明</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-gray-50">
                    {batch.rows.map(/* detailRow 渲染单行批次明细。 */ row => (
                      <tr key={row.id} className="hover:bg-gray-50">
                        <td className="px-4 py-3 font-mono text-xs">{row.row_no}</td>
                        <td className="px-4 py-3">
                          <span className={`inline-flex px-2 py-1 rounded-lg border text-xs font-extrabold ${batchStatusClass(row.status)}`}>
                            {batchStatusText(row.status)}
                          </span>
                        </td>
                        <td className="px-4 py-3 text-xs text-gray-700 font-mono">{row.cookie_id.slice(0, 6)} · {row.item_id}</td>
                        <td className="px-4 py-3 text-red-600 text-xs max-w-[280px]">{row.error_message || '-'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}
        </div>
        <div className="modal-footer flex gap-3">
          {inProgress && (
            <button disabled={busy} onClick={onCancel} className="flex-1 px-6 py-3.5 rounded-xl bg-gray-900 text-white hover:bg-black font-bold disabled:opacity-50">
              取消任务
            </button>
          )}
          {!inProgress && retryableCount > 0 && (
            <button disabled={busy} onClick={onRetry} className="flex-1 ios-btn-primary px-6 py-3.5 rounded-xl font-bold disabled:opacity-50">
              重试失败项（{retryableCount}）
            </button>
          )}
          {!inProgress && (
            <button onClick={onClose} className="flex-1 px-6 py-3.5 rounded-xl bg-gray-100 hover:bg-gray-200 text-gray-800 font-bold">关闭</button>
          )}
        </div>
      </div>
    </div>
  );
};
