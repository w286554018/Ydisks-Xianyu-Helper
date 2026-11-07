package adapter

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	libraryapp "xianyu-go/internal/application/library"
	marketintel "xianyu-go/internal/application/marketintel"
)

// MarketIntelCarryImageFetcher 把注入的公网图片下载函数适配为竞品搬运图片下载端口。
// 下载函数由组合根注入，与批量发布、复制发布共用同一套公网访问、大小与媒体类型限制。
type MarketIntelCarryImageFetcher struct {
	// download 是注入的公网图片下载实现。
	download func(ctx context.Context, rawURL string) ([]byte, string, error)
}

// NewMarketIntelCarryImageFetcher 构造竞品搬运图片下载适配器，下载函数缺失时快速失败。
func NewMarketIntelCarryImageFetcher(download func(ctx context.Context, rawURL string) ([]byte, string, error)) (*MarketIntelCarryImageFetcher, error) {
	if download == nil {
		return nil, errors.New("竞品搬运图片下载函数不能为空")
	}
	return &MarketIntelCarryImageFetcher{download: download}, nil
}

// DownloadCarryImage 下载单张竞品图片，并按媒体类型生成稳定的入库文件名。
func (f *MarketIntelCarryImageFetcher) DownloadCarryImage(ctx context.Context, imageURL string, itemID string, index int) (marketintel.CarryImageData, error) {
	// data、contentType、err 分别是图片字节、媒体类型与下载错误。
	data, contentType, err := f.download(ctx, imageURL)
	if err != nil {
		return marketintel.CarryImageData{}, err
	}
	return marketintel.CarryImageData{
		Filename: carryImageFilename(itemID, index, contentType),
		Data:     data,
	}, nil
}

// carryImageFilename 生成搬运图片文件名：商品标识 + 序号 + 按媒体类型推断的扩展名。
// 商品标识缺失时用毫秒时间戳兜底，保证同一详情内多次搬运的文件名互不冲突。
func carryImageFilename(itemID string, index int, contentType string) string {
	// base 是文件名主体。
	base := strings.TrimSpace(itemID)
	if base == "" {
		base = strconv.FormatInt(time.Now().UnixMilli(), 10)
	}
	return "carry-" + base + "-" + strconv.Itoa(index) + carryImageExtension(contentType)
}

// carryImageExtension 按响应媒体类型推断图片扩展名；未知类型按 JPEG 处理。
func carryImageExtension(contentType string) string {
	switch {
	case strings.Contains(contentType, "png"):
		return ".png"
	case strings.Contains(contentType, "webp"):
		return ".webp"
	default:
		return ".jpg"
	}
}

// MarketIntelImageLibrary 把资源库图片用例适配为竞品搬运图片入库端口。
type MarketIntelImageLibrary struct {
	// library 是本地资源库应用服务（图片上传按内容摘要去重）。
	library *libraryapp.Service
}

// NewMarketIntelImageLibrary 构造竞品搬运图片入库适配器，资源库缺失时构造失败。
func NewMarketIntelImageLibrary(library *libraryapp.Service) (*MarketIntelImageLibrary, error) {
	if library == nil {
		return nil, errors.New("竞品搬运图片入库需要资源库应用服务")
	}
	return &MarketIntelImageLibrary{library: library}, nil
}

// SaveCarryImages 把已下载的竞品图片写入本地资源库；单张失败只跳过该图片。
func (a *MarketIntelImageLibrary) SaveCarryImages(ctx context.Context, userID int64, images []marketintel.CarryImageData) ([]marketintel.CarryUploadedImage, error) {
	// uploads 是资源库上传入参集合。
	uploads := make([]libraryapp.ImageUpload, 0, len(images))
	// image 是当前待入库的竞品图片。
	for _, image := range images {
		uploads = append(uploads, libraryapp.ImageUpload{Filename: image.Filename, Data: image.Data})
	}
	// results、err 分别是单文件上传结果与整体失败原因。
	results, err := a.library.UploadImages(ctx, userID, uploads)
	if err != nil {
		return nil, err
	}
	// uploaded 是成功入库的图片引用集合。
	uploaded := make([]marketintel.CarryUploadedImage, 0, len(results))
	// result 是当前单文件上传结果。
	for _, result := range results {
		if result.Error != "" || result.Image.ID == 0 {
			continue
		}
		uploaded = append(uploaded, marketintel.CarryUploadedImage{ID: result.Image.ID, Filename: result.Image.Filename})
	}
	return uploaded, nil
}

// 编译期断言：适配器必须满足市场情报应用层定义的搬运端口。
var (
	// _ 断言竞品图片下载适配器满足市场情报图片下载端口。
	_ marketintel.CarryImageFetcher = (*MarketIntelCarryImageFetcher)(nil)
	// _ 断言资源库入库适配器满足市场情报图片入库端口。
	_ marketintel.CarryImageLibrary = (*MarketIntelImageLibrary)(nil)
)
