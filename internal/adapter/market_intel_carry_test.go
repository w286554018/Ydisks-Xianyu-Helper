package adapter

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	libraryapp "xianyu-go/internal/application/library"
	marketintel "xianyu-go/internal/application/marketintel"
)

// carryTestPNG 是 1×1 透明 PNG，用于搬运图片入库测试。
var carryTestPNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")

// TestNewMarketIntelCarryImageFetcherValidation 验证下载适配器拒绝空下载函数。
func TestNewMarketIntelCarryImageFetcherValidation(t *testing.T) {
	// 空下载函数必须构造失败，避免运行期空指针。
	if _, err := NewMarketIntelCarryImageFetcher(nil); err == nil {
		t.Fatal("空下载函数应构造失败")
	}
	// fetcher 是合法适配器。
	fetcher, err := NewMarketIntelCarryImageFetcher(func(context.Context, string) ([]byte, string, error) { return nil, "", nil })
	if err != nil || fetcher == nil {
		t.Fatalf("构造下载适配器失败: %v", err)
	}
}

// TestDownloadCarryImageFilenames 验证文件扩展名按媒体类型推断，商品标识缺失时用时间戳兜底。
func TestDownloadCarryImageFilenames(t *testing.T) {
	// contentType 是当前用例的响应媒体类型。
	cases := []struct {
		// name 是用例名。
		name string
		// contentType 是下载函数返回的媒体类型。
		contentType string
		// wantSuffix 是期望的文件扩展名。
		wantSuffix string
	}{
		{name: "png", contentType: "image/png", wantSuffix: ".png"},
		{name: "webp", contentType: "image/webp", wantSuffix: ".webp"},
		{name: "jpeg", contentType: "image/jpeg", wantSuffix: ".jpg"},
		{name: "unknown", contentType: "", wantSuffix: ".jpg"},
	}
	// tc 是当前媒体类型用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// fetcher 是返回固定媒体类型的下载适配器。
			fetcher, err := NewMarketIntelCarryImageFetcher(func(context.Context, string) ([]byte, string, error) {
				return []byte("png"), tc.contentType, nil
			})
			if err != nil {
				t.Fatalf("构造下载适配器失败: %v", err)
			}
			// data、downloadErr 是下载结果与错误。
			data, downloadErr := fetcher.DownloadCarryImage(context.Background(), "https://example.com/a", "item-7", 2)
			if downloadErr != nil {
				t.Fatalf("下载失败: %v", downloadErr)
			}
			if data.Filename != "carry-item-7-2"+tc.wantSuffix || string(data.Data) != "png" {
				t.Fatalf("文件名或字节不符: %+v", data)
			}
		})
	}
	// fallback 是商品标识缺失的下载适配器。
	fallback, _ := NewMarketIntelCarryImageFetcher(func(context.Context, string) ([]byte, string, error) {
		return []byte("png"), "image/png", nil
	})
	// blank 是空商品标识的下载结果。
	blank, blankErr := fallback.DownloadCarryImage(context.Background(), "https://example.com/a", "   ", 0)
	if blankErr != nil {
		t.Fatalf("空商品标识下载失败: %v", blankErr)
	}
	if !strings.HasPrefix(blank.Filename, "carry-") || !strings.HasSuffix(blank.Filename, "-0.png") {
		t.Fatalf("空商品标识应使用时间戳兜底: %s", blank.Filename)
	}
}

// TestDownloadCarryImageError 验证下载失败时返回原始错误且不产生文件名。
func TestDownloadCarryImageError(t *testing.T) {
	// sentinel 是下载错误。
	sentinel := errors.New("网络不可达")
	// fetcher 是必然失败的下载适配器。
	fetcher, _ := NewMarketIntelCarryImageFetcher(func(context.Context, string) ([]byte, string, error) {
		return nil, "", sentinel
	})
	// data、err 是下载结果与错误。
	data, err := fetcher.DownloadCarryImage(context.Background(), "https://example.com/a", "i1", 0)
	if !errors.Is(err, sentinel) {
		t.Fatalf("应透传下载错误, got %v", err)
	}
	if data.Filename != "" || data.Data != nil {
		t.Fatalf("失败时不应返回数据: %+v", data)
	}
}

// newCarryLibraryForTest 构造真实的资源库应用服务与一个用户主键，覆盖入库适配器完整路径。
func newCarryLibraryForTest(t *testing.T) (*libraryapp.Service, int64) {
	t.Helper()
	// store、cleanup 是测试数据库与关闭函数。
	store, cleanup := newDeliveryTemplateAdapterStore(t)
	t.Cleanup(cleanup)
	// 搬运图片入库需要用户外键，先写入一个测试用户。
	result, err := store.DB.ExecContext(context.Background(), `INSERT INTO users (username, email, password_hash) VALUES ('carry','carry@example.test','x')`)
	if err != nil {
		t.Fatalf("写入测试用户失败: %v", err)
	}
	// userID 是新建用户主键。
	userID, _ := result.LastInsertId()
	// fileStore 是上传根目录文件存储。
	fileStore, fileStoreErr := NewLibraryFileStore(t.TempDir())
	if fileStoreErr != nil {
		t.Fatalf("构造文件存储失败: %v", fileStoreErr)
	}
	// images、imagesErr 是图片库用例与构造错误。
	images, imagesErr := libraryapp.NewImageService(NewLibraryImageRepository(store), fileStore, LibraryImageInspector{})
	if imagesErr != nil {
		t.Fatalf("构造图片库用例失败: %v", imagesErr)
	}
	return libraryapp.NewService(images, nil, nil), userID
}

// TestNewMarketIntelImageLibraryValidation 验证入库适配器拒绝空资源库服务。
func TestNewMarketIntelImageLibraryValidation(t *testing.T) {
	// 空资源库必须构造失败，避免运行期空指针。
	if _, err := NewMarketIntelImageLibrary(nil); err == nil {
		t.Fatal("空资源库应构造失败")
	}
}

// TestSaveCarryImages 验证搬运图片写库：有效图片入库、非图片被跳过并保留失败原因。
func TestSaveCarryImages(t *testing.T) {
	// library、userID 是真实资源库应用服务与测试用户。
	library, userID := newCarryLibraryForTest(t)
	// uploader 是被测入库适配器。
	uploader, err := NewMarketIntelImageLibrary(library)
	if err != nil {
		t.Fatalf("构造入库适配器失败: %v", err)
	}
	// uploaded、saveErr 是入库结果与错误。
	uploaded, saveErr := uploader.SaveCarryImages(context.Background(), userID, []marketintel.CarryImageData{
		{Filename: "carry-i1-0.png", Data: carryTestPNG},
		{Filename: "carry-i1-1.txt", Data: []byte("not an image")},
	})
	if saveErr != nil {
		t.Fatalf("入库失败: %v", saveErr)
	}
	if len(uploaded) != 1 || uploaded[0].ID == 0 || uploaded[0].Filename != "carry-i1-0.png" {
		t.Fatalf("入库结果不符: %+v", uploaded)
	}
	// 同内容再次入库应命中去重，仍返回同一图片主键。
	repeated, repeatErr := uploader.SaveCarryImages(context.Background(), userID, []marketintel.CarryImageData{{Filename: "carry-i1-0.png", Data: carryTestPNG}})
	if repeatErr != nil {
		t.Fatalf("重复入库失败: %v", repeatErr)
	}
	if len(repeated) != 1 || repeated[0].ID != uploaded[0].ID {
		t.Fatalf("重复图片应命中去重: %+v", repeated)
	}
}

// TestSaveCarryImagesUploadError 验证图片数量超过资源库上限时返回错误。
func TestSaveCarryImagesUploadError(t *testing.T) {
	// library、userID 是真实资源库应用服务与测试用户。
	library, userID := newCarryLibraryForTest(t)
	// uploader 是被测入库适配器。
	uploader, _ := NewMarketIntelImageLibrary(library)
	// images 是超过资源库单次上传上限的图片集合。
	images := make([]marketintel.CarryImageData, 0, libraryapp.MaxUploadFiles+1)
	// index 构造超量图片。
	for index := 0; index <= libraryapp.MaxUploadFiles; index++ {
		images = append(images, marketintel.CarryImageData{Filename: "carry-i1.png", Data: carryTestPNG})
	}
	// err 是入库错误。
	if _, err := uploader.SaveCarryImages(context.Background(), userID, images); err == nil {
		t.Fatal("超过上传上限应返回错误")
	}
}
