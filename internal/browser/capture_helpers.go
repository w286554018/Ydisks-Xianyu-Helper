package browser

import (
	"fmt"

	"github.com/mxschmitt/playwright-go"
)

// ChromiumLaunchArgsForCapture 暴露启动参数供独立抓包工具与生产浏览器保持一致指纹。
func ChromiumLaunchArgsForCapture() []string {
	return chromiumLaunchArgs()
}

// AddCookieStrForCapture 把 cookie 字符串注入浏览器上下文，供独立抓包工具复用解析逻辑。
func AddCookieStrForCapture(ctx playwright.BrowserContext, cookieStr string) error {
	// err 保存逐条 cookie 注入过程中的错误。
	if err := addCookieStr(ctx, cookieStr); err != nil {
		return fmt.Errorf("注入 cookie 失败: %w", err)
	}
	return nil
}
