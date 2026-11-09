// Package localfetch: 本机助手 Chrome 调试端口客户端的参数、就绪探测与标签页生命周期测试。
package localfetch

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newChromeTestClient 构造一个 baseURL 指向给定测试服务的 Chrome 客户端。
func newChromeTestClient(baseURL, chromePath, profileDir string) *ChromeClient {
	return &ChromeClient{
		debugPort:  1,
		chromePath: chromePath,
		profileDir: profileDir,
		baseURL:    baseURL,
		httpClient: &http.Client{},
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// TestNewChromeClientDefaults 验证构造函数补齐默认日志器并生成调试端口基地址。
func TestNewChromeClientDefaults(t *testing.T) {
	// client 是未传日志器的客户端；必须回退到默认日志器而不是留空。
	client := NewChromeClient(9222, "/x/chrome", "/x/profile", nil)
	if client.logger == nil {
		t.Fatal("未传日志器时必须回退默认日志器")
	}
	if client.baseURL != "http://127.0.0.1:9222" {
		t.Fatalf("调试端口基地址不符: %s", client.baseURL)
	}
	if client.debugPort != 9222 || client.chromePath != "/x/chrome" || client.profileDir != "/x/profile" {
		t.Fatalf("客户端字段不符: %+v", client)
	}
}

// TestChromeArgsUsesProfileAndDebugPort 验证启动参数使用专用 profile 与调试端口，且不注入任何自动化开关。
func TestChromeArgsUsesProfileAndDebugPort(t *testing.T) {
	// client 是待检查启动参数的客户端。
	client := newChromeTestClient("http://127.0.0.1:1", "/x/chrome", "/x/profile")
	// args 是拼接出的 Chrome 启动参数。
	args := client.chromeArgs()
	if len(args) == 0 || args[0] != "--user-data-dir=/x/profile" {
		t.Fatalf("必须使用专用 profile 目录: %v", args)
	}
	if args[1] != "--remote-debugging-port=1" {
		t.Fatalf("必须指定调试端口: %v", args)
	}
	// joined 用于确认没有出现无头或自动化开关，保证平台看到的是真实浏览器。
	joined := strings.Join(args, " ")
	// forbidden 是启动参数中必须缺席的自动化开关。
	for _, forbidden := range []string{"--headless", "--enable-automation", "--disable-blink-features"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("启动参数不得包含 %s: %v", forbidden, args)
		}
	}
	if args[len(args)-1] != "about:blank" {
		t.Fatalf("启动页面必须为 about:blank: %v", args)
	}
}

// TestDebugReadyStatuses 验证调试端口探测只接受 200，其它状态与连接失败都视为未就绪。
func TestDebugReadyStatuses(t *testing.T) {
	// cases 是调试端口响应与期望就绪结果的对照表。
	cases := []struct {
		// name 是用例名。
		name string
		// status 是探测响应状态码。
		status int
		// wantReady 是期望是否就绪。
		wantReady bool
	}{
		{name: "就绪", status: http.StatusOK, wantReady: true},
		{name: "服务端错误未就绪", status: http.StatusInternalServerError, wantReady: false},
	}
	// tc 是当前探测用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// server 是模拟调试端口的测试服务。
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/json/version" {
					t.Errorf("探测路径不符: %s", r.URL.Path)
				}
				w.WriteHeader(tc.status)
			}))
			defer server.Close()
			// client 是被测客户端。
			client := newChromeTestClient(server.URL, "", "")
			if // ready 是本次探测结果。
			ready := client.debugReady(context.Background()); ready != tc.wantReady {
				t.Fatalf("就绪判断不符: got=%v want=%v", ready, tc.wantReady)
			}
		})
	}

	// 未启动的端口必须视为未就绪而不是报错。
	// offline 是已关闭的测试服务。
	offline := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	// offlineURL 是关闭前的服务地址。
	offlineURL := offline.URL
	offline.Close()
	// offlineClient 是被测客户端。
	offlineClient := newChromeTestClient(offlineURL, "", "")
	if offlineClient.debugReady(context.Background()) {
		t.Fatal("调试端口不可用时必须返回未就绪")
	}
}

// TestEnsureRunningShortCircuitsWhenReady 验证调试端口已就绪时不再尝试启动 Chrome。
func TestEnsureRunningShortCircuitsWhenReady(t *testing.T) {
	// server 是直接返回就绪的测试服务。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	// client 是被测客户端；chromePath 为空也不影响就该绪分支。
	client := newChromeTestClient(server.URL, "", "")
	if // err 是本次确保就绪的错误。
	err := client.EnsureRunning(context.Background()); err != nil {
		t.Fatalf("端口已就绪时不应报错: %v", err)
	}
}

// TestEnsureRunningRejectsUnusableChrome 验证未配置或找不到 Chrome 可执行文件时返回不可用错误。
func TestEnsureRunningRejectsUnusableChrome(t *testing.T) {
	// notReady 是调试端口不可用的测试服务。
	notReady := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer notReady.Close()

	// 未配置 Chrome 路径时必须明确报错。
	// blankPathClient 是未配置 Chrome 路径的客户端。
	blankPathClient := newChromeTestClient(notReady.URL, "  ", t.TempDir())
	if // blankErr 是未配置路径时的错误。
	blankErr := blankPathClient.EnsureRunning(context.Background()); !errors.Is(blankErr, ErrChromeUnavailable) {
		t.Fatalf("未配置 Chrome 路径必须返回不可用错误: %v", blankErr)
	}

	// 路径不存在时必须明确报错。
	// missingPathClient 是 Chrome 路径不存在的客户端。
	missingPathClient := newChromeTestClient(notReady.URL, filepath.Join(t.TempDir(), "absent-chrome"), t.TempDir())
	// missingErr 是路径不存在时的错误。
	missingErr := missingPathClient.EnsureRunning(context.Background())
	if !errors.Is(missingErr, ErrChromeUnavailable) || !strings.Contains(missingErr.Error(), "找不到 Chrome") {
		t.Fatalf("Chrome 路径不存在必须返回可定位错误: %v", missingErr)
	}
}

// TestEnsureRunningProfileAndLaunchFailures 验证专用 profile 目录创建失败与进程启动失败的分支。
func TestEnsureRunningProfileAndLaunchFailures(t *testing.T) {
	// notReady 是调试端口不可用的测试服务。
	notReady := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer notReady.Close()

	// profileFile 是占位用的普通文件；把它当作 profile 目录会让 MkdirAll 失败。
	profileFile := filepath.Join(t.TempDir(), "profile-file")
	if // writeErr 是占位文件写入失败原因。
	writeErr := os.WriteFile(profileFile, []byte("x"), 0o600); writeErr != nil {
		t.Fatalf("写入占位文件失败: %v", writeErr)
	}
	// profileClient 是 profile 目录不可用的客户端。
	profileClient := newChromeTestClient(notReady.URL, "/usr/bin/true", profileFile)
	// profileErr 是创建 profile 目录失败时的错误。
	profileErr := profileClient.EnsureRunning(context.Background())
	if !errors.Is(profileErr, ErrChromeUnavailable) || !strings.Contains(profileErr.Error(), "创建专用 profile 目录失败") {
		t.Fatalf("profile 目录不可用必须返回可定位错误: %v", profileErr)
	}

	// 不可执行的占位文件必须让进程启动失败并给出可定位错误。
	// nonExecutable 是不可执行的占位可执行文件路径。
	nonExecutable := filepath.Join(t.TempDir(), "not-executable")
	if // writeErr 是占位可执行文件写入失败原因。
	writeErr := os.WriteFile(nonExecutable, []byte("#!/bin/sh\n"), 0o600); writeErr != nil {
		t.Fatalf("写入占位可执行文件失败: %v", writeErr)
	}
	// launchClient 是启动必然失败的客户端。
	launchClient := newChromeTestClient(notReady.URL, nonExecutable, t.TempDir())
	// launchErr 是启动失败时的错误。
	launchErr := launchClient.EnsureRunning(context.Background())
	if !errors.Is(launchErr, ErrChromeUnavailable) || !strings.Contains(launchErr.Error(), "启动 Chrome 失败") {
		t.Fatalf("进程启动失败必须返回可定位错误: %v", launchErr)
	}
}

// TestEnsureRunningCancelledWhileWaiting 验证等待调试端口期间上下文取消会立即停止等待。
func TestEnsureRunningCancelledWhileWaiting(t *testing.T) {
	// notReady 是调试端口不可用的测试服务。
	notReady := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer notReady.Close()
	// cancelledCtx 是已取消的上下文；等待循环必须立刻返回而不是等到超时。
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	// client 是拉起了真实可执行文件但端口始终不可用的客户端。
	client := newChromeTestClient(notReady.URL, "/usr/bin/true", t.TempDir())
	if // err 是等待被取消时的错误。
	err := client.EnsureRunning(cancelledCtx); !errors.Is(err, ErrChromeUnavailable) {
		t.Fatalf("等待被取消必须返回不可用错误: %v", err)
	}
}

// TestOpenTabResponses 验证新建标签页的成功与各类失败分支。
func TestOpenTabResponses(t *testing.T) {
	// cases 是调试端口响应与期望结果的对照表。
	cases := []struct {
		// name 是用例名。
		name string
		// status 是响应状态码。
		status int
		// body 是响应正文。
		body string
		// wantErrText 是期望错误包含的片段；为空表示期望成功。
		wantErrText string
	}{
		{name: "成功", status: http.StatusOK, body: `{"id":"tab-1","webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/page/tab-1"}`},
		{name: "非 200", status: http.StatusInternalServerError, body: `{}`, wantErrText: "打开标签页返回状态 500"},
		{name: "正文非法", status: http.StatusOK, body: "not-json", wantErrText: "解析标签页描述失败"},
		{name: "缺少会话地址", status: http.StatusOK, body: `{"id":"tab-1"}`, wantErrText: "缺少 CDP 会话地址"},
	}
	// tc 是当前用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// server 是模拟新建标签页端点的测试服务。
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					t.Errorf("新建标签页必须使用 PUT，实际 %s", r.Method)
				}
				if !strings.HasPrefix(r.URL.RequestURI(), "/json/new?") {
					t.Errorf("端点不符: %s", r.URL.RequestURI())
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			// client 是被测客户端。
			client := newChromeTestClient(server.URL, "", "")
			// tab、err 是新建标签页结果与错误。
			tab, err := client.openTab(context.Background(), "about:blank")
			if tc.wantErrText == "" {
				if err != nil {
					t.Fatalf("成功路径不应报错: %v", err)
				}
				if tab.ID != "tab-1" || tab.WebSocketDebuggerURL == "" {
					t.Fatalf("标签页描述不符: %+v", tab)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErrText) || !errors.Is(err, ErrChromeUnavailable) {
				t.Fatalf("期望可定位的不可用错误 %q，实际 %v", tc.wantErrText, err)
			}
		})
	}
}

// TestCloseTabBehaviour 验证关闭标签页在空标识与失败响应下都不影响调用方。
func TestCloseTabBehaviour(t *testing.T) {
	// called 记录关闭端点是否被访问。
	called := false
	// server 是模拟关闭标签页端点的测试服务。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if !strings.HasPrefix(r.URL.Path, "/json/close/") {
			t.Errorf("关闭端点不符: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	// client 是被测客户端。
	client := newChromeTestClient(server.URL, "", "")

	// 空标识必须直接返回，不发起请求。
	client.closeTab(context.Background(), "  ")
	if called {
		t.Fatal("空标签页标识不应发起关闭请求")
	}
	// 关闭失败只记录日志；不得 panic，也不得影响调用方流程。
	client.closeTab(context.Background(), "tab-1")
	if !called {
		t.Fatal("有效标签页标识必须发起关闭请求")
	}
}
