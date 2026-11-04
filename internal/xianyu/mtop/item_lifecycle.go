// Package mtop: 商品生命周期域 — 平台下架与删除调用。
// 接口 ground truth 见 .trellis/tasks/11-01-item-platform-delete/protocol-notes.md：
// 两个接口请求体同构（data={"itemId":"<id>"}），仅 API 名与版本不同。
package mtop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/protocol"
)

// DownshelfItemAPI 是平台商品下架接口地址（版本 2.0）。
const DownshelfItemAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.item.downshelf/2.0/"

// DeleteItemAPI 是平台商品删除接口地址（版本 1.1）。
const DeleteItemAPI = "https://h5api.m.goofish.com/h5/com.taobao.idle.item.delete/1.1/"

// DownshelfItem 调用 mtop.taobao.idle.item.downshelf 下架商品（无 Context 版本）。
func (c *ClientImpl) DownshelfItem(cookiesStr, itemID string) (ok bool, ret []string, updatedCookies string, err error) {
	return c.DownshelfItemContext(context.Background(), cookiesStr, itemID)
}

// DownshelfItemContext 下架商品；签名 token 过期时使用响应下发的新 Cookie 重签并重试。
func (c *ClientImpl) DownshelfItemContext(ctx context.Context, cookiesStr, itemID string) (ok bool, ret []string, updatedCookies string, err error) {
	return c.itemLifecycleRetry(ctx, cookiesStr, itemID, DownshelfItemAPI, "mtop.taobao.idle.item.downshelf", "2.0", "下架商品")
}

// DeleteItem 调用 com.taobao.idle.item.delete 删除商品（无 Context 版本）。
func (c *ClientImpl) DeleteItem(cookiesStr, itemID string) (ok bool, ret []string, updatedCookies string, err error) {
	return c.DeleteItemContext(context.Background(), cookiesStr, itemID)
}

// DeleteItemContext 删除商品；签名 token 过期时使用响应下发的新 Cookie 重签并重试。
func (c *ClientImpl) DeleteItemContext(ctx context.Context, cookiesStr, itemID string) (ok bool, ret []string, updatedCookies string, err error) {
	return c.itemLifecycleRetry(ctx, cookiesStr, itemID, DeleteItemAPI, "com.taobao.idle.item.delete", "1.1", "删除商品")
}

// itemLifecycleRetry 是下架/删除共用的 token 过期重试包装：
// 最多 4 次尝试；token 过期时优先使用响应下发的新 Cookie，未下发则主动刷新一次；
// 风控（RGV587）与会话失效按业务错误立即返回，不自动重放。
func (c *ClientImpl) itemLifecycleRetry(ctx context.Context, cookiesStr, itemID, apiURL, apiName, version, label string) (ok bool, ret []string, updatedCookies string, err error) {
	// currentCookies 用于本次流程后续判断的currentCookies
	currentCookies := cookiesStr
	// session 是上下文携带的会话；存在时以会话当前 Cookie 为准。
	if session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, _, _ = session.State()
	}
	// lastRet 用于本次流程后续判断的lastRet
	var lastRet []string
	// attempt 用于本次流程后续判断的尝试次数
	for attempt := 0; attempt < 4; attempt++ {
		// previousCookies 用于本次流程后续判断的previousCookies
		previousCookies := currentCookies
		// ok、ret、updated、requestErr 用于本次流程后续判断的ok、ret、updated、requestErr
		ok, ret, updated, requestErr := c.itemLifecycleOnce(ctx, currentCookies, itemID, apiURL, apiName, version, label)
		if requestErr != nil {
			if !IsMTopTokenExpiredErr(requestErr) {
				return false, ret, currentCookies, requestErr
			}
			lastRet = mtopErrorRet(requestErr)
		} else {
			lastRet = ret
			if updated != "" {
				currentCookies = updated
			}
			if ok {
				return true, ret, currentCookies, nil
			}
			requestErr = c.mtopResponseFailure(label, http.StatusOK, ret, "平台 ret 未包含 SUCCESS")
			// kind、classified 保存业务错误分类及其识别结果；业务拒绝（含风控）不重试直接返回。
			if kind, classified := MTopErrorKindOf(requestErr); classified && kind == MTopErrorBusiness {
				return false, ret, currentCookies, nil
			}
			if !IsMTopTokenExpiredErr(requestErr) {
				return false, ret, currentCookies, requestErr
			}
		}
		if updated != "" {
			currentCookies = updated
		}
		if attempt == 3 {
			break
		}
		// MTop 通常会在 token 过期响应中通过 Set-Cookie 下发新签名 token；
		// 若没有下发，则主动调用 token API 尝试刷新一次。
		if !mtopTokenCookieChanged(previousCookies, currentCookies) {
			// refreshed、refreshErr 用于本次流程后续判断的refreshed、refreshErr
			refreshed, refreshErr := c.RefreshTokenContext(ctx, currentCookies)
			if refreshErr != nil {
				return false, ret, currentCookies, fmt.Errorf("%s token 过期且刷新失败: %w", label, refreshErr)
			}
			if refreshed.UpdatedCookies != "" {
				currentCookies = refreshed.UpdatedCookies
			}
		}
		// err 保存重试间隔等待错误（含取消）。
		if err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return false, ret, currentCookies, err
		}
	}
	return false, lastRet, currentCookies, fmt.Errorf("%s接口 Token 重试失败: %w", label, c.mtopResponseFailure(label, http.StatusOK, lastRet, "重试次数已耗尽"))
}

// itemLifecycleOnce 发送一次下架或删除请求；两接口请求体同构，仅 URL 与版本不同。
func (c *ClientImpl) itemLifecycleOnce(ctx context.Context, cookiesStr, itemID, apiURL, apiName, version, label string) (ok bool, ret []string, updatedCookies string, err error) {
	// hc 用于本次流程后续判断的hc
	hc := c.httpClient()
	// overrideURL 保存测试注入的本地端点；空值使用官方地址。
	overrideURL := apiURL
	if apiURL == DownshelfItemAPI && c.DownshelfItemURL != "" {
		overrideURL = c.DownshelfItemURL
	}
	if apiURL == DeleteItemAPI && c.DeleteItemURL != "" {
		overrideURL = c.DeleteItemURL
	}
	apiURL = overrideURL
	// signingCookies、requestCookies 用于本次流程后续判断的signingCookies、requestCookies
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, "https://www.goofish.com/", apiURL)
	// token 用于本次流程后续判断的令牌
	token := protocol.SignToken(signingCookies)
	// t 用于本次流程后续判断的时间戳
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// payload 是生命周期接口统一请求体：ground truth 仅包含 itemId。
	payload := struct {
		// ItemID 是目标平台商品 ID。
		ItemID string `json:"itemId"`
	}{ItemID: itemID}
	// dataVal 是签名和请求体共同使用的 JSON。
	dataBytes, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return false, nil, cookiesStr, fmt.Errorf("构造 %s 请求数据失败: %w", label, marshalErr)
	}
	// dataVal 是序列化后的请求数据，参与签名并作为表单值提交。
	dataVal := string(dataBytes)
	// sign 用于本次流程后续判断的签名
	sign := protocol.GenerateSign(t, token, dataVal)
	// query、body 分别是 URL 查询串与表单正文。
	query := buildLifecycleQuery(t, sign, apiName, version)
	// body 是 urlencoded 表单正文：data=<JSON>。
	body := "data=" + url.QueryEscape(dataVal)
	// req、err 用于本次流程后续判断的req、err
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"?"+query, strings.NewReader(body))
	if err != nil {
		return false, nil, cookiesStr, err
	}
	setCommonHeaders(req, requestCookies)
	// resp、err 用于本次流程后续判断的resp、err
	resp, err := hc.Do(req)
	if err != nil {
		return false, nil, cookiesStr, fmt.Errorf("%s请求失败: %w", label, err)
	}
	defer resp.Body.Close()
	// updated 用于本次流程后续判断的updated
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := readMTopBody(resp)
	if err != nil {
		return false, nil, updated, c.mtopResponseFailureWithCause(label, resp.StatusCode, nil, "读取响应失败", err)
	}
	// res 用于本次流程后续判断的响应
	var res struct {
		Ret []string `json:"ret"`
	}
	// err 保存响应 JSON 解析错误。
	if err := json.Unmarshal(raw, &res); err != nil {
		return false, nil, updated, c.mtopResponseFailureWithCause(label, resp.StatusCode, nil, "JSON 解析失败", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		// failure 保存非业务型非 2xx 响应的统一错误。
		failure := c.mtopResponseFailure(label, resp.StatusCode, res.Ret, "HTTP 状态异常")
		if isMTopBusinessRet(res.Ret) {
			return false, res.Ret, updated, nil
		}
		return false, res.Ret, updated, failure
	}
	// r 表示当前遍历过程中的r；mtop 成功 ret 形如 SUCCESS::调用成功，用前缀判定以兼容不同文案。
	for _, r := range res.Ret {
		if strings.HasPrefix(r, "SUCCESS::") {
			return true, res.Ret, updated, nil
		}
	}
	return false, res.Ret, updated, nil
}

// buildLifecycleQuery 构建下架/删除接口的 URL 查询串；参数结构与抓包 ground truth 一致。
func buildLifecycleQuery(t, sign, apiName, version string) string {
	// parts 用于本次流程后续判断的parts
	parts := [][2]string{
		{"jsv", "2.7.2"},
		{"appKey", protocol.SignAppKey},
		{"t", t},
		{"sign", sign},
		{"v", version},
		{"type", "originaljson"},
		{"accountSite", "xianyu"},
		{"dataType", "json"},
		{"timeout", "20000"},
		{"api", apiName},
		{"sessionOption", "AutoLoginOnly"},
	}
	// b 用于本次流程后续判断的b
	var b strings.Builder
	// i、kv 保存当前参数序号与键值对。
	for i, kv := range parts {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(kv[0])
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(kv[1]))
	}
	return b.String()
}
