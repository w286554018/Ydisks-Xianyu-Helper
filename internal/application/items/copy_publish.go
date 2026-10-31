package items

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// CopySourceItem 是复制发布可读取的源商品快照视图；应用层自有模型，不向端口暴露持久化行结构。
type CopySourceItem struct {
	// ItemID 是源商品标识。
	ItemID string
	// Title 是商品标题。
	Title string
	// Description 是商品描述。
	Description string
	// Price 是售价文本。
	Price string
	// IsMultiSpec 表示该商品是否为多规格商品。
	IsMultiSpec bool
	// DetailJSON 是原始商品详情 JSON，用于解析平台图片地址列表。
	DetailJSON string
}

// 复制发布默认值：与批量铺货保持一致的发布间隔。
const copyDefaultPublishIntervalSeconds = 5

// copyImageNamePrefix 是复制任务批次目录内图片文件名前缀，后接序号与扩展名。
const copyImageNamePrefix = "copy_"

// ErrCopySourceMissing 表示源商品快照不存在或不可复制。
var ErrCopySourceMissing = errors.New("源商品不存在或快照数据不足，无法复制")

// ErrCopyMultiSpecUnsupported 表示多规格商品暂不支持复制发布。
var ErrCopyMultiSpecUnsupported = errors.New("多规格商品暂不支持复制发布，请使用批量铺货表格")

// ErrCopyNotOwned 表示源或目标账号不属于当前用户（HTTP 403）。
var ErrCopyNotOwned = errors.New("账号不属于当前用户")

// ErrCopyNoTargets 表示未选择任何可复制到的目标账号（HTTP 400）。
var ErrCopyNoTargets = errors.New("请选择要复制到的目标账号")

// ErrCopyTooManyTargets 表示目标账号数量超过单批上限（HTTP 400）。
var ErrCopyTooManyTargets = errors.New("目标账号数量超过单次复制上限")

// ErrCopyNoImages 表示源商品快照中没有任何可用图片。
var ErrCopyNoImages = errors.New("源商品快照缺少图片数据，请先在商品页执行同步后重试")

// CopySourceItems 读取源商品快照的仓储端口。
type CopySourceItems interface {
	// GetByCookieItem 按账号与商品主键读取商品快照行。
	GetByCookieItem(ctx context.Context, cookieID, itemID string) (CopySourceItem, error)
}

// CopyPreviewRowsValidator 复用批量预检行校验能力的窄接口（由 BatchPreviewService 实现）。
type CopyPreviewRowsValidator interface {
	// PreviewRows 校验结构化预检行，逐行错误写入各行。
	PreviewRows(ctx context.Context, input BatchPreviewInput, rows []BatchPreviewRow) ([]BatchPreviewRow, error)
}

// CopyPreviewPersister 复用批量预检批次落库能力的窄接口（由 BatchPreviewPersistenceService 实现）。
type CopyPreviewPersister interface {
	// Persist 持久化预检批次与全部明细。
	Persist(ctx context.Context, batch BatchPreviewPersistenceBatch, rows []BatchPreviewRow) (BatchPreviewPersistenceResult, error)
}

// copyOwnershipPort 复用批量预检的账号归属查询能力。
type copyOwnershipPort interface {
	// CookieOwned 判断账号是否属于指定用户。
	CookieOwned(context.Context, int64, string) (bool, error)
}

// CopyAccount 是复制预览中目标账号的预检视图（非敏感字段）。
type CopyAccount struct {
	// CookieID 是账号稳定标识。
	CookieID string
	// Name 是展示名（备注优先，昵称兜底）。
	Name string
	// Usable 表示该账号当前是否可作为复制目标。
	Usable bool
	// Reason 是不可用原因说明；可用时为空。
	Reason string
}

// CopyAccountsPort 读取用户名下账号摘要的端口（预检展示用，不读取任何凭据）。
type CopyAccountsPort interface {
	// ListForUser 返回用户名下全部账号摘要。
	ListForUser(ctx context.Context, userID int64) ([]AccountSummaryView, error)
}

// AccountSummaryView 是复制服务可见的账号摘要切片（避免依赖 account 应用包）。
type AccountSummaryView struct {
	// ID 是账号稳定标识。
	ID string
	// Remark 是账号备注。
	Remark string
	// Nickname 是平台昵称缓存。
	Nickname string
	// PausedUntil 是暂停截止时间的 Unix 秒；零表示未暂停。
	PausedUntil int64
}

// CopySnapshotStore 管理复制批次的上传目录与图片落盘。
type CopySnapshotStore interface {
	// Prepare 为批次创建专属上传目录并返回其绝对路径。
	Prepare(ctx context.Context, batchID string) (uploadDir string, err error)
	// SaveImage 把图片字节写入上传目录下的 name 文件。
	SaveImage(ctx context.Context, uploadDir, name string, data []byte) error
	// Discard 在预检失败时删除批次目录。
	Discard(ctx context.Context, uploadDir string) error
}

// CopyImageFetcher 从公网 CDN 下载图片字节的端口（非平台 MTOP 请求）。
type CopyImageFetcher interface {
	// Download 返回图片字节与媒体类型。
	Download(ctx context.Context, url string) (data []byte, contentType string, err error)
}

// CopyPreview 是复制预览的响应模型：源商品快照 + 可编辑字段 + 目标账号预检。
type CopyPreview struct {
	// SourceCookieID 是源账号标识。
	SourceCookieID string
	// SourceItemID 是源商品标识。
	SourceItemID string
	// Title 是预填标题（可编辑）。
	Title string
	// Description 是预填描述（可编辑）。
	Description string
	// Price 是预填售价文本（可编辑）。
	Price string
	// ImageURLs 是源商品平台图片地址列表（展示用）。
	ImageURLs []string
	// Targets 是目标账号预检结果。
	Targets []CopyAccount
}

// CopyTaskInput 是创建复制任务的输入。
type CopyTaskInput struct {
	// UserID 是发起用户。
	UserID int64
	// CookieID 是源账号。
	CookieID string
	// ItemID 是源商品。
	ItemID string
	// TargetCookieIDs 是目标账号列表。
	TargetCookieIDs []string
	// TitleEdit 为空时沿用源标题，非空时覆盖。
	TitleEdit string
	// DescriptionEdit 为空时沿用源描述，非空时覆盖。
	DescriptionEdit string
	// PriceEdit 为空时沿用源价格，非空时覆盖。
	PriceEdit string
	// PublishIntervalSeconds 是相邻发布的最小间隔；非正数使用默认值。
	PublishIntervalSeconds int
}

// CopyTaskResult 是创建复制任务的结果。
type CopyTaskResult struct {
	// BatchID 是生成的批量铺货批次标识，前端用既有批次进度接口轮询。
	BatchID string
	// RowCount 是批次行数（目标账号数）。
	RowCount int
}

// CopyPublishService 把已发布商品复制为「目标账号 × 商品内容」的批量铺货预检批次，
// 后续发布、进度、取消与重试完全复用既有批量 worker。
type CopyPublishService struct {
	// items 读取源商品快照。
	items CopySourceItems
	// ownership 校验账号归属。
	ownership copyOwnershipPort
	// accounts 读取账号摘要（预检展示）。
	accounts CopyAccountsPort
	// snapshots 管理批次目录与图片落盘。
	snapshots CopySnapshotStore
	// fetcher 下载平台 CDN 图片。
	fetcher CopyImageFetcher
	// validator 复用批量预检行校验。
	validator CopyPreviewRowsValidator
	// persister 复用批量预检批次落库。
	persister CopyPreviewPersister
	// newBatchID 生成批次标识。
	newBatchID func() string
	// maxRows 是单批最大行数。
	maxRows int
}

// NewCopyPublishService 构造复制发布服务并校验必需依赖。
func NewCopyPublishService(items CopySourceItems, ownership copyOwnershipPort, accounts CopyAccountsPort,
	snapshots CopySnapshotStore, fetcher CopyImageFetcher, validator CopyPreviewRowsValidator,
	persister CopyPreviewPersister, newBatchID func() string, maxRows int) (*CopyPublishService, error) {
	if items == nil || ownership == nil || accounts == nil || snapshots == nil || fetcher == nil || validator == nil || persister == nil || newBatchID == nil || maxRows <= 0 {
		return nil, errors.New("复制发布服务依赖不能为空")
	}
	return &CopyPublishService{items: items, ownership: ownership, accounts: accounts, snapshots: snapshots,
		fetcher: fetcher, validator: validator, persister: persister, newBatchID: newBatchID, maxRows: maxRows}, nil
}

// PreviewCopy 组装复制预览：读本地快照 + 目标账号预检，全程不发任何平台请求。
func (s *CopyPublishService) PreviewCopy(ctx context.Context, userID int64, cookieID, itemID string, targetCookieIDs []string) (CopyPreview, error) {
	// row 是源商品快照。
	row, err := s.loadSourceItem(ctx, userID, cookieID, itemID)
	if err != nil {
		return CopyPreview{}, err
	}
	// targets 是目标账号预检结果。
	targets, err := s.precheckTargets(ctx, userID, targetCookieIDs, cookieID)
	if err != nil {
		return CopyPreview{}, err
	}
	return CopyPreview{
		SourceCookieID: cookieID, SourceItemID: itemID,
		Title: row.Title, Description: row.Description, Price: row.Price,
		ImageURLs: snapshotImageURLs(row.DetailJSON),
		Targets:   targets,
	}, nil
}

// CreateCopyTask 下载图片快照、展开目标账号行、校验并落库为 preview 批次。
// 全程不发 MTOP 请求：图片走公网 CDN，发布由既有批量 worker 在调度窗口执行。
func (s *CopyPublishService) CreateCopyTask(ctx context.Context, input CopyTaskInput) (CopyTaskResult, error) {
	// row 是源商品快照。
	row, err := s.loadSourceItem(ctx, input.UserID, input.CookieID, input.ItemID)
	if err != nil {
		return CopyTaskResult{}, err
	}
	// 多规格商品本期不支持复制。
	if row.IsMultiSpec {
		return CopyTaskResult{}, ErrCopyMultiSpecUnsupported
	}
	// imageURLs 是快照中的平台图片地址。
	imageURLs := snapshotImageURLs(row.DetailJSON)
	if len(imageURLs) == 0 {
		return CopyTaskResult{}, ErrCopyNoImages
	}
	// targets 是去重后的目标账号（排除源账号自身）。
	targets := uniqueCopyTargets(input.TargetCookieIDs, input.CookieID)
	if len(targets) == 0 {
		return CopyTaskResult{}, ErrCopyNoTargets
	}
	if len(targets) > s.maxRows {
		return CopyTaskResult{}, fmt.Errorf("%w: 单次最多复制到 %d 个账号，当前选择 %d 个", ErrCopyTooManyTargets, s.maxRows, len(targets))
	}
	// 逐目标校验归属，任何一个不属于用户都整体失败。
	for _, target := range targets {
		// owned 表示归属校验结果。
		owned, ownErr := s.ownership.CookieOwned(ctx, input.UserID, target)
		if ownErr != nil {
			return CopyTaskResult{}, fmt.Errorf("校验目标账号失败: %w", ownErr)
		}
		if !owned {
			return CopyTaskResult{}, fmt.Errorf("%w: 目标账号 %s", ErrCopyNotOwned, target)
		}
	}
	// batchID 标识新批次；uploadDir 是其专属上传目录。
	batchID := s.newBatchID()
	// uploadDir 保存 Prepare 的返回结果。
	uploadDir, err := s.snapshots.Prepare(ctx, batchID)
	if err != nil {
		return CopyTaskResult{}, err
	}
	// result、taskErr 保存任务构建结果；失败时回收批次目录。
	result, taskErr := s.buildAndPersist(ctx, input, row, batchID, uploadDir, targets, imageURLs)
	if taskErr != nil {
		_ = s.snapshots.Discard(ctx, uploadDir)
		return CopyTaskResult{}, taskErr
	}
	return result, nil
}

// buildAndPersist 下载图片到批次目录、展开行、校验并落库。
func (s *CopyPublishService) buildAndPersist(ctx context.Context, input CopyTaskInput, row CopySourceItem,
	batchID, uploadDir string, targets []string, imageURLs []string) (CopyTaskResult, error) {
	// refs 是图片下载后在本批次目录内的文件名列表。
	refs, err := s.downloadImages(ctx, uploadDir, imageURLs)
	if err != nil {
		return CopyTaskResult{}, err
	}
	// title/description/price 是可编辑字段与源值的合并结果。
	title := mergeCopyField(input.TitleEdit, row.Title)
	// description 是描述与源值的合并结果。
	description := mergeCopyField(input.DescriptionEdit, row.Description)
	// price 是售价与源值的合并结果。
	price := mergeCopyField(input.PriceEdit, row.Price)
	// rows 按目标账号展开；行号从 1 开始与批量预检约定一致。
	rows := make([]BatchPreviewRow, 0, len(targets))
	// index 是目标账号序号。
	for index, target := range targets {
		rows = append(rows, BatchPreviewRow{
			RowNo: index + 1, CookieID: target,
			Title: title, Description: description, Price: price,
			Quantity: 1, PostageMode: "free", Images: refs,
		})
	}
	// validated 是逐行校验结果。
	validated, validateErr := s.validator.PreviewRows(ctx, BatchPreviewInput{
		UserID: input.UserID, DefaultCookieID: targets[0], UploadDir: uploadDir,
	}, rows)
	if validateErr != nil {
		return CopyTaskResult{}, validateErr
	}
	// interval 是生效的发布间隔。
	interval := input.PublishIntervalSeconds
	if interval <= 0 {
		interval = copyDefaultPublishIntervalSeconds
	}
	// persisted 是落库结果。
	persisted, persistErr := s.persister.Persist(ctx, BatchPreviewPersistenceBatch{
		ID: batchID, UserID: input.UserID, DefaultCookieID: targets[0],
		Filename: "商品复制发布", UploadDir: uploadDir,
		PublishIntervalSeconds: interval, Status: "preview",
	}, validated)
	if persistErr != nil {
		return CopyTaskResult{}, persistErr
	}
	return CopyTaskResult{BatchID: persisted.PreviewID, RowCount: len(targets)}, nil
}

// downloadImages 把快照图片逐一下载进批次目录，返回文件名列表。
// 任何一张下载失败都整体失败（复制任务要求内容完整）。
func (s *CopyPublishService) downloadImages(ctx context.Context, uploadDir string, imageURLs []string) ([]string, error) {
	// refs 累积文件名。
	refs := make([]string, 0, len(imageURLs))
	// index 是图片序号（从 1 开始）。
	for index, rawURL := range imageURLs {
		// data 与 contentType 是下载结果。
		data, contentType, err := s.fetcher.Download(ctx, rawURL)
		if err != nil {
			return nil, fmt.Errorf("下载第 %d 张图片失败: %w", index+1, err)
		}
		// name 是批次目录内文件名。
		name := fmt.Sprintf("%s%d%s", copyImageNamePrefix, index+1, copyImageExtension(rawURL, contentType))
		// saveErr 表示单张图片写盘失败原因。
		if saveErr := s.snapshots.SaveImage(ctx, uploadDir, name, data); saveErr != nil {
			return nil, fmt.Errorf("保存第 %d 张图片失败: %w", index+1, saveErr)
		}
		refs = append(refs, name)
	}
	return refs, nil
}

// loadSourceItem 读取并校验源商品快照（归属 + 存在性）。
func (s *CopyPublishService) loadSourceItem(ctx context.Context, userID int64, cookieID, itemID string) (CopySourceItem, error) {
	// owned 校验源账号归属。
	owned, err := s.ownership.CookieOwned(ctx, userID, cookieID)
	if err != nil {
		return CopySourceItem{}, fmt.Errorf("校验源账号失败: %w", err)
	}
	if !owned {
		return CopySourceItem{}, ErrCopyNotOwned
	}
	// row 是商品快照行。
	row, err := s.items.GetByCookieItem(ctx, cookieID, itemID)
	if err != nil {
		return CopySourceItem{}, ErrCopySourceMissing
	}
	if strings.TrimSpace(row.Title) == "" {
		return CopySourceItem{}, ErrCopySourceMissing
	}
	return row, nil
}

// precheckTargets 展开目标账号预检：归属、去重、排除源账号、暂停状态。
func (s *CopyPublishService) precheckTargets(ctx context.Context, userID int64, targetCookieIDs []string, sourceCookieID string) ([]CopyAccount, error) {
	// summaries 是用户名下账号摘要。
	summaries, err := s.accounts.ListForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("读取账号列表失败: %w", err)
	}
	// byID 按账号标识索引摘要。
	byID := make(map[string]AccountSummaryView, len(summaries))
	for _, summary := range summaries { // summary 是当前遍历的账号摘要。
		byID[summary.ID] = summary
	}
	// targets 是去重后的预检结果。
	targets := make([]CopyAccount, 0, len(targetCookieIDs))
	// seen 去重。
	seen := make(map[string]struct{}, len(targetCookieIDs))
	// now 用于暂停判定。
	now := time.Now().Unix()
	// raw 是当前候选目标。
	for _, raw := range targetCookieIDs { // raw 是当前遍历的候选目标。
		// id 是去空白后的账号标识。
		id := strings.TrimSpace(raw)
		if id == "" || id == sourceCookieID {
			continue
		}
		// ok 表示该标识已出现过。
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		// summary 是该账号摘要；缺失说明账号已被删除。
		summary, ok := byID[id]
		if !ok {
			targets = append(targets, CopyAccount{CookieID: id, Usable: false, Reason: "账号不存在"})
			continue
		}
		// account 组装预检视图。
		account := CopyAccount{CookieID: id, Name: copyDisplayName(summary), Usable: true}
		// 暂停中的账号提示但允许选择（暂停结束后发布才能执行；发布期遇暂停由 worker 既有逻辑处理）。
		if summary.PausedUntil > now {
			account.Usable = false
			account.Reason = "账号处于暂停期"
		}
		targets = append(targets, account)
	}
	return targets, nil
}

// copyDisplayName 取账号展示名：备注优先，昵称兜底，均为空时用账号标识。
func copyDisplayName(summary AccountSummaryView) string {
	// name 是备注优先的展示名。
	name := strings.TrimSpace(summary.Remark)
	if name == "" {
		name = strings.TrimSpace(summary.Nickname)
	}
	if name == "" {
		name = summary.ID
	}
	return name
}

// mergeCopyField 合并可编辑字段：编辑值为空时沿用源值。
func mergeCopyField(edit, source string) string {
	// trimmed 是编辑值。
	trimmed := strings.TrimSpace(edit)
	if trimmed != "" {
		return trimmed
	}
	return source
}

// uniqueCopyTargets 去重目标账号并排除源账号，保持选择顺序。
func uniqueCopyTargets(targets []string, sourceCookieID string) []string {
	// seen 去重。
	seen := make(map[string]struct{}, len(targets))
	// result 保存结果。
	var result []string
	// raw 是当前候选。
	for _, raw := range targets {
		// id 是去空白后的目标。
		id := strings.TrimSpace(raw)
		// 空值、源账号、重复值跳过。
		if id == "" || id == sourceCookieID {
			continue
		}
		// ok 表示该标识已出现过。
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

// snapshotImageURLs 从商品快照 detail JSON 提取平台图片地址列表。
// 兼容两种形态：列表同步快照的 detail_params.imageInfos（字符串化 JSON 数组）
// 与单品发布落库的 item_image 单图字段；顺序保持 imageInfos 原序。
func snapshotImageURLs(detailJSON string) []string {
	// detail 是快照顶层结构。
	var detail struct {
		// ItemImage 是单品发布落库的主图地址。
		ItemImage string `json:"item_image"`
		// DetailParams 是列表同步快照的卡片参数。
		DetailParams struct {
			// ImageInfos 是字符串化的图片信息 JSON 数组。
			ImageInfos string `json:"imageInfos"`
		} `json:"detail_params"`
	}
	// err 表示快照 detail 不是合法 JSON；解析失败视为无图片。
	if err := json.Unmarshal([]byte(detailJSON), &detail); err != nil {
		return nil
	}
	// urls 累积结果。
	var urls []string
	// imageInfos 非空时解析二次 JSON。
	if strings.TrimSpace(detail.DetailParams.ImageInfos) != "" {
		// infos 是图片信息数组。
		var infos []struct {
			// URL 是图片地址。
			URL string `json:"url"`
			// Major 表示是否主图。
			Major bool `json:"major"`
		}
		// err 表示二次 JSON 解析失败；失败时回退主图字段。
		if err := json.Unmarshal([]byte(detail.DetailParams.ImageInfos), &infos); err == nil {
			for _, info := range infos { // info 是当前遍历的图片信息。
				// u 是去空白后的图片地址。
				if u := strings.TrimSpace(info.URL); u != "" {
					urls = append(urls, u)
				}
			}
		}
	}
	// 无 imageInfos 时回退主图字段。
	if len(urls) == 0 && strings.TrimSpace(detail.ItemImage) != "" {
		urls = append(urls, strings.TrimSpace(detail.ItemImage))
	}
	return urls
}

// copyImageExtension 依据 URL 路径与媒体类型推导图片扩展名（含点）。
// 两者都不可用时回退 .jpg（闲鱼发布端按内容识别，扩展名仅用于展示）。
func copyImageExtension(rawURL, contentType string) string {
	// base 是 URL 路径末段。
	base := filepath.Base(strings.Split(rawURL, "?")[0])
	// ext 是路径扩展名。
	if ext := filepath.Ext(base); ext != "" {
		return ext
	}
	// 按 MIME 推导。
	switch contentType {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".jpg"
	}
}
