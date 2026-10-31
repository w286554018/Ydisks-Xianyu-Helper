package items

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeCopyItems 是源商品快照仓储替身。
type fakeCopyItems struct {
	// row 返回的源商品快照视图。
	row CopySourceItem
	// err 返回的错误。
	err error
}

// GetByCookieItem 实现 CopySourceItems。
func (f *fakeCopyItems) GetByCookieItem(_ context.Context, _, _ string) (CopySourceItem, error) {
	return f.row, f.err
}

// fakeCopyOwnership 是归属查询替身。
type fakeCopyOwnership struct {
	// owned 为归属结论。
	owned bool
}

// CookieOwned 实现 copyOwnershipPort。
func (f *fakeCopyOwnership) CookieOwned(_ context.Context, _ int64, _ string) (bool, error) {
	return f.owned, nil
}

// fakeCopyAccounts 是账号摘要替身。
type fakeCopyAccounts struct {
	// summaries 返回的摘要列表。
	summaries []AccountSummaryView
}

// ListForUser 实现 CopyAccountsPort。
func (f *fakeCopyAccounts) ListForUser(_ context.Context, _ int64) ([]AccountSummaryView, error) {
	return f.summaries, nil
}

// fakeCopySnapshots 是批次目录替身。
type fakeCopySnapshots struct {
	// saved 记录保存的文件名。
	saved []string
	// discarded 记录回收调用次数。
	discarded int
}

// Prepare 实现 CopySnapshotStore。
func (f *fakeCopySnapshots) Prepare(_ context.Context, _ string) (string, error) {
	return "/tmp/fake-upload", nil
}

// SaveImage 实现 CopySnapshotStore。
func (f *fakeCopySnapshots) SaveImage(_ context.Context, _, name string, _ []byte) error {
	f.saved = append(f.saved, name)
	return nil
}

// Discard 实现 CopySnapshotStore。
func (f *fakeCopySnapshots) Discard(_ context.Context, _ string) error {
	f.discarded++
	return nil
}

// fakeCopyFetcher 是图片下载替身。
type fakeCopyFetcher struct {
	// calls 记录下载的地址。
	calls []string
	// err 注入下载错误。
	err error
}

// Download 实现 CopyImageFetcher。
func (f *fakeCopyFetcher) Download(_ context.Context, url string) ([]byte, string, error) {
	if f.err != nil {
		return nil, "", f.err
	}
	f.calls = append(f.calls, url)
	return []byte("img-" + url), "image/jpeg", nil
}

// fakeCopyValidator 是预检行校验替身。
type fakeCopyValidator struct {
	// rows 原样回传。
	rows []BatchPreviewRow
}

// PreviewRows 实现 CopyPreviewRowsValidator。
func (f *fakeCopyValidator) PreviewRows(_ context.Context, _ BatchPreviewInput, rows []BatchPreviewRow) ([]BatchPreviewRow, error) {
	f.rows = rows
	return rows, nil
}

// fakeCopyPersister 是批次落库替身。
type fakeCopyPersister struct {
	// batch 记录落库批次。
	batch BatchPreviewPersistenceBatch
	// rows 记录落库行。
	rows []BatchPreviewRow
}

// Persist 实现 CopyPreviewPersister。
func (f *fakeCopyPersister) Persist(_ context.Context, batch BatchPreviewPersistenceBatch, rows []BatchPreviewRow) (BatchPreviewPersistenceResult, error) {
	f.batch = batch
	f.rows = rows
	return BatchPreviewPersistenceResult{Success: true, PreviewID: batch.ID, Total: len(rows), Valid: len(rows)}, nil
}

// copySourceRow 构造可复制的源商品快照。
func copySourceRow() CopySourceItem {
	// detail 模拟列表同步快照：imageInfos 为字符串化 JSON 数组。
	detail := `{"detail_params":{"imageInfos":"[{\"url\":\"http://img.alicdn.com/a.jpg\",\"major\":true},{\"url\":\"http://img.alicdn.com/b.png\",\"major\":false}]"}}`
	return CopySourceItem{ItemID: "i1", Title: "源商品", Description: "描述", Price: "9.90", DetailJSON: detail}
}

// newCopyService 构造全替身服务。
func newCopyService(t *testing.T) (*CopyPublishService, *fakeCopyItems, *fakeCopyOwnership, *fakeCopySnapshots, *fakeCopyFetcher, *fakeCopyPersister) {
	t.Helper()
	// items 为快照替身。
	items := &fakeCopyItems{row: copySourceRow()}
	// ownership 为归属替身。
	ownership := &fakeCopyOwnership{owned: true}
	// accounts 为摘要替身。
	accounts := &fakeCopyAccounts{summaries: []AccountSummaryView{
		{ID: "t1", Remark: "二号机"}, {ID: "t2", Nickname: "三号机"}, {ID: "t3", PausedUntil: time.Now().Add(time.Hour).Unix(), Remark: "暂停机"},
	}}
	// snapshots 为目录替身。
	snapshots := &fakeCopySnapshots{}
	// fetcher 为下载替身。
	fetcher := &fakeCopyFetcher{}
	// validator 为校验替身。
	validator := &fakeCopyValidator{}
	// persister 为落库替身。
	persister := &fakeCopyPersister{}
	// svc 为被测服务。
	svc, err := NewCopyPublishService(items, ownership, accounts, snapshots, fetcher, validator, persister, func() string { return "b1" }, 50)
	if err != nil {
		t.Fatalf("构造服务失败: %v", err)
	}
	return svc, items, ownership, snapshots, fetcher, persister
}

// TestCreateCopyTaskExpandsTargets 验证按目标账号展开行、图片下载与字段合并。
func TestCreateCopyTaskExpandsTargets(t *testing.T) {
	// svc 与替身组。
	svc, _, _, snapshots, fetcher, persister := newCopyService(t)
	// result 是创建结果。
	result, err := svc.CreateCopyTask(context.Background(), CopyTaskInput{
		UserID: 1, CookieID: "src", ItemID: "i1", TargetCookieIDs: []string{"t1", "t2", "src"},
		TitleEdit: "改过的标题", PriceEdit: "19.90",
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	// 源账号被排除，行数应为 2。
	if result.RowCount != 2 {
		t.Fatalf("应排除源账号得 2 行，实际 %d", result.RowCount)
	}
	// 图片按快照顺序下载。
	if len(fetcher.calls) != 2 || !strings.HasSuffix(fetcher.calls[0], "a.jpg") {
		t.Fatalf("图片下载异常: %v", fetcher.calls)
	}
	// 批次目录收到 2 个文件。
	if len(snapshots.saved) != 2 {
		t.Fatalf("图片落盘数量异常: %v", snapshots.saved)
	}
	// 行内容断言：标题/价格用编辑值，描述沿用源值。
	if len(persister.rows) != 2 {
		t.Fatalf("落库行数异常: %d", len(persister.rows))
	}
	// first 是第一行。
	first := persister.rows[0]
	if first.CookieID != "t1" || first.Title != "改过的标题" || first.Price != "19.90" || first.Description != "描述" {
		t.Fatalf("行内容异常: %+v", first)
	}
	// 编辑值作用于全部目标行，第二行标题与第一行一致。
	second := persister.rows[1]
	if second.Title != "改过的标题" || second.CookieID != "t2" {
		t.Fatalf("第二行内容异常: %+v", second)
	}
	// 批次元数据。
	if persister.batch.Status != "preview" || persister.batch.Filename != "商品复制发布" {
		t.Fatalf("批次元数据异常: %+v", persister.batch)
	}
}

// TestCreateCopyTaskRejectsMultiSpec 验证多规格源商品被拒绝。
func TestCreateCopyTaskRejectsMultiSpec(t *testing.T) {
	// svc 与替身组。
	svc, items, _, _, _, _ := newCopyService(t)
	// 注入多规格标记。
	items.row.IsMultiSpec = true
	// 期望明确错误。
	_, err := svc.CreateCopyTask(context.Background(), CopyTaskInput{UserID: 1, CookieID: "src", ItemID: "i1", TargetCookieIDs: []string{"t1"}})
	if !errors.Is(err, ErrCopyMultiSpecUnsupported) {
		t.Fatalf("多规格应被拒绝，实际: %v", err)
	}
}

// TestCreateCopyTaskNoImages 验证无图片快照被拒绝。
func TestCreateCopyTaskNoImages(t *testing.T) {
	// svc 与替身组。
	svc, items, _, _, _, _ := newCopyService(t)
	// 注入无图片快照。
	items.row.DetailJSON = `{}`
	// 期望明确错误。
	_, err := svc.CreateCopyTask(context.Background(), CopyTaskInput{UserID: 1, CookieID: "src", ItemID: "i1", TargetCookieIDs: []string{"t1"}})
	if !errors.Is(err, ErrCopyNoImages) {
		t.Fatalf("无图片应被拒绝，实际: %v", err)
	}
}

// TestCreateCopyTaskDownloadFailureDiscards 验证图片下载失败回收批次目录。
func TestCreateCopyTaskDownloadFailureDiscards(t *testing.T) {
	// svc 与替身组。
	svc, _, _, snapshots, fetcher, _ := newCopyService(t)
	// 注入下载错误。
	fetcher.err = errors.New("网络错误")
	// 期望失败。
	_, err := svc.CreateCopyTask(context.Background(), CopyTaskInput{UserID: 1, CookieID: "src", ItemID: "i1", TargetCookieIDs: []string{"t1"}})
	if err == nil {
		t.Fatal("下载失败应返回错误")
	}
	// 批次目录应被回收。
	if snapshots.discarded != 1 {
		t.Fatalf("失败应回收批次目录，实际 %d 次", snapshots.discarded)
	}
}

// TestCreateCopyTaskOwnership 验证源/目标账号归属校验。
func TestCreateCopyTaskOwnership(t *testing.T) {
	// svc 与替身组。
	svc, _, ownership, _, _, _ := newCopyService(t)
	// 源账号不归属整体失败。
	ownership.owned = false
	// 期望归属错误。
	_, err := svc.CreateCopyTask(context.Background(), CopyTaskInput{UserID: 1, CookieID: "src", ItemID: "i1", TargetCookieIDs: []string{"t1"}})
	if err == nil || !errors.Is(err, ErrCopyNotOwned) {
		t.Fatalf("源账号归属校验异常: %v", err)
	}
}

// TestPreviewCopyTargets 验证预览的目标账号预检（排除源账号、暂停标黄）。
func TestPreviewCopyTargets(t *testing.T) {
	// svc 与替身组。
	svc, _, _, _, _, _ := newCopyService(t)
	// preview 是预览结果。
	preview, err := svc.PreviewCopy(context.Background(), 1, "src", "i1", []string{"t1", "t2", "t3", "src"})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	// 源账号被排除，剩 3 个。
	if len(preview.Targets) != 3 {
		t.Fatalf("目标数异常: %d", len(preview.Targets))
	}
	// t1 备注优先、t2 昵称兜底、t3 暂停不可用。
	if preview.Targets[0].Name != "二号机" || preview.Targets[1].Name != "三号机" {
		t.Fatalf("展示名异常: %+v", preview.Targets)
	}
	if preview.Targets[2].Usable || preview.Targets[2].Reason != "账号处于暂停期" {
		t.Fatalf("暂停账号应不可用: %+v", preview.Targets[2])
	}
	// 快照字段透传。
	if preview.Title != "源商品" || preview.Price != "9.90" || len(preview.ImageURLs) != 2 {
		t.Fatalf("快照字段异常: %+v", preview)
	}
}

// TestSnapshotImageURLsFallback 验证单品发布形态（item_image 单图）的图片回退。
func TestSnapshotImageURLsFallback(t *testing.T) {
	// urls 为单图回退结果。
	urls := snapshotImageURLs(`{"item_image":"https://img.alicdn.com/main.png"}`)
	if len(urls) != 1 || urls[0] != "https://img.alicdn.com/main.png" {
		t.Fatalf("单图回退异常: %v", urls)
	}
	// 非法 JSON 返回空。
	if urls := snapshotImageURLs("not-json"); len(urls) != 0 {
		t.Fatalf("非法 JSON 应返回空: %v", urls)
	}
}
