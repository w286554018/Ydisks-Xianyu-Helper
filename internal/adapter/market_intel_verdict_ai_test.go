// Package adapter: 市场情报模型端口的改写、卖点分析与请求失败分支焦点测试。
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	marketintel "xianyu-go/internal/application/marketintel"
)

// errMarketIntelTestClient 是测试中模拟客户端构造失败的错误。
var errMarketIntelTestClient = errors.New("测试用地址无效")

// marketIntelVerdictHarness 是一次市场情报模型端口测试的全部装配产物。
type marketIntelVerdictHarness struct {
	// port 是被测端口；其 HTTP 客户端已被替换为直连测试服务器。
	port *MarketIntelVerdictPort
	// server 是替代真实模型的本地测试服务器。
	server *httptest.Server
	// authorization 记录最近一次请求的认证头。
	authorization string
	// lastBody 记录最近一次请求的正文。
	lastBody string
	// mu 保护 authorization 与 lastBody 的并发访问。
	mu sync.Mutex
}

// newMarketIntelVerdictHarness 装配市场情报模型端口；respond 决定测试服务器对 /chat/completions 的响应。
func newMarketIntelVerdictHarness(t *testing.T, respond func(w http.ResponseWriter)) *marketIntelVerdictHarness {
	t.Helper()
	// 敏感设置需要数据密钥；测试使用固定值，避免读取真实密钥。
	t.Setenv("XIANYU_DATA_KEY", "unit-test-data-key")
	// store、cleanup 是测试数据库与关闭函数。
	store, cleanup := newDeliveryTemplateAdapterStore(t)
	t.Cleanup(cleanup)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// 敏感设置读取需要用户拥有者，先写入一个测试用户。
	result, err := store.DB.ExecContext(ctx, `INSERT INTO users (username, email, password_hash) VALUES ('mi','mi@example.test','x')`)
	if err != nil {
		t.Fatalf("写入测试用户失败: %v", err)
	}
	// userID 是新建用户主键。
	userID, _ := result.LastInsertId()
	// harness 是被测端口与请求记录容器。
	harness := &marketIntelVerdictHarness{}
	// server 是替代真实模型的本地测试服务器。
	harness.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// body 是最近一次请求正文。
		body, _ := io.ReadAll(r.Body)
		harness.mu.Lock()
		harness.authorization = r.Header.Get("Authorization")
		harness.lastBody = string(body)
		harness.mu.Unlock()
		respond(w)
	}))
	t.Cleanup(harness.server.Close)
	// 配置模型端点与密钥，保证 loadConfig 成功。
	if setErr := store.Settings.Set(ctx, SettingMarketIntelAPIURL, harness.server.URL); setErr != nil {
		t.Fatalf("写入模型地址失败: %v", setErr)
	}
	// setErr 是设置写入返回的错误。
	if setErr := store.Settings.Set(ctx, SettingMarketIntelModel, "unit-test-model"); setErr != nil {
		t.Fatalf("写入模型名称失败: %v", setErr)
	}
	// setErr 是设置写入返回的错误。
	if setErr := store.Settings.Set(ctx, SettingMarketIntelAPIKey, "unit-test-key"); setErr != nil {
		t.Fatalf("写入模型密钥失败: %v", setErr)
	}
	// port 是被测市场情报模型端口。
	harness.port = NewMarketIntelVerdictPort(store, userID)
	// 替换 HTTP 客户端，让本地测试服务器绕过出站策略限制。
	harness.port.newHTTPClient = func(string) (*http.Client, error) { return harness.server.Client(), nil }
	return harness
}

// writeChatResponse 写出 OpenAI 兼容的成功响应。
func writeChatResponse(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{{"message": map[string]string{"content": content}}},
	})
}

// TestMarketIntelRewriteCopy 验证文案改写的成功、栅栏剥离与失败分支。
func TestMarketIntelRewriteCopy(t *testing.T) {
	// content 是本次用例的模型正文。
	cases := []struct {
		// name 是用例名。
		name string
		// content 是模型返回的正文。
		content string
		// wantTitle 是期望标题；失败用例忽略。
		wantTitle string
		// wantErr 表示是否期望返回错误。
		wantErr bool
	}{
		{name: "裸 JSON", content: `{"title":"全新标题","desc":"全新描述"}`, wantTitle: "全新标题"},
		{name: "栅栏包裹", content: "```json\n{\"title\":\" 带空白标题 \",\"desc\":\"描述\"}\n```", wantTitle: "带空白标题"},
		{name: "非法 JSON", content: `不是 JSON`, wantErr: true},
		{name: "缺少标题", content: `{"title":"  ","desc":"描述"}`, wantErr: true},
	}
	// testCase 是当前对照表用例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// harness 是本次子测试独享的端口装配。
			harness := newMarketIntelVerdictHarness(t, func(w http.ResponseWriter) { writeChatResponse(w, testCase.content) })
			// rewritten、err 是改写结果与错误。
			rewritten, err := harness.port.RewriteCopy(context.Background(), "竞品标题", "竞品描述")
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("期望失败，实际成功: %+v", rewritten)
				}
				return
			}
			if err != nil {
				t.Fatalf("改写失败: %v", err)
			}
			if rewritten.Title != testCase.wantTitle || rewritten.Desc == "" {
				t.Fatalf("改写结果不符: %+v", rewritten)
			}
			// 请求必须带上 Bearer 认证头且正文含竞品标题。
			harness.mu.Lock()
			defer harness.mu.Unlock()
			if harness.authorization != "Bearer unit-test-key" || !strings.Contains(harness.lastBody, "竞品标题") {
				t.Fatalf("请求不符: auth=%q body=%s", harness.authorization, harness.lastBody)
			}
		})
	}
}

// TestMarketIntelRewriteCopyUnconfigured 验证模型密钥缺失时改写返回配置错误。
func TestMarketIntelRewriteCopyUnconfigured(t *testing.T) {
	// 敏感设置需要数据密钥。
	t.Setenv("XIANYU_DATA_KEY", "unit-test-data-key")
	// store、cleanup 是未配置模型密钥的测试数据库。
	store, cleanup := newDeliveryTemplateAdapterStore(t)
	t.Cleanup(cleanup)
	// port 是未配置密钥的端口。
	port := NewMarketIntelVerdictPort(store, 1)
	// err 是改写错误。
	if _, err := port.RewriteCopy(context.Background(), "标题", "描述"); err == nil {
		t.Fatal("未配置密钥应返回错误")
	}
	// analyzeErr 是卖点分析错误。
	if _, analyzeErr := port.AnalyzeSellingPoints(context.Background(), "标题", "描述", "9.9", "10人想要"); analyzeErr == nil {
		t.Fatal("未配置密钥应返回错误")
	}
	// 未配置必须包装应用层哨兵，否则传输层无法把它与「模型调用失败」区分开。
	if // rewriteErr 是改写错误，必须可被 errors.Is 识别为未配置。
	_, rewriteErr := port.RewriteCopy(context.Background(), "标题", "描述"); !errors.Is(rewriteErr, marketintel.ErrAINotConfigured) {
		t.Fatalf("改写未配置错误必须包装 ErrAINotConfigured，实际 %v", rewriteErr)
	}
	if // analyzeSentinelErr 是卖点分析错误，必须同样可识别。
	_, analyzeSentinelErr := port.AnalyzeSellingPoints(context.Background(), "标题", "描述", "9.9", "10人想要"); !errors.Is(analyzeSentinelErr, marketintel.ErrAINotConfigured) {
		t.Fatalf("卖点分析未配置错误必须包装 ErrAINotConfigured，实际 %v", analyzeSentinelErr)
	}
}

// TestMarketIntelAnalyzeSellingPoints 验证卖点分析透传模型正文。
func TestMarketIntelAnalyzeSellingPoints(t *testing.T) {
	// harness 是返回固定分析文本的端口装配。
	harness := newMarketIntelVerdictHarness(t, func(w http.ResponseWriter) { writeChatResponse(w, "  核心卖点：便宜  ") })
	// analysis、err 是分析结果与错误。
	analysis, err := harness.port.AnalyzeSellingPoints(context.Background(), "标题", "描述", "9.9", "10人想要")
	if err != nil || analysis != "核心卖点：便宜" {
		t.Fatalf("分析结果不符: %q err=%v", analysis, err)
	}
	// 请求正文必须包含价格与想要人数。
	harness.mu.Lock()
	defer harness.mu.Unlock()
	if !strings.Contains(harness.lastBody, "9.9") || !strings.Contains(harness.lastBody, "10人想要") {
		t.Fatalf("分析提示缺少关键信息: %s", harness.lastBody)
	}
}

// TestMarketIntelChatCompletionFailures 验证请求失败分支：客户端构造失败、非 2xx、非法响应与空结果。
func TestMarketIntelChatCompletionFailures(t *testing.T) {
	// status 是本次用例的响应状态码；body 是响应正文。
	cases := []struct {
		// name 是用例名。
		name string
		// status 是测试服务器返回的状态码。
		status int
		// body 是测试服务器返回的正文。
		body string
	}{
		{name: "认证失败", status: http.StatusUnauthorized, body: "{}"},
		{name: "服务错误", status: http.StatusBadGateway, body: "{}"},
		{name: "非法 JSON", status: http.StatusOK, body: "不是 JSON"},
		{name: "空结果", status: http.StatusOK, body: `{"choices":[]}`},
	}
	// testCase 是当前对照表用例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// harness 是返回固定失败响应的端口装配。
			harness := newMarketIntelVerdictHarness(t, func(w http.ResponseWriter) {
				w.WriteHeader(testCase.status)
				_, _ = w.Write([]byte(testCase.body))
			})
			// err 是改写错误。
			if _, err := harness.port.RewriteCopy(context.Background(), "标题", "描述"); err == nil {
				t.Fatal("失败响应应返回错误")
			}
			// batchErr 是批量判定错误。
			if _, batchErr := harness.port.JudgeBatch(context.Background(), "AI课程", []marketintel.VerdictInput{{SnapshotID: 1, Title: "AI课程"}}); batchErr == nil {
				t.Fatal("失败响应应返回错误")
			}
		})
	}
}

// TestMarketIntelModelClientConstructionFailure 验证模型地址客户端构造失败时返回包裹错误。
func TestMarketIntelModelClientConstructionFailure(t *testing.T) {
	// harness 是端口装配；响应内容无关紧要，因为客户端构造会先失败。
	harness := newMarketIntelVerdictHarness(t, func(w http.ResponseWriter) { writeChatResponse(w, `{"title":"t","desc":"d"}`) })
	// 让客户端构造必然失败，覆盖地址无效分支。
	harness.port.newHTTPClient = func(string) (*http.Client, error) { return nil, errMarketIntelTestClient }
	// rewriteErr 是改写错误。
	if _, rewriteErr := harness.port.RewriteCopy(context.Background(), "标题", "描述"); rewriteErr == nil {
		t.Fatal("客户端构造失败应返回错误")
	}
	// batchErr 是批量判定错误。
	if _, batchErr := harness.port.JudgeBatch(context.Background(), "AI课程", []marketintel.VerdictInput{{SnapshotID: 1, Title: "AI课程"}}); batchErr == nil {
		t.Fatal("客户端构造失败应返回错误")
	}
}

// TestMarketIntelJudgeBatchSuccess 验证批量判定成功路径与快照主键映射。
func TestMarketIntelJudgeBatchSuccess(t *testing.T) {
	// harness 是返回两条判定结果的端口装配。
	harness := newMarketIntelVerdictHarness(t, func(w http.ResponseWriter) {
		writeChatResponse(w, `[{"index":0,"is_ai_related":true,"category":"AI课程","reason":"标题含AI"},{"index":1,"is_ai_related":false,"category":"","reason":"普通商品"}]`)
	})
	// results、err 是批量判定结果与错误。
	results, err := harness.port.JudgeBatch(context.Background(), "AI课程", []marketintel.VerdictInput{
		{SnapshotID: 11, Title: "AI课程代做", Price: "99", WantCountText: "120人想要"},
		{SnapshotID: 12, Title: "蓝牙耳机", Price: "29", WantCountText: "8人想要"},
	})
	if err != nil || len(results) != 2 {
		t.Fatalf("批量判定不符: %+v err=%v", results, err)
	}
	if results[0].SnapshotID != 11 || !results[0].IsAIRelated || results[0].Category != "AI课程" {
		t.Fatalf("首条判定不符: %+v", results[0])
	}
	if results[1].SnapshotID != 12 || results[1].IsAIRelated {
		t.Fatalf("次条判定不符: %+v", results[1])
	}
}
