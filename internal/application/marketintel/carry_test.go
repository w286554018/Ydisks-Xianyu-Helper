package marketintel

import (
	"context"
	"errors"
	"strconv"
	"testing"
)

// fakeCarryDetail 是搬运详情与改写能力替身。
type fakeCarryDetail struct {
	// detail 是详情返回值。
	detail CompetitorDetail
	// detailErr 是详情返回错误。
	detailErr error
	// rewritten 是改写返回值。
	rewritten RewrittenCopy
	// rewriteErr 是改写返回错误。
	rewriteErr error
	// rewriteCalls 记录改写调用次数。
	rewriteCalls int
}

// GetCompetitorDetail 返回预置详情或错误。
func (f *fakeCarryDetail) GetCompetitorDetail(_ context.Context, itemID string) (CompetitorDetail, error) {
	if f.detailErr != nil {
		return CompetitorDetail{}, f.detailErr
	}
	// out 是本次返回的详情副本，商品标识以调用方传入的为准。
	out := f.detail
	out.ItemID = itemID
	return out, nil
}

// RewriteCompetitorCopy 返回预置改写结果或错误。
func (f *fakeCarryDetail) RewriteCompetitorCopy(_ context.Context, _, _ string) (RewrittenCopy, error) {
	f.rewriteCalls++
	if f.rewriteErr != nil {
		return RewrittenCopy{}, f.rewriteErr
	}
	return f.rewritten, nil
}

// fakeCarryImages 是竞品图片下载替身。
type fakeCarryImages struct {
	// failIndexes 是需要模拟下载失败的图片序号集合。
	failIndexes map[int]bool
	// calls 是实际收到的下载请求序号。
	calls []int
	// lastItemID 是最近一次请求携带的商品标识。
	lastItemID string
}

// DownloadCarryImage 按序号决定成功或失败，并记录调用。
func (f *fakeCarryImages) DownloadCarryImage(_ context.Context, imageURL string, itemID string, index int) (CarryImageData, error) {
	f.calls = append(f.calls, index)
	f.lastItemID = itemID
	if f.failIndexes[index] {
		return CarryImageData{}, errors.New("下载失败: " + imageURL)
	}
	return CarryImageData{Filename: "carry-" + strconv.Itoa(index) + ".jpg", Data: []byte(imageURL)}, nil
}

// fakeCarryLibrary 是图片入库替身。
type fakeCarryLibrary struct {
	// saved 是入库返回结果。
	saved []CarryUploadedImage
	// err 是入库返回错误。
	err error
	// calls 记录调用次数。
	calls int
	// lastUserID 是最近一次请求携带的用户主键。
	lastUserID int64
	// received 是最近一次收到的图片集合。
	received []CarryImageData
}

// SaveCarryImages 记录入参并返回预置结果或错误。
func (f *fakeCarryLibrary) SaveCarryImages(_ context.Context, userID int64, images []CarryImageData) ([]CarryUploadedImage, error) {
	f.calls++
	f.lastUserID = userID
	f.received = images
	if f.err != nil {
		return nil, f.err
	}
	return f.saved, nil
}

// newCarryServiceForTest 构造搬运服务替身组合；nil 参数表示该能力缺失。
func newCarryServiceForTest(t *testing.T, detail CarryDetailProvider, images CarryImageFetcher, library CarryImageLibrary) *CarryService {
	t.Helper()
	// service、err 分别是搬运用例服务与构造错误。
	service, err := NewCarryService(detail, images, library)
	if err != nil {
		t.Fatalf("构造搬运服务失败: %v", err)
	}
	return service
}

// TestNewCarryServiceValidation 验证搬运服务的必填依赖校验。
func TestNewCarryServiceValidation(t *testing.T) {
	// detail 是最小可用详情替身。
	detail := &fakeCarryDetail{}
	// library 是最小可用入库替身。
	library := &fakeCarryLibrary{}
	// 详情缺失必须构造失败。
	if _, err := NewCarryService(nil, nil, library); err == nil {
		t.Fatal("缺少详情能力应构造失败")
	}
	// 资源库缺失必须返回稳定错误，供 transport 映射为 503。
	if _, err := NewCarryService(detail, nil, nil); !errors.Is(err, ErrImageLibraryUnavailable) {
		t.Fatalf("缺少资源库应返回 ErrImageLibraryUnavailable, got %v", err)
	}
	// 图片下载缺失时仍可构造（只搬运文案）。
	if _, err := NewCarryService(detail, nil, library); err != nil {
		t.Fatalf("缺少图片下载能力不应构造失败: %v", err)
	}
}

// TestCarryCompetitorItemSuccess 验证搬运成功路径：详情 → 改写 → 下载 → 入库。
func TestCarryCompetitorItemSuccess(t *testing.T) {
	// detail 是含两张图片的详情替身。
	detail := &fakeCarryDetail{
		detail: CompetitorDetail{
			Title: "竞品标题", DescText: "竞品描述", Price: "88",
			Images: []string{"https://example.com/a.jpg", "https://example.com/b.jpg"},
		},
		rewritten: RewrittenCopy{Title: "改写标题", Desc: "改写描述"},
	}
	// images 是下载替身。
	images := &fakeCarryImages{}
	// library 是返回两张已入库图片的替身。
	library := &fakeCarryLibrary{saved: []CarryUploadedImage{{ID: 7, Filename: "carry-0.jpg"}, {ID: 8, Filename: "carry-1.jpg"}}}
	// service 是被测服务。
	service := newCarryServiceForTest(t, detail, images, library)
	// result、err 是搬运结果与错误。
	result, err := service.CarryCompetitorItem(context.Background(), 42, "item-1")
	if err != nil {
		t.Fatalf("搬运失败: %v", err)
	}
	if result.Title != "改写标题" || result.Desc != "改写描述" || result.Price != "88" {
		t.Fatalf("预填文案不符: %+v", result)
	}
	if len(result.Images) != 2 || result.Images[0].ID != 7 || result.SameImagesHint != CarrySameImagesHint {
		t.Fatalf("图片或提示不符: %+v", result)
	}
	if len(images.calls) != 2 || images.lastItemID != "item-1" {
		t.Fatalf("图片下载调用不符: %+v", images.calls)
	}
	if library.calls != 1 || library.lastUserID != 42 || len(library.received) != 2 {
		t.Fatalf("图片入库调用不符: calls=%d user=%d n=%d", library.calls, library.lastUserID, len(library.received))
	}
}

// TestCarryCompetitorItemRewriteDegrade 验证 AI 改写失败时退化为竞品原文，搬运仍成功。
func TestCarryCompetitorItemRewriteDegrade(t *testing.T) {
	// detail 是改写必然失败的替身。
	detail := &fakeCarryDetail{
		detail:     CompetitorDetail{Title: "竞品标题", DescText: "竞品描述", Price: "88"},
		rewriteErr: ErrAINotConfigured,
	}
	// service 是被测服务；详情无图片，因此不触达下载与入库。
	service := newCarryServiceForTest(t, detail, &fakeCarryImages{}, &fakeCarryLibrary{})
	// result、err 是搬运结果与错误。
	result, err := service.CarryCompetitorItem(context.Background(), 1, "item-1")
	if err != nil {
		t.Fatalf("改写失败不应阻断搬运: %v", err)
	}
	if result.Title != "竞品标题" || result.Desc != "竞品描述" {
		t.Fatalf("应退化为竞品原文: %+v", result)
	}
	if result.Images == nil || len(result.Images) != 0 {
		t.Fatalf("无图片时应返回空集合: %+v", result.Images)
	}
}

// TestCarryCompetitorItemDetailError 验证详情失败时搬运直接返回该错误。
func TestCarryCompetitorItemDetailError(t *testing.T) {
	// sentinel 是详情错误。
	sentinel := errors.New("详情抓取失败")
	// detail 是详情必然失败的替身。
	detail := &fakeCarryDetail{detailErr: sentinel}
	// service 是被测服务。
	service := newCarryServiceForTest(t, detail, &fakeCarryImages{}, &fakeCarryLibrary{})
	// err 是搬运错误。
	_, err := service.CarryCompetitorItem(context.Background(), 1, "item-1")
	if !errors.Is(err, sentinel) {
		t.Fatalf("应透传详情错误, got %v", err)
	}
	if detail.rewriteCalls != 0 {
		t.Fatal("详情失败时不应调用改写")
	}
}

// TestCarryCompetitorItemImageFailures 验证单张图片下载失败只跳过该图片，全部失败时不入库。
func TestCarryCompetitorItemImageFailures(t *testing.T) {
	// detail 是含三张图片的详情替身。
	detail := &fakeCarryDetail{
		detail: CompetitorDetail{
			ItemID: "item-9", Title: "标题", DescText: "描述", Price: "1",
			Images: []string{"a", "b", "c"},
		},
		rewritten: RewrittenCopy{Title: "改写标题", Desc: "改写描述"},
	}
	// images 是第 0、2 张失败的下载替身。
	images := &fakeCarryImages{failIndexes: map[int]bool{0: true, 2: true}}
	// library 是入库替身。
	library := &fakeCarryLibrary{saved: []CarryUploadedImage{{ID: 3, Filename: "carry-1.jpg"}}}
	// service 是被测服务。
	service := newCarryServiceForTest(t, detail, images, library)
	// result、err 是搬运结果与错误。
	result, err := service.CarryCompetitorItem(context.Background(), 1, "item-9")
	if err != nil {
		t.Fatalf("部分图片失败不应中断搬运: %v", err)
	}
	if library.calls != 1 || len(library.received) != 1 || result.Images[0].ID != 3 {
		t.Fatalf("应只入库下载成功的图片: %+v", library.received)
	}

	// allFailed 是全部失败的下载替身。
	allFailed := &fakeCarryImages{failIndexes: map[int]bool{0: true, 1: true, 2: true}}
	// emptyLibrary 记录是否被调用。
	emptyLibrary := &fakeCarryLibrary{}
	// secondService 是被测服务。
	secondService := newCarryServiceForTest(t, detail, allFailed, emptyLibrary)
	// emptyResult、emptyErr 是全部失败时的搬运结果与错误。
	emptyResult, emptyErr := secondService.CarryCompetitorItem(context.Background(), 1, "item-9")
	if emptyErr != nil {
		t.Fatalf("全部图片失败仍应返回文案: %v", emptyErr)
	}
	if emptyLibrary.calls != 0 || len(emptyResult.Images) != 0 {
		t.Fatalf("无成功图片时不应入库: calls=%d images=%+v", emptyLibrary.calls, emptyResult.Images)
	}
}

// TestCarryCompetitorItemLibraryError 验证图片入库失败时整次搬运返回错误。
func TestCarryCompetitorItemLibraryError(t *testing.T) {
	// detail 是含一张图片的详情替身。
	detail := &fakeCarryDetail{
		detail:    CompetitorDetail{ItemID: "i1", Title: "标题", DescText: "描述", Images: []string{"a"}},
		rewritten: RewrittenCopy{Title: "改写标题", Desc: "改写描述"},
	}
	// sentinel 是入库错误。
	sentinel := errors.New("磁盘不可写")
	// service 是被测服务。
	service := newCarryServiceForTest(t, detail, &fakeCarryImages{}, &fakeCarryLibrary{err: sentinel})
	// err 是搬运错误。
	_, err := service.CarryCompetitorItem(context.Background(), 1, "i1")
	if !errors.Is(err, sentinel) {
		t.Fatalf("入库失败应返回包裹错误, got %v", err)
	}
}

// TestCarryCompetitorItemImageLimit 验证图片数量超过上限时只处理前 CarryMaxImages 张。
func TestCarryCompetitorItemImageLimit(t *testing.T) {
	// urls 是超过上限的图片地址集合。
	urls := make([]string, 0, CarryMaxImages+5)
	// index 构造图片地址。
	for index := 0; index < CarryMaxImages+5; index++ {
		urls = append(urls, "u"+strconv.Itoa(index))
	}
	// detail 是含超量图片的详情替身。
	detail := &fakeCarryDetail{
		detail:    CompetitorDetail{ItemID: "i1", Title: "标题", DescText: "描述", Images: urls},
		rewritten: RewrittenCopy{Title: "改写标题", Desc: "改写描述"},
	}
	// images 是下载替身。
	images := &fakeCarryImages{}
	// service 是被测服务。
	service := newCarryServiceForTest(t, detail, images, &fakeCarryLibrary{})
	// result、err 是搬运结果与错误。
	if _, err := service.CarryCompetitorItem(context.Background(), 1, "i1"); err != nil {
		t.Fatalf("搬运失败: %v", err)
	}
	if len(images.calls) != CarryMaxImages {
		t.Fatalf("下载次数应受上限约束, got %d", len(images.calls))
	}
}

// TestCarryCompetitorItemWithoutFetcher 验证缺少下载能力时只返回文案与空图片集合。
func TestCarryCompetitorItemWithoutFetcher(t *testing.T) {
	// detail 是含图片的详情替身（图片应被忽略）。
	detail := &fakeCarryDetail{
		detail:    CompetitorDetail{ItemID: "i1", Title: "标题", DescText: "描述", Images: []string{"a"}},
		rewritten: RewrittenCopy{Title: "改写标题", Desc: "改写描述"},
	}
	// library 是入库替身。
	library := &fakeCarryLibrary{}
	// service 是缺少下载能力的被测服务。
	service := newCarryServiceForTest(t, detail, nil, library)
	// result、err 是搬运结果与错误。
	result, err := service.CarryCompetitorItem(context.Background(), 1, "i1")
	if err != nil {
		t.Fatalf("缺少下载能力不应报错: %v", err)
	}
	if len(result.Images) != 0 || library.calls != 0 {
		t.Fatalf("缺少下载能力时不应入库: %+v", result.Images)
	}
}
