package helper

import (
	"context"
	"net/http"
	"time"
)

func GetUrlDelay(httpClient *http.Client, url string, ctx context.Context) (int, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return int(time.Since(start).Milliseconds()), nil
}
