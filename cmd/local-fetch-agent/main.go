// Command local-fetch-agent 是运行在用户 Mac 上的「本机抓取助手」入口。
//
// 它只做参数解析、依赖构造与生命周期管理；取数与 HTTP 逻辑位于 internal/localfetch。
// 助手只监听回环地址，并要求调用方携带共享令牌，服务器侧经 SSH 反向隧道访问。
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"xianyu-go/internal/localfetch"
)

// agentFlags 汇总命令行参数。
type agentFlags struct {
	// listen 是监听地址，必须是回环地址。
	listen string
	// chromePath 是 Chrome 可执行文件路径。
	chromePath string
	// profileDir 是专用浏览器用户数据目录。
	profileDir string
	// debugPort 是 Chrome 调试端口。
	debugPort int
	// tokenFile 是共享令牌文件路径。
	tokenFile string
}

// main 是进程入口；退出码由 run 决定，便于测试。
func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

// run 解析参数、构造助手并阻塞提供服务；返回进程退出码。
func run(args []string, out io.Writer) int {
	// flags 是本进程的命令行参数。
	flags, parseErr := parseAgentFlags(args)
	if parseErr != nil {
		fmt.Fprintf(out, "参数错误: %v\n", parseErr)
		return 2
	}
	// loopbackErr 表示监听地址不在回环范围内；助手绝不允许暴露到网络。
	if loopbackErr := requireLoopback(flags.listen); loopbackErr != nil {
		fmt.Fprintf(out, "拒绝启动: %v\n", loopbackErr)
		return 2
	}
	// logger 是助手日志器；只输出到标准输出，不写任何凭据。
	logger := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// agent 是本机抓取助手实例。
	agent, agentErr := localfetch.NewAgent(localfetch.Config{
		Listen:     flags.listen,
		ChromePath: flags.chromePath,
		ProfileDir: flags.profileDir,
		DebugPort:  flags.debugPort,
		TokenFile:  flags.tokenFile,
		Logger:     logger,
	})
	if agentErr != nil {
		fmt.Fprintf(out, "初始化失败: %v\n", agentErr)
		return 1
	}

	// server 是助手 HTTP 服务；只监听回环，读写均有超时，避免慢连接占住资源。
	server := &http.Server{
		Addr:              flags.listen,
		Handler:           agent.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      180 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// shutdown 是收到终止信号后触发的关停通道。
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-shutdown
		logger.Info("收到退出信号，正在停止助手")
		// stopCtx 是优雅关停的上下文，3 秒内未完成即强制结束。
		stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(stopCtx)
	}()

	logger.Info("本机抓取助手已启动",
		"listen", flags.listen, "debug_port", flags.debugPort, "profile_dir", flags.profileDir)
	if // listenErr 是 HTTP 服务退出原因，正常关停不算错误。
	listenErr := server.ListenAndServe(); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
		fmt.Fprintf(out, "服务异常退出: %v\n", listenErr)
		return 1
	}
	return 0
}

// parseAgentFlags 解析命令行参数；缺省值全部落在用户主目录下的专用位置。
func parseAgentFlags(args []string) (agentFlags, error) {
	// home、homeErr 是用户主目录与读取失败原因。
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		return agentFlags{}, fmt.Errorf("读取用户主目录失败: %w", homeErr)
	}
	// flags 是带缺省值的参数集合。
	flags := agentFlags{
		listen:     localfetch.DefaultListen,
		chromePath: localfetch.DefaultMacChromePath,
		profileDir: filepath.Join(home, localfetch.DefaultChromeProfileDirName),
		debugPort:  localfetch.DefaultDebugPort,
		tokenFile:  filepath.Join(home, localfetch.DefaultTokenFileName),
	}
	// index 遍历参数；支持 -flag value 与 -flag=value 两种写法。
	for index := 0; index < len(args); index++ {
		// arg 是当前参数。
		arg := args[index]
		// name、inlineValue 是参数名与内联取值（存在等号时非空）。
		name, inlineValue, hasInline := strings.Cut(arg, "=")
		// value 是本次参数的取值。
		value := inlineValue
		// takeValue 在未使用内联写法时消费下一个参数。
		takeValue := func() error {
			if hasInline {
				return nil
			}
			if index+1 >= len(args) {
				return fmt.Errorf("%s 缺少取值", name)
			}
			index++
			value = args[index]
			return nil
		}
		switch name {
		case "-listen":
			if // err 是本次参数取值错误。
			err := takeValue(); err != nil {
				return agentFlags{}, err
			}
			flags.listen = strings.TrimSpace(value)
		case "-chrome-path":
			if // err 是本次参数取值错误。
			err := takeValue(); err != nil {
				return agentFlags{}, err
			}
			flags.chromePath = strings.TrimSpace(value)
		case "-profile-dir":
			if // err 是本次参数取值错误。
			err := takeValue(); err != nil {
				return agentFlags{}, err
			}
			flags.profileDir = strings.TrimSpace(value)
		case "-token-file":
			if // err 是本次参数取值错误。
			err := takeValue(); err != nil {
				return agentFlags{}, err
			}
			flags.tokenFile = strings.TrimSpace(value)
		case "-debug-port":
			if // err 是本次参数取值错误。
			err := takeValue(); err != nil {
				return agentFlags{}, err
			}
			// parsed、portErr 是端口取值与解析结果。
			parsed, portErr := parsePort(value)
			if portErr != nil {
				return agentFlags{}, portErr
			}
			flags.debugPort = parsed
		default:
			return agentFlags{}, fmt.Errorf("未知参数 %s", arg)
		}
	}
	return flags, nil
}

// parsePort 解析并校验调试端口。
func parsePort(value string) (int, error) {
	// port 是解析出的端口号。
	port, err := net.LookupPort("tcp", strings.TrimSpace(value))
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("调试端口非法: %s", value)
	}
	return port, nil
}

// requireLoopback 校验监听地址只绑定回环，避免助手被暴露到网络。
func requireLoopback(listen string) error {
	// host 是监听地址中的主机部分。
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("监听地址非法: %s", listen)
	}
	// trimmedHost 是规范化后的主机名。
	trimmedHost := strings.Trim(strings.ToLower(host), "[]")
	if trimmedHost == "127.0.0.1" || trimmedHost == "::1" || trimmedHost == "localhost" {
		return nil
	}
	return fmt.Errorf("助手只允许监听回环地址，当前为 %s", host)
}
