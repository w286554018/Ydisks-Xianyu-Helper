package adapter

import (
	"context"
	"fmt"

	accountapp "xianyu-go/internal/application/account"
	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// CopyHTTPImageFetcher 把注入的公网图片下载函数适配为复制发布图片端口。
// 下载函数由组合根注入（与批量发布图片下载共用同一套公网/大小/媒体类型限制）。
type CopyHTTPImageFetcher struct {
	// download 是注入的下载实现。
	download func(ctx context.Context, rawURL string) ([]byte, string, error)
}

// NewCopyHTTPImageFetcher 构造图片下载适配器，下载函数缺失时快速失败。
func NewCopyHTTPImageFetcher(download func(ctx context.Context, rawURL string) ([]byte, string, error)) (*CopyHTTPImageFetcher, error) {
	if download == nil {
		return nil, fmt.Errorf("复制发布图片下载函数不能为空")
	}
	return &CopyHTTPImageFetcher{download: download}, nil
}

// Download 实现 itemapp.CopyImageFetcher。
func (f *CopyHTTPImageFetcher) Download(ctx context.Context, url string) ([]byte, string, error) {
	return f.download(ctx, url)
}

// CopyAccountsAdapter 把账号摘要仓储适配为复制发布的账号预检端口。
// 只透传非敏感摘要字段，不读取或解密任何凭据。
type CopyAccountsAdapter struct {
	// summaries 是账号摘要仓储。
	summaries *AccountSummaryRepository
}

// NewCopyAccountsAdapter 构造账号预检适配器。
func NewCopyAccountsAdapter(summaries *AccountSummaryRepository) *CopyAccountsAdapter {
	return &CopyAccountsAdapter{summaries: summaries}
}

// ListForUser 实现 itemapp.CopyAccountsPort。
func (a *CopyAccountsAdapter) ListForUser(ctx context.Context, userID int64) ([]itemapp.AccountSummaryView, error) {
	// list 是非敏感摘要列表。
	list, err := a.summaries.ListSummaries(ctx, userID)
	if err != nil {
		return nil, err
	}
	// views 转换为复制服务可见的视图。
	views := make([]itemapp.AccountSummaryView, 0, len(list))
	for _, summary := range list { // summary 是当前遍历的账号摘要。
		views = append(views, itemapp.AccountSummaryView{
			ID: summary.ID, Remark: summary.Remark, Nickname: summary.Nickname, PausedUntil: summary.PausedUntil,
		})
	}
	return views, nil
}

// 编译期确认适配器满足应用端口。
var (
	_ itemapp.CopyAccountsPort  = (*CopyAccountsAdapter)(nil)
	_ itemapp.CopyImageFetcher  = (*CopyHTTPImageFetcher)(nil)
	_ itemapp.CopySnapshotStore = (*LibraryFileStore)(nil)
)

// CopyItemSourceAdapter 把商品快照仓储适配为复制发布的源读取端口。
type CopyItemSourceAdapter struct {
	// items 是商品快照仓储。
	items *db.Items
}

// NewCopyItemSourceAdapter 构造源商品读取适配器。
func NewCopyItemSourceAdapter(items *db.Items) *CopyItemSourceAdapter {
	return &CopyItemSourceAdapter{items: items}
}

// GetByCookieItem 实现 itemapp.CopySourceItems：读取持久化行并转换为应用层快照视图。
func (a *CopyItemSourceAdapter) GetByCookieItem(ctx context.Context, cookieID, itemID string) (itemapp.CopySourceItem, error) {
	// row 是商品快照持久化行。
	row, err := a.items.GetByCookieItem(ctx, cookieID, itemID)
	if err != nil {
		return itemapp.CopySourceItem{}, err
	}
	return itemapp.CopySourceItem{
		ItemID: row.ItemID, Title: row.ItemTitle, Description: row.ItemDescription,
		Price: row.ItemPrice, IsMultiSpec: row.IsMultiSpec, DetailJSON: row.ItemDetail,
	}, nil
}

// 编译期确认源读取适配器满足端口。
var _ itemapp.CopySourceItems = (*CopyItemSourceAdapter)(nil)

// 确保 accountapp 导入用于摘要类型断言（Summary 结构体经 ListSummaries 返回）。
var _ = accountapp.Summary{}
