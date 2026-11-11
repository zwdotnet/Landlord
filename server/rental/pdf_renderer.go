package rental

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Tests can replace the renderer at their boundary; production always uses a
// local Chromium browser. No document data is sent to a conversion service.
var documentPDFRenderer = chromiumPDF
var pdfRenderSlot = make(chan struct{}, 1)

func chromiumPDF(ctx context.Context, html string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	select {
	case pdfRenderSlot <- struct{}{}:
		defer func() { <-pdfRenderSlot }()
	case <-ctx.Done():
		return nil, errors.New("PDF 生成等待超时，请稍后重试")
	}

	browser := os.Getenv("LANDLORD_PDF_BROWSER")
	if browser == "" {
		candidates := []string{"chromium", "chromium-browser", "google-chrome", "msedge"}
		if runtime.GOOS == "windows" {
			candidates = append([]string{
				`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
				`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
				`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			}, candidates...)
		}
		for _, candidate := range candidates {
			if resolved, err := exec.LookPath(candidate); err == nil {
				browser = resolved
				break
			}
		}
	}
	if browser == "" {
		return nil, errors.New("PDF 生成器未配置，请安装 Chromium 或设置 LANDLORD_PDF_BROWSER")
	}
	dir, err := os.MkdirTemp("", "landlord-pdf-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	input, output := filepath.Join(dir, "document.html"), filepath.Join(dir, "document.pdf")
	if err := os.WriteFile(input, []byte(html), 0600); err != nil {
		return nil, err
	}
	path := filepath.ToSlash(input)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	fileURL := (&url.URL{Scheme: "file", Path: path}).String()
	cmd := exec.CommandContext(ctx, browser, "--headless=new", "--disable-gpu", "--no-first-run",
		"--no-default-browser-check", "--disable-extensions", "--disable-background-networking",
		"--no-pdf-header-footer", "--user-data-dir="+filepath.Join(dir, "profile"), "--print-to-pdf="+output, fileURL)
	if err := cmd.Run(); err != nil {
		return nil, errors.New("PDF 生成失败，请检查本机 Chromium 配置后重试")
	}
	data, err := os.ReadFile(output)
	if err != nil || len(data) < 100 || !strings.HasPrefix(string(data), "%PDF-") || len(data) > 20<<20 {
		return nil, errors.New("PDF 输出不完整，请重试")
	}
	return data, nil
}
