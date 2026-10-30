import React from 'react';
import { ExternalLink,Search,Sparkles } from 'lucide-react';
import type { SearchResultItem,SourcingAccount } from '../types';

/** ItemSearchPanelProps 描述关键词查货面板的输入、结果与操作回调。 */
interface ItemSearchPanelProps {
  /** accounts 是可用于查货的账号。 */
  accounts: SourcingAccount[];
  /** accountId 是当前选择的账号。 */
  accountId: string;
  /** onAccountChange 切换查货账号。 */
  onAccountChange: (id: string) => void;
  /** keyword 是关键词输入。 */
  keyword: string;
  /** onKeywordChange 更新关键词输入。 */
  onKeywordChange: (value: string) => void;
  /** results 是最近一次查货结果。 */
  results: SearchResultItem[];
  /** searchedKeyword 是 results 对应的关键词。 */
  searchedKeyword: string;
  /** searching 表示是否正在查货。 */
  searching: boolean;
  /** error 是查货失败原因。 */
  error: string;
  /** onSearch 执行一次查货。 */
  onSearch: () => void;
  /** onWriteCopy 用某个商品预填上架文案。 */
  onWriteCopy: (item: SearchResultItem) => void;
}

/** ItemSearchPanel 渲染关键词查货：每次点击只发起一次搜索，返回最多 30 条在售商品。 */
export const ItemSearchPanel: React.FC<ItemSearchPanelProps> = ({
  accounts, accountId, onAccountChange, keyword, onKeywordChange, results, searchedKeyword, searching, error, onSearch, onWriteCopy,
}) => (
  <section className="ios-card rounded-2xl bg-white p-5 shadow-sm">
    <div className="flex items-center gap-2">
      <Search className="h-5 w-5 text-blue-500" />
      <h2 className="text-lg font-extrabold text-gray-900">关键词查货</h2>
    </div>
    <p className="mt-1 text-xs leading-5 text-gray-500">用你的账号登录态打开一次闲鱼搜索页，最多返回 30 条。同一关键词 10 秒内不能重复查询；遇到风控会立即停止，请先到闲鱼网页手动完成验证。</p>
    <form
      className="mt-4 flex flex-col gap-2 sm:flex-row"
      onSubmit={/* submitSearch 阻止表单默认提交并执行一次查货。 */ event => { event.preventDefault(); onSearch(); }}
    >
      <select className="ios-input rounded-xl px-3 py-2.5 sm:w-56" value={accountId} onChange={/* changeAccount 切换查货账号。 */ event => onAccountChange(event.target.value)} aria-label="查货使用的账号">
        {accounts.length === 0 && <option value="">暂无账号</option>}
        {accounts.map(/* account 是一个可选账号。 */ account => <option key={account.id} value={account.id}>{account.label}</option>)}
      </select>
      <input className="ios-input flex-1 rounded-xl px-4 py-2.5" placeholder="例如：蓝牙耳机" value={keyword} onChange={/* changeKeyword 更新关键词输入。 */ event => onKeywordChange(event.target.value)} />
      <button type="submit" disabled={searching} className="ios-btn-primary rounded-xl px-5 py-2.5 font-bold disabled:opacity-50">{searching ? '查询中...' : '查一下'}</button>
    </form>
    {error && <div className="mt-3 rounded-xl bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>}
    {searchedKeyword && !searching && results.length === 0 && !error && <div className="mt-4 text-sm text-gray-500">“{searchedKeyword}”没有取到在售商品。</div>}
    {results.length > 0 && (
      <div className="mt-4 overflow-x-auto">
        <div className="mb-2 text-xs text-gray-400">“{searchedKeyword}”共 {results.length} 条</div>
        <table className="w-full min-w-[640px] text-sm">
          <thead>
            <tr className="border-b border-gray-100 text-left text-xs text-gray-500">
              <th className="py-2 pr-3">商品</th>
              <th className="py-2 pr-3">价格</th>
              <th className="py-2 pr-3">想要</th>
              <th className="py-2 pr-3">地区</th>
              <th className="py-2">操作</th>
            </tr>
          </thead>
          <tbody>
            {results.map(/* item 是一条查货结果。 */ (item, index /* index 用于商品标识缺失时生成稳定键。 */) => (
              <tr key={item.itemId || `row-${index}`} className="border-b border-gray-50 align-top">
                <td className="max-w-md py-2 pr-3 text-gray-800">
                  <div className="line-clamp-2">{item.title}</div>
                </td>
                <td className="py-2 pr-3 font-bold text-red-600">{item.price ? `¥${item.price}` : '-'}</td>
                <td className="py-2 pr-3 text-gray-700">{item.wantCount || '-'}</td>
                <td className="py-2 pr-3 text-gray-500">{item.location || '-'}</td>
                <td className="py-2">
                  <div className="flex items-center gap-2">
                    {item.detailUrl && (
                      <a href={item.detailUrl} target="_blank" rel="noreferrer" className="text-gray-400 hover:text-blue-600" title="在闲鱼打开" aria-label="在闲鱼打开">
                        <ExternalLink className="h-4 w-4" />
                      </a>
                    )}
                    <button type="button" onClick={/* writeCopy 用该商品预填上架文案。 */ () => onWriteCopy(item)} className="flex items-center gap-1 rounded-lg bg-indigo-50 px-2 py-1 text-xs font-bold text-indigo-700 hover:bg-indigo-100">
                      <Sparkles className="h-3.5 w-3.5" />写文案
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )}
  </section>
);
