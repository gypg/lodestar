package cloudflare

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	transformermodel "github.com/gypg/lodestar/internal/transformer/model"
)

// TestChatOutboundTransformRequest_StripsModelFromBody 防止回归：
// Cloudflare Workers AI 的模型名由 URL 路径承载，请求体中不得出现 model 字段，
// 否则会触发其 anyOf 输入格式动态检测失败（oneOf 0 matches）。
func TestChatOutboundTransformRequest_StripsModelFromBody(t *testing.T) {
	userMsg := "Hello, what is the origin of the phrase Hello, World?"
	sysMsg := "You are a concise assistant."
	request := &transformermodel.InternalLLMRequest{
		Model: "@cf/openai/gpt-oss-120b",
		Messages: []transformermodel.Message{
			{Role: "system", Content: transformermodel.MessageContent{Content: &sysMsg}},
			{Role: "user", Content: transformermodel.MessageContent{Content: &userMsg}},
		},
	}

	httpReq, err := (&ChatOutbound{}).TransformRequest(
		context.Background(),
		request,
		"https://api.cloudflare.com/client/v4/accounts/abc",
		"sk-test",
	)
	if err != nil {
		t.Fatalf("TransformRequest error: %v", err)
	}

	if got, want := httpReq.URL.String(), "https://api.cloudflare.com/client/v4/accounts/abc/ai/run/@cf/openai/gpt-oss-120b"; got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}

	body, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("body is not a JSON object: %v\nbody: %s", err, string(body))
	}

	if _, ok := parsed["model"]; ok {
		t.Errorf("request body must not contain a model field; body: %s", string(body))
	}

	msgs, ok := parsed["messages"]
	if !ok {
		t.Fatalf("request body missing messages; body: %s", string(body))
	}
	if !strings.Contains(string(msgs), sysMsg) || !strings.Contains(string(msgs), userMsg) {
		t.Errorf("messages content lost; body: %s", string(body))
	}
}
