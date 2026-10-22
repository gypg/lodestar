package relay

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// WO-052 回归：API Key 模型白名单的共享判定。
//
// 历史bug：supported_models 只在 LLM 对话路径检查（relay.go），media/utility
// 端点完全绕过——限定 gpt-4o 的 key 仍能打 gpt-image-1。抽成 apiKeyAllowsModel
// 后两条路径共享同一份语义（与 LLM 路径历史行为一致：空=不限、逗号分隔、
// 逐项 trim、精确匹配、大小写敏感）。
func TestAPIKeyAllowsModel(t *testing.T) {
	tests := []struct {
		name    string
		allowed string
		model   string
		want    bool
	}{
		{"unrestricted", "", "gpt-4o", true},
		{"listed", "gpt-4o,gpt-image-1", "gpt-image-1", true},
		{"trim entries", " gpt-4o , gpt-image-1 ", "gpt-image-1", true},
		{"unlisted", "gpt-4o", "gpt-image-1", false},
		{"exact match", "gpt-4o", "gpt-4o-mini", false},
		{"case sensitive", "gpt-4o", "GPT-4O", false},
		{"whitespace is not unrestricted", " ", "gpt-4o", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := apiKeyAllowsModel(tt.allowed, tt.model); got != tt.want {
				t.Fatalf("apiKeyAllowsModel(%q, %q) = %v, want %v", tt.allowed, tt.model, got, tt.want)
			}
		})
	}
}

// 每个 media/utility 端点都必须在路由解析前拒绝白名单外的模型——
// 含 multipart 端点（历史上模型名从 form 字段提取后直接进路由）。
func TestMediaHandlerRejectsUnsupportedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for endpoint, cfg := range mediaEndpointConfigs {
		t.Run(cfg.UpstreamPath, func(t *testing.T) {
			var body *bytes.Buffer
			contentType := "application/json"
			if cfg.MultipartInput {
				body = &bytes.Buffer{}
				writer := multipart.NewWriter(body)
				if err := writer.WriteField("model", "forbidden-model"); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				contentType = writer.FormDataContentType()
			} else {
				body = bytes.NewBufferString(`{"model":"forbidden-model"}`)
			}

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, cfg.UpstreamPath, body)
			ctx.Request.Header.Set("Content-Type", contentType)
			ctx.Set("supported_models", "allowed-model")

			MediaHandler(endpoint, ctx)

			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "model not supported") {
				t.Fatalf("response = %d %s, want model whitelist rejection before routing", recorder.Code, recorder.Body.String())
			}
		})
	}
}

// 白名单内的模型必须放行到路由解析（不会被误拦）——拒绝 404
// （model not found in media relay）说明已越过白名单检查。
func TestMediaHandlerAllowsListModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations",
		bytes.NewBufferString(`{"model":"allowed-model"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("supported_models", "allowed-model,other-model")

	// 不初始化 DB/缓存：路由解析找不到分组会返回 404，恰好证明请求穿过了
	// 白名单检查。若白名单误拦，会先收到 400 "model not supported"。
	MediaHandler(MediaEndpointImageGeneration, ctx)

	if recorder.Code == http.StatusBadRequest && strings.Contains(recorder.Body.String(), "model not supported") {
		t.Fatalf("listed model was rejected by the whitelist: %d %s", recorder.Code, recorder.Body.String())
	}
}
