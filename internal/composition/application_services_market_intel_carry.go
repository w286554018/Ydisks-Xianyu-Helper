package composition

import (
	"context"
	"fmt"

	"xianyu-go/internal/adapter"
	libraryapp "xianyu-go/internal/application/library"
	marketintelapp "xianyu-go/internal/application/marketintel"
)

// buildMarketIntelCarry 构造竞品一键搬运用例服务：详情与 AI 改写复用市场情报服务，图片入库复用资源库服务。
// 市场情报或资源库未启用时返回 nil，对应 HTTP 端点按功能未启用降级。
func buildMarketIntelCarry(marketIntel *marketintelapp.Service, library *libraryapp.Service) (*marketintelapp.CarryService, error) {
	if marketIntel == nil || library == nil {
		return nil, nil
	}
	return newMarketIntelCarry(marketIntel, library, downloadImageURL)
}

// newMarketIntelCarry 装配搬运服务；download 作为参数注入，便于测试下载能力缺失时的快速失败路径。
// 该用例不触发平台搜索或发布请求；图片只在用户主动点击搬运时下载，符合 MTOP 防风控约束。
func newMarketIntelCarry(marketIntel marketintelapp.CarryDetailProvider, library *libraryapp.Service, download func(ctx context.Context, rawURL string) ([]byte, string, error)) (*marketintelapp.CarryService, error) {
	// images 是竞品图片下载适配器，下载实现与批量发布、复制发布共用同一套公网限制。
	images, imagesErr := adapter.NewMarketIntelCarryImageFetcher(download)
	if imagesErr != nil {
		return nil, fmt.Errorf("构造竞品搬运图片下载适配器失败: %w", imagesErr)
	}
	// imageLibrary 是竞品图片入库适配器，写入本地资源库并返回可引用的图片主键。
	imageLibrary, imageLibraryErr := adapter.NewMarketIntelImageLibrary(library)
	if imageLibraryErr != nil {
		return nil, fmt.Errorf("构造竞品搬运图片入库适配器失败: %w", imageLibraryErr)
	}
	// carryService、carryErr 分别是搬运用例服务及其构造错误。
	carryService, carryErr := marketintelapp.NewCarryService(marketIntel, images, imageLibrary)
	if carryErr != nil {
		return nil, fmt.Errorf("构造竞品搬运服务失败: %w", carryErr)
	}
	return carryService, nil
}
