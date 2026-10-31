package adapter

import (
	"context"
	"errors"
	"strings"
	"testing"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// TestCopyHTTPImageFetcherRejectsNilDownload 验证下载函数缺失时构造快速失败。
func TestCopyHTTPImageFetcherRejectsNilDownload(t *testing.T) {
	// err 表示空下载函数导致的构造错误，期望非空。
	_, err := NewCopyHTTPImageFetcher(nil)
	if err == nil || !strings.Contains(err.Error(), "下载函数不能为空") {
		t.Fatalf("空下载函数应构造失败: %v", err)
	}
}

// TestCopyHTTPImageFetcherDownloadDelegates 验证 Download 原样透传注入的下载函数结果。
func TestCopyHTTPImageFetcherDownloadDelegates(t *testing.T) {
	// fetcher 使用记录调用参数的固定下载实现。
	fetcher, buildErr := NewCopyHTTPImageFetcher(func(_ context.Context, rawURL string) ([]byte, string, error) {
		if rawURL != "https://img.example.com/a.jpg" {
			return nil, "", errors.New("意外地址")
		}
		return []byte("bytes"), "image/jpeg", nil
	})
	if buildErr != nil {
		t.Fatalf("构造失败: %v", buildErr)
	}
	// data、contentType、err 是下载结果与错误。
	data, contentType, err := fetcher.Download(context.Background(), "https://img.example.com/a.jpg")
	if err != nil || string(data) != "bytes" || contentType != "image/jpeg" {
		t.Fatalf("Download 透传不符: data=%s type=%s err=%v", data, contentType, err)
	}
	// 编译期确认端口满足。
	var _ itemapp.CopyImageFetcher = fetcher
}

// TestCopyAccountsAdapterMapsNonSensitiveSummary 验证账号摘要适配器只透传非敏感字段。
func TestCopyAccountsAdapterMapsNonSensitiveSummary(t *testing.T) {
	// store、cleanup 是内存 SQLite 测试库及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是测试上下文。
	ctx := context.Background()
	// remark 是写入账号的备注文本。
	remark := "复制备注"
	// _, remarkErr 分别是设置更新的影响行数与失败原因。
	if _, remarkErr := store.Cookies.UpdateSettings(ctx, "cid", db.AccountSettingsUpdate{UserID: 1, Remark: &remark}); remarkErr != nil {
		t.Fatalf("写入备注失败: %v", remarkErr)
	}
	// nicknameErr 表示昵称写入失败原因。
	if nicknameErr := store.Cookies.UpdateProfile(ctx, "cid", "昵称甲", ""); nicknameErr != nil {
		t.Fatalf("写入昵称失败: %v", nicknameErr)
	}
	// adapter 是被测账号预检适配器。
	adapter := NewCopyAccountsAdapter(NewAccountSummaryRepository(store))
	// views、err 是用户名下摘要与错误。
	views, err := adapter.ListForUser(ctx, 1)
	if err != nil {
		t.Fatalf("ListForUser 失败: %v", err)
	}
	if len(views) != 1 || views[0].ID != "cid" {
		t.Fatalf("摘要映射不符: %+v", views)
	}
	// 编译期确认端口满足。
	var _ itemapp.CopyAccountsPort = adapter
}

// TestCopyItemSourceAdapterConvertsRow 验证源商品读取适配器把持久化行转换为应用层快照视图。
func TestCopyItemSourceAdapterConvertsRow(t *testing.T) {
	// store、cleanup 是内存 SQLite 测试库及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是测试上下文。
	ctx := context.Background()
	// detailJSON 模拟列表同步快照的图片信息结构。
	detailJSON := `{"detail_params":{"imageInfos":"[{\"url\":\"http://img.alicdn.com/a.jpg\",\"major\":true}]"}}`
	// row 是待落库的商品快照行。
	row := &db.ItemInfoRow{CookieID: "cid", ItemID: "i1", ItemTitle: "源标题", ItemDescription: "源描述", ItemPrice: "9.90", ItemDetail: detailJSON}
	// upsertErr 表示商品快照落库失败原因。
	if upsertErr := store.Items.Upsert(ctx, row); upsertErr != nil {
		t.Fatalf("落库失败: %v", upsertErr)
	}
	// adapter 是被测源读取适配器。
	adapter := NewCopyItemSourceAdapter(store.Items)
	// source、err 是应用层快照视图与错误。
	source, err := adapter.GetByCookieItem(ctx, "cid", "i1")
	if err != nil {
		t.Fatalf("GetByCookieItem 失败: %v", err)
	}
	if source.ItemID != "i1" || source.Title != "源标题" || source.Description != "源描述" || source.Price != "9.90" || source.DetailJSON != detailJSON {
		t.Fatalf("快照视图转换不符: %+v", source)
	}
	// 不存在的商品按端口语义透传错误。
	if _, missingErr := adapter.GetByCookieItem(ctx, "cid", "missing"); missingErr == nil {
		t.Fatalf("缺失商品应返回错误")
	}
	// 编译期确认端口满足。
	var _ itemapp.CopySourceItems = adapter
}
