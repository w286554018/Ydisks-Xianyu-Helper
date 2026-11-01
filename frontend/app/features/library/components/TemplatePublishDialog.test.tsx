// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import * as api from '../api';
import type { LibraryLocationPickerSlotProps, ProductTemplate } from '../types';
import { TemplatePublishDialog } from './TemplatePublishDialog';

vi.mock('../api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../api')>()), previewTemplatePublish: vi.fn(), startPublishBatch: vi.fn() }));

// template 是被发布的最小模板。
const template = { id: 1, name: '模板A' } as ProductTemplate;
// accounts 是可选账号。
const accounts = [{ id: 'a1', label: '店铺1' }, { id: 'a2', label: '店铺2' }];

describe('TemplatePublishDialog', /* 先预检再确认的发布流程。 */ () => {
  beforeEach(/* 重置请求替身。 */ () => { vi.clearAllMocks(); });
  afterEach(/* 卸载渲染树。 */ () => cleanup());

  test('未生成预检前不能确认；预检后展示行并可启动批次', /* 成功路径。 */ async () => {
    vi.mocked(api.previewTemplatePublish).mockResolvedValue({ previewId: 'p1', total: 1, valid: 1, invalid: 0, rows: [{ rowNo: 1, valid: true, errors: [], cookieId: 'a1', title: '商品标题X', price: '9.90', quantity: 1 }] });
    vi.mocked(api.startPublishBatch).mockResolvedValue('batch-1');
    // onStarted 记录启动结果。
    const onStarted = vi.fn();
    render(<TemplatePublishDialog templates={[template]} accounts={accounts} onClose={vi.fn()} onStarted={onStarted} />);
    expect((screen.getByText('确认发布') as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByText('生成预检') as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByLabelText('店铺1'));
    fireEvent.click(screen.getByText('生成预检'));
    await waitFor(/* 等待预检表格。 */ () => expect(screen.getByText('商品标题X')).toBeTruthy());
    fireEvent.click(screen.getByText('确认发布 1 个商品'));
    await waitFor(/* 等待批次启动。 */ () => expect(onStarted).toHaveBeenCalledWith('batch-1'));
    expect(api.previewTemplatePublish).toHaveBeenCalledWith(expect.objectContaining({ templateIds: [1], cookieIds: ['a1'] }), expect.anything());
  });

  test('预检失败展示原因；更换账号使旧预检失效', /* 失败与失效。 */ async () => {
    vi.mocked(api.previewTemplatePublish).mockRejectedValueOnce(new Error('预检失败')).mockResolvedValueOnce({ previewId: 'p', total: 1, valid: 0, invalid: 1, rows: [{ rowNo: 1, valid: false, errors: ['缺少卡密'], cookieId: 'a1', title: 't', price: '1', quantity: 1 }] });
    render(<TemplatePublishDialog templates={[template]} accounts={accounts} onClose={vi.fn()} onStarted={vi.fn()} />);
    fireEvent.click(screen.getByLabelText('店铺1'));
    fireEvent.click(screen.getByText('生成预检'));
    await waitFor(/* 等待错误提示。 */ () => expect(screen.getByRole('alert').textContent).toContain('预检失败'));
    fireEvent.click(screen.getByText('生成预检'));
    await waitFor(/* 等待无效行。 */ () => expect(screen.getByText('缺少卡密')).toBeTruthy());
    expect((screen.getByText('确认发布 0 个商品') as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByLabelText('店铺2'));
    expect(screen.queryByText('缺少卡密')).toBeNull();
  });

  test('账号搜索后全选只选中可见账号，并使旧预检失效', /* 搜索与全选。 */ async () => {
    vi.mocked(api.previewTemplatePublish).mockResolvedValue({ previewId: 'p', total: 1, valid: 1, invalid: 0, rows: [{ rowNo: 1, valid: true, errors: [], cookieId: 'a2', title: '标题Y', price: '1', quantity: 1 }] });
    render(<TemplatePublishDialog templates={[template]} accounts={accounts} onClose={vi.fn()} onStarted={vi.fn()} />);
    fireEvent.change(screen.getByLabelText('搜索账号'), { target: { value: '店铺2' } });
    expect(screen.queryByLabelText('店铺1')).toBeNull();
    fireEvent.click(screen.getByLabelText('全选账号'));
    expect((screen.getByLabelText('店铺2') as HTMLInputElement).checked).toBe(true);
    fireEvent.click(screen.getByText('生成预检'));
    await waitFor(/* 等待预检表格，账号列显示展示名而不是标识。 */ () => expect(screen.getByText('标题Y')).toBeTruthy());
    expect(api.previewTemplatePublish).toHaveBeenCalledWith(expect.objectContaining({ cookieIds: ['a2'] }), expect.anything());
    expect(screen.getAllByText('店铺2').length).toBeGreaterThan(1);
    fireEvent.click(screen.getByLabelText('全选账号'));
    expect(screen.queryByText('标题Y')).toBeNull();
    fireEvent.change(screen.getByLabelText('搜索账号'), { target: { value: '不存在' } });
    expect(screen.getByText('没有匹配的账号。')).toBeTruthy();
    expect((screen.getByLabelText('全选账号') as HTMLInputElement).disabled).toBe(true);
  });

  test('只看需修正仅过滤预检表展示', /* 预检行过滤。 */ async () => {
    vi.mocked(api.previewTemplatePublish).mockResolvedValue({ previewId: 'p', total: 2, valid: 1, invalid: 1, rows: [
      { rowNo: 1, valid: true, errors: [], cookieId: 'a1', title: '正常行', price: '1', quantity: 1 },
      { rowNo: 2, valid: false, errors: ['缺少卡密'], cookieId: 'a2', title: '问题行', price: '1', quantity: 1 },
    ] });
    render(<TemplatePublishDialog templates={[template]} accounts={accounts} onClose={vi.fn()} onStarted={vi.fn()} />);
    fireEvent.click(screen.getByLabelText('全选账号'));
    fireEvent.click(screen.getByText('生成预检'));
    await waitFor(/* 等待预检表格。 */ () => expect(screen.getByText('正常行')).toBeTruthy());
    fireEvent.click(screen.getByLabelText('只看需修正'));
    expect(screen.queryByText('正常行')).toBeNull();
    expect(screen.getByText('问题行')).toBeTruthy();
  });

  test('选择发货地后预检携带完整位置，清除后省略', /* 发货地流转。 */ async () => {
    vi.mocked(api.previewTemplatePublish).mockResolvedValue({ previewId: 'p', total: 1, valid: 1, invalid: 0, rows: [{ rowNo: 1, valid: true, errors: [], cookieId: 'a1', title: '标题Z', price: '1', quantity: 1 }] });
    // picker 是注入的发货地选择器替身，点确认按钮返回固定 POI。
    const picker = ({ open, onConfirm }: LibraryLocationPickerSlotProps) => (
      open ? <button type="button" onClick={/* 确认选择。 */ () => onConfirm({ province: '广东省', city: '深圳市', area: '南山区', divisionId: '440305', longitude: 113.9, latitude: 22.5, poiId: 'B001', poiName: '科技园' })}>确认地点</button> : null
    );
    render(<TemplatePublishDialog templates={[template]} accounts={accounts} locationPicker={picker} onClose={vi.fn()} onStarted={vi.fn()} />);
    fireEvent.click(screen.getByLabelText('店铺1'));
    // 未选择发货地时预检请求省略 location。
    fireEvent.click(screen.getByText('生成预检'));
    await waitFor(/* 等待预检完成。 */ () => expect(screen.getByText('标题Z')).toBeTruthy());
    expect(api.previewTemplatePublish).toHaveBeenLastCalledWith(expect.objectContaining({ location: undefined }), expect.anything());
    // 选择发货地后旧预检失效，再次预检携带完整 8 字段。
    fireEvent.click(screen.getByText('选择发货地（可选）'));
    fireEvent.click(screen.getByText('确认地点'));
    expect(screen.getByText(/广东省 深圳市 南山区 科技园/)).toBeTruthy();
    expect(screen.queryByText('标题Z')).toBeNull();
    fireEvent.click(screen.getByText('生成预检'));
    await waitFor(/* 等待新预检完成。 */ () => expect(screen.getByText('标题Z')).toBeTruthy());
    expect(api.previewTemplatePublish).toHaveBeenLastCalledWith(expect.objectContaining({
      location: { province: '广东省', city: '深圳市', area: '南山区', divisionId: '440305', longitude: 113.9, latitude: 22.5, poiId: 'B001', poiName: '科技园' },
    }), expect.anything());
    // 清除发货地回到账号默认语义。
    fireEvent.click(screen.getByText('清除，改用账号默认'));
    expect(screen.queryByText(/科技园/)).toBeNull();
    fireEvent.click(screen.getByText('生成预检'));
    await waitFor(/* 等待预检完成。 */ () => expect(screen.getByText('标题Z')).toBeTruthy());
    expect(api.previewTemplatePublish).toHaveBeenLastCalledWith(expect.objectContaining({ location: undefined }), expect.anything());
  });
});
