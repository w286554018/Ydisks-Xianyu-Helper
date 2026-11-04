// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test, vi } from 'vitest';
import type { Item, ShippingRule } from '../models';
import ItemTable from './ItemTable';

// itemFixture 是账号 acc1 下的一个商品。
const itemFixture = { cookie_id: 'acc1', item_id: 'i1', item_title: '示例商品', item_price: '9.9', ai_override: 'enabled' } as Item;
// downFixture 是已下架状态的同类商品。
const downFixture = { ...itemFixture, item_id: 'i2', item_title: '已下架商品', status: 'down' } as Item;

// paidRule 是该商品专属的付款后自动发货规则。
const paidRule = { id: '1', name: 'r', trigger_type: 'order_paid', item_keyword: '', cookie_id: 'acc1', item_id: 'i1', card_group_id: 0, priority: 0, enabled: true, actions: [], variants: [] } as ShippingRule;

afterEach(cleanup);

// renderTable 渲染列表视图并返回各回调的记录函数。
const renderTable = (items: Item[], rules: ShippingRule[], extra?: {
  /** selectedKeys 是传入表格的勾选键集合。 */
  selectedKeys?: string[];
  /** onToggleSelect 是传入表格的勾选切换回调。 */
  onToggleSelect?: (item: Item) => void;
}) => {
  // handlers 记录表格触发的全部用户操作回调。
  const handlers = { onConfigureDelivery: vi.fn(), onOpenAISettings: vi.fn(), onEdit: vi.fn(), onCopyToAccounts: vi.fn(), onDownshelf: vi.fn(), onDelete: vi.fn() };
  render(<ItemTable items={items} rules={rules} accountNickname={/* 返回固定昵称。 */ () => '店铺A'} formatPrice={/* 原样返回价格。 */ price => `¥${price}`} aiStatusText={{ inherit: '继承账号', enabled: '已启用', disabled: '已停用' }} emptyText="暂无商品" selectedKeys={extra?.selectedKeys} onToggleSelect={extra?.onToggleSelect} {...handlers} />);
  return handlers;
};

describe('ItemTable', /* 覆盖列表视图渲染与操作回调。 */ () => {
  test('shows rule coverage per trigger and fires row actions', /* 已配置显示条数，其余显示未配置，操作回调带上商品。 */ () => {
    // handlers 是各行操作的记录函数。
    const handlers = renderTable([itemFixture], [paidRule]);
    expect(screen.getByText('1 条')).toBeTruthy();
    expect(screen.getAllByText('未配置')).toHaveLength(3);
    fireEvent.click(screen.getByText('发货规则'));
    fireEvent.click(screen.getByLabelText('编辑'));
    fireEvent.click(screen.getByLabelText('下架'));
    fireEvent.click(screen.getByLabelText('删除'));
    fireEvent.click(screen.getByLabelText('配置商品 AI 客服：示例商品'));
    expect(handlers.onConfigureDelivery).toHaveBeenCalledWith(itemFixture);
    expect(handlers.onEdit).toHaveBeenCalledWith(itemFixture);
    expect(handlers.onDownshelf).toHaveBeenCalledWith(itemFixture);
    expect(handlers.onDelete).toHaveBeenCalledWith(itemFixture);
    expect(handlers.onOpenAISettings).toHaveBeenCalledWith(itemFixture);
  });

  test('renders the empty text when there are no items', /* 无商品时只显示提示。 */ () => {
    renderTable([], []);
    expect(screen.getByText('暂无商品')).toBeTruthy();
  });

  test('shows down badge and hides edit/copy/downshelf for down items', /* 已下架商品显示徽标并收起编辑、复制与下架入口。 */ () => {
    renderTable([downFixture], []);
    expect(screen.getByText('已下架')).toBeTruthy();
    expect(screen.queryByLabelText('编辑')).toBeNull();
    expect(screen.queryByLabelText('下架')).toBeNull();
    expect(screen.getByLabelText('删除')).toBeTruthy();
    // 复制按钮保留在 DOM 但禁用。
    expect((screen.getByLabelText('复制商品到其他账号：已下架商品') as HTMLButtonElement).disabled).toBe(true);
  });

  test('toggles selection via checkbox when selection props are provided', /* 勾选框把商品交回父级维护。 */ () => {
    // onToggleSelect 记录勾选切换回调。
    const onToggleSelect = vi.fn();
    renderTable([itemFixture], [], { selectedKeys: [], onToggleSelect });
    fireEvent.click(screen.getByLabelText('选择商品：示例商品'));
    expect(onToggleSelect).toHaveBeenCalledWith(itemFixture);
  });
});
