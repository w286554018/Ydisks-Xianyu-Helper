// Package marketintel: 竞品一键搬运用例——详情 → AI 改写 → 图片入资源库 → 预填数据。
package marketintel

import (
	"context"
	"errors"
	"fmt"
)

// CarrySameImagesHint 是搬运结果固定提示：图片与竞品一致时建议替换首图。
const CarrySameImagesHint = "图片与竞品相同，建议替换或调整首图以降低重复判定"

// CarryMaxImages 是单次搬运最多入库的图片数量，与资源库单次上传上限保持一致。
const CarryMaxImages = 20

// CarryImageData 是下载完成、尚待入库的单张竞品图片。
type CarryImageData struct {
	// Filename 是入库文件名，含按媒体类型推断的扩展名。
	Filename string
	// Data 是图片原始字节；由资源库按内容摘要去重。
	Data []byte
}

// CarryUploadedImage 是搬运完成后资源库中的图片引用。
type CarryUploadedImage struct {
	// ID 是资源库图片主键，供发布表单直接引用。
	ID int64
	// Filename 是资源库中的文件名。
	Filename string
}

// CarryResult 是搬运用例结果：预填文案、价格与已入库图片。
type CarryResult struct {
	// Title 是预填标题（AI 改写失败时退化为竞品原文）。
	Title string
	// Desc 是预填描述（AI 改写失败时退化为竞品原文）。
	Desc string
	// Price 是竞品价格文本。
	Price string
	// Images 是本次成功入库的资源库图片。
	Images []CarryUploadedImage
	// SameImagesHint 是固定提示文案。
	SameImagesHint string
}

// ErrImageLibraryUnavailable 表示资源库功能不可用，搬运无法完成。
var ErrImageLibraryUnavailable = errors.New("资源库功能未启用，无法搬运竞品图片")

// CarryDetailProvider 是搬运依据的详情与改写能力边界（由市场情报 Service 实现）。
// 组合根通过它注入明细能力，因此搬运装配可以在详情缺失时快速失败。
type CarryDetailProvider interface {
	// GetCompetitorDetail 读取竞品详情（7 天内优先读缓存）。
	GetCompetitorDetail(ctx context.Context, itemID string) (CompetitorDetail, error)
	// RewriteCompetitorCopy 用 AI 改写竞品文案。
	RewriteCompetitorCopy(ctx context.Context, title, desc string) (RewrittenCopy, error)
}

// CarryImageFetcher 是竞品图片下载边界；实现方保证一次调用只下载一张图片。
type CarryImageFetcher interface {
	// DownloadCarryImage 下载单张竞品图片并生成入库文件名。
	DownloadCarryImage(ctx context.Context, imageURL string, itemID string, index int) (CarryImageData, error)
}

// CarryImageLibrary 是图片入库边界；实现方保证一次调用只写入一批图片。
type CarryImageLibrary interface {
	// SaveCarryImages 把下载好的图片写入本地资源库，返回成功入库的图片。
	SaveCarryImages(ctx context.Context, userID int64, images []CarryImageData) ([]CarryUploadedImage, error)
}

// CarryService 编排竞品一键搬运用例：详情 → AI 改写 → 图片入资源库 → 预填数据。
// 该服务不触发任何平台搜索或发布请求，图片只在用户主动点击搬运时才下载。
type CarryService struct {
	// detail 提供竞品详情与 AI 改写能力。
	detail CarryDetailProvider
	// images 是竞品图片下载边界；可为空（浏览器不可用时只搬运文案）。
	images CarryImageFetcher
	// library 是图片入库边界，缺失时构造失败。
	library CarryImageLibrary
}

// NewCarryService 构造搬运用例服务；详情与图片入库能力必填，图片下载能力可空。
func NewCarryService(detail CarryDetailProvider, images CarryImageFetcher, library CarryImageLibrary) (*CarryService, error) {
	if detail == nil {
		return nil, errors.New("搬运服务缺少竞品详情能力")
	}
	if library == nil {
		return nil, ErrImageLibraryUnavailable
	}
	return &CarryService{detail: detail, images: images, library: library}, nil
}

// CarryCompetitorItem 执行搬运：读取详情 → AI 改写（失败降级原文）→ 下载图片入库 → 返回预填数据。
// 单张图片下载失败只跳过该图片，不阻断整个搬运；入库失败则整次搬运返回错误。
func (s *CarryService) CarryCompetitorItem(ctx context.Context, userID int64, itemID string) (CarryResult, error) {
	// detail 是搬运依据的竞品详情（缓存优先）。
	detail, err := s.detail.GetCompetitorDetail(ctx, itemID)
	if err != nil {
		return CarryResult{}, err
	}
	// rewritten 是 AI 改写结果；模型未配置或调用失败时退化为竞品原文，保证搬运可用。
	rewritten, rewriteErr := s.detail.RewriteCompetitorCopy(ctx, detail.Title, detail.DescText)
	if rewriteErr != nil {
		rewritten = RewrittenCopy{Title: detail.Title, Desc: detail.DescText}
	}
	// images 是下载成功的图片字节集合，数量受 CarryMaxImages 限制。
	images := s.downloadCarryImages(ctx, detail)
	// uploaded 是成功写入资源库的图片；无图片可入库时保持为空集合。
	uploaded := make([]CarryUploadedImage, 0, len(images))
	if len(images) > 0 {
		// saved、saveErr 分别是图片入库结果与入库失败原因。
		saved, saveErr := s.library.SaveCarryImages(ctx, userID, images)
		if saveErr != nil {
			return CarryResult{}, fmt.Errorf("搬运竞品图片入库失败: %w", saveErr)
		}
		uploaded = saved
		if uploaded == nil {
			uploaded = make([]CarryUploadedImage, 0, 0)
		}
	}
	return CarryResult{
		Title: rewritten.Title, Desc: rewritten.Desc, Price: detail.Price,
		Images: uploaded, SameImagesHint: CarrySameImagesHint,
	}, nil
}

// downloadCarryImages 逐张下载竞品图片；单张失败跳过，下载能力缺失时返回空集合。
func (s *CarryService) downloadCarryImages(ctx context.Context, detail CompetitorDetail) []CarryImageData {
	// images 是下载成功的图片集合。
	images := make([]CarryImageData, 0, len(detail.Images))
	if s.images == nil {
		return images
	}
	// index 是图片在详情中的序号，遍历上限为 CarryMaxImages。
	for index, imageURL := range detail.Images {
		if index >= CarryMaxImages {
			break
		}
		// data 是单张图片的下载结果。
		data, downloadErr := s.images.DownloadCarryImage(ctx, imageURL, detail.ItemID, index)
		if downloadErr != nil {
			continue
		}
		images = append(images, data)
	}
	return images
}
