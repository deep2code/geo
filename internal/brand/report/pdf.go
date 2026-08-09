package report

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// GeneratePDF 将 GenerateHTML 的输出渲染为 PDF（A4，0.8 寸页边距）。
//
// 需要当前系统可运行 headless Chromium（macOS 多数预装；Linux 需要安装 chromium 包）。
// 无 Chromium 或 chromedp 初始化失败时返回带降级指引的错误。
//
// 可选环境变量：
//   GEO_PDF_WAIT_MS   页面渲染后等待毫秒数（默认 600，给 SVG 动画/字体加载留足时间）
//   GEO_CHROME_PATH   自定义 Chromium 可执行文件路径（默认自动查找）
func GeneratePDF(ctx context.Context, html string) ([]byte, error) {
	if html == "" {
		return nil, fmt.Errorf("report: 空 HTML 无法生成 PDF")
	}
	wait := 600
	if s := os.Getenv("GEO_PDF_WAIT_MS"); s != "" {
		if n, err := parseInt(s); err == nil && n > 0 {
			wait = n
		}
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoSandbox,
		chromedp.DisableGPU,
		chromedp.Headless,
	)
	if cp := os.Getenv("GEO_CHROME_PATH"); cp != "" {
		opts = append(opts, chromedp.ExecPath(cp))
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	timeoutCtx, cancelTimeout := context.WithTimeout(browserCtx, 45*time.Second)
	defer cancelTimeout()

	var buf []byte
	err := chromedp.Run(timeoutCtx,
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			frameTree, err := page.GetFrameTree().Do(ctx)
			if err != nil {
				return err
			}
			return page.SetDocumentContent(frameTree.Frame.ID, html).Do(ctx)
		}),
		chromedp.Sleep(time.Duration(wait)*time.Millisecond),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			// A4: 8.27 x 11.69 英寸，margin 0.8 寸
			marginTop, marginBottom, marginLeft, marginRight := 0.8, 0.8, 0.8, 0.8
			buf, _, err = page.PrintToPDF().
				WithPaperWidth(8.27).
				WithPaperHeight(11.69).
				WithMarginTop(marginTop).
				WithMarginBottom(marginBottom).
				WithMarginLeft(marginLeft).
				WithMarginRight(marginRight).
				WithPrintBackground(true).
				WithPreferCSSPageSize(true).
				WithLandscape(false).
				Do(ctx)
			return err
		}),
	)
	if err != nil {
		var (
			// chromedp 在无 Chromium 时通常会返回 *exec.Error 或含 "executable" / "not found"
			// 字样的错误；这里统一给出降级指引。
			notFound = errors.Is(err, context.DeadlineExceeded) ||
				containsAny(err.Error(), "executable", "not found", "chrome", "chromium", "exec:")
		)
		if notFound {
			return nil, fmt.Errorf("report: 服务端 PDF 需要 headless Chromium（请安装 chromium/google-chrome，或设置 GEO_CHROME_PATH，或改用 HTML 版本后在浏览器打印为 PDF）: %w", err)
		}
		return nil, fmt.Errorf("report: PDF 渲染失败: %w", err)
	}
	return buf, nil
}

func parseInt(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("非数字字符: %q", s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if sub == "" {
			continue
		}
		if strings.Contains(strings.ToLower(s), sub) {
			return true
		}
	}
	return false
}
