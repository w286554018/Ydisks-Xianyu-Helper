package sourcing

import (
	"context"
	"strings"
)

// listingSystemPrompt 是上架文案模式的系统提示，约束输出可直接粘贴到发布页。
const listingSystemPrompt = `你是闲鱼卖家的文案助手。请根据用户给出的品类或商品信息，输出可直接粘贴到闲鱼发布页的内容。
要求：
1. 第一行只写商品标题，不超过 30 个字，突出品类、核心卖点与适用人群。
2. 从第二行开始写商品描述，150 到 400 字，分 3 到 5 条要点，说明成色、规格、适用场景与售后说明。
3. 不写价格承诺、不写绝对化用语、不写平台禁止的诱导交易话术。
4. 只输出文案正文，不要解释你的思路，不要使用 Markdown 代码块。`

// analysisSystemPrompt 是品类分析模式的系统提示，约束输出可执行的选品判断。
const analysisSystemPrompt = `你是闲鱼选品分析助手。请根据用户给出的品类关键词或商品信息，输出简明分析。
要求：
1. 用 3 到 5 条说明这个品类当前的买家关注点与常见需求。
2. 给出售价区间建议，并说明判断依据（买家价格敏感度、同类供给密度）。
3. 指出 2 到 3 个经营风险或注意事项，例如退换争议、成色描述、平台规则。
4. 只输出分析正文，不要解释你的思路，不要使用 Markdown 代码块。`

// GenerateCopywriting 调用 AI 生成指定模式的文案；AI 未配置或返回空内容时返回明确错误。
func (s *Service) GenerateCopywriting(ctx context.Context, userID int64, req CopywritingRequest) (Copywriting, error) {
	// mode 是归一化后的文案模式。
	mode := strings.TrimSpace(req.Mode)
	if mode != CopywritingModeListing && mode != CopywritingModeAnalysis {
		return Copywriting{}, ErrInvalidCopywritingMode
	}
	// topic 是去空白后的主题文本，空值无法生成有意义内容。
	topic := strings.TrimSpace(req.Topic)
	if topic == "" {
		return Copywriting{}, ErrInvalidCopywritingMode
	}
	if s.chat == nil {
		return Copywriting{}, ErrAIConfigMissing
	}
	// systemPrompt 是本次模式对应的系统提示。
	systemPrompt := listingSystemPrompt
	if mode == CopywritingModeAnalysis {
		systemPrompt = analysisSystemPrompt
	}
	// userPrompt 是拼装后的用户提示，包含主题与可选参考信息。
	userPrompt := buildCopywritingUserPrompt(topic, req.Reference)
	// content、err 是 AI 补全结果与错误。
	content, err := s.chat.Complete(ctx, userID, systemPrompt, userPrompt)
	if err != nil {
		return Copywriting{}, err
	}
	// trimmed 是去首尾空白后的生成内容，空内容视为失败而不是成功。
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return Copywriting{}, ErrAIEmptyContent
	}
	return Copywriting{Mode: mode, Content: trimmed}, nil
}

// buildCopywritingUserPrompt 拼装用户提示，把主题与参考信息组织成稳定的输入格式。
func buildCopywritingUserPrompt(topic, reference string) string {
	// builder 逐步拼接提示正文。
	var builder strings.Builder
	builder.WriteString("品类或商品：")
	builder.WriteString(topic)
	// trimmedReference 是去空白后的参考信息，空值时不输出该段以免干扰模型。
	trimmedReference := strings.TrimSpace(reference)
	if trimmedReference != "" {
		builder.WriteString("\n参考信息：")
		builder.WriteString(trimmedReference)
	}
	return builder.String()
}
