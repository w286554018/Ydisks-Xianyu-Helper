package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sourcingapp "xianyu-go/internal/application/sourcing"
	"xianyu-go/internal/browser"
)

// fakeSourcingAISettings 是 AI 配置读取替身，避免测试依赖真实敏感设置与审计表。
type fakeSourcingAISettings struct {
	// apiKey 是返回的密钥。
	apiKey string
	// keyErr 是读取密钥的错误。
	keyErr error
	// values 是非敏感设置键值。
	values map[string]string
}

// ReadAPIKey 返回预置密钥。
func (f fakeSourcingAISettings) ReadAPIKey(context.Context, int64) (string, error) {
	return f.apiKey, f.keyErr
}

// ReadSetting 返回预置设置。
func (f fakeSourcingAISettings) ReadSetting(_ context.Context, key string) (string, error) {
	return f.values[key], nil
}

// newSourcingChatTestClient 构造指向本地测试服务器的 AI 文案客户端。
func newSourcingChatTestClient(settings fakeSourcingAISettings) *SourcingChatClient {
	return &SourcingChatClient{settings: settings, newHTTPClient: func(string) (*http.Client, error) { return &http.Client{Timeout: 5 * time.Second}, nil }}
}

// TestSourcingChatClientSuccess 验证请求携带系统与用户提示、模型与鉴权头，并返回模型正文。
func TestSourcingChatClientSuccess(t *testing.T) {
	// captured 保存服务端收到的请求体，用于断言提示词与模型。
	var captured sourcingChatRequest
	// authHeader 保存服务端收到的鉴权头。
	var authHeader string
	// server 是本地模拟的 OpenAI 兼容服务，测试不访问外网。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		// body 是请求正文。
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"  标题\n描述  "}}]}`))
	}))
	defer server.Close()
	// client 是被测客户端。
	client := newSourcingChatTestClient(fakeSourcingAISettings{apiKey: "k-test", values: map[string]string{"ai_api_url": server.URL + "/v1/", "ai_model": "m1"}})

	// content、err 是生成结果。
	content, err := client.Complete(context.Background(), 1, "系统", "用户")
	if err != nil || content != "标题\n描述" {
		t.Fatalf("生成结果不符: %q err=%v", content, err)
	}
	if authHeader != "Bearer k-test" || captured.Model != "m1" || len(captured.Messages) != 2 || captured.Messages[0].Role != "system" || captured.Messages[1].Content != "用户" {
		t.Fatalf("请求内容不符: auth=%q req=%+v", authHeader, captured)
	}
}

// TestSourcingChatClientFallsBackToLegacyURLAndDefaultModel 验证兼容旧地址键并使用默认模型。
func TestSourcingChatClientFallsBackToLegacyURLAndDefaultModel(t *testing.T) {
	// model 保存服务端收到的模型名称。
	var model string
	// server 是本地模拟的 OpenAI 兼容服务，测试不访问外网。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// request 是解析后的请求体。
		var request sourcingChatRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		model = request.Model
		_, _ = w.Write([]byte(`{"choices":[{"text":"旧式文本"}]}`))
	}))
	defer server.Close()
	// client 是只配置旧地址键的客户端。
	client := newSourcingChatTestClient(fakeSourcingAISettings{apiKey: "k", values: map[string]string{"ai_base_url": server.URL}})
	// content、err 是生成结果。
	content, err := client.Complete(context.Background(), 1, "s", "u")
	if err != nil || content != "旧式文本" || model != sourcingAIDefaultModel {
		t.Fatalf("回退结果不符: content=%q model=%q err=%v", content, model, err)
	}
}

// TestSourcingChatClientErrors 验证缺少密钥、鉴权失败、非 2xx、非法 JSON 与空候选均返回明确错误且不泄露密钥。
func TestSourcingChatClientErrors(t *testing.T) {
	// err 是调用 newSourcingChatTestClient(...).Complete 的结果，按本用例期望断言成功或特定领域错误。
	if _, err := newSourcingChatTestClient(fakeSourcingAISettings{}).Complete(context.Background(), 1, "s", "u"); !errors.Is(err, sourcingapp.ErrAIConfigMissing) {
		t.Fatalf("缺少密钥应返回 AI 未配置, got %v", err)
	}
	// err 是读取 AI 密钥失败（审计不可用）时 Complete 的结果，期望为非空错误。
	if _, err := newSourcingChatTestClient(fakeSourcingAISettings{keyErr: errors.New("审计失败")}).Complete(context.Background(), 1, "s", "u"); err == nil {
		t.Fatal("读取密钥失败应返回错误")
	}
	// cases 覆盖远端失败形态。
	cases := []struct {
		name   string
		status int
		body   string
		isErr  error
	}{
		{"鉴权失败", http.StatusUnauthorized, `{}`, nil},
		{"服务失败", http.StatusInternalServerError, `{}`, nil},
		{"非法JSON", http.StatusOK, `not-json`, nil},
		{"空候选", http.StatusOK, `{"choices":[]}`, sourcingapp.ErrAIEmptyContent},
	}
	// c 是当前用例的输入与期望结果。
	for _, c := range cases {
		// server 是本地模拟的 OpenAI 兼容服务，测试不访问外网。
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))
		// client 是指向当前失败服务端的客户端。
		client := newSourcingChatTestClient(fakeSourcingAISettings{apiKey: "secret-key-value", values: map[string]string{"ai_api_url": server.URL}})
		// err 是调用 client.Complete 的结果，按本用例期望断言成功或特定领域错误。
		_, err := client.Complete(context.Background(), 1, "s", "u")
		server.Close()
		if err == nil {
			t.Fatalf("%s: 应返回错误", c.name)
		}
		if c.isErr != nil && !errors.Is(err, c.isErr) {
			t.Fatalf("%s: 错误类型不符 %v", c.name, err)
		}
		if strings.Contains(err.Error(), "secret-key-value") {
			t.Fatalf("%s: 错误信息泄露密钥", c.name)
		}
	}
	// invalid 是地址策略拒绝时的客户端。
	invalid := &SourcingChatClient{settings: fakeSourcingAISettings{apiKey: "k"}, newHTTPClient: func(string) (*http.Client, error) { return nil, errors.New("blocked") }}
	// err 是调用 invalid.Complete 的结果，按本用例期望断言成功或特定领域错误。
	if _, err := invalid.Complete(context.Background(), 1, "s", "u"); err == nil || !strings.Contains(err.Error(), "地址无效") {
		t.Fatalf("地址无效应报错, got %v", err)
	}
	// unreachable 是连接失败的客户端。
	unreachable := newSourcingChatTestClient(fakeSourcingAISettings{apiKey: "k", values: map[string]string{"ai_api_url": "http://127.0.0.1:1"}})
	// err 是调用 unreachable.Complete 的结果，按本用例期望断言成功或特定领域错误。
	if _, err := unreachable.Complete(context.Background(), 1, "s", "u"); err == nil {
		t.Fatal("连接失败应报错")
	}
}

// TestMapSourcingBrowserError 验证浏览器错误到应用领域错误的映射。
func TestMapSourcingBrowserError(t *testing.T) {
	// cases 是浏览器错误与期望领域错误。
	cases := map[error]error{
		browser.ErrSourcingRiskControl:           sourcingapp.ErrRiskControl,
		browser.ErrSourcingSessionExpired:        sourcingapp.ErrSessionExpired,
		browser.ErrSourcingNoResult:              sourcingapp.ErrNoResult,
		browser.ErrSourcingHotKeywordUnavailable: sourcingapp.ErrHotKeywordUnavailable,
	}
	// in 是浏览器层错误，want 是期望映射出的应用层领域错误。
	for in, want := range cases {
		// got 是 mapSourcingBrowserError 对当前用例输入的实际结果。
		if got := mapSourcingBrowserError(in); !errors.Is(got, want) {
			t.Fatalf("映射不符: %v -> %v", in, got)
		}
	}
	if mapSourcingBrowserError(nil) != nil {
		t.Fatal("nil 应保持 nil")
	}
	// other 是未知错误，应原样返回。
	other := errors.New("other")
	if mapSourcingBrowserError(other) != other {
		t.Fatal("未知错误应原样返回")
	}
	if NewSourcingBrowserGateway(nil) != nil {
		t.Fatal("浏览器未启用时网关应为 nil")
	}
}

// TestSourcingRepositoryAndCredentialReader 验证快照往返、不存在映射、订单读取与账号归属校验。
func TestSourcingRepositoryAndCredentialReader(t *testing.T) {
	// store 是预置管理员与账号 cid 的临时数据库；cleanup 在测试结束时关闭连接。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是测试上下文。
	ctx := context.Background()
	// repository 是被测持久化适配器。
	repository := NewSourcingRepository(store)

	// err 是调用 repository.GetSnapshot 的结果，按本用例期望断言成功或特定领域错误。
	if _, err := repository.GetSnapshot(ctx, sourcingapp.KindHotKeywords, "2026-10-29"); !errors.Is(err, sourcingapp.ErrSnapshotNotFound) {
		t.Fatalf("缺少快照应映射为领域错误, got %v", err)
	}
	// err 是调用 repository.SaveSnapshot 的结果，按本用例期望断言成功或特定领域错误。
	if err := repository.SaveSnapshot(ctx, sourcingapp.Snapshot{Kind: sourcingapp.KindHotKeywords, Date: "2026-10-29", PayloadJSON: `{"keywords":["a"]}`, Status: sourcingapp.StatusOK}); err != nil {
		t.Fatal(err)
	}
	// snapshot 是读回的快照。
	snapshot, err := repository.GetSnapshot(ctx, sourcingapp.KindHotKeywords, "2026-10-29")
	if err != nil || snapshot.Status != sourcingapp.StatusOK || snapshot.PayloadJSON != `{"keywords":["a"]}` {
		t.Fatalf("快照往返不符: %+v err=%v", snapshot, err)
	}
	// err 是调用 repository.DeleteSnapshotsBefore 的结果，按本用例期望断言成功或特定领域错误。
	if err := repository.DeleteSnapshotsBefore(ctx, "2026-10-30"); err != nil {
		t.Fatal(err)
	}
	// err 是调用 repository.GetSnapshot 的结果，按本用例期望断言成功或特定领域错误。
	if _, err := repository.GetSnapshot(ctx, sourcingapp.KindHotKeywords, "2026-10-29"); !errors.Is(err, sourcingapp.ErrSnapshotNotFound) {
		t.Fatal("清理后快照应不存在")
	}

	// admin 是夹具管理员，其名下有账号 cid。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// err 是调用 store.DB.ExecContext 的结果，按本用例期望断言成功或特定领域错误。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, cookie_id, quantity, amount, order_status) VALUES ('o1','i1','cid','2','10','completed')`); err != nil {
		t.Fatal(err)
	}
	// rows 是管理员窗口内订单。
	rows, err := repository.ListOrderRows(ctx, admin.ID, time.Now().Add(-24*time.Hour))
	if err != nil || len(rows) != 1 || rows[0].ItemID != "i1" || rows[0].Quantity != "2" {
		t.Fatalf("订单读取不符: %+v err=%v", rows, err)
	}

	// reader 是被测凭证读取适配器。
	reader := NewSourcingCredentialReader(store)
	// value 是账号所有者读取到的解密 cookie；err 是读取错误，期望为空。
	if value, err := reader.ReadCookie(ctx, admin.ID, "cid"); err != nil || !strings.Contains(value, "unb=1") {
		t.Fatalf("账号所有者应能读取 cookie, err=%v", err)
	}
	// err 是调用 reader.ReadCookie 的结果，按本用例期望断言成功或特定领域错误。
	if _, err := reader.ReadCookie(ctx, admin.ID+999, "cid"); !errors.Is(err, sourcingapp.ErrAccountNotOwned) {
		t.Fatalf("非所有者应被拒绝, got %v", err)
	}
	// err 是调用 reader.ReadCookie 的结果，按本用例期望断言成功或特定领域错误。
	if _, err := reader.ReadCookie(ctx, admin.ID, "  "); !errors.Is(err, sourcingapp.ErrAccountNotOwned) {
		t.Fatalf("空账号应被拒绝, got %v", err)
	}
}
