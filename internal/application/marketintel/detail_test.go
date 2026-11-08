// Package marketintel: 竞品详情缓存、AI 改写与卖点分析的焦点测试。
package marketintel

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeDetailFetcher 是竞品详情抓取替身。
type fakeDetailFetcher struct {
	// detail 是抓取返回值。
	detail CompetitorDetail
	// err 是抓取返回错误。
	err error
	// calls 记录抓取调用次数。
	calls int
}

// FetchCompetitorDetail 返回预置详情或错误。
func (f *fakeDetailFetcher) FetchCompetitorDetail(_ context.Context, itemID string) (CompetitorDetail, error) {
	f.calls++
	if f.err != nil {
		return CompetitorDetail{}, f.err
	}
	// out 是本次调用的详情副本。
	out := f.detail
	out.ItemID = itemID
	return out, nil
}

// fakeDetailCacheScan 是同时提供扫描持久化与详情缓存的替身。
type fakeDetailCacheScan struct {
	// fakeScanStore 复用扫描持久化替身的最小实现。
	fakeScanStore
	// rows 是内存缓存行，键为商品标识。
	rows map[string]detailCacheRow
	// loadErr 是缓存读取错误。
	loadErr error
	// saveErr 是缓存写入错误。
	saveErr error
	// saved 记录写入的缓存行。
	saved []detailCacheRow
}

// LoadDetailRow 读取内存缓存行。
func (f *fakeDetailCacheScan) LoadDetailRow(_ context.Context, itemID string) (detailCacheRow, error) {
	if f.loadErr != nil {
		return detailCacheRow{}, f.loadErr
	}
	// row、ok 是本次查询结果。
	row, ok := f.rows[itemID]
	if !ok {
		return detailCacheRow{}, errors.New("竞品详情不存在")
	}
	return row, nil
}

// SaveDetailRow 记录写入的缓存行。
func (f *fakeDetailCacheScan) SaveDetailRow(_ context.Context, row detailCacheRow) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, row)
	return nil
}

// fakeAIRewriter 是 AI 改写与分析替身。
type fakeAIRewriter struct {
	// rewritten 是改写返回值。
	rewritten RewrittenCopy
	// rewriteErr 是改写返回错误。
	rewriteErr error
	// analysis 是分析返回值。
	analysis string
	// analyzeErr 是分析返回错误。
	analyzeErr error
	// rewriteCalls 记录改写调用次数。
	rewriteCalls int
	// analyzeCalls 记录分析调用次数。
	analyzeCalls int
}

// JudgeBatch 让替身满足判定端口；本测试不使用批量判定。
func (f *fakeAIRewriter) JudgeBatch(context.Context, string, []VerdictInput) ([]VerdictResult, error) {
	return nil, nil
}

// RewriteCopy 返回预置改写结果或错误。
func (f *fakeAIRewriter) RewriteCopy(context.Context, string, string) (RewrittenCopy, error) {
	f.rewriteCalls++
	if f.rewriteErr != nil {
		return RewrittenCopy{}, f.rewriteErr
	}
	return f.rewritten, nil
}

// AnalyzeSellingPoints 返回预置分析结果或错误。
func (f *fakeAIRewriter) AnalyzeSellingPoints(context.Context, string, string, string, string) (string, error) {
	f.analyzeCalls++
	if f.analyzeErr != nil {
		return "", f.analyzeErr
	}
	return f.analysis, nil
}

// newDetailServiceForTest 构造带详情缓存能力的市场情报服务。
func newDetailServiceForTest(scan scanPersistence, detail DetailFetcher, verdict VerdictPort) *Service {
	return NewService(newFakeStore(), scan, nil, verdict, detail, nil)
}

// TestGetCompetitorDetailCacheFresh 验证 7 天内缓存直接复用且不抓取。
func TestGetCompetitorDetailCacheFresh(t *testing.T) {
	// scan 是含新鲜缓存行的替身。
	scan := &fakeDetailCacheScan{rows: map[string]detailCacheRow{
		"item-1": {
			ItemID: "item-1", Title: "缓存标题", Price: "88", DescText: "缓存描述",
			ImagesJSON: `["https://example.com/a.jpg"]`, SellerNickname: "卖家甲", SellerID: "u1",
			WantCountText: "10人想要", FetchedAt: time.Now().Add(-time.Hour),
		},
	}}
	// fetcher 是抓取替身；命中缓存时不应调用。
	fetcher := &fakeDetailFetcher{}
	// service 是被测服务。
	service := newDetailServiceForTest(scan, fetcher, nil)
	// detail、err 是详情结果与错误。
	detail, err := service.GetCompetitorDetail(context.Background(), "item-1")
	if err != nil {
		t.Fatalf("读取缓存失败: %v", err)
	}
	if !detail.Cached || detail.Title != "缓存标题" || len(detail.Images) != 1 || detail.WantCountText != "10人想要" {
		t.Fatalf("缓存详情不符: %+v", detail)
	}
	if fetcher.calls != 0 {
		t.Fatalf("命中缓存不应抓取, calls=%d", fetcher.calls)
	}
}

// TestGetCompetitorDetailCacheStale 验证缓存过期时实时抓取并回写缓存。
func TestGetCompetitorDetailCacheStale(t *testing.T) {
	// scan 是含过期缓存行的替身。
	scan := &fakeDetailCacheScan{rows: map[string]detailCacheRow{
		"item-1": {ItemID: "item-1", Title: "旧标题", FetchedAt: time.Now().Add(-8 * 24 * time.Hour)},
	}}
	// fetcher 是抓取替身。
	fetcher := &fakeDetailFetcher{detail: CompetitorDetail{Title: "新标题", DescText: "新描述", Price: "99", Images: []string{"u1"}}}
	// service 是被测服务。
	service := newDetailServiceForTest(scan, fetcher, nil)
	// detail、err 是详情结果与错误。
	detail, err := service.GetCompetitorDetail(context.Background(), "item-1")
	if err != nil {
		t.Fatalf("抓取失败: %v", err)
	}
	if detail.Cached || detail.Title != "新标题" || fetcher.calls != 1 {
		t.Fatalf("过期缓存应重新抓取: detail=%+v calls=%d", detail, fetcher.calls)
	}
	if len(scan.saved) != 1 || scan.saved[0].Title != "新标题" || scan.saved[0].ImagesJSON != `["u1"]` || scan.saved[0].FetchedAt.IsZero() {
		t.Fatalf("缓存回写不符: %+v", scan.saved)
	}
}

// TestGetCompetitorDetailCacheMissAndDegrades 验证缓存未命中、缓存损坏与浏览器不可用时的行为。
func TestGetCompetitorDetailCacheMissAndDegrades(t *testing.T) {
	// missing 是无任何缓存行的替身；缓存读取返回未找到。
	missing := &fakeDetailCacheScan{rows: map[string]detailCacheRow{}, loadErr: errors.New("竞品详情不存在")}
	// fetcher 是抓取替身。
	fetcher := &fakeDetailFetcher{detail: CompetitorDetail{Title: "标题"}}
	// service 是被测服务。
	service := newDetailServiceForTest(missing, fetcher, nil)
	// err 是本次调用的返回错误。
	if _, err := service.GetCompetitorDetail(context.Background(), "item-1"); err != nil || fetcher.calls != 1 {
		t.Fatalf("未命中缓存应抓取: calls=%d err=%v", fetcher.calls, err)
	}

	// broken 是图片 JSON 损坏的缓存替身；损坏时图片降级为空数组而非报错。
	broken := &fakeDetailCacheScan{rows: map[string]detailCacheRow{
		"item-2": {ItemID: "item-2", Title: "标题", ImagesJSON: "not-json", FetchedAt: time.Now()},
	}}
	// brokenService 是被测服务；抓取替身不应被调用。
	brokenFetcher := &fakeDetailFetcher{}
	// brokenService 是图片 JSON 损坏场景的被测服务。
	brokenService := newDetailServiceForTest(broken, brokenFetcher, nil)
	// brokenDetail、brokenErr 是损坏缓存行的读取结果。
	brokenDetail, brokenErr := brokenService.GetCompetitorDetail(context.Background(), "item-2")
	if brokenErr != nil || len(brokenDetail.Images) != 0 || brokenFetcher.calls != 0 {
		t.Fatalf("损坏图片 JSON 应降级为空数组: detail=%+v err=%v", brokenDetail, brokenErr)
	}

	// noBrowser 是无抓取能力且无缓存的服务。
	noBrowser := newDetailServiceForTest(&fakeDetailCacheScan{rows: map[string]detailCacheRow{}, loadErr: errors.New("竞品详情不存在")}, nil, nil)
	// noBrowserErr 是浏览器不可用时的返回错误。
	if _, noBrowserErr := noBrowser.GetCompetitorDetail(context.Background(), "item-3"); noBrowserErr == nil {
		t.Fatal("浏览器不可用且无缓存时应返回错误")
	}

	// fetchFailed 是抓取失败的服务；缓存读取同样未命中。
	fetchFailed := newDetailServiceForTest(&fakeDetailCacheScan{rows: map[string]detailCacheRow{}, loadErr: errors.New("竞品详情不存在")},
		&fakeDetailFetcher{err: errors.New("抓取失败")}, nil)
	// fetchErr 是抓取失败时的返回错误。
	if _, fetchErr := fetchFailed.GetCompetitorDetail(context.Background(), "item-4"); fetchErr == nil {
		t.Fatal("抓取失败应返回错误")
	}

	// saveFailed 是缓存写入失败的服务；抓取成功时仍必须返回详情。
	saveFailed := newDetailServiceForTest(&fakeDetailCacheScan{rows: map[string]detailCacheRow{}, loadErr: errors.New("竞品详情不存在"), saveErr: errors.New("磁盘不可写")},
		&fakeDetailFetcher{detail: CompetitorDetail{Title: "标题"}}, nil)
	// saveErr、savedDetail 是本次写入返回的错误；缓存写入失败时的详情结果。
	if savedDetail, saveErr := saveFailed.GetCompetitorDetail(context.Background(), "item-5"); saveErr != nil || savedDetail.Title != "标题" {
		t.Fatalf("缓存写入失败不应影响返回: detail=%+v err=%v", savedDetail, saveErr)
	}

	// noCacheService 的扫描持久化不含缓存能力，抓取能力可用时必须直接抓取。
	noCacheService := newDetailServiceForTest(&fakeScanStore{}, &fakeDetailFetcher{detail: CompetitorDetail{Title: "无缓存标题"}}, nil)
	// noCacheDetail、noCacheErr 是无缓存能力时的详情结果；无缓存能力时的返回错误。
	if noCacheDetail, noCacheErr := noCacheService.GetCompetitorDetail(context.Background(), "item-6"); noCacheErr != nil || noCacheDetail.Title != "无缓存标题" {
		t.Fatalf("无缓存能力时应直接抓取: detail=%+v err=%v", noCacheDetail, noCacheErr)
	}
}

// TestRewriteAndAnalyzeCompetitorCopy 验证改写与分析端口缺失与可用两种情况。
func TestRewriteAndAnalyzeCompetitorCopy(t *testing.T) {
	// rewriter 是 AI 替身。
	rewriter := &fakeAIRewriter{rewritten: RewrittenCopy{Title: "改写标题", Desc: "改写描述"}, analysis: "核心卖点"}
	// service 是装配了 AI 能力的服务。
	service := newDetailServiceForTest(&fakeScanStore{}, nil, rewriter)
	// rewritten、err 是改写结果与错误。
	rewritten, err := service.RewriteCompetitorCopy(context.Background(), "标题", "描述")
	if err != nil || rewritten.Title != "改写标题" || rewriter.rewriteCalls != 1 {
		t.Fatalf("改写结果不符: %+v err=%v calls=%d", rewritten, err, rewriter.rewriteCalls)
	}
	// analysis、analyzeErr 是分析结果与错误。
	analysis, analyzeErr := service.AnalyzeCompetitorSellingPoints(context.Background(), "标题", "描述", "9.9", "10人想要")
	if analyzeErr != nil || analysis != "核心卖点" || rewriter.analyzeCalls != 1 {
		t.Fatalf("分析结果不符: %q err=%v calls=%d", analysis, analyzeErr, rewriter.analyzeCalls)
	}

	// 端口未装配时必须返回稳定错误，供 transport 映射为 400。
	// notConfigured 是无 AI 能力的服务。
	notConfigured := newDetailServiceForTest(&fakeScanStore{}, nil, nil)
	// rewriteErr 是改写返回的错误。
	if _, rewriteErr := notConfigured.RewriteCompetitorCopy(context.Background(), "标题", "描述"); !errors.Is(rewriteErr, ErrAINotConfigured) {
		t.Fatalf("未装配 AI 能力应返回 ErrAINotConfigured, got %v", rewriteErr)
	}
	// analyzeErr 是卖点分析返回的错误。
	if _, analyzeErr := notConfigured.AnalyzeCompetitorSellingPoints(context.Background(), "标题", "描述", "9.9", "10人想要"); !errors.Is(analyzeErr, ErrAINotConfigured) {
		t.Fatalf("未装配 AI 能力应返回 ErrAINotConfigured, got %v", analyzeErr)
	}

	// 只装配判定端口的服务同样必须返回未配置（改写在别处实现）。
	verdictOnly := newDetailServiceForTest(&fakeScanStore{}, nil, &fakeVerdict{})
	// verdictErr 是缺少改写能力时返回的错误。
	if _, verdictErr := verdictOnly.RewriteCompetitorCopy(context.Background(), "标题", "描述"); !errors.Is(verdictErr, ErrAINotConfigured) {
		t.Fatalf("缺少改写能力应返回 ErrAINotConfigured, got %v", verdictErr)
	}
}

// TestTrimCopyFields 验证文案字段去空白。
func TestTrimCopyFields(t *testing.T) {
	// title、desc 是去空白后的标题与描述。
	title, desc := trimCopyFields("  标题  ", "\n描述\t")
	if title != "标题" || desc != "描述" {
		t.Fatalf("去空白不符: title=%q desc=%q", title, desc)
	}
}
