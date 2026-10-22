package source

import (
	"context"
	"net/http"

	"github.com/zzzwannasleep/assm/internal/httpx"
)

// getJSON 抓取一个 URL 并返回原始字节（调用方自己解析）。
func getJSON(ctx context.Context, client *http.Client, rawURL string) ([]byte, int, error) {
	return httpx.Get(ctx, client, rawURL, map[string]string{"Accept": "application/json"})
}

// getText 抓取 HTML/XML 文本。
func getText(ctx context.Context, client *http.Client, rawURL string) ([]byte, int, error) {
	return httpx.Get(ctx, client, rawURL, map[string]string{"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"})
}
