package openai

import (
	"testing"

	"github.com/gypg/lodestar/internal/transformer/model"
)

func TestIsMoonshotCompatRequest(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		model   string
		want    bool
	}{
		{"base moonshot", "https://api.moonshot.cn", "kimi-k2.5", true},
		{"base kimi", "https://api.kimi.com/coding", "kimi-k2.5", true},
		{"model only", "https://example.com", "kimi-k2.6", true},
		{"unrelated", "https://api.openai.com", "gpt-4o", false},
		{"empty", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := &model.InternalLLMRequest{Model: c.model}
			if got := isMoonshotCompatRequest(c.baseURL, req); got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestIsZhipuCompatRequest(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		model   string
		want    bool
	}{
		{"base bigmodel", "https://open.bigmodel.cn/api/paas/v4", "glm-4", true},
		{"base z.ai", "https://api.z.ai/api/paas/v4", "glm-4.6", true},
		{"model only", "https://example.com", "glm-4v", true},
		{"unrelated", "https://api.openai.com", "gpt-4o", false},
		{"empty", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := &model.InternalLLMRequest{Model: c.model}
			if got := isZhipuCompatRequest(c.baseURL, req); got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestSanitizeMoonshotForcesTemperatureOneForKimiK26(t *testing.T) {
	temp := 0.3
	req := &model.InternalLLMRequest{Model: "kimi-k2.6", Temperature: &temp}
	sanitizeMoonshotRequest(req, "https://api.moonshot.cn")
	if req.Temperature == nil || *req.Temperature != 1.0 {
		t.Fatalf("temperature = %v, want 1.0", req.Temperature)
	}
}

func TestSanitizeMoonshotLeavesNonK26Alone(t *testing.T) {
	temp := 0.3
	req := &model.InternalLLMRequest{Model: "kimi-k2.5", Temperature: &temp}
	sanitizeMoonshotRequest(req, "https://api.moonshot.cn")
	if req.Temperature == nil || *req.Temperature != 0.3 {
		t.Fatalf("temperature = %v, want 0.3 (untouched for non-k2.6)", req.Temperature)
	}
}

func TestSanitizeMoonshotLeavesNilTemperatureAlone(t *testing.T) {
	req := &model.InternalLLMRequest{Model: "kimi-k2.6", Temperature: nil}
	sanitizeMoonshotRequest(req, "https://api.moonshot.cn")
	if req.Temperature != nil {
		t.Fatalf("temperature = %v, want nil (do not inject default)", req.Temperature)
	}
}

func TestSanitizeMoonshotNoopForNonMoonshot(t *testing.T) {
	temp := 0.3
	req := &model.InternalLLMRequest{Model: "gpt-4o", Temperature: &temp}
	sanitizeMoonshotRequest(req, "https://api.openai.com")
	if req.Temperature == nil || *req.Temperature != 0.3 {
		t.Fatalf("temperature = %v, want 0.3 (non-moonshot untouched)", req.Temperature)
	}
}

func TestSanitizeZhipuClampsTopP(t *testing.T) {
	cases := []struct {
		name string
		topP float64
		want float64
	}{
		{"exactly 1.0", 1.0, 0.99},
		{"above 1.0", 1.5, 0.99},
		{"below 1.0 unchanged", 0.8, 0.8},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			topP := c.topP
			req := &model.InternalLLMRequest{Model: "glm-4", TopP: &topP}
			sanitizeZhipuRequest(req, "https://open.bigmodel.cn/api/paas/v4")
			if req.TopP == nil || *req.TopP != c.want {
				t.Fatalf("topP = %v, want %v", req.TopP, c.want)
			}
		})
	}
}

func TestSanitizeZhipuLeavesNilTopPAlone(t *testing.T) {
	req := &model.InternalLLMRequest{Model: "glm-4", TopP: nil}
	sanitizeZhipuRequest(req, "https://open.bigmodel.cn/api/paas/v4")
	if req.TopP != nil {
		t.Fatalf("topP = %v, want nil", req.TopP)
	}
}

func TestSanitizeZhipuStripsDataURLImagePrefix(t *testing.T) {
	dataURI := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUg=="
	req := &model.InternalLLMRequest{
		Model: "glm-4v",
		Messages: []model.Message{
			{
				Role: "user",
				Content: model.MessageContent{
					MultipleContent: []model.MessageContentPart{
						{Type: "image_url", ImageURL: &model.ImageURL{URL: dataURI}},
					},
				},
			},
		},
	}
	sanitizeZhipuRequest(req, "https://open.bigmodel.cn/api/paas/v4")

	got := req.Messages[0].Content.MultipleContent[0].ImageURL.URL
	const want = "iVBORw0KGgoAAAANSUhEUg=="
	if got != want {
		t.Fatalf("image url = %q, want raw base64 %q", got, want)
	}
}

func TestSanitizeZhipuLeavesHTTPSImageURLAlone(t *testing.T) {
	url := "https://example.com/cat.png"
	req := &model.InternalLLMRequest{
		Model: "glm-4v",
		Messages: []model.Message{
			{
				Role: "user",
				Content: model.MessageContent{
					MultipleContent: []model.MessageContentPart{
						{Type: "image_url", ImageURL: &model.ImageURL{URL: url}},
					},
				},
			},
		},
	}
	sanitizeZhipuRequest(req, "https://open.bigmodel.cn/api/paas/v4")
	got := req.Messages[0].Content.MultipleContent[0].ImageURL.URL
	if got != url {
		t.Fatalf("image url = %q, want %q (https url must not be stripped)", got, url)
	}
}

func TestSanitizeZhipuNoopForNonZhipu(t *testing.T) {
	topP := 1.5
	req := &model.InternalLLMRequest{Model: "gpt-4o", TopP: &topP}
	sanitizeZhipuRequest(req, "https://api.openai.com")
	if req.TopP == nil || *req.TopP != 1.5 {
		t.Fatalf("topP = %v, want 1.5 (non-zhipu untouched)", req.TopP)
	}
}

func TestStripDataURLPrefix(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"data:image/png;base64,AAA", "AAA"},
		{"data:image/jpeg;base64,BBB", "BBB"},
		{"data:image/webp;base64,CCC", "CCC"},
		{"https://x/y.png", "https://x/y.png"},
		{"not-a-data-uri", "not-a-data-uri"},
		{"", ""},
	}
	for _, c := range cases {
		if got := stripDataURLPrefix(c.in); got != c.want {
			t.Fatalf("stripDataURLPrefix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
