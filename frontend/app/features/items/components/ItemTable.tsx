import { Archive,CheckCircle2,CircleDashed,Copy,Edit,Link2,RefreshCw,Trash2 } from 'lucide-react';
import React from 'react';
import type { Item,ItemAIOverride,ShippingRule } from '../api';
import { countTriggerRules,TRADE_AUTOMATION_TRIGGERS } from '../ruleCoverage';
import { statsTooltip } from '../stats';

/** itemRowKey 计算商品行的稳定选择键。 */
const itemRowKey = (item: Item): string => `${item.cookie_id}-${item.item_id}`;

/** ItemTableProps 描述商品列表视图需要的数据与回调。 */
export interface ItemTableProps {
  /** items 是当前页要展示的商品行。 */
  items: Item[];
  /** rules 是全部自动化规则，用于计算每个商品的各类规则覆盖情况。 */
  rules: ShippingRule[];
  /** accountNickname 把账号标识转换为展示昵称。 */
  accountNickname: (cookieId: string) => string;
  /** formatPrice 把商品价格转换为展示文本。 */
  formatPrice: (price?: string) => string;
  /** aiStatusText 是商品级 AI 三态对应的展示文本。 */
  aiStatusText: Record<ItemAIOverride, string>;
  /** onConfigureDelivery 打开该商品的发货规则配置。 */
  onConfigureDelivery: (item: Item) => void;
  /** onOpenAISettings 打开该商品的 AI 客服配置。 */
  onOpenAISettings: (item: Item) => void;
  /** onEdit 打开商品编辑弹窗。 */
  onEdit: (item: Item) => void;
  /** onCopyToAccounts 打开该商品的跨账号复制发布弹窗。 */
  onCopyToAccounts: (item: Item) => void;
  /** onDownshelf 打开该商品的平台下架确认。 */
  onDownshelf: (item: Item) => void;
  /** onDelete 删除商品。 */
  onDelete: (item: Item) => void;
  /** selectedKeys 是已勾选商品的选择键集合；缺省时隐藏多选列。 */
  selectedKeys?: string[];
  /** onToggleSelect 切换一个商品的勾选状态；与 selectedKeys 同时提供才启用多选。 */
  onToggleSelect?: (item: Item) => void;
  /** onRefreshStats 刷新单个商品的浏览量/想要量统计；缺省时不显示行内刷新按钮。 */
  onRefreshStats?: (item: Item) => void;
  /** refreshingKeys 是正在刷新中的商品行键集合，用于禁用对应行内按钮。 */
  refreshingKeys?: string[];
  /** emptyText 是没有商品时的提示文案。 */
  emptyText: string;
}

/** ItemTable 以表格形式展示商品，并逐列标出四类交易自动化规则的配置数量。 */
const ItemTable: React.FC<ItemTableProps> = ({ items, rules, accountNickname, formatPrice, aiStatusText, onConfigureDelivery, onOpenAISettings, onEdit, onCopyToAccounts, onDownshelf, onDelete, selectedKeys, onToggleSelect, onRefreshStats, refreshingKeys, emptyText }) => {
  if (items.length === 0) return <div className="py-20 text-center text-gray-400">{emptyText}</div>;
  return (
    <div className="ios-card overflow-x-auto rounded-2xl">
      <table className="w-full min-w-[960px] text-left text-xs">
        <thead className="bg-gray-50 text-[11px] font-extrabold text-gray-500">
          <tr>
            {onToggleSelect && <th className="w-10 px-2 py-3" aria-label="批量选择列" />}
            <th className="px-4 py-3">商品</th>
            <th className="px-4 py-3">账号</th>
            <th className="px-4 py-3">价格</th>
            <th className="px-4 py-3">浏览</th>
            <th className="px-4 py-3">想要</th>
            {TRADE_AUTOMATION_TRIGGERS.map(/* entry 是一类交易自动化触发器列。 */ entry => <th key={entry.trigger} className="px-4 py-3">{entry.label}</th>)}
            <th className="px-4 py-3">AI 客服</th>
            <th className="px-4 py-3 text-right">操作</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-100">
          {items.map(/* item 是当前行对应的商品。 */ item => {
            // rowKey 是当前行的稳定选择键。
            const rowKey = itemRowKey(item);
            // isDown 表示当前商品是否已在本地下架。
            const isDown = item.status === 'down';
            return (
              <tr key={rowKey} className="hover:bg-gray-50/60">
                {onToggleSelect && (
                  <td className="px-2 py-3">
                    <input
                      type="checkbox"
                      className="h-4 w-4 rounded border-gray-300"
                      aria-label={`选择商品：${item.item_title || item.item_id}`}
                      checked={(selectedKeys || []).includes(rowKey)}
                      onChange={/* 选择切换回调把当前行交给父级维护勾选集合。 */ () => onToggleSelect(item)}
                    />
                  </td>
                )}
                <td className="max-w-[280px] px-4 py-3">
                  <div className="line-clamp-2 font-bold text-gray-900">{item.item_title}{isDown && <span className="ml-2 inline-flex items-center rounded-md bg-gray-200 px-1.5 py-0.5 align-middle text-[10px] font-extrabold text-gray-600">已下架</span>}</div>
                  <div className="mt-1 select-all text-[10px] text-gray-400">ID: {item.item_id}</div>
                </td>
                <td className="px-4 py-3 text-gray-600">{accountNickname(item.cookie_id)}</td>
                <td className="px-4 py-3 text-gray-700">{formatPrice(item.item_price)}</td>
                <td className="px-4 py-3 text-gray-700" title={item.stats ? statsTooltip(item.stats) : '尚未抓取统计，可点击行内刷新'}>{item.stats ? item.stats.browse_cnt : '—'}</td>
                <td className="px-4 py-3 text-gray-700" title={item.stats ? statsTooltip(item.stats) : '尚未抓取统计，可点击行内刷新'}>{item.stats ? item.stats.want_cnt : '—'}</td>
                {TRADE_AUTOMATION_TRIGGERS.map(/* entry 是一类交易自动化触发器列。 */ entry => {
                  // ruleCount 是该商品在此触发器下生效的规则条数，0 表示未配置。
                  const ruleCount = countTriggerRules(item, entry.trigger, rules);
                  return (
                    <td key={entry.trigger} className="px-4 py-3">
                      <span className={`inline-flex items-center gap-1 font-bold ${ruleCount > 0 ? 'text-emerald-600' : 'text-amber-600'}`}>
                        {ruleCount > 0 ? <CheckCircle2 className="h-3 w-3" /> : <CircleDashed className="h-3 w-3" />}
                        {ruleCount > 0 ? `${ruleCount} 条` : '未配置'}
                      </span>
                    </td>
                  );
                })}
                <td className="px-4 py-3">
                  <button type="button" onClick={/* 点击打开该商品的 AI 客服配置。 */ () => onOpenAISettings(item)} className="font-bold text-gray-600 hover:text-brand" aria-label={`配置商品 AI 客服：${item.item_title || item.item_id}`}>
                    {aiStatusText[item.ai_override || 'inherit']}
                  </button>
                </td>
                <td className="px-4 py-3">
                  <div className="flex justify-end gap-1.5">
                    <button type="button" onClick={/* 点击打开该商品的跨账号复制发布。 */ () => onCopyToAccounts(item)} disabled={isDown} className="rounded-lg bg-gray-100 p-1.5 text-gray-600 hover:bg-brand hover:text-white disabled:cursor-not-allowed disabled:opacity-40" title="复制到其他账号" aria-label={`复制商品到其他账号：${item.item_title || item.item_id}`}><Copy className="h-3.5 w-3.5" /></button>
                    {onRefreshStats && <button type="button" onClick={/* 点击刷新该商品的浏览与想要数据。 */ () => onRefreshStats(item)} disabled={(refreshingKeys || []).includes(rowKey)} className="rounded-lg bg-gray-100 p-1.5 text-gray-600 hover:bg-brand hover:text-white disabled:cursor-not-allowed disabled:opacity-40" title="刷新浏览/想要数据" aria-label={`刷新商品统计：${item.item_title || item.item_id}`}><RefreshCw className="h-3.5 w-3.5" /></button>}
                    <button type="button" onClick={/* 点击打开该商品的发货规则配置。 */ () => onConfigureDelivery(item)} className="inline-flex items-center gap-1 rounded-lg bg-gray-900 px-2.5 py-1.5 font-bold text-white hover:bg-black"><Link2 className="h-3 w-3" />发货规则</button>
                    {!isDown && <button type="button" onClick={/* 点击编辑商品。 */ () => onEdit(item)} className="rounded-lg bg-gray-100 p-1.5 text-gray-600 hover:bg-brand hover:text-white" title="编辑" aria-label="编辑"><Edit className="h-3.5 w-3.5" /></button>}
                    {!isDown && <button type="button" onClick={/* 点击打开该商品的下架确认。 */ () => onDownshelf(item)} className="rounded-lg bg-gray-100 p-1.5 text-gray-600 hover:bg-amber-100 hover:text-amber-700" title="下架" aria-label="下架"><Archive className="h-3.5 w-3.5" /></button>}
                    <button type="button" onClick={/* 点击删除商品。 */ () => onDelete(item)} className="rounded-lg bg-gray-100 p-1.5 text-red-500 hover:bg-red-100" title="删除" aria-label="删除"><Trash2 className="h-3.5 w-3.5" /></button>
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
};

export default ItemTable;
