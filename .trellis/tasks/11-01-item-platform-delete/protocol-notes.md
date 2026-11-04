# 商品平台生命周期接口抓包记录（阶段 0 产出）

- 抓包时间：2026-11-03 21:26–21:49（本地时间）
- 账号：123807039（水影莫），cookie 经本地 data-key 解密导出（584 字节）
- 目标商品：`1097378253052`（测试商品-请勿拍下-抓包专用08，已通过线上系统模板发布）
- 工具：`cmd/capture-lifecycle`（步进模式：由 agent 逐步发送指令驱动页面点击，每次点击前有诊断输出；所有平台请求均为页面自发产生，未构造、未重放任何平台调用）
- 过程说明：页面加载后 sellerButtonGroup 渲染「下架/删除」两个按钮；点击下架 → Ant Design 弹窗「确定要下架这个宝贝吗？」→ 确定 → downshelf 请求；点击删除 → 弹窗「确定要删除这个宝贝吗？」→ 确定 → delete 请求。全程未触发人机验证。
- 原始产物：`/tmp/lifecycle-capture/protocol-notes-delete-final.json`（106 条请求）、`capture-step.log`

## 1. 下架接口 mtop.taobao.idle.item.downshelf

```
POST https://h5api.m.goofish.com/h5/mtop.taobao.idle.item.downshelf/2.0/
  ?jsv=2.7.2&appKey=34839810&t=<毫秒时间戳>&sign=<32位hex>
  &v=2.0&type=originaljson&accountSite=xianyu&dataType=json&timeout=20000
  &api=mtop.taobao.idle.item.downshelf&sessionOption=AutoLoginOnly&spm_cnt=a21ybx.item.0.0
Content-Type: application/x-www-form-urlencoded
Cookie: <账号 cookie>

body: data={"itemId":"1097378253052"}   （urlencoded: data=%7B%22itemId%22%3A%22...%22%7D）
```

## 2. 删除接口 com.taobao.idle.item.delete

```
POST https://h5api.m.goofish.com/h5/com.taobao.idle.item.delete/1.1/
  ?jsv=2.7.2&appKey=34839810&t=<毫秒时间戳>&sign=<32位hex>
  &v=1.1&type=originaljson&accountSite=xianyu&dataType=json&timeout=20000
  &api=com.taobao.idle.item.delete&sessionOption=AutoLoginOnly&spm_cnt=a21ybx.item.0.0
Content-Type: application/x-www-form-urlencoded
Cookie: <账号 cookie>

body: data={"itemId":"1097378253052"}   （与下架完全同构，仅 API 名与版本不同）
```

## 3. 关键结论

1. **两接口请求体同构**：`data={"itemId":"<id>"}`，无其他字段，无 nested 参数。
2. **URL 签名参数与现有 mtop 客户端约定一致**（appKey=34839810、t、sign、api、v、accountSite=xianyu），走 `internal/xianyu/mtop` 现有签名/节流路径即可，无需新增签名逻辑。
3. **记录到的 headers 只有 content-type**（其余 mtop 头在 h5 页面走 cookie+URL sign，无 x-sign 类头），与现有 mtop.Client 的 h5 请求形态一致。
4. **页面侧确认交互**：下架/删除都有 Ant Design 确认弹窗；对应到我们产品内的「防呆确认」UX 决策（批量删除输入"删除"二字）不受影响。
5. **响应体未捕获**（工具只记录请求）。响应按标准 mtop envelope 处理（`ret: ["SUCCESS::..."]`），在阶段 2 实现时用下一次真实调用（需要再备一个测试商品）做首次验证，失败分类以响应码为准。

## 4. 抓包工具状态（阶段 0 遗留清理项）

- `cmd/capture-lifecycle/main.go` 已改造为**步进模式**：每步等待 `/tmp/lifecycle-capture/next-step` 触发文件指令（open/diag/locate/btns/down/delete/confirm/more/done），每步输出关键词计数诊断与截图；不再自动连点。
- `-auto-delete-only` 旗标已删除；步进模式下人工验证中断天然受控（不发指令即暂停）。
- 定位教训：`text=下架` 会命中 textContent="下架删除" 的父容器 div，点击落在空隙上无效；必须用 `GetByText(exact)` 精确匹配叶子文本。此教训对实现层无影响（实现走 mtop 协议，不走 UI），仅供后续 UI 自动化参考。
