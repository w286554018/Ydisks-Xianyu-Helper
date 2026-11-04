// Command capture-lifecycle 捕获闲鱼商品下架/删除的平台接口 ground truth。
// 用法：
//
//	go run ./cmd/capture-lifecycle -cookie-file <cookie.txt> -out protocol-notes.json [-auto-item <商品ID>]
//
// 打开有头 Chromium 加载页面，只监听并落盘页面自发请求（不构造、不重放任何平台调用）。
// 不带 -auto-item 时等待 Ctrl+C 手动退出；带 -auto-item 时进入步进模式，
// 每一步等待 /tmp/lifecycle-capture/next-step 触发文件指令（open/diag/locate/btns/down/more/delete/confirm/done）。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mxschmitt/playwright-go"

	"xianyu-go/internal/browser"
)

// capturedRequest 保存一次页面自发请求的关键信息。
type capturedRequest struct {
	// At 是捕获时刻。
	At time.Time `json:"at"`
	// URL 是完整请求地址。
	URL string `json:"url"`
	// Method 是 HTTP 方法。
	Method string `json:"method"`
	// API 是从 URL 提取的 mtop API 名称。
	API string `json:"api,omitempty"`
	// Headers 是与签名相关的请求头。
	Headers map[string]string `json:"headers,omitempty"`
	// Body 是请求正文（脱敏后截断）。
	Body string `json:"body,omitempty"`
}

// captureOptions 保存一次抓包运行的全部输入参数。
type captureOptions struct {
	// CookieFile 是账号 cookie 字符串文件路径。
	CookieFile string
	// Out 是捕获结果输出文件路径。
	Out string
	// StartURL 是打开的初始页面。
	StartURL string
	// Proxy 是可选代理地址。
	Proxy string
	// AutoItemID 非空时进入步进驱动模式并作用于该商品。
	AutoItemID string
}

// main 解析命令行参数并启动抓包流程。
func main() {
	// cookieFile 是账号 cookie 字符串文件路径。
	cookieFile := flag.String("cookie-file", "", "账号 cookie 字符串文件")
	// out 是捕获结果输出文件。
	out := flag.String("out", "protocol-notes.json", "捕获输出文件")
	// startURL 是打开的初始页面。
	startURL := flag.String("url", "https://www.goofish.com/", "起始页面")
	// proxyFlag 是可选代理地址。
	proxyFlag := flag.String("proxy", "", "可选代理，例如 http://127.0.0.1:7892")
	// autoItemID 非空时进入步进模式对指定商品执行下架与删除。
	autoItemID := flag.String("auto-item", "", "步进模式目标商品 ID")
	flag.Parse()

	// opts 汇总命令行输入。
	opts := captureOptions{CookieFile: *cookieFile, Out: *out, StartURL: *startURL, Proxy: *proxyFlag, AutoItemID: *autoItemID}
	if opts.CookieFile == "" {
		fmt.Fprintln(os.Stderr, "错误：必须通过 -cookie-file 提供登录态")
		os.Exit(1)
	}
	// err 保存抓包运行错误。
	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "抓包失败:", err)
		os.Exit(1)
	}
}

// run 完成一次抓包运行：启动浏览器、挂监听、等待结束方式并落盘捕获结果。
func run(opts captureOptions) error {
	// raw、err 保存 cookie 文件原始内容与读取错误。
	raw, err := os.ReadFile(opts.CookieFile)
	if err != nil {
		return fmt.Errorf("读取 cookie 失败: %w", err)
	}
	// cookieStr 是去除首尾空白后的账号 Cookie 明文。
	cookieStr := strings.TrimSpace(string(raw))

	// pw、chromium、context、page 及清理函数由启动流程统一创建。
	pw, chromium, context, page, stopBrowser, startErr := startBrowser(opts, cookieStr)
	if startErr != nil {
		return startErr
	}
	// captured 是捕获集合；record 负责追加并即时打印。
	captured := make([]capturedRequest, 0)
	// record 把一次请求追加进捕获集合并即时打印。
	record := func(request capturedRequest) {
		captured = append(captured, request)
		// apiText 供终端即时提示。
		apiText := request.API
		if apiText == "" {
			apiText = request.URL
		}
		fmt.Printf("[捕获] %s %s\n", request.Method, apiText)
	}
	// 新开标签页同样被监听。
	context.On("page", func(newPage playwright.Page) {
		attachListener(newPage, record)
	})
	// 初始页面也挂上监听。
	attachListener(page, record)

	// gotoErr 保存起始页导航错误；失败不阻断，用户仍可手动导航。
	if _, gotoErr := page.Goto(opts.StartURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded}); gotoErr != nil {
		fmt.Fprintf(os.Stderr, "打开页面失败: %v\n", gotoErr)
	}

	fmt.Println("=== 抓包就绪 ===")
	if opts.AutoItemID != "" {
		fmt.Println("步进模式：写入 /tmp/lifecycle-capture/next-step 逐条下发指令")
		driveSteps(page, opts.AutoItemID)
		// 留 2 秒让尾部请求完成记录。
		time.Sleep(2 * time.Second)
	} else {
		fmt.Println("1. 在窗口中进入「我的商品/卖家中心」")
		fmt.Println("2. 对测试商品点一次「下架」，等待完成")
		fmt.Println("3. 对另一个测试商品点一次「删除」，等待完成")
		fmt.Println("完成后按 Ctrl+C 退出并保存捕获结果")
		// signals 等待中断信号。
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
		<-signals
	}

	// saveErr 保存捕获集合落盘错误。
	if saveErr := saveCaptured(opts.Out, captured); saveErr != nil {
		_ = context.Close()
		_ = chromium.Close()
		_ = pw.Stop()
		return saveErr
	}
	_ = context.Close()
	_ = chromium.Close()
	_ = pw.Stop()
	stopBrowser()
	return nil
}

// startBrowser 启动有头 Chromium 并注入登录态，返回运行时、页面与清理函数。
func startBrowser(opts captureOptions, cookieStr string) (*playwright.Playwright, playwright.Browser, playwright.BrowserContext, playwright.Page, func(), error) {
	// options 是启动配置；有头模式加可选代理。
	options := playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(false), Args: browser.ChromiumLaunchArgsForCapture()}
	if opts.Proxy != "" {
		options.Proxy = &playwright.Proxy{Server: opts.Proxy}
	}
	// pw 是 playwright 运行时。
	pw, err := playwright.Run()
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("启动 playwright 失败: %w", err)
	}
	// chromium 是有头浏览器实例。
	chromium, err := pw.Chromium.Launch(options)
	if err != nil {
		_ = pw.Stop()
		return nil, nil, nil, nil, nil, fmt.Errorf("启动 chromium 失败: %w", err)
	}
	// context 是注入登录态的浏览器上下文。
	context, err := chromium.NewContext(playwright.BrowserNewContextOptions{
		Viewport:   &playwright.Size{Width: 1440, Height: 900},
		Locale:     playwright.String("zh-CN"),
		TimezoneId: playwright.String("Asia/Shanghai"),
	})
	if err != nil {
		_ = chromium.Close()
		_ = pw.Stop()
		return nil, nil, nil, nil, nil, fmt.Errorf("创建 context 失败: %w", err)
	}
	// cookieErr 保存 cookie 注入错误。
	if cookieErr := browser.AddCookieStrForCapture(context, cookieStr); cookieErr != nil {
		_ = context.Close()
		_ = chromium.Close()
		_ = pw.Stop()
		return nil, nil, nil, nil, nil, fmt.Errorf("注入 cookie 失败: %w", cookieErr)
	}
	// page 是用户操作的页面。
	page, err := context.NewPage()
	if err != nil {
		_ = context.Close()
		_ = chromium.Close()
		_ = pw.Stop()
		return nil, nil, nil, nil, nil, fmt.Errorf("新建页面失败: %w", err)
	}
	// 自动接受页面原生弹窗（confirm/alert/prompt），避免 Playwright 默认 dismiss 使确认被静默取消。
	page.On("dialog", func(d playwright.Dialog) {
		fmt.Println("[弹窗]", d.Type(), d.Message())
		_ = d.Accept()
	})
	// stopBrowser 停留为空操作；资源关闭统一由 run 收尾处理。
	stopBrowser := func() {}
	return pw, chromium, context, page, stopBrowser, nil
}

// filterAPI 判断 URL 是否值得记录：只记录平台业务接口，过滤静态资源。
func filterAPI(url string) bool {
	if !strings.Contains(url, "goofish.com") && !strings.Contains(url, "taobao.com") && !strings.Contains(url, "alibaba.com") {
		return false
	}
	// suffix 是当前检查的静态资源后缀。
	for _, suffix := range []string{".png", ".jpg", ".gif", ".css", ".js", ".woff", ".svg", ".webp"} {
		if strings.HasSuffix(strings.Split(url, "?")[0], suffix) {
			return false
		}
	}
	return true
}

// attachListener 为给定页面注册请求监听，把符合条件请求交给 record。
func attachListener(page playwright.Page, record func(capturedRequest)) {
	page.On("request", func(request playwright.Request) {
		// url 是当前请求的完整地址。
		url := request.URL()
		if !filterAPI(url) {
			return
		}
		// entry 是待记录的请求条目。
		entry := capturedRequest{At: time.Now(), URL: url, Method: request.Method()}
		// allHeaders、headerErr 保存全部请求头与读取错误；只挑选签名相关字段记录。
		if allHeaders, headerErr := request.AllHeaders(); headerErr == nil {
			// name 是当前检查的签名相关头字段名。
			for _, name := range []string{"x-sign", "x-s", "x-s-common", "x-mini-wua", "x-umt", "x-t", "x-ttid", "x-appkey", "x-c-traceid", "wua", "content-type"} {
				// value、ok 保存命中的头字段值与存在标记。
				if value, ok := allHeaders[name]; ok {
					entry.Headers[name] = value
				}
			}
		}
		// data、postErr 保存请求体与读取错误，失败不阻断。
		if data, postErr := request.PostData(); postErr == nil && data != "" {
			// 截断过长的正文。
			if len(data) > 4000 {
				data = data[:4000] + "...(截断)"
			}
			entry.Body = data
		}
		if strings.Contains(url, "/h5/mtop.") {
			// parts 提取 mtop API 名。
			parts := strings.Split(url, "/h5/mtop.")
			if len(parts) > 1 {
				// segment 取 API 名主体段，用于还原 mtop 全名。
				segment := strings.Split(parts[1], "/")
				entry.API = "mtop." + segment[0]
			}
		}
		record(entry)
	})
}

// saveCaptured 把捕获集合格式化写入输出文件。
func saveCaptured(out string, captured []capturedRequest) error {
	// file、writeErr 保存输出文件与创建错误。
	file, writeErr := os.Create(out)
	if writeErr != nil {
		return fmt.Errorf("创建输出文件失败: %w", writeErr)
	}
	defer file.Close()
	// encoder 是格式化 JSON 输出器，逐条写入捕获记录。
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	// encodeErr 保存捕获集合写出错误。
	if encodeErr := encoder.Encode(captured); encodeErr != nil {
		return fmt.Errorf("写出捕获失败: %w", encodeErr)
	}
	fmt.Printf("已保存 %d 条捕获到 %s\n", len(captured), out)
	return nil
}

// driveSteps 步进驱动页面：每一步等待 /tmp/lifecycle-capture/next-step 触发文件中的指令并执行。
func driveSteps(page playwright.Page, itemID string) {
	// dumpPageDiag 输出页面上关键候选文本计数与正文摘要，帮助定位卖家管理入口。
	dumpPageDiag := func(tag string) {
		// kw 是当前检查的候选关键词。
		for _, kw := range []string{"下架", "删除", "编辑", "更多", "管理", "卖家", "擦亮", "重新编辑", "登录", "验证", "滑动", "拖动"} {
			// c、e 保存关键词命中数量与计数错误。
			if c, e := page.Locator("text=" + kw).Count(); e == nil {
				fmt.Printf("[诊断/%s] %s x%d\n", tag, kw, c)
			}
		}
		// txt、e 保存页面正文文本与读取错误。
		if txt, e := page.Locator("body").First().InnerText(); e == nil {
			// runes 用于按字符截断过长的正文摘要。
			runes := []rune(txt)
			if len(runes) > 400 {
				runes = runes[:400]
			}
			fmt.Printf("[诊断/%s] 正文摘要: %s\n", tag, string(runes))
		}
	}
	// shot 截图到 /tmp/lifecycle-capture/ 下的指定文件名。
	shot := func(name string) {
		_, _ = page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String("/tmp/lifecycle-capture/" + name)})
	}
	// waitForStep 轮询等待触发文件出现，返回指令内容并删除触发文件。
	waitForStep := func() string {
		// path 是步进指令触发文件路径。
		path := "/tmp/lifecycle-capture/next-step"
		for {
			// data、err 保存触发文件内容与读取错误；读到即消费并删除。
			if data, err := os.ReadFile(path); err == nil {
				_ = os.Remove(path)
				return strings.TrimSpace(string(data))
			}
			page.WaitForTimeout(500)
		}
	}
	// clickFirst 点击第一个精确匹配文本的元素，返回是否成功。
	clickFirst := func(text string) bool {
		// loc 使用精确文本匹配，避免命中包含多个按钮文本的父容器。
		loc := page.GetByText(text, playwright.PageGetByTextOptions{Exact: playwright.Bool(true)}).First()
		// c、e 保存元素命中数量与计数错误。
		if c, e := loc.Count(); e != nil || c == 0 {
			fmt.Println("[步进] 未找到:", text)
			return false
		}
		// clickErr 保存点击执行错误。
		if clickErr := loc.Click(); clickErr != nil {
			fmt.Println("[步进] 点击失败:", text, clickErr)
			return false
		}
		fmt.Println("[步进] 已点击:", text)
		return true
	}
	fmt.Println("[步进] 等待指令（写入 /tmp/lifecycle-capture/next-step）")
	for {
		// step 是触发文件里的下一条步进指令。
		step := waitForStep()
		fmt.Println("[步进] 执行:", step)
		switch step {
		case "open":
			// itemURL 是目标商品详情页。
			itemURL := "https://www.goofish.com/item?id=" + itemID
			// gotoErr 保存商品页导航错误。
			if _, gotoErr := page.Goto(itemURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(30000)}); gotoErr != nil {
				fmt.Fprintf(os.Stderr, "[步进] 打开商品页失败: %v\n", gotoErr)
			}
			page.WaitForTimeout(4000)
			dumpPageDiag("open")
			shot("step-open.png")
		case "locate":
			// locateOut 收集页面（含 shadow DOM）中含目标文本的元素信息，用于定位真实可点入口。
			locateOut, locErr := page.Evaluate(`() => {
				const out = [];
				const collect = (el) => {
					if (el.textContent && (el.textContent.includes("下架") || el.textContent.includes("删除"))) {
						const r = el.getBoundingClientRect();
						const st = getComputedStyle(el);
						out.push({tag: el.tagName, cls: String(el.className).slice(0,60), text: el.textContent.trim().slice(0,12), x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height), vis: st.visibility, disp: st.display});
					}
				};
				const walk = (root) => {
					for (const el of root.querySelectorAll("*")) {
						collect(el);
						if (el.shadowRoot) walk(el.shadowRoot);
					}
				};
				walk(document);
				return JSON.stringify(out.slice(0, 40));
			}`)
			fmt.Println("[定位]", locErr, locateOut)
			shot("step-locate.png")
		case "btns":
			// btnsOut 列出页面（含 shadow DOM）中全部可见按钮文本与位置，用于确认弹窗按钮。
			btnsOut, btnErr := page.Evaluate(`() => {
				const out = [];
				const walk = (root) => {
					for (const el of root.querySelectorAll("*")) {
						const txt = (el.tagName === "BUTTON" || el.tagName === "A" || el.getAttribute("role") === "button") ? el.textContent.trim() : "";
						if (txt && txt.length <= 10) {
							const r = el.getBoundingClientRect();
							if (r.width > 0 && r.height > 0) out.push({tag: el.tagName, text: txt.slice(0,10), x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height)});
						}
						if (el.shadowRoot) walk(el.shadowRoot);
					}
				};
				walk(document);
				return JSON.stringify(out.slice(0, 60));
			}`)
			fmt.Println("[按钮]", btnErr, btnsOut)
			shot("step-btns.png")
		case "diag":
			dumpPageDiag("diag")
			shot("step-diag.png")
		case "down":
			clickFirst("下架")
			page.WaitForTimeout(2000)
			dumpPageDiag("down")
			shot("step-down.png")
		case "confirm":
			clickFirst("确定")
			page.WaitForTimeout(2500)
			dumpPageDiag("confirm")
			shot("step-confirm.png")
		case "more":
			clickFirst("更多")
			page.WaitForTimeout(1500)
			dumpPageDiag("more")
			shot("step-more.png")
		case "delete":
			clickFirst("删除")
			page.WaitForTimeout(2000)
			dumpPageDiag("delete")
			shot("step-delete.png")
		case "done":
			fmt.Println("[步进] 结束")
			return
		default:
			fmt.Println("[步进] 未知指令:", step)
		}
	}
}
